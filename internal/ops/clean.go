package ops

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// ErrMainCheckout is a request to clean up a project's main clone, which is never done.
var ErrMainCheckout = errors.New("the main worktree of a project is never cleaned up")

// protected matches branches clean up never deletes, whatever their state: main, master and
// release branches carry shared history, never one piece of work.
var protected = regexp.MustCompile(`^(main|master|release[-_.].*|release)$`)

// RefusedError is clean up (or a switch to the next branch) declining because work would be
// lost, with the reason in the developer's terms and the actions that would make it safe: save
// the changes to the branch, put them aside, or push the commits (D-024). The offers are only
// offered; nothing runs until the developer chooses one.
type RefusedError struct {
	Reason string
	Offers []action.Offer
}

func (e *RefusedError) Error() string { return e.Reason }

// CleanWork is the plan for cleaning up a piece of work on a project.
func CleanWork(cfg *config.Config, project, work string) (action.Plan, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return action.Plan{}, &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	if work == "" {
		return action.Plan{}, fmt.Errorf("%w (%s)", ErrMainCheckout, p.Name)
	}
	return cleanDir(p, WorktreePath(p.Path, work))
}

// CleanSession is the plan for cleaning up the work a session belongs to.
func CleanSession(cfg *config.Config, session string) (action.Plan, error) {
	c, ok := checkouts(cfg)[session]
	if !ok {
		return action.Plan{}, fmt.Errorf("%q is not the session of a registered project's worktree", session)
	}
	p, _ := cfg.Project(c.project)
	return cleanDir(p, c.path)
}

// cleanDir decides, before anything runs, whether the worktree at dir may be removed, and
// builds the plan. Everything that would make removal lose something (uncommitted changes,
// commits on no remote) refuses the whole plan: nothing is stopped or removed.
//
// The remote is fetched while planning, because whether the branch has been merged, and so
// may be deleted, depends on the remote's main as it is now. The plan also lists the fetch,
// so the developer sees it among the commands.
func cleanDir(p config.Project, dir string) (action.Plan, error) {
	if resolvePath(dir) == resolvePath(p.Path) {
		return action.Plan{}, fmt.Errorf("%w (%s)", ErrMainCheckout, p.Name)
	}
	if !isWorktree(p.Path, dir) {
		return action.Plan{}, fmt.Errorf("%s is not a worktree of %s", dir, p.Name)
	}
	sp, spErr := settings.Load(p.Name, p.Path, dir)
	hasRemote, err := nothingToLose(p, dir, sp, spErr, "nothing was stopped or removed")
	if err != nil {
		return action.Plan{}, err
	}
	branch := strings.TrimSpace(gitOut(dir, "branch", "--show-current"))
	work := strings.TrimPrefix(filepath.Base(dir), filepath.Base(p.Path)+"-")
	name := zj.SessionName(dir)

	plan := action.Plan{Title: fmt.Sprintf("Clean up %s on %s", work, p.Name)}
	if hasRemote {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "fetch", "origin"}, Why: "bring the remote's branches up to date"})
	}
	// The stack is stopped first, while the session and the worktree it runs from are still
	// there, so nothing it started is left running against a worktree that is gone
	// (WORK-STACK-003).
	if c, ok := script(p, dir, dir, work, "down"); ok {
		c.OnFail = "The down script failed, so nothing else was stopped or removed. Fix the script and clean up again."
		plan.Commands = append(plan.Commands, c)
	}
	// The session is ended fully, as Stop does (D-015): stopped if it is running, then what
	// Zellij keeps of it deleted, so nothing of a worktree that is about to go is left listed as
	// an ended session that could be brought back into a folder that no longer exists.
	if s, err := find(name); err == nil && s != nil {
		if s.Ended {
			plan.Commands = append(plan.Commands, action.Command{Argv: []string{"zellij", "delete-session", name}, Why: "forget the ended session"})
		} else {
			plan.Commands = append(plan.Commands, zj.End(name))
		}
	}
	// Then whatever still runs in the session's name, before the folder it runs from goes
	// (D-020).
	plan.Commands = append(plan.Commands, reapCommand(name))
	if c, ok := script(p, dir, dir, work, "teardown"); ok {
		c.OnFail = "The teardown script failed, so the worktree was kept. Fix the script and clean up again."
		plan.Commands = append(plan.Commands, c)
	}
	// The slot is given back once nothing of the worktree is running, before the worktree goes,
	// so the next new work can have it (PORT-SLOT-002).
	if spErr == nil && sp.HasSlots() {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"engineroom", "slot", "release", "--worktree", dir}, Why: "give the worktree's slot back, so new work can have it"})
	}
	plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "worktree", "remove", dir}, Why: "remove the worktree"})
	// A worktree kept for a role is forgotten as one once it is gone (D-024), so a worktree made
	// later under the same name is not taken for it.
	if settings.HasRole(p.Name, p.Path, work) {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"engineroom", "roles", "remove", p.Name, work}, Why: "forget " + work + " as one of " + p.Name + "'s roles"})
	}

	if branch != "" && !protected.MatchString(branch) && merged(p.Path, branch, hasRemote) {
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "branch", "-D", branch}, Why: "delete the branch here; it is merged into main"})
		if hasRemote && gitOut(p.Path, "ls-remote", "--heads", "origin", branch) != "" {
			plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", p.Path, "push", "origin", "--delete", branch}, Why: "delete the branch on the remote; it is merged into main"})
		}
	}
	return plan, nil
}

// nothingToLose refuses, before anything runs, when what is about to happen would lose work in the
// checkout at dir: uncommitted changes, or commits on no remote. Each refusal names what it found
// (done says what was therefore not done) and offers what would make it safe. The remote is
// fetched first, so "pushed" means as the remote is now. It reports whether there is a remote.
func nothingToLose(p config.Project, dir string, sp settings.Project, spErr error, done string) (bool, error) {
	if dirty := withoutSlotFiles(gitLines(dir, "status", "--porcelain"), sp, spErr); len(dirty) > 0 {
		return false, &RefusedError{
			Reason: fmt.Sprintf("%s has uncommitted changes, so %s: %s", filepath.Base(dir), done, strings.Join(firstN(dirty, 5), "; ")),
			Offers: []action.Offer{
				{Label: "Save them to the branch and push", Action: []string{"save-work", p.Name, dir}},
				{Label: "Put them aside (a stash you can bring back)", Action: []string{"put-aside", p.Name, dir}},
			},
		}
	}
	_, hasRemote := remote(p.Path)
	if hasRemote {
		_ = exec.Command("git", "-C", p.Path, "fetch", "origin", "--quiet").Run()
	}
	if unpushed := gitLines(dir, "log", "--oneline", "HEAD", "--not", "--remotes"); len(unpushed) > 0 {
		e := &RefusedError{Reason: fmt.Sprintf("%s has %d commit(s) that were never pushed, so %s: %s", filepath.Base(dir), len(unpushed), done, strings.Join(firstN(unpushed, 3), "; "))}
		if hasRemote {
			e.Offers = []action.Offer{{Label: "Push them", Action: []string{"push-work", p.Name, dir}}}
		}
		return hasRemote, e
	}
	return hasRemote, nil
}

// withoutSlotFiles leaves out of git's status the files the program itself wrote for the
// worktree's slot (its record, environment file and filled-in templates): they are removed with
// the slot, so they are no reason to refuse a clean up, even in a repository that does not ignore
// them yet.
func withoutSlotFiles(lines []string, sp settings.Project, err error) []string {
	if err != nil || !sp.HasSlots() {
		return lines
	}
	ours := map[string]bool{sp.Slots.Record: true, slots.EnvFile(sp.Slots.Record): true}
	for _, r := range sp.Render {
		ours[filepath.ToSlash(r.To)] = true
	}
	var out []string
	for _, l := range lines {
		// Lines arrive trimmed ("?? file", "M file"): the path is what follows the status.
		if _, path, ok := strings.Cut(l, " "); ok && ours[strings.TrimSpace(path)] {
			continue
		}
		out = append(out, l)
	}
	return out
}

// remote reports the main branch work is merged into: the remote's default branch when there
// is a remote, otherwise main.
func remote(main string) (string, bool) {
	if exec.Command("git", "-C", main, "remote", "get-url", "origin").Run() != nil {
		return "main", false
	}
	base, _ := startPoint(main)
	return base, true
}

// merged reports whether every commit of branch is already in the main branch.
func merged(main, branch string, hasRemote bool) bool {
	base, _ := remote(main)
	if !hasRemote {
		base = "main"
	}
	return exec.Command("git", "-C", main, "merge-base", "--is-ancestor", branch, base).Run() == nil
}

func gitOut(dir string, args ...string) string {
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out)
}

func gitLines(dir string, args ...string) []string {
	var out []string
	for _, l := range strings.Split(gitOut(dir, args...), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
	}
	return s
}
