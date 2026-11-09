package handoff

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
)

// These tests run real git against a bare repository standing in for GitHub, with two clones
// standing in for the two machines. A handoff is built from real readings and, where the outcome
// matters, run with the real runner, so what is asserted is the state git is left in.

type world struct {
	t      *testing.T
	github string
	a, b   string // the giving and the receiving machine's clones
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "dev")
	t.Setenv("GIT_AUTHOR_EMAIL", "dev@engine.example")
	t.Setenv("GIT_COMMITTER_NAME", "dev")
	t.Setenv("GIT_COMMITTER_EMAIL", "dev@engine.example")
	w := &world{t: t, github: filepath.Join(home, "github", "project-a.git"), a: filepath.Join(home, "a", "project-a"), b: filepath.Join(home, "b", "project-a")}
	seed := filepath.Join(home, "seed")
	w.sh(home, `git init -q --bare -b main "$1" && git init -q -b main "$2" && cd "$2" && echo one > f && git add f && git commit -qm one && git push -q "$1" main`, w.github, seed)
	w.sh(home, `git clone -q "$1" "$2" && git clone -q "$1" "$3"`, w.github, w.a, w.b)
	return w
}

func (w *world) sh(dir, script string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command("sh", append([]string{"-c", script, "sh"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("%s: %v\n%s", script, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit makes a commit in dir and returns it.
func (w *world) commit(dir, text string) string {
	w.t.Helper()
	return w.sh(dir, `echo "$1" >> f && git commit -qam "$1" && git rev-parse HEAD`, text)
}

func (w *world) head(dir, ref string) string {
	w.t.Helper()
	return w.sh(dir, `git rev-parse "$1"`, ref)
}

func read(t *testing.T, name string, paths ...string) *State {
	t.Helper()
	st := &State{}
	for _, p := range paths {
		st.Repos = append(st.Repos, readRepo(config.Project{Name: name, Path: p}))
	}
	return st
}

func side(name string, st *State) Side {
	return Side{Name: name, State: st, Wrap: func(c action.Command) action.Command { return c }}
}

func run(t *testing.T, p action.Plan) bool {
	t.Helper()
	var steps []action.Step
	ok := action.Run(context.Background(), p, func(s action.Step) { steps = append(steps, s) })
	if !ok {
		t.Logf("steps: %+v", steps)
	}
	return ok
}

func notes(p action.Plan) string { return strings.Join(p.Notes, "\n") }

// TestMain builds this engineroom for the handover's own steps (verify, aside, pack, unpack), and
// puts a stand-in ssh beside it that runs the engine's side of a git transfer here, so a whole
// handover between the two test clones runs for real.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "handoff-bin")
	if err != nil {
		panic(err)
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "engineroom"), "../../cmd/engineroom").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("building engineroom: %v\n%s", err, out))
	}
	ssh := "#!/bin/sh\n# Stands in for ssh to the engine: runs the command it was given here.\nfor a; do last=$a; done\nexec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(ssh), 0o755); err != nil {
		panic(err)
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// engineSide is the engine's side of a client's handover: its commands run here as they are, and
// work is carried to and from it over git's SSH by its address (the stand-in ssh).
func engineSide(st *State) Side {
	s := side("the engine", st)
	s.Git = &action.Remote{Host: "engine.example"}
	return s
}

// resolved is a test clone's folder as git names it, through the temporary folder's link.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// checkoutOf is the reading of a clone's main checkout.
func checkoutOf(t *testing.T, dir string) Checkout {
	t.Helper()
	return read(t, "project-a", dir).Repos[0].Checkouts[0]
}

// inOrder fails unless every one of want is in got, in that order.
func inOrder(t *testing.T, got string, want []string) {
	t.Helper()
	at := 0
	for _, wl := range want {
		i := strings.Index(got[at:], wl)
		if i < 0 {
			t.Fatalf("missing or out of order: %q in\n%s", wl, got)
		}
		at += i + len(wl)
	}
}

func blocked(t *testing.T, p action.Plan, why string) {
	t.Helper()
	if !p.Blocked || len(p.Commands) != 0 {
		t.Fatalf("want a blocked plan with no commands: blocked=%v %v\n%s", p.Blocked, p.Lines(), notes(p))
	}
	// What blocks it comes last, under its heading, so it is the end of the screen.
	n := notes(p)
	at := strings.Index(n, BlockedHeading)
	if at < 0 || !strings.Contains(n[at:], why) {
		t.Fatalf("notes do not end by saying %q:\n%s", why, n)
	}
}

// addWorktree makes a linked worktree of the test clone in dir, on a new branch: a fixture of the
// giving or receiving machine's own, in the test's temporary folder.
func (w *world) addWorktree(dir, branch, path string) {
	w.t.Helper()
	w.sh(dir, `git worktree "$1" -q -b "$2" "$3"`, "add", branch, path)
}

// CONN-HANDOFF-001, 002 at the unit level: a commit on the giving machine is pushed and the
// receiving checkout is put at it, after checks that change nothing. Fails if the checks do not
// come first, the push is missing, or the receiving checkout is not at the commit after the run.
func TestHandsACommitOver(t *testing.T) {
	w := newWorld(t)
	c := w.commit(w.a, "two")
	p := Build(side("this machine", read(t, "project-a", w.a)), side("the engine", read(t, "project-a", w.b)))
	inOrder(t, strings.Join(p.Lines(), "\n"), []string{"engineroom handoff verify", "engineroom handoff verify", "push --dry-run -q origin main", "push origin main", "switch -q -C main " + c})
	if !run(t, p) {
		t.Fatal("the handoff failed")
	}
	if got := w.head(w.github, "main"); got != c {
		t.Fatalf("GitHub's main is %s, want %s", got, c)
	}
	if got := w.head(w.b, "HEAD"); got != c {
		t.Fatalf("the receiving checkout is at %s, want %s", got, c)
	}
}

// Idempotency: a handover run twice hands nothing over the second time when there was only
// committed work, and loses nothing when there was uncommitted work: the second run makes the
// receiving checkout match the giving one again, now clean, so the work carried the first time is
// set aside in the receiving machine's stash list rather than dropped. Fails if the second plan
// pushes or moves anything for committed work, or the carried work is not in the stash list.
func TestASecondHandoffChangesNothingAndLosesNothing(t *testing.T) {
	w := newWorld(t)
	w.commit(w.a, "two")
	if !run(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))) {
		t.Fatal("the first handoff failed")
	}
	second := Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))
	if len(second.Commands) != 0 || second.Notes[0] != "Nothing needs handing over." {
		t.Fatalf("second handoff: %v %v", second.Lines(), second.Notes)
	}
	w.sh(w.a, `echo edit >> f`)
	for i := 0; i < 2; i++ {
		if !run(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))) {
			t.Fatalf("handover %d failed", i+1)
		}
	}
	if aside := w.sh(w.b, `git stash show -p stash@{0}`); !strings.Contains(aside, "+edit") {
		t.Fatalf("the work carried the first time is not in the stash list: %q", aside)
	}
}

// CONN-HANDOFF-016 at the unit level: the receiving checkout ends in the giving checkout's state,
// whatever it held: its own work (a changed file and a new file, on main) is set aside in its stash
// list labelled with its branch, then it is put on the giving branch at the giving commit, and the
// giving checkout's uncommitted work (a changed file and a new file) is applied. The giving
// checkout keeps a copy in its stash list. Fails if the two checkouts' states differ afterwards
// (branch, commit, every change), the receiving work is lost, or the giver keeps the work in its
// folder.
func TestTheReceivingCheckoutTakesTheGivingState(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `git switch -q -c feature && echo f >> f && git commit -qam feature && git push -q origin feature && echo edit >> f && echo new > new.txt`)
	w.sh(w.b, `echo mine >> f && echo own > own.txt`)
	giver := read(t, "project-a", w.a)
	want := giver.Repos[0].Checkouts[0]
	p := Build(engineSide(giver), side("this machine", read(t, "project-a", w.b)))
	if n := notes(p); !strings.Contains(n, "project-a: its own 2 changes (M f, ?? own.txt) on this machine set aside in its stash list, labelled \"work on main\"; switched from main to feature at") || !strings.Contains(n, "2 changes (M f, ?? new.txt) carried from the engine") {
		t.Fatalf("notes: %s", n)
	}
	if !run(t, p) {
		t.Fatal("the handover failed")
	}
	got := checkoutOf(t, w.b)
	if got.Branch != want.Branch || got.Head != want.Head || got.Fingerprint != want.Fingerprint {
		t.Fatalf("the receiving checkout is %s at %s (%v), want %s at %s (%v)", got.Branch, got.Head, got.Changes, want.Branch, want.Head, want.Changes)
	}
	if list := w.sh(w.b, `git stash list`); !strings.Contains(list, "engineroom: work on main set aside before the handover from the engine") {
		t.Fatalf("the receiving checkout's own work is not in its stash list: %q", list)
	}
	if aside := w.sh(w.b, `git stash show --include-untracked --name-only stash@{0}`); aside != "f\nown.txt" {
		t.Fatalf("set aside: %q", aside)
	}
	if st := w.sh(w.a, `git status --porcelain`); st != "" {
		t.Fatalf("the giving checkout still has: %q", st)
	}
	if list := w.sh(w.a, `git stash list`); !strings.Contains(list, "engineroom: handover of feature to the client") {
		t.Fatalf("the giving machine keeps no copy: %q", list)
	}
	for _, dir := range []string{w.a, w.b} {
		if refs := w.sh(dir, `git for-each-ref refs/engineroom/`); refs != "" {
			t.Fatalf("a handover ref is left in %s: %q", dir, refs)
		}
	}
}

// Going back to the engine, the work travels the other way: stashed here, pushed straight to the
// engine's repository over SSH (never to GitHub), and applied there. Fails if a step is missing or
// out of order, the work goes to GitHub, or the engine's checkout does not end in this state.
func TestUncommittedWorkIsCarriedToTheEngine(t *testing.T) {
	w := newWorld(t)
	a, b := resolved(t, w.a), resolved(t, w.b)
	w.sh(w.a, `echo edit >> f && echo new > fresh.txt`)
	giver := read(t, "project-a", w.a)
	p := Build(side("this machine", giver), engineSide(read(t, "project-a", w.b)))
	ref := HandoverRef("main")
	got := strings.Join(p.Lines(), "\n")
	inOrder(t, got, []string{
		"engineroom handoff pack " + a + " " + ref + " --label 'engineroom: handover of main to the engine'",
		"git -C " + a + " -c 'core.sshCommand=ssh -o BatchMode=yes",
		"push -q engine.example:" + b + " +" + ref + ":" + ref,
		"engineroom handoff unpack " + b + " " + ref,
	})
	if strings.Contains(got, "push origin") {
		t.Fatalf("something goes to GitHub: %s", got)
	}
	if !run(t, p) {
		t.Fatal("the handover failed")
	}
	if checkoutOf(t, w.b).Fingerprint != giver.Repos[0].Checkouts[0].Fingerprint {
		t.Fatalf("the engine's checkout differs: %v", checkoutOf(t, w.b).Changes)
	}
}

// Finder's .DS_Store files and the .claude folder (Claude's agent memory) belong to the machine:
// they are neither carried nor set aside, and stay where they are on both machines. Fails if
// either moves, or counts as work.
func TestTheMachinesOwnFilesStay(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `echo edit >> f && touch .DS_Store && mkdir -p sub .claude/agent-memory && touch sub/.DS_Store .claude/agent-memory/notes.md`)
	w.sh(w.b, `touch .DS_Store && mkdir -p .claude && touch .claude/settings.local.json`)
	giver := read(t, "project-a", w.a)
	if ch := giver.Repos[0].Checkouts[0].Changes; len(ch) != 1 || ch[0] != "M f" {
		t.Fatalf("changes read: %v", ch)
	}
	if !run(t, Build(engineSide(giver), side("this machine", read(t, "project-a", w.b)))) {
		t.Fatal("the handover failed")
	}
	for _, f := range []string{".DS_Store", "sub/.DS_Store", ".claude/agent-memory/notes.md"} {
		if _, err := os.Stat(filepath.Join(w.a, f)); err != nil {
			t.Fatalf("%s left the giving machine", f)
		}
	}
	if _, err := os.Stat(filepath.Join(w.b, ".claude", "agent-memory")); err == nil {
		t.Fatal("the agent memory travelled")
	}
	if _, err := os.Stat(filepath.Join(w.b, ".claude", "settings.local.json")); err != nil {
		t.Fatal("the receiving machine's own .claude file was set aside")
	}
	if list := w.sh(w.b, `git stash list`); list != "" {
		t.Fatalf("the receiving machine's own files were set aside: %q", list)
	}
}

// The handover is all or nothing: one checkout that cannot be put in the giving state blocks the
// whole plan, which then has no commands, and says what to clear. Each case is one blocker. Fails
// if any case plans a command, or does not say why.
func TestAnythingThatCannotBeDoneBlocksTheWholeHandover(t *testing.T) {
	cases := map[string]struct {
		setup func(w *world)
		why   string
	}{
		"a receiving branch with commits of its own": {func(w *world) { w.commit(w.b, "theirs") }, "of its own that GitHub does not"},
		"a branch both sides moved on": {func(w *world) {
			w.commit(w.b, "on github")
			w.sh(w.b, `git push -q origin main && git reset -q --hard HEAD~1`)
			w.commit(w.a, "mine")
			w.sh(w.a, `git fetch -q origin`)
		}, "have both moved on"},
		"a release branch with commits": {func(w *world) {
			w.sh(w.a, `git switch -q -c release/1.2.0 && echo r >> f && git commit -qam r`)
		}, "A handover never pushes a release branch"},
		"a detached commit": {func(w *world) { w.sh(w.a, `git switch -q --detach`) }, "is on a detached commit"},
		"two repositories under one name": {func(w *world) {
			w.sh(w.b, `git remote set-url origin git@github.com:someone-else/project-a.git`)
		}, "different repositories"},
		"the branch checked out elsewhere there": {func(w *world) {
			w.sh(w.a, `git switch -q -c work && git push -q origin work`)
			w.sh(w.b, `git fetch -q origin && git branch -q work origin/work`)
			w.sh(w.b, `git worktree "$1" -q ../elsewhere work`, "add")
		}, "is checked out in"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.commit(w.a, "something to hand over")
			c.setup(w)
			blocked(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b))), c.why)
		})
	}
}

// A blocked handover changes nothing on either machine. Fails if GitHub or either machine's
// checkout changes when it is run.
func TestABlockedHandoverChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.commit(w.a, "mine")
	w.sh(w.a, `echo edit >> f`)
	w.commit(w.b, "theirs")
	before := []string{w.head(w.github, "main"), w.sh(w.a, `git status --porcelain`), w.head(w.b, "HEAD")}
	run(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b))))
	after := []string{w.head(w.github, "main"), w.sh(w.a, `git status --porcelain`), w.head(w.b, "HEAD")}
	if strings.Join(before, "|") != strings.Join(after, "|") {
		t.Fatalf("a blocked handover changed something: %v -> %v", before, after)
	}
}

// Partial failure, before anything changes: a commit or an edit made on either machine after the
// handover was planned stops it at its first step, and GitHub and both checkouts are as they were.
// Fails if anything was pushed, stashed or moved.
func TestAChangeSinceThePlanStopsTheHandoverBeforeAnythingChanges(t *testing.T) {
	cases := map[string]func(w *world){
		"an edit on the giving machine":     func(w *world) { w.sh(w.a, `echo later >> f`) },
		"a commit on the receiving machine": func(w *world) { w.sh(w.b, `echo later >> f && git commit -qm later f`) },
	}
	state := func(w *world) string {
		return strings.Join([]string{w.head(w.github, "main"), w.sh(w.a, `git status --porcelain; git stash list`), w.sh(w.b, `git rev-parse HEAD; git status --porcelain; git stash list`)}, "|")
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.commit(w.a, "mine")
			w.sh(w.a, `echo edit >> f`)
			w.sh(w.b, `echo own > own.txt`)
			p := Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))
			change(w)
			before := state(w)
			if run(t, p) {
				t.Fatal("the handover ran though something changed since it was planned")
			}
			if after := state(w); after != before {
				t.Fatalf("something changed:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

// A push GitHub would refuse stops the handover at its dry run, before any push: with two branches
// to send, GitHub having moved on the second leaves the first unsent too, and nothing else
// changed. Fails if either branch reaches GitHub, or the receiving checkout changes.
func TestAPushGitHubWouldRefuseStopsTheHandoverBeforeAnyPush(t *testing.T) {
	w := newWorld(t)
	linked := filepath.Join(filepath.Dir(w.a), "project-a-work")
	w.addWorktree(w.a, "work", linked)
	w.sh(linked, `git push -q origin work`)
	w.commit(w.a, "main work")
	w.commit(linked, "more work")
	p := Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))
	// GitHub's work moves on after the plan was read, from a third clone.
	w.sh(filepath.Dir(w.b), `git clone -q "$1" third && cd third && git switch -q work && echo gh > gh.txt && git add gh.txt && git commit -qm gh && git push -q origin work`, w.github)
	mainBefore, bBefore := w.head(w.github, "main"), w.head(w.b, "HEAD")
	if run(t, p) {
		t.Fatal("the handover ran though GitHub would refuse a push")
	}
	if w.head(w.github, "main") != mainBefore || w.head(w.b, "HEAD") != bBefore {
		t.Fatal("something was pushed or moved before the refused push was found")
	}
}

// A receiving checkout behind GitHub is brought to the giving commit even when nothing is pushed.
// Fails if it is not moved.
func TestAReceivingCheckoutBehindGitHubIsBroughtUp(t *testing.T) {
	w := newWorld(t)
	c := w.commit(w.a, "pushed earlier")
	w.sh(w.a, `git push -q origin main`)
	p := Build(side("this machine", read(t, "project-a", w.a)), side("the engine", read(t, "project-a", w.b)))
	if !run(t, p) || w.head(w.b, "HEAD") != c {
		t.Fatalf("the checkout behind GitHub was not brought up: %v %v", p.Lines(), p.Notes)
	}
}

// A giving checkout behind GitHub (GitHub has commits it never took) is matched as it is when the
// receiving checkout is further behind: it is brought forward to the same commit. Fails if the
// receiving checkout is not at the giving commit.
func TestAGivingCheckoutBehindGitHubIsMatchedFromFurtherBehind(t *testing.T) {
	w := newWorld(t)
	old := w.head(w.b, "HEAD")
	w.commit(w.b, "first on github")
	w.sh(w.b, `git push -q origin main`)
	w.sh(w.a, `git pull -q --ff-only origin main`)
	given := w.head(w.a, "HEAD")
	w.commit(w.b, "second on github")
	w.sh(w.b, `git push -q origin main && git checkout -q -B main "$1"`, old)
	w.sh(w.a, `git fetch -q origin`)
	if !run(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))) {
		t.Fatal("the handover failed")
	}
	if got := w.head(w.b, "HEAD"); got != given {
		t.Fatalf("the receiving checkout is at %s, want the giving %s", got, given)
	}
}

// A giving checkout behind GitHub, where the receiving checkout has more of GitHub's commits (work
// done there since, and pushed), would move the receiving machine back: the handover blocks, saying
// how to bring the giving machine up first. Fails if it plans anything.
func TestAReceivingCheckoutAheadOfTheGivingOneBlocks(t *testing.T) {
	w := newWorld(t)
	w.commit(w.b, "newer work on the receiving machine")
	w.sh(w.b, `git push -q origin main`)
	w.sh(w.a, `git fetch -q origin`)
	blocked(t, Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b))), "so taking the engine's state would move this machine back")
	// The same when the giving machine never fetched GitHub's newer commits.
	w2 := newWorld(t)
	w2.commit(w2.b, "newer work on the receiving machine")
	w2.sh(w2.b, `git push -q origin main`)
	blocked(t, Build(engineSide(read(t, "project-a", w2.a)), side("this machine", read(t, "project-a", w2.b))), "so taking the engine's state would move this machine back")
}

// A branch checked out on the giving machine that GitHub has never had is pushed, even with no
// commits of its own (new work, not yet worked on), so the receiving machine can take it; a
// branch in no checkout is not. Fails on either.
func TestNewBranches(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `git branch empty-work && git switch -q -c real-work`)
	p := Build(side("this machine", read(t, "project-a", w.a)), side("the engine", read(t, "project-a", w.b)))
	text := strings.Join(p.Lines(), "\n")
	if !strings.Contains(text, "push origin real-work") || strings.Contains(text, "empty-work") {
		t.Fatalf("plan: %s", text)
	}
}

// CONN-HANDOFF-005 at the unit level: a giving machine that could not be read plans nothing and
// says what could not be checked. Fails if a command is planned or the note is missing.
func TestAnUnreadGivingMachineIsSaid(t *testing.T) {
	w := newWorld(t)
	p := Build(Side{Name: "the engine", Unread: "did not answer"}, side("this machine", read(t, "project-a", w.b)))
	if len(p.Commands) != 0 || !strings.Contains(notes(p), "The engine did not answer, so it could not be checked") {
		t.Fatalf("plan: %v %s", p.Lines(), notes(p))
	}
}

// With the receiving machine unread, the giving machine's work still goes to GitHub, and its
// uncommitted work is said to stay. Fails if the push is missing or anything is carried.
func TestAnUnreadReceivingMachineGetsOnlyThePushes(t *testing.T) {
	w := newWorld(t)
	w.commit(w.a, "two")
	w.sh(w.a, `echo edit >> f`)
	p := Build(side("this machine", read(t, "project-a", w.a)), Side{Name: "the engine", Unread: "did not answer"})
	if got := strings.Join(p.Lines(), "\n"); !strings.Contains(got, "push origin main") || strings.Contains(got, "handoff pack") || len(p.Commands) != 1 {
		t.Fatalf("plan: %s", got)
	}
	if !strings.Contains(notes(p), "project-a: its 1 change (M f) on this machine stays there.") {
		t.Fatalf("notes: %s", notes(p))
	}
}

// Malformed state: a registered folder that is gone, or is not a repository, or has no origin,
// is named and never handed over. Fails if a command is planned for it.
func TestUnusableProjectsAreNamed(t *testing.T) {
	w := newWorld(t)
	plain := t.TempDir()
	noOrigin := filepath.Join(t.TempDir(), "x")
	w.sh(plain, `git init -q "$1"`, noOrigin)
	st := &State{Repos: []Repo{
		readRepo(config.Project{Name: "gone", Path: filepath.Join(plain, "missing")}),
		readRepo(config.Project{Name: "plain", Path: plain}),
		readRepo(config.Project{Name: "no-origin", Path: noOrigin}),
	}}
	p := Build(side("this machine", st), side("the engine", &State{}))
	n := notes(p)
	for _, want := range []string{"does not exist", "is not a git repository", "no remote called origin"} {
		if !strings.Contains(n, want) {
			t.Fatalf("missing %q in:\n%s", want, n)
		}
	}
	if len(p.Commands) != 0 {
		t.Fatalf("commands for unusable projects: %v", p.Lines())
	}
}

// Dependency down: GitHub cannot be read, so the branches are compared with what was last
// fetched, and the note says so. Fails if the reading gives up or the note is missing.
func TestGitHubUnreadable(t *testing.T) {
	w := newWorld(t)
	w.commit(w.a, "two")
	if err := os.Rename(w.github, w.github+".away"); err != nil {
		t.Fatal(err)
	}
	st := read(t, "project-a", w.a)
	if st.Repos[0].GitHubProblem == "" || st.Repos[0].Branches[0].Ahead != 1 {
		t.Fatalf("reading: %+v", st.Repos[0])
	}
	p := Build(side("this machine", st), side("the engine", read(t, "project-a", w.b)))
	if !strings.Contains(notes(p), "GitHub could not be read") {
		t.Fatalf("notes: %s", notes(p))
	}
}

// Only the work checked out is pushed (CONN-HANDOFF-007): a branch with commits GitHub lacks in no
// checkout (a backup) is named with the command to push it and never pushed, while the main
// clone's own branch is. Fails if the backup is pushed, its note or command is missing, or the
// checked-out branch is left behind.
func TestABranchInNoCheckoutIsNamedNotPushed(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `git switch -q -c backup && echo b >> f && git commit -qam b && git switch -q main && echo m >> f && git commit -qam m`)
	p := Build(side("this machine", read(t, "project-a", w.a)), side("the engine", read(t, "project-a", w.b)))
	got := strings.Join(p.Lines(), "\n")
	if !strings.Contains(got, "project-a push origin main") || strings.Contains(got, "backup") {
		t.Fatalf("plan: %s", got)
	}
	// Folded into one line for every project, with the command's shape.
	if !strings.Contains(notes(p), "Left as they are on this machine, in no checkout and with commits GitHub lacks (send one with git -C <its project's folder> push origin <branch>): project-a: backup.") {
		t.Fatalf("notes: %s", notes(p))
	}
	if !run(t, p) {
		t.Fatal("the plan failed")
	}
	if out := w.sh(w.a, `git ls-remote --heads origin backup`); strings.Contains(out, "backup") {
		t.Fatal("GitHub has the backup branch")
	}
}

// A linked worktree on the giving machine is made on the receiving machine beside its main clone,
// on the same branch at the same commit, and its branch is pushed from the worktree. Fails if the
// worktree is not made where it belongs, or is on another commit.
func TestALinkedWorktreeIsMadeOnTheReceivingMachine(t *testing.T) {
	w := newWorld(t)
	linked := filepath.Join(filepath.Dir(w.a), "project-a-work")
	w.addWorktree(w.a, "work", linked)
	c := w.commit(linked, "w")
	p := Build(side("this machine", read(t, "project-a", w.a)), side("the engine", read(t, "project-a", w.b)))
	dest := filepath.Join(filepath.Dir(resolved(t, w.b)), "project-a-work")
	inOrder(t, strings.Join(p.Lines(), "\n"), []string{resolved(t, linked) + " push origin work", "add -q -B work " + dest + " " + c})
	// The project has no settings here, so the last step, its secret files, has nothing to
	// restore; the worktree itself is what is checked.
	p.Commands = p.Commands[:len(p.Commands)-1]
	if !run(t, p) {
		t.Fatal("the handover failed")
	}
	if got := w.head(dest, "HEAD"); got != c {
		t.Fatalf("the worktree is at %s, want %s", got, c)
	}
}

// A worktree away from its main clone (a tool's scratch copy in a temporary folder) is not one of
// the project's jobs: it is said and left where it is, and the rest is handed over. Fails if it
// blocks the handover, or a command makes it on the receiving machine.
func TestAWorktreeAwayFromItsMainCloneIsLeftWhereItIs(t *testing.T) {
	w := newWorld(t)
	scratch := filepath.Join(t.TempDir(), "scratchpad", "copy")
	w.addWorktree(w.a, "scratch", scratch)
	c := w.commit(w.a, "real work")
	p := Build(engineSide(read(t, "project-a", w.a)), side("this machine", read(t, "project-a", w.b)))
	if p.Blocked {
		t.Fatalf("blocked: %s", notes(p))
	}
	if !strings.Contains(notes(p), "is not beside its main clone, so it is not one of the project's jobs and is left where it is") {
		t.Fatalf("notes: %s", notes(p))
	}
	if got := strings.Join(p.Lines(), "\n"); strings.Contains(got, "-B scratch") {
		t.Fatalf("the scratch worktree is made on the receiving machine:\n%s", got)
	}
	if !run(t, p) || w.head(w.b, "HEAD") != c {
		t.Fatal("the rest was not handed over")
	}
}

// A project the receiving machine lacks is cloned to the same place under its home, registered,
// and each checkout is put in the giving state there like any other: the clone on the giving
// branch at its commit, the linked worktree made beside it. Fails if a step is missing or out of
// order, or the clone goes elsewhere.
func TestAMissingProjectIsMirrored(t *testing.T) {
	w := newWorld(t)
	home := resolved(t, filepath.Dir(filepath.Dir(w.a)))
	a := resolved(t, w.a)
	linked := filepath.Join(home, "a", "project-a-work")
	w.addWorktree(a, "work", linked)
	w.commit(linked, "w")
	st := read(t, "project-a", a)
	st.Home = home
	st.Repos[0].Category, st.Repos[0].Slots = "tools", true
	main := w.head(a, "HEAD")
	p := Build(engineSide(st), side("this machine", &State{Home: "/Users/other"}))
	dest := "/Users/other/a/project-a"
	inOrder(t, strings.Join(p.Lines(), "\n"), []string{
		"git -C " + linked + " push origin work",
		"git clone -q " + w.github + " " + dest,
		"engineroom project add " + dest + " --name project-a --category tools",
		"engineroom secrets restore project-a",
		"git -C " + dest + " switch -q -C main " + main,
		"add -q -B work /Users/other/a/project-a-work",
		"engineroom slot claim --worktree /Users/other/a/project-a-work",
		"engineroom secrets restore project-a --worktree /Users/other/a/project-a-work",
	})
	if !strings.Contains(notes(p), "project-a: copied to "+dest+" on this machine, as on the engine, and registered; put on main at") {
		t.Fatalf("notes: %s", notes(p))
	}
}

// A project the receiving machine has not registered, but whose clone is already at the place the
// mirror would copy it to, is fetched and registered there, not cloned over, and anything of its
// own is set aside; a folder there holding something else blocks the handover. Fails if the clone
// is attempted over the folder, the clone is not fetched, registered and set aside, or a folder of
// another repository does not block.
func TestAnExistingCloneIsRegisteredNotClonedOver(t *testing.T) {
	w := newWorld(t)
	home := resolved(t, filepath.Dir(filepath.Dir(w.a)))
	st := read(t, "project-a", resolved(t, w.a))
	st.Home = home
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	w.sh(home, `git clone -q "$1" "$2/a/project-a"`, w.github, other)
	receiver := side("this machine", &State{Home: other})
	receiver.Folder = LocalFolder
	p := Build(engineSide(st), receiver)
	got := strings.Join(p.Lines(), "\n")
	dest := filepath.Join(other, "a", "project-a")
	if strings.Contains(got, "git clone") {
		t.Fatalf("the clone already there is cloned over:\n%s", got)
	}
	inOrder(t, got, []string{dest + " fetch -q origin", "engineroom project add " + dest + " --name project-a", "engineroom handoff aside " + dest})
	w.sh(home, `git -C "$1/a/project-a" remote set-url origin git@github.com:owner/elsewhere.git`, other)
	blocked(t, Build(engineSide(st), receiver), "is a clone of git@github.com:owner/elsewhere.git")
}

// A release has no other machine to carry uncommitted work to, and work in progress does not go
// to GitHub: a worktree with uncommitted changes is said, and committing it as partial work is
// offered; nothing in the plan touches it. Fails if the offer is missing or a command touches it.
func TestAReleaseOffersUncommittedWorkAsPartialWork(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `echo edit >> f`)
	p := Build(side("the engine", read(t, "project-a", w.a)), Side{Name: "GitHub", State: &State{}, PushOnly: true})
	if len(p.Commands) != 0 || !strings.Contains(notes(p), "has uncommitted changes, which a release does not send to GitHub") {
		t.Fatalf("plan: %v %s", p.Lines(), notes(p))
	}
	if act := p.Offers; len(act) != 1 || act[0].Action[0] != "commit-partial" || act[0].Action[1] != "project-a" || !strings.HasSuffix(act[0].Action[2], "/a/project-a") {
		t.Fatalf("offers: %+v", p.Offers)
	}
}

// Projects are handed over in name order, ignoring case, whatever order they were registered or
// sent in, so the plan reads like every other list of projects. Fails if the pushes or the moves
// follow the order the machines sent, or an order that minds case.
func TestProjectsAreHandedOverInNameOrder(t *testing.T) {
	ahead := func(name string) Repo {
		return Repo{Project: name, Path: "/p/" + name, Remote: "git@github.com:owner-a/" + name + ".git",
			Branches:  []Branch{{Name: "main", Head: "b", GitHub: "a", Ahead: 1}},
			Checkouts: []Checkout{{Path: "/p/" + name, Branch: "main", Head: "b"}}}
	}
	behind := func(r Repo) Repo {
		r.Path = "/q/" + r.Project
		r.Branches = []Branch{{Name: "main", Head: "a", GitHub: "b", Behind: 1}}
		r.Checkouts = []Checkout{{Path: r.Path, Branch: "main", Head: "a"}}
		return r
	}
	from := &State{Repos: []Repo{ahead("zeta"), ahead("Beta"), ahead("alpha")}}
	to := &State{Repos: []Repo{behind(ahead("zeta")), behind(ahead("Beta")), behind(ahead("alpha"))}}
	p := Build(side("this machine", from), side("the engine", to))
	inOrder(t, strings.Join(p.Lines(), "\n"), []string{
		"/p/alpha push origin", "/p/Beta push origin", "/p/zeta push origin",
		"/q/alpha switch", "/q/Beta switch", "/q/zeta switch",
	})
}

// The same repository reached through different addresses is one repository; a different owner
// or name is not. Fails if an SSH host alias, https, or a rewritten local path is told apart from
// the same owner/name, or two repositories are mixed.
func TestSameRemote(t *testing.T) {
	same := [][2]string{
		{"git@github.com:owner-a/project-a.git", "https://github.com/owner-a/project-a"},
		{"git@github-work:owner-a/project-a.git", "git@github.com:Owner-A/project-a"},
		{"engine:/home/dev/github/owner-a/project-a.git", "/home/dev/github/owner-a/project-a.git/"},
	}
	for _, p := range same {
		if !sameRemote(p[0], p[1]) {
			t.Errorf("%s and %s should be the same repository", p[0], p[1])
		}
	}
	differ := [][2]string{
		{"git@github.com:owner-a/project-a.git", "git@github.com:owner-b/project-a.git"},
		{"git@github.com:owner-a/project-a.git", "git@github.com:owner-a/project-b.git"},
	}
	for _, p := range differ {
		if sameRemote(p[0], p[1]) {
			t.Errorf("%s and %s should differ", p[0], p[1])
		}
	}
}
