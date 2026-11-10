package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Fails if the job would hand the session the SSH connection's variables (an agent socket that
// closes with the connection, the connection's temporary folder) or a Claude Code session's markers,
// or would lose the developer's own settings and the program's folders.
func TestEnvKeepsTheDevelopersOwnAndLeavesTheLogins(t *testing.T) {
	env := Env([]string{
		"HOME=/Users/dev", "PATH=/opt/homebrew/bin:/usr/bin", "XDG_CONFIG_HOME=/x/config", "GH_TOKEN=kept",
		"SSH_CONNECTION=1 2 3 4", "SSH_CLIENT=1 2 3", "SSH_TTY=/dev/ttys001", "SSH_AUTH_SOCK=/tmp/agent", "TMPDIR=/tmp/ssh/",
		"PWD=/x", "SHLVL=2", "_=/usr/bin/env", "CLAUDECODE=1", "not-a-variable",
	}, []string{"CLAUDECODE"})
	for _, kept := range []string{"HOME", "PATH", "XDG_CONFIG_HOME", "GH_TOKEN"} {
		if _, ok := env[kept]; !ok {
			t.Errorf("%s was left out", kept)
		}
	}
	for _, left := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", "SSH_AUTH_SOCK", "TMPDIR", "PWD", "SHLVL", "_", "CLAUDECODE"} {
		if _, ok := env[left]; ok {
			t.Errorf("%s was given to the session", left)
		}
	}
}

// Fails if the job file is not a property list launchd reads, runs anything but the given command
// (quoted by nobody), can be started again or stops what it started when it goes, or does not
// remove itself.
func TestJobRunsTheCommandOnceAndRemovesItself(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd jobs are macOS only")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	argv := []string{"zellij", "attach", "--create-background", "project-a", "options", "--default-cwd", "/src/a b & <c>"}
	path, err := Write("gui/501", "project-a", argv, "/src/a b & <c>", map[string]string{"HOME": "/Users/dev", "ODD": `a"b<c>&`})
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("the job file is readable by others: %v", info.Mode())
	}
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatalf("not a property list: %v", err)
	}
	var job struct {
		Label                string
		ProgramArguments     []string
		WorkingDirectory     string
		EnvironmentVariables map[string]string
		RunAtLoad            bool
		AbandonProcessGroup  bool
		KeepAlive            *bool
		StandardErrorPath    string
	}
	if err := json.Unmarshal(out, &job); err != nil {
		t.Fatal(err)
	}
	if job.Label != Label("project-a") || !strings.HasSuffix(path, job.Label+".plist") {
		t.Fatalf("label %q, file %s", job.Label, path)
	}
	if strings.Join(job.ProgramArguments[4:], "\x00") != strings.Join(argv, "\x00") || job.ProgramArguments[3] != "sh" {
		t.Fatalf("runs %q", job.ProgramArguments)
	}
	if want := "/bin/launchctl bootout gui/501/" + job.Label; !strings.Contains(job.ProgramArguments[2], want) {
		t.Fatalf("does not remove itself: %q", job.ProgramArguments[2])
	}
	if job.WorkingDirectory != "/src/a b & <c>" || job.EnvironmentVariables["ODD"] != `a"b<c>&` || job.EnvironmentVariables["HOME"] != "/Users/dev" {
		t.Fatalf("folder or environment lost: %+v", job)
	}
	if !job.RunAtLoad || !job.AbandonProcessGroup || job.KeepAlive != nil || job.StandardErrorPath != LogFile("project-a") {
		t.Fatalf("runs again, or stops what it started: %+v", job)
	}
}

// Fails if two state folders (the developer's and a test's) could ask launchd for one label.
func TestLabelDiffersByStateFolder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "a"))
	a := Label("project-a")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "b"))
	if b := Label("project-a"); a == b || !strings.HasSuffix(a, ".project-a") {
		t.Fatalf("%s and %s", a, b)
	}
}

// Fails if turning the route off does not keep a session out of the desktop login.
func TestDomainOff(t *testing.T) {
	t.Setenv(ModeVar, "off")
	if d, ok := Domain(); ok {
		t.Fatalf("chose %s while off", d)
	}
}
