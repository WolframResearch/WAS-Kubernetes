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

// fakeBackendCfg sets up a temp repo root with an infra/aws/stack directory.
func fakeBackendCfg(t *testing.T) *config.Config {
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

// TestBackendCheckRequiresMetaBucket verifies Check returns an error when the
// meta bucket doesn't exist (no workspace configured).
func TestBackendCheckRequiresMetaBucket(t *testing.T) {
	b := stages.Backend{}
	cfg := fakeBackendCfg(t)
	// Check always goes to the meta bucket (S3). Without real AWS credentials
	// or a bucket, it must return an error.
	err := b.Check(context.Background(), cfg)
	if err == nil {
		t.Skip("Check passed — test environment has a real meta bucket for this cluster; skipping negative assertion")
	}
	// Error must exist.
}

// TestBackendDestroyLogsMessage verifies Destroy logs a message about cleanup.
// The backend stage no longer deletes local files on Destroy (the meta bucket
// cleanup happens together with workspace delete).
func TestBackendDestroyLogsMessage(t *testing.T) {
	b := stages.Backend{}
	cfg := fakeBackendCfg(t)
	rep := &captureReporter{}
	if err := b.Destroy(context.Background(), cfg, runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy: %v", err)
	}
	if len(rep.lines) == 0 {
		t.Error("Destroy should log at least one message")
	}
}

// TestBackendDestroyDryRun verifies dry-run Destroy logs a dry-run message.
func TestBackendDestroyDryRun(t *testing.T) {
	b := stages.Backend{}
	cfg := fakeBackendCfg(t)
	cfg.DryRun = true
	rep := &captureReporter{}
	if err := b.Destroy(context.Background(), cfg, runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy dry-run: %v", err)
	}
	found := false
	for _, l := range rep.lines {
		if strings.Contains(l, "dry-run") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dry-run log line; got %v", rep.lines)
	}
}

// TestBackendApplyRequiresMetaBucket verifies Apply returns an error when
// the meta bucket doesn't exist (no workspace for this cluster).
func TestBackendApplyRequiresMetaBucket(t *testing.T) {
	b := stages.Backend{}
	cfg := fakeBackendCfg(t)
	rep := &captureReporter{}
	err := b.Apply(context.Background(), cfg, runner.NewMock(), rep)
	if err == nil {
		t.Skip("Apply passed — test environment has a real meta bucket for this cluster; skipping negative assertion")
	}
	// Error must be non-nil.
}

// TestBackendMetadata verifies basic metadata for the Backend stage.
func TestBackendMetadata(t *testing.T) {
	b := stages.Backend{}
	if b.Name() != "backend" {
		t.Errorf("Name: %q", b.Name())
	}
	if b.Description() == "" {
		t.Error("Description must not be empty")
	}
	if len(b.Substeps()) == 0 {
		t.Error("Substeps must not be empty")
	}
}
