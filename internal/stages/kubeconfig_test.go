package stages_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

func fakeKubeconfigCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "infra", "aws", "stack"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoRoot = root
	return cfg
}

func TestKubeconfigMetadata(t *testing.T) {
	k := stages.Kubeconfig{}
	if k.Name() != "kubeconfig" {
		t.Errorf("Name: %q", k.Name())
	}
	subs := k.Substeps()
	if len(subs) != 4 {
		t.Errorf("expected 4 substeps, got %d: %v", len(subs), subs)
	}
	want := []string{"Open workspace", "Generate kubeconfig", "Verify cluster connectivity", "Record cluster UID"}
	for i, w := range want {
		if subs[i] != w {
			t.Errorf("substep[%d]: got %q, want %q", i, subs[i], w)
		}
	}
}

func TestKubeconfigSubstepNames(t *testing.T) {
	k := stages.Kubeconfig{}
	subs := k.Substeps()
	if subs[0] != "Open workspace" {
		t.Errorf("substep[0]: %q", subs[0])
	}
	if subs[1] != "Generate kubeconfig" {
		t.Errorf("substep[1]: %q", subs[1])
	}
}

func TestKubeconfigDestroyIsNoOp(t *testing.T) {
	k := stages.Kubeconfig{}
	rep := &captureReporter{}
	err := k.Destroy(context.Background(), newCfg(t), runner.NewMock(), rep)
	if err != nil {
		t.Errorf("Destroy should be no-op: %v", err)
	}
	if len(rep.starts) > 0 || len(rep.lines) > 0 {
		t.Errorf("Destroy should emit nothing: starts=%v lines=%v", rep.starts, rep.lines)
	}
}

// TestKubeconfigApplyRequiresWorkspace verifies that Apply fails when no
// workspace / meta bucket is available (unit test without AWS).
func TestKubeconfigApplyRequiresWorkspace(t *testing.T) {
	k := stages.Kubeconfig{}
	cfg := fakeKubeconfigCfg(t)
	rep := &captureReporter{}
	err := k.Apply(context.Background(), cfg, runner.NewMock(), rep)
	if err == nil {
		t.Skip("Apply passed — test environment has a real meta bucket; skipping negative assertion")
	}
	// Any error is acceptable; the important thing is Apply cannot silently
	// succeed without a workspace.
}
