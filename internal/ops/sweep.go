package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/procs"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// SweepGrace is how long a checkout's session must have been gone before what was started for
// it is stopped (D-020): a session quit by mistake and joined again within it keeps its stack.
const SweepGrace = 2 * time.Minute

// Sweep is what `engineroom sweep` finds on this machine: the plans it would run now, and the
// sessions whose leftovers are waiting out the grace period.
type Sweep struct {
	Plans   []action.Plan
	Waiting map[string]time.Duration // session -> time left before it is acted on
}

// sweepState is when each session was first seen gone while something still ran for it, kept
// between runs so the grace period is measured from the first sighting.
type sweepState map[string]int64

func sweepStatePath() string { return filepath.Join(paths.StateDir(), "sweep.json") }

func loadSweepState() sweepState {
	s := sweepState{}
	if b, err := os.ReadFile(sweepStatePath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveSweepState(s sweepState) {
	if b, err := json.Marshal(s); err == nil {
		_ = os.MkdirAll(filepath.Dir(sweepStatePath()), 0o755)
		_ = os.WriteFile(sweepStatePath(), b, 0o644)
	}
}

// FindSweep looks at this machine's sessions and processes and works out what to do (D-020):
//   - an ended session of a registered checkout, or its Ask Claude session, is deleted: what
//     Zellij keeps of it is never used (D-015);
//   - a registered checkout whose session is gone while something started for it still runs
//     has that stopped, as Stop would (its down, then `engineroom session reap`), once it has been
//     gone for SweepGrace.
//
// Only sessions of registered checkouts are considered, and a process only by the session its
// environment names. Nothing else on the machine is looked at, let alone stopped. now is the
// time of this run; record says whether to keep the first sightings for the next run (a dry
// run does not).
func FindSweep(cfg *config.Config, now time.Time, record bool) (Sweep, error) {
	sw := Sweep{Waiting: map[string]time.Duration{}}
	sessions, err := zj.List()
	if err != nil {
		return sw, err
	}
	known := checkouts(cfg)
	running := map[string]bool{}
	for _, s := range sessions {
		if !s.Ended {
			running[s.Name] = true
			continue
		}
		_, isCheckout := known[s.Name]
		_, isAssistant := assistantOf(s.Name, known)
		if isCheckout || isAssistant {
			sw.Plans = append(sw.Plans, action.Plan{
				Title:   "Forget the ended session " + s.Name,
				Machine: "this machine",
				Commands: []action.Command{{
					Argv: []string{"zellij", "delete-session", s.Name},
					Why:  "delete what Zellij kept of a session that ended: it is never brought back (D-015)",
				}},
			})
		}
	}

	all, err := procs.List()
	if err != nil {
		return sw, err
	}
	orphaned := map[string]bool{}
	for owner := range servers(all, procs.Ancestors(all), sessionOfWorktree(cfg)) {
		if _, ok := known[owner]; ok && !running[owner] {
			orphaned[owner] = true
		}
	}

	state := loadSweepState()
	names := make([]string, 0, len(orphaned))
	for n := range orphaned {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		first, seen := state[name]
		if !seen {
			state[name] = now.Unix()
			sw.Waiting[name] = SweepGrace
			continue
		}
		if gone := now.Sub(time.Unix(first, 0)); gone < SweepGrace {
			sw.Waiting[name] = SweepGrace - gone
			continue
		}
		c := known[name]
		p, _ := cfg.Project(c.project)
		var cmds []action.Command
		if down, ok := script(p, c.path, c.path, workOf(p.Path, c.path), "down"); ok {
			if _, err := os.Stat(c.path); err == nil {
				cmds = append(cmds, down)
			}
		}
		sw.Plans = append(sw.Plans, action.Plan{
			Title:    "Stop what ran for " + name + ": its session has ended",
			Machine:  "this machine",
			Commands: append(cmds, reapCommand(name)),
		})
	}
	// A session that came back, or whose leftovers are gone, is forgotten, so a later end starts
	// its grace afresh.
	for n := range state {
		if !orphaned[n] {
			delete(state, n)
		}
	}
	if record {
		saveSweepState(state)
	}
	return sw, nil
}
