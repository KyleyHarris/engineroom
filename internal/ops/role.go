package ops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// Worktrees come in two kinds, chosen when one is made (docs/decisions.md D-024):
//
//   - branch work (NewWork): named for its branch, <repo>-<branch>, closed when the branch is done;
//   - a role (NewRoleWork): named for what it is for, <repo>-<role> (designer, tester, primary),
//     kept while branches come and go in it (NextBranch). Its folder, and so its session name and
//     its slot of ports, stay the same whichever branch it holds.
//
// A role is recorded in the developer's settings (settings.RolesFile), so every machine knows it.

// NewRoleWork is the plan for a worktree kept for role, holding branch: a new branch from the
// remote's main, or the branch as it is when it exists here or on the remote. Asked again for a
// role that already has its worktree, it opens that one.
func NewRoleWork(cfg *config.Config, project, role, branch string, start Start) (action.Plan, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return action.Plan{}, &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	for what, v := range map[string]string{"role": role, "branch": branch} {
		if !workName.MatchString(v) {
			return action.Plan{}, fmt.Errorf("the %s %q may only hold letters, digits, '.', '_' and '-', starting with a letter or digit", what, v)
		}
	}
	dir := WorktreePath(p.Path, role)
	name := zj.SessionName(dir)
	if isWorktree(p.Path, dir) {
		if !settings.HasRole(p.Name, p.Path, role) {
			return action.Plan{}, fmt.Errorf("%s is already a worktree for the branch work %s; choose another name for the role", dir, role)
		}
		cmds, err := openCheckout(name, dir, start)
		if err != nil {
			return action.Plan{}, err
		}
		return action.Plan{Title: fmt.Sprintf("Open the %s worktree of %s", role, p.Name), Commands: cmds}, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return action.Plan{}, fmt.Errorf("%s already exists and is not a worktree of %s; move it aside or choose another name", dir, p.Name)
	}
	if err := slotsLeft(p); err != nil {
		return action.Plan{}, fmt.Errorf("the %s worktree of %s was not made, because %w", role, p.Name, err)
	}

	plan := action.Plan{Title: fmt.Sprintf("New %s worktree on %s, on %s", role, p.Name, branch)}
	base, fetch := startPoint(p.Path)
	if fetch {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "fetch", "origin"}, Why: "bring the remote's branches up to date"})
	}
	add, ref := addBranch(p.Path, dir, branch, base)
	plan.Commands = append(plan.Commands, add)
	plan.Commands = append(plan.Commands, action.Command{
		Argv: []string{"engineroom", "roles", "add", p.Name, role},
		Why:  "record " + role + " as one of " + p.Name + "'s roles in your settings, so every machine keeps it as a role",
	})
	rest, err := setUpWorktree(p, ref, dir, role, name, start)
	if err != nil {
		return action.Plan{}, err
	}
	plan.Commands = append(plan.Commands, rest...)
	return plan, nil
}

// addBranch is the git command that makes the worktree at dir on branch, and the commit it starts
// from: the branch itself when this clone has it, a branch following the remote's when only the
// remote has it, otherwise a new branch from base. What the remote has is as last fetched.
func addBranch(main, dir, branch, base string) (action.Command, string) {
	switch {
	case hasRef(main, "refs/heads/"+branch):
		return action.Command{Argv: []string{"git", "-C", main, "worktree", "add", dir, branch}, Why: "create the worktree on the existing branch " + branch}, branch
	case hasRef(main, "refs/remotes/origin/"+branch):
		return action.Command{Argv: []string{"git", "-C", main, "worktree", "add", "--track", "-b", branch, dir, "origin/" + branch}, Why: "create the worktree on " + branch + ", following the remote's"}, "origin/" + branch
	default:
		return action.Command{Argv: []string{"git", "-C", main, "worktree", "add", "-b", branch, dir, base}, Why: "create the worktree on a new branch from " + base}, base
	}
}

func hasRef(main, ref string) bool {
	return exec.Command("git", "-C", main, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

// slotsLeft refuses new work before anything is made when the project's settings cannot be read,
// or when every slot of a project that has them is held, rather than leave a worktree with no
// ports of its own (PORT-SLOT-004). Settings that are refused (a file that is wrong, or a key that
// would overwrite another, D-028) stop the work with the refusal: read as "nothing declared",
// the worktree would be made with no slot and the refusal never seen (PORT-SET-002).
func slotsLeft(p config.Project) error {
	sp, err := settings.Load(p.Name, p.Path, p.Path)
	if err != nil {
		return err
	}
	if sp.HasSlots() {
		var all *slots.AllTaken
		if _, err := slots.NextFor(sp, p.Path); errors.As(err, &all) {
			return err
		}
	}
	return nil
}

// NextBranch is the plan for moving the role worktree of role on to branch once its last branch
// is done: the same folder, session and slot, holding the next piece of work. It refuses, before
// anything runs, when the move would lose work (nothingToLose), with what would make it safe.
func NextBranch(cfg *config.Config, project, role, branch string) (action.Plan, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return action.Plan{}, &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	if !workName.MatchString(branch) {
		return action.Plan{}, fmt.Errorf("the branch %q may only hold letters, digits, '.', '_' and '-', starting with a letter or digit", branch)
	}
	dir := WorktreePath(p.Path, role)
	if !isWorktree(p.Path, dir) || !settings.HasRole(p.Name, p.Path, role) {
		return action.Plan{}, fmt.Errorf("%s has no worktree kept for the role %s; a branch's worktree is closed when its branch is done, not moved on", p.Name, role)
	}
	current := strings.TrimSpace(gitOut(dir, "branch", "--show-current"))
	if current == branch {
		return action.Plan{}, fmt.Errorf("the %s worktree is already on %s", role, branch)
	}
	sp, spErr := settings.Load(p.Name, p.Path, dir)
	hasRemote, err := nothingToLose(p, dir, sp, spErr, "it was left on "+current)
	if err != nil {
		return action.Plan{}, err
	}
	plan := action.Plan{Title: fmt.Sprintf("Move the %s worktree of %s on to %s", role, p.Name, branch)}
	base := "main"
	if hasRemote {
		base, _ = startPoint(p.Path)
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "fetch", "origin"}, Why: "bring the remote's branches up to date"})
	}
	switch {
	case hasRef(p.Path, "refs/heads/"+branch):
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", dir, "switch", branch}, Why: "move on to the existing branch " + branch})
	case hasRef(p.Path, "refs/remotes/origin/"+branch):
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", dir, "switch", "-c", branch, "--track", "origin/" + branch}, Why: "move on to " + branch + ", following the remote's"})
	default:
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", dir, "switch", "-c", branch, base}, Why: "move on to a new branch " + branch + " from " + base})
	}
	plan.Commands[len(plan.Commands)-1].OnFail = "The worktree is still on " + current + ". git refuses a branch another worktree has out; choose another branch, or close that worktree first."
	plan.Notes = []string{"The session keeps running in the same folder; a stack started for " + current + " is still running and may need Run again."}
	return plan, nil
}

// workCheckout finds the worktree folder of a project that an action names, refusing the main
// clone: saving, putting aside and pushing are offered by Close and Next branch, which act on a
// worktree only.
func workCheckout(cfg *config.Config, project, dir string) (config.Project, string, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return config.Project{}, "", &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	if resolvePath(dir) == resolvePath(p.Path) || !isWorktree(p.Path, dir) {
		return config.Project{}, "", fmt.Errorf("%s is not a worktree of %s", dir, p.Name)
	}
	branch := strings.TrimSpace(gitOut(dir, "branch", "--show-current"))
	if branch == "" {
		return config.Project{}, "", fmt.Errorf("%s is on no branch, so there is no branch to save its work to", dir)
	}
	return p, branch, nil
}

// SaveWork is the plan for committing everything uncommitted in a worktree on its branch and
// pushing the branch, so the work is on the remote and the worktree can be closed or moved on.
func SaveWork(cfg *config.Config, project, dir string) (action.Plan, error) {
	p, branch, err := workCheckout(cfg, project, dir)
	if err != nil {
		return action.Plan{}, err
	}
	if len(gitLines(dir, "status", "--porcelain")) == 0 {
		return action.Plan{}, fmt.Errorf("%s has nothing uncommitted to save", filepath.Base(dir))
	}
	plan := action.Plan{Title: "Save the work in " + filepath.Base(dir) + " to " + branch, Commands: []action.Command{
		{Argv: []string{"git", "-C", dir, "add", "-A"}, Why: "take every change, new files included"},
		{Argv: []string{"git", "-C", dir, "commit", "-q", "-m", "Work in progress, saved before closing"}, Why: "commit it on " + branch},
	}}
	if _, hasRemote := remote(p.Path); hasRemote {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", dir, "push", "-u", "origin", branch}, Why: "send " + branch + " to the remote, so the work is kept there"})
	}
	return plan, nil
}

// PutAside is the plan for setting a worktree's uncommitted changes aside as a stash. A stash
// belongs to the whole repository, not to the worktree, so it outlives the worktree being removed
// and can be brought back in any checkout: the undo, short of throwing the work away.
func PutAside(cfg *config.Config, project, dir string) (action.Plan, error) {
	p, branch, err := workCheckout(cfg, project, dir)
	if err != nil {
		return action.Plan{}, err
	}
	if len(gitLines(dir, "status", "--porcelain")) == 0 {
		return action.Plan{}, fmt.Errorf("%s has nothing uncommitted to put aside", filepath.Base(dir))
	}
	label := fmt.Sprintf("engineroom: put aside from %s on %s, %s", filepath.Base(dir), branch, time.Now().Format("2006-01-02 15:04"))
	return action.Plan{
		Title:    "Put aside the changes in " + filepath.Base(dir),
		Commands: []action.Command{{Argv: []string{"git", "-C", dir, "stash", "push", "--include-untracked", "-m", label}, Why: "keep every change, new files included, as a stash of the repository"}},
		Notes:    []string{"To bring them back in any checkout of " + p.Name + ": git stash list, then git stash apply with the stash labelled \"" + label + "\"."},
	}, nil
}

// PushWork is the plan for sending a worktree's branch to the remote, so commits that were only
// here are kept there.
func PushWork(cfg *config.Config, project, dir string) (action.Plan, error) {
	p, branch, err := workCheckout(cfg, project, dir)
	if err != nil {
		return action.Plan{}, err
	}
	if _, hasRemote := remote(p.Path); !hasRemote {
		return action.Plan{}, fmt.Errorf("%s has no remote to push to", p.Name)
	}
	return action.Plan{Title: "Push " + branch, Commands: []action.Command{
		{Argv: []string{"git", "-C", dir, "push", "-u", "origin", branch}, Why: "send " + branch + " to the remote"},
	}}, nil
}
