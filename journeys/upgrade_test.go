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
)

// APP-UPGRADE-001: the app's Upgrade starts a Claude session of its own on this machine, briefed
// with the steps, the running build and where the app is, and asked to upgrade at once. Started
// with Remote Control alone, so no terminal window opens. Fails if no session starts, the brief
// lacks the steps or this machine's facts, or the agent is not given the brief and the request.
func TestAppUpgrade001(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	app := filepath.Join(m.AppsDir(), "engineroom.app")

	scenarios.Run(t, scenarios.AppUpgrade001{
		GivenThisMachineRunsEngineroomWithItsApp: func() {
			if err := os.MkdirAll(app, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperAsksToUpgradeThisMachine: func() {
			// Without Remote Control, the app's default: the opening followed --add-dir there and
			// was taken as a folder, so the session sat at an empty prompt.
			mustRun(t, m, "upgrade", "--mode", "zellij")
		},
		ThenAnUpgradeSessionStartsOnThisMachine: func() {
			m.WaitSession("engineroom-upgrade", "running")
		},
		AndClaudeIsToldTheStepsThisMachinesBuildAndWhereItsAppIs: func() {
			out, err := m.Run("sh", "-c", `cat "$1"/.local/state/engineroom/assistant/engineroom-upgrade.md`, "sh", m.Home)
			if err != nil {
				t.Fatalf("no brief: %v\n%s", err, out)
			}
			for _, want := range []string{"## The steps", "git -C <clone> pull --ff-only origin main", "engineroom app relaunch", "Stop and ask before anything that could lose work", "Running build: engineroom ", "Menu bar app: installed at " + app} {
				if !strings.Contains(out, want) {
					t.Fatalf("the brief lacks %q:\n%s", want, out)
				}
			}
		},
		AndClaudeIsAskedToUpgradeItNow: func() {
			var start string
			for i := 0; i < 50 && start == ""; i++ {
				for _, s := range m.AgentStarts() {
					if strings.Contains(s, "engineroom-upgrade.md") {
						start = s
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !strings.Contains(start, "--append-system-prompt-file") || strings.Contains(start, "--continue") {
				t.Fatalf("the agent was not started with the brief: %q", start)
			}
			// The request is the prompt Claude Code takes from those arguments, not merely among
			// them.
			asked := false
			for _, p := range m.AgentPrompts() {
				asked = asked || strings.Contains(p, "now, following your instructions")
			}
			if !asked {
				t.Fatalf("the agent was not asked to upgrade: prompts %q from start %q", m.AgentPrompts(), start)
			}
		},
	}, nil)
}

// APP-UPGRADE-002: a client's "Upgrade the engine" starts the upgrade session on the engine, over
// SSH, and nothing here. Fails if the session is not on the engine or one starts on the client.
func TestAppUpgrade002(t *testing.T) {
	client, engine := clientAndEngine(t)

	scenarios.Run(t, scenarios.AppUpgrade002{
		GivenThisMachineIsAClientOfAnEngineThatAnswers: func() {},
		WhenTheDeveloperAsksToUpgradeTheEngine: func() {
			mustRun(t, client, "upgrade", "--engine", "--mode", "remote")
		},
		ThenAnUpgradeSessionStartsOnTheEngine: func() {
			if journey.SessionState(engine.Sessions(), "engineroom-upgrade") != "running" {
				t.Fatalf("no upgrade session on the engine:\n%s", engine.Sessions())
			}
		},
		AndNothingIsStartedOnThisMachine: func() {
			if strings.Contains(client.Sessions(), "engineroom-upgrade") {
				t.Fatalf("an upgrade session started on the client:\n%s", client.Sessions())
			}
		},
	}, nil)
}

// APP-UPGRADE-003: pressing Upgrade with an upgrade session left from before (idle, its upgrade
// done or never asked) ends it and starts a new one, which is asked to upgrade. Fails if the old
// session is joined or kept, so the agent is started only once.
func TestAppUpgrade003(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	upgradeStarts := func() int {
		n := 0
		for _, s := range m.AgentStarts() {
			if strings.Contains(s, "engineroom-upgrade.md") {
				n++
			}
		}
		return n
	}

	scenarios.Run(t, scenarios.AppUpgrade003{
		GivenAnUpgradeSessionFromAnEarlierUpgradeIsStillOnThisMachine: func() {
			mustRun(t, m, "upgrade", "--mode", "remote")
			m.WaitSession("engineroom-upgrade", "running")
			for i := 0; i < 50 && upgradeStarts() < 1; i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if upgradeStarts() != 1 {
				t.Fatalf("the earlier upgrade session's agent did not start: %q", m.AgentStarts())
			}
		},
		WhenTheDeveloperAsksToUpgradeThisMachine: func() {
			mustRun(t, m, "upgrade", "--mode", "remote")
		},
		ThenTheEarlierUpgradeSessionIsEndedAndANewOneStarts: func() {
			m.WaitSession("engineroom-upgrade", "running")
			for i := 0; i < 50 && upgradeStarts() < 2; i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if upgradeStarts() != 2 {
				t.Fatalf("no new upgrade session started; agent starts %q", m.AgentStarts())
			}
		},
		AndClaudeIsAskedToUpgradeItNow: func() {
			asked := 0
			for _, p := range m.AgentPrompts() {
				if strings.Contains(p, "now, following your instructions") {
					asked++
				}
			}
			if asked != 2 {
				t.Fatalf("the new session was not asked to upgrade: prompts %q", m.AgentPrompts())
			}
		},
	}, nil)
}
