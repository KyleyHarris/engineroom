package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The reason a join failed is kept from what Zellij wrote, without the panic's header and
// backtrace hint, whatever came before it. Fails if the reason is lost or buried in those lines.
func TestTheLastWordsAreKept(t *testing.T) {
	w := &tailWriter{}
	_, _ = w.Write([]byte(strings.Repeat("earlier output\n", 500)))
	_, _ = w.Write([]byte("thread 'main' (23774271) panicked at src/commands.rs:805:25:\r\n" +
		"You are trying to attach to the current session (\"project-a\"). This is not supported.\r\n" +
		"note: run with `RUST_BACKTRACE=1` environment variable to display a backtrace\r\n"))
	got := w.Said()
	if !strings.HasSuffix(got, `You are trying to attach to the current session ("project-a"). This is not supported.`) || strings.Contains(got, "panicked") || strings.Contains(got, "RUST_BACKTRACE") {
		t.Fatalf("said %q", got)
	}
	if len(w.buf) > tailMax {
		t.Fatalf("kept %d bytes", len(w.buf))
	}
}

// A join that fails and says why, in a program started only to join, stays open with the reason
// and exits 1, so a window opened for it (the app's) does not close before the reason is read;
// one that ends well, or whose session ended under it, still quits. Fails if a failed join quits,
// loses its reason, or exits 0, or if a session's end holds the program open.
func TestAFailedJoinStaysOpenWithItsReason(t *testing.T) {
	m := Model{quitAfter: true}
	next, cmd := m.Update(handedBackMsg{err: errors.New("exit status 101"), command: "zellij attach project-a", said: "This is not supported."})
	got := next.(Model)
	if got.message != "zellij attach project-a ended with exit status 101: This is not supported." || got.ExitCode() != 1 || isQuit(cmd) {
		t.Fatalf("message %q, exit %d, quit %v", got.message, got.ExitCode(), isQuit(cmd))
	}
	_, cmd = m.Update(handedBackMsg{command: "zellij attach project-a"})
	if !isQuit(cmd) {
		t.Fatal("a join that ended well did not quit")
	}
	// A session ended while joined ends Zellij with a failure status and no words on standard
	// error; that is the session's end, and the program ends with it.
	_, cmd = m.Update(handedBackMsg{err: errors.New("exit status 1"), command: "zellij attach project-a"})
	if !isQuit(cmd) {
		t.Fatal("a session that ended while joined did not quit")
	}
	// A join over SSH that was attached, then ended with words when its session was restarted,
	// was the session's end: the program ends, so the app's tab is free for the next join.
	_, cmd = m.Update(handedBackMsg{err: errors.New("exit status 255"), command: "ssh -t engine zellij attach project-a", said: "Connection to engine closed.", lasted: attachedFor})
	if !isQuit(cmd) {
		t.Fatal("a join that was attached and ended with its session's end did not quit")
	}
}

// isQuit reports whether cmd is tea.Quit itself, without running it: the other commands an update
// returns read sessions, which a bare model has nothing to read them from.
func isQuit(cmd tea.Cmd) bool {
	return cmd != nil && reflect.ValueOf(cmd).Pointer() == reflect.ValueOf(tea.Quit).Pointer()
}
