//go:build journeys

package journeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// detach is Zellij's default "detach" key sequence: Ctrl-o for session mode, then d.
func detach(term *termtest.Terminal) {
	term.Type("\x0f")
	term.Type("d")
}

// SESS-JOIN-001: from a client, joining shows the engine's session itself. Fails if the client
// starts a session of its own, or shows anything but the session running on the engine.
func TestSessJoin001(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessJoin001{
		GivenASessionIsRunningOnTheEngine: func() {
			engine.StartSession("project-a", "/home/dev/src/project-a")
		},
		WhenTheDeveloperJoinsItFromAClient: func() {
			term = client.Start(t)
			term.WaitFor("Sessions on the engine engine")
			term.WaitFor("project-a")
			term.Press(termtest.Enter)
		},
		ThenTheySeeThatSessionAsItIsOnTheEngine: func() {
			term.WaitFor("Zellij (project-a)")
			term.WaitFor("AGENT new conversation in /home/dev/src/project-a")
			if s := client.Sessions(); strings.Contains(s, "[Created") {
				t.Fatalf("a session was started on the client:\n%s", s)
			}
		},
	}, term0(&term))
}

// SESS-JOIN-002: leaving keeps the engine's session and its work running.
func TestSessJoin002(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessJoin002{
		GivenTheDeveloperHasJoinedASessionFromAClient: func() {
			engine.StartSession("project-a", "/home/dev/src/project-a")
			term = client.Start(t)
			term.WaitFor("project-a")
			term.Press(termtest.Enter)
			term.WaitFor("AGENT new conversation")
		},
		WhenTheyLeaveIt: func() {
			detach(term)
			term.WaitFor("Sessions on the engine engine")
		},
		ThenTheSessionIsStillRunningOnTheEngine: func() {
			if journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("the engine session is not running after leaving:\n%s", engine.Sessions())
			}
		},
		AndTheWorkItWasDoingCarriesOn: func() {
			// The agent process is still alive on the engine after leaving.
			out, _ := engine.Exec("ps -eo args")
			if !strings.Contains(out, "fake-agent") {
				t.Fatalf("the agent stopped when the developer left:\n%s", out)
			}
		},
	}, term0(&term))
}

// SESS-BACK-004: a client's window joined to a session (`engineroom join <session>`, as the app opens it)
// ends when the session is restarted from the app, instead of staying on the session list for the
// next join to bring forward. The join goes over SSH, whose closing words made the program take
// the session's end for a failed join. Fails if the program in the window is still running after
// the restart, or the session is not running again.
func TestSessBack004(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	// ssh as the developer's client runs it: at a Mac's default LogLevel INFO, and sharing one
	// connection to the engine (ControlMaster auto, as a client's config commonly sets). On a client,
	// joins riding such a connection were recorded ending with exit 255 and ssh's words "Shared
	// connection to <engine> closed." after many minutes attached, and the program then stayed on
	// the session list. Here the engine's Zellij (Linux) ends a killed session's join with exit 0, so
	// this journey cannot show that exit: it guards the outcome, and the hand-back unit test
	// (TestAFailedJoinStaysOpenWithItsReason) guards the rule for a join that ends with words.
	b, err := os.ReadFile(engine.SSHConfig)
	if err != nil {
		t.Fatal(err)
	}
	// A socket's path is limited to about 100 bytes, which the test's own folder overruns.
	sockets, err := os.MkdirTemp("/tmp", "hcm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	shared := "LogLevel INFO\n  ControlMaster auto\n  ControlPath " + filepath.Join(sockets, "%C") + "\n  ControlPersist 60\n"
	client.WriteFile(engine.SSHConfig, strings.Replace(string(b), "LogLevel ERROR\n", shared, 1), 0o600)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessBack004{
		GivenTheDeveloperHasJoinedASessionFromAClientInAWindowOpenedOnlyToJoinIt: func() {
			engine.StartSession("project-a", "/home/dev/src/project-a")
			term = client.Start(t, "join", "project-a")
			term.WaitFor("AGENT new conversation")
			// Joined for longer than a join takes to fail (tui.attachedFor, 10 seconds), as a
			// developer working in it is.
			time.Sleep(11 * time.Second)
			if !term.Running() {
				t.Fatalf("the join ended before the restart:\n%s", term.Screen())
			}
		},
		WhenTheyRestartItFromTheApp: func() {
			// Remote Control only opens no window, so the client's own windows stay out of it.
			mustRun(t, client, "restart", "project-a", "--mode", "remote")
		},
		ThenTheProgramInThatWindowEndsRatherThanStayingOpenOnTheSessionList: func() {
			deadline := time.Now().Add(15 * time.Second)
			for term.Running() && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if term.Running() {
				t.Fatalf("the join is still running after the restart:\n%s", term.Screen())
			}
		},
		AndTheSessionIsRunningAgainOnTheEngine: func() {
			if journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("the session is not running on the engine after the restart:\n%s", engine.Sessions())
			}
		},
	}, term0(&term))
}
