//go:build journeys

package journeys

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// offlineClient is a client of an engine that answers, with its own copy of project-a registered, and
// every ssh call it makes recorded.
func offlineClient(t *testing.T) (*journey.Machine, *journey.Engine, string) {
	t.Helper()
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	copyA := client.NewRepo("project-a")
	client.AddLocalProject("project-a", copyA)
	realSSH, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	client.RecordSSH(realSSH)
	return client, engine, copyA
}

// CONN-OFFLINE-001: switched offline, opening a project opens the client's own copy, never asks
// the engine, and is marked OFFLINE. Fails if any ssh call is made, the session opens on the engine,
// or the marker is missing.
func TestConnOffline001(t *testing.T) {
	client, engine, copyA := offlineClient(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnOffline001{
		GivenAClientWhoseEngineAnswers: func() {
			if out, err := client.Run("ssh", "-F", engine.SSHConfig, "engine", "true"); err != nil {
				t.Fatalf("the engine does not answer: %v\n%s", err, out)
			}
		},
		AndTheClientHasItsOwnCheckoutOfAProject: func() {},
		WhenTheDeveloperSwitchesTheClientOfflineAndOpensThatProject: func() {
			if out, err := client.Run("engineroom", "offline", "on"); err != nil {
				t.Fatalf("engineroom offline on: %v\n%s", err, out)
			}
			client.Run("sh", "-c", ": > \"$HOME/ssh.log\"")
			term = client.Start(t, "open", "project-a")
			term.WaitFor("AGENT new conversation")
		},
		ThenASessionIsOpenedOnTheClientInItsCheckout: func() {
			client.WaitSession("project-a", "running")
			if !client.AgentStartedIn(copyA) {
				t.Fatalf("the agent did not start in %s: %q", copyA, client.AgentStarts())
			}
		},
		AndNothingIsAskedOfTheEngine: func() {
			if calls := client.SSHCalls(); len(calls) != 0 {
				t.Fatalf("the engine was asked: %q", calls)
			}
			if strings.Contains(engine.Sessions(), "project-a") {
				t.Fatalf("a session was started on the engine:\n%s", engine.Sessions())
			}
		},
		AndEveryScreenIsMarkedOFFLINE: func() {
			list := client.Start(t)
			list.WaitFor("Sessions on this machine")
			list.WaitFor("OFFLINE")
			list.Type("h")
			list.WaitFor("History")
			list.WaitFor("OFFLINE")
		},
	}, term0(&term))
}

// CONN-OFFLINE-002: offline holds across runs, and switching back online lists the engine's
// sessions. Fails if a new run forgets it, or going online still shows this machine.
func TestConnOffline002(t *testing.T) {
	client, engine, _ := offlineClient(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnOffline002{
		GivenTheDeveloperSwitchedTheClientOffline: func() {
			engine.StartSession("project-b", "/home/dev/src/project-a")
			first := client.Start(t)
			first.WaitFor("Sessions on the engine engine")
			first.WaitFor("project-b")
			first.Type("f")
			first.WaitFor("OFFLINE")
			first.Type("q")
			first.WaitExit()
		},
		WhenTheyStartTheToolAgain: func() {
			term = client.Start(t)
		},
		ThenItIsStillOfflineAndMarkedOFFLINE: func() {
			term.WaitFor("OFFLINE")
			// This machine has no sessions, so the list leads to its projects (SESS-LIST-004).
			term.WaitFor("There are no sessions.")
			if strings.Contains(term.Screen(), "project-b") {
				t.Fatalf("the engine's session is listed while offline:\n%s", term.Screen())
			}
		},
		AndOnceTheySwitchItBackOnlineTheSessionsItListsAreThoseOnTheEngine: func() {
			term.Type("f")
			term.WaitFor("Sessions on the engine engine")
			term.WaitFor("project-b")
			term.WaitGone("OFFLINE")
		},
	}, term0(&term))
}

// CONN-OFFLINE-003: going back online stops nothing on the client. Fails if the client's own
// session is stopped or ended by the switch.
func TestConnOffline003(t *testing.T) {
	client, _, copyA := offlineClient(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnOffline003{
		GivenASessionWasStartedOnTheClientWhileItWasOffline: func() {
			if out, err := client.Run("engineroom", "offline", "on"); err != nil {
				t.Fatalf("engineroom offline on: %v\n%s", err, out)
			}
			start := client.Start(t, "open", "project-a", "--no-join")
			start.WaitExit()
			client.WaitSession("project-a", "running")
			_ = copyA
		},
		WhenTheDeveloperSwitchesTheClientBackOnline: func() {
			term = run(t, client, "offline", "off")
			term.WaitFor("Online")
		},
		ThenThatSessionIsStillRunningOnTheClient: func() {
			if journey.SessionState(client.Sessions(), "project-a") != "running" {
				t.Fatalf("the client's session is not running:\n%s", client.Sessions())
			}
		},
	}, term0(&term))
}
