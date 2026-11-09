package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The mouse (SESS-MOUSE-001, 002) and copying commands (LEARN-COPY-001, 002).
//
// A click is answered from the frame the developer is looking at. While a screen is drawn, each
// clickable row is tagged with an invisible marker naming what it is (a session, a project, a
// command); View strips the markers and records the row each was on, so a click's row maps to
// exactly what was drawn there. Nothing about the layout is computed twice, so a click can never
// disagree with the screen.

// Kinds of clickable row.
const (
	hitSession  = 's'
	hitProject  = 'p'
	hitRoot     = 'r'
	hitRepo     = 'b'
	hitEntry    = 'h' // an action in the history list
	hitCommand  = 'c' // one command of the action shown: clicking copies it
	hitCopyAll  = 'A' // the "copy all" target
	markerStart = ''
	markerEnd   = ''
)

// hit is one clickable row of the last frame.
type hit struct {
	y     int
	kind  rune
	index int
}

// layout is where the last frame put its clickable rows. The model holds it by pointer, so View,
// which works on a copy of the model, can record it for the next Update to read.
type layout struct{ hits []hit }

// mark tags the row it starts as clickable. It is stripped before the frame is shown.
func mark(kind rune, index int) string {
	return string(markerStart) + string(kind) + strconv.Itoa(index) + string(markerEnd)
}

// strip removes the markers from a frame and records the row of each in l. Bubble Tea shows the
// bottom of a frame taller than the screen, so rows are counted from the first one shown.
func (l *layout) strip(frame string, height int) string {
	lines := strings.Split(frame, "\n")
	first := 0
	if height > 0 && len(lines) > height {
		first = len(lines) - height
	}
	l.hits = l.hits[:0]
	for y, line := range lines {
		if !strings.ContainsRune(line, markerStart) {
			continue
		}
		var b strings.Builder
		rest := line
		for {
			i := strings.IndexRune(rest, markerStart)
			if i < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:i])
			rest = rest[i+len(string(markerStart)):]
			j := strings.IndexRune(rest, markerEnd)
			if j < 1 {
				break
			}
			kind, _ := firstRune(rest[:j])
			n, _ := strconv.Atoi(rest[len(string(kind)):j])
			if y >= first {
				l.hits = append(l.hits, hit{y: y - first, kind: kind, index: n})
			}
			rest = rest[j+len(string(markerEnd)):]
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n")
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

// at is the clickable row at screen row y.
func (l *layout) at(y int) (hit, bool) {
	for _, h := range l.hits {
		if h.y == y {
			return h, true
		}
	}
	return hit{}, false
}

// mouse answers a click or a turn of the wheel. The wheel is the arrow keys; a click on a row
// selects it, and a click on the row already selected acts on it as enter does. A click on a
// command copies it, and on "copy all" copies the whole action.
func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action == tea.MouseActionPress {
			return m.key(tea.KeyMsg{Type: tea.KeyUp})
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		if msg.Action == tea.MouseActionPress {
			return m.key(tea.KeyMsg{Type: tea.KeyDown})
		}
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || m.layout == nil {
		return m, nil
	}
	h, ok := m.layout.at(msg.Y)
	if !ok {
		return m, nil
	}
	switch h.kind {
	case hitCommand:
		m.cmdCursor = h.index
		return m.copyCommand(h.index)
	case hitCopyAll:
		return m.copyAll()
	}
	if h.index == m.cursor {
		return m.key(tea.KeyMsg{Type: tea.KeyEnter})
	}
	m.cursor = h.index
	return m, nil
}

// shownCommands is the commands of the action on screen, as they are shown and run: the plan's
// on the preview and run screens, the selected action's in the history.
func (m Model) shownCommands() []string {
	switch m.screen {
	case screenPreview, screenRun:
		return m.plan.Lines()
	case screenHistory:
		if m.cursor < len(m.history) {
			var out []string
			for _, c := range m.history[m.cursor].Commands {
				out = append(out, c.Display)
			}
			return out
		}
	}
	return nil
}

// copyCommand sends one command to the terminal's clipboard (LEARN-COPY-001). The terminal never
// confirms a clipboard request, so the message says it was sent, not that it was copied.
func (m Model) copyCommand(i int) (tea.Model, tea.Cmd) {
	cmds := m.shownCommands()
	if i < 0 || i >= len(cmds) {
		return m, nil
	}
	m.copyNote = "Sent to the terminal's clipboard: " + cmds[i]
	return m, copyToClipboard(cmds[i])
}

// copyAll sends every command of the action to the terminal's clipboard, each joined to the
// next with "&&" at the end of its line, so pasted into a shell they stop at the first failure
// as the program does (LEARN-COPY-002).
func (m Model) copyAll() (tea.Model, tea.Cmd) {
	cmds := m.shownCommands()
	if len(cmds) == 0 {
		return m, nil
	}
	m.copyNote = fmt.Sprintf("Sent %d command(s) to the terminal's clipboard, joined with && so a paste stops at the first failure.", len(cmds))
	return m, copyToClipboard(JoinAll(cmds))
}

// JoinAll is commands as one shell command that stops at the first failure: each line ends in
// "&&" except the last, so it reads one command per line and pastes as a single chain.
func JoinAll(cmds []string) string {
	return strings.Join(cmds, " &&\n") + "\n"
}

// copyKeys are the copy keys on a screen that shows an action's commands: up and down (or tab)
// choose a command, y copies it, c copies them all. It reports whether the key was one of them.
func (m Model) copyKeys(key string, arrows bool) (tea.Model, tea.Cmd, bool) {
	n := len(m.shownCommands())
	switch {
	case key == "y":
		mm, cmd := m.copyCommand(m.cmdCursor)
		return mm, cmd, true
	case key == "c":
		mm, cmd := m.copyAll()
		return mm, cmd, true
	case key == "tab" || arrows && (key == "down" || key == "j"):
		if m.cmdCursor < n-1 {
			m.cmdCursor++
		}
		return m, nil, true
	case key == "shift+tab" || arrows && (key == "up" || key == "k"):
		if m.cmdCursor > 0 {
			m.cmdCursor--
		}
		return m, nil, true
	}
	return m, nil, false
}

// cmdGutter is the start of a command's row: its click marker and, for the selected command, the
// cursor.
func (m Model) cmdGutter(i int) string {
	if i == m.cmdCursor {
		return mark(hitCommand, i) + keyStyle.Render("> ")
	}
	return mark(hitCommand, i) + "  "
}

// copyLine is the copy targets and keys under an action's commands, and what the last copy sent.
func (m Model) copyLine() string {
	s := mark(hitCopyAll, 0) + keyStyle.Render("[copy all]") + dimStyle.Render("  click a command to copy it, or "+keyStyle.Render("y")+" copy the selected one, "+keyStyle.Render("c")+" copy all") + "\n"
	if m.copyNote != "" {
		s += okStyle.Width(m.cols()-1).Render(m.copyNote) + "\n"
	}
	return s
}
