//go:build journeys

package journeys

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// The agent stand-in records each start as "start <dir> <args>". Remote Control is never
// started for real here: the stand-in only records the arguments the real agent would get.

// lastStart is the last agent start recorded, or "" when there is none.
func lastStart(starts []string) string {
	if len(starts) == 0 {
		return ""
	}
	return starts[len(starts)-1]
}

// SESS-NEW-004: starting a project without joining creates its session, running, and leaves
// the developer on the session list. Fails if no session is created, if the developer is put
// into the session, or if the list is not what they are left looking at.
func TestSessNew004(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessNew004{
		GivenAProjectHasNoSessionOnTheEngine: func() {
			if journey.SessionState(m.Sessions(), "project-a") != "" {
				t.Fatalf("a session already exists:\n%s", m.Sessions())
			}
		},
		WhenTheDeveloperStartsItWithoutJoining: func() {
			// With no sessions, the program opens on the projects; s starts without joining.
			term = m.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("project-a")
			term.Type("s")
		},
		ThenASessionIsCreatedInItsCheckoutAndIsRunning: func() {
			m.WaitSession("project-a", "running")
			deadlineAgent(t, m, path)
		},
		AndTheDeveloperStaysWhereTheyWereShownTheListOfSessions: func() {
			term.WaitFor("the session is running")
			term.WaitFor("Sessions on the engine")
			if strings.Contains(term.Screen(), "Zellij (project-a)") || strings.Contains(term.Screen(), "AGENT") {
				t.Fatalf("the developer was put into the session:\n%s", term.Screen())
			}
			if !term.Running() {
				t.Fatal("the program ended instead of showing the list")
			}
		},
	}, term0(&term))
}

// deadlineAgent waits until the agent stand-in has started in dir, so a session that exists but
// never started its agent is not taken for a working one.
func deadlineAgent(t *testing.T, m *journey.Machine, dir string) {
	t.Helper()
	for i := 0; i < 100 && !m.AgentStartedIn(dir); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !m.AgentStartedIn(dir) {
		t.Fatalf("the agent never started in %s: %q", dir, m.AgentStarts())
	}
}

// settledStarts is the agent's starts once they stop changing. A first session's agent is started
// twice in quick succession: `--continue`, which the stand-in refuses with no earlier conversation,
// then the plain start after `||`. Read between the two, the second start looked like a restart,
// which made SESS-REMOTE-004 fail now and then.
func settledStarts(m *journey.Machine) []string {
	last := m.AgentStarts()
	for still := 0; still < 3; {
		time.Sleep(200 * time.Millisecond)
		now := m.AgentStarts()
		if len(now) == len(last) {
			still++
		} else {
			still, last = 0, now
		}
	}
	return last
}

// SESS-REMOTE-001: the Remote Control choice is shown where a project is chosen, flips with one
// key, and is still on after the program is started again. Fails if the choice is not shown,
// does not change on screen, or is lost between runs (read from the screen of a new run, and
// from the configuration file the program wrote).
func TestSessRemote001(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessRemote001{
		GivenTheDeveloperIsChoosingAProjectToOpen: func() {
			term = m.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("Remote Control: off")
		},
		WhenTheyTurnRemoteControlOn: func() {
			term.Type("m")
		},
		ThenTheChoiceIsShownAsOn: func() {
			term.WaitFor("Remote Control: on")
		},
		AndItIsStillOnTheNextTimeTheProgramIsStarted: func() {
			term.Type("q")
			term.WaitExit()
			cfg, err := os.ReadFile(m.ConfigPath())
			if err != nil || !strings.Contains(string(cfg), "remote_control = true") {
				t.Fatalf("the choice was not remembered in the configuration (%v):\n%s", err, cfg)
			}
			term = m.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("Remote Control: on")
		},
	}, term0(&term))
}

// SESS-REMOTE-002: with Remote Control on, a new session's agent starts with it, under the
// session's own name. Fails if the agent starts without the flag, or with any name but the
// one Zellij lists the session by.
func TestSessRemote002(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessRemote002{
		GivenRemoteControlIsOn: func() {
			m.RememberRemoteControl()
		},
		AndAProjectHasNoSessionOnTheEngine: func() {
			if journey.SessionState(m.Sessions(), "project-a") != "" {
				t.Fatalf("a session already exists:\n%s", m.Sessions())
			}
		},
		WhenTheDeveloperOpensIt: func() {
			term = m.Start(t, "open", "project-a")
			term.WaitFor("AGENT new conversation")
		},
		ThenClaudeCodeInTheNewSessionStartsWithRemoteControl: func() {
			if !strings.Contains(lastStart(m.AgentStarts()), "--remote-control ") {
				t.Fatalf("the agent started without Remote Control: %q", m.AgentStarts())
			}
		},
		AndTheNameItIsReachedByIsTheSessionsName: func() {
			name := strings.TrimSpace(lastStart(m.AgentStarts())[strings.Index(lastStart(m.AgentStarts()), "--remote-control ")+len("--remote-control "):])
			if journey.SessionState(m.Sessions(), name) != "running" {
				t.Fatalf("Remote Control is named %q, which is not the running session's name:\n%s", name, m.Sessions())
			}
		},
	}, term0(&term))
}

// SESS-REMOTE-003: a client's own choice decides how a session on the engine starts. Fails if the
// engine's agent starts without Remote Control while the client asked for it.
func TestSessRemote003(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessRemote003{
		GivenRemoteControlIsOnForTheClientAndOffOnTheEngine: func() {
			client.RememberRemoteControl()
			if out, _ := engine.Exec("cat /home/dev/.config/engineroom/config.toml"); strings.Contains(out, "remote_control") {
				t.Fatalf("the engine has a Remote Control setting of its own:\n%s", out)
			}
		},
		WhenTheDeveloperOpensAProjectWithNoSessionFromTheClient: func() {
			term = client.Start(t, "open", "project-a")
			term.WaitFor("AGENT new conversation")
		},
		ThenClaudeCodeInTheNewSessionStartsWithRemoteControl: func() {
			if !strings.Contains(lastStart(engine.AgentStarts()), "--remote-control project-a") {
				t.Fatalf("the engine's agent did not start with Remote Control under the session's name: %q", engine.AgentStarts())
			}
		},
	}, term0(&term))
}

// SESS-REMOTE-004: turning Remote Control on never touches a running session: opening its
// project joins it as it is. Fails if the agent is started again, or started with Remote
// Control, or the developer lands anywhere but that session.
func TestSessRemote004(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	m.BeEngine("project-a", path)
	var term *termtest.Terminal
	var before []string

	scenarios.Run(t, scenarios.SessRemote004{
		GivenASessionIsRunningWithoutRemoteControl: func() {
			first := m.Start(t, "open", "project-a", "--no-join")
			first.WaitExit()
			m.WaitSession("project-a", "running")
			deadlineAgent(t, m, path)
			before = settledStarts(m)
		},
		AndRemoteControlIsOn: func() {
			m.RememberRemoteControl()
		},
		WhenTheDeveloperOpensItsProject: func() {
			term = m.Start(t, "open", "project-a")
		},
		ThenTheyJoinThatSessionAsItIs: func() {
			term.WaitFor("Zellij (project-a)")
			term.WaitFor("AGENT new conversation")
		},
		AndItIsNotRestarted: func() {
			after := m.AgentStarts()
			if len(after) != len(before) {
				t.Fatalf("the agent was started again: before %q, after %q", before, after)
			}
			for _, s := range after {
				if strings.Contains(s, "--remote-control") {
					t.Fatalf("a start with Remote Control was made: %q", after)
				}
			}
		},
	}, term0(&term))
}
