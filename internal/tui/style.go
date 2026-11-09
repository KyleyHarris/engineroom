package tui

import "github.com/charmbracelet/lipgloss"

// The palette is small on purpose: the screen is read at a glance, so colour marks only state
// (waiting, failed, ended, local) and everything else is plain text.
var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	headingStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
	keyStyle     = lipgloss.NewStyle().Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	localBadge   = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
	commandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)
