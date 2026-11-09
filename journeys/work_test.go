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

// workProject is an engine machine with project-a registered, cloned from a remote.
func workProject(t *testing.T) (*journey.Machine, string, string) {
	t.Helper()
	m := journey.NewMachine(t)
	clone, remote := m.NewRepoWithRemote("project-a")
	m.BeEngine("project-a", clone)
	return m, clone, remote
}

// commitScript adds an executable .engineroom/<name> script to the remote's main, so a new worktree
// has it, and keeps it in the main clone too.
func commitScript(t *testing.T, m *journey.Machine, clone, name, body string) {
	t.Helper()
	m.WriteFile(filepath.Join(clone, ".engineroom", name), "#!/bin/sh\n"+body, 0o755)
	m.Git(clone, "add", ".engineroom")
	m.Git(clone, "commit", "-qm", "add "+name)
	m.Publish(clone, "main")
}

// WORK-NEW-001: new work gets a sibling worktree on its own branch and a session in it.
func TestWorkNew001(t *testing.T) {
	m, clone, _ := workProject(t)
	want := clone + "-fix-login"
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew001{
		GivenAProjectIsRegistered: func() {},
		WhenTheDeveloperStartsNewWorkOnItCalledFixLogin: func() {
			term = m.Start(t, "new", "project-a", "fix-login")
		},
		ThenACheckoutNamedAfterTheProjectAndFixLoginExistsBesideTheMainOneOnANewBranchCalledFixLogin: func() {
			term.WaitFor("AGENT new conversation")
			if got := m.Git(want, "branch", "--show-current"); got != "fix-login" {
				t.Fatalf("branch %q in %s", got, want)
			}
			if !strings.Contains(m.Git(clone, "worktree", "list"), want) {
				t.Fatalf("%s is not a worktree of the main clone", want)
			}
		},
		AndASessionIsOpenInThatCheckout: func() {
			m.WaitSession("project-a-fix-login", "running")
			if !m.AgentStartedIn(want) {
				t.Fatalf("the agent did not start in %s: %q", want, m.AgentStarts())
			}
		},
	}, term0(&term))
}

// WORK-NEW-002: the new branch starts from the remote's main, including a commit this machine
// had not fetched.
func TestWorkNew002(t *testing.T) {
	m, clone, remote := workProject(t)
	var term *termtest.Terminal
	var newer string

	scenarios.Run(t, scenarios.WorkNew002{
		GivenTheRemotesMainHasACommitThisMachineHasNotFetched: func() {
			other := filepath.Join(m.Home, "other")
			m.Git(m.Home, "clone", "-q", remote, other)
			m.WriteFile(filepath.Join(other, "later.txt"), "later\n", 0o644)
			m.Git(other, "add", ".")
			m.Git(other, "commit", "-qm", "later")
			m.Publish(other, "main")
			newer = m.Git(other, "rev-parse", "HEAD")
			if m.Git(clone, "rev-parse", "origin/main") == newer {
				t.Fatal("the main clone already has the commit")
			}
		},
		WhenTheDeveloperStartsNewWorkOnTheProject: func() {
			term = m.Start(t, "new", "project-a", "next")
			term.WaitFor("AGENT new conversation")
		},
		ThenTheNewBranchStartsFromThatCommit: func() {
			if got := m.Git(clone+"-next", "rev-parse", "HEAD"); got != newer {
				t.Fatalf("the new branch starts at %s, not the remote's %s", got, newer)
			}
		},
	}, term0(&term))
}

// WORK-NEW-003: a name with a slash or a space is refused and nothing is created.
func TestWorkNew003(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew003{
		GivenAProjectIsRegistered: func() {},
		WhenTheDeveloperStartsNewWorkOnItCalledFeatureLoginPage: func() {
			term = m.Start(t, "new", "project-a", "feature/login page")
		},
		ThenTheyAreToldTheNameMayOnlyHoldLettersDigitsAnd: func() {
			term.WaitFor("may only hold letters, digits, '.', '_' and '-'")
		},
		AndNoCheckoutBranchOrSessionIsCreated: func() {
			if list := m.Git(clone, "worktree", "list"); strings.Count(list, "\n") != 0 {
				t.Fatalf("a worktree was created:\n%s", list)
			}
			if branches := m.Git(clone, "branch", "--list"); strings.TrimSpace(branches) != "* main" {
				t.Fatalf("a branch was created:\n%s", branches)
			}
			if countSessions(m.Sessions()) != 0 {
				t.Fatalf("a session was created:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// WORK-NEW-004: starting work that exists opens it and makes nothing new.
func TestWorkNew004(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew004{
		GivenNewWorkCalledFixLoginWasStartedOnTheProject: func() {
			first := m.Start(t, "new", "project-a", "fix-login")
			first.WaitFor("AGENT new conversation")
			detach(first)
			first.WaitExit()
		},
		WhenTheDeveloperStartsNewWorkCalledFixLoginAgain: func() {
			term = m.Start(t, "new", "project-a", "fix-login")
		},
		ThenTheSessionInTheExistingCheckoutIsOpened: func() {
			term.WaitFor("Zellij (project-a-fix-login)")
		},
		AndNoSecondCheckoutOrBranchIsCreated: func() {
			if list := m.Git(clone, "worktree", "list"); strings.Count(list, "\n") != 1 {
				t.Fatalf("worktrees:\n%s", list)
			}
			if countSessions(m.Sessions()) != 1 {
				t.Fatalf("sessions:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// WORK-NEW-005: the repository's create script runs in the new worktree, with the program's
// details, before the session opens, and its output is shown while it runs.
func TestWorkNew005(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew005{
		GivenTheProjectDeclaresACreateScript: func() {
			commitScript(t, m, clone, "create", `echo "preparing $ENGINEROOM_WORK for $ENGINEROOM_PROJECT"
pwd > .created-in
sleep 1
echo "database ready"
`)
		},
		WhenTheDeveloperStartsNewWorkOnIt: func() {
			term = m.Start(t, "new", "project-a", "with-db")
		},
		ThenTheCreateScriptRunsInTheNewCheckoutBeforeTheSessionOpens: func() {
			term.WaitFor("preparing with-db for project-a")
			if countSessions(m.Sessions()) != 0 {
				t.Fatalf("the session opened before the script finished:\n%s", m.Sessions())
			}
			term.WaitFor("AGENT new conversation")
			b, err := os.ReadFile(filepath.Join(clone+"-with-db", ".created-in"))
			if err != nil || resolve(strings.TrimSpace(string(b))) != resolve(clone+"-with-db") {
				t.Fatalf("the script did not run in the new worktree: %q %v", b, err)
			}
		},
		AndWhatTheScriptPrintsIsShownWhileItRuns: func() {
			// The script's first line was read from the screen above while the script was still
			// running (no session existed yet). The history keeps the command too.
			out, _ := m.Run("engineroom", "history")
			if !strings.Contains(out, "./.engineroom/create") {
				t.Fatalf("the script is not in the history:\n%s", out)
			}
		},
	}, term0(&term))
}

// WORK-NEW-006: a failing create script is reported, with its error, and no session opens.
func TestWorkNew006(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew006{
		GivenTheProjectDeclaresACreateScriptThatFails: func() {
			commitScript(t, m, clone, "create", "echo 'cannot reach the database server' >&2\nexit 4\n")
		},
		WhenTheDeveloperStartsNewWorkOnIt: func() {
			term = m.Start(t, "new", "project-a", "broken")
		},
		ThenTheyAreToldTheCheckoutWasCreatedButItsSetupFailedWithTheScriptsError: func() {
			term.WaitFor("FAILED (exit 4)")
			term.WaitFor("cannot reach the database server")
			term.WaitFor("was created, but its setup failed")
			if _, err := os.Stat(clone + "-broken"); err != nil {
				t.Fatalf("the worktree is not there: %v", err)
			}
		},
		AndNoSessionIsOpened: func() {
			if countSessions(m.Sessions()) != 0 {
				t.Fatalf("a session opened:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// WORK-NEW-007: new work starts from the remote's main, so it runs main's create script, not one
// that only a branch the main clone has checked out adds. Fails if the plan runs ./.engineroom/create
// in a worktree that does not have it, which stops the new work with no session.
func TestWorkNew007(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkNew007{
		GivenTheMainCheckoutIsOnABranchThatAddsACreateScriptTheRemotesMainDoesNotHave: func() {
			m.Git(clone, "switch", "-q", "-c", "adds-create")
			m.WriteFile(filepath.Join(clone, ".engineroom", "create"), "#!/bin/sh\necho branch create ran\n", 0o755)
			m.Git(clone, "add", ".engineroom")
			m.Git(clone, "commit", "-qm", "add create on a branch")
		},
		WhenTheDeveloperStartsNewWorkOnTheProject: func() {
			term = m.Start(t, "new", "project-a", "from-main")
		},
		ThenNoCreateScriptIsRun: func() {
			term.WaitFor("AGENT new conversation")
			out, _ := m.Run("engineroom", "history")
			if strings.Contains(out, ".engineroom/create") {
				t.Fatalf("a create script was run:\n%s", out)
			}
		},
		AndTheSessionInTheNewCheckoutOpens: func() {
			if journey.SessionState(m.Sessions(), "project-a-from-main") != "running" {
				t.Fatalf("no session in the new worktree:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}
