package report_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/WolframResearch/WAS-Kubernetes/internal/report"
)

func newPlain(b *strings.Builder) *report.PlainReporter {
	return report.NewPlain(b)
}

type writerAdapter struct{ *strings.Builder }

func (w *writerAdapter) Write(p []byte) (int, error) {
	return w.Builder.Write(p)
}

func TestPlainStageStart(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.StageStart("Infrastructure")
	out := b.String()
	if !strings.Contains(out, "Infrastructure") {
		t.Errorf("StageStart output missing stage name: %q", out)
	}
}

func TestPlainStageDone(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.StageDone()
	if !strings.Contains(b.String(), "complete") {
		t.Errorf("StageDone should mention 'complete': %q", b.String())
	}
}

func TestPlainStageFail(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.StageFail(errors.New("boom"))
	out := b.String()
	if !strings.Contains(out, "boom") {
		t.Errorf("StageFail should include error: %q", out)
	}
}

func TestPlainSubstepCycle(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.SubstepStart("Run preflight")
	p.SubstepDone()
	out := b.String()
	if !strings.Contains(out, "Run preflight") {
		t.Errorf("substep name not in output: %q", out)
	}
	// Done should re-print substep name.
	count := strings.Count(out, "Run preflight")
	if count < 2 {
		t.Errorf("expected substep name twice (start+done), got %d: %q", count, out)
	}
}

func TestPlainLogLine(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.LogLine("hello world")
	if !strings.Contains(b.String(), "hello world") {
		t.Errorf("LogLine not in output: %q", b.String())
	}
}

func TestPlainInstallComplete(t *testing.T) {
	var b strings.Builder
	p := report.NewPlain(&b)
	p.InstallComplete(nil)
	if !strings.Contains(b.String(), "complete") {
		t.Errorf("InstallComplete(nil) should say 'complete': %q", b.String())
	}

	b.Reset()
	p.InstallComplete(errors.New("network timeout"))
	if b.Len() != 0 {
		t.Errorf("InstallComplete(err) should produce no output (error printed by main): %q", b.String())
	}
}

func TestFakeReporterRecordsAll(t *testing.T) {
	f := &report.FakeReporter{}
	f.StageStart("infra")
	f.StageDone()
	f.StageFail(errors.New("e1"))
	f.InstallComplete(nil)
	f.SubstepStart("sub1")
	f.SubstepDone()
	f.SubstepFail(errors.New("e2"))
	f.LogLine("line1")
	f.LogLine("line2")

	if len(f.StageStarts) != 1 || f.StageStarts[0] != "infra" {
		t.Errorf("StageStarts: %v", f.StageStarts)
	}
	if f.StageDones != 1 {
		t.Errorf("StageDones: %d", f.StageDones)
	}
	if len(f.StageFails) != 1 {
		t.Errorf("StageFails: %v", f.StageFails)
	}
	if len(f.Results) != 1 {
		t.Errorf("Results: %v", f.Results)
	}
	if len(f.Starts) != 1 || f.Starts[0] != "sub1" {
		t.Errorf("Starts: %v", f.Starts)
	}
	if f.Dones != 1 {
		t.Errorf("Dones: %d", f.Dones)
	}
	if len(f.Fails) != 1 {
		t.Errorf("Fails: %v", f.Fails)
	}
	if len(f.Lines) != 2 {
		t.Errorf("Lines: %v", f.Lines)
	}
}
