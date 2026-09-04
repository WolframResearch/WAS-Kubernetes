package stages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

type retryReporter struct {
	lines []string
}

func (r *retryReporter) SubstepStart(string) {}
func (r *retryReporter) SubstepDone()        {}
func (r *retryReporter) SubstepFail(error)   {}
func (r *retryReporter) LogLine(line string) { r.lines = append(r.lines, line) }

type countingRunner struct {
	calls   int
	errs    []error
	callIdx int
}

func (c *countingRunner) Run(_ context.Context, _ []string, _ []string, _ runner.Reporter) error {
	if c.callIdx < len(c.errs) {
		err := c.errs[c.callIdx]
		c.callIdx++
		c.calls++
		return err
	}
	c.calls++
	return nil
}

func (c *countingRunner) Output(context.Context, []string, []string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func TestIsTransientNetworkError(t *testing.T) {
	transient := []string{
		`terraform init failed: Error: failed to lock azure state: HTTP response was nil; connection may have been reset`,
		`terraform init failed: Put "https://acct.blob.core.windows.net/...": i/o timeout`,
		`terraform init failed: dial tcp 20.60.1.2:443: i/o timeout`,
		`terraform init failed: lookup acct.blob.core.windows.net on 127.0.0.53:53: read udp 127.0.0.1:57529->127.0.0.53:53: i/o timeout`,
		`Error loading state: executing request: Get "https://acct.blob.core.windows.net/tfstate/stack/terraform.tfstate": dial tcp: lookup ... i/o timeout`,
	}
	for _, msg := range transient {
		if !isTransientNetworkError(errors.New(msg)) {
			t.Errorf("expected transient: %q", msg)
		}
	}

	permanent := []string{
		`terraform apply failed: Error: creating EKS Cluster: AccessDenied`,
		`terraform init failed: Error: Backend configuration changed`,
		`helm install failed: chart not found`,
		`helm install failed: unexpected EOF`,
		`kubectl failed: Error looking up resource`,
		`terraform init failed: Error: failed to lock azure state: state blob is already locked`,
		`Error loading state: failed to lock azure state: blob metadata "terraformlockid" was empty`,
	}
	for _, msg := range permanent {
		if isTransientNetworkError(errors.New(msg)) {
			t.Errorf("expected permanent: %q", msg)
		}
	}
}

func TestIsAzureStateBlobLocked(t *testing.T) {
	if !isAzureStateBlobLocked(errors.New(`state blob is already locked`)) {
		t.Fatal("expected locked")
	}
	if !isAzureStateBlobLocked(errors.New(`blob metadata "terraformlockid" was empty`)) {
		t.Fatal("expected locked for empty terraformlockid")
	}
	if isAzureStateBlobLocked(errors.New(`dial tcp: i/o timeout`)) {
		t.Fatal("network timeout is not a lock conflict")
	}
}

func TestRunCmdBreaksAzureStateLeaseThenRetries(t *testing.T) {
	oldDelay := cmdRetryBaseDelay
	cmdRetryBaseDelay = 10 * time.Millisecond
	t.Cleanup(func() { cmdRetryBaseDelay = oldDelay })

	dir := t.TempDir()
	backendPath := filepath.Join(dir, "backend.tfvars")
	content := `resource_group_name  = "rg"
storage_account_name = "acct"
container_name       = "tfstate"
key                  = "stack/terraform.tfstate"
access_key           = "secret"
`
	if err := os.WriteFile(backendPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	broke := 0
	oldBreak := breakAzureTFStateLease
	breakAzureTFStateLease = func(ctx context.Context, path string) error {
		broke++
		if path != backendPath {
			t.Fatalf("path=%q", path)
		}
		return nil
	}
	t.Cleanup(func() { breakAzureTFStateLease = oldBreak })

	locked := errors.New(`terraform init failed:
Error: Error loading state: failed to lock azure state: state blob is already locked
blob metadata "terraformlockid" was empty`)
	r := &countingRunner{errs: []error{locked, nil}}
	rep := &retryReporter{}
	cfg := &config.Config{}

	err := runCmd(context.Background(), cfg, r, rep,
		[]string{"terraform", "-chdir=" + dir, "init", "-reconfigure", "-backend-config=" + backendPath})
	if err != nil {
		t.Fatalf("expected success after lease break; got %v", err)
	}
	if broke != 1 {
		t.Fatalf("expected 1 lease break; got %d", broke)
	}
	if r.calls != 2 {
		t.Fatalf("expected 2 terraform attempts; got %d", r.calls)
	}
}

func TestRunCmdDoesNotRetryNonTerraform(t *testing.T) {
	transientErr := errors.New(`dial tcp: i/o timeout`)
	r := &countingRunner{errs: []error{transientErr}}
	rep := &retryReporter{}
	cfg := &config.Config{}

	err := runCmd(context.Background(), cfg, r, rep, []string{"helm", "install", "was", "./chart"})
	if err == nil {
		t.Fatal("expected error")
	}
	if r.calls != 1 {
		t.Fatalf("expected single attempt for non-terraform; got %d", r.calls)
	}
}

func TestRunCmdRetriesTransientNetworkError(t *testing.T) {
	oldDelay := cmdRetryBaseDelay
	cmdRetryBaseDelay = 10 * time.Millisecond
	t.Cleanup(func() { cmdRetryBaseDelay = oldDelay })

	transientErr := fmt.Errorf(`terraform init failed:
Error: Error loading state: failed to lock azure state: HTTP response was nil; connection may have been reset`)

	r := &countingRunner{errs: []error{transientErr, transientErr, nil}}
	rep := &retryReporter{}
	cfg := &config.Config{}

	start := time.Now()
	err := runCmd(context.Background(), cfg, r, rep, []string{"terraform", "-chdir=/tmp/stack", "init"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success after retries; got: %v", err)
	}
	if r.calls != 3 {
		t.Fatalf("expected 3 attempts; got %d", r.calls)
	}
	if elapsed < cmdRetryBaseDelay {
		t.Fatalf("expected at least one retry wait; elapsed=%s", elapsed)
	}

	retryLogs := 0
	for _, line := range rep.lines {
		if strings.Contains(line, "[retry] transient network error") {
			retryLogs++
		}
	}
	if retryLogs != 2 {
		t.Fatalf("expected 2 retry log lines; got %d: %v", retryLogs, rep.lines)
	}
}

func TestRunCmdDoesNotRetryPermanentError(t *testing.T) {
	permanentErr := errors.New(`terraform apply failed: Error: creating resource: AccessDenied`)

	r := &countingRunner{errs: []error{permanentErr}}
	rep := &retryReporter{}
	cfg := &config.Config{}

	err := runCmd(context.Background(), cfg, r, rep, []string{"terraform", "-chdir=/tmp/stack", "apply", "-auto-approve"})
	if err == nil {
		t.Fatal("expected permanent error")
	}
	if r.calls != 1 {
		t.Fatalf("expected single attempt; got %d", r.calls)
	}
}
