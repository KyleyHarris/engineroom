package mux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// Rex keeps sessions in a Rex server and shows them in the Rex app, which is the terminal
// (docs/rex.md, D-029). Written from Rex's documented command line before Rex was run here: what
// the documentation leaves open is kept to one function (ShowArgv), and checked with
// scripts/rex-probe.sh once Rex is installed.
//
// A session is a Rex session labelled with engineroom's session name, with two windows: claude,
// running the layout's agent script, and terminal, a shell in the checkout. It is addressed by
// that label; two sessions sharing one make Rex refuse (exit 3) rather than guess, and engineroom
// never creates a second session under a name in use. Rex keeps no ended sessions: a session is
// running, or it is not there.
type Rex struct {
	// Bin is where `rex` is, from the configuration, when Rex keeps it off PATH.
	Bin string
}

// Kind implements Backend.
func (Rex) Kind() string { return "rex" }

// bin is the rex to run: the configured one, else the one Rex puts first on PATH in its own
// terminals (REX_BIN_DIR), else rex on PATH.
func (r Rex) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		if p := filepath.Join(dir, "rex"); isExecutable(p) {
			return p
		}
	}
	return "rex"
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// rexList is `rex ls --json`, as documented: {"sessions": [{"session_id": ..., "label": ...}]}.
type rexList struct {
	Sessions []struct {
		ID    string `json:"session_id"`
		Label string `json:"label"`
	} `json:"sessions"`
}

// ParseList reads `rex ls --json`: each session by its label, which is engineroom's session
// name. None is ended: Rex keeps no ended sessions.
func ParseList(out []byte) ([]Session, error) {
	var l rexList
	if err := json.Unmarshal(out, &l); err != nil {
		return nil, fmt.Errorf("reading rex ls --json: %w", err)
	}
	list := []Session{}
	for _, s := range l.Sessions {
		list = append(list, Session{Name: s.Label})
	}
	return list, nil
}

// List implements Backend. Exit 4 is Rex's "the server could not be reached".
func (r Rex) List() ([]Session, error) {
	out, err := exec.Command(r.bin(), "ls", "--json").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 4 {
			return nil, fmt.Errorf("the Rex server is not running here (rex ls: exit 4)")
		}
		if _, lookErr := exec.LookPath(r.bin()); lookErr != nil {
			return nil, fmt.Errorf("rex is not installed or not found; set rex.path in engineroom's configuration")
		}
		return nil, fmt.Errorf("rex ls: %v", err)
	}
	return ParseList(out)
}

// Ready implements Backend: Rex answers for the session (exit 0 from session inspect).
func (r Rex) Ready(name string) bool {
	return exec.Command(r.bin(), "session", "inspect", name, "--json").Run() == nil
}

// Create implements Backend: the session with its claude window running the layout's agent
// script, then, for a checkout, its terminal window, a shell in the checkout. Each runs through
// the login shell's environment with its interactive startup files (--shell, --interactive), as a
// terminal window would, because the Rex server's own environment is the app's, which may not have
// the agent on its PATH. Each starts without another Claude Code session's markers
// (zj.AgentSessionVars), and with ENGINEROOM_SESSION set to the session's name, which the waiting
// mark, the stack and process ownership read.
func (r Rex) Create(name, dir string, l Layout) []action.Command {
	shell := loginShell()
	env := []string{"env"}
	for _, v := range zj.AgentSessionVars {
		env = append(env, "-u", v)
	}
	env = append(env, SessionVar+"="+name)
	cmds := []action.Command{{
		Argv: append(append([]string{r.bin(), "session", "new", name, "--cwd", dir, "--window", "claude",
			"--shell", shell, "--interactive", "--json", "--"}, env...), "sh", l.Agent),
		Dir: dir,
		Why: "start the session in Rex, with Claude in its first window",
	}}
	if l.Terminal {
		cmds = append(cmds, action.Command{
			Argv: append(append([]string{r.bin(), "window", "new", "-s", name, "terminal", "--focus=false", "--cwd", dir,
				"--shell", shell, "--interactive", "--"}, env...), shell),
			Dir: dir,
			Why: "add its terminal window, a shell in the checkout",
		})
	}
	return cmds
}

// loginShell is the developer's shell, for Rex to start a window's command in its environment.
func loginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

// ShowArgv are the rex commands that bring a session up in the Rex app of the machine they run on:
// Go to Session (session.select). host, when set, is the label of the engine's Rex server among
// this machine's Rex hosts: the app is switched to it first (Switch Host, client.host.switch), and
// the session is then chosen there. The actions' names are documented; the names of their
// arguments are not, so this is the one place to correct them once scripts/rex-probe.sh has
// recorded them (`rex client inspect --json`). With exactly one app connected, Rex sends a client
// action to it.
func (r Rex) ShowArgv(name, host string) [][]string {
	if host == "" {
		return [][]string{{r.bin(), "do", "session.select", "session=" + name}}
	}
	return [][]string{
		{r.bin(), "do", "client.host.switch", "host=" + host},
		{r.bin(), "-S", host, "do", "session.select", "session=" + name},
	}
}

// Join implements Backend: `engineroom rex show`, run on the machine the developer is at (Viewer),
// asks the Rex app there to show the session, switching it to the engine's Rex first when that
// machine is a client (ShowArgv). It returns at once; nothing takes over a terminal.
func (r Rex) Join(name string) action.Command {
	return action.Command{
		Argv:   []string{"engineroom", "rex", "show", name},
		Viewer: true,
		Why:    "show the session in the Rex app on this machine",
	}
}

// InTerminal implements Backend: the Rex app is the terminal.
func (Rex) InTerminal() bool { return false }

// Switch implements Backend: in Rex, moving to another session is showing it.
func (r Rex) Switch(name string) action.Command { return r.Join(name) }

// Stop implements Backend.
func (r Rex) Stop(name string) []string { return []string{r.bin(), "session", "kill", name} }

// Forget implements Backend: Rex keeps nothing of a session once it is killed.
func (Rex) Forget(string) []string { return nil }

// RexExit is what one of Rex's documented exit statuses means, said for the developer (docs/rex.md,
// "Errors"); host is the engine's Rex host when the command went to it. "" for a status that has
// nothing to add to Rex's own message.
func RexExit(code int, host string) string {
	switch code {
	case 2:
		return "Rex refused the command line engineroom gave it: a defect in engineroom, please report it"
	case 3:
		return "Rex found no such session, or more than one by that name; nothing was changed"
	case 4:
		if host != "" {
			return "the engine's Rex server (" + host + ") did not answer: check Tailscale is up on both machines and the engine's Rex serves on the tailnet (rex server tailscale on)"
		}
		return "the Rex server on this machine did not answer: open the Rex app, which starts it"
	}
	return ""
}
