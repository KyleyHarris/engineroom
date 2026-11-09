//go:build journeys

package journeys

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/history"
	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// A client's connection to the engine dropping, as it does when the client sleeps or changes
// network: the engine's end of the developer's SSH connections is killed, so the client's ssh ends
// with its own failure status (255) while the session keeps running on the engine. Nothing is
// stubbed: the join is a real ssh to the engine container, and the drop is a real lost connection.
func dropConnection(t *testing.T, engine *journey.Engine) {
	t.Helper()
	engine.MustExec(`for p in $(ps -eo pid=,args= | awk '/sshd(-session)?: dev@/ {print $1}'); do kill -9 "$p"; done`)
}

// joinFromClient joins project-a from the client in a window opened only to join it (engineroom join,
// as the app opens it), and stays attached past tui.attachedFor, as a developer working in it is.
func joinFromClient(t *testing.T, client *journey.Machine, engine *journey.Engine) *termtest.Terminal {
	t.Helper()
	engine.StartSession("project-a", "/home/dev/src/project-a")
	term := client.Start(t, "join", "project-a")
	term.WaitFor("AGENT new conversation")
	time.Sleep(11 * time.Second)
	if !term.Running() {
		t.Fatalf("the join ended before the connection dropped:\n%s", term.Screen())
	}
	return term
}

// joins is the client's history entries joining project-a, oldest first.
func joins(t *testing.T, client *journey.Machine) []history.Entry {
	t.Helper()
	out, err := client.Run(client.Engineroom, "history", "--json")
	if err != nil {
		t.Fatalf("engineroom history: %v\n%s", err, out)
	}
	var all []history.Entry
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatalf("reading the history: %v\n%s", err, out)
	}
	var got []history.Entry
	for _, e := range all {
		if e.Title == "Join project-a" {
			got = append(got, e)
		}
	}
	return got
}

// waitForJoins waits until the history holds n joins of project-a.
func waitForJoins(t *testing.T, client *journey.Machine, n int) []history.Entry {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		j := joins(t, client)
		if len(j) >= n {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history holds %d joins of project-a, want %d", len(j), n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func rejoinSetup(t *testing.T) (*journey.Machine, *journey.Engine) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	return client, engine
}

// CONN-REJOIN-001: the connection drops while the session runs on, and the same window joins it
// again, with the join in the history. Fails if the program ends, as a session's end did before,
// or joins without recording it.
func TestConnRejoin001(t *testing.T) {
	client, engine := rejoinSetup(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRejoin001{
		GivenTheDeveloperHasJoinedASessionFromAClient: func() {
			term = joinFromClient(t, client, engine)
		},
		WhenTheConnectionToTheEngineDropsWhileTheSessionKeepsRunning: func() {
			dropConnection(t, engine)
		},
		ThenTheSameWindowJoinsThatSessionAgain: func() {
			waitForJoins(t, client, 2)
			term.WaitFor("AGENT new conversation")
			if !term.Running() {
				t.Fatalf("the program ended instead of joining again:\n%s", term.Screen())
			}
			if journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("the session is not running on the engine:\n%s", engine.Sessions())
			}
		},
		AndTheCommandsItRanToDoSoAreInTheHistory: func() {
			j := waitForJoins(t, client, 2)
			last := j[len(j)-1]
			if len(last.Commands) == 0 || !strings.Contains(last.Commands[len(last.Commands)-1].Display, "zellij attach project-a") {
				t.Fatalf("the join again does not record its command: %+v", last)
			}
		},
	}, term0(&term))
}

// CONN-REJOIN-002: while the engine does not answer, the window says so and waits; once it answers,
// the session is joined again. Fails if the window ends, shows nothing of the wait, or never
// joins again.
func TestConnRejoin002(t *testing.T) {
	client, engine := rejoinSetup(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRejoin002{
		GivenTheDeveloperHasJoinedASessionFromAClient: func() {
			term = joinFromClient(t, client, engine)
		},
		WhenTheConnectionToTheEngineDropsAndTheEngineDoesNotAnswer: func() {
			engine.SetReachable(client, false)
			dropConnection(t, engine)
		},
		ThenTheWindowSaysTheConnectionWasLostAndThatItJoinsAgainWhenTheEngineAnswers: func() {
			term.WaitFor("The connection to the engine engine was lost")
			term.WaitFor("joins it again as soon as the engine answers")
			term.WaitFor("It has not answered")
			if !term.Running() {
				t.Fatalf("the window ended while the engine did not answer:\n%s", term.Screen())
			}
		},
		AndOnceTheEngineAnswersItJoinsTheSessionAgain: func() {
			engine.SetReachable(client, true)
			term.WaitFor("AGENT new conversation")
			waitForJoins(t, client, 2)
		},
	}, term0(&term))
}

// CONN-REJOIN-003: a session stopped while the connection was down is not joined again, and
// nothing is started. Fails if the window joins or starts a session, or waits on.
func TestConnRejoin003(t *testing.T) {
	client, engine := rejoinSetup(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRejoin003{
		GivenTheDeveloperHasJoinedASessionFromAClient: func() {
			term = joinFromClient(t, client, engine)
		},
		WhenTheConnectionToTheEngineDropsAndTheSessionIsStoppedBeforeItComesBack: func() {
			engine.SetReachable(client, false)
			dropConnection(t, engine)
			term.WaitFor("joins it again as soon as the engine answers")
			engine.MustExec("engineroom session end project-a")
			engine.SetReachable(client, true)
		},
		ThenTheWindowDoesNotJoinASessionAgain: func() {
			term.WaitExit()
			if j := joins(t, client); len(j) != 1 {
				t.Fatalf("the history holds %d joins of project-a, want only the first", len(j))
			}
		},
		AndNoSessionIsStartedOnEitherMachine: func() {
			if s := journey.SessionState(engine.Sessions(), "project-a"); s != "" {
				t.Fatalf("project-a is %q on the engine after it was stopped:\n%s", s, engine.Sessions())
			}
			if s := client.Sessions(); strings.Contains(s, "project-a") {
				t.Fatalf("a session was started on the client:\n%s", s)
			}
		},
	}, term0(&term))
}

// CONN-REJOIN-004: q stops the waiting and joins nothing, even once the engine answers again. Fails
// if the window waits on or joins the session.
func TestConnRejoin004(t *testing.T) {
	client, engine := rejoinSetup(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnRejoin004{
		GivenTheWindowIsWaitingForTheEngineToAnswerAfterTheConnectionDropped: func() {
			term = joinFromClient(t, client, engine)
			engine.SetReachable(client, false)
			dropConnection(t, engine)
			term.WaitFor("joins it again as soon as the engine answers")
		},
		WhenTheDeveloperPressesQ: func() {
			term.Type("q")
		},
		ThenTheWindowStopsWaitingAndJoinsNothing: func() {
			term.WaitExit()
			engine.SetReachable(client, true)
			time.Sleep(4 * time.Second)
			if j := joins(t, client); len(j) != 1 {
				t.Fatalf("the history holds %d joins of project-a after stopping, want only the first", len(j))
			}
		},
	}, term0(&term))
}
