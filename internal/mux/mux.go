// Package mux is what keeps a machine's sessions and shows them (docs/decisions.md D-029): Zellij,
// joined through a terminal app, or Rex, whose app is the terminal (docs/rex.md). Everything that
// starts, lists, joins or ends a session goes through the active Backend, so the rest of the
// program is the same whichever keeps the sessions.
//
// Like the zj package it builds commands and reads lists; running a command is the action
// package's job, so every step the developer sees is the backend's real command.
package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// Session is one session as the backend lists it: its name, whether it ended (Zellij keeps an
// ended session to bring back; Rex keeps none), and whether this terminal is in it.
type Session = zj.Session

// Layout is a new session's starting point, as files engineroom writes before it is created:
// File is Zellij's layout (KDL), and Agent a script that starts the agent, for a backend that
// takes a command rather than a layout file (Rex). Both are written together (AgentScript).
// Terminal is whether the session has a terminal window beside the agent's: a checkout's session
// has one, an assistant or upgrade session does not (Zellij reads the same from File).
type Layout struct {
	File     string
	Agent    string
	Terminal bool
}

// Standard is a checkout's session from the standard layout written at kdl: the agent, and a
// terminal beside it.
func Standard(kdl string) Layout {
	return Layout{File: kdl, Agent: AgentScript(kdl), Terminal: true}
}

// AgentOnly is an assistant's or the upgrade's session from the layout written at kdl: the agent
// alone.
func AgentOnly(kdl string) Layout {
	return Layout{File: kdl, Agent: AgentScript(kdl)}
}

// AgentScript is where the agent's start script is written for the layout file kdl: beside it,
// with .sh for .kdl.
func AgentScript(kdl string) string {
	return strings.TrimSuffix(kdl, filepath.Ext(kdl)) + ".sh"
}

// SessionVar is the variable engineroom sets to the session's name in everything a session runs,
// whichever backend keeps it. Zellij also sets its own ZELLIJ_SESSION_NAME; Rex sets REX_SESSION
// to an ID, not a name, so engineroom's own is the one read.
const SessionVar = "ENGINEROOM_SESSION"

// Current is the session this process runs inside: engineroom's own variable, else Zellij's for
// a session started before engineroom set its own. "" outside any session.
func Current() string {
	if s := os.Getenv(SessionVar); s != "" {
		return s
	}
	return os.Getenv("ZELLIJ_SESSION_NAME")
}

// Backend is what keeps sessions on this machine.
type Backend interface {
	// Kind is the backend's name, as configured: "zellij" or "rex".
	Kind() string
	// List lists the sessions, running and ended.
	List() ([]Session, error)
	// Ready reports whether a session takes clients yet.
	Ready(name string) bool
	// Create starts a session named name in dir from the layout, in the background, with
	// ENGINEROOM_SESSION set to its name in everything it runs.
	Create(name, dir string, l Layout) []action.Command
	// Join shows the session to the developer: joins it in this terminal (Zellij, interactive),
	// or asks the Rex app on the machine the developer is at to show it (a Viewer command, run
	// there rather than on the engine).
	Join(name string) action.Command
	// InTerminal reports whether joining needs a terminal window to run in (Zellij), rather than
	// showing the session in the backend's own app (Rex).
	InTerminal() bool
	// Switch moves what is showing a session to another one.
	Switch(name string) action.Command
	// Stop stops a running session, as argv run by `engineroom session end`.
	Stop(name string) []string
	// Forget deletes what is kept of an ended session, as argv, or nil when nothing is kept.
	Forget(name string) []string
}

// active is the backend this process uses; Use sets it from the configuration.
var active Backend = Zellij{}

// Active is the backend this process uses.
func Active() Backend { return active }

// Use makes the configured backend active: kind is the configuration's multiplexer ("" or
// "zellij" for Zellij, "rex" for Rex), and rexPath where Rex's command line is when set.
func Use(kind, rexPath string) error {
	switch kind {
	case "", "zellij":
		active = Zellij{}
	case "rex":
		active = Rex{Bin: rexPath}
	default:
		return fmt.Errorf("multiplexer %q is not one of \"zellij\" or \"rex\"", kind)
	}
	return nil
}

// End is the step that ends a session fully, whichever backend keeps it: `engineroom session end`
// stops it if it runs, then deletes whatever the backend keeps of it, and returns once it is no
// longer listed (D-015).
func End(name string) action.Command {
	return action.Command{Argv: []string{"engineroom", "session", "end", name}, Why: "end the session fully: stop it, then delete whatever is kept of it"}
}
