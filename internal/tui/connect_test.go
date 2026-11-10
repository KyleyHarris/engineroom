package tui

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/sshtest"
)

// clientOf is a client's configuration for an engine reached as host through sshConfig, written to
// a file of the test's own (working locally reads it again).
func clientOf(t *testing.T, host, sshConfig, timeout string) *config.Config {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.toml")
	text := "role = \"client\"\n[engine]\nhost = \"" + host + "\"\nssh_config = \"" + sshConfig + "\"\nconnect_timeout = \"" + timeout + "\"\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINEROOM_CONFIG", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// sshChildren is every ssh this test process has started that is still running, found by its
// parent, never by a name pattern across the machine.
func sshChildren(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		t.Fatal(err)
	}
	me := strconv.Itoa(os.Getpid())
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] == me && filepath.Base(f[2]) == "ssh" {
			found = append(found, line)
		}
	}
	return found
}

func key(r string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)} }

// Connection obligation O10 on screen: an engine whose host key has changed is said to have a
// changed host key, with what to do, and no retry is offered or taken; working locally and stopping
// still are. Real sshd, restarted with a key the client does not know. Fails if the question leads
// with "did not answer", shows the retry key, or r checks the engine again.
func TestAChangedHostKeyIsNotOfferedAsARetry(t *testing.T) {
	s := sshtest.Start(t)
	s.ChangeHostKey()
	m := New(clientOf(t, sshtest.Host, s.SSHConfig, "5s"), Intent{Open: "project-a"})
	next, _ := m.Update(m.Init()())
	down := next.(Model)
	view := down.View()
	if down.screen != screenEngineDown || !strings.Contains(view, "host key has changed") || strings.Contains(view, "did not answer") {
		t.Fatalf("screen %v:\n%s", down.screen, view)
	}
	if strings.Contains(view, "retry the engine") || !strings.Contains(view, "work locally") || !strings.Contains(view, "stop") {
		t.Fatalf("the choices for a changed host key:\n%s", view)
	}
	if again, cmd := down.Update(key("r")); again.(Model).screen != screenEngineDown || cmd != nil {
		t.Fatalf("r checked the engine again: screen %v, command %v", again.(Model).screen, cmd != nil)
	}
}

// Connection obligation O5: Ctrl-C while the engine is being checked ends the program with nothing
// started and no connection left behind, on either machine. The engine is a real listener that
// takes the connection and never speaks, so the check is in flight when Ctrl-C comes. Fails if the
// ssh of the check outlives the program's quit (it would otherwise run on until its timeout), the
// engine side keeps the connection, or anything but quitting follows.
func TestInterruptingTheEngineCheckLeavesNoConnection(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- c
		}
	}()
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	sshConfig := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(sshConfig, []byte("Host silent\n  HostName 127.0.0.1\n  Port "+port+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(clientOf(t, "silent", sshConfig, "30s"), Intent{Open: "project-a"})
	check := m.Init()
	result := make(chan tea.Msg, 1)
	go func() { result <- check() }()

	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("the check never reached the engine")
	}
	defer conn.Close()
	if len(sshChildren(t)) == 0 {
		t.Fatal("no ssh is running for the check: the test would prove nothing")
	}

	start := time.Now()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuit(cmd) {
		t.Fatalf("Ctrl-C did not quit: %v", cmd)
	}
	if left := sshChildren(t); len(left) > 0 {
		t.Fatalf("the check's ssh is still running after the quit:\n%s", strings.Join(left, "\n"))
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("quitting took %s", took)
	}
	// The engine side: once ssh's greeting is read, the connection ends (EOF) because the client's
	// ssh has gone; a deadline passing instead means it is still open.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("the engine side still holds an open connection: %v", err)
	}
	select {
	case <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("the check never returned")
	}
}

// Connection obligation O5 at the question: Ctrl-C (and q, and s) at the engine-down question
// ends the program with nothing started and no ssh running. Fails if any of them returns anything
// but quitting, or leaves an ssh behind.
func TestInterruptingTheQuestionStartsNothing(t *testing.T) {
	sshConfig := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(sshConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(clientOf(t, unreachableAddress, sshConfig, "1s"), Intent{Open: "project-a"})
	next, _ := m.Update(m.Init()())
	down := next.(Model)
	if down.screen != screenEngineDown {
		t.Fatalf("screen %v", down.screen)
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, key("q"), key("s")} {
		if _, cmd := down.Update(k); !isQuit(cmd) {
			t.Fatalf("%s at the question: %v, want quit", k, cmd)
		}
	}
	if left := sshChildren(t); len(left) > 0 {
		t.Fatalf("ssh still running:\n%s", strings.Join(left, "\n"))
	}
}
