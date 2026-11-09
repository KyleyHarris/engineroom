package procs

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// A process started with ENGINEROOM_SESSION is found with that owner, its environment read whole (a
// value holding "=" included), and Stop ends it. Fails if the environment is misread (the
// owner would be empty) or Stop leaves the process running.
func TestOwnerAndStop(t *testing.T) {
	// The test binary itself, sleeping: Apple's own binaries (/bin/sleep) have their environment
	// hidden from other processes, as a developer's servers do not.
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperSleep")
	cmd.Env = append(os.Environ(), "PROCS_HELPER=1", "ENGINEROOM_SESSION=procs-test-owner", "ZELLIJ_SESSION_NAME=other", "ENGINEROOM_X=a=b")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	go func() { _, _ = cmd.Process.Wait() }()
	time.Sleep(100 * time.Millisecond)

	all, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var found *Proc
	for i := range all {
		if all[i].PID == cmd.Process.Pid {
			found = &all[i]
		}
	}
	if found == nil {
		t.Fatalf("the started process %d is not listed", cmd.Process.Pid)
	}
	if got := found.Owner(nil); got != "procs-test-owner" {
		t.Fatalf("owner %q: ENGINEROOM_SESSION must win over ZELLIJ_SESSION_NAME", got)
	}
	if found.Env["ENGINEROOM_X"] != "a=b" {
		t.Fatalf("a value holding = was misread: %q", found.Env["ENGINEROOM_X"])
	}
	if Ancestors(all)[cmd.Process.Pid] {
		t.Fatalf("a child is not an ancestor")
	}
	if left := Stop([]Proc{*found}, 2*time.Second); len(left) != 0 {
		t.Fatalf("still running after Stop: %v", left)
	}
}

// Without ENGINEROOM_SESSION, the checkout engineroom ran it for decides, then the Zellij session.
func TestOwnerFallbacks(t *testing.T) {
	p := Proc{Env: map[string]string{"ENGINEROOM_WORKTREE": "/w/a", "ZELLIJ_SESSION_NAME": "z"}}
	if got := p.Owner(func(w string) string { return map[string]string{"/w/a": "a"}[w] }); got != "a" {
		t.Fatalf("owner %q, want the worktree's session", got)
	}
	if got := p.Owner(func(string) string { return "" }); got != "z" {
		t.Fatalf("owner %q, want the Zellij session", got)
	}
	if got := (Proc{Env: map[string]string{}}).Owner(nil); got != "" {
		t.Fatalf("owner %q, want none", got)
	}
}

// TestHelperSleep is the process TestOwnerAndStop starts; it does nothing on its own.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("PROCS_HELPER") != "1" {
		t.Skip("only run as a helper process")
	}
	time.Sleep(30 * time.Second)
}
