package action

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/sshtest"
)

// Connection obligation O2: an action that ran part of its commands on the engine before the
// connection dropped reports which ran and which did not. The engine is a real sshd on this
// machine, so what each command did there is read from its files; the connection is dropped while
// the second of three commands is running. Fails if the first is not reported as run, the second
// is reported as anything but failed with the lost connection said, the third ran or is not
// reported as not run, or the action is reported as successful.
func TestADroppedConnectionReportsWhatRanAndWhatDidNot(t *testing.T) {
	s := sshtest.Start(t)
	r := Remote{Host: sshtest.Host, SSHConfig: s.SSHConfig, ConnectTimeout: 5 * time.Second}
	dir := t.TempDir()
	at := func(name string) string { return filepath.Join(dir, name) }
	p := r.WrapPlan(Plan{Title: "three steps on the engine", Commands: []Command{
		{Argv: []string{"touch", at("first")}},
		{Argv: []string{"sh", "-c", `touch "$1"; sleep 30; touch "$2"`, "sh", at("second-started"), at("second-finished")}},
		{Argv: []string{"touch", at("third")}},
	}})

	done := make(chan []Step, 1)
	go func() {
		steps, ok := collect(p, context.Background())
		if ok {
			t.Error("an action whose connection dropped was reported as successful")
		}
		done <- steps
	}()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(at("second-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the second command never started on the engine\n%s", s.Log())
		}
	}
	s.Stop()
	var steps []Step
	select {
	case steps = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the action did not end when the connection dropped")
	}

	got := final(steps)
	if got[0] != StatusOK || got[1] != StatusFailed || got[2] != StatusNotRun {
		t.Fatalf("statuses %v, want ok, failed, not run", got)
	}
	last := steps[len(steps)-1]
	for _, st := range steps {
		if st.Index == 1 && st.Status == StatusFailed {
			last = st
		}
	}
	if last.Exit != 255 || !strings.Contains(last.Output, LostConnection) {
		t.Fatalf("the dropped command does not say the connection was lost: %+v", last)
	}
	for name, want := range map[string]bool{"first": true, "second-started": true, "second-finished": false, "third": false} {
		if _, err := os.Stat(at(name)); (err == nil) != want {
			t.Errorf("on the engine, %s exists: %v, want %v", name, err == nil, want)
		}
	}
}
