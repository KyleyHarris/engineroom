// Package journey builds the isolated world a journey runs in: the program built from this
// checkout, a private home directory and configuration, a private Zellij, a stand-in for the
// AI agent, and (when a journey needs one) a throwaway engine reached over real SSH.
//
// Nothing here reads or writes the developer's own setup. Every path is a fresh temporary
// directory, every Zellij socket lives in its own short directory, and the engine is a container
// that is removed when the run ends (docs/decisions.md D-003).
package journey

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// Root is the repository root, found from this file's location so journeys run from any
// directory.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// Binary is the program built from this checkout for this machine, built once per run.
func Binary(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "engineroom-build-")
		if buildErr != nil {
			return
		}
		out, err := goBuild(filepath.Join(buildDir, "engineroom"), runtime.GOOS, runtime.GOARCH)
		if err != nil {
			buildErr = fmt.Errorf("building engineroom: %v\n%s", err, out)
			return
		}
		// The stand-in for a developer's server (internal/journey/listen), on every machine's
		// PATH beside engineroom. A built program, not a script run by an Apple-signed interpreter,
		// so its environment can be read as a real server's can.
		cmd := exec.Command("go", "build", "-o", filepath.Join(buildDir, "listen"), "./internal/journey/listen")
		cmd.Dir = Root()
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building listen: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return filepath.Join(buildDir, "engineroom")
}

func goBuild(out, goos, goarch string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", out, "./cmd/engineroom")
	cmd.Dir = Root()
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	return cmd.CombinedOutput()
}

// Machine is one isolated machine as the program sees it: a home directory, its configuration
// and state, its own Zellij, and a PATH holding the program and the agent stand-in.
type Machine struct {
	t          testing.TB
	Home       string
	Sockets    string
	Bin        string
	Engineroom string
	// AgentLog records every start of the agent stand-in, one line per start.
	AgentLog string
	// AgentPromptLog records the prompt each start of the agent stand-in was given, when it was
	// given one, read from its arguments as Claude Code reads them.
	AgentPromptLog string
	// EditorLog records every start of the editor stand-in, one line per start.
	EditorLog string
	// Extra is added to this machine's environment, for a journey that needs a setting of its
	// own (where applications are looked for).
	Extra []string
}

// NewMachine prepares an isolated machine, with the stand-ins for open and osascript and a
// Ghostty of its own (DefaultTerminals), so nothing a journey does opens a window or starts an
// application on the machine running it. The Zellij socket directory is created under /tmp
// with a short name on purpose: Zellij derives its maximum session name length from the socket
// path, and a long temporary path leaves it no room for any name at all.
func NewMachine(t testing.TB) *Machine {
	t.Helper()
	home := t.TempDir()
	sockets, err := os.MkdirTemp("/tmp", "zh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	m := &Machine{t: t, Home: home, Sockets: sockets, Bin: filepath.Join(home, "bin"), AgentLog: filepath.Join(home, "agent.log"), AgentPromptLog: filepath.Join(home, "agent-prompts.log"), EditorLog: filepath.Join(home, "editor.log")}
	for _, d := range []string{m.Bin, filepath.Join(home, ".config", "zellij"), filepath.Join(home, ".config", "engineroom"), filepath.Join(home, ".local", "state")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.Engineroom = Binary(t)
	if err := os.Symlink(m.Engineroom, filepath.Join(m.Bin, "engineroom")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(m.Engineroom), "listen"), filepath.Join(m.Bin, "listen")); err != nil {
		t.Fatal(err)
	}
	m.write(filepath.Join(home, ".config", "zellij", "config.kdl"), ZellijConfig)
	for name, script := range map[string]string{"fake-agent": FakeAgent, "fake-editor": FakeEditor} {
		m.write(filepath.Join(m.Bin, name), script)
		if err := os.Chmod(filepath.Join(m.Bin, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// No journey reaches the real terminal or the real applications, whether or not it looks at
	// what would have opened.
	m.standInLaunchers()
	m.FakeTerminals(DefaultTerminals...)
	t.Cleanup(m.stopZellij)
	return m
}

func (m *Machine) write(path, content string) {
	m.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

// ConfigPath is where the program reads this machine's configuration.
func (m *Machine) ConfigPath() string {
	return filepath.Join(m.Home, ".config", "engineroom", "config.toml")
}

// WriteConfig writes this machine's configuration file.
func (m *Machine) WriteConfig(toml string) {
	m.t.Helper()
	m.write(m.ConfigPath(), toml)
}

// Env is the environment every process on this machine runs with. It is built from scratch
// rather than inherited, so nothing from the developer's shell (their real HOME, their
// ZELLIJ variables, their SSH agent) can leak into a journey.
func (m *Machine) Env() []string {
	return append([]string{
		"HOME=" + m.Home,
		"USER=dev",
		"LOGNAME=dev",
		"SHELL=/bin/sh",
		"LANG=en_US.UTF-8",
		"PATH=" + m.Bin + ":" + systemPath(),
		"XDG_CONFIG_HOME=" + filepath.Join(m.Home, ".config"),
		"XDG_STATE_HOME=" + filepath.Join(m.Home, ".local", "state"),
		"ZELLIJ_SOCKET_DIR=" + m.Sockets,
		"ZELLIJ_CONFIG_DIR=" + filepath.Join(m.Home, ".config", "zellij"),
		"FAKE_AGENT_LOG=" + m.AgentLog,
		"FAKE_AGENT_PROMPTS=" + m.AgentPromptLog,
		"FAKE_EDITOR_LOG=" + m.EditorLog,
		// Sessions start as the journey runs, never in the developer's real desktop login (D-031);
		// the journey that proves that route turns it on through Extra, which comes later and wins.
		"ENGINEROOM_DESKTOP_SESSIONS=off",
	}, m.Extra...)
}

// systemPath is the directories where git, ssh, zellij and docker live, without anything from
// the developer's own PATH that could shadow them unpredictably.
func systemPath() string {
	dirs := []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	for _, tool := range []string{"zellij", "git", "ssh", "docker"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append([]string{filepath.Dir(p)}, dirs...)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return strings.Join(out, ":")
}

// Command builds a command that runs on this machine with its environment.
func (m *Machine) Command(name string, args ...string) *exec.Cmd {
	// The program itself is resolved on this machine's PATH, not the test process's.
	if name == "engineroom" {
		name = m.Engineroom
	}
	cmd := exec.Command(name, args...)
	cmd.Env = m.Env()
	cmd.Dir = m.Home
	return cmd
}

// Run runs a command on this machine to check a side effect, returning its combined output.
func (m *Machine) Run(name string, args ...string) (string, error) {
	out, err := m.Command(name, args...).CombinedOutput()
	return string(out), err
}

// Start runs the program in a terminal of a fixed size, recording it when JOURNEY_RECORD
// names a directory.
func (m *Machine) Start(t testing.TB, args ...string) *termtest.Terminal {
	t.Helper()
	cmd := m.Command(m.Engineroom, args...)
	opts := termtest.Options{Cols: 120, Rows: 34, Timeout: 20 * time.Second, Title: t.Name()}
	if dir := os.Getenv("JOURNEY_RECORD"); dir != "" {
		opts.Record = filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "_")+".cast")
	}
	return termtest.Start(t, cmd, opts)
}

// Sessions lists this machine's Zellij sessions, read from Zellij itself rather than from the
// program, as "name state" lines.
func (m *Machine) Sessions() string {
	out, _ := m.Run("zellij", "list-sessions", "--no-formatting")
	return out
}

func (m *Machine) stopZellij() {
	_, _ = m.Run("zellij", "kill-all-sessions", "--yes")
	_, _ = m.Run("zellij", "delete-all-sessions", "--yes", "--force")
}

// ZellijConfig keeps journeys' Zellij quiet and predictable: no release notes or tips over the
// panes, and no session resurrection timing differences between machines.
const ZellijConfig = `show_release_notes false
show_startup_tips false
session_serialization true
serialization_interval 1
`

// FakeAgent stands in for Claude Code. The real agent needs an account and a network and
// costs money per run, so it is the one dependency journeys do not run for real.
//
// It records each start (working directory and arguments) in FAKE_AGENT_LOG, or in
// $HOME/agent.log where that is not set (an engine session started over SSH does not inherit the
// container's environment). It records the prompt the start was given, read from its arguments as
// Claude Code reads them, in FAKE_AGENT_PROMPTS (or $HOME/agent-prompts.log): --add-dir takes every
// argument up to the next option, the other options used take one value each (--remote-control's
// is optional, and always given), so a prompt placed where Claude Code would take it as a folder
// is not recorded as a prompt. It keeps a
// conversation marker in the directory it runs in, refuses --continue when there is no earlier
// conversation (so "continue, or start fresh" behaves as the real one does), and asks a
// question through the program when FAKE_AGENT_ASK is set.
const FakeAgent = `#!/bin/sh
dir="$(pwd)"
echo "start $dir $*" >> "${FAKE_AGENT_LOG:-$HOME/agent.log}"
prompt=""; take=""
for a in "$@"; do
  if [ "$take" = many ]; then
    case "$a" in -*) take="" ;; *) continue ;; esac
  fi
  if [ "$take" = one ]; then take=""; continue; fi
  case "$a" in
    --add-dir) take=many ;;
    --append-system-prompt-file|--name|-n|--remote-control) take=one ;;
    -*) ;;
    *) prompt="$a" ;;
  esac
done
if [ -n "$prompt" ]; then
  echo "prompt $dir $prompt" >> "${FAKE_AGENT_PROMPTS:-$HOME/agent-prompts.log}"
fi
marker="$dir/.fake-agent-conversation"
if [ "$1" = "--continue" ]; then
  if [ ! -f "$marker" ]; then
    echo "No conversation found to continue"
    exit 1
  fi
  echo "AGENT continuing conversation $(cat "$marker") in $dir"
else
  date +%s > "$marker"
  echo "AGENT new conversation in $dir"
fi
if [ -n "${FAKE_AGENT_ASK:-}" ]; then
  engineroom signal waiting --message "$FAKE_AGENT_ASK"
fi
while true; do sleep 60; done
`

// FakeEditor stands in for VS Code's command line. The editor is a desktop application that a
// journey cannot drive or read, and the real command on a developer's machine would open real
// windows, so journeys configure this stand-in instead and never run "code". It records each
// start's arguments in FAKE_EDITOR_LOG and returns at once, as the real command does.
const FakeEditor = `#!/bin/sh
echo "$*" >> "${FAKE_EDITOR_LOG:-/dev/null}"
`

// EditorStarts is every start of the editor stand-in on this machine, one line of arguments
// each.
func (m *Machine) EditorStarts() []string {
	b, err := os.ReadFile(m.EditorLog)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// RemoveEditor uninstalls the editor stand-in, so this machine has no editor command at all.
func (m *Machine) RemoveEditor() {
	m.t.Helper()
	if err := os.Remove(filepath.Join(m.Bin, "fake-editor")); err != nil {
		m.t.Fatal(err)
	}
}

// editorSetting points the program at the editor stand-in, so no journey can start the real
// editor installed on the machine running the journeys.
const editorSetting = "[editor]\ncommand = \"fake-editor\"\n"

// NewRepo creates a git repository with one commit under this machine's home and returns its
// path. The folder name is the project name, so its session is named after it.
func (m *Machine) NewRepo(name string) string {
	m.t.Helper()
	path := filepath.Join(m.Home, "src", name)
	script := `mkdir -p "$1" && cd "$1" && git init -q -b main && echo readme > README.md && git add . && git -c user.email=dev@engine.example -c user.name=dev commit -qm init`
	if out, err := m.Run("sh", "-c", script, "sh", path); err != nil {
		m.t.Fatalf("creating repo %s: %v\n%s", name, err, out)
	}
	return path
}

// BeEngine configures this machine as the engine with the given projects (name, path pairs), using
// the agent stand-in.
func (m *Machine) BeEngine(projects ...string) {
	m.t.Helper()
	var b strings.Builder
	b.WriteString("role = \"engine\"\n[agent]\ncommand = \"fake-agent\"\n" + editorSetting)
	for i := 0; i+1 < len(projects); i += 2 {
		fmt.Fprintf(&b, "[[project]]\nname = %q\npath = %q\n", projects[i], projects[i+1])
	}
	m.WriteConfig(b.String())
}

// RememberRemoteControl sets this machine's remembered Remote Control choice in its
// configuration, as the interface would after the developer turned it on.
func (m *Machine) RememberRemoteControl() {
	m.t.Helper()
	b, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		m.t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "[agent]\n") {
		text += "[agent]\n"
	}
	m.write(m.ConfigPath(), strings.Replace(text, "[agent]\n", "[agent]\nremote_control = true\n", 1))
}

// StartSession creates a plain background session directly with Zellij, in dir, to set up a
// journey's starting state without going through the program under test.
func (m *Machine) StartSession(name, dir string) {
	m.t.Helper()
	cmd := m.Command("zellij", "attach", "--create-background", name, "options", "--default-cwd", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		m.t.Fatalf("creating session %s: %v\n%s", name, err, out)
	}
	m.WaitSession(name, "running")
}

// WaitSession waits until Zellij lists the session in the given state ("running" or "ended"),
// read from Zellij itself.
func (m *Machine) WaitSession(name, state string) {
	m.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if SessionState(m.Sessions(), name) == state {
			return
		}
		if time.Now().After(deadline) {
			m.t.Fatalf("session %s never became %s:\n%s", name, state, m.Sessions())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// SessionState reads one session's state from Zellij's plain session list: "running",
// "ended", or "" when it is not listed.
func SessionState(list, name string) string {
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != name {
			continue
		}
		if strings.Contains(line, "EXITED") {
			return "ended"
		}
		return "running"
	}
	return ""
}

// AgentStarts is every start of the agent stand-in on this machine, one line each:
// "start <dir> <args>".
func (m *Machine) AgentStarts() []string {
	b, err := os.ReadFile(m.AgentLog)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// AgentPrompts is every prompt the agent stand-in was started with on this machine, one line
// each: "prompt <dir> <prompt>". A start whose prompt Claude Code would not take as its prompt
// (one placed after --add-dir) has none here.
func (m *Machine) AgentPrompts() []string {
	b, err := os.ReadFile(m.AgentPromptLog)
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// AgentStartedIn reports whether the agent stand-in was ever started in dir. Paths are compared
// after resolving symbolic links, because macOS reports its temporary folder under /private.
func (m *Machine) AgentStartedIn(dir string) bool {
	want := resolve(dir)
	for _, line := range m.AgentStarts() {
		fields := strings.Fields(line)
		if len(fields) >= 2 && resolve(fields[1]) == want {
			return true
		}
	}
	return false
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// WaitSerialized waits until Zellij has written the session's layout to its cache, holding the
// given text, which is what lets an ended session be brought back. It looks only under this
// machine's home.
func (m *Machine) WaitSerialized(t testing.TB, name, contains string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		found := false
		_ = filepath.WalkDir(m.Home, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "session-layout.kdl" && filepath.Base(filepath.Dir(path)) == name {
				if b, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(b), contains) {
					found = true
					return filepath.SkipAll
				}
			}
			return nil
		})
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Zellij never saved session %s under %s", name, m.Home)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// KillServer ends a session abruptly, as a machine restart would: every process holding the
// session's socket in this run's own socket directory is killed. Processes are found through
// that socket, never by name, so nothing outside the run can be reached.
func (m *Machine) KillServer(t testing.TB, name string) {
	t.Helper()
	var socket string
	_ = filepath.WalkDir(m.Sockets, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name {
			socket = path
			return filepath.SkipAll
		}
		return nil
	})
	if socket == "" {
		t.Fatalf("no socket for session %s under %s", name, m.Sockets)
	}
	out, _ := exec.Command("lsof", "-t", socket).Output()
	pids := strings.Fields(string(out))
	if len(pids) == 0 {
		t.Fatalf("no process holds %s", socket)
	}
	for _, pid := range pids {
		_ = exec.Command("kill", "-9", pid).Run()
	}
}

// AddLocalProject registers a project in this machine's own configuration (a client's copy,
// for working locally), keeping the role and engine settings already there, and sets the agent
// stand-in as the agent here.
func (m *Machine) AddLocalProject(name, path string) {
	m.t.Helper()
	b, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		m.t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "[agent]") {
		text = strings.Replace(text, "[engine]", "[agent]\ncommand = \"fake-agent\"\n\n[engine]", 1)
	}
	text += fmt.Sprintf("\n[[project]]\nname = %q\npath = %q\n", name, path)
	m.WriteConfig(text)
}
