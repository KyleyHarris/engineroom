package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rexMachine sets up a machine whose sessions are kept in Rex, with a stand-in rex that records
// each command line and exits with code (docs/rex.md; the real one needs the Rex app). conf is
// the rest of the configuration: the role and, on a client, rex.host.
func rexMachine(t *testing.T, conf string, code string) (log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log := filepath.Join(dir, "rex"), filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\nexit " + code + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("multiplexer = \"rex\"\n"+conf+"\n\n[rex]\npath = \""+bin+"\"\n"+hostLine(conf)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINEROOM_CONFIG", path)
	return log
}

// hostLine is rex.host for a client's configuration.
func hostLine(conf string) string {
	if strings.Contains(conf, `"client"`) {
		return "host = \"engine\"\n"
	}
	return ""
}

func ran(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// On the engine, showing a session is Go to Session in this machine's Rex app, and nothing else.
func TestRexShowOnTheEngine(t *testing.T) {
	log := rexMachine(t, `role = "engine"`, "0")
	if code, _, errOut := engineroom(t, "rex", "show", "project-a"); code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if got := ran(t, log); len(got) != 1 || got[0] != "do session.select session=project-a" {
		t.Fatalf("ran %q", got)
	}
}

// On a client, the app is switched to the engine's Rex server first and the session chosen there;
// with --here it is one of the client's own sessions. Fails if a client shows a session on its
// own server, or --here goes to the engine's.
func TestRexShowOnAClientSwitchesToTheEngine(t *testing.T) {
	log := rexMachine(t, "role = \"client\"\n[engine]\nhost = \"engine.example\"", "0")
	if code, _, errOut := engineroom(t, "rex", "show", "project-a"); code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	want := []string{"do client.host.switch host=engine", "-S engine do session.select session=project-a"}
	if got := ran(t, log); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", got, want)
	}
	_ = os.Remove(log)
	if code, _, errOut := engineroom(t, "rex", "show", "project-a", "--here"); code != 0 {
		t.Fatalf("--here: %d %s", code, errOut)
	}
	if got := ran(t, log); len(got) != 1 || got[0] != "do session.select session=project-a" {
		t.Fatalf("--here ran %q", got)
	}
}

// A client without rex.host is told what to set, and nothing is run: showing on its own server
// would show the wrong machine's sessions.
func TestRexShowOnAClientNeedsTheEngineHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("role = \"client\"\nmultiplexer = \"rex\"\n[engine]\nhost = \"engine.example\"\n[rex]\npath = \"/nonexistent/rex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINEROOM_CONFIG", path)
	if code, _, errOut := engineroom(t, "rex", "show", "project-a"); code != 1 || !strings.Contains(errOut, "rex.host") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// A failure is said by what Rex's exit status means: 3 is no such session, or more than one.
func TestRexShowSaysWhatAFailureMeans(t *testing.T) {
	rexMachine(t, `role = "engine"`, "3")
	code, _, errOut := engineroom(t, "rex", "show", "project-a")
	if code != 1 || !strings.Contains(errOut, "+ ") || !strings.Contains(errOut, "nothing was changed") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// A machine not set to Rex refuses, rather than driving a Rex it does not use.
func TestRexShowRefusedWithoutRex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("role = \"engine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINEROOM_CONFIG", path)
	if code, _, errOut := engineroom(t, "rex", "show", "project-a"); code != 1 || !strings.Contains(errOut, `multiplexer = "rex"`) {
		t.Fatalf("%d %q", code, errOut)
	}
}
