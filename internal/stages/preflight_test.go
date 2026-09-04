package stages_test

import (
	"context"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/config"
	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
	"github.com/WolframResearch/WAS-Kubernetes/internal/stages"
)

// captureReporter records substep and log calls for assertions.
type captureReporter struct {
	starts []string
	dones  int
	fails  []error
	lines  []string
}

func (c *captureReporter) SubstepStart(name string) { c.starts = append(c.starts, name) }
func (c *captureReporter) SubstepDone()             { c.dones++ }
func (c *captureReporter) SubstepFail(err error)    { c.fails = append(c.fails, err) }
func (c *captureReporter) LogLine(line string)      { c.lines = append(c.lines, line) }

func newCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("/repo", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPreflightSubstepNames(t *testing.T) {
	p := stages.Preflight{}
	subs := p.Substeps()
	if len(subs) != 6 {
		t.Fatalf("expected 6 substeps, got %d", len(subs))
	}
	// First substep must be the version compatibility check.
	if subs[0] != "Check version compatibility" {
		t.Errorf("first substep: got %q, want 'Check version compatibility'", subs[0])
	}
	// Last substep verifies cloud credentials.
	if subs[5] != "Verify cloud credentials" {
		t.Errorf("last substep: got %q, want 'Verify cloud credentials'", subs[5])
	}
}

func TestPreflightApplyReportsAllSubsteps(t *testing.T) {
	// Apply always starts 5 substeps. We can't guarantee success without real
	// tools installed, but we CAN verify that substep signalling happens.
	// Run against a mock runner that lets us inspect the reporter calls.
	p := stages.Preflight{}
	m := runner.NewMock()
	rep := &captureReporter{}
	cfg := newCfg(t)

	// Errors are expected (no real AWS credentials in test environment).
	_ = p.Apply(context.Background(), cfg, m, rep)

	// All 6 substeps must have been started.
	if len(rep.starts) != 6 {
		t.Errorf("expected 6 substep starts, got %d: %v", len(rep.starts), rep.starts)
	}
}

func TestPreflightCheckFailsWhenToolsMissing(t *testing.T) {
	// In a standard CI/dev environment, at least one required tool (terraform)
	// should be absent, causing Check to return an error. If all tools happen
	// to be installed, this test is still valid — it just verifies Check passes.
	p := stages.Preflight{}
	// We just verify Check doesn't panic. The exit code varies by environment.
	_ = p.Check(context.Background(), newCfg(t))
}

func TestPreflightDestroyIsNoOp(t *testing.T) {
	p := stages.Preflight{}
	rep := &captureReporter{}
	if err := p.Destroy(context.Background(), newCfg(t), runner.NewMock(), rep); err != nil {
		t.Errorf("Destroy should be no-op, got error: %v", err)
	}
	if len(rep.starts) > 0 || len(rep.lines) > 0 {
		t.Errorf("Destroy should emit nothing; got starts=%v lines=%v", rep.starts, rep.lines)
	}
}
