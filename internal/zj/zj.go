// Package zj knows how Zellij is driven: the commands that create, join, stop, bring back and
// switch sessions, how its session list reads, and the standard layout a new session gets.
//
// It builds commands and parses output; it never runs anything itself. Running is the action
// package's job, so every Zellij command the program uses passes through a plan the developer
// can see.
package zj

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/desktop"
	"github.com/KyleyHarris/engineroom/internal/shell"
)

// Session is one line of Zellij's session list.
type Session struct {
	Name    string `json:"name"`
	Ended   bool   `json:"ended"`
	Current bool   `json:"current"`
	Created string `json:"created"`
}

// ListCommand reads the session list.
func ListCommand() []string {
	return []string{"zellij", "list-sessions", "--no-formatting"}
}

// Answers reports whether a session's server answers a client: it lists the session's tabs. Zellij
// lists a new session as soon as its socket exists, before its server takes clients, and joining
// it then fails with "No session with the name ... found!" (seen on a loaded machine, where the gap
// is longest). A session that does not answer says it was not found, with a list of the others,
// and still exits 0, so the answer is read rather than the exit status.
func Answers(name string) bool {
	out, err := exec.Command("zellij", "--session", name, "action", "query-tab-names").CombinedOutput()
	text := strings.TrimSpace(string(out))
	return err == nil && text != "" && !strings.Contains(text, "not found")
}

// List runs Zellij's session list on this machine and parses it. "No active zellij sessions
// found" is an empty list, not an error. Every action on sessions reads this list first, so a
// Zellij that is missing or older than MinVersion is refused here, with what is needed (sessions
// obligation, dependency down).
func List() ([]Session, error) {
	if err := CheckVersion(); err != nil {
		return nil, err
	}
	out, err := exec.Command(ListCommand()[0], ListCommand()[1:]...).CombinedOutput()
	text := string(out)
	if err != nil {
		if strings.Contains(text, "No active zellij sessions") {
			return nil, nil
		}
		return nil, fmt.Errorf("zellij list-sessions: %v: %s", err, strings.TrimSpace(text))
	}
	return Parse(text), nil
}

// MinVersion is the oldest Zellij the program works with, as docs/setup.md states it. The
// newest command the program uses, `zellij action switch-session`, arrived in 0.44.0.
var MinVersion = [3]int{0, 45, 0}

// versionChecked is set once this process has found a Zellij new enough; a failed check is made
// again at the next read, so installing or updating Zellij is seen without restarting a program
// that keeps running (the app's feed).
var versionChecked atomic.Bool

// versionRule reads "zellij 0.45.1" as Zellij prints it for --version.
var versionRule = regexp.MustCompile(`^zellij (\d+)\.(\d+)\.(\d+)`)

// CheckVersion refuses a Zellij that is not installed or is older than MinVersion, saying which
// version is needed and how to get it. Once a Zellij new enough is found, this process does not ask
// again (versionChecked).
func CheckVersion() error {
	if versionChecked.Load() {
		return nil
	}
	if _, err := Version(); err != nil {
		return err
	}
	versionChecked.Store(true)
	return nil
}

// Version asks Zellij for its version every time it is called, and refuses one that is not
// installed or is older than MinVersion, as CheckVersion does; the setup check calls it directly,
// so what it reports is what is installed now. A version it cannot read is not refused: the check
// stands guard against an old Zellij, and an answer in an unknown form says nothing about its age.
// It returns what Zellij printed.
func Version() (string, error) {
	need := NeedVersion()
	out, err := exec.Command("zellij", "--version").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("zellij is not installed or not on PATH; engineroom needs Zellij %s or later (brew install zellij)", need)
	}
	if err != nil {
		return "", fmt.Errorf("zellij --version: %v", err)
	}
	said := strings.TrimSpace(string(out))
	m := versionRule.FindStringSubmatch(said)
	if m != nil {
		var have [3]int
		for i := range have {
			have[i], _ = strconv.Atoi(m[i+1])
		}
		for i := range have {
			if have[i] != MinVersion[i] {
				if have[i] < MinVersion[i] {
					return said, fmt.Errorf("the installed Zellij is %s.%s.%s; engineroom needs Zellij %s or later (brew upgrade zellij)", m[1], m[2], m[3], need)
				}
				break
			}
		}
	}
	return said, nil
}

// NeedVersion is MinVersion as text, "0.45.0".
func NeedVersion() string {
	return fmt.Sprintf("%d.%d.%d", MinVersion[0], MinVersion[1], MinVersion[2])
}

// lineRule matches "name [Created 3s ago] (EXITED - attach to resurrect)" and its variants.
var lineRule = regexp.MustCompile(`^(\S+)\s+\[Created ([^\]]*)\](.*)$`)

// Parse reads Zellij's plain session list.
func Parse(text string) []Session {
	var out []Session
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		m := lineRule.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := m[3]
		out = append(out, Session{
			Name:    m[1],
			Created: strings.TrimSpace(m[2]),
			Ended:   strings.Contains(rest, "EXITED"),
			Current: strings.Contains(rest, "(current)"),
		})
	}
	return out
}

// MaxNameLength keeps session names inside the limit Zellij derives from its socket path: the
// socket folder plus the name must fit the operating system's limit for a socket address, 103
// bytes on macOS. On macOS the folder is $TMPDIR/zellij-<uid>/contract_version_1/, and $TMPDIR is
// /var/folders/<2>/<28>/T/ for every account, so the folder is 79 bytes and a name may be 24:
// measured on an engine with Zellij 0.45.1, 24 starts and 25 is refused ("the IPC socket path is too
// long (104 bytes, max 103)"). 36, the earlier limit, let a worktree such as
// a worktree named project-alpha-docs-viewer (25) be made and then fail to start its session. Longer names are
// shortened with a hash so they stay distinct (sessions obligation O9). One limit everywhere,
// rather than one measured per machine, keeps a checkout's session name the same on the engine and
// on a client working local. Zellij 0.46 names a socket by a short session id instead, so the
// limit no longer binds there; it stays, since the setup check accepts Zellij from 0.45.0, and a
// name that started with one version must start with the other.
const MaxNameLength = 24

var nameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// SessionName is the session name for a checkout folder: its base name, with anything outside
// letters, digits, '.', '_' and '-' turned into '-', and shortened deterministically when long.
func SessionName(folder string) string {
	base := folder
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	name := strings.Trim(nameUnsafe.ReplaceAllString(base, "-"), "-.")
	if name == "" {
		name = "session"
	}
	if len(name) > MaxNameLength {
		sum := sha256.Sum256([]byte(base))
		name = name[:MaxNameLength-7] + "-" + hex.EncodeToString(sum[:])[:6]
	}
	return name
}

// AgentSessionVars are the variables Claude Code sets on what it runs to mark it as started from
// inside one of its sessions: which session, its messaging socket and token, its entry point. A
// Zellij session's server keeps the environment of the command that started it and gives it to
// every pane, so a session started by engineroom from inside a Claude Code session (Claude running engineroom,
// or the app when it was opened from one) would give them to its own agent. That agent would then
// take itself for a child of the other session and save no transcript ("Transcript saving is off -
// inherited CLAUDE_CODE_CHILD_SESSION marker"). A session's agent is the developer's own,
// never a child of whatever started it, so a session is started without them. Only these are
// left out: any other CLAUDE_CODE_ setting is the developer's own choice and is kept.
var AgentSessionVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_CODE_BRIDGE_SESSION_ID",
}

// withoutAgentSession is argv run by env with AgentSessionVars unset, so the command and every
// process it starts (a session's server, and through it each pane) is free of them. It is part of
// the command itself, so the developer sees it wherever the command is shown.
func withoutAgentSession(argv ...string) []string {
	out := []string{"env"}
	for _, v := range AgentSessionVars {
		out = append(out, "-u", v)
	}
	return append(out, argv...)
}

// Create starts a session in the background from a layout, in a directory. The command runs
// from that directory as well as naming it: Zellij starts a background session's panes in the
// directory the command was run from, and --default-cwd alone does not reach them. It starts
// without another Claude Code session's markers (AgentSessionVars).
func Create(name, layout, dir string) action.Command {
	return action.Command{
		Argv: withoutAgentSession("zellij", "attach", "--create-background", name, "options", "--default-layout", layout, "--default-cwd", dir),
		Dir:  dir,
		Why:  "start the session in the background, without the markers of any Claude Code session it was started from",
	}
}

// StartOnce is the one command a plan uses to start a session: `engineroom session start`, which
// takes a lock held for that session's name, starts the session with the Start commands only when
// it is not already running (deleting what Zellij kept of an ended one first, D-015), and returns
// once it answers. Two starts of one session at once, from two machines or two windows, then make
// one session that both can join (sessions obligations O3, O4): Zellij's own background create,
// run twice at once for one name, can leave no session listed and a server running with no socket.
// Its description names the commands it runs, so what the plan shows is what runs.
func StartOnce(name, layout, dir string) action.Command {
	var inner []string
	for _, c := range Start(name, layout, dir) {
		inner = append(inner, c.Display())
	}
	return action.Command{
		Argv: []string{"engineroom", "session", "start", name, layout, dir},
		Why: "start the session once, under a lock for its name so two starts at once make one session, and wait until it answers; " +
			"unless it is already running, it runs: " + strings.Join(inner, " ; "),
		OnFail: "The session did not start; what the start said is above. Nothing else of the action ran after it.",
	}
}

// Start is the commands that start a session in the background from a layout, in a directory
// (Create). They are run by StartOnce, never put in a plan on their own. On a Mac where the program runs outside the developer's desktop login while one exists
// (over SSH, say), the start is handed to launchd as a one-time job in that login, so the session,
// and Claude Code and everything else in it, has the keychain and permissions the developer has at
// the screen (D-031): the job is written by `engineroom session job`, which shows it, then started
// with launchctl bootstrap, and removes itself once the start has returned.
func Start(name, layout, dir string) []action.Command {
	create := Create(name, layout, dir)
	domain, ok := desktop.Domain()
	if !ok {
		return []action.Command{create}
	}
	return []action.Command{
		{
			Argv: []string{"engineroom", "session", "job", name, layout, dir},
			Why:  "write the one-time job that starts the session in your desktop login: " + shell.Join(create.Argv),
		},
		{
			Argv:   []string{"launchctl", "bootstrap", domain, desktop.JobFile(name)},
			Why:    "start the session in your desktop login, with the keychain and permissions you have at the screen; the job removes itself once the session has started",
			OnFail: "launchd did not take the job, so the session was not started. If a job of the same name is still loaded, `launchctl bootout " + domain + "/" + desktop.Label(name) + "` removes it; then start the session again.",
		},
	}
}

// Attach joins a running session, taking over the terminal.
func Attach(name string) action.Command {
	return action.Command{Argv: []string{"zellij", "attach", name}, Interactive: true, Why: "join the session"}
}

// Resurrect brings back an ended session and runs its panes' commands straight away, so the
// agent picks up its conversation without waiting for a key press.
// Bringing it back starts its server again, so it too starts without another Claude Code
// session's markers (AgentSessionVars).
func Resurrect(name string) action.Command {
	return action.Command{Argv: withoutAgentSession("zellij", "attach", "--force-run-commands", name), Interactive: true, Why: "bring the session back and join it"}
}

// End stops a session if it is running and deletes what Zellij keeps of it, so nothing of it is
// left to be brought back with an old layout (D-015): a session is running, or it is not there.
func End(name string) action.Command {
	return action.Command{Argv: []string{"engineroom", "session", "end", name}, Why: "end the session fully: stop it, then delete what Zellij keeps of it (zellij kill-session, zellij delete-session)"}
}

// Switch moves the current Zellij window to another session; it only works from inside one.
func Switch(name string) action.Command {
	return action.Command{Argv: []string{"zellij", "action", "switch-session", name}, Why: "move this window to the other session"}
}

// kdlString quotes a value for a KDL string.
func kdlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

// Layout is the standard layout (SESS-NEW-001): the agent alone on the first tab, so it has the
// whole screen, continuing its last conversation in this folder when there is one; and a tab
// for git, using lazygit when it is installed, otherwise a shell with git status. There are no
// panes for a dev server, tests or logs: Zellij opens a tab whenever one is needed, and a
// repository starts its own stack.
//
// With remoteControl, the agent starts with Remote Control under the session's own name
// (SESS-REMOTE-002), and with the session's name as its display name (--name) too. A session that
// continues a conversation came back in the Claude app under that conversation's old name, not the
// session's, with --remote-control's name alone; --name sets the name the session shows. The name is read from ZELLIJ_SESSION_NAME, which
// Zellij sets in every pane, so one layout file serves every session.
//
// With shims, the agent runs with that folder first on its PATH: it holds a pbcopy that copies to
// the clipboard of the terminal the session is viewed through (ClipboardShim), so the agent's
// copies reach the Mac the developer is working on, over SSH too.
func Layout(agentCommand string, remoteControl bool, shims string) string {
	start := agentCommand
	if remoteControl {
		start += ` --name "$ZELLIJ_SESSION_NAME" --remote-control "$ZELLIJ_SESSION_NAME"`
	}
	// --continue comes first because the agent refuses it when this folder has no earlier
	// conversation; the plain start after || is what a first session gets.
	agent := agentCommand + " --continue" + strings.TrimPrefix(start, agentCommand) + " || " + start
	if shims != "" {
		agent = "PATH=" + shell.Quote(shims) + `:"$PATH"; export PATH; ` + agent
	}
	git := `if command -v lazygit >/dev/null 2>&1; then lazygit; else git status; fi; exec "${SHELL:-sh}"`
	return fmt.Sprintf(`layout {
    default_tab_template {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        children
        pane size=2 borderless=true {
            plugin location="zellij:status-bar"
        }
    }
    tab name="agent" focus=true {
        pane name="agent" focus=true command="sh" {
            args "-c" %s
        }
    }
    tab name="git" {
        pane name="git" command="sh" {
            args "-c" %s
        }
    }
}
`, kdlString(agent), kdlString(git))
}

// ClipboardShim is the pbcopy a session's agent finds first on its PATH (Layout). It puts the
// text on the clipboard of the terminal the session is viewed through, by OSC 52, which Zellij
// passes from the pane to every attached terminal: the Mac the developer is working on, over SSH
// too, where the real pbcopy would put it on the engine's clipboard instead. It also runs the real
// pbcopy when there is one, so a copy made while working on the engine itself lands there as before.
// Writing to the terminal is skipped quietly when there is none, and the copy never fails.
// ENGINEROOM_REAL_PBCOPY and ENGINEROOM_CLIPBOARD_TTY replace the real pbcopy and the terminal, for its test.
const ClipboardShim = `#!/bin/sh
# Written by engineroom (zj.ClipboardShim): pbcopy for a session's agent. Copies to the clipboard of the
# terminal the session is viewed through (OSC 52, passed on by Zellij), and to this machine's.
data=$(cat; printf x)
data=${data%x}
real=${ENGINEROOM_REAL_PBCOPY:-/usr/bin/pbcopy}
if [ -x "$real" ]; then printf '%s' "$data" | "$real" 2>/dev/null; fi
b64=$(printf '%s' "$data" | base64 | tr -d '\n')
{ printf '\033]52;c;%s\a' "$b64" > "${ENGINEROOM_CLIPBOARD_TTY:-/dev/tty}"; } 2>/dev/null
exit 0
`
