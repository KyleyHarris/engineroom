//go:build journeys

package journeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// startWork sets up a piece of work directly with git (not through the program under test):
// a sibling worktree on its own branch with one commit, published to the remote, and a
// running session in it. It returns the worktree's path.
func startWork(t *testing.T, m *journey.Machine, clone, work string) string {
	t.Helper()
	dir := publishWork(t, m, clone, work)
	m.StartSession("project-a-"+work, dir)
	return dir
}

// publishWork is startWork without the session: the worktree, its commit and its pushed branch.
func publishWork(t *testing.T, m *journey.Machine, clone, work string) string {
	t.Helper()
	dir := clone + "-" + work
	m.Git(clone, "worktree", "add", "-q", "-b", work, dir, "origin/main")
	m.WriteFile(filepath.Join(dir, work+".txt"), work+"\n", 0o644)
	m.Git(dir, "add", ".")
	m.Git(dir, "commit", "-qm", "work on "+work)
	m.Git(dir, "push", "-q", "-u", "origin", work)
	return dir
}

// startSavedSession starts a session in dir with the program's standard layout, as new work
// does, and waits until Zellij has saved it. A real session that has run a while is saved, and a
// saved session that is only stopped is still listed, as ended; a bare session that was never
// saved disappears entirely when stopped, so it cannot show whether a stop left anything behind.
func startSavedSession(t *testing.T, m *journey.Machine, name, dir string) {
	t.Helper()
	layout := filepath.Join(m.Home, "standard.kdl")
	if out, err := m.Run("engineroom", "layout", "write", "--out", layout); err != nil {
		t.Fatalf("writing the standard layout: %v\n%s", err, out)
	}
	if out, err := m.Run("zellij", "attach", "--create-background", name, "options", "--default-layout", layout, "--default-cwd", dir); err != nil {
		t.Fatalf("creating session %s: %v\n%s", name, err, out)
	}
	m.WaitSession(name, "running")
	m.WaitSerialized(t, name, `name="agent"`)
}

// mergeOnRemote merges the work's branch into the remote's main, as a merged pull request
// would, from a separate clone.
func mergeOnRemote(t *testing.T, m *journey.Machine, remote, work string) {
	t.Helper()
	other := filepath.Join(m.Home, "merger")
	m.Git(m.Home, "clone", "-q", remote, other)
	m.Git(other, "merge", "-q", "--no-ff", "-m", "merge "+work, "origin/"+work)
	m.Publish(other, "main")
}

// cleanAndConfirm asks to clean up the work and confirms at the preview.
func cleanAndConfirm(t *testing.T, m *journey.Machine, work string) *termtest.Terminal {
	t.Helper()
	term := m.Start(t, "clean", "project-a", work)
	term.WaitFor("Nothing has run yet.")
	term.Press(termtest.Enter)
	term.WaitFor("Done.")
	return term
}

func remoteHas(m *journey.Machine, remote, branch string) bool {
	return m.Git(remote, "branch", "--list", branch) != ""
}

// WORK-CLEAN-001: clean up asks, then stops the session and removes the worktree.
func TestWorkClean001(t *testing.T) {
	m, clone, _ := workProject(t)
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean001{
		GivenNewWorkWhoseChangesAreAllCommittedAndPushedHasARunningSession: func() {
			// A saved session, as any real one is, so a clean up that only stops it and leaves
			// it listed as ended fails the Then below (D-015: a session is running or it is not
			// there).
			dir = publishWork(t, m, clone, "done-work")
			startSavedSession(t, m, "project-a-done-work", dir)
		},
		WhenTheDeveloperCleansItUpAndConfirms: func() {
			term = m.Start(t, "clean", "project-a", "done-work")
			term.WaitFor("Nothing has run yet.")
			if journey.SessionState(m.Sessions(), "project-a-done-work") != "running" {
				t.Fatalf("something ran before confirming:\n%s", m.Sessions())
			}
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenItsSessionIsStopped: func() {
			if state := journey.SessionState(m.Sessions(), "project-a-done-work"); state != "" {
				t.Fatalf("the session is still %s:\n%s", state, m.Sessions())
			}
		},
		AndItsCheckoutIsRemoved: func() {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the worktree is still there: %v", err)
			}
			if strings.Contains(m.Git(clone, "worktree", "list"), "done-work") {
				t.Fatalf("git still lists the worktree")
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-002: uncommitted changes refuse the clean up, naming the file.
func TestWorkClean002(t *testing.T) {
	m, clone, _ := workProject(t)
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean002{
		GivenNewWorkHasAFileChangedButNotCommitted: func() {
			dir = startWork(t, m, clone, "wip")
			m.WriteFile(filepath.Join(dir, "notes.md"), "unsaved thoughts\n", 0o644)
		},
		WhenTheDeveloperCleansItUp: func() {
			term = m.Start(t, "clean", "project-a", "wip")
		},
		ThenTheyAreToldItHasUncommittedChangesNamingTheFile: func() {
			term.WaitFor("has uncommitted changes")
			term.WaitFor("notes.md")
		},
		AndNothingIsStoppedOrRemoved: func() {
			untouched(t, m, dir, "project-a-wip")
		},
	}, term0(&term))
}

// WORK-CLEAN-003: commits that were never pushed refuse the clean up.
func TestWorkClean003(t *testing.T) {
	m, clone, _ := workProject(t)
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean003{
		GivenNewWorkHasACommitThatIsNotOnTheRemote: func() {
			dir = startWork(t, m, clone, "local-only")
			m.WriteFile(filepath.Join(dir, "more.txt"), "more\n", 0o644)
			m.Git(dir, "add", ".")
			m.Git(dir, "commit", "-qm", "never pushed")
		},
		WhenTheDeveloperCleansItUp: func() {
			term = m.Start(t, "clean", "project-a", "local-only")
		},
		ThenTheyAreToldItHasCommitsThatWereNeverPushed: func() {
			term.WaitFor("commit(s) that were never pushed")
		},
		AndNothingIsStoppedOrRemoved: func() {
			untouched(t, m, dir, "project-a-local-only")
		},
	}, term0(&term))
}

// untouched fails if the worktree or its session changed.
func untouched(t *testing.T, m *journey.Machine, dir, session string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the worktree was removed: %v", err)
	}
	if journey.SessionState(m.Sessions(), session) != "running" {
		t.Fatalf("the session was stopped:\n%s", m.Sessions())
	}
}

// WORK-CLEAN-004: a merged branch goes with its worktree, here and on the remote.
func TestWorkClean004(t *testing.T) {
	m, clone, remote := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean004{
		GivenNewWorkWhoseBranchHasBeenMergedIntoMain: func() {
			startWork(t, m, clone, "merged-work")
			mergeOnRemote(t, m, remote, "merged-work")
		},
		WhenTheDeveloperCleansItUpAndConfirms: func() {
			term = cleanAndConfirm(t, m, "merged-work")
		},
		ThenItsBranchNoLongerExistsOnThisMachineOrOnTheRemote: func() {
			if m.Git(clone, "branch", "--list", "merged-work") != "" {
				t.Fatal("the branch still exists here")
			}
			if remoteHas(m, remote, "merged-work") {
				t.Fatal("the branch still exists on the remote")
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-005: an unmerged branch survives its worktree, here and on the remote.
func TestWorkClean005(t *testing.T) {
	m, clone, remote := workProject(t)
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean005{
		GivenNewWorkWhoseBranchIsPushedButNotMergedIntoMain: func() {
			dir = startWork(t, m, clone, "open-work")
		},
		WhenTheDeveloperCleansItUpAndConfirms: func() {
			term = cleanAndConfirm(t, m, "open-work")
		},
		ThenItsCheckoutIsRemoved: func() {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the worktree is still there: %v", err)
			}
		},
		AndItsBranchStillExistsOnThisMachineAndOnTheRemote: func() {
			if m.Git(clone, "branch", "--list", "open-work") == "" {
				t.Fatal("the unmerged branch was deleted here")
			}
			if !remoteHas(m, remote, "open-work") {
				t.Fatal("the unmerged branch was deleted on the remote")
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-006: the main worktree is never cleaned up.
func TestWorkClean006(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean006{
		GivenAProjectIsRegistered: func() {
			m.StartSession("project-a", clone)
		},
		WhenTheDeveloperAsksToCleanUpItsMainCheckout: func() {
			term = m.Start(t, "clean", "project-a")
		},
		ThenTheyAreToldTheMainCheckoutIsNeverCleanedUp: func() {
			term.WaitFor("the main worktree of a project is never cleaned up")
		},
		AndNothingIsStoppedOrRemoved: func() {
			untouched(t, m, clone, "project-a")
			if _, err := os.Stat(filepath.Join(clone, "README.md")); err != nil {
				t.Fatalf("the main clone changed: %v", err)
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-007: the teardown script runs in the worktree before it is removed.
func TestWorkClean007(t *testing.T) {
	m, clone, _ := workProject(t)
	record := filepath.Join(m.Home, "teardown.log")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkClean007{
		GivenTheProjectDeclaresATeardownScript: func() {
			commitScript(t, m, clone, "teardown", `echo "$(pwd) $ENGINEROOM_WORK $(test -f torn.txt && echo still-there)" >> "`+record+`"
`)
			m.Git(clone, "fetch", "-q", "origin")
			dir := startWork(t, m, clone, "torn")
			m.WriteFile(filepath.Join(dir, ".gitignore"), "", 0o644)
			m.Git(dir, "add", ".")
			m.Git(dir, "commit", "-qm", "ignore")
			m.Publish(dir, "torn")
		},
		WhenTheDeveloperCleansUpNewWorkOnItAndConfirms: func() {
			term = cleanAndConfirm(t, m, "torn")
		},
		ThenTheTeardownScriptRunsInTheCheckoutBeforeItIsRemoved: func() {
			b, err := os.ReadFile(record)
			if err != nil {
				t.Fatalf("the teardown script did not run: %v", err)
			}
			line := strings.TrimSpace(string(b))
			// It ran inside the worktree (its own file was still there), for this work.
			if !strings.Contains(line, "torn still-there") || !strings.HasSuffix(strings.Fields(line)[0], "project-a-torn") {
				t.Fatalf("the teardown script did not run in the worktree before removal: %q", line)
			}
		},
	}, term0(&term))
}
