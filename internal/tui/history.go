package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/history"
)

// The history screen lists every action this machine ran, newest first, with the selected
// action's commands and their outcomes beneath, so the developer can see what was done and do
// it again by hand (LEARN-HIST-001, LEARN-HIST-002).

func (m Model) openHistory() (tea.Model, tea.Cmd) {
	m.screen = screenHistory
	m.cursor = 0
	m.copyNote, m.cmdCursor = "", 0
	return m, func() tea.Msg {
		e, bad, err := history.Read()
		return historyMsg{entries: e, bad: bad, err: err}
	}
}

func (m Model) historyKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		m.screen = screenList
		m.cursor = 0
		return m, m.loadSessions()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.cmdCursor, m.copyNote = 0, ""
		}
	case "down", "j":
		if m.cursor < len(m.history)-1 {
			m.cursor++
			m.cmdCursor, m.copyNote = 0, ""
		}
	default:
		// tab chooses one of the selected action's commands, y copies it, c copies them all
		// (LEARN-COPY-001, 002); up and down stay on the list of actions.
		if mm, cmd, ok := m.copyKeys(key, false); ok {
			return mm, cmd
		}
	}
	return m, nil
}

// CommandsText is an action's commands as a script that runs as it is when pasted: one command
// per line, in order. The interactive join is included, since pasting it joins the session.
func CommandsText(e history.Entry) string {
	lines := make([]string, len(e.Commands))
	for i, c := range e.Commands {
		lines[i] = c.Display
	}
	return strings.Join(lines, "\n") + "\n"
}

// copyToClipboard asks the terminal to put text on the clipboard with OSC 52, which works over
// SSH and through Zellij because the terminal the developer is looking at does the copying.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
		_, _ = os.Stdout.WriteString(seq)
		return nil
	}
}

func (m Model) historyView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History") + dimStyle.Render("  every action run on this machine, newest first") + "\n\n")
	if len(m.history) == 0 {
		b.WriteString("Nothing has been run yet.\n")
	}
	for i, e := range m.history {
		cursor := "  "
		if i == m.cursor {
			cursor = keyStyle.Render("> ")
		}
		status := e.Status
		switch e.Status {
		case history.Done:
			status = okStyle.Render(status)
		case history.Failed, history.Interrupted:
			status = errStyle.Render(status)
		}
		b.WriteString(fmt.Sprintf("%s%s%s  %-14s %-12s %s\n", mark(hitEntry, i), cursor, e.Started.Local().Format("2006-01-02 15:04:05"), e.Machine, status, e.Title))
		if i > 12 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d older", len(m.history)-i-1)) + "\n")
			break
		}
	}
	if m.cursor < len(m.history) {
		e := m.history[m.cursor]
		b.WriteString("\n" + titleStyle.Render(e.Title) + dimStyle.Render("  ran on "+e.Machine) + "\n")
		for j, c := range e.Commands {
			outcome := string(c.Status)
			if c.Status == action.StatusOK {
				outcome = "ok, done"
			}
			if c.Status == action.StatusFailed {
				outcome = fmt.Sprintf("FAILED (exit %d)", c.Exit)
			}
			b.WriteString(wrapped(m.cmdGutter(j)+dimStyle.Render(fmt.Sprintf("%-17s", outcome))+" ", c.Display, m.cols()))
		}
	}
	if m.historyBad > 0 {
		b.WriteString("\n" + warnStyle.Render(fmt.Sprintf("%d damaged line(s) in the history were skipped.", m.historyBad)) + "\n")
	}
	if m.cursor < len(m.history) {
		b.WriteString("\n" + m.copyLine())
	}
	b.WriteString("\n" + dimStyle.Render(keyStyle.Render("tab")+" choose a command   "+keyStyle.Render("esc")+" sessions   "+keyStyle.Render("q")+" quit") + "\n")
	b.WriteString(dimStyle.Render("Kept in "+history.File()) + "\n")
	return b.String()
}
