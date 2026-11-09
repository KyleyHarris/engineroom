package handoff

import (
	"fmt"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// Keeping a client ready (Mirror) copies a project it lacks whole, worktrees included; a handover
// copies the clone (mirrorClone) and makes each worktree as it puts the receiving machine in the
// giving machine's state.

// mirrorAll adds the commands that bring a project the receiving machine does not have: a clone at
// the same place under its home as on the giving machine, so both machines' project folders mirror
// each other, registered under the same name and category, with each of
// the giving machine's worktrees whose branch is on GitHub, a slot for each when the project has
// ports, and the project's secret files put in place from the developer's settings (D-014).
// Everything runs on the receiving machine after every push, so the branches are on GitHub by
// then.
//
// It returns the checkouts it makes on the receiving machine, folder to branch (empty for the
// clone, which is on GitHub's default branch), so uncommitted work can be carried into them.
func mirrorAll(plan *action.Plan, moves *[]action.Command, r Repo, from, to Side, pushed map[string]bool) map[string]string {
	made := map[string]string{}
	note := func(format string, args ...any) { plan.Notes = append(plan.Notes, fmt.Sprintf(format, args...)) }
	dest, ok := mirrorPath(r.Path, from.State.Home, to.State.Home)
	if !ok {
		note("%s is not registered on %s, and its folder %s on %s is not under the home folder, so there is no matching place to copy it to. Get it on %s with: engineroom project add <folder> --name %s", r.Project, to.Name, r.Path, from.Name, to.Name, r.Project)
		return made
	}
	add := func(why string, argv ...string) {
		*moves = append(*moves, to.Wrap(action.Command{Argv: argv, Why: fmt.Sprintf("on %s: %s", to.Name, why)}))
	}
	// A folder already at that place is used when it is a clone of the same repository (made by
	// hand, never registered), and left alone, with the reason, when it is anything else.
	if to.Folder != nil {
		if exists, origin := to.Folder(dest); exists {
			if origin == "" || !sameRemote(origin, r.Remote) {
				what := "is not a git repository"
				if origin != "" {
					what = "is a clone of " + origin
				}
				note("%s is not registered on %s, and %s there %s, not %s, so it is left alone. Move it aside, or register the right folder with: engineroom project add <folder> --name %s", r.Project, to.Name, dest, what, r.Remote, r.Project)
				return made
			}
			note("%s is not registered on %s, but %s there is already a clone of it: it is brought up to date and registered, with its worktrees and secret files.", r.Project, to.Name, dest)
			add("bring the clone already there up to date with GitHub", "git", "-C", dest, "fetch", "origin")
		} else {
			note("%s is not on %s: it is copied to %s, as on %s, with its worktrees and secret files.", r.Project, to.Name, dest, from.Name)
			add("copy "+r.Project+" from GitHub to the same place as on "+from.Name, "git", "clone", "-q", r.Remote, dest)
		}
	} else {
		note("%s is not on %s: it is copied to %s, as on %s, with its worktrees and secret files.", r.Project, to.Name, dest, from.Name)
		add("copy "+r.Project+" from GitHub to the same place as on "+from.Name, "git", "clone", "-q", r.Remote, dest)
	}
	register := []string{"engineroom", "project", "add", dest, "--name", r.Project}
	if r.Category != "" {
		register = append(register, "--category", r.Category)
	}
	add("register it under the same name", register...)
	made[dest] = ""
	onGitHub := map[string]bool{}
	for _, b := range r.Branches {
		if b.GitHub != "" || pushed[b.Name] {
			onGitHub[b.Name] = true
		}
	}
	var worktrees []string
	for _, c := range r.Checkouts {
		if c.Path == r.Path || c.Branch == "" {
			continue
		}
		wdest, ok := mirrorPath(c.Path, from.State.Home, to.State.Home)
		if !ok || !onGitHub[c.Branch] {
			note("%s: the worktree %s on %s is not copied, because its branch %s is not on GitHub or its folder is not under the home folder.", r.Project, c.Path, from.Name, c.Branch)
			continue
		}
		add("make the worktree for "+c.Branch+", as on "+from.Name, "git", "-C", dest, "worktree", "add", wdest, c.Branch)
		worktrees = append(worktrees, wdest)
		made[wdest] = c.Branch
	}
	add("put the project's secret files in place from your settings", "engineroom", "secrets", "restore", r.Project)
	for _, w := range worktrees {
		if r.Slots {
			add("give the worktree its own slot of ports", "engineroom", "slot", "claim", "--worktree", w)
		}
		add("put the secret files in the worktree too", "engineroom", "secrets", "restore", r.Project, "--worktree", w)
	}
	return made
}
