package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// zellijMachine is an isolated machine with its own Zellij (journey.NewMachine: its own home, state
// and short socket folder) and a plain layout, for starting sessions with the built program. Every
// Zellij server started under its socket folder is stopped when the test ends, by process id,
// including one whose socket is gone, which Zellij itself can no longer reach.
func zellijMachine(t *testing.T) (*journey.Machine, string) {
	t.Helper()
	m := journey.NewMachine(t)
	layout := filepath.Join(m.Home, "plain.kdl")
	if err := os.WriteFile(layout, []byte("layout {\n    pane command=\"sh\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, pid := range servers(t, m, "") {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return m, layout
}

// servers is the process id of every Zellij server for this machine's session called name (any
// session, when name is empty), read from the process table by the socket path the server was
// started with, so a server that lost its socket is found too.
func servers(t *testing.T, m *journey.Machine, name string) []int {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "--server " + filepath.Join(m.Sockets, "contract_version_1") + "/"
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		if session := strings.Fields(line[i+len(prefix):]); name != "" && (len(session) == 0 || session[0] != name) {
			continue
		}
		if pid, err := strconv.Atoi(strings.Fields(line)[0]); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// listed is how many times Zellij on this machine lists name as running.
func listed(m *journey.Machine, name string) int {
	n := 0
	for _, s := range zj.Parse(m.Sessions()) {
		if s.Name == name && !s.Ended {
			n++
		}
	}
	return n
}

// settled waits until the session called name is either whole (listed once as running, answering,
// with one server) or absent (not listed, no server), and says which; anything else after ten
// seconds is a half-made session and fails the test.
func settled(t *testing.T, m *journey.Machine, name string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n, srv := listed(m, name), servers(t, m, name)
		if n == 1 && len(srv) == 1 {
			if out, err := m.Run("zellij", "--session", name, "action", "query-tab-names"); err == nil && strings.TrimSpace(out) != "" && !strings.Contains(out, "not found") {
				return "whole"
			}
		}
		if n == 0 && len(srv) == 0 {
			return "absent"
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is half made: listed %d times, %d servers %v\n%s", name, n, len(srv), srv, m.Sessions())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Sessions obligation O4: two machines (two processes) starting the same session at the same
// moment get one session, and both starts succeed, so both go on to join it. Zellij's own
// background create, run twice at once for one name, can leave no session listed and a server
// running with no socket; the start's lock is what prevents it. Three rounds, since an unguarded
// race is lost only most of the time. Fails if either start fails, the session is listed other than
// once, or more or fewer than one server runs for it.
func TestTwoStartsAtOnceMakeOneSession(t *testing.T) {
	m, layout := zellijMachine(t)
	for round := 1; round <= 3; round++ {
		name := fmt.Sprintf("race-%d", round)
		var wg sync.WaitGroup
		outs := make([]string, 2)
		errs := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				outs[i], errs[i] = m.Run("engineroom", "session", "start", name, layout, m.Home)
			}()
		}
		wg.Wait()
		for i := range 2 {
			if errs[i] != nil {
				t.Fatalf("round %d: start %d failed: %v\n%s", round, i+1, errs[i], outs[i])
			}
		}
		if got := settled(t, m, name); got != "whole" {
			t.Fatalf("round %d: the session is %s after two starts\n%s\n%s", round, got, outs[0], outs[1])
		}
		if !strings.Contains(outs[0]+outs[1], "already running, so it is not started again") {
			t.Fatalf("round %d: neither start found the other's session:\n%s\n%s", round, outs[0], outs[1])
		}
	}
}

// Sessions obligation O5: a start interrupted at any point (the program kills it, as Ctrl-C does
// to a running step) leaves either a whole session or none, never a half-made one; and the next
// start of the same session succeeds, so an interrupted start holds nothing (its lock) behind it.
// The interruption comes at several points, from before Zellij is asked to well after. Fails if a
// session is left listed but not answering, a server is left without a listed session, or the
// start after it fails or makes a second session.
func TestAnInterruptedStartLeavesAWholeSessionOrNone(t *testing.T) {
	m, layout := zellijMachine(t)
	for i, after := range []time.Duration{0, 20 * time.Millisecond, 100 * time.Millisecond, 300 * time.Millisecond, time.Second} {
		name := fmt.Sprintf("cut-%d", i)
		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, m.Engineroom, "session", "start", name, layout, m.Home)
		cmd.Env, cmd.Dir = m.Env(), m.Home
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(after)
		cancel()
		_ = cmd.Wait()
		state := settled(t, m, name)
		t.Logf("interrupted after %s: %s", after, state)

		out, err := m.Run("engineroom", "session", "start", name, layout, m.Home)
		if err != nil {
			t.Fatalf("the start after one interrupted at %s failed: %v\n%s", after, err, out)
		}
		if got := settled(t, m, name); got != "whole" {
			t.Fatalf("after the start that followed one interrupted at %s, the session is %s", after, got)
		}
	}
}
