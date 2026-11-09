// Package editor builds the plan that opens a session's worktree in the code editor
// (acceptance/editor, docs/decisions.md D-007).
//
// The editor always runs on the machine the developer is at. On the engine, or on a client working
// locally, it opens the folder directly. On a client it opens the engine's folder through the
// editor's own SSH remote, so the engine's files and toolchain are used and nothing is copied; no
// command runs on the engine for it.
//
// The plan is built from the session as the list already shows it, so opening the editor never
// asks the engine anything the list did not already answer (editor obligation O9).
package editor

import (
	"errors"
	"fmt"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// ErrNoFolder is a session whose folder the program does not know.
var ErrNoFolder = errors.New("its folder is not known")

// NotFoundError is the editor's command missing on this machine.
type NotFoundError struct{ Command string }

func (e *NotFoundError) Error() string {
	// One line per sentence, each short enough for a normal terminal, so the fix is never
	// broken across a wrap.
	return fmt.Sprintf("The editor command %q was not found on this machine, so nothing was run.\n"+
		"To install VS Code's command, run \"Shell Command: Install 'code' command in PATH\" from VS Code's Command Palette.\n"+
		"To use another command, set command under [editor] in %s.", e.Command, paths.ConfigFile())
}

// Plan is the plan for opening session s's worktree in the editor run by command. remoteHost
// is the engine's SSH destination when the folder is on the engine and the developer is on a client,
// and empty when the folder is on this machine. lookPath finds the command on this machine
// (exec.LookPath outside tests).
//
// Every refusal happens here, before anything runs: a session with no registered folder, a
// folder the engine did not report, and a command that is not installed. A command known to be
// missing is never put in front of the developer as though it could run.
func Plan(command string, s ops.SessionView, remoteHost string, lookPath func(string) (string, error)) (action.Plan, error) {
	if command == "" {
		command = config.DefaultEditorCommand
	}
	if s.Project == "" {
		return action.Plan{}, fmt.Errorf("%q cannot be opened in the editor: %w because it is not in a registered project's worktree", s.Name, ErrNoFolder)
	}
	if s.Path == "" {
		// The session belongs to a project but its folder was not reported: an engine running an
		// older engineroom. Guessing the folder from the screen's ~/ form would open the wrong one.
		return action.Plan{}, fmt.Errorf("%q cannot be opened in the editor: %w because the engine did not report it; update engineroom on the engine", s.Name, ErrNoFolder)
	}
	if _, err := lookPath(command); err != nil {
		return action.Plan{}, &NotFoundError{Command: command}
	}
	c := action.Command{Argv: []string{command, s.Path}, Why: "open the worktree in the editor"}
	if remoteHost != "" {
		// One argument each, never a joined string, so a host or folder with spaces or shell
		// syntax reaches the editor unchanged (editor obligation O8).
		c = action.Command{
			Argv: []string{command, "--remote", "ssh-remote+" + remoteHost, s.Path},
			Why:  "open the engine's worktree in the editor, over SSH to " + remoteHost,
		}
	}
	return action.Plan{Title: "Open " + s.Name + " in the editor", Commands: []action.Command{c}}, nil
}
