package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// ── fakeReporter ─────────────────────────────────────────────────────────────

type fakeReporter struct{ lines []string }

func (f *fakeReporter) SubstepStart(string) {}
func (f *fakeReporter) SubstepDone()        {}
func (f *fakeReporter) SubstepFail(error)   {}
func (f *fakeReporter) LogLine(line string) { f.lines = append(f.lines, line) }

// ── ExecRunner ───────────────────────────────────────────────────────────────

func TestExecRunnerStreamsLines(t *testing.T) {
	r := runner.ExecRunner{}
	fr := &fakeReporter{}
	err := r.Run(context.Background(),
		[]string{"bash", "-c", "echo hello; echo world"},
		nil, fr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fr.lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(fr.lines), fr.lines)
	}
	if fr.lines[0] != "hello" || fr.lines[1] != "world" {
		t.Errorf("unexpected lines: %v", fr.lines)
	}
}

func TestExecRunnerCombinesStderr(t *testing.T) {
	r := runner.ExecRunner{}
	fr := &fakeReporter{}
	err := r.Run(context.Background(),
		[]string{"bash", "-c", "echo stdout; echo stderr >&2"},
		nil, fr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(fr.lines, " ")
	if !strings.Contains(joined, "stdout") || !strings.Contains(joined, "stderr") {
		t.Errorf("expected both stdout and stderr, got: %v", fr.lines)
	}
}

func TestExecRunnerExitCode(t *testing.T) {
	r := runner.ExecRunner{}
	fr := &fakeReporter{}
	err := r.Run(context.Background(),
		[]string{"bash", "-c", "exit 42"},
		nil, fr)
	if err == nil {
		t.Fatal("expected error for exit 42, got nil")
	}
}

func TestExecRunnerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	r := runner.ExecRunner{}
	fr := &fakeReporter{}
	err := r.Run(ctx, []string{"sleep", "10"}, nil, fr)
	if err == nil {
		t.Fatal("expected error when context cancelled, got nil")
	}
}

func TestExecRunnerEmptyCommand(t *testing.T) {
	r := runner.ExecRunner{}
	fr := &fakeReporter{}
	err := r.Run(context.Background(), nil, nil, fr)
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// ── MockRunner ───────────────────────────────────────────────────────────────

func TestMockRunnerMatch(t *testing.T) {
	m := runner.NewMock()
	m.Register("terraform init", []string{"Initializing..."})

	fr := &fakeReporter{}
	err := m.Run(context.Background(), []string{"terraform", "init"}, nil, fr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fr.lines) != 1 || fr.lines[0] != "Initializing..." {
		t.Errorf("unexpected lines: %v", fr.lines)
	}
	if !m.CalledWith("terraform init") {
		t.Error("CalledWith should return true")
	}
}

func TestMockRunnerError(t *testing.T) {
	m := runner.NewMock()
	want := errors.New("apply failed")
	m.RegisterError("terraform apply", []string{"Error!"}, want)

	fr := &fakeReporter{}
	err := m.Run(context.Background(), []string{"terraform", "apply", "-auto-approve"}, nil, fr)
	if !errors.Is(err, want) {
		t.Errorf("expected wrapped %v, got %v", want, err)
	}
	if len(fr.lines) != 1 || fr.lines[0] != "Error!" {
		t.Errorf("unexpected lines: %v", fr.lines)
	}
}

func TestMockRunnerUnexpectedCommand(t *testing.T) {
	m := runner.NewMock()
	fr := &fakeReporter{}
	err := m.Run(context.Background(), []string{"unknown", "cmd"}, nil, fr)
	if err == nil {
		t.Error("expected error for unregistered command")
	}
}

func TestMockRunnerFirstMatchWins(t *testing.T) {
	m := runner.NewMock()
	m.Register("terraform", []string{"first"})
	m.Register("terraform init", []string{"second"})

	fr := &fakeReporter{}
	_ = m.Run(context.Background(), []string{"terraform", "init"}, nil, fr)
	if len(fr.lines) == 0 || fr.lines[0] != "first" {
		t.Errorf("first-match-wins violated: %v", fr.lines)
	}
}

func TestMockRunnerRecordsCalls(t *testing.T) {
	m := runner.NewMock()
	m.Register("helm", nil)
	m.Register("kubectl", nil)

	fr := &fakeReporter{}
	_ = m.Run(context.Background(), []string{"helm", "status", "was"}, nil, fr)
	_ = m.Run(context.Background(), []string{"kubectl", "cluster-info"}, nil, fr)

	if len(m.Calls) != 2 {
		t.Fatalf("expected 2 recorded calls, got %d", len(m.Calls))
	}
	if m.Calls[0][0] != "helm" || m.Calls[1][0] != "kubectl" {
		t.Errorf("unexpected calls: %v", m.Calls)
	}
}
