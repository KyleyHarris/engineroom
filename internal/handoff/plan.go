package handoff

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/roots"
)

// Side is one machine in a handoff: what it is called, what was read there, and how a command
// is run there.
type Side struct {
	// Name is how notes and command reasons name the machine: "this machine" or "the engine".
	Name string
	// State is the machine's reading, or nil when it could not be read.
	State *State
	// Unread is why State is nil, said as it is to the developer.
	Unread string
	// Wrap turns a command into the one this machine runs to do it there: itself on this
	// machine, an ssh command for the engine.
	Wrap func(action.Command) action.Command
	// Here marks the client's own side when the client is online, where `engineroom api` actions act
	// on the engine unless told --here: an offer for this side carries it.
	Here bool
	// PushOnly is a receiving side that is only GitHub (a release on the engine): nothing is brought
	// up to date or mirrored, and nothing is said about what it lacks.
	PushOnly bool
	// Folder, when set, says whether a folder exists on this machine and, when it is a git
	// repository, its origin. A mirror asks it before cloning a project the machine has not
	// registered: a clone made by hand there is registered rather than cloned over, which git
	// refuses ("destination path ... already exists"), stopping the plan. Nil where it cannot be
	// asked (the engine, over SSH), and the
	// mirror clones as before.
	Folder func(path string) (exists bool, origin string)
	// Git, on the engine's side of a client's handover, is the connection git reaches the engine's
	// repositories by over SSH, to carry uncommitted work to or from it as a stash. Nil on this
	// machine's side, and where no work is carried (a release).
	Git *action.Remote
}

// LocalFolder is Folder on this machine: whether path exists, and its origin as configured when
// it is a git repository.
func LocalFolder(path string) (bool, string) {
	if _, err := os.Stat(path); err != nil {
		return false, ""
	}
	out, err := git(path, "config", "--get", "remote.origin.url")
	if err != nil {
		return true, ""
	}
	return true, strings.TrimSpace(out)
}

// Build is the plan that hands the work over from one machine to the other: afterwards the
// receiving machine is in the giving machine's state, checkout by checkout: identical state is what
// a handover means (D-017, amended).
//
// It is all or nothing: everything is checked before anything is done. Planning finds everything
// that would stop the receiving machine reaching that state (a branch GitHub cannot take, a
// receiving branch with commits of its own, a detached commit, two different repositories under
// one name); any of them blocks the whole plan, which then has no commands, only what to clear.
// A plan that can run starts with checks that change nothing (each machine's checkouts are as they
// were read, GitHub takes every push), so a commit or an edit made since, or a GitHub that moved
// on, stops it before anything is changed.
//
// Then, in order: the giving machine's checked-out branches are pushed; a project the receiving
// machine lacks is cloned and registered (mirrorClone); each receiving checkout sets its own
// uncommitted work aside in its stash list, labelled with its branch, and is put on the giving
// checkout's branch at its commit, or made when it is missing; and last, each giving checkout's
// uncommitted work is stashed there (kept in its stash list) and carried straight to the receiving
// checkout over SSH, never through GitHub, and applied.
//
// A release (to GitHub only), or a handover with one machine unread, cannot reach that state, and
// pushes what it can instead (pushOnly).
func Build(from, to Side) action.Plan {
	plan := action.Plan{
		Title:   "Hand over from " + from.Name + " to " + to.Name,
		Machine: from.Name + " and " + to.Name,
	}
	if to.PushOnly || from.State == nil || to.State == nil {
		return pushOnly(plan, from, to)
	}
	return handover(plan, from, to)
}

// BlockedHeading leads what blocks a handover, the last of a blocked plan's notes.
const BlockedHeading = "This handover cannot run yet, so nothing has been changed on either machine. Clear what is below, then hand over again:"

// planner gathers a plan's parts as it is worked out.
type planner struct {
	from, to                                  Side
	notes, blockers                           []string
	checksFrom, checksTo                      []string
	leaveFrom, leaveTo                        map[string]bool
	dryRuns, pushes, moves, prepares, carries []action.Command
	// What the plan does is said one line per checkout (say), what it leaves alone folded into a
	// line per kind (idle branches, unchecked projects), so a handover across many projects reads
	// at a glance.
	summaries []*summary
	idle      []string
	unchecked map[string][]string
	reasons   []string
}

// summary is one checkout's line: which checkout, then what the handover does to it.
type summary struct {
	head  string
	parts []string
}

// say adds what the handover does to one checkout to that checkout's line.
func (p *planner) say(head, format string, args ...any) {
	for _, s := range p.summaries {
		if s.head == head {
			s.parts = append(s.parts, fmt.Sprintf(format, args...))
			return
		}
	}
	p.summaries = append(p.summaries, &summary{head: head, parts: []string{fmt.Sprintf(format, args...)}})
}

// headOf names a checkout on its line: the project for its main clone, the project and the
// worktree's folder for a worktree, so no line carries a whole path.
func headOf(r Repo, path string) string {
	if path == mainOf(r) || path == r.Path {
		return r.Project
	}
	return r.Project + ", worktree " + filepath.Base(path)
}

// lines is the plan's notes: a line per checkout, then what is left alone, folded, then the rest.
func (p *planner) lines() []string {
	var out []string
	for _, s := range p.summaries {
		out = append(out, s.head+": "+strings.Join(s.parts, "; ")+".")
	}
	if len(p.idle) > 0 {
		out = append(out, fmt.Sprintf("Left as they are on %s, in no checkout and with commits GitHub lacks (send one with git -C <its project's folder> push origin <branch>): %s.", p.from.Name, strings.Join(p.idle, "; ")))
	}
	for _, why := range p.reasons {
		out = append(out, fmt.Sprintf("Not checked on %s, because %s: %s.", p.from.Name, why, strings.Join(p.unchecked[why], ", ")))
	}
	return append(out, p.notes...)
}

func (p *planner) note(format string, args ...any) {
	p.notes = append(p.notes, fmt.Sprintf(format, args...))
}

func (p *planner) block(format string, args ...any) {
	p.blockers = append(p.blockers, fmt.Sprintf(format, args...))
}

// checkFrom and checkTo add a checkout to the giving or the receiving machine's first step, which
// confirms it is still as read (Verify).
func (p *planner) checkFrom(c Checkout, leave []string) {
	p.checksFrom = append(p.checksFrom, c.Path, c.Fingerprint)
	for _, l := range leave {
		p.leaveFrom[l] = true
	}
}

func (p *planner) checkTo(c Checkout, leave []string) {
	p.checksTo = append(p.checksTo, c.Path, c.Fingerprint)
	for _, l := range leave {
		p.leaveTo[l] = true
	}
}

func handover(plan action.Plan, from, to Side) action.Plan {
	p := &planner{from: from, to: to, leaveFrom: map[string]bool{}, leaveTo: map[string]bool{}, unchecked: map[string][]string{}}
	toRepos := map[string]Repo{}
	for _, r := range to.State.Repos {
		toRepos[r.Project] = r
	}
	// The giving machine's projects are gone through by name, ignoring case, so the notes and
	// commands read in the order every other list of projects is in.
	repos := append([]Repo(nil), from.State.Repos...)
	sort.SliceStable(repos, func(i, j int) bool { return roots.NameLess(repos[i].Project, repos[j].Project) })
	for _, r := range repos {
		if r.Problem != "" {
			if p.unchecked[r.Problem] == nil {
				p.reasons = append(p.reasons, r.Problem)
			}
			p.unchecked[r.Problem] = append(p.unchecked[r.Problem], r.Project)
			continue
		}
		tr, inBoth := toRepos[r.Project]
		if inBoth && tr.Problem != "" {
			p.block("%s on %s cannot be read, because %s, so it cannot be put in %s's state.", r.Project, to.Name, tr.Problem, from.Name)
			continue
		}
		if inBoth && !sameRemote(r.Remote, tr.Remote) {
			p.block("%s: %s and %s have it from different repositories (%s and %s). Register the same repository under that name on both.", r.Project, from.Name, to.Name, r.Remote, tr.Remote)
			continue
		}
		if r.GitHubProblem != "" {
			p.note("%s on %s: GitHub could not be read (%s), so its branches are compared with what %s last fetched.", r.Project, from.Name, r.GitHubProblem, from.Name)
		}
		pushes, problems, idle := branchPushes(r, from)
		p.blockers = append(p.blockers, problems...)
		if len(idle) > 0 {
			p.idle = append(p.idle, r.Project+": "+strings.Join(idle, ", "))
		}
		for _, c := range pushes {
			p.pushes = append(p.pushes, c.push)
			p.dryRuns = append(p.dryRuns, c.dryRun)
			p.say(headOf(r, c.dir), "%s sent to GitHub from %s", c.what(), from.Name)
		}
		if !inBoth {
			var ok bool
			if tr, ok = p.mirrorClone(r); !ok {
				continue
			}
		}
		for _, c := range r.Checkouts {
			p.checkout(r, c, tr)
		}
	}
	if len(from.State.Sessions) > 0 && to.Name == "the engine" {
		var running []string
		for _, s := range from.State.Sessions {
			running = append(running, s.Name)
		}
		p.note("Still running on %s: %s. They keep running, and are listed with the engine's sessions as on this machine.", from.Name, strings.Join(running, ", "))
	}

	if len(p.blockers) > 0 {
		// What it would have done comes first and what blocks it last, under its heading, so the
		// blockers sit right above the end of the screen in the terminal, where a long plan
		// leaves the eye, rather than scrolled out of sight above "Nothing to run".
		plan.Blocked = true
		plan.Notes = append(append(p.lines(), BlockedHeading), p.blockers...)
		return plan
	}
	var changes []action.Command
	for _, part := range [][]action.Command{p.pushes, p.moves, p.prepares, p.carries} {
		changes = append(changes, part...)
	}
	if len(changes) == 0 {
		plan.Notes = append([]string{"Nothing needs handing over."}, p.lines()...)
		return plan
	}
	var checks []action.Command
	if len(p.checksFrom) > 0 {
		checks = append(checks, from.Wrap(verifyCommand(from.Name, p.checksFrom, p.leaveFrom)))
	}
	if len(p.checksTo) > 0 {
		checks = append(checks, to.Wrap(verifyCommand(to.Name, p.checksTo, p.leaveTo)))
	}
	plan.Commands = append(append(checks, p.dryRuns...), changes...)
	plan.Notes = p.lines()
	return plan
}

// verifyCommand is a machine's first step: every checkout the handover reads or changes there is
// still as it was read.
func verifyCommand(machine string, checks []string, leave map[string]bool) action.Command {
	argv := []string{"engineroom", "handoff", "verify"}
	names := make([]string, 0, len(leave))
	for l := range leave {
		names = append(names, l)
	}
	sort.Strings(names)
	for _, l := range names {
		argv = append(argv, "--leave", l)
	}
	argv = append(argv, checks...)
	return action.Command{Argv: argv,
		Why:    fmt.Sprintf("on %s: check every checkout is still as it was read (its branch, commit and uncommitted work); changes nothing", machine),
		OnFail: "Nothing has been handed over: something changed since the handover was planned. Plan it again."}
}

// checkout puts the receiving machine's counterpart of the giving checkout c (of r) in c's state:
// its own uncommitted work set aside, then the same branch at the same commit, then c's
// uncommitted work. tr is the receiving machine's repository (one the mirror is making, when it
// lacks the project).
func (p *planner) checkout(r Repo, c Checkout, tr Repo) {
	from, to := p.from, p.to
	if c.Branch == "" {
		p.block("%s: %s on %s is on a detached commit, not a branch, so %s cannot be put in the same state. Switch it to a branch first.", r.Project, c.Path, from.Name, to.Name)
		return
	}
	// A worktree away from its main clone (one a tool made in a temporary folder) is not one of
	// the project's jobs, which engineroom makes beside the main clone: it is left where it is,
	// said, rather than stopping the handover.
	dest, ok := counterpart(mainOf(r), c.Path, mainOf(tr))
	if !ok {
		p.note("%s: the worktree %s on %s is not beside its main clone, so it is not one of the project's jobs and is left where it is.", r.Project, c.Path, from.Name)
		return
	}
	b, rb := branchOf(r, c.Branch), branchOf(tr, c.Branch)
	if rb.Ahead > 0 {
		p.block("%s: %s on %s has %s of its own that GitHub does not, which taking %s's state would leave behind. Push or merge them there first: git -C %s push origin %s", r.Project, c.Branch, to.Name, commits(rb.Ahead), from.Name, tr.Path, c.Branch)
		return
	}
	// A giving branch behind GitHub, where the receiving machine has more of GitHub's commits,
	// would move the receiving machine back: work done there since (pushed, so not lost) would
	// leave its checkout. That is not a handover of the newer work, so it blocks, saying how to
	// bring the giving machine up first. Behind is counted from what each machine last fetched;
	// a giving machine that has not fetched GitHub's newest commits (Behind unknown) is behind
	// when the receiving branch is at GitHub's head.
	ahead := rb.Head != "" && rb.Head != c.Head &&
		((b.Behind > 0 && rb.Behind >= 0 && rb.Behind < b.Behind) || (b.Behind == Unknown && rb.Head == b.GitHub))
	if ahead {
		p.block("%s: %s on %s is behind GitHub, and %s's %s has more of GitHub's commits, so taking %s's state would move %s back. Bring %s up to GitHub first: git -C %s pull --ff-only origin %s", r.Project, c.Branch, from.Name, to.Name, c.Branch, from.Name, to.Name, from.Name, c.Path, c.Branch)
		return
	}
	if other, ok := checkoutOn(tr, c.Branch); ok && other.Path != dest {
		p.block("%s: %s is checked out in %s on %s, but %s's checkout of it goes in %s. Switch %s to another branch first.", r.Project, c.Branch, other.Path, to.Name, from.Name, dest, other.Path)
		return
	}
	p.checkFrom(c, r.Leave)
	head := headOf(r, c.Path)
	here, exists := checkoutAt(tr, dest)
	short := c.Head
	if len(short) > 7 {
		short = short[:7]
	}
	add := func(cmd action.Command) { p.prepares = append(p.prepares, to.Wrap(cmd)) }
	if exists {
		if here.made == "" {
			p.checkTo(here, tr.Leave)
		}
		if len(here.Changes) > 0 || here.made == "existing" {
			label := fmt.Sprintf("engineroom: work on %s set aside before the handover from %s, %s", orDetached(here.Branch), otherName(from), time.Now().Format("2006-01-02 15:04"))
			argv := []string{"engineroom", "handoff", "aside", dest, "--label", label}
			for _, l := range tr.Leave {
				argv = append(argv, "--leave", l)
			}
			add(action.Command{Argv: argv, Why: fmt.Sprintf("on %s: set the uncommitted work in %s aside in its stash list (git stash push --include-untracked), labelled with its branch, so the checkout can take %s's state", to.Name, dest, from.Name)})
			if len(here.Changes) > 0 {
				p.say(head, "its own %s on %s set aside in its stash list, labelled \"work on %s\"", describeChanges(here), to.Name, orDetached(here.Branch))
			}
		}
		if here.Branch != c.Branch || here.Head != c.Head {
			add(action.Command{Argv: []string{"git", "-C", dest, "fetch", "-q", "origin", c.Branch}, Why: fmt.Sprintf("on %s: fetch %s from GitHub", to.Name, c.Branch)})
			add(action.Command{Argv: []string{"git", "-C", dest, "switch", "-q", "-C", c.Branch, c.Head},
				Why: fmt.Sprintf("on %s: put %s on %s at %s, where %s's checkout is; %s there has nothing GitHub lacks, so nothing is lost", to.Name, dest, c.Branch, short, from.Name, c.Branch)})
			add(action.Command{Argv: []string{"git", "-C", dest, "branch", "-q", "--set-upstream-to=origin/" + c.Branch, c.Branch}, Why: "track GitHub's " + c.Branch})
			switch {
			case here.made == "clone":
				p.say(head, "put on %s at %s", c.Branch, short)
			case here.Branch != c.Branch:
				p.say(head, "switched from %s to %s at %s on %s", orDetached(here.Branch), c.Branch, short, to.Name)
			default:
				p.say(head, "brought to %s's %s at %s on %s", from.Name, c.Branch, short, to.Name)
			}
		}
	} else {
		add(action.Command{Argv: []string{"git", "-C", tr.Path, "fetch", "-q", "origin", c.Branch}, Why: fmt.Sprintf("on %s: fetch %s from GitHub", to.Name, c.Branch)})
		add(action.Command{Argv: []string{"git", "-C", tr.Path, "worktree", "add", "-q", "-B", c.Branch, dest, c.Head},
			Why: fmt.Sprintf("on %s: make the worktree %s on %s at %s, as on %s", to.Name, dest, c.Branch, short, from.Name)})
		add(action.Command{Argv: []string{"git", "-C", dest, "branch", "-q", "--set-upstream-to=origin/" + c.Branch, c.Branch}, Why: "track GitHub's " + c.Branch})
		if r.Slots {
			add(action.Command{Argv: []string{"engineroom", "slot", "claim", "--worktree", dest}, Why: fmt.Sprintf("on %s: give the worktree its own slot of ports", to.Name)})
		}
		add(action.Command{Argv: []string{"engineroom", "secrets", "restore", r.Project, "--worktree", dest}, Why: fmt.Sprintf("on %s: put the project's secret files in the worktree from your settings", to.Name)})
		p.say(head, "made on %s, on %s at %s", to.Name, c.Branch, short)
	}
	if len(c.Changes) > 0 {
		p.carry(r, c, dest, tr.Leave, head)
	}
}

// carry stashes the giving checkout's uncommitted work there (kept in its stash list), sends it
// straight to the receiving machine's repository over SSH, never through GitHub, and applies it in
// dest, which by then is on the same branch at the same commit with nothing of its own.
func (p *planner) carry(r Repo, c Checkout, dest string, leaveTo []string, head string) {
	from, to := p.from, p.to
	if (from.Git == nil) == (to.Git == nil) {
		p.block("%s: the uncommitted work in %s on %s can be carried only between a client and its engine.", r.Project, c.Path, from.Name)
		return
	}
	ref := HandoverRef(c.Branch)
	pack := []string{"engineroom", "handoff", "pack", c.Path, ref, "--label", "engineroom: handover of " + c.Branch + " to " + otherName(to)}
	for _, l := range r.Leave {
		pack = append(pack, "--leave", l)
	}
	unpack := []string{"engineroom", "handoff", "unpack", dest, ref}
	for _, l := range leaveTo {
		unpack = append(unpack, "--leave", l)
	}
	p.carries = append(p.carries, from.Wrap(action.Command{Argv: pack,
		Why: fmt.Sprintf("on %s: put the uncommitted work in %s, new files included, in a stash (git stash push --include-untracked), named %s to send it; the stash stays in %s's stash list too", from.Name, c.Path, ref, from.Name)}))
	if to.Git != nil {
		p.carries = append(p.carries, action.Command{
			Argv:   []string{"git", "-C", c.Path, "-c", "core.sshCommand=" + to.Git.GitSSH(), "push", "-q", to.Git.GitURL(dest), "+" + ref + ":" + ref},
			Why:    fmt.Sprintf("send it straight to %s's repository over SSH, not to GitHub", to.Name),
			OnFail: fmt.Sprintf("The work is stashed in %s and was not sent. Get it back there with: git -C %s stash pop", c.Path, c.Path),
		})
	} else {
		p.carries = append(p.carries, action.Command{
			Argv:   []string{"git", "-C", dest, "-c", "core.sshCommand=" + from.Git.GitSSH(), "fetch", "-q", from.Git.GitURL(c.Path), "+" + ref + ":" + ref},
			Why:    fmt.Sprintf("bring it straight from %s's repository over SSH, not through GitHub", from.Name),
			OnFail: fmt.Sprintf("The work is stashed in %s on %s and was not brought here. Get it back there with: git -C %s stash pop", c.Path, from.Name, c.Path),
		})
	}
	// Sent, the giving machine's name for it is spent: the stash itself stays in its stash list.
	p.carries = append(p.carries, from.Wrap(action.Command{
		Argv: []string{"git", "-C", c.Path, "update-ref", "-d", ref},
		Why:  fmt.Sprintf("on %s: drop the name the work was sent under; the stash stays in its stash list", from.Name),
	}))
	p.carries = append(p.carries, to.Wrap(action.Command{Argv: unpack,
		Why:    fmt.Sprintf("on %s: apply the work in %s, now on %s at the commit it was made on", to.Name, dest, c.Branch),
		OnFail: fmt.Sprintf("The work was not applied in %s on %s; it is kept there as %s, and in %s's stash list on %s.", dest, to.Name, ref, c.Path, from.Name)}))
	p.say(head, "%s carried from %s straight over SSH, never through GitHub, and kept in its stash list there", describeChanges(c), from.Name)
	var working []string
	for _, s := range from.State.Sessions {
		if s.Path == c.Path {
			working = append(working, s.Name)
		}
	}
	if len(working) > 0 {
		p.say(head, "%s's session %s works there, and its changes leave it, so carry on with them on %s", from.Name, strings.Join(working, " and "), to.Name)
	}
}

// mirrorClone brings a project the receiving machine lacks: a clone at the same place under its
// home as on the giving machine, so both machines' project folders mirror each other, or the clone
// of it already there, registered under the same name and category, with
// its secret files. It returns the repository as the handover then finds it there, so each
// checkout is put in the giving machine's state like any other; false, with what blocks it, when
// there is no place for it.
func (p *planner) mirrorClone(r Repo) (Repo, bool) {
	from, to := p.from, p.to
	dest, ok := mirrorPath(mainOf(r), from.State.Home, to.State.Home)
	if !ok {
		p.block("%s is not on %s, and its folder %s on %s is not under the home folder, so there is no matching place to copy it to. Get it there with: engineroom project add <folder> --name %s", r.Project, to.Name, r.Path, from.Name, r.Project)
		return Repo{}, false
	}
	add := func(why string, argv ...string) {
		p.moves = append(p.moves, to.Wrap(action.Command{Argv: argv, Why: fmt.Sprintf("on %s: %s", to.Name, why)}))
	}
	made := "clone"
	exists, origin := false, ""
	if to.Folder != nil {
		exists, origin = to.Folder(dest)
	}
	if exists {
		if origin == "" || !sameRemote(origin, r.Remote) {
			what := "is not a git repository"
			if origin != "" {
				what = "is a clone of " + origin
			}
			p.block("%s is not registered on %s, and %s there %s, not %s. Move it aside, or register the right folder with: engineroom project add <folder> --name %s", r.Project, to.Name, dest, what, r.Remote, r.Project)
			return Repo{}, false
		}
		made = "existing"
		p.say(r.Project, "registered on %s from the clone of it already at %s", to.Name, dest)
		add("bring the clone already there up to date with GitHub", "git", "-C", dest, "fetch", "-q", "origin")
	} else {
		p.say(r.Project, "copied to %s on %s, as on %s, and registered", dest, to.Name, from.Name)
		add("copy "+r.Project+" from GitHub to the same place as on "+from.Name, "git", "clone", "-q", r.Remote, dest)
	}
	register := []string{"engineroom", "project", "add", dest, "--name", r.Project}
	if r.Category != "" {
		register = append(register, "--category", r.Category)
	}
	add("register it under the same name", register...)
	add("put the project's secret files in place from your settings", "engineroom", "secrets", "restore", r.Project)
	return Repo{Project: r.Project, Path: dest, Remote: r.Remote, Slots: r.Slots, Leave: r.Leave,
		Checkouts: []Checkout{{Path: dest, made: made}}}, true
}

// pushOnly is a plan that cannot put the receiving machine in the giving machine's state, and
// pushes what it can: a release, to GitHub only, or a handover with one machine unread. What
// stops a branch is said, and pushing the rest goes ahead.
func pushOnly(plan action.Plan, from, to Side) action.Plan {
	note := func(format string, args ...any) { plan.Notes = append(plan.Notes, fmt.Sprintf(format, args...)) }
	if from.State == nil {
		note("%s %s, so it could not be checked for work that is not on GitHub. Anything committed there and not pushed stays there.", upper(from.Name), from.Unread)
	}
	if to.State == nil && !to.PushOnly {
		note("%s %s, so it cannot be put in %s's state now. Hand over again once it answers.", upper(to.Name), to.Unread, from.Name)
	}
	if from.State != nil {
		repos := append([]Repo(nil), from.State.Repos...)
		sort.SliceStable(repos, func(i, j int) bool { return roots.NameLess(repos[i].Project, repos[j].Project) })
		for _, r := range repos {
			if r.Problem != "" {
				note("%s on %s: not checked, because %s.", r.Project, from.Name, r.Problem)
				continue
			}
			if r.GitHubProblem != "" {
				note("%s on %s: GitHub could not be read (%s), so its branches are compared with what %s last fetched.", r.Project, from.Name, r.GitHubProblem, from.Name)
			}
			pushes, problems, idle := branchPushes(r, from)
			plan.Notes = append(plan.Notes, problems...)
			for _, c := range pushes {
				plan.Commands = append(plan.Commands, c.push)
				note("%s: %s sent to GitHub from %s.", headOf(r, c.dir), c.what(), from.Name)
			}
			if len(idle) > 0 {
				note("%s: left as they are on %s, in no checkout and with commits GitHub lacks (send one with git -C %s push origin <branch>): %s.", r.Project, from.Name, r.Path, strings.Join(idle, ", "))
			}
			// Uncommitted work has nowhere to go: a release does not send work in progress to
			// GitHub, and an unread machine cannot take it. It is said; a release offers to
			// commit it as partial work, which the next release then pushes.
			for _, c := range r.Checkouts {
				if len(c.Changes) == 0 && !c.Dirty {
					continue
				}
				if !to.PushOnly {
					note("%s: its %s on %s stays there.", headOf(r, c.Path), describeChanges(c), from.Name)
					continue
				}
				note("%s: the worktree %s on %s has uncommitted changes, which a release does not send to GitHub. Open its session and finish them, or commit them as partial work, then release again.", r.Project, c.Path, from.Name)
				act := []string{"commit-partial", r.Project, c.Path}
				if from.Here {
					act = append(act, "--here")
				}
				plan.Offers = append(plan.Offers, action.Offer{Label: "Commit as partial work: " + c.Path + " on " + from.Name, Action: act})
			}
		}
	}
	if len(plan.Commands) == 0 {
		plan.Notes = append([]string{"Nothing needs handing over."}, plan.Notes...)
	}
	return plan
}

// aPush is one branch's push and the dry run of it that a handover checks first.
type aPush struct {
	push, dryRun action.Command
	// branch is pushed from the checkout at dir, with ahead commits GitHub lacks (none for a
	// branch GitHub does not have yet).
	branch, dir string
	ahead       int
}

// what names the push on its checkout's line.
func (a aPush) what() string {
	if a.ahead == 0 {
		return a.branch + ", new to GitHub,"
	}
	return commits(a.ahead) + " of " + a.branch
}

// branchPushes is what sends r's checked-out work on the giving machine to GitHub: the branch each
// checkout has out (its main clone and its worktrees, the jobs the app shows) where GitHub lacks
// commits of it, or lacks the branch: only registered work is pushed, the safer rule
// (CONN-HANDOFF-007). problems are what stops a
// checked-out branch from being sent (a release branch, a branch GitHub has also moved on); idle
// names each branch in no checkout that has commits GitHub lacks, left where it is (a backup, an
// experiment), with how many when more than one.
func branchPushes(r Repo, from Side) (pushes []aPush, problems, idle []string) {
	worktreeOf := map[string]string{}
	for _, c := range r.Checkouts {
		if c.Branch != "" {
			worktreeOf[c.Branch] = c.Path
		}
	}
	for _, b := range r.Branches {
		dir, inWorktree := worktreeOf[b.Name]
		missing := b.GitHub == "" && r.GitHubProblem == ""
		if b.Ahead == 0 && !(inWorktree && missing) {
			continue
		}
		if !inWorktree {
			if b.Ahead > 1 {
				idle = append(idle, fmt.Sprintf("%s (%s)", b.Name, commits(b.Ahead)))
			} else {
				idle = append(idle, b.Name)
			}
			continue
		}
		// Pushing a release branch is how a repository starts a release (a pipeline on
		// release/<version>), so a handover never pushes one, even checked out (CONN-HANDOFF-012).
		if isReleaseBranch(b.Name) {
			problems = append(problems, fmt.Sprintf("%s: %s on %s has %s that GitHub does not. A handover never pushes a release branch, since pushing one can start a release; push it yourself when you mean to: git -C %s push origin %s", r.Project, b.Name, from.Name, orNoCommits(b.Ahead), dir, b.Name))
			continue
		}
		if b.Behind > 0 && b.Ahead > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s on %s and on GitHub have both moved on, so it is not pushed. Bring GitHub's commits in on %s first: git -C %s pull origin %s", r.Project, b.Name, from.Name, from.Name, dir, b.Name))
			continue
		}
		what := commits(b.Ahead) + " of " + b.Name
		if b.Ahead == 0 {
			what = b.Name + ", which GitHub does not have yet"
		}
		pushes = append(pushes, aPush{
			branch: b.Name, dir: dir, ahead: b.Ahead,
			push: from.Wrap(action.Command{
				Argv: []string{"git", "-C", dir, "push", "origin", b.Name},
				Why:  fmt.Sprintf("on %s: send %s to GitHub", from.Name, what),
				OnFail: fmt.Sprintf("GitHub did not take %s. If GitHub has commits on %s that %s does not, bring them in there first (git -C %s pull origin %s), then hand over again.",
					b.Name, b.Name, from.Name, dir, b.Name),
			}),
			dryRun: from.Wrap(action.Command{
				Argv:   []string{"git", "-C", dir, "push", "--dry-run", "-q", "origin", b.Name},
				Why:    fmt.Sprintf("on %s: check GitHub takes %s; sends nothing", from.Name, b.Name),
				OnFail: fmt.Sprintf("Nothing has been handed over: GitHub would not take %s from %s. Bring GitHub's commits in there first (git -C %s pull origin %s), then hand over again.", b.Name, from.Name, dir, b.Name),
			}),
		})
	}
	return pushes, problems, idle
}

// counterpart is where the giving checkout at path goes on the receiving machine: its main clone
// for the giving main clone, and a worktree in the same place beside it for a linked worktree, so
// a project registered in different folders on the two machines still lines up.
func counterpart(main, path, toMain string) (string, bool) {
	if path == main {
		return toMain, true
	}
	rel, err := filepath.Rel(filepath.Dir(main), path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.Join(filepath.Dir(toMain), rel), true
}

// mainOf is a repository's main clone as git names it (the first of its checkouts), which may
// differ from the folder it was registered by through a symbolic link; the registered folder
// when it has no checkouts read.
func mainOf(r Repo) string {
	if len(r.Checkouts) > 0 {
		return r.Checkouts[0].Path
	}
	return r.Path
}

// otherName is how a stash label stored on one machine names the other.
func otherName(s Side) string {
	if s.Name == "the engine" {
		return "the engine"
	}
	return "the client"
}

func orNoCommits(n int) string {
	if n == 0 {
		return "no commits"
	}
	return commits(n)
}

// mirrorPath is path's place under toHome: the same path below the home folder as under fromHome.
func mirrorPath(path, fromHome, toHome string) (string, bool) {
	if fromHome == "" || toHome == "" {
		return "", false
	}
	rel, ok := strings.CutPrefix(path, strings.TrimSuffix(fromHome, "/")+"/")
	if !ok || rel == "" || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return strings.TrimSuffix(toHome, "/") + "/" + rel, true
}

func commits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// sameRemote reports whether two origins name the same repository: the same owner and name,
// whatever the address around them. One machine may reach GitHub through an SSH host alias of its
// own (git@github-work:owner/name) and the other by its address (https://github.com/owner/name);
// both are the same repository.
func sameRemote(a, b string) bool {
	return repoKey(a) == repoKey(b)
}

// repoKey is an origin's last two path parts, owner/name, lower case, without ".git".
func repoKey(url string) string {
	s := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) >= 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.ToLower(strings.Join(parts, "/"))
}

// isReleaseBranch is a branch named release/<anything> or release-<anything>, the names under
// which a repository's pipeline starts a release when the branch is pushed. A handover never
// pushes one (CONN-HANDOFF-012).
func isReleaseBranch(name string) bool {
	return strings.HasPrefix(name, "release/") || strings.HasPrefix(name, "release-")
}
