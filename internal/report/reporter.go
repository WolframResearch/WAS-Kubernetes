// Package report provides PlainReporter, the non-TUI presenter for wasctl.
//
// Both PlainReporter and FakeReporter implement the Conductor interface, which
// extends runner.Reporter with stage-level lifecycle signals used by the
// install orchestrator.
package report

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/WolframResearch/WAS-Kubernetes/internal/runner"
)

// Conductor extends runner.Reporter with stage-level lifecycle signals.
// The install orchestrator calls StageStart/StageDone/StageFail around each
// stage.Apply(); stages themselves only call the runner.Reporter methods.
type Conductor interface {
	runner.Reporter
	StageStart(name string)
	StageDone()
	StageFail(err error)
	InstallComplete(err error)
}

const (
	iconRunning = "⋯"
	iconDone    = "✓"
	iconFail    = "✗"

	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiCyan   = "\x1b[36m"
	ansiDim    = "\x1b[2m"
)

// PlainReporter writes stage and substep progress as plain text to w.
// Color codes are emitted when w is a real terminal.
type PlainReporter struct {
	w     io.Writer
	color bool
	cur   string // current substep name
}

// NewPlain returns a PlainReporter writing to w. Color is enabled when w is a
// real TTY.
func NewPlain(w io.Writer) *PlainReporter {
	color := false
	if f, ok := w.(*os.File); ok {
		color = term.IsTerminal(int(f.Fd()))
	}
	return &PlainReporter{w: w, color: color}
}

func (p *PlainReporter) color_(s, code string) string {
	if !p.color {
		return s
	}
	return code + s + ansiReset
}

// ── Stage-level signals (Conductor extra methods) ────────────────────────────

const stageSep = "────────────────────────────────────────────────────────────────────────────────"

// StageStart prints a stage header line.
func (p *PlainReporter) StageStart(name string) {
	fmt.Fprintf(p.w, "\n%s\n%s\n", p.color_(name, ansiCyan), p.color_(stageSep, ansiDim))
}

// StageDone prints a completion confirmation.
func (p *PlainReporter) StageDone() {
	fmt.Fprintf(p.w, "%s\n", p.color_(iconDone+" Stage complete.", ansiGreen))
}

// StageFail prints the stage error.
func (p *PlainReporter) StageFail(err error) {
	fmt.Fprintf(p.w, "%s %v\n", p.color_(iconFail+" Stage failed:", ansiRed), err)
}

// InstallComplete prints the success banner. On error, main() prints the error
// — printing here too would duplicate it.
func (p *PlainReporter) InstallComplete(err error) {
	if err == nil {
		fmt.Fprintf(p.w, "\n%s\n", p.color_(iconDone+" Installation complete.", ansiGreen))
	}
}

// ── Substep-level signals (runner.Reporter) ──────────────────────────────────

// SubstepStart signals the beginning of a substep.
func (p *PlainReporter) SubstepStart(name string) {
	p.cur = name
	fmt.Fprintf(p.w, "  %s %s\n", p.color_(iconRunning, ansiYellow), name)
}

// SubstepDone signals successful substep completion.
func (p *PlainReporter) SubstepDone() {
	fmt.Fprintf(p.w, "  %s %s\n", p.color_(iconDone, ansiGreen), p.cur)
}

// SubstepFail signals a substep error.
func (p *PlainReporter) SubstepFail(err error) {
	fmt.Fprintf(p.w, "  %s %s: %v\n", p.color_(iconFail, ansiRed), p.cur, err)
}

// LogLine emits a single line of subprocess output.
func (p *PlainReporter) LogLine(line string) {
	fmt.Fprintf(p.w, "    %s\n", line)
}

// ── FakeReporter — test double ───────────────────────────────────────────────

// FakeReporter is an in-memory implementation of Conductor used in tests.
type FakeReporter struct {
	StageStarts []string
	StageDones  int
	StageFails  []error
	Results     []error
	Starts      []string
	Dones       int
	Fails       []error
	Lines       []string
}

func (f *FakeReporter) StageStart(name string) { f.StageStarts = append(f.StageStarts, name) }
func (f *FakeReporter) StageDone()             { f.StageDones++ }
func (f *FakeReporter) StageFail(err error)    { f.StageFails = append(f.StageFails, err) }
func (f *FakeReporter) InstallComplete(err error) {
	f.Results = append(f.Results, err)
}
func (f *FakeReporter) SubstepStart(name string) { f.Starts = append(f.Starts, name) }
func (f *FakeReporter) SubstepDone()             { f.Dones++ }
func (f *FakeReporter) SubstepFail(err error)    { f.Fails = append(f.Fails, err) }
func (f *FakeReporter) LogLine(line string)       { f.Lines = append(f.Lines, line) }
