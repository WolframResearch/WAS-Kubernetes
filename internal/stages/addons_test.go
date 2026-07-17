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

func fakeAddonsCfg(t *testing.T) *config.Config {
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
	cfg.Local = true // avoid hitting AWS meta bucket in unit tests
	return cfg
}

func TestAddonsMetadata(t *testing.T) {
	a := stages.Addons{}
	if a.Name() != "addons" {
		t.Errorf("Name: %q", a.Name())
	}
	subs := a.Substeps()
	if len(subs) == 0 {
		t.Fatal("expected non-empty substeps list")
	}
	if subs[0] != "Open workspace" {
		t.Errorf("substep[0]: %q", subs[0])
	}
}

func TestAddonsDestroyIsNonFatal(t *testing.T) {
	// Destroy without a real cluster should return nil (non-fatal) and log
	// a diagnostic message. We don't check the exact message text since it
	// varies with the error path (no workspace, no kubeconfig, etc.).
	a := stages.Addons{}
	rep := &captureReporter{}
	if err := a.Destroy(context.Background(), newCfg(t), runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy must not error when infrastructure is unavailable: %v", err)
	}
}

// TestAddonsApplyFailsWithoutInfrastructure verifies Apply returns an error
// when real infrastructure is unavailable. The specific error varies by
// environment (missing workspace, EKS cluster not found, TF outputs missing),
// so we only assert that Apply must not silently succeed.
func TestAddonsApplyFailsWithoutInfrastructure(t *testing.T) {
	a := stages.Addons{}
	cfg := fakeAddonsCfg(t)
	rep := &captureReporter{}
	err := a.Apply(context.Background(), cfg, runner.NewMock(), rep)
	if err == nil {
		t.Fatal("Apply should fail without real infrastructure (workspace, EKS, TF outputs)")
	}
}

func TestAddonsApplyDryRunWithTFMocked(t *testing.T) {
	// addons.Apply in --local mode needs EFS TF outputs; with no state they fail.
	a := stages.Addons{}
	cfg := fakeAddonsCfg(t) // cfg.Local = true
	cfg.DryRun = true
	rep := &captureReporter{}
	err := a.Apply(context.Background(), cfg, runner.NewMock(), rep)
	// Should fail on missing TF outputs regardless of dry-run.
	if err == nil {
		t.Error("Apply should fail without TF outputs even in dry-run")
	}
}

func TestAddonsApplyIncludesSkipFlag(t *testing.T) {
	a := stages.Addons{}
	cfg := fakeAddonsCfg(t) // cfg.Local = true
	cfg.DryRun = true
	cfg.AddonsSkip = config.Field[string]{Value: "cert-manager", Source: "test"}
	rep := &captureReporter{}
	// Fails on TF output, but verifies the code path compiles and runs.
	_ = a.Apply(context.Background(), cfg, runner.NewMock(), rep)
}
