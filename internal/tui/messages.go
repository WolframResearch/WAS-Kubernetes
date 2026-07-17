package tui

import "time"

// Message types sent from TUIReporter to the Bubble Tea program.

// StageStartMsg signals that a new stage has begun.
type StageStartMsg struct{ Name string }

// StageDoneMsg signals that the current stage completed successfully.
type StageDoneMsg struct{}

// StageFailMsg signals that the current stage failed.
type StageFailMsg struct{ Err error }

// SubstepStartMsg signals the beginning of a substep within the current stage.
type SubstepStartMsg struct{ Name string }

// SubstepDoneMsg signals a successful substep completion.
type SubstepDoneMsg struct{}

// SubstepFailMsg signals a substep failure.
type SubstepFailMsg struct{ Err error }

// LogLineMsg carries one line of subprocess output.
type LogLineMsg struct{ Line string }

// InstallDoneMsg is the final message; Err is nil on success.
type InstallDoneMsg struct{ Err error }

// tickMsg is sent every second to refresh the elapsed-time counter.
type tickMsg time.Time
