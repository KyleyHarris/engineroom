//go:build journeys

package journeys

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

const engineProject = "/home/dev/src/project-a"

// clientAndEngine is a client set up to reach an engine that has project-a registered.
func clientAndEngine(t *testing.T) (*journey.Machine, *journey.Engine) {
	t.Helper()
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", engineProject)
	client.BeClientOf(engine, "engine")
	return client, engine
}

// nothingStarted fails if a session exists on either machine.
func nothingStarted(t *testing.T, client *journey.Machine, engine *journey.Engine) {
	t.Helper()
	if strings.Contains(client.Sessions(), "[Created") {
		t.Fatalf("a session was started on the client:\n%s", client.Sessions())
	}
	if strings.Contains(engine.Sessions(), "[Created") {
		t.Fatalf("a session was started on the engine:\n%s", engine.Sessions())
	}
}

// CONN-ROLE-001: on a client, opening a project opens it on the engine and starts nothing here.
func TestConnRole001(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRole001{
		GivenThisMachineIsSetUpAsAClient: func() {
			if out, _ := client.Run("engineroom", "role", "show"); !strings.Contains(out, "client of engine") {
				t.Fatalf("not a client: %s", out)
			}
		},
		AndTheEngineAnswers: func() {},
		WhenTheDeveloperOpensAProject: func() {
			term = client.Start(t, "open", "project-a")
		},
		ThenItsSessionIsOpenedOnTheEngine: func() {
			term.WaitFor("AGENT new conversation in " + engineProject)
			if journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("no session on the engine:\n%s", engine.Sessions())
			}
		},
		AndNothingIsStartedOnThisMachine: func() {
			if strings.Contains(client.Sessions(), "[Created") {
				t.Fatalf("a session was started on the client:\n%s", client.Sessions())
			}
			if len(client.AgentStarts()) != 0 {
				t.Fatalf("the agent was started on the client: %q", client.AgentStarts())
			}
		},
	}, term0(&term))
}

// CONN-ROLE-003: a client names its engine by the engine's own machine name, read over SSH in the
// version check, not by the SSH name ("engine") its configuration reaches it with. The engine is
// a real container whose host name is its own. Fails if the name is missing, is the SSH name, or
// is not the engine's.
func TestConnRole003(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal
	var answer struct {
		Engine string `json:"engine"`
		Name   string `json:"name"`
	}

	scenarios.Run(t, scenarios.ConnRole003{
		GivenThisMachineIsAClientThatReachesTheEngineByAnSSHNameOfItsOwn: func() {
			if out, _ := client.Run("engineroom", "role", "show"); strings.TrimSpace(out) != "client of engine" {
				t.Fatalf("not a client of the SSH name engine: %s", out)
			}
		},
		AndTheEngineAnswers: func() {},
		WhenTheClientComparesItsVersionWithTheEngines: func() {
			out, err := client.Run("engineroom", "api", "engine-version")
			if err != nil {
				t.Fatalf("engineroom api engine-version: %v\n%s", err, out)
			}
			if err := json.Unmarshal([]byte(out), &answer); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, out)
			}
		},
		ThenItIsToldTheEnginesOwnMachineNameNotTheSSHName: func() {
			want := strings.SplitN(strings.TrimSpace(engine.MustExec("hostname")), ".", 2)[0]
			if answer.Name == "" || answer.Name == "engine" || answer.Name != want {
				t.Fatalf("the engine was named %q, want its own host name %q", answer.Name, want)
			}
			if answer.Engine == "" {
				t.Fatalf("the engine's version is missing")
			}
		},
	}, term0(&term))
}

// CONN-ROLE-002: on the engine, opening a project runs here, and no command shown reaches
// another machine. The commands are read from the program's own history of what it ran.
func TestConnRole002(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRole002{
		GivenThisMachineIsSetUpAsTheEngine: func() {
			m.BeEngine("project-a", a)
		},
		WhenTheDeveloperOpensAProject: func() {
			term = m.Start(t, "open", "project-a")
		},
		ThenItsSessionIsOpenedOnThisMachine: func() {
			term.WaitFor("AGENT new conversation")
			m.WaitSession("project-a", "running")
		},
		AndNoneOfTheCommandsShownConnectToAnotherMachine: func() {
			out, err := m.Run("engineroom", "history", "--json")
			if err != nil {
				t.Fatalf("history: %v\n%s", err, out)
			}
			var entries []struct {
				Commands []struct{ Display string } `json:"commands"`
			}
			if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) == 0 {
				t.Fatalf("history unreadable or empty: %v\n%s", err, out)
			}
			for _, c := range entries[0].Commands {
				if strings.HasPrefix(c.Display, "ssh ") || strings.Contains(c.Display, " ssh ") {
					t.Fatalf("a command reaches another machine: %s", c.Display)
				}
			}
		},
	}, term0(&term))
}

// CONN-DOWN-001: an unreachable engine is a question, and nothing starts anywhere.
func TestConnDown001(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnDown001{
		GivenThisMachineIsAClient: func() {},
		AndTheEngineDoesNotAnswer: func() {
			engine.SetReachable(client, false)
		},
		WhenTheDeveloperOpensAProject: func() {
			term = client.Start(t, "open", "project-a")
		},
		ThenTheyAreAskedToRetryWorkLocallyOrStop: func() {
			term.WaitFor("did not answer")
			term.WaitFor("retry the engine")
			term.WaitFor("work locally")
			term.WaitFor("stop")
		},
		AndNothingHasBeenStartedOnEitherMachine: func() {
			nothingStarted(t, client, engine)
		},
	}, term0(&term))
}

// engineDownPrompt brings a client to the "engine did not answer" question for project-a.
func engineDownPrompt(t *testing.T, client *journey.Machine, engine *journey.Engine) *termtest.Terminal {
	t.Helper()
	engine.SetReachable(client, false)
	term := client.Start(t, "open", "project-a")
	term.WaitFor("did not answer")
	term.WaitFor("retry the engine")
	return term
}

// CONN-DOWN-002: stop changes nothing on either machine.
func TestConnDown002(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal
	configBefore := ""

	scenarios.Run(t, scenarios.ConnDown002{
		GivenTheDeveloperWasAskedWhatToDoBecauseTheEngineDidNotAnswer: func() {
			configBefore, _ = client.Run("cat", client.ConfigPath())
			term = engineDownPrompt(t, client, engine)
		},
		WhenTheyChooseStop: func() {
			term.Type("s")
			term.WaitExit()
		},
		ThenNothingHasBeenStartedOrChangedOnEitherMachine: func() {
			nothingStarted(t, client, engine)
			if after, _ := client.Run("cat", client.ConfigPath()); after != configBefore {
				t.Fatalf("the configuration changed:\n%s", after)
			}
			if out, _ := client.Run("engineroom", "history"); strings.TrimSpace(out) != "" {
				t.Fatalf("an action was recorded:\n%s", out)
			}
		},
	}, term0(&term))
}

// CONN-DOWN-003: retry carries on once the engine answers, with the same request.
func TestConnDown003(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnDown003{
		GivenTheDeveloperWasAskedWhatToDoBecauseTheEngineDidNotAnswer: func() {
			term = engineDownPrompt(t, client, engine)
		},
		AndTheEngineNowAnswers: func() {
			engine.SetReachable(client, true)
		},
		WhenTheyChooseRetry: func() {
			term.Type("r")
		},
		ThenTheProjectTheyAskedForIsOpenedOnTheEngine: func() {
			term.WaitFor("AGENT new conversation in " + engineProject)
			if journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("no session on the engine:\n%s", engine.Sessions())
			}
		},
	}, term0(&term))
}

// CONN-DOWN-004: retry asks again while the engine is still down, and starts nothing.
func TestConnDown004(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnDown004{
		GivenTheDeveloperWasAskedWhatToDoBecauseTheEngineDidNotAnswer: func() {
			term = engineDownPrompt(t, client, engine)
		},
		AndTheEngineStillDoesNotAnswer: func() {},
		WhenTheyChooseRetry: func() {
			term.Type("r")
			term.WaitFor("Reaching the engine")
		},
		ThenTheyAreAskedAgainToRetryWorkLocallyOrStop: func() {
			term.WaitFor("did not answer")
			term.WaitFor("retry the engine")
		},
		AndNothingHasBeenStartedOnEitherMachine: func() {
			nothingStarted(t, client, engine)
		},
	}, term0(&term))
}

// CONN-LOCAL-001: working locally opens the project in this machine's own copy.
func TestConnLocal001(t *testing.T) {
	client, engine := clientAndEngine(t)
	local := client.NewRepo("project-a")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnLocal001{
		GivenTheDeveloperWasAskedWhatToDoBecauseTheEngineDidNotAnswer: func() {
			term = engineDownPrompt(t, client, engine)
		},
		AndThisMachineHasItsOwnCheckoutOfTheProject: func() {
			client.AddLocalProject("project-a", local)
		},
		WhenTheyChooseToWorkLocally: func() {
			term.Type("l")
		},
		ThenASessionIsOpenedOnThisMachineInThatCheckout: func() {
			term.WaitFor("AGENT new conversation")
			client.WaitSession("project-a", "running")
			if !client.AgentStartedIn(local) {
				t.Fatalf("the agent did not start in %s: %q", local, client.AgentStarts())
			}
		},
	}, term0(&term))
}

// CONN-LOCAL-002: every screen says LOCAL once working locally.
func TestConnLocal002(t *testing.T) {
	client, engine := clientAndEngine(t)
	local := client.NewRepo("project-a")
	client.AddLocalProject("project-a", local)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnLocal002{
		GivenTheDeveloperChoseToWorkLocally: func() {
			engine.SetReachable(client, false)
			term = client.Start(t)
			term.WaitFor("did not answer")
			term.Type("l")
		},
		WhenAnyScreenOfTheToolIsShown: func() {},
		ThenItIsMarkedLOCAL: func() {
			// The empty session list leads to the projects; then the preview, the history,
			// and back.
			term.WaitFor("Registered projects")
			mustBeLocal(t, term)
			term.Type("p")
			term.WaitFor("Nothing has run yet.")
			mustBeLocal(t, term)
			term.Press(termtest.Esc)
			// A key straight after Esc would read as Alt plus that key, so wait for the screen.
			term.WaitGone("Nothing has run yet.")
			term.Type("h")
			term.WaitFor("History")
			mustBeLocal(t, term)
		},
	}, term0(&term))
}

func mustBeLocal(t *testing.T, term *termtest.Terminal) {
	t.Helper()
	if !strings.Contains(strings.SplitN(term.Screen(), "\n", 2)[0], "LOCAL") {
		t.Fatalf("the screen is not marked LOCAL:\n%s", term.Screen())
	}
}

// CONN-LOCAL-003: with no copy here, working locally is refused and nothing starts.
func TestConnLocal003(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnLocal003{
		GivenTheDeveloperWasAskedWhatToDoBecauseTheEngineDidNotAnswer: func() {
			term = engineDownPrompt(t, client, engine)
		},
		AndThisMachineHasNoCheckoutOfTheProject: func() {},
		WhenTheyChooseToWorkLocally: func() {
			term.Type("l")
		},
		ThenTheyAreToldThisMachineHasNoCopyOfTheProject: func() {
			term.WaitFor("This machine has no copy of project-a")
		},
		AndNothingIsStartedOnThisMachine: func() {
			if strings.Contains(client.Sessions(), "[Created") {
				t.Fatalf("a session was started on the client:\n%s", client.Sessions())
			}
		},
	}, term0(&term))
}
