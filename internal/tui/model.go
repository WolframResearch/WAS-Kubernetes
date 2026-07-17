// Package tui implements the Bubble Tea live-refresh terminal UI. It is imported
// only when TUI capability is detected at startup; plain-mode callers never
// reach this package.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/WolframResearch/WAS-Kubernetes/internal/version"
)

// ── TUIReporter ──────────────────────────────────────────────────────────────

// TUIReporter implements report.Conductor by forwarding lifecycle signals as
// Bubble Tea messages. Goroutines in the install orchestrator call these
// methods concurrently while the program loop runs on the main goroutine.
type TUIReporter struct {
	prog *tea.Program
}

// NewTUIReporter creates a TUIReporter that sends to prog.
func NewTUIReporter(prog *tea.Program) *TUIReporter {
	return &TUIReporter{prog: prog}
}

func (t *TUIReporter) StageStart(name string)    { t.prog.Send(StageStartMsg{Name: name}) }
func (t *TUIReporter) StageDone()                { t.prog.Send(StageDoneMsg{}) }
func (t *TUIReporter) StageFail(err error)       { t.prog.Send(StageFailMsg{Err: err}) }
func (t *TUIReporter) InstallComplete(err error)  { t.prog.Send(InstallDoneMsg{Err: err}) }
func (t *TUIReporter) SubstepStart(name string)  { t.prog.Send(SubstepStartMsg{Name: name}) }
func (t *TUIReporter) SubstepDone()              { t.prog.Send(SubstepDoneMsg{}) }
func (t *TUIReporter) SubstepFail(err error)     { t.prog.Send(SubstepFailMsg{Err: err}) }
func (t *TUIReporter) LogLine(line string)        { t.prog.Send(LogLineMsg{Line: line}) }

// ── Model ────────────────────────────────────────────────────────────────────

type stageStatus int

const (
	stagePending stageStatus = iota
	stageRunning
	stageDone
	stageFailed
)

type stageEntry struct {
	name    string
	status  stageStatus
	substep string
	err     error
}

const maxLogs = 200

// Model is the Bubble Tea model for the wasctl live TUI.
type Model struct {
	stages     []stageEntry
	curStage   int // index into stages; -1 = not started
	logs       []string
	spinner    spinner.Model
	startTime  time.Time
	elapsed    time.Duration
	width      int
	height     int
	done       bool
	installErr error
	onReady    func() // fired once from Init when the event loop is live
}

// NewModel creates a Model pre-populated with the given stage names in pending
// state. onReady, if non-nil, is called once from Init after the program loop
// starts — use it to start the install goroutine so Send() is not racing Run().
func NewModel(stageNames []string, onReady func()) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot // braille-style
	sp.Style = activeStyle

	entries := make([]stageEntry, len(stageNames))
	for i, n := range stageNames {
		entries[i] = stageEntry{name: n, status: stagePending}
	}
	return Model{
		stages:    entries,
		curStage:  -1,
		spinner:   sp,
		startTime: time.Now(),
		onReady:   onReady,
	}
}

// ── tea.Model interface ───────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		tea.Every(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
	}
	if m.onReady != nil {
		onReady := m.onReady
		cmds = append(cmds, func() tea.Msg {
			onReady()
			return nil
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		m.elapsed = time.Since(m.startTime)
		cmds = append(cmds, tea.Every(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }))

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case StageStartMsg:
		m.curStage++
		if m.curStage < len(m.stages) {
			m.stages[m.curStage].status = stageRunning
			m.stages[m.curStage].substep = ""
		}

	case StageDoneMsg:
		if m.curStage >= 0 && m.curStage < len(m.stages) {
			m.stages[m.curStage].status = stageDone
			m.stages[m.curStage].substep = ""
		}

	case StageFailMsg:
		if m.curStage >= 0 && m.curStage < len(m.stages) {
			m.stages[m.curStage].status = stageFailed
			m.stages[m.curStage].err = msg.Err
		}

	case SubstepStartMsg:
		if m.curStage >= 0 && m.curStage < len(m.stages) {
			m.stages[m.curStage].substep = msg.Name
		}

	case SubstepDoneMsg, SubstepFailMsg:
		// substep done/fail are reflected at stage level via StageDoneMsg/StageFailMsg

	case LogLineMsg:
		m.logs = append(m.logs, msg.Line)
		if len(m.logs) > maxLogs {
			m.logs = m.logs[len(m.logs)-maxLogs:]
		}

	case InstallDoneMsg:
		m.done = true
		m.installErr = msg.Err
		// Pause so the user can read the final status before alt-screen clears.
		delay := 2 * time.Second
		if msg.Err != nil {
			delay = 3 * time.Second
		}
		return m, tea.Tick(delay, func(time.Time) tea.Msg { return tea.QuitMsg{} })

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder

	// ── Header ────────────────────────────────────────────────────────────────
	title := titleStyle.Render("wasctl") + "  " + dimStyle.Render("v"+version.Version)
	elapsed := dimStyle.Render(formatElapsed(m.elapsed))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(elapsed)
	if gap < 1 {
		gap = 1
	}
	headerContent := title + strings.Repeat(" ", gap) + elapsed
	sb.WriteString(headerStyle.Width(m.width).Render(headerContent))
	sb.WriteByte('\n')

	// ── Stage panel ───────────────────────────────────────────────────────────
	for _, s := range m.stages {
		icon := m.stageIcon(s)
		line := "  " + icon + " " + s.name
		if s.status == stageRunning && s.substep != "" {
			subStr := substepStyle.Render("  " + truncate(s.substep, m.width-lipgloss.Width(line)-4))
			line += subStr
		}
		if s.status == stageFailed && s.err != nil {
			errStr := failStyle.Render("  " + truncate(s.err.Error(), m.width-lipgloss.Width(line)-4))
			line += errStr
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	// ── Log separator ─────────────────────────────────────────────────────────
	sb.WriteString(sepStyle.Render(strings.Repeat("─", m.width)))
	sb.WriteByte('\n')

	// ── Log tail ──────────────────────────────────────────────────────────────
	// Available height: total − header(2) − stages − separator(1) − footer(2)
	logHeight := m.height - 2 - len(m.stages) - 1 - 2
	if logHeight < 2 {
		logHeight = 2
	}
	start := 0
	if len(m.logs) > logHeight {
		start = len(m.logs) - logHeight
	}
	for _, l := range m.logs[start:] {
		line := "  " + truncate(l, m.width-3)
		sb.WriteString(logStyle.Render(line))
		sb.WriteByte('\n')
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	footerText := "q quit  ctrl+c abort"
	if m.done {
		if m.installErr != nil {
			footerText = fmt.Sprintf("✗ Installation failed: %v  (press q)", m.installErr)
			footerText = failStyle.Render(footerText)
		} else {
			footerText = "✓ Installation complete  (press q)"
			footerText = doneStyle.Render(footerText)
		}
	}
	sb.WriteString(footerStyle.Width(m.width).Render("  " + footerText))

	return sb.String()
}

func (m Model) stageIcon(s stageEntry) string {
	switch s.status {
	case stageDone:
		return doneStyle.Render("✓")
	case stageFailed:
		return failStyle.Render("✗")
	case stageRunning:
		return m.spinner.View()
	default:
		return pendingStyle.Render("○")
	}
}
