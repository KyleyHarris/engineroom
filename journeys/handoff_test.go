//go:build journeys

package journeys

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// engineCopy is the engine's worktree of project-a in every handoff journey.
const engineCopy = "/home/dev/src/project-a"

// hereHeading is the heading this machine's own sessions are listed under, among the engine's.
const hereHeading = "Still running on this machine, started while it was offline"

// handoffWorld is a client and an engine that both have project-a, cloned from the same stand-in
// GitHub and registered on each, with nothing yet that one has and the other does not. It
// returns the client's worktree.
func handoffWorld(t *testing.T) (*journey.Machine, *journey.Engine, string) {
	t.Helper()
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	journey.SharedGitHub(client, engine)
	engine.AddGitHubRepo("owner-a", "project-a")
	engine.MustExec("git clone -q git@github.com:owner-a/project-a.git " + engineCopy)
	engine.WriteConfig("role = \"engine\"\n[agent]\ncommand = \"fake-agent\"\n[[project]]\nname = \"project-a\"\npath = \"" + engineCopy + "\"\n")
	client.BeClientOf(engine, "engine")
	mine := filepath.Join(client.Home, "src", "project-a")
	if out, err := client.Run("git", "clone", "-q", "git@github.com:owner-a/project-a.git", mine); err != nil {
		t.Fatalf("cloning on the client: %v\n%s", err, out)
	}
	client.AddLocalProject("project-a", mine)
	// The agent stand-in keeps a marker file in the folder it runs in; on a real machine Claude
	// Code keeps its own files under .claude, which a handover leaves where it is. The marker is
	// excluded on both machines, so a session's stand-in is not taken for uncommitted work.
	engine.MustExec("echo .fake-agent-conversation >> " + engineCopy + "/.git/info/exclude")
	if out, err := client.Run("sh", "-c", `echo .fake-agent-conversation >> "$1/.git/info/exclude"`, "sh", mine); err != nil {
		t.Fatalf("excluding the agent's marker: %v\n%s", err, out)
	}
	return client, engine, mine
}

// showsCommand reports whether the screen shows the command text, however the screen wrapped it.
// A long command is wrapped either at a space or mid-word, depending on the length of the temporary
// folders in it, so the screen's continuation lines are joined back on and both sides are
// compared with their spaces taken out.
func showsCommand(screen, command string) bool {
	joined := regexp.MustCompile(`\n {5,}`).ReplaceAllString(screen, "")
	squash := func(s string) string { return strings.ReplaceAll(s, " ", "") }
	return strings.Contains(squash(joined), squash(command))
}

// commitOnClient commits a change in the client's worktree and returns the commit.
func commitOnClient(t *testing.T, client *journey.Machine, dir string) string {
	t.Helper()
	out, err := client.Run("sh", "-c", `cd "$1" && echo client-work >> README.md && git commit -qam client-work && git rev-parse HEAD`, "sh", dir)
	if err != nil {
		t.Fatalf("committing on the client: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

func goOffline(t *testing.T, client *journey.Machine) {
	t.Helper()
	if out, err := client.Run("engineroom", "offline", "on"); err != nil {
		t.Fatalf("engineroom offline on: %v\n%s", err, out)
	}
}

func engineHead(engine *journey.Engine) string {
	return strings.TrimSpace(engine.MustExec("git -C " + engineCopy + " rev-parse HEAD"))
}

// backOnline starts the program on an offline client with no sessions and switches it online.
func backOnline(t *testing.T, client *journey.Machine) *termtest.Terminal {
	t.Helper()
	term := client.Start(t)
	term.WaitFor("There are no sessions.")
	term.Type("f")
	return term
}

// CONN-HANDOFF-001: going back online shows the client's unpushed commit and the commands that
// carry it to the engine, and runs nothing. Fails if the commit is not named, a command is missing,
// or GitHub or the engine's worktree changed before the developer asked.
func TestConnHandoff001(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var term *termtest.Terminal
	var githubBefore, engineBefore string

	scenarios.Run(t, scenarios.ConnHandoff001{
		GivenTheClientIsOffline: func() { goOffline(t, client) },
		AndACommitWasMadeInTheClientSCheckoutOfAProjectAndNotPushed: func() {
			githubBefore, engineBefore = engine.GitHubHead("owner-a", "project-a", "main"), engineHead(engine)
			commitOnClient(t, client, mine)
		},
		WhenTheDeveloperSwitchesTheClientBackOnline: func() {
			term = backOnline(t, client)
		},
		ThenTheyAreShownThatTheProjectHasACommitOnTheClientThatIsNotOnGitHub: func() {
			// The plan is longer than the screen, so its title has scrolled out of sight.
			term.WaitFor("These commands will run")
			term.WaitFor("project-a: 1 commit of main sent to GitHub from this machine")
		},
		AndTheyAreShownTheCommandsThatPushItFromTheClientAndBringTheEngineSCheckoutUpToIt: func() {
			term.WaitFor("on this machine: send 1 commit of main to GitHub")
			term.WaitFor("on the engine: put " + engineCopy + " on main at")
			for _, want := range []string{"push origin main", "engine -- 'git -C " + engineCopy + " switch -q -C main"} {
				if !showsCommand(term.Screen(), want) {
					t.Fatalf("the command %q is not shown:\n%s", want, term.Screen())
				}
			}
		},
		AndNothingHasRunYet: func() {
			term.WaitFor("Nothing has run yet.")
			if got := engine.GitHubHead("owner-a", "project-a", "main"); got != githubBefore {
				t.Fatalf("GitHub changed before the handoff was run: %s, was %s", got, githubBefore)
			}
			if got := engineHead(engine); got != engineBefore {
				t.Fatalf("the engine's worktree changed before the handoff was run: %s, was %s", got, engineBefore)
			}
		},
	}, term0(&term))
}

// CONN-HANDOFF-002: running the handoff pushes the commit and moves the engine's worktree to it.
// Fails if GitHub or the engine's worktree is not at the client's commit afterwards.
func TestConnHandoff002(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var term *termtest.Terminal
	var commit string

	scenarios.Run(t, scenarios.ConnHandoff002{
		GivenGoingBackOnlineShowedACommitOnTheClientToHandOver: func() {
			goOffline(t, client)
			commit = commitOnClient(t, client, mine)
			term = backOnline(t, client)
			term.WaitFor("Nothing has run yet.")
		},
		WhenTheDeveloperRunsTheHandoff: func() {
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenTheCommitIsOnGitHub: func() {
			if got := engine.GitHubHead("owner-a", "project-a", "main"); got != commit {
				t.Fatalf("GitHub's main is %s, want the client's %s", got, commit)
			}
		},
		AndTheEngineSCheckoutOfTheProjectIsAtThatCommit: func() {
			if got := engineHead(engine); got != commit {
				t.Fatalf("the engine's worktree is at %s, want %s", got, commit)
			}
		},
	}, term0(&term))
}

// CONN-HANDOFF-003: handing back to the engine, the engine's checkout sets its own uncommitted
// changes aside in its stash list, labelled with its branch, and takes the client's commit. Fails
// if the engine's checkout is not at the client's commit, keeps changes, or its changes are lost.
func TestConnHandoff003(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var commit string

	scenarios.Run(t, scenarios.ConnHandoff003{
		GivenTheClientHasACommitToHandOver: func() {
			goOffline(t, client)
			commit = commitOnClient(t, client, mine)
		},
		AndTheEnginesCheckoutOfThatProjectHasUncommittedChanges: func() {
			engine.MustExec("echo engine-edit >> " + engineCopy + "/README.md")
		},
		WhenTheDeveloperHandsTheWorkBackToTheEngineAndRunsIt: func() {
			mustRun(t, client, "handoff", "to-engine")
		},
		ThenTheEnginesCheckoutIsAtTheClientSCommitWithNoChangesOfItsOwn: func() {
			if got := engineHead(engine); got != commit {
				t.Fatalf("the engine's checkout is at %s, want %s", got, commit)
			}
			if st := strings.TrimSpace(engine.MustExec("git -C " + engineCopy + " status --porcelain")); st != "" {
				t.Fatalf("the engine's checkout still has: %s", st)
			}
		},
		AndTheEnginesChangesAreInItsStashListLabelledWithItsBranch: func() {
			list := engine.MustExec("git -C " + engineCopy + " stash list")
			if !strings.Contains(list, "engineroom: work on main set aside before the handover from the client") {
				t.Fatalf("stash list: %q", list)
			}
			if diff := engine.MustExec("git -C " + engineCopy + " stash show -p stash@{0}"); !strings.Contains(diff, "+engine-edit") {
				t.Fatalf("the engine's change is not in the stash: %s", diff)
			}
		},
	}, nil)
}

// CONN-HANDOFF-004: going offline while the engine answers shows the engine's unpushed commit and the
// commands that bring it to the client. Fails if the commit or either side's command is missing.
func TestConnHandoff004(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnHandoff004{
		GivenACommitWasMadeInTheEngineSCheckoutOfAProjectAndNotPushed: func() {
			engine.MustExec("cd " + engineCopy + " && echo engine-work >> README.md && git commit -qam engine-work")
		},
		AndTheEngineAnswers: func() {
			if out, err := client.Run("ssh", "-F", engine.SSHConfig, "engine", "true"); err != nil {
				t.Fatalf("the engine does not answer: %v\n%s", err, out)
			}
		},
		WhenTheDeveloperSwitchesTheClientOffline: func() {
			term = client.Start(t)
			term.WaitFor("There are no sessions.")
			term.Type("f")
		},
		ThenTheyAreShownThatTheProjectHasACommitOnTheEngineThatIsNotOnGitHub: func() {
			// The plan is longer than the screen, so its title has scrolled out of sight.
			term.WaitFor("These commands will run")
			term.WaitFor("project-a: 1 commit of main sent to GitHub from the engine")
		},
		AndTheyAreShownTheCommandsThatPushItFromTheEngineAndBringTheClientSCheckoutUpToIt: func() {
			term.WaitFor("on the engine: send 1 commit of main to GitHub")
			term.WaitFor("on this machine: put ")
			term.WaitFor("Nothing has run yet.")
		},
	}, term0(&term))
}

// CONN-HANDOFF-005: going offline with the engine not answering still goes offline, and says the
// engine could not be checked. Fails if the switch waits on the engine, or the note is missing.
func TestConnHandoff005(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnHandoff005{
		GivenTheEngineDoesNotAnswer: func() {
			engine.SetReachable(client, false)
		},
		WhenTheDeveloperSwitchesTheClientOffline: func() {
			term = client.Start(t)
			term.WaitFor("did not answer")
			term.Type("f")
		},
		ThenTheClientIsOffline: func() {
			term.WaitFor("OFFLINE")
			if out, _ := client.Run("engineroom", "offline", "status"); !strings.HasPrefix(out, "offline:") {
				t.Fatalf("engineroom offline status: %s", out)
			}
		},
		AndTheyAreToldTheEngineCouldNotBeCheckedForWorkThatIsNotOnGitHub: func() {
			term.WaitFor("The engine did not answer, so it could not be checked for work that is not on GitHub.")
		},
	}, term0(&term))
}

// CONN-HANDOFF-006: back online, a session started on the client while offline is listed after
// the engine's, under a heading of its own, and joining it joins it on the client. The engine has a
// session of the same name, so joining the wrong one is caught. Fails if the client's session is
// missing, unmarked, or the join reaches the engine.
func TestConnHandoff006(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnHandoff006{
		GivenASessionStartedOnTheClientWhileItWasOfflineIsStillRunning: func() {
			goOffline(t, client)
			start := client.Start(t, "open", "project-a", "--no-join")
			start.WaitExit()
			client.WaitSession("project-a", "running")
			engine.StartSession("project-a", engineCopy)
		},
		WhenTheDeveloperSwitchesTheClientBackOnline: func() {
			term = client.Start(t)
			term.WaitFor("Sessions on this machine")
			term.Type("f")
		},
		ThenTheSessionListShowsTheEngineSSessions: func() {
			term.WaitFor("Sessions on the engine engine")
			term.WaitFor(hereHeading)
			// The engine's own session is listed first, above the heading for this machine's.
			screen := term.Screen()
			list := screen[strings.Index(screen, "Sessions on the engine engine"):]
			if !strings.Contains(list[:strings.Index(list, hereHeading)], "project-a") {
				t.Fatalf("the engine's session is not listed:\n%s", screen)
			}
		},
		AndItShowsThatSessionMarkedAsOnThisMachine: func() {
			term.WaitFor(hereHeading)
			screen := term.Screen()
			heading := strings.Index(screen, hereHeading)
			if heading < 0 || !strings.Contains(screen[heading:], "project-a") {
				t.Fatalf("the client's session is not listed under the heading:\n%s", screen)
			}
		},
		AndJoiningItJoinsItOnTheClient: func() {
			term.Type("j")
			term.Press(termtest.Enter)
			term.WaitFor("Zellij (project-a)")
			term.WaitFor("AGENT new conversation in")
			if strings.Contains(term.Screen(), "/home/dev") {
				t.Fatalf("the join reached the engine's session:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}
