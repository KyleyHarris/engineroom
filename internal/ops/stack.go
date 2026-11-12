package ops

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/netport"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/shell"
	"github.com/KyleyHarris/engineroom/internal/slots"
)

// NoScriptError is a stack request for a repository that does not declare the script. The tool
// never guesses how to start or stop a stack, so the developer is told what the script must do
// instead (WORK-STACK-004).
type NoScriptError struct {
	Project, Script string
}

func (e *NoScriptError) Error() string {
	does := "starts what the worktree needs to run and returns once it is running (servers in the background or a container)"
	if e.Script == "down" {
		does = "stops what the up script started"
	}
	return fmt.Sprintf("%s has no %s script, so nothing was run. Add an executable %s to the project's folder in your settings, or .engineroom/%s to the repository, that %s. It runs in the worktree with ENGINEROOM_PROJECT, ENGINEROOM_WORK, ENGINEROOM_MAIN and ENGINEROOM_WORKTREE set, and must be safe to run again (docs/decisions.md D-006, D-013).",
		e.Project, e.Script, e.Script, e.Script, does)
}

// Stack is the plan for starting ("up") or stopping ("down") the app stack of the checkout a
// session belongs to: the developer's own script from their settings, or else the repository's
// .engineroom/ script, run in that checkout (docs/decisions.md D-006, D-013; WORK-STACK-001,
// 002). A repository's script is read from the checkout, so a branch
// that changes it runs its own version.
//
// A running stack belongs to a running session (WORK-STACK-005): a client forwards the ports of
// running sessions only, so a stack started with no session ran on the engine and could not be
// reached from the client. So starting the stack of a checkout whose session is not
// running starts that session first, in the background with no window, with Remote Control as
// start says; the session's commands are the plan's first steps, so they are seen like any other.
// Stopping the stack never starts a session.
func Stack(cfg *config.Config, kind, session string, start Start) (action.Plan, error) {
	if kind != "up" && kind != "down" {
		return action.Plan{}, fmt.Errorf("unknown stack action %q", kind)
	}
	c, ok := checkouts(cfg)[session]
	if !ok {
		return action.Plan{}, fmt.Errorf("%q is not the session of a registered project's worktree", session)
	}
	p, _ := cfg.Project(c.project)
	cmd, ok := script(p, c.path, c.path, workOf(p.Path, c.path), kind)
	if !ok {
		return action.Plan{}, &NoScriptError{Project: p.Name, Script: kind}
	}
	if kind == "down" {
		cmd.OnFail = "The down script failed, so parts of the stack may still be running. Fix the script and stop the stack again."
		return action.Plan{Title: "Stop the stack of " + session, Commands: []action.Command{cmd}}, nil
	}
	cmd.OnFail = "The up script failed. Whatever it started before failing may still be running; fix the script and start the stack again, or stop it."
	existing, err := find(session)
	if err != nil {
		return action.Plan{}, err
	}
	if existing != nil && !existing.Ended {
		return action.Plan{Title: "Start the stack of " + session, Commands: []action.Command{cmd}}, nil
	}
	// openCheckout ends what Zellij kept of an ended session before creating it fresh (D-015).
	open, err := openCheckout(session, c.path, Start{RemoteControl: start.RemoteControl, NoJoin: true})
	if err != nil {
		return action.Plan{}, err
	}
	return action.Plan{Title: "Start the session and the stack of " + session, Commands: append(open, cmd)}, nil
}

// Serve is what opening one of a session's links needs first, so a link never opens on a stack
// that is not running: nothing when something
// already answers on the link's port on this machine, where the stack runs; otherwise the
// session's stack started, its session too when it is not running (Stack "up"). A project with no
// up script, or a session that is not a registered checkout's, needs nothing: the link is opened
// as it is, and a note says why nothing was started.
func Serve(cfg *config.Config, session string, port int, start Start) (action.Plan, error) {
	title := fmt.Sprintf("Open %s's app on port %d", session, port)
	if netport.Answers(port) {
		return action.Plan{Title: title, Notes: []string{fmt.Sprintf("Something answers on port %d already, so nothing is started.", port)}}, nil
	}
	p, err := Stack(cfg, "up", session, start)
	if err != nil {
		return action.Plan{Title: title, Notes: []string{fmt.Sprintf("Nothing answers on port %d, and the app is not started for it: %v.", port, err)}}, nil
	}
	p.Title = fmt.Sprintf("Start %s's app, then open it on port %d", session, port)
	p.Notes = append([]string{fmt.Sprintf("Nothing answers on port %d, so the app is started first; the link opens once it is up.", port)}, p.Notes...)
	return p, nil
}

// workOf is the piece of work a checkout holds: the end of a worktree's folder name after the
// main clone's, or empty for the main clone itself.
func workOf(main, dir string) string {
	if resolvePath(dir) == resolvePath(main) {
		return ""
	}
	return strings.TrimPrefix(filepath.Base(dir), filepath.Base(main)+"-")
}

// Tasks are the named tasks of the project a session's checkout belongs to.
func Tasks(cfg *config.Config, session string) ([]engineroomfile.Task, error) {
	c, ok := checkouts(cfg)[session]
	if !ok {
		return nil, fmt.Errorf("%q is not the session of a registered project's worktree", session)
	}
	p, _ := cfg.Project(c.project)
	sp, err := settings.Load(p.Name, p.Path, c.path)
	return sp.Tasks, err
}

// Task is the plan that runs one of a project's own named tasks in the checkout a session belongs
// to (engineroomfile.Task): by sh, in the checkout, with the same ENGINEROOM_ variables as its scripts and the
// slot's ports, so a task such as fetching a copy of the production database works in any
// worktree. A task's ./file runs from the folder its engineroom.toml is in (the developer's settings, or
// the repository's .engineroom).
func Task(cfg *config.Config, session, name string) (action.Plan, error) {
	c, ok := checkouts(cfg)[session]
	if !ok {
		return action.Plan{}, fmt.Errorf("%q is not the session of a registered project's worktree", session)
	}
	p, _ := cfg.Project(c.project)
	sp, err := settings.Load(p.Name, p.Path, c.path)
	if err != nil {
		return action.Plan{}, err
	}
	for _, t := range sp.Tasks {
		if !strings.EqualFold(t.Name, name) {
			continue
		}
		run := t.Run
		if rest, ok := strings.CutPrefix(run, "./"); ok {
			run = shell.Quote(filepath.Join(sp.TaskDir, rest))
		}
		// The slot's environment first, when the project has slots and the worktree holds one,
		// so the task reads its ports as the stack scripts do: from the file the project's own
		// record names, not a fixed name (PORT-SLOT-003). Exported (set -a), so a task that is a
		// file of its own sees them too.
		argv := []string{"env", "ENGINEROOM_PROJECT=" + p.Name, "ENGINEROOM_WORK=" + workOf(p.Path, c.path), "ENGINEROOM_MAIN=" + p.Path, "ENGINEROOM_WORKTREE=" + c.path, "ENGINEROOM_SESSION=" + session, "sh", "-c"}
		if sp.HasSlots() {
			argv = append(argv, slotEnvPrefix+run, t.Name, slots.EnvFile(sp.Slots.Record))
		} else {
			argv = append(argv, run)
		}
		cmd := action.Command{
			Argv:   argv,
			Dir:    c.path,
			Why:    t.About,
			OnFail: "The task " + t.Name + " failed; its output is above.",
		}
		return action.Plan{Title: t.Name + " in " + session, Commands: []action.Command{cmd}}, nil
	}
	var names []string
	for _, t := range sp.Tasks {
		names = append(names, t.Name)
	}
	return action.Plan{}, fmt.Errorf("%s has no task called %q (it has: %s)", p.Name, name, strings.Join(names, ", "))
}

// AppCheck is how the health view checks a checkout's app (docs/decisions.md D-037): its project's
// status script, run as up is run, in the checkout with the slot's values; and whether the project
// starts an app at all, which is having an up script.
type AppCheck struct {
	Project   string
	Status    action.Command
	HasStatus bool
	HasUp     bool
}

// StatusScriptHelp is what a status script must do, said to a project that starts an app and has
// none, as a missing up script is told what an up script must do (WORK-STACK-004).
const StatusScriptHelp = "Add an executable status script to the project's folder in your settings (engineroom settings where <project>), or .engineroom/status to the repository, that ends with success when everything the app needs is running and with a failure otherwise, printing what it found. It runs in the worktree with the same values as up, must change nothing, and must end within 10 seconds; with the stack helper it can be: . \"$(engineroom stack-lib)\"; running <each part up starts> (docs/decisions.md D-037)."

// Apps returns the app check of each named session that is a registered project's checkout; a
// session that is not one (an Ask Claude session, or one started outside engineroom) is left out.
// The checkouts are read once for every session, as each read asks git for every project's
// worktrees.
func Apps(cfg *config.Config, sessions []string) map[string]AppCheck {
	all := checkouts(cfg)
	out := map[string]AppCheck{}
	for _, name := range sessions {
		c, ok := all[name]
		if !ok {
			continue
		}
		p, _ := cfg.Project(c.project)
		a := AppCheck{Project: p.Name}
		a.Status, a.HasStatus = script(p, c.path, c.path, workOf(p.Path, c.path), "status")
		_, _, a.HasUp = settings.Script(p.Name, p.Path, c.path, "up")
		out[name] = a
	}
	return out
}
