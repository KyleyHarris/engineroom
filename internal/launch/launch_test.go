package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fails if a command run through Clean still sees this program's environment: an application it
// started would keep the caller's home, folders and session markers.
func TestCleanRunsWithAnEmptyEnvironment(t *testing.T) {
	t.Setenv("ENGINEROOM_LAUNCH_PROBE", "from-the-caller")
	argv := Clean("sh", "-c", "env")
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("%q: %v", argv, err)
	}
	for _, name := range []string{"ENGINEROOM_LAUNCH_PROBE=", "HOME=", "PATH=/"} {
		if strings.Contains(string(out), name) {
			t.Fatalf("the command saw %s from the caller:\n%s", name, out)
		}
	}
}

// Fails if the command is looked up on the empty environment's default path instead of this
// program's PATH: a stand-in for osascript (as the journeys install) would be passed over for
// the real one, which opens a real window.
func TestCleanFindsTheCommandOnThisProgramsPath(t *testing.T) {
	dir := t.TempDir()
	standIn := filepath.Join(dir, "osascript")
	if err := os.WriteFile(standIn, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got := Clean("osascript", "-e", "x")
	want := []string{Env, "-i", standIn, "-e", "x"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %q, want %q", got, want)
	}
	// A command not on PATH is left as given, so running it fails with its own name.
	if got := Clean("no-such-launcher-here"); got[2] != "no-such-launcher-here" {
		t.Fatalf("got %q", got)
	}
}
