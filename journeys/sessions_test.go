//go:build journeys

package journeys

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
	"github.com/KyleyHarris/engineroom/internal/tui"
)

// SESS-NEW-001: opening a project with no session creates one in its folder, from the
// standard layout. Fails if no session appears, if it runs somewhere else, if the agent is not
// alone on its own tab, if the git tab is missing, or if the old dev server, tests or logs
// panes come back.
func TestSessNew001(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew001{
		GivenAProjectHasNoSessionOnTheEngine: func() {
			if journey.SessionState(m.Sessions(), "project-a") != "" {
				t.Fatalf("a session already exists:\n%s", m.Sessions())
			}
		},
		WhenTheDeveloperOpensIt: func() {
			term = m.Start(t, "open", "project-a")
		},
		ThenASessionIsCreatedInItsCheckout: func() {
			term.WaitFor("AGENT new conversation")
			m.WaitSession("project-a", "running")
			if !m.AgentStartedIn(path) {
				t.Fatalf("the agent did not start in %s: %q", path, m.AgentStarts())
			}
		},
		AndItHoldsClaudeCodeOnItsOwnTabAndAPlaceForGit: func() {
			// Zellij's own description of the running session proves the tabs, including the
			// git tab that is not on screen.
			layout, err := m.Run("zellij", "--session", "project-a", "action", "dump-layout")
			if err != nil {
				t.Fatalf("dump-layout: %v\n%s", err, layout)
			}
			for _, want := range []string{`tab name="agent"`, `name="agent"`, `tab name="git"`} {
				if !strings.Contains(layout, want) {
					t.Fatalf("the session has no %s:\n%s", want, layout)
				}
			}
			// The agent's tab, up to the next tab, holds the agent and Zellij's own bars (each a
			// pane holding a plugin), and no other pane.
			agentTab := layout[strings.Index(layout, `tab name="agent"`):]
			if end := strings.Index(agentTab, `tab name="git"`); end >= 0 {
				agentTab = agentTab[:end]
			}
			if n := strings.Count(agentTab, "pane ") - strings.Count(agentTab, "plugin location"); n != 1 {
				t.Fatalf("the agent's tab holds %d panes besides Zellij's bars, not the agent alone:\n%s", n, agentTab)
			}
			for _, gone := range []string{"dev server", `"tests"`, `"logs"`} {
				if strings.Contains(layout, gone) {
					t.Fatalf("the session still has %s:\n%s", gone, layout)
				}
			}
		},
	}, term0(&term))
}

// SESS-NEW-002: opening a project whose session is running joins it. Fails if a second
// session is created or the developer lands anywhere but that session.
func TestSessNew002(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew002{
		GivenAProjectAlreadyHasARunningSession: func() {
			m.StartSession("project-a", path)
		},
		WhenTheDeveloperOpensIt: func() {
			term = m.Start(t, "open", "project-a")
		},
		ThenTheyJoinThatSession: func() {
			term.WaitFor("Zellij (project-a)")
		},
		AndNoSecondSessionIsCreated: func() {
			if n := countSessions(m.Sessions()); n != 1 {
				t.Fatalf("expected exactly one session, found %d:\n%s", n, m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-NEW-003: a name the program does not know is answered with the names it does, and
// nothing is created.
func TestSessNew003(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew003{
		GivenNoProjectCalledAcmeSiteIsRegistered: func() {
			m.BeEngine("project-a", path)
		},
		WhenTheDeveloperOpensAcmeSite: func() {
			term = m.Start(t, "open", "acme-site")
		},
		ThenTheyAreToldItIsNotARegisteredProject: func() {
			term.WaitFor(`"acme-site" is not a registered project.`)
		},
		AndTheyAreShownTheRegisteredProjects: func() {
			term.WaitFor("Registered projects")
			term.WaitFor("project-a")
		},
		AndNoSessionIsCreated: func() {
			if n := countSessions(m.Sessions()); n != 0 {
				t.Fatalf("a session was created:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-LIST-001: every session is listed with its project, its checkout and that it is
// running. Fails if either session is missing or shown without its project or folder.
func TestSessList001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessList001{
		GivenTheEngineHasSessionsForTwoProjects: func() {
			m.BeEngine("project-a", a, "project-b", b)
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
		},
		WhenTheDeveloperOpensTheListOfSessions: func() {
			term = m.Start(t)
		},
		ThenEachSessionIsListedWithItsProjectItsCheckoutAndWhetherItIsRunning: func() {
			term.WaitFor("Sessions on the engine")
			term.WaitFor("project-a")
			term.WaitFor("project-b")
			for _, want := range []struct{ name, dir string }{{"project-a", "~/src/project-a"}, {"project-b", "~/src/project-b"}} {
				row := rowOf(t, term, want.name)
				if !strings.Contains(row, "running") || strings.Count(row, want.name) < 2 || !strings.Contains(row, want.dir) {
					t.Fatalf("row for %s lacks its state, project or folder (%s):\n%s", want.name, want.dir, row)
				}
			}
		},
	}, term0(&term))
}

// SESS-LIST-002: a session that asked the developer something keeps its place in the list, by
// name, marked with Claude's mark, with the question. project-b waits, so a list that put waiting
// sessions first would move it above project-a. Fails if it moves, has no mark, or the question
// is missing.
func TestSessList002(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessList002{
		GivenOneSessionHasStoppedToAskTheDeveloperAQuestion: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
			// The agent's notification hook calls this command inside its session; here it
			// names the session explicitly, as the hook's environment would.
			if out, err := m.Run("engineroom", "signal", "waiting", "--session", "project-b", "--message", "Approve the migration?"); err != nil {
				t.Fatalf("signal: %v\n%s", err, out)
			}
		},
		WhenTheDeveloperOpensTheListOfSessions: func() {
			term = m.Start(t)
		},
		ThenThatSessionIsListedWhereItWouldBeIfItWereNotWaiting: func() {
			term.WaitFor("Sessions on the engine")
			term.WaitFor("asks: Approve the migration?")
			screen := term.Screen()
			if strings.Index(screen, "project-b") < strings.Index(screen, "project-a") {
				t.Fatalf("the waiting session was moved above project-a:\n%s", screen)
			}
		},
		AndItIsMarkedWithClaudesMarkAsWaiting: func() {
			if row := rowOf(t, term, "project-b"); !strings.Contains(row, tui.ClaudeMark) || !strings.Contains(row, "waiting") {
				t.Fatalf("the waiting session is not marked:\n%s", term.Screen())
			}
			if row := rowOf(t, term, "project-a"); strings.Contains(row, tui.ClaudeMark) {
				t.Fatalf("project-a is marked, and is not waiting:\n%s", term.Screen())
			}
		},
		AndTheQuestionItAskedIsShownBesideIt: func() {
			term.WaitFor("asks: Approve the migration?")
		},
	}, term0(&term))
}

// SESS-LIST-005: the pinned project's session is listed first, the others after it by name,
// however they would sort by name alone. Fails if the pinned one is not first, or the rest are
// out of name order.
func TestSessList005(t *testing.T) {
	m := journey.NewMachine(t)
	al, be, ga := m.NewRepo("alpha"), m.NewRepo("beta"), m.NewRepo("gamma")
	m.BeEngine("alpha", al, "beta", be, "gamma", ga)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessList005{
		GivenRunningSessionsForAlphaBetaAndGammaWithGammaPinned: func() {
			m.StartSession("gamma", ga)
			m.StartSession("beta", be)
			m.StartSession("alpha", al)
			mustRun(t, m, "pin", "gamma", "on")
		},
		WhenTheDeveloperOpensTheListOfSessions: func() {
			term = m.Start(t)
		},
		ThenGammasSessionIsListedFirst: func() {
			term.WaitFor("Sessions on the engine")
			term.WaitFor("alpha")
			screen := term.Screen()
			g, a := strings.Index(screen, "gamma"), strings.Index(screen, "alpha")
			if g < 0 || g > a {
				t.Fatalf("gamma is not first:\n%s", screen)
			}
		},
		AndAlphasAndBetasFollowItByName: func() {
			screen := term.Screen()
			if a, b := strings.Index(screen, "alpha"), strings.Index(screen, "beta"); a < 0 || b < 0 || a > b {
				t.Fatalf("alpha and beta are not in name order:\n%s", screen)
			}
		},
	}, term0(&term))
}

// SESS-LIST-003: an ended session reads as ended. Fails if it is listed as running.
func TestSessList003(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessList003{
		GivenASessionHasEnded: func() {
			// Opened through the program so it has the standard layout, which Zellij saves;
			// a session that was never saved disappears entirely when it ends.
			first := m.Start(t, "open", "project-a")
			first.WaitFor("AGENT new conversation")
			m.WaitSerialized(t, "project-a", `name="agent"`)
			if out, err := m.Run("zellij", "kill-session", "project-a"); err != nil {
				t.Fatalf("kill: %v\n%s", err, out)
			}
			m.WaitSession("project-a", "ended")
		},
		WhenTheDeveloperOpensTheListOfSessions: func() {
			term = m.Start(t)
		},
		ThenItIsListedAsEnded: func() {
			term.WaitFor("Sessions on the engine")
			term.WaitFor("project-a")
			if !strings.Contains(rowOf(t, term, "project-a"), "ended") {
				t.Fatalf("not shown as ended:\n%s", term.Screen())
			}
		},
		AndItIsNotShownAsRunning: func() {
			if strings.Contains(rowOf(t, term, "project-a"), "running") {
				t.Fatalf("an ended session is shown as running:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}

// SESS-LIST-004: with no sessions, the list says so and offers the projects.
func TestSessList004(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessList004{
		GivenTheEngineHasNoSessions: func() {
			m.BeEngine("project-a", a, "project-b", b)
		},
		WhenTheDeveloperOpensTheListOfSessions: func() {
			term = m.Start(t)
		},
		ThenTheyAreToldThereAreNoSessions: func() {
			term.WaitFor("There are no sessions.")
		},
		AndTheyAreOfferedTheRegisteredProjectsToOpen: func() {
			term.WaitFor("Registered projects")
			term.WaitFor("project-a")
			term.WaitFor("project-b")
			term.WaitFor("enter open")
		},
	}, term0(&term))
}

// SESS-STOP-001: choosing stop asks before anything is stopped.
func TestSessStop001(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessStop001{
		GivenASessionIsRunning: func() {
			m.StartSession("project-a", a)
		},
		WhenTheDeveloperChoosesToStopIt: func() {
			term = m.Start(t)
			term.WaitFor("project-a")
			term.Type("x")
		},
		ThenTheyAreAskedToConfirmBeforeAnythingIsStopped: func() {
			term.WaitFor("Stop the session project-a?")
			if journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("the session was stopped before confirming:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-STOP-002: a confirmed stop ends that session and only that one.
func TestSessStop002(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessStop002{
		GivenTwoSessionsAreRunning: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
		},
		WhenTheDeveloperStopsOneOfThemAndConfirms: func() {
			term = m.Start(t)
			term.WaitFor("project-b")
			// The list is in name order; the cursor starts on project-a.
			term.Type("x")
			term.WaitFor("Stop the session project-a?")
			term.Type("y")
		},
		ThenThatSessionIsGoneWithNothingOfItKeptToBringBack: func() {
			term.WaitFor("engineroom session end project-a")
			term.WaitFor("Done.")
			// Gone, not ended: no snapshot is kept that could come back with an old layout (D-015).
			if state := journey.SessionState(m.Sessions(), "project-a"); state != "" {
				t.Fatalf("the stopped session is still listed, as %q:\n%s", state, m.Sessions())
			}
		},
		AndTheOtherIsStillRunning: func() {
			if journey.SessionState(m.Sessions(), "project-b") != "running" {
				t.Fatalf("the other session was affected:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-STOP-003: declining leaves the session running.
func TestSessStop003(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessStop003{
		GivenTheDeveloperHasBeenAskedToConfirmStoppingASession: func() {
			m.StartSession("project-a", a)
			term = m.Start(t)
			term.WaitFor("project-a")
			term.Type("x")
			term.WaitFor("Stop the session project-a?")
		},
		WhenTheyDecline: func() {
			term.Type("n")
		},
		ThenTheSessionIsStillRunning: func() {
			term.WaitFor("Nothing was stopped.")
			if journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("declining stopped the session:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-SWITCH-001: from inside one session, the window moves to another, and the first keeps
// running. Fails if the window stays, or the first session ends.
func TestSessSwitch001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessSwitch001{
		GivenTheDeveloperIsInOneSession: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
			term = termtest.Start(t, m.Command("zellij", "attach", "project-a"), journeyOptions(t))
			term.WaitFor("Zellij (project-a)")
		},
		WhenTheyChooseAnotherSessionFromInsideIt: func() {
			term.Type("engineroom switch\r")
			term.WaitFor("Move this window to which session?")
			term.WaitFor("project-b")
			term.Press(termtest.Down, termtest.Enter)
		},
		ThenThisWindowShowsTheOtherSession: func() {
			term.WaitFor("Zellij (project-b)")
		},
		AndTheOneTheyLeftIsStillRunning: func() {
			if journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("the session left behind is not running:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-BACK-001: a session that ended with a layout of its own starts again fresh, from the
// current standard layout. Fails if Zellij's snapshot is brought back (its extra tab would be
// there) or the agent tab is missing.
func TestSessBack001(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessBack001{
		GivenASessionWithALayoutOfItsOwnEndedBecauseTheEngineRestarted: func() {
			endedWithOwnLayout(t, m)
		},
		WhenTheDeveloperStartsItAgain: func() {
			term = m.Start(t)
			term.WaitFor("ended")
			term.Press(termtest.Enter)
		},
		ThenItOpensWithTheCurrentStandardLayout: func() {
			term.WaitFor("Zellij (project-a)")
			term.WaitFor("AGENT")
			layout := dumpLayout(t, m, "project-a")
			for _, want := range []string{`tab name="agent"`, `tab name="git"`} {
				if !strings.Contains(layout, want) {
					t.Fatalf("the session has no %s:\n%s", want, layout)
				}
			}
		},
		AndNotWithTheTabsAndPanesItHad: func() {
			if layout := dumpLayout(t, m, "project-a"); strings.Contains(layout, "old-panes") {
				t.Fatalf("the old snapshot was brought back:\n%s", layout)
			}
		},
	}, term0(&term))
}

// SESS-BACK-002: starting a session again continues its conversation. Fails if the agent is
// started without --continue.
func TestSessBack002(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessBack002{
		GivenASessionEndedBecauseTheEngineRestarted: func() {
			endedByRestart(t, m, a)
		},
		WhenTheDeveloperStartsItAgain: func() {
			term = m.Start(t)
			term.WaitFor("ended")
			term.Press(termtest.Enter)
		},
		ThenClaudeCodeContinuesTheConversationItWasHaving: func() {
			term.WaitFor("AGENT continuing conversation")
		},
		AndANewEmptyConversationIsNotStartedInItsPlace: func() {
			starts := m.AgentStarts()
			last := starts[len(starts)-1]
			if !strings.Contains(last, "--continue") {
				t.Fatalf("the agent was last started without --continue: %q", starts)
			}
			if strings.Contains(term.Screen(), "AGENT new conversation") {
				t.Fatalf("a new conversation was started:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}

// SESS-BACK-003: restarting a running session from the app (its API, as the app calls it) puts it
// on the current layout and continues the conversation. Fails if the old tab survives, or the
// agent starts a new conversation.
func TestSessBack003(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var out string

	scenarios.Run(t, scenarios.SessBack003{
		GivenASessionWithALayoutOfItsOwnIsRunning: func() {
			first := m.Start(t, "open", "project-a")
			first.WaitFor("AGENT new conversation")
			if o, err := m.Run("zellij", "--session", "project-a", "action", "new-tab", "--name", "old-panes"); err != nil {
				t.Fatalf("new tab: %v\n%s", err, o)
			}
			if !strings.Contains(dumpLayout(t, m, "project-a"), "old-panes") {
				t.Fatal("the session's own tab was not made")
			}
		},
		WhenTheDeveloperRestartsItFromTheApp: func() {
			// Remote Control mode, so the plan opens no terminal window.
			out, _ = m.Run("engineroom", "api", "run", "restart", "project-a", "--mode", "remote")
			if !strings.Contains(out, `"ok":true`) {
				t.Fatalf("the restart did not finish:\n%s", out)
			}
			m.WaitSession("project-a", "running")
		},
		ThenItRunsWithTheCurrentStandardLayout: func() {
			layout := dumpLayout(t, m, "project-a")
			if !strings.Contains(layout, `tab name="agent"`) || strings.Contains(layout, "old-panes") {
				t.Fatalf("not on the current layout:\n%s", layout)
			}
		},
		AndClaudeCodeContinuesTheConversationItWasHaving: func() {
			starts := m.AgentStarts()
			if last := starts[len(starts)-1]; !strings.Contains(last, "--continue") {
				t.Fatalf("the agent was last started without --continue: %q", starts)
			}
		},
	}, nil)
}

// endedWithOwnLayout opens the project, gives the session a tab of its own (as an older layout
// would have), waits until Zellij has saved that, then ends the session's server as a restart
// would.
func endedWithOwnLayout(t *testing.T, m *journey.Machine) {
	t.Helper()
	first := m.Start(t, "open", "project-a")
	first.WaitFor("AGENT new conversation")
	if o, err := m.Run("zellij", "--session", "project-a", "action", "new-tab", "--name", "old-panes"); err != nil {
		t.Fatalf("new tab: %v\n%s", err, o)
	}
	m.WaitSerialized(t, "project-a", "old-panes")
	m.KillServer(t, "project-a")
	m.WaitSession("project-a", "ended")
	first.WaitExit()
}

// dumpLayout is a running session's layout as Zellij describes it.
func dumpLayout(t *testing.T, m *journey.Machine, name string) string {
	t.Helper()
	out, err := m.Run("zellij", "--session", name, "action", "dump-layout")
	if err != nil {
		t.Fatalf("dump-layout: %v\n%s", err, out)
	}
	return out
}

// endedByRestart opens the project through the program (so the session has the standard
// layout and a conversation), waits until Zellij has saved the session to disk, then ends the
// session's server abruptly, as a restart of the machine would. The server is found through
// this run's own socket directory, never by a process name, so nothing outside the run is
// touched.
func endedByRestart(t *testing.T, m *journey.Machine, dir string) {
	t.Helper()
	first := m.Start(t, "open", "project-a")
	first.WaitFor("AGENT new conversation")
	m.WaitSerialized(t, "project-a", `name="agent"`)
	m.KillServer(t, "project-a")
	m.WaitSession("project-a", "ended")
	first.WaitExit()
}

// countSessions counts the sessions in Zellij's plain list.
func countSessions(list string) int {
	n := 0
	for _, line := range strings.Split(list, "\n") {
		if strings.Contains(line, "[Created") {
			n++
		}
	}
	return n
}

// rowOf returns the screen line naming the session.
func rowOf(t *testing.T, term *termtest.Terminal, name string) string {
	t.Helper()
	for _, line := range strings.Split(term.Screen(), "\n") {
		if strings.Contains(line, name) && !strings.Contains(line, "asks:") {
			return line
		}
	}
	t.Fatalf("no row for %s:\n%s", name, term.Screen())
	return ""
}

// SESS-NEW-005: a session started by engineroom from inside a Claude Code session (here, with that
// session's markers in the environment engineroom runs in, as Claude's own commands have them) gives
// its agent none of them, while a Claude Code setting of the developer's own still reaches it.
// Fails if any marker reaches the agent, which then saves no transcript, or if the agent's
// environment cannot be read at all (the kept setting is the probe's own proof).
func TestSessNew005(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	markers := []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_SOCKET"}
	// Nothing runs in a terminal here, so captions have nowhere to go.
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew005{
		GivenTheProgramIsRunByAClaudeCodeAgentWithThatAgentsMarkersInItsEnvironment: func() {},
		WhenItStartsAProjectSSession: func() {
			env := "CLAUDE_CODE_MAX_OUTPUT_TOKENS=kept"
			for _, v := range markers {
				env += " " + v + "=from-another-session"
			}
			engine.MustExec(env + " engineroom api run start project-a --mode remote")
			deadline := time.Now().Add(15 * time.Second)
			for !strings.Contains(engine.MustExec("ps -eo args"), "fake-agent") {
				if time.Now().After(deadline) {
					t.Fatalf("the agent never started:\n%s", engine.Sessions())
				}
				time.Sleep(200 * time.Millisecond)
			}
		},
		ThenTheAgentInThatSessionIsGivenNoneOfThoseMarkers: func() {
			env := engine.MustExec(`for p in $(pgrep -f fake-agent); do tr '\0' '\n' < /proc/$p/environ; done`)
			if !strings.Contains(env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS=kept") {
				t.Fatalf("the agent's environment was not read, or lost the developer's own setting:\n%s", env)
			}
			for _, v := range markers {
				if strings.Contains(env, v+"=") {
					t.Errorf("%s reached the session's agent", v)
				}
			}
		},
	}, term0(&term))
}

// SESS-NEW-006: a session started from outside the desktop login runs inside it, through a launchd
// job that is gone once it has started the session. The route is turned on outright, since the
// journeys keep it off; run from outside the desktop login (an SSH login, or a Claude Code session
// started from one), the login the session's pane reports is the proof: without the route it
// would be the runner's own. Fails if the start does not go through launchd, a command run in the
// session is not in the desktop login, or the job is still loaded or running afterwards.
func TestSessNew006(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine("project-a", m.NewRepo("project-a"))
	m.Extra = append(m.Extra, "ENGINEROOM_DESKTOP_SESSIONS=on")
	gui := fmt.Sprintf("gui/%d", os.Getuid())
	var done map[string]any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew006{
		GivenTheDeveloperIsLoggedInAtTheEnginesScreen: func() {
			if exec.Command("launchctl", "print", gui).Run() != nil {
				t.Skip("no one is logged in at this Mac's screen, so there is no desktop login to start in")
			}
		},
		AndTheProgramRunsOnTheEngineOutsideThatLoginAsItDoesOverSSH: func() {
			if out, _ := exec.Command("launchctl", "managername").Output(); strings.TrimSpace(string(out)) == "Aqua" {
				t.Log("this run is itself inside the desktop login, so the login the session reports cannot tell the routes apart; the launchd job is still checked")
			}
		},
		WhenItStartsAProjectSSession: func() {
			done = mustRun(t, m, "start", "project-a", "--mode", "zellij")
			m.WaitSession("project-a", "running")
		},
		ThenWhatRunsInTheSessionRunsInsideTheDevelopersDesktopLogin: func() {
			// The start (engineroom session start) prints each command it runs, so what it ran is in
			// its step's output.
			var ran []string
			for _, e := range asList(done["events"]) {
				if ev := asMap(e); ev["event"] == "step" {
					ran = append(ran, fmt.Sprint(ev["output"]))
				}
			}
			if !strings.Contains(strings.Join(ran, "\n"), "+ launchctl bootstrap "+gui+" ") {
				t.Fatalf("the start did not go through launchd:\n%s", strings.Join(ran, "\n"))
			}
			mark := filepath.Join(m.Home, "login")
			if out, err := m.Run("zellij", "--session", "project-a", "run", "--", "sh", "-c", "launchctl managername > "+mark); err != nil {
				t.Fatalf("running in the session: %v\n%s", err, out)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if b, _ := os.ReadFile(mark); strings.TrimSpace(string(b)) != "" {
					if got := strings.TrimSpace(string(b)); got != "Aqua" {
						t.Fatalf("a command in the session runs in the %q login, not the desktop's", got)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the command run in the session never reported its login")
				}
				time.Sleep(200 * time.Millisecond)
			}
		},
		AndNothingOfHowItWasStartedThereIsLeftRunningOrLoaded: func() {
			jobs, _ := filepath.Glob(filepath.Join(m.Home, ".local", "state", "engineroom", "jobs", "*.plist"))
			if len(jobs) != 1 {
				t.Fatalf("expected the one job file, found %q", jobs)
			}
			label := strings.TrimSuffix(filepath.Base(jobs[0]), ".plist")
			deadline := time.Now().Add(5 * time.Second)
			for exec.Command("launchctl", "print", gui+"/"+label).Run() == nil {
				if time.Now().After(deadline) {
					t.Fatalf("the job %s is still loaded", label)
				}
				time.Sleep(200 * time.Millisecond)
			}
			if out, _ := exec.Command("pgrep", "-f", "launchctl bootout "+gui+"/"+label).Output(); len(strings.TrimSpace(string(out))) > 0 {
				t.Fatalf("the job's shell still runs: %s", out)
			}
		},
	}, term0(&term))
}
