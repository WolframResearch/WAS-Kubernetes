package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "regenerate golden files")

var testStageNames = []string{
	"Preflight",
	"Bootstrap",
	"Backend",
	"Infrastructure",
	"Kubeconfig",
	"Add-ons",
	"App deployment",
}

// fixedModel returns a Model with deterministic state for golden tests
// (no spinner tick, elapsed frozen at zero).
func fixedModel(width, height int) Model {
	m := NewModel(testStageNames, nil)
	m.width = width
	m.height = height
	m.elapsed = 0
	m.startTime = time.Time{} // zero; formatElapsed(0) → "0:00"
	return m
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Logf("golden %q missing — run with -update to create", path)
		return // not a failure on first run
	}
	if err != nil {
		t.Fatal(err)
	}
	want := string(b)
	if got != want {
		t.Errorf("View() mismatch for %q\ngot:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestModelView_Idle(t *testing.T) {
	m := fixedModel(80, 24)
	v := m.View()

	// Width == 0 check: ensure we get actual output at 80 cols.
	if v == "" {
		t.Fatal("View() returned empty string for width=80")
	}

	// All stages should appear as pending.
	for _, name := range testStageNames {
		if !strings.Contains(v, name) {
			t.Errorf("stage %q not in idle View()", name)
		}
	}

	// Pending icon "○" should appear for all stages.
	count := strings.Count(v, "○")
	if count != len(testStageNames) {
		t.Errorf("expected %d pending icons, got %d\n%s", len(testStageNames), count, v)
	}

	checkGolden(t, "idle", v)
}

func TestModelView_StageRunning(t *testing.T) {
	m := fixedModel(80, 24)
	// Simulate two stages done, one running with a substep.
	m.stages[0].status = stageDone
	m.stages[1].status = stageDone
	m.stages[2].status = stageRunning
	m.stages[2].substep = "Terraform apply"
	m.curStage = 2
	m.logs = []string{
		"-> terraform apply -auto-approve",
		"Plan: 3 to add, 0 to change, 0 to destroy.",
	}

	v := m.View()

	if !strings.Contains(v, "Terraform apply") {
		t.Errorf("running substep not in View()\n%s", v)
	}
	if strings.Count(v, "✓") < 2 {
		t.Errorf("expected 2 done icons\n%s", v)
	}
	if !strings.Contains(v, "terraform apply") {
		t.Errorf("log line not in View()\n%s", v)
	}

	checkGolden(t, "running", v)
}

func TestModelView_StageFailed(t *testing.T) {
	m := fixedModel(80, 24)
	m.stages[0].status = stageDone
	m.stages[1].status = stageFailed
	m.stages[1].err = errorf("state bucket already exists")
	m.curStage = 1

	v := m.View()

	if !strings.Contains(v, "✗") {
		t.Errorf("fail icon not in View()\n%s", v)
	}
	if !strings.Contains(v, "state bucket already exists") {
		t.Errorf("error message not in View()\n%s", v)
	}

	checkGolden(t, "failed", v)
}

func TestModelView_Resized(t *testing.T) {
	m := fixedModel(100, 30)
	m.stages[0].status = stageDone
	v := m.View()
	if v == "" {
		t.Fatal("View() empty at 100 cols")
	}
	// All stages still render correctly at wider terminal.
	for _, name := range testStageNames {
		if !strings.Contains(v, name) {
			t.Errorf("stage %q missing at 100 cols\n%s", name, v)
		}
	}
	checkGolden(t, "resized", v)
}

func TestModelView_ZeroWidth(t *testing.T) {
	m := NewModel(testStageNames, nil)
	// width=0 means the window size message hasn't arrived yet.
	if m.View() != "" {
		t.Error("View() should be empty when width=0 (not yet sized)")
	}
}

func TestModelUpdate_StageLifecycle(t *testing.T) {
	m := NewModel(testStageNames, nil)
	m.width = 80
	m.height = 24

	// Start a stage.
	next, _ := m.Update(StageStartMsg{Name: "Preflight"})
	m = next.(Model)
	if m.curStage != 0 {
		t.Errorf("curStage should be 0, got %d", m.curStage)
	}
	if m.stages[0].status != stageRunning {
		t.Errorf("stage[0] should be running, got %v", m.stages[0].status)
	}

	// Send a substep.
	next, _ = m.Update(SubstepStartMsg{Name: "Check tools"})
	m = next.(Model)
	if m.stages[0].substep != "Check tools" {
		t.Errorf("substep not set: %q", m.stages[0].substep)
	}

	// Complete the stage.
	next, _ = m.Update(StageDoneMsg{})
	m = next.(Model)
	if m.stages[0].status != stageDone {
		t.Errorf("stage[0] should be done, got %v", m.stages[0].status)
	}
}

func TestModelUpdate_LogTail(t *testing.T) {
	m := NewModel(testStageNames, nil)
	for i := 0; i < maxLogs+10; i++ {
		next, _ := m.Update(LogLineMsg{Line: "line"})
		m = next.(Model)
	}
	if len(m.logs) != maxLogs {
		t.Errorf("log tail should be capped at %d, got %d", maxLogs, len(m.logs))
	}
}

func TestModelUpdate_InstallDone(t *testing.T) {
	m := NewModel(testStageNames, nil)
	m.width = 80
	m.height = 24

	_, cmd := m.Update(InstallDoneMsg{Err: nil})
	if cmd == nil {
		t.Error("InstallDoneMsg should return a quit-after-delay cmd")
	}
	next, _ := m.Update(InstallDoneMsg{Err: nil})
	m2 := next.(Model)
	if !m2.done {
		t.Error("Model.done should be true after InstallDoneMsg")
	}
}

// errorf returns an error with the given message (avoids importing errors in test).
func errorf(s string) error {
	return errorString(s)
}

type errorString string

func (e errorString) Error() string { return string(e) }
