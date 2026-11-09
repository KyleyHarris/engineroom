// Package action is how the program does anything: every action is a Plan, an ordered list of
// real commands, and the program runs exactly that list.
//
// This is the mechanism behind "the program never hides what it does" (CLAUDE.md). The list
// is what the developer can see before an action runs, what is shown while it runs, and what
// the history keeps; and because each command is shown by rendering the same argument list
// that runs, what is shown pastes back into a shell as the same command.
package action

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/shell"
)

// Command is one real command.
type Command struct {
	Argv []string `json:"argv"`
	// Dir is the directory the command runs in; empty means wherever the program runs.
	Dir string `json:"dir,omitempty"`
	// Interactive commands take over the terminal (joining a session). A plan holds at most
	// one, last, and it is run by the interface rather than captured.
	Interactive bool `json:"interactive,omitempty"`
	// Why says, in a few words, what the command is for. Shown beside it.
	Why string `json:"why,omitempty"`
	// OnFail, when set, is what the developer is told if this command fails: what state the
	// action has left behind and how to go on, so a partial action is never left unexplained.
	OnFail string `json:"onFail,omitempty"`
	// Viewer marks a command that runs on the machine the developer is at rather than on the
	// engine: showing a session in the Rex app there (D-029). A client runs it as it is, never over
	// SSH, and only in the one shape that does this (IsShow); any other is run on the engine.
	Viewer bool `json:"viewer,omitempty"`
}

// IsShow reports whether argv is `engineroom rex show <session>`, the one command an engine's plan
// may have a client run on its own machine (Viewer).
func IsShow(argv []string) bool {
	return len(argv) == 4 && argv[0] == "engineroom" && argv[1] == "rex" && argv[2] == "show" && argv[3] != ""
}

// Display is the command as text a person can paste into a shell: a cd into its directory
// when it has one, then the quoted arguments.
func (c Command) Display() string {
	if c.Dir != "" {
		return "cd " + shell.Quote(c.Dir) + " && " + shell.Join(c.Argv)
	}
	return shell.Join(c.Argv)
}

// Plan is one action: what it is called, which machine it acts on, and its commands in order.
type Plan struct {
	Title    string    `json:"title"`
	Machine  string    `json:"machine"`
	Commands []Command `json:"commands"`
	// Notes are what the developer should know before the commands run: what the action does
	// not do, and why. A handoff says here which worktrees it leaves alone (CONN-HANDOFF-003).
	Notes []string `json:"notes,omitempty"`
	// Offers are other actions the developer may want because of what this plan found, each run
	// only if they choose it (a release offers to commit a worktree's uncommitted work as
	// partial work). They are never part of this plan's commands.
	Offers []Offer `json:"offers,omitempty"`
	// Blocked marks a plan that cannot run as asked: it has no commands, and its notes say what
	// stands in the way. A step the plan would end with (a switch) is not added to it.
	Blocked bool `json:"blocked,omitempty"`
}

// Offer is an action a plan suggests: what it is called on screen, and the `engineroom api` action
// that does it.
type Offer struct {
	Label  string   `json:"label"`
	Action []string `json:"action"`
}

// Lines is every command of the plan as pasteable text, one per line, in order.
func (p Plan) Lines() []string {
	out := make([]string, len(p.Commands))
	for i, c := range p.Commands {
		out[i] = c.Display()
	}
	return out
}

// Remote is how a client reaches the engine.
type Remote struct {
	Host           string
	SSHConfig      string
	ConnectTimeout time.Duration
	// ControlPath, when set, is an open connection's control socket that commands go through,
	// with no new connection of their own (ssh connects afresh only if the socket is gone).
	ControlPath string
}

// sshArgs is the start of every ssh command to the engine. BatchMode means ssh never stops to ask
// for a password or a passphrase: an engine that cannot be reached with the configured key counts
// as not answering, rather than leaving the developer at a prompt they did not expect.
func (r Remote) sshArgs(tty bool) []string {
	args := []string{"ssh"}
	if r.SSHConfig != "" {
		args = append(args, "-F", r.SSHConfig)
	}
	secs := int(r.ConnectTimeout.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	// ServerAlive lets a connection to the engine notice within about 45 seconds that the network is
	// gone (a client asleep or moved), instead of hanging until the system gives up on it.
	args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout="+strconv.Itoa(secs), "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3")
	if r.ControlPath != "" {
		args = append(args, "-o", "ControlPath="+r.ControlPath, "-o", "ControlMaster=no")
	}
	if tty {
		args = append(args, "-t")
	}
	return append(args, r.Host)
}

// Wrap turns a command meant for the engine into the ssh command a client runs. The remote side
// receives one string, built by quoting the arguments, so a name with spaces or shell syntax
// arrives as the same single argument (connection obligation O8).
//
// A Viewer command in the shape IsShow allows is left as it is: it shows the session on the
// client's own screen, and run on the engine it would show it on the engine's.
func (r Remote) Wrap(c Command) Command {
	if c.Viewer && IsShow(c.Argv) {
		return c
	}
	remote := shell.Join(c.Argv)
	if c.Dir != "" {
		remote = "cd " + shell.Quote(c.Dir) + " && " + remote
	}
	argv := append(r.sshArgs(c.Interactive), "--", remote)
	return Command{Argv: argv, Interactive: c.Interactive, Why: c.Why, OnFail: c.OnFail}
}

// CopyArgs is scp with the same configuration and options as the engine's ssh, ready for a source
// and a destination, so a file reaches the engine the way its commands do.
func (r Remote) CopyArgs() []string {
	args := r.sshArgs(false)
	args[0] = "scp"
	return args[:len(args)-1]
}

// GitSSH is the ssh command git uses to reach the engine (core.sshCommand), with the same
// configuration and options as the engine's other commands; git adds the host itself.
func (r Remote) GitSSH() string {
	args := r.sshArgs(false)
	return shell.Join(args[:len(args)-1])
}

// GitURL is a folder on the engine as git addresses it over SSH (host:path).
func (r Remote) GitURL(path string) string { return r.Host + ":" + path }

// WrapPlan wraps every command of an engine plan for running from a client.
func (r Remote) WrapPlan(p Plan) Plan {
	out := Plan{Title: p.Title, Machine: "engine", Commands: make([]Command, len(p.Commands)), Notes: p.Notes}
	for i, c := range p.Commands {
		out.Commands[i] = r.Wrap(c)
	}
	return out
}

// Probe is the command that checks the engine answers: a connection that runs nothing.
func (r Remote) Probe() Command {
	return Command{Argv: append(r.sshArgs(false), "--", "true"), Why: "check the engine answers"}
}

// Engineroom is a command that runs this program on the engine, used by a client to read state there.
func (r Remote) Engineroom(args ...string) Command {
	return r.Wrap(Command{Argv: append([]string{"engineroom"}, args...)})
}

// Validate refuses a plan that could not be run as shown: an empty command, or an interactive
// command anywhere but last.
func (p Plan) Validate() error {
	for i, c := range p.Commands {
		if len(c.Argv) == 0 || strings.TrimSpace(c.Argv[0]) == "" {
			return fmt.Errorf("plan %q: command %d is empty", p.Title, i+1)
		}
		if c.Interactive && i != len(p.Commands)-1 {
			return fmt.Errorf("plan %q: the interactive command %d is not last", p.Title, i+1)
		}
	}
	return nil
}
