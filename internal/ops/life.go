package ops

import (
	"fmt"
	"io"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/procs"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// What runs for a session lives only as long as the session (docs/decisions.md D-020). A
// process belongs to the session its environment names (procs.Proc.Owner): ENGINEROOM_SESSION, set on
// everything engineroom runs for a checkout; the session of the checkout engineroom ran it for, for what
// was started before ENGINEROOM_SESSION existed; or ZELLIJ_SESSION_NAME, set on everything started
// inside a session. Nothing is ever matched by its name.

// sessionOfWorktree maps a checkout's folder to its session's name, for the registered
// projects' checkouts, so a process started for a checkout before ENGINEROOM_SESSION existed is still
// known by the folder engineroom ran it in.
func sessionOfWorktree(cfg *config.Config) func(string) string {
	byPath := map[string]string{}
	for name, c := range checkouts(cfg) {
		byPath[resolvePath(c.path)] = name
	}
	return func(w string) string { return byPath[resolvePath(w)] }
}

// Leftovers are the servers still running for the session called name (servers), leaving out
// engineroom itself and everything above it.
func Leftovers(cfg *config.Config, name string) ([]procs.Proc, error) {
	all, err := procs.List()
	if err != nil {
		return nil, err
	}
	return servers(all, procs.Ancestors(all), sessionOfWorktree(cfg))[name], nil
}

// servers groups, by the session their environment names, the servers started for a session:
// each process that listens on a TCP port, the processes of the same session that started it
// (npm, dotnet run), and those it started (a build worker). Only servers: an editor or an app
// opened from inside a session carries the session's name too, listens on nothing, and is the
// developer's to close, never the program's (VS Code opened from a session carries its name).
// keep is left out whatever it names.
func servers(all []procs.Proc, keep map[int]bool, of func(string) string) map[string][]procs.Proc {
	owner := map[int]string{}
	byPID := map[int]procs.Proc{}
	children := map[int][]int{}
	for _, p := range all {
		byPID[p.PID] = p
		children[p.PPID] = append(children[p.PPID], p.PID)
		if !keep[p.PID] {
			owner[p.PID] = p.Owner(of)
		}
	}
	chosen := map[int]bool{}
	var down func(pid int, name string)
	down = func(pid int, name string) {
		for _, c := range children[pid] {
			if owner[c] == name && !chosen[c] {
				chosen[c] = true
				down(c, name)
			}
		}
	}
	for _, p := range all {
		name := owner[p.PID]
		if name == "" || len(p.Ports) == 0 {
			continue
		}
		chosen[p.PID] = true
		down(p.PID, name)
		for up := p.PPID; owner[up] == name && !chosen[up]; up = byPID[up].PPID {
			chosen[up] = true
		}
	}
	out := map[string][]procs.Proc{}
	for _, p := range all {
		if chosen[p.PID] {
			out[owner[p.PID]] = append(out[owner[p.PID]], p)
		}
	}
	return out
}

// Reap stops every process still running for the session called name, once that session has
// ended, and says what it stopped. It refuses while the session runs: its own panes carry its
// name, and stopping them is ending the session, which is Stop's to do.
func Reap(cfg *config.Config, name string, dryRun bool, w io.Writer) error {
	if s, err := find(name); err == nil && s != nil && !s.Ended {
		return fmt.Errorf("%s is still running, so nothing started for it was stopped; stop the session itself", name)
	}
	left, err := Leftovers(cfg, name)
	if err != nil {
		return err
	}
	if len(left) == 0 {
		fmt.Fprintf(w, "Nothing started for %s is still running.\n", name)
		return nil
	}
	for _, p := range left {
		verb := "stopping"
		if dryRun {
			verb = "would stop"
		}
		fmt.Fprintf(w, "%s %s, started for %s\n", verb, p.Describe(), name)
	}
	if dryRun {
		return nil
	}
	if still := procs.Stop(left, 5*time.Second); len(still) > 0 {
		for _, p := range still {
			fmt.Fprintf(w, "could not stop %s\n", p.Describe())
		}
		return fmt.Errorf("%d process(es) started for %s are still running", len(still), name)
	}
	fmt.Fprintf(w, "Stopped %d process(es) started for %s.\n", len(left), name)
	return nil
}

// reapCommand is the plan's step that stops what still runs for an ended session, shown like
// every other command (`engineroom session reap <name>`).
func reapCommand(name string) action.Command {
	return action.Command{
		Argv:   []string{"engineroom", "session", "reap", name},
		Why:    "stop anything still running that was started for the session: its servers outlive it otherwise (D-020)",
		OnFail: "The session has ended, but something started for it could not be stopped; it is named above.",
	}
}

// endCheckoutSession is how a checkout's session is ended fully (Stop, clean up): the project's
// down script while the session is still there, then the session, then whatever still runs for
// it (D-020). A session that is not a registered checkout's is only ended.
func endCheckoutSession(cfg *config.Config, name string) []action.Command {
	c, ok := checkouts(cfg)[name]
	if !ok {
		return []action.Command{zj.End(name)}
	}
	var cmds []action.Command
	p, _ := cfg.Project(c.project)
	if down, ok := script(p, c.path, c.path, workOf(p.Path, c.path), "down"); ok {
		down.OnFail = "The down script failed, so the session was left running. Fix the script and stop the session again."
		cmds = append(cmds, down)
	}
	return append(cmds, zj.End(name), reapCommand(name))
}
