package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	headerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("8"))
	footerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(lipgloss.Color("8")).
			Foreground(lipgloss.Color("8"))
	sepStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	activeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	doneStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	failStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	logStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	substepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
