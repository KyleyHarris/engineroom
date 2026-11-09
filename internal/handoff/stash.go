package handoff

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Uncommitted work travels between the two machines as a stash (D-017, amended 2026-11-09): the
// giving machine stashes a checkout's changes and new files (Pack), the stash is sent straight to
// the receiving machine's repository over SSH under a ref of engineroom's own (HandoverRef), and the
// receiving checkout, on the same branch at the same commit, applies it (Unpack). It never goes
// to GitHub, so work in progress is never published, even from a public repository.

// stashTimeout bounds stashing or applying a checkout's work, which writes every changed file.
const stashTimeout = 2 * time.Minute

// HandoverRef is the ref a checkout's uncommitted work is carried under, by its branch. Worktrees
// of one repository share their refs, and a branch is checked out in one worktree at a time, so
// the branch names the checkout.
func HandoverRef(branch string) string { return "refs/engineroom/handover/" + branch }

// Pack puts every uncommitted change in dir, new files included, in a stash with label as its
// message, and names the stash ref. The stash stays in dir's stash list, so the machine keeps the
// work too. The machine's own files (MachineOwn, and the leave files: its slot files) are not
// stashed and stay in place.
//
// With a ref, it is the giving machine's work to send, and it fails, stashing nothing, when there
// is nothing to stash: the work was committed or undone since the handover was planned, and an
// older stash must never be sent in its place. Without one, it is the receiving checkout's own
// work set aside before it takes the other machine's state (Aside), and a checkout with nothing to
// set aside is left as it is.
func Pack(dir, ref, label string, leave []string) error {
	before, _ := git(dir, "rev-parse", "-q", "--verify", "refs/stash")
	args := append([]string{"stash", "push", "--include-untracked", "-m", label, "--", "."}, Excludes(leave)...)
	if _, err := gitWithin(stashTimeout, dir, args...); err != nil {
		return err
	}
	after, _ := git(dir, "rev-parse", "-q", "--verify", "refs/stash")
	if after == "" || after == before {
		if ref == "" {
			return nil
		}
		return fmt.Errorf("there was nothing to stash in %s: its changes were committed or undone since the handover was planned, so nothing is carried from it", dir)
	}
	if ref == "" {
		return nil
	}
	if _, err := git(dir, "update-ref", ref, after); err != nil {
		return fmt.Errorf("%v; the work is stashed in %s as %s", err, dir, after)
	}
	return nil
}

// Unpack applies the work carried under ref to dir, then removes the ref. It applies only when
// the work lands as it was made: dir has no changes of its own (beyond the leave files) and is at
// the commit the work was made on. Otherwise it changes nothing and says how to apply it by
// hand, keeping the ref.
func Unpack(dir, ref string, leave []string) error {
	keep := fmt.Sprintf("The work is kept here as %s; apply it with: git -C %s stash apply %s", ref, dir, ref)
	stash, err := git(dir, "rev-parse", "-q", "--verify", ref+"^{commit}")
	if err != nil || stash == "" {
		return fmt.Errorf("no work was carried to %s under %s", dir, ref)
	}
	base, _ := git(dir, "rev-parse", ref+"^1")
	head, _ := git(dir, "rev-parse", "HEAD")
	if head != base {
		return fmt.Errorf("%s is at %s, not at %s where the work was made, so it is not applied. %s", dir, short(head), short(base), keep)
	}
	status, _ := gitRaw(dir, "status", "--porcelain", "--untracked-files=normal")
	if own := ChangesWithout(status, leave); len(own) > 0 {
		return fmt.Errorf("%s has changes of its own (%s), so the work is not applied over them. %s", dir, strings.Join(own, ", "), keep)
	}
	if _, err := gitWithin(stashTimeout, dir, "stash", "apply", ref); err != nil {
		return fmt.Errorf("%v. %s", err, keep)
	}
	if _, err := git(dir, "update-ref", "-d", ref); err != nil {
		return errors.New("the work is applied, but its ref " + ref + " could not be removed: " + err.Error())
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "no commit"
	}
	return sha
}

// Verify reports whether dir is still as the handover saw it when it was planned: the same
// branch, commit and uncommitted work (Fingerprint). It is a handover's first step on each
// machine, before anything is changed, so a commit or an edit made since stops the handover with
// nothing done.
func Verify(dir, fingerprint string, leave []string) error {
	branch, _ := git(dir, "branch", "--show-current")
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("%s cannot be read: %v", dir, err)
	}
	status, _ := gitRaw(dir, "status", "--porcelain", "--untracked-files=all")
	if Fingerprint(dir, branch, head, ChangesWithout(status, leave), leave) != fingerprint {
		return fmt.Errorf("%s has changed since the handover was planned (a commit, a switch or an edit), so nothing has been handed over; plan it again", dir)
	}
	return nil
}
