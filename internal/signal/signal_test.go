package signal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Sessions obligation O6: a waiting report is believed only for a while, because the agent
// can miss clearing it; a damaged report is never believed. Fails if a stale or damaged
// report shows as waiting.
func TestWaitingStaleDamagedAndCleared(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Now()
	if st, _ := Read("project-a", time.Hour, now); st != None {
		t.Fatalf("no report: %v", st)
	}
	if err := SetWaiting("project-a", "Approve?"); err != nil {
		t.Fatal(err)
	}
	if st, q := Read("project-a", time.Hour, now); st != Waiting || q != "Approve?" {
		t.Fatalf("fresh: %v %q", st, q)
	}
	if st, _ := Read("project-a", time.Hour, now.Add(2*time.Hour)); st != Unknown {
		t.Fatalf("stale report believed: %v", st)
	}
	if err := Clear("project-a"); err != nil {
		t.Fatal(err)
	}
	if err := Clear("project-a"); err != nil {
		t.Fatal("clearing twice is an error")
	}
	if st, _ := Read("project-a", time.Hour, now); st != None {
		t.Fatalf("cleared: %v", st)
	}
	path, _ := file("project-b")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("{broken"), 0o644)
	if st, _ := Read("project-b", time.Hour, now); st != Unknown {
		t.Fatalf("damaged report: %v", st)
	}
}

// A session name that is not one this program makes can never become a path elsewhere.
func TestUnsafeSessionNamesAreRefused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, bad := range []string{"../escape", "a/b", "", "a b"} {
		if err := SetWaiting(bad, "x"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
