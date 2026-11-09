// Command engineroom runs Claude Code sessions on an engine machine inside Zellij and lets any client
// machine find, join and work on them, showing every command it runs.
//
// With no arguments it opens the terminal interface. Subcommands do the same things from a
// script, a shell or another program, and print JSON with --json where the result is data.
// Some subcommands exist for the program's own use across machines: a client asks the engine for
// its sessions and plans with `engineroom sessions --json` and `engineroom plan ... --json` over SSH.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/history"
	"github.com/KyleyHarris/engineroom/internal/mux"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/projects"
	"github.com/KyleyHarris/engineroom/internal/signal"
	"github.com/KyleyHarris/engineroom/internal/source"
	"github.com/KyleyHarris/engineroom/internal/stacklib"
	"github.com/KyleyHarris/engineroom/internal/tui"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

// built is the clone the binary was built from, set by scripts/install.sh with
// -ldflags "-X main.built=...". It lets a client tell, with git's own ancestry, whether the engine's
// build or its own is the older one (newerBuild); empty when it was built some other way.
var built = ""

func main() {
	// The program was zellij-claude-hub before (D-026): its own folders under that name are moved
	// to the new one the first time it runs, and said once. Here in main, never in run, so a test
	// that calls run in this process can never move a real machine's folders (one did, once, and
	// the installed build then found no configuration).
	for _, m := range paths.Migrate() {
		fmt.Fprintf(os.Stderr, "engineroom: moved %s (the program's new name)\n", m)
	}
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run dispatches a command line and returns the exit code, so it can be tested without
// exiting the test process.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	useMultiplexer()
	if len(args) == 0 {
		return runTUI(tui.Intent{}, stderr)
	}
	switch args[0] {
	case "open":
		// --no-join starts the session and leaves it running (SESS-NEW-004); --remote-control
		// turns Remote Control on for this one start, whatever this machine remembers.
		start, rest := source.ParseStartFlags(args[1:])
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "usage: engineroom open <project> [--no-join] [--remote-control]")
			return 2
		}
		return runTUI(tui.Intent{Open: rest[0], NoJoin: start.NoJoin, RemoteControl: start.RemoteControl}, stderr)
	case "new":
		start, rest := source.ParseStartFlags(args[1:])
		if len(rest) != 2 {
			fmt.Fprintln(stderr, "usage: engineroom new <project> <work> [--no-join] [--remote-control]")
			return 2
		}
		return runTUI(tui.Intent{NewProject: rest[0], NewWork: rest[1], NoJoin: start.NoJoin, RemoteControl: start.RemoteControl}, stderr)
	case "clean":
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintln(stderr, "usage: engineroom clean <project> <work>")
			return 2
		}
		intent := tui.Intent{CleanProject: args[1], Clean: true}
		if len(args) == 3 {
			intent.CleanWork = args[2]
		}
		return runTUI(intent, stderr)
	case "edit":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: engineroom edit <session>")
			return 2
		}
		return runTUI(tui.Intent{Edit: args[1]}, stderr)
	case "switch":
		if os.Getenv("ZELLIJ") == "" && mux.Current() == "" {
			fmt.Fprintln(stderr, "engineroom switch moves the current window to another session, so it only works inside a session.")
			return 2
		}
		return runTUI(tui.Intent{Switch: true}, stderr)
	case "clone":
		// Clone one of a root's owner's repositories into the root and register it (PROJ-CLONE).
		opt, rest, ok := newRepoFlags(args[1:], stderr)
		if !ok || len(rest) != 2 || opt.public {
			fmt.Fprintln(stderr, "usage: engineroom clone <root> <repository> [--category <category>]")
			return 2
		}
		return runTUI(tui.Intent{Clone: &tui.RepoRequest{Root: rest[0], Name: rest[1], Category: opt.category}}, stderr)
	case "create":
		// Create a repository in a root and on GitHub. It publishes something, so its commands
		// are shown and wait for enter (PROJ-CREATE).
		opt, rest, ok := newRepoFlags(args[1:], stderr)
		if !ok || len(rest) != 2 {
			fmt.Fprintln(stderr, "usage: engineroom create <root> <name> [--category <category>] [--public]")
			return 2
		}
		return runTUI(tui.Intent{Create: &tui.RepoRequest{Root: rest[0], Name: rest[1], Category: opt.category, Public: opt.public}}, stderr)
	case "up", "down":
		return runStack(args[0], args[1:], stdout, stderr)
	case "task":
		return runTask(args[1:], stdout, stderr)
	case "stack-lib":
		// The stack helper an up or down script sources: `. "$(engineroom stack-lib)"`.
		path, err := stacklib.Path()
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, path)
		return 0
	case "slot":
		return runSlot(args[1:], stdout, stderr)
	case "ports":
		return runPorts(args[1:], stdout, stderr)
	case "forward":
		return runForward(args[1:], stdout, stderr)
	case "mirror":
		return runMirror(stdout, stderr)
	case "clipboard":
		return runClipboard(args[1:], stdout, stderr)
	case "assistant":
		return runAssistant(args[1:], stdout, stderr)
	case "upgrade":
		return runUpgrade(args[1:], stdout, stderr)
	case "app":
		return runApp(args[1:], stdout, stderr)
	case "secrets":
		return runSecrets(args[1:], stdout, stderr)
	case "settings":
		return runSettings(args[1:], stdout, stderr)
	case "roles":
		return runRoles(args[1:], stdout, stderr)
	case "offline":
		return runOffline(args[1:], stdout, stderr)
	case "handoff":
		return runHandoff(args[1:], stdout, stderr)
	case "api":
		return runAPI(args[1:], stdout, stderr)
	case "join":
		// --here joins a session on this machine while a client works on the engine: one started
		// while it was offline (CONN-HANDOFF-006).
		here := len(args) == 3 && args[2] == "--here"
		if len(args) != 2 && !here {
			fmt.Fprintln(stderr, "usage: engineroom join <session> [--here]")
			return 2
		}
		return runTUI(tui.Intent{Join: args[1], JoinHere: here}, stderr)
	case "roots":
		return runRoots(args[1:], stdout, stderr)
	case "root":
		return runRoot(args[1:], stdout, stderr)
	case "browse":
		return runBrowse(args[1:], stdout, stderr)
	case "gh":
		return runGH(args[1:], stdin, stdout, stderr)
	case "sessions":
		return runSessions(args[1:], stdout, stderr)
	case "projects":
		return runProjects(args[1:], stdout, stderr)
	case "project":
		return runProject(args[1:], stdout, stderr)
	case "plan":
		return runPlan(args[1:], stdout, stderr)
	case "layout":
		return runLayout(args[1:], stdout, stderr)
	case "signal":
		return runSignal(args[1:], stdin, stderr)
	case "history":
		return runHistory(args[1:], stdout, stderr)
	case "sweep":
		return runSweep(args[1:], stdout, stderr)
	case "process":
		return runProcess(args[1:], stdout, stderr)
	case "session":
		return runSession(args[1:], stdout, stderr)
	case "rex":
		return runRex(args[1:], stdout, stderr)
	case "role":
		return runRole(args[1:], stdout, stderr)
	case "version", "--version":
		// --clone is the clone this build was made from (scripts/install.sh records it), where
		// its docs and scripts are; empty for a build that recorded none.
		if len(args) == 2 && args[1] == "--clone" {
			fmt.Fprintln(stdout, built)
			return 0
		}
		fmt.Fprintln(stdout, "engineroom", version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "engineroom: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  engineroom                                   the session list
  engineroom open <project> [--no-join] [--remote-control]
                                         open a project's session (creating it if needed) and join
                                         it; --no-join starts it and leaves it running;
                                         --remote-control starts the agent with Remote Control this
                                         once (the remembered choice is [agent] remote_control)
  engineroom new <project> <work> [--no-join] [--remote-control]
                                         start new work: a worktree beside the main clone on a new
                                         branch, the repository's create script, a session in it
  engineroom clean <project> <work>            clean up finished work: stop its session, run the repository's
                                         teardown script, remove the worktree, delete the branch if merged
  engineroom roles <project>                   the worktrees kept for a role (designer, tester), not one branch
  engineroom roles add|remove <project> <role> record a role in your settings, or forget it (the app, and
                                         "engineroom api run new-role", do this as they make one)
  engineroom up [<session>]                    build and run everything a session's worktree needs, with
                                         its up script (inside a session: this one); scripts must be
                                         safe to run again
  engineroom down [<session>]                  stop it with the down script
  engineroom stack-lib                         the path of the stack helper an up or down script sources:
                                               . "$(engineroom stack-lib)", then deps, start and stop
  engineroom slot claim|release [--worktree <dir>]
                                         give a worktree its own slot of its project's ports and names
                                         and write its record, or give the slot back
  engineroom slot status|json|env [--worktree <dir>]  |  engineroom slot get <key>
                                         read a worktree's slot (status --hook: for a SessionStart hook)
  engineroom slot ignore [--worktree <dir>]    add the slot's files git does not ignore yet to the
                                         repository's .gitignore (then commit it)
  engineroom ports                             every project's reserved ports and slots
  engineroom ports check                       clashes across the projects, and reserved ports in use now
  engineroom forward [--check|--watch [seconds]]
                                         on a client: reach the ports of the engine's running sessions
                                         here, same numbers; --check only over the connection already
                                         open; --watch follows the engine's sessions over it as they
                                         start and stop
  engineroom forward on|off|status             the remembered switch (off closes every forward), or what
                                         is forwarded now
  engineroom forward add|remove <session>      one session's switch: forward its ports here, or stop
  engineroom forward skip|unskip <session>     leave one session's ports out, or put them back
  engineroom settings where <project>          the folder of your own settings for a project (its engineroom.toml
                                         and scripts), which decide over the repository's .engineroom/
  engineroom settings status                   your settings as a repository: where they sync, what is
                                         not sent, whether their secrets are readable here
  engineroom settings init [<repository>]      keep your settings in a private repository, with every
                                         secrets/ folder encrypted by git-crypt
  engineroom settings sync                     commit this machine's settings, take the others', send
  engineroom secrets status|save|restore <project> [--worktree <dir>]
                                         a project's secret files (engineroom.toml's secrets list) compared
                                         with, saved to, or put back from your settings
  engineroom settings clone <repository> [<key file>]
                                         bring your settings to this machine and unlock them
  engineroom ports reserve <project> [--write] reserve a block of 100 ports from 20000-29999 for a project:
                                         10 slots of 10, slot 1 keeping its own numbers
  engineroom edit <session>                    open a session's worktree in VS Code on this machine (on a
                                         client, the engine's folder over SSH)
  engineroom switch                            inside a session: move this window to another session
  engineroom sessions [--json] [--github]      list the sessions on this machine; --github adds each branch on GitHub
  engineroom sessions --follow                 the list as JSON now and on each change (a client's forwarder)
  engineroom sweep [--dry-run]                 stop what still runs for a checkout whose session has been gone
                                         two minutes, and forget ended sessions (D-020); silent if none
  engineroom sweep --watch                     sweep every minute while the app that started it runs (the
                                         app starts it on the engine, and on a client while working local)
  engineroom sweep --uninstall                 remove the launchd job an earlier build installed
  engineroom session reap <name> [--dry-run]   stop the servers still running for a session that has ended
  engineroom process stop <pid> --port <n>     stop one process, only while it still listens on that port
  engineroom projects [--json [--detail]]      list the registered projects (--detail: with their category)
  engineroom project add <folder> [--name <n>] [--category <c>]
                                         register a git repository as a project, listed under the
                                         category; again on a registered one, set its category
  engineroom project remove <name>             forget a project (nothing on disk changes)
  engineroom project here [<folder>]           exit 0 and print the project when the folder (default: here)
                                         is in a registered project's main clone or a worktree of it
  engineroom root add <folder> --owner <o> [--account <a>] [--name <n>]
                                         register a folder for one GitHub owner's repositories, listed
                                         with the gh account <a> (default: gh's active account)
  engineroom roots [--json]                    list the roots
  engineroom root remove <name>                forget a root (nothing on disk changes)
  engineroom browse <root> [--json]            list the owner's repositories on GitHub, marking those here
  engineroom clone <root> <repository> [--category <c>]
                                         clone one into <root>/<repository> and register it
  engineroom create <root> <name> [--category <c>] [--public]
                                         make <root>/<name> with a first commit, create it on GitHub
                                         (private unless --public), push, and register it
  engineroom gh --account <a> <gh arguments>   run gh as the account <a>, without switching gh's active one
  engineroom join <session> [--here]           join a session by its name (brings an ended one back);
                                         --here: the one on this machine, on a client working on the engine
  engineroom offline on|off|status             on a client: work on this machine as though it were the engine,
                                         until switched off (remembered); each switch shows the handoff
  engineroom handoff to-engine|from-engine     on a client: show the work one machine holds that the other
                                         does not, and the git commands that carry it across GitHub
  engineroom handoff release                   on the engine: push the work checked out to GitHub and name what is
                                         uncommitted, before a client works local
  engineroom task [<session>] <name>           run one of a project's own tasks ([[task]] in its engineroom.toml) in
                                         a session's worktree; engineroom task list names them
  engineroom clipboard send                    on a client: put this Mac's clipboard image on the engine's, so
                                         pasting in a session there pastes it
  engineroom mirror                            on a client: copy here the engine's projects this machine
                                         lacks, and fetch the rest; moves no worktree
  engineroom api ...                           JSON for the menu bar app (see engineroom api help)
  engineroom role set engine                   this machine is the engine
  engineroom role set client --engine <dest>   this machine works on the engine at <dest> (an SSH destination)
  engineroom role show                         print this machine's role
  engineroom signal waiting --message <text>   (for the agent's hooks) this session is waiting for the developer
  engineroom signal waiting --from-hook        the same, reading the hook's JSON from standard input
  engineroom signal clear                      (for the agent's hooks) the developer answered
  engineroom history [--json]                  every action run on this machine, newest first
  engineroom history commands <id>             one action's commands, ready to paste into a shell
  engineroom version [--clone]                 print the version, or the clone it was built from
  engineroom upgrade prepare                   (for the app's Upgrade) write the upgrade session's brief and layout
  engineroom app relaunch                      quit the menu bar app and open it again, a moment after returning

Used between machines:
  engineroom plan open <project> [--remote-control] [--no-join] --json
                                         the commands that would open a project here
  engineroom plan new <project> <work> [--remote-control] [--no-join] --json
  engineroom plan clean <project> <work> --json
  engineroom plan join|back|stop|switch|clean-session <session> --json
  engineroom plan up|down <session> --json
  engineroom plan clone <root> <repository> [--category <c>] --json
  engineroom plan create <root> <name> [--category <c>] [--public] --json
  engineroom handoff state --json             this machine's projects against GitHub, for a client's handoff
  engineroom layout write [--remote-control] --out <file>
                                         write the standard session layout
`)
}

// loadConfig reads the configuration, treating "never set up" as an empty configuration so
// the interface can say what is missing instead of failing.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNoConfig) {
		return cfg, nil
	}
	return cfg, err
}

func runTUI(intent tui.Intent, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	final, err := tea.NewProgram(tui.New(cfg, intent), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if m, ok := final.(tui.Model); ok {
		return m.ExitCode()
	}
	return 0
}

// engineConfig loads the configuration for a subcommand that acts on this machine's own sessions.
func engineConfig(stderr io.Writer) (*config.Config, bool) {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintln(stderr, "engineroom: this machine has no role yet (engineroom role set engine)")
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return nil, false
	}
	return cfg, true
}

func jsonFlag(args []string) (bool, []string) {
	var rest []string
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		rest = append(rest, a)
	}
	return asJSON, rest
}

func runSessions(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if len(rest) == 1 && rest[0] == "--follow" {
		return followSessionList(cfg, stdout, stderr)
	}
	// --github adds each running session's branch on GitHub, which only the app's panel shows
	// (APP-LIST-003); without it no git command beyond finding each checkout is run.
	list, err := ops.Sessions(cfg)
	if len(rest) == 1 && rest[0] == "--github" {
		list, err = ops.SessionsWithGitHub(cfg)
	}
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if asJSON {
		if list == nil {
			list = []ops.SessionView{}
		}
		return writeJSON(stdout, list)
	}
	for _, s := range list {
		state := "running"
		if s.Ended {
			state = "ended"
		} else if s.Waiting != signal.None {
			state = string(s.Waiting)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", s.Name, state, s.Project, s.Checkout)
	}
	return 0
}

// followInterval is how often `engineroom sessions --follow` looks at this machine's sessions. It is a
// local look (Zellij's list and the slot files), on the engine, which is plugged in; only a change
// is sent.
var followInterval = 3 * time.Second

// followSessionList is `engineroom sessions --follow`, for a client's forwarder: this machine's session
// list as one line of JSON now, and again each time a session starts or stops or its ports change,
// so the client hears of a change within seconds while nothing is sent in between. A change of
// waiting state alone is not sent: forwarding does not depend on it. It ends when the reader goes
// (the write fails), or when the list cannot be read.
func followSessionList(cfg *config.Config, stdout, stderr io.Writer) int {
	sent := ""
	for {
		list, err := ops.Sessions(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		if list == nil {
			list = []ops.SessionView{}
		}
		key := forwardKey(list)
		if key != sent {
			b, _ := json.Marshal(list)
			if _, err := fmt.Fprintf(stdout, "%s\n", b); err != nil {
				return 0
			}
			sent = key
		}
		time.Sleep(followInterval)
	}
}

// forwardKey is what forwarding depends on in a session list: each running session's name and
// ports.
func forwardKey(list []ops.SessionView) string {
	var b strings.Builder
	for _, s := range list {
		if s.Ended {
			continue
		}
		fmt.Fprintf(&b, "%s:", s.Name)
		for _, p := range s.Ports {
			fmt.Fprintf(&b, "%d,", p.Number)
		}
		b.WriteString(";")
	}
	return b.String()
}

func runProjects(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	detail := false
	for _, a := range rest {
		if a == "--detail" {
			detail = true
		}
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if asJSON && detail {
		// The list with each project's heading, in list order (PROJ-CAT-001). It is asked for
		// with a flag of its own so that plain --json stays the list of names an older client
		// reads, and an older engine, which ignores the flag, still answers with names a newer
		// client understands (PROJ-CAT-002).
		return writeJSON(stdout, ops.Projects(cfg))
	}
	names := cfg.ProjectNames()
	if asJSON {
		if names == nil {
			names = []string{}
		}
		return writeJSON(stdout, names)
	}
	for _, p := range cfg.Projects {
		if p.Category != "" {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.Name, p.Path, p.Category)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", p.Name, p.Path)
	}
	return 0
}

// runPlan prints the plan for an action on this machine. A client reads it over SSH and runs
// the commands itself, so the commands it shows are exactly what this machine would run.
// Errors are printed as JSON too, with exit 3, so the client can tell "the engine answered no"
// from "the engine did not answer".
func runPlan(args []string, stdout, stderr io.Writer) int {
	_, rest := jsonFlag(args)
	if len(rest) > 0 && (rest[0] == "clone" || rest[0] == "create" || rest[0] == "register") {
		return runRepoPlan(rest, stdout, stderr)
	}
	start, rest := source.ParseStartFlags(rest)
	if len(rest) == 1 && rest[0] == "upgrade" {
		// The engine's upgrade session, asked for by a client's "Upgrade the engine".
		cfg, ok := engineConfig(stderr)
		if !ok {
			return 1
		}
		p := upgradePlan(cfg, start.RemoteControl)
		p.Machine = "engine"
		return writeJSON(stdout, map[string]any{"plan": p})
	}
	// How many arguments each action takes after its name; clean takes one or two, any other
	// action not listed takes one (a session).
	takes := map[string]int{"new": 2, "assistant": 2, "task": 2, "serve": 2, "new-role": 3, "next-branch": 3, "save-work": 2, "put-aside": 2, "push-work": 2}
	want, listed := takes[rest0(rest)]
	if len(rest) < 2 || (listed && len(rest) != want+1) || (!listed && rest[0] != "clean" && len(rest) != 2) || (rest[0] == "clean" && len(rest) > 3) {
		fmt.Fprintln(stderr, "usage: engineroom plan open <project> | engineroom plan new <project> <work> | engineroom plan join|back|stop|restart|switch <session>")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	type reply struct {
		Plan          any                     `json:"plan,omitempty"`
		Error         string                  `json:"error,omitempty"`
		NotRegistered *ops.NotRegisteredError `json:"notRegistered,omitempty"`
		Offers        []action.Offer          `json:"offers,omitempty"`
	}
	var r reply
	var err error
	switch rest[0] {
	case "open":
		p, e := ops.Open(cfg, rest[1], start)
		p.Machine, r.Plan, err = "engine", p, e
	case "new":
		p, e := ops.NewWork(cfg, rest[1], rest[2], start)
		p.Machine, r.Plan, err = "engine", p, e
	case "clean":
		work := ""
		if len(rest) == 3 {
			work = rest[2]
		}
		p, e := ops.CleanWork(cfg, rest[1], work)
		p.Machine, r.Plan, err = "engine", p, e
	case "clean-session":
		p, e := ops.CleanSession(cfg, rest[1])
		p.Machine, r.Plan, err = "engine", p, e
	case "up", "down":
		p, e := ops.Stack(cfg, rest[0], rest[1], start)
		p.Machine, r.Plan, err = "engine", p, e
	case "serve":
		port, perr := strconv.Atoi(rest[2])
		if perr != nil {
			fmt.Fprintln(stderr, "usage: engineroom plan serve <session> <port>")
			return 2
		}
		p, e := ops.Serve(cfg, rest[1], port, start)
		p.Machine, r.Plan, err = "engine", p, e
	case "assistant":
		p, e := assistantPlan(cfg, rest[1], rest[2], start.RemoteControl)
		p.Machine, r.Plan, err = "engine", p, e
	case "task":
		p, e := ops.Task(cfg, rest[1], rest[2])
		p.Machine, r.Plan, err = "engine", p, e
	case "new-role":
		p, e := ops.NewRoleWork(cfg, rest[1], rest[2], rest[3], start)
		p.Machine, r.Plan, err = "engine", p, e
	case "next-branch":
		p, e := ops.NextBranch(cfg, rest[1], rest[2], rest[3])
		p.Machine, r.Plan, err = "engine", p, e
	case "save-work":
		p, e := ops.SaveWork(cfg, rest[1], rest[2])
		p.Machine, r.Plan, err = "engine", p, e
	case "put-aside":
		p, e := ops.PutAside(cfg, rest[1], rest[2])
		p.Machine, r.Plan, err = "engine", p, e
	case "push-work":
		p, e := ops.PushWork(cfg, rest[1], rest[2])
		p.Machine, r.Plan, err = "engine", p, e
	default:
		p, e := ops.OnSession(cfg, rest[0], rest[1], start)
		p.Machine, r.Plan, err = "engine", p, e
	}
	if err != nil {
		r.Plan = nil
		var nr *ops.NotRegisteredError
		if errors.As(err, &nr) {
			r.NotRegistered = nr
		}
		var refused *ops.RefusedError
		if errors.As(err, &refused) {
			r.Offers = refused.Offers
		}
		r.Error = err.Error()
		writeJSON(stdout, r)
		return 3
	}
	return writeJSON(stdout, r)
}

// rest0 is the first argument, or "" when there is none.
func rest0(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func runLayout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("layout write", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "file to write")
	remote := fs.Bool("remote-control", false, "start the agent with Remote Control under the session's name")
	if len(args) == 0 || args[0] != "write" {
		fmt.Fprintln(stderr, "usage: engineroom layout write [--remote-control] --out <file>")
		return 2
	}
	if err := fs.Parse(args[1:]); err != nil || *out == "" {
		fmt.Fprintln(stderr, "usage: engineroom layout write [--remote-control] --out <file>")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	// The clipboard shim beside the layouts, so the agent's copies reach the viewing terminal
	// (zj.ClipboardShim). Written each time, so a new engineroom brings its own.
	shims := filepath.Join(paths.StateDir(), "shims")
	if err := os.MkdirAll(shims, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(shims, "pbcopy"), []byte(zj.ClipboardShim), 0o755)
	}
	if err := os.WriteFile(*out, []byte(zj.Layout(cfg.Agent.Command, *remote, shims)), 0o644); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	// The same agent start as a script beside it, which a Rex session's claude window runs
	// (mux.AgentScript, D-029); Zellij reads the layout alone.
	script := mux.AgentScript(*out)
	if err := os.WriteFile(script, []byte(zj.Agent(cfg.Agent.Command, *remote, shims)+"\n"), 0o644); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "wrote", *out)
	fmt.Fprintln(stdout, "wrote", script)
	return 0
}

// runSignal records or clears "this session is waiting for the developer". It is called from
// the agent's hooks, so it always exits 0: a problem is reported on stderr and never fails or
// blocks the agent (sessions obligation O6).
func runSignal(args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: engineroom signal waiting --message <text> | --from-hook ; engineroom signal clear")
		return 0
	}
	fs := flag.NewFlagSet("signal", flag.ContinueOnError)
	fs.SetOutput(stderr)
	message := fs.String("message", "", "what the session is asking")
	fromHook := fs.Bool("from-hook", false, "read the message from the hook's JSON on standard input")
	session := fs.String("session", mux.Current(), "session name (default: the session this runs in)")
	if err := fs.Parse(args[1:]); err != nil {
		return 0
	}
	if *session == "" {
		fmt.Fprintln(stderr, "engineroom signal: not inside a session; nothing recorded")
		return 0
	}
	var err error
	switch args[0] {
	case "waiting":
		if *fromHook {
			var hook struct {
				Message string `json:"message"`
			}
			if derr := json.NewDecoder(stdin).Decode(&hook); derr == nil {
				*message = hook.Message
			}
		}
		if *message == "" {
			*message = "waiting for you"
		}
		err = signal.SetWaiting(*session, *message)
	case "clear":
		err = signal.Clear(*session)
	default:
		fmt.Fprintf(stderr, "engineroom signal: unknown %q\n", args[0])
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "engineroom signal: %v\n", err)
	}
	return 0
}

func writeJSON(w io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return 1
	}
	return 0
}

// runRole sets or shows this machine's role. Setting it keeps every other setting, including
// the registered projects.
func runRole(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: engineroom role set engine | engineroom role set client --engine <dest> | engineroom role show")
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	switch args[0] {
	case "show":
		switch cfg.Role {
		case "":
			fmt.Fprintln(stdout, "no role")
		case config.RoleClient:
			fmt.Fprintf(stdout, "client of %s\n", cfg.Engine.Host)
		default:
			fmt.Fprintln(stdout, cfg.Role)
		}
		return 0
	case "set":
	default:
		fmt.Fprintf(stderr, "engineroom role: unknown %q\n", args[0])
		return 2
	}
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: engineroom role set engine | engineroom role set client --engine <dest>")
		return 2
	}
	fs := flag.NewFlagSet("role set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	engine := fs.String("engine", "", "SSH destination of the engine (client only)")
	sshConfig := fs.String("ssh-config", "", "SSH configuration file to use instead of the default (client only)")
	if err := fs.Parse(args[2:]); err != nil {
		return 2
	}
	switch config.Role(args[1]) {
	case config.RoleEngine:
		cfg.Role = config.RoleEngine
	case config.RoleClient:
		if *engine == "" && cfg.Engine.Host == "" {
			fmt.Fprintln(stderr, "engineroom role set client: --engine <ssh destination> is required")
			return 2
		}
		cfg.Role = config.RoleClient
		if *engine != "" {
			cfg.Engine.Host = *engine
		}
		if *sshConfig != "" {
			cfg.Engine.SSHConfig = *sshConfig
		}
	default:
		fmt.Fprintf(stderr, "engineroom role set: %q is not engine or client\n", args[1])
		return 2
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(stderr, "engineroom: saving %s: %v\n", paths.ConfigFile(), err)
		return 1
	}
	fmt.Fprintf(stdout, "This machine is now the %s. Saved to %s\n", cfg.Role, paths.ConfigFile())
	return 0
}

func runHistory(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	if len(rest) == 2 && rest[0] == "commands" {
		e, ok, err := history.Find(rest[1])
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(stderr, "engineroom: no action %q in the history\n", rest[1])
			return 1
		}
		fmt.Fprint(stdout, tui.CommandsText(e))
		return 0
	}
	// --limit N keeps the newest N, so a reader that shows only the latest (the app shows 60) is
	// not handed the whole file every time it looks.
	limit := 0
	if len(rest) == 2 && rest[0] == "--limit" {
		n, err := strconv.Atoi(rest[1])
		if err != nil || n < 1 {
			fmt.Fprintln(stderr, "usage: engineroom history [--json] [--limit <n>]")
			return 2
		}
		limit, rest = n, nil
	}
	if len(rest) != 0 {
		fmt.Fprintln(stderr, "usage: engineroom history [--json] [--limit <n>] | engineroom history commands <id>")
		return 2
	}
	entries, bad, err := history.Read()
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if bad > 0 {
		fmt.Fprintf(stderr, "engineroom: %d damaged line(s) in %s were skipped\n", bad, history.File())
	}
	if asJSON {
		if entries == nil {
			entries = []history.Entry{}
		}
		return writeJSON(stdout, entries)
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s  %s  %s  %-11s %s\n", e.ID, e.Started.Local().Format("2006-01-02 15:04:05"), e.Machine, e.Status, e.Title)
		for _, c := range e.Commands {
			fmt.Fprintf(stdout, "    %-11s %s\n", c.Status, c.Display)
		}
	}
	return 0
}

// runProject registers and forgets projects. It changes only this machine's configuration.
func runProject(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: engineroom project add <folder> [--name <name>] [--category <category>] | engineroom project set <name> category <c>|pinned on|off | engineroom project remove <name> | engineroom project list | engineroom project here [<folder>]")
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	switch args[0] {
	case "list":
		return runProjects(args[1:], stdout, stderr)
	case "here":
		// Whether a folder (default: the current one) is in a registered project's main clone or
		// one of its worktrees, by its exit code, with the project's name printed: cheap enough
		// for a hook, one git call (the clone a folder belongs to is the parent of its shared .git).
		dir := "."
		if len(args) == 2 {
			dir = args[1]
		} else if len(args) > 2 {
			fmt.Fprintln(stderr, "usage: engineroom project here [<folder>]")
			return 2
		}
		if name, ok := ops.ProjectAt(cfg, dir); ok {
			fmt.Fprintln(stdout, name)
			return 0
		}
		return 1
	case "add":
		fs := flag.NewFlagSet("project add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "the project's name (default: the folder's name)")
		category := fs.String("category", "", "the category it is listed under")
		folder := ""
		rest := args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			folder, rest = rest[0], rest[1:]
		}
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if folder == "" && fs.NArg() > 0 {
			folder = fs.Arg(0)
		}
		if folder == "" {
			fmt.Fprintln(stderr, "usage: engineroom project add <folder> [--name <name>] [--category <category>]")
			return 2
		}
		r, err := projects.Add(cfg, folder, projects.Options{Name: *name, Category: *category})
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		switch {
		case r.Recategorised:
			fmt.Fprintf(stdout, "Already registered as %s: %s\nIts category is now %s. Nothing on disk was changed.\n", r.Name, r.Path, r.Category)
		case r.Already:
			fmt.Fprintf(stdout, "Already registered as %s: %s\n", r.Name, r.Path)
		case r.Category != "":
			fmt.Fprintf(stdout, "Registered as %s in %s: %s\n", r.Name, r.Category, r.Path)
		default:
			fmt.Fprintf(stdout, "Registered as %s: %s\n", r.Name, r.Path)
		}
		if len(r.Declared) == 0 {
			fmt.Fprintln(stdout, "It declares no setup scripts (.engineroom/create, .engineroom/up, .engineroom/down, .engineroom/teardown): new work gets a plain worktree and session. Your own scripts for it go in: engineroom settings where "+r.Name)
		} else {
			fmt.Fprintf(stdout, "It declares these setup scripts: %s.\n", strings.Join(r.Declared, ", "))
		}
		return 0
	case "set":
		if len(args) != 4 {
			fmt.Fprintln(stderr, "usage: engineroom project set <name> category <category>|pinned on|off")
			return 2
		}
		if err := projects.Set(cfg, args[1], args[2], args[3]); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s: %s is now %q.\n", args[1], args[2], args[3])
		return 0
	case "remove":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: engineroom project remove <name>")
			return 2
		}
		if err := projects.Remove(cfg, args[1]); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Forgot %s. Nothing on disk was changed.\n", args[1])
		return 0
	}
	fmt.Fprintf(stderr, "engineroom project: unknown %q\n", args[0])
	return 2
}

// endSession ends a session fully (D-015): stops it if it runs, then deletes what the backend kept
// of it (Zellij keeps an ended session; Rex keeps none), and returns once it is no longer listed.
// Zellij's own `delete-session --force` reports a failure when stopping a session leaves no
// snapshot to delete, so each step is taken only for the state the session is in, and the outcome
// is checked rather than the exit code believed. Each command it runs is printed, so the step's
// output shows it.
func endSession(name string, stderr io.Writer) int {
	backend := mux.Active()
	state := func() string {
		list, err := backend.List()
		if err != nil {
			return "unknown"
		}
		for _, s := range list {
			if s.Name == name {
				if s.Ended {
					return "ended"
				}
				return "running"
			}
		}
		return ""
	}
	run := func(argv ...string) {
		fmt.Fprintln(stderr, "+ "+strings.Join(argv, " "))
		if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil && len(out) > 0 {
			fmt.Fprint(stderr, string(out))
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		switch state() {
		case "":
			return 0
		case "running":
			run(backend.Stop(name)...)
		case "ended":
			if forget := backend.Forget(name); forget != nil {
				run(forget...)
			}
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stderr, "engineroom: session %s is still listed after 10 seconds (%s)\n", name, state())
			return 1
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// useMultiplexer makes the configured multiplexer the one this run uses (D-029), before any
// command reads a session. Without a readable configuration it stays Zellij, the default; a
// command that needs the configuration says why it could not read it.
func useMultiplexer() {
	kind, path := "", ""
	if cfg, err := config.Load(); err == nil {
		kind, path = cfg.Multiplexer, cfg.Rex.Path
	}
	if err := mux.Use(kind, path); err != nil {
		_ = mux.Use("", "")
	}
}

// runRex is `engineroom rex show <session> [--here]`: brings a session up in the Rex app on this
// machine (mux.Rex.ShowArgv), the step a join ends with when sessions are kept in Rex. On a
// client working on the engine, the app is first switched to the engine's Rex server, named by
// rex.host; --here, or a client working offline, shows one of this machine's own sessions. Each
// rex command is printed before it runs, and a failure is said by what Rex's exit status means.
func runRex(args []string, stdout, stderr io.Writer) int {
	here := len(args) == 3 && args[2] == "--here"
	if len(args) < 2 || args[0] != "show" || args[1] == "" || (len(args) == 3 && !here) || len(args) > 3 {
		fmt.Fprintln(stderr, "usage: engineroom rex show <session> [--here]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if cfg.Multiplexer != "rex" {
		fmt.Fprintln(stderr, `engineroom: this machine does not keep its sessions in Rex (set multiplexer = "rex" in its configuration)`)
		return 1
	}
	host := ""
	if cfg.Role == config.RoleClient && !cfg.Offline && !here {
		host = cfg.Rex.Host
		if host == "" {
			fmt.Fprintln(stderr, "engineroom: set rex.host to the label of the engine's Rex server among this machine's Rex hosts (added with: rex hosts add <label> <tailnet endpoint>)")
			return 1
		}
	}
	for _, argv := range (mux.Rex{Bin: cfg.Rex.Path}).ShowArgv(args[1], host) {
		fmt.Fprintln(stderr, "+ "+strings.Join(argv, " "))
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if len(out) > 0 {
			fmt.Fprint(stderr, string(out))
		}
		if err != nil {
			code := 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
			if why := mux.RexExit(code, host); why != "" {
				fmt.Fprintln(stderr, "engineroom: "+why)
			} else {
				fmt.Fprintf(stderr, "engineroom: %v\n", err)
			}
			return 1
		}
	}
	return 0
}

// runSession holds small session commands used inside plans. "wait" returns once Zellij lists
// the session as running and its server answers (zj.Answers), or fails after ten seconds, so a
// plan never joins a session that is not ready yet.
func runSession(args []string, stdout, stderr io.Writer) int {
	// reap stops what still runs for an ended session (D-020); --dry-run only says what.
	if len(args) >= 2 && args[0] == "reap" && (len(args) == 2 || (len(args) == 3 && args[2] == "--dry-run")) {
		cfg, ok := engineConfig(stderr)
		if !ok {
			return 1
		}
		if err := ops.Reap(cfg, args[1], len(args) == 3, stdout); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	}
	if len(args) != 2 || (args[0] != "wait" && args[0] != "end") {
		fmt.Fprintln(stderr, "usage: engineroom session wait|end|reap <name> [--dry-run]")
		return 2
	}
	if args[0] == "end" {
		return endSession(args[1], stderr)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, err := mux.Active().List()
		if err == nil {
			for _, s := range list {
				if s.Name == args[1] && !s.Ended && mux.Active().Ready(args[1]) {
					return 0
				}
			}
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stderr, "engineroom: session %s did not start within 10 seconds\n", args[1])
			return 1
		}
		time.Sleep(100 * time.Millisecond)
	}
}
