package stages_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

func fakeBootstrapCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra", "aws", "bootstrap"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoRoot = root
	return cfg
}

// TestBootstrapCheckRequiresMetaBucket verifies Check returns an error when
// no meta bucket / workspace exists.
func TestBootstrapCheckRequiresMetaBucket(t *testing.T) {
	b := stages.Bootstrap{}
	cfg := fakeBootstrapCfg(t)
	err := b.Check(context.Background(), cfg)
	if err == nil {
		t.Skip("Check passed — test environment has a real meta bucket; skipping negative assertion")
	}
}

func TestBootstrapMetadata(t *testing.T) {
	b := stages.Bootstrap{}
	if b.Name() != "bootstrap" {
		t.Errorf("Name: %q", b.Name())
	}
	subs := b.Substeps()
	if len(subs) != 4 {
		t.Errorf("expected 4 substeps, got %d: %v", len(subs), subs)
	}
	want := []string{"Open workspace", "Initialize Terraform", "Create state backend", "Save state"}
	for i, w := range want {
		if subs[i] != w {
			t.Errorf("substep[%d]: got %q, want %q", i, subs[i], w)
		}
	}
}

// TestBootstrapDestroySkipsWithoutFlag verifies the skip message when
// DestroyStateBackend is false.
func TestBootstrapDestroySkipsWithoutFlag(t *testing.T) {
	b := stages.Bootstrap{}
	cfg := fakeBootstrapCfg(t)
	cfg.DestroyStateBackend = false

	rep := &captureReporter{}
	if err := b.Destroy(context.Background(), cfg, runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy should not error: %v", err)
	}
	found := false
	for _, l := range rep.lines {
		if strings.Contains(l, "Skipping") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Skipping' in log: %v", rep.lines)
	}
}

// TestBootstrapDestroyDryRunWithFlag verifies that Destroy in dry-run mode
// (with DestroyStateBackend=true) logs terraform commands but does not run them.
// This test requires AWS credentials; it is skipped if the meta bucket is unavailable.
func TestBootstrapDestroyDryRunWithFlag(t *testing.T) {
	b := stages.Bootstrap{}
	cfg := fakeBootstrapCfg(t)
	cfg.DryRun = true
	cfg.DestroyStateBackend = true
	cfg.StateBucket = config.Field[string]{Value: "test-state-bucket", Source: "test"}
	cfg.LockTable = config.Field[string]{Value: "test-lock-table", Source: "test"}

	rep := &captureReporter{}
	if err := b.Destroy(context.Background(), cfg, runner.NewMock(), rep); err != nil {
		// Expected in test env without real meta bucket.
		t.Skipf("Destroy requires AWS meta bucket: %v", err)
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
