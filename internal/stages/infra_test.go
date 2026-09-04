package stages_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

func fakeInfraCfg(t *testing.T, withBackend bool) *config.Config {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra", "aws", "stack"), 0755); err != nil {
		t.Fatal(err)
	}
	if withBackend {
		p := filepath.Join(root, "infra", "aws", "stack", "backend.hcl")
		if err := os.WriteFile(p, []byte("bucket = \"x\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoRoot = root
	cfg.Local = true // use local filesystem; avoids hitting AWS meta bucket
	return cfg
}

func TestInfraApplyRequiresBackendHCL(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, false) // no backend.hcl
	rep := &captureReporter{}
	m := runner.NewMock()
	err := inf.Apply(context.Background(), cfg, m, rep)
	if err == nil {
		t.Error("expected error when backend.hcl missing")
	}
}

func TestInfraApplyDryRunLogsCommands(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)
	cfg.DryRun = true
	rep := &captureReporter{}
	m := runner.NewMock()
	if err := inf.Apply(context.Background(), cfg, m, rep); err != nil {
		t.Errorf("dry-run Apply: %v", err)
	}
	// Should have logged two "-> terraform" lines (init + apply).
	terraformLines := 0
	for _, l := range rep.lines {
		if strings.Contains(l, "terraform") {
			terraformLines++
		}
	}
	if terraformLines < 2 {
		t.Errorf("expected at least 2 terraform log lines, got %d: %v", terraformLines, rep.lines)
	}
}

func TestInfraApplyHappyPath(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true) // cfg.Local=true, with backend.hcl
	rep := &captureReporter{}

	m := runner.NewMock()
	m.Register("terraform -chdir=", []string{"init: ok"})
	if err := inf.Apply(context.Background(), cfg, m, rep); err != nil {
		t.Errorf("Apply: %v", err)
	}
	// Local mode: "Open workspace", "Initialize Terraform", "Apply infrastructure" = 3.
	if len(rep.starts) != 3 {
		t.Errorf("expected 3 substep starts, got %d: %v", len(rep.starts), rep.starts)
	}
}

func TestInfraApplyFailPropagates(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)
	rep := &captureReporter{}

	wantErr := errors.New("init failed")
	m := runner.NewMock()
	m.RegisterError("terraform -chdir=", []string{"Error!"}, wantErr)

	err := inf.Apply(context.Background(), cfg, m, rep)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(rep.fails) == 0 {
		t.Error("SubstepFail should have been called")
	}
}

func TestInfraDestroyRequiresBackendHCL(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, false) // no backend.hcl
	rep := &captureReporter{}
	err := inf.Destroy(context.Background(), cfg, runner.NewMock(), rep)
	if err == nil {
		t.Error("Destroy should fail without backend.hcl")
	}
}

func TestInfraDestroyDryRun(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)
	cfg.DryRun = true
	rep := &captureReporter{}
	if err := inf.Destroy(context.Background(), cfg, runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy dry-run: %v", err)
	}
	found := false
	for _, l := range rep.lines {
		if strings.Contains(l, "terraform") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected terraform destroy log: %v", rep.lines)
	}
}

// TestInfraCheckWithoutTFOutputs uses --local mode so Check reads the local
// stack dir. With no terraform state in the temp dir it should return an error.
func TestInfraCheckWithoutTFOutputs(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true) // cfg.Local=true — uses local path, avoids AWS
	err := inf.Check(context.Background(), cfg)
	if err == nil {
		t.Error("Check should fail without real terraform state (--local mode, empty temp dir)")
	}
}

// seqRunner is a test double that differentiates terraform subcommands by
// keyword. It counts "destroy" invocations and returns a configurable error on
// the first one, letting subsequent calls succeed. This is necessary because
// MockRunner uses first-match-wins prefix matching and cannot express "fail
// once then succeed" for commands that share a common prefix (terraform -chdir=).
type seqRunner struct {
	calls        [][]string
	destroyCount int
	firstDestrErr error // error to return on the first "destroy" call; nil = succeed
	reporter     runner.Reporter
}

func (r *seqRunner) Run(_ context.Context, cmd []string, _ []string, rep runner.Reporter) error {
	r.calls = append(r.calls, cmd)
	joined := strings.Join(cmd, " ")
	if strings.Contains(joined, " destroy ") {
		// Only count and apply errors to untargeted (full) destroy calls
		if !strings.Contains(joined, "-target=") {
			r.destroyCount++
			if r.destroyCount == 1 && r.firstDestrErr != nil {
				return r.firstDestrErr
			}
		}
		if rep != nil {
			rep.LogLine("Destroy complete!")
		}
	}
	return nil
}

func (r *seqRunner) Output(_ context.Context, _ []string, _ []string) ([]byte, error) {
	return nil, nil
}

func (r *seqRunner) destroyCalls() int {
	count := 0
	for _, c := range r.calls {
		if strings.Contains(strings.Join(c, " "), " destroy ") {
			count++
		}
	}
	return count
}

// TestInfraDestroyVPCRetry verifies that when terraform destroy exits with a
// VPC DependencyViolation error, the destroy is retried exactly once (after a
// post-fail sweep). No infinite loop, no extra retry on other errors.
func TestInfraDestroyVPCRetry(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)

	// The error string must contain both markers that isVPCDependencyViolation checks.
	vpcErr := errors.New("terraform destroy failed:\n│ Error: deleting EC2 VPC (vpc-0abc): DependencyViolation: vpc has dependencies")
	r := &seqRunner{firstDestrErr: vpcErr}

	rep := &captureReporter{}
	if err := inf.Destroy(context.Background(), cfg, r, rep); err != nil {
		t.Errorf("expected retry to succeed; got: %v", err)
	}

	if r.destroyCalls() != 3 {
		t.Errorf("expected exactly 3 terraform destroy calls (targeted EKS + failed full + retry full); got %d", r.destroyCalls())
	}

	retryLogged := false
	for _, l := range rep.lines {
		if strings.Contains(l, "retrying terraform destroy") {
			retryLogged = true
		}
	}
	if !retryLogged {
		t.Errorf("expected 'retrying terraform destroy' log line; got: %v", rep.lines)
	}
}

// TestInfraDestroyNoRetryOnOtherError verifies that non-VPC errors are NOT
// retried — a DependencyViolation on a subnet stops immediately.
func TestInfraDestroyNoRetryOnOtherError(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)

	// This error does NOT contain "EC2 VPC" — only a subnet DependencyViolation.
	subnetErr := errors.New("terraform destroy failed:\n│ Error: deleting EC2 Subnet (subnet-0abc): DependencyViolation")
	r := &seqRunner{firstDestrErr: subnetErr}

	rep := &captureReporter{}
	err := inf.Destroy(context.Background(), cfg, r, rep)
	if err == nil {
		t.Fatal("expected destroy to fail; got nil")
	}

	if r.destroyCalls() != 2 {
		t.Errorf("expected exactly 2 terraform destroy calls (targeted EKS + failed full); got %d", r.destroyCalls())
	}
}

func TestInfraVarsContainNodeInstanceTypes(t *testing.T) {
	inf := stages.Infra{}
	cfg := fakeInfraCfg(t, true)
	cfg.DryRun = true
	cfg.NodeType.Value = "m5.xlarge"
	rep := &captureReporter{}
	m := runner.NewMock()
	_ = inf.Apply(context.Background(), cfg, m, rep)
	// The -var=node_instance_types=["m5.xlarge"] JSON array must appear in log.
	found := false
	for _, l := range rep.lines {
		if strings.Contains(l, `node_instance_types=["m5.xlarge"]`) {
			found = true
		}
	}
	if !found {
		t.Errorf("node_instance_types JSON array not found in log: %v", rep.lines)
	}
}
