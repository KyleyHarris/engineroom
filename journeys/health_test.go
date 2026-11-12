//go:build journeys

package journeys

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/health"
	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/setup"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// The health check's journeys run the built program on an isolated machine with real Zellij
// sessions, real status scripts in a project's settings and real processes in those sessions.
// Three things are stood in, each answering exactly what the check reads of the real one:
//
//   - Claude Code, as claude on the PATH: it answers the setup check's questions (its version, its
//     plugins) and, started as the agent in a session, keeps sessions/<pid>.json in its folder as
//     Claude Code does, with bridgeSessionId while Remote Control is on (D-037), then becomes a
//     long-running built program (listen idle) under the same pid. A built program because macOS
//     hides the environment of its own tools (sh, sleep), and the check finds a session's agents by
//     their environment.
//   - pmset and ioreg, which read the power settings and the lid: the machine running the journeys
//     has its own, which a journey must neither depend on nor change.

// claudeStandIn is Claude Code for the health journeys. The setup answers are the ones
// standInClaude gives; any other start is the agent in a session.
const claudeStandIn = `case "$1" in
--version) echo "2.1.0 (Claude Code)" ;;
plugin) echo '[{"id":"engineroom@engineroom","version":"0.1.0","scope":"user","enabled":true}]' ;;
*)
  dir="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/sessions"
  mkdir -p "$dir"
  bridge=""
  for a in "$@"; do
    if [ "$a" = --remote-control ]; then bridge=', "bridgeSessionId": "session_stand-in"'; fi
  done
  printf '{"pid": %s, "cwd": "%s"%s}\n' "$$" "$(pwd)" "$bridge" > "$dir/$$.json"
  exec listen idle ;;
esac
`

// standInPower is pmset reporting the given sleep setting under AC Power, laid out as
// `pmset -g custom` lays it out. Battery Power sleeps after a minute, so a check that read the
// wrong section would say so.
func standInPower(t *testing.T, m *journey.Machine, acSleep string) {
	t.Helper()
	tool(t, m, "pmset", `cat <<'EOF'
Battery Power:
 displaysleep         30
 sleep                1
 disksleep            10
AC Power:
 displaysleep         30
 sleep                `+acSleep+`
 disksleep            10
EOF
`)
}

// standInLid is ioreg describing the lid: causesSleep is AppleClamshellCausesSleep ("Yes" or "No"),
// and "" is a Mac with no lid, for which ioreg lists nothing.
func standInLid(t *testing.T, m *journey.Machine, causesSleep string) {
	t.Helper()
	if causesSleep == "" {
		tool(t, m, "ioreg", "exit 0\n")
		return
	}
	tool(t, m, "ioreg", `cat <<'EOF'
      "AppleClamshellCausesSleep" = `+causesSleep+`
      "AppleClamshellState" = No
EOF
`)
}

// healthEngine is a machine set up as the engine with the given projects (each a repository under
// its home, registered), Claude Code as the agent, and an engine that stays awake on power and with
// its lid closed. It returns the machine and each project's folder.
func healthEngine(t *testing.T, projects ...string) (*journey.Machine, map[string]string) {
	t.Helper()
	m := journey.NewMachine(t)
	paths := map[string]string{}
	var b strings.Builder
	b.WriteString("role = \"engine\"\n[agent]\ncommand = \"claude\"\n[editor]\ncommand = \"fake-editor\"\n")
	for _, name := range projects {
		paths[name] = m.NewRepo(name)
		fmt.Fprintf(&b, "[[project]]\nname = %q\npath = %q\n", name, paths[name])
	}
	m.WriteConfig(b.String())
	tool(t, m, "claude", claudeStandIn)
	standInPower(t, m, "0")
	standInLid(t, m, "No")
	return m, paths
}

// projectScript writes an executable script into a project's folder in the developer's settings,
// where engineroom looks before the repository (D-006).
func projectScript(t *testing.T, m *journey.Machine, project, name, body string) {
	t.Helper()
	out, err := m.Run("engineroom", "settings", "where", project)
	if err != nil {
		t.Fatalf("engineroom settings where: %v\n%s", err, out)
	}
	m.WriteFile(filepath.Join(strings.TrimSpace(out), name), "#!/bin/sh\n"+body, 0o755)
}

// startAgent starts a project's session as the app starts it ("zellij" without Remote Control,
// "remote" with it) and returns the Claude Code session file its agent keeps, once the agent is
// running as the long-lived process whose environment the check reads.
func startAgent(t *testing.T, m *journey.Machine, project, dir, mode string) string {
	t.Helper()
	mustRun(t, m, "start", project, "--mode", mode)
	m.WaitSession(project, "running")
	deadline := time.Now().Add(15 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(m.Home, ".claude", "sessions", "*.json"))
		for _, f := range files {
			s := sessionFile(t, f)
			if resolve(fmt.Sprint(s["cwd"])) != resolve(dir) {
				continue
			}
			pid := strconv.Itoa(int(s["pid"].(float64)))
			if out, err := exec.Command("ps", "-o", "comm=", "-p", pid).Output(); err == nil && strings.Contains(string(out), "listen") {
				return f
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent of %s never kept a session file in %s: %q", project, filepath.Join(m.Home, ".claude", "sessions"), files)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func sessionFile(t *testing.T, path string) map[string]any {
	t.Helper()
	var s map[string]any
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		t.Fatalf("Claude Code's session file %s: %v", path, err)
	}
	return s
}

// turnOnInside is Remote Control turned on inside a running session with /remote-control: Claude
// Code adds bridgeSessionId to its session file, and nothing else about the process changes.
func turnOnInside(t *testing.T, file string) {
	t.Helper()
	s := sessionFile(t, file)
	s["bridgeSessionId"] = "session_turned-on-inside"
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// runHealth runs `engineroom health` (with args) in a terminal, so a recording shows the report,
// and reads what it printed from a second run outside the terminal, as runCheck does for the setup
// check: the terminal wraps long lines. The two must end the same way.
func runHealth(t *testing.T, m *journey.Machine, term **termtest.Terminal, args ...string) checkRun {
	t.Helper()
	argv := append([]string{"health"}, args...)
	*term = m.Start(t, argv...)
	shown := (*term).WaitExit()
	out, exit := plainHealth(t, m, args...)
	if exit != shown {
		t.Fatalf("the health check ended with %d in the terminal and %d outside it", shown, exit)
	}
	return checkRun{out: out, exit: exit}
}

// plainHealth runs `engineroom health` with no terminal and returns what it printed and its exit.
func plainHealth(t *testing.T, m *journey.Machine, args ...string) (string, int) {
	t.Helper()
	out, err := m.Command("engineroom", append([]string{"health"}, args...)...).Output()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("the health check did not run: %v", err)
		}
		exit = ee.ExitCode()
	}
	return string(out), exit
}

// healthJSON is the report as the app reads it, `engineroom health --json`.
func healthJSON(t *testing.T, m *journey.Machine) health.Report {
	t.Helper()
	out, _ := plainHealth(t, m, "--json")
	var r health.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("engineroom health --json: %v\n%s", err, out)
	}
	return r
}

// itemOf is the named item of a report.
func itemOf(t *testing.T, r health.Report, name string) health.Item {
	t.Helper()
	for _, it := range r.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item %q in the report: %+v", name, r.Items)
	return health.Item{}
}

// marked is how many of the report's lines mark an item present or missing.
func marked(out string) int {
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "present  ") || strings.HasPrefix(l, "missing  ") {
			n++
		}
	}
	return n
}

// HEALTH-CHECK-001: the engine with two running sessions lists every setup item the setup check
// reports, both ways of staying awake, and each session's Remote Control and app, each marked.
// Fails if any item is left out, a session lacks either of its items, or an item is not marked.
func TestHealthCheck001(t *testing.T) {
	m, paths := healthEngine(t, "project-a", "project-b")
	var term *termtest.Terminal
	var run checkRun
	var report health.Report
	scenarios.Run(t, scenarios.HealthCheck001{
		GivenTheEngineHasTwoRunningSessionS: func() {
			projectScript(t, m, "project-a", "status", "echo web answering\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
			startAgent(t, m, "project-b", paths["project-b"], "zellij")
		},
		WhenTheDeveloperRunsTheHealthCheckOnTheEngine: func() {
			run = runHealth(t, m, &term)
			report = healthJSON(t, m)
			if !strings.Contains(run.out, "Health of the engine, checked on this machine.") {
				t.Fatalf("the report does not say where it was checked:\n%s", run.out)
			}
		},
		ThenEveryItemTheSetupCheckReportsForTheEngineIsListed: func() {
			raw, _ := m.Command("engineroom", "setup", "check", "--json").Output()
			var s setup.Report
			if err := json.Unmarshal(raw, &s); err != nil || len(s.Items) == 0 {
				t.Fatalf("the setup check's report: %v\n%s", err, raw)
			}
			for _, want := range s.Items {
				got := itemOf(t, report, want.Name)
				if got.Group != health.Setup || got.Present != want.Present {
					t.Fatalf("the setup item %q is %+v in the health check, %+v in the setup check", want.Name, got, want)
				}
				itemLines(t, run.out, want.Name)
			}
		},
		AndStayingAwakeOnPowerAndStayingAwakeWithTheLidClosedAreListed: func() {
			for _, name := range []string{"stays awake on power", "stays awake with the lid closed"} {
				if !strings.HasPrefix(itemLines(t, run.out, name), "present") {
					t.Fatalf("%q is not present on an engine set to stay awake:\n%s", name, run.out)
				}
			}
		},
		AndEachRunningSessionIsListedWithItsRemoteControlAndItsApp: func() {
			for _, s := range []string{"project-a", "project-b"} {
				for _, name := range []string{"Remote Control in " + s, "the app of " + s} {
					if it := itemOf(t, report, name); it.Session != s || it.Group != health.Session {
						t.Fatalf("%q is not the item of the session %s: %+v", name, s, it)
					}
					itemLines(t, run.out, name)
				}
			}
		},
		AndEachItemIsMarkedPresentOrMissing: func() {
			if n := marked(run.out); n != len(report.Items) {
				t.Fatalf("%d lines are marked, the report has %d items:\n%s", n, len(report.Items), run.out)
			}
		},
	}, term0(&term))
}

// healthTree is every file and folder under dir with its size, time and mode, except Zellij's own cache,
// which Zellij rewrites every second while its sessions run and which no check writes to.
func healthTree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && strings.Contains(info.Name(), "Zellij") {
			return filepath.SkipDir
		}
		lines = append(lines, p+" "+strconv.FormatInt(info.Size(), 10)+" "+info.ModTime().String()+" "+info.Mode().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

// HEALTH-CHECK-002: on an engine that sleeps on power, every item names the command or file it was
// checked with, staying awake on power is missing with its fix, and the machine's whole home is the
// same before and after (Zellij's own cache aside). Fails if an item hides how it was checked, the
// setting is not reported, or the check writes anything, a history entry included.
func TestHealthCheck002(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	var before string
	scenarios.Run(t, scenarios.HealthCheck002{
		GivenTheEngineSleepsOnPower: func() {
			standInPower(t, m, "10")
			projectScript(t, m, "project-a", "status", "echo web answering\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
			before = healthTree(t, m.Home)
		},
		WhenTheDeveloperRunsTheHealthCheckOnTheEngine: func() { run = runHealth(t, m, &term) },
		ThenEachItemShowsTheCommandItWasCheckedWithOrTheFileItWasReadFrom: func() {
			r := healthJSON(t, m)
			for _, it := range r.Items {
				if strings.TrimSpace(it.Checked) == "" {
					t.Fatalf("%q does not say how it was checked: %+v", it.Name, it)
				}
				if block := itemLines(t, run.out, it.Name); !strings.Contains(block, "checked with: "+it.Checked) {
					t.Fatalf("%q does not show how it was checked:\n%s", it.Name, block)
				}
			}
			// The awake items name commands that exist here, and the app item names the script.
			for name, first := range map[string]string{"stays awake on power": "pmset", "stays awake with the lid closed": "ioreg"} {
				if !strings.HasPrefix(itemOf(t, r, name).Checked, first+" ") {
					t.Fatalf("%q was not checked with %s: %s", name, first, itemOf(t, r, name).Checked)
				}
			}
			if app := itemOf(t, r, "the app of project-a"); !strings.Contains(app.Checked, "status") {
				t.Fatalf("the app is not said as checked by its status script: %s", app.Checked)
			}
		},
		AndStayingAwakeOnPowerIsMarkedMissingWithItsFix: func() {
			p := itemLines(t, run.out, "stays awake on power")
			if !strings.HasPrefix(p, "missing") || !strings.Contains(p, "fix: ") || !strings.Contains(p, "sudo pmset -c sleep 0") || run.exit != 1 {
				t.Fatalf("an engine that sleeps on power is reported (exit %d) as:\n%s", run.exit, p)
			}
		},
		AndNothingOnTheEngineHasChanged: func() {
			if after := healthTree(t, m.Home); after != before {
				t.Fatalf("the check changed the engine:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		},
	}, term0(&term))
}

// readyEngine is the throwaway engine with every item present: Claude Code with the plugin, the
// program found by an SSH command's shell, a project with a running session whose status script
// says its app runs, and pmset and ioreg saying it stays awake. The container is Linux, so the two
// are stand-ins there as they are on a journey machine.
func readyEngine(t *testing.T, client *journey.Machine) *journey.Engine {
	t.Helper()
	engine := journey.NewEngine(t, client)
	client.BeClientOf(engine, "engine")
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	engine.MustExec(`set -e
printf '%s\n' 'export PATH="$HOME/bin:$PATH"' > ~/.zshenv
cat > bin/claude <<'EOF'
#!/bin/sh
case "$1" in
--version) echo "2.1.0 (Claude Code)" ;;
plugin) echo '[{"id":"engineroom@engineroom","version":"0.1.0","scope":"user","enabled":true}]' ;;
esac
EOF
printf '#!/bin/sh\nprintf "AC Power:\\n sleep                0\\n"\n' > bin/pmset
printf '#!/bin/sh\nexit 0\n' > bin/ioreg
mkdir -p .config/engineroom/settings/projects/project-a
printf '#!/bin/sh\necho web answering\n' > .config/engineroom/settings/projects/project-a/status
chmod +x bin/claude bin/pmset bin/ioreg .config/engineroom/settings/projects/project-a/status`)
	engine.StartSession("project-a", "/home/dev/src/project-a")
	return engine
}

// HEALTH-CHECK-003: a client's health check is the engine's report, asked over SSH: the engine's
// own items and its session, none of the client's; with the engine not reached, the cause is said
// and nothing is marked present. Fails if the client checks itself, or an unreached engine reads as
// a report with anything present.
func TestHealthCheck003(t *testing.T) {
	client := journey.NewMachine(t)
	var engine *journey.Engine
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthCheck003{
		GivenThisMachineIsAClient: func() { engine = readyEngine(t, client) },
		WhenTheDeveloperRunsTheHealthCheck: func() {
			run = runHealth(t, client, &term)
		},
		ThenTheItemsAreTheEnginesCheckedOnTheEngine: func() {
			r := healthJSON(t, client)
			if r.Machine != "engine" || !strings.Contains(run.out, "Health of the engine, checked on engine.") {
				t.Fatalf("not said as checked on the engine (%q):\n%s", r.Machine, run.out)
			}
			// The session runs on the engine and not here, so its items can only be the engine's.
			if s := client.Sessions(); strings.Contains(s, "project-a") {
				t.Fatalf("the client has a session of its own: %s", s)
			}
			for _, name := range []string{"Remote Control in project-a", "the app of project-a", "engineroom found by an SSH command"} {
				itemLines(t, run.out, name)
			}
			for _, it := range r.Items {
				if it.For == setup.Client {
					t.Fatalf("a client's item is in the engine's report: %+v", it)
				}
			}
		},
		AndWhenTheEngineIsNotReachedThatIsSaidWithItsCauseAndNoItemIsMarkedPresent: func() {
			engine.SetReachable(client, false)
			again := runHealth(t, client, &term)
			if again.exit != 1 || !strings.HasPrefix(again.out, "The engine was not reached, so nothing was checked: ") || !strings.Contains(again.out, "did not answer") {
				t.Fatalf("an unreached engine is reported (exit %d) as:\n%s", again.exit, again.out)
			}
			if n := marked(again.out); n != 0 {
				t.Fatalf("%d items are marked while the engine was not reached:\n%s", n, again.out)
			}
			if r := healthJSON(t, client); r.Unreached == "" || len(r.Items) != 0 || r.Missing != 0 && r.Ready() {
				t.Fatalf("as JSON, an unreached engine is %+v", r)
			}
		},
	}, term0(&term))
}

// HEALTH-AWAKE-001: an engine that sleeps after ten minutes on power (and after one on battery,
// which is not what is asked) is missing, with the setting read and its fix. Fails if the battery
// setting is read in its place, or the setting or fix is not said.
func TestHealthAwake001(t *testing.T) {
	m, _ := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthAwake001{
		GivenTheEngineIsSetToSleepAfterATimeOnPower: func() { standInPower(t, m, "10") },
		WhenTheDeveloperRunsTheHealthCheck:          func() { run = runHealth(t, m, &term) },
		ThenStayingAwakeOnPowerIsMarkedMissingWithTheSettingThatWasReadAndItsFix: func() {
			p := itemLines(t, run.out, "stays awake on power")
			if !strings.HasPrefix(p, "missing  stays awake on power: sleep 10 on power: sleeps after 10 minutes idle") ||
				!strings.Contains(p, "checked with: pmset -g custom (sleep under AC Power)") || !strings.Contains(p, "fix: System Settings, Battery") {
				t.Fatalf("an engine that sleeps on power is reported as:\n%s", p)
			}
		},
	}, term0(&term))
}

// HEALTH-AWAKE-002: a laptop that would sleep if its lid were closed now is missing, with its fix;
// a Mac with no lid is present, said as having none. Fails if either is read the other way, or the
// laptop's fix is not given.
func TestHealthAwake002(t *testing.T) {
	m, _ := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthAwake002{
		GivenTheEngineIsALaptopThatWouldSleepIfItsLidWereClosedNow: func() { standInLid(t, m, "Yes") },
		WhenTheDeveloperRunsTheHealthCheck:                         func() { run = runHealth(t, m, &term) },
		ThenStayingAwakeWithTheLidClosedIsMarkedMissingWithItsFix: func() {
			l := itemLines(t, run.out, "stays awake with the lid closed")
			if !strings.HasPrefix(l, "missing  stays awake with the lid closed: closing the lid now would sleep it") || !strings.Contains(l, "fix: ") ||
				!strings.Contains(l, "sudo pmset -a disablesleep 1") {
				t.Fatalf("a laptop that sleeps with its lid closed is reported as:\n%s", l)
			}
		},
		AndOnAnEngineWithNoLidItIsMarkedPresentAsHavingNoLid: func() {
			standInLid(t, m, "")
			again := runHealth(t, m, &term)
			if l := itemLines(t, again.out, "stays awake with the lid closed"); !strings.HasPrefix(l, "present  stays awake with the lid closed: no lid") {
				t.Fatalf("a Mac with no lid is reported as:\n%s", l)
			}
		},
	}, term0(&term))
}

// HEALTH-REMOTE-001: three sessions, one started with Remote Control, one where it was turned on
// inside afterwards, one without: the first two read on, the third off. Fails if Remote Control is
// read from how the session was started rather than from its running agent.
func TestHealthRemote001(t *testing.T) {
	m, paths := healthEngine(t, "project-a", "project-b", "project-c")
	var term *termtest.Terminal
	var r health.Report
	scenarios.Run(t, scenarios.HealthRemote001{
		GivenASessionStartedWithRemoteControl: func() {
			startAgent(t, m, "project-a", paths["project-a"], "remote")
		},
		AndASessionStartedWithoutItWhereRemoteControlWasThenTurnedOnInsideIt: func() {
			turnOnInside(t, startAgent(t, m, "project-b", paths["project-b"], "zellij"))
		},
		AndASessionWithoutRemoteControl: func() {
			startAgent(t, m, "project-c", paths["project-c"], "zellij")
		},
		WhenTheDeveloperRunsTheHealthCheck: func() {
			runHealth(t, m, &term)
			r = healthJSON(t, m)
		},
		ThenTheFirstTwoAreShownWithRemoteControlOn: func() {
			for _, s := range []string{"project-a", "project-b"} {
				if it := itemOf(t, r, "Remote Control in "+s); !it.Present || !strings.HasPrefix(it.Found, "on (Claude pid ") {
					t.Fatalf("Remote Control in %s is not shown on: %+v", s, it)
				}
			}
		},
		AndTheThirdIsShownWithRemoteControlOff: func() {
			if it := itemOf(t, r, "Remote Control in project-c"); !strings.HasPrefix(it.Found, "off") {
				t.Fatalf("Remote Control in project-c is not shown off: %+v", it)
			}
		},
	}, term0(&term))
}

// HEALTH-REMOTE-002: with Remote Control chosen, a session running without it is missing, with
// /remote-control to type inside it, and its agent is the same process after the check. Fails if
// it is counted present, the fix restarts it, or the check touches the running agent.
func TestHealthRemote002(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	var file string
	var pid string
	scenarios.Run(t, scenarios.HealthRemote002{
		GivenRemoteControlIsOnInTheEnginesChoice: func() { m.RememberRemoteControl() },
		AndASessionIsRunningWithoutRemoteControl: func() {
			// The app's start without Remote Control, whatever the remembered choice.
			file = startAgent(t, m, "project-a", paths["project-a"], "zellij")
			if _, on := sessionFile(t, file)["bridgeSessionId"]; on {
				t.Fatal("the session started with Remote Control")
			}
			pid = strconv.Itoa(int(sessionFile(t, file)["pid"].(float64)))
		},
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, m, &term) },
		ThenItsRemoteControlIsMarkedMissingWithRemoteControlToRunInsideIt: func() {
			rc := itemLines(t, run.out, "Remote Control in project-a")
			if !strings.HasPrefix(rc, "missing  Remote Control in project-a: off; chosen because agent.remote_control is on") ||
				!strings.Contains(rc, "fix: in the session, type /remote-control to Claude") {
				t.Fatalf("a session without Remote Control, with it chosen, is reported as:\n%s", rc)
			}
		},
		AndTheSessionIsNotRestarted: func() {
			if out, err := exec.Command("ps", "-o", "comm=", "-p", pid).Output(); err != nil || !strings.Contains(string(out), "listen") {
				t.Fatalf("the agent %s is no longer running: %v %s", pid, err, out)
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(file), "*.json"))
			if len(files) != 1 || journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("an agent was started again (%q), or the session is not running:\n%s", files, m.Sessions())
			}
		},
	}, term0(&term))
}

// HEALTH-APP-001: a status script that ends with success marks the app present, with what it
// printed. Fails if the script's word is not taken, or its output is not shown.
func TestHealthApp001(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthApp001{
		GivenARunningSessionWhoseProjectHasAStatusScript: func() {
			projectScript(t, m, "project-a", "status", "echo web answering on its port\nexit 0\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
		},
		AndTheScriptEndsWithSuccess:        func() {},
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, m, &term) },
		ThenTheSessionsAppIsMarkedPresentWithWhatTheScriptPrinted: func() {
			if a := itemLines(t, run.out, "the app of project-a"); !strings.HasPrefix(a, "present  the app of project-a: running: web answering on its port") {
				t.Fatalf("a running app is reported as:\n%s", a)
			}
		},
	}, term0(&term))
}

// HEALTH-APP-002: a status script that ends with a failure marks the app missing with what it
// printed, and the fix is engineroom up with the session's name. Fails if a failing script reads as
// running, its words are lost, or the fix is not the program's own start.
func TestHealthApp002(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthApp002{
		GivenARunningSessionWhoseProjectHasAStatusScript: func() {
			projectScript(t, m, "project-a", "up", "exit 0\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
		},
		AndTheScriptEndsWithAFailure: func() {
			projectScript(t, m, "project-a", "status", "echo web not answering\nexit 1\n")
		},
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, m, &term) },
		ThenTheSessionsAppIsMarkedMissingWithWhatTheScriptPrinted: func() {
			if a := itemLines(t, run.out, "the app of project-a"); !strings.HasPrefix(a, "missing  the app of project-a: not running: web not answering") {
				t.Fatalf("an app that is not running is reported as:\n%s", a)
			}
		},
		AndItsFixIsStartingTheAppWithEngineroomUpAndTheSessionsName: func() {
			if a := itemLines(t, run.out, "the app of project-a"); !strings.Contains(a, "fix: start the app: engineroom up project-a") {
				t.Fatalf("the fix is not engineroom up project-a:\n%s", a)
			}
		},
	}, term0(&term))
}

// HEALTH-APP-003: a project with an up script and no status script is missing, as not checked, with
// what a status script must do and where it goes. Fails if it is counted running unproved, or the
// fix does not say where the script goes.
func TestHealthApp003(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthApp003{
		GivenARunningSessionWhoseProjectHasAnUpScriptAndNoStatusScript: func() {
			projectScript(t, m, "project-a", "up", "exit 0\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
		},
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, m, &term) },
		ThenTheSessionsAppIsMarkedMissingAsNotChecked: func() {
			if a := itemLines(t, run.out, "the app of project-a"); !strings.HasPrefix(a, "missing  the app of project-a: not checked: project-a has no status script") {
				t.Fatalf("an app with no status script is reported as:\n%s", a)
			}
		},
		AndItsFixSaysWhatAStatusScriptMustDoAndWhereItGoes: func() {
			a := itemLines(t, run.out, "the app of project-a")
			for _, want := range []string{"engineroom settings where project-a", ".engineroom/status", "ends with success when everything the app needs is running", "must change nothing"} {
				if !strings.Contains(a, want) {
					t.Fatalf("the fix does not say %q:\n%s", want, a)
				}
			}
		},
	}, term0(&term))
}

// HEALTH-APP-004: a project with neither script has no app to run, and is present. Fails if it is
// reported missing.
func TestHealthApp004(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthApp004{
		GivenARunningSessionWhoseProjectHasNeitherAnUpScriptNorAStatusScript: func() {
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
		},
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, m, &term) },
		ThenTheSessionsAppIsMarkedPresentAsHavingNoAppToRun: func() {
			if a := itemLines(t, run.out, "the app of project-a"); !strings.HasPrefix(a, "present  the app of project-a: no app to run: project-a has no up script") {
				t.Fatalf("a project with no app is reported as:\n%s", a)
			}
		},
	}, term0(&term))
}

// HEALTH-APP-005: a status script that never ends is stopped at its time and reported, and the
// other session's app and every other item still come back. Fails if one script holds the report,
// or the rest is left out.
func TestHealthApp005(t *testing.T) {
	m, paths := healthEngine(t, "project-a", "project-b")
	var term *termtest.Terminal
	var run checkRun
	var took time.Duration
	scenarios.Run(t, scenarios.HealthApp005{
		GivenARunningSessionWhoseStatusScriptDoesNotEnd: func() {
			projectScript(t, m, "project-a", "status", "echo checking\nsleep 600\n")
			projectScript(t, m, "project-b", "status", "echo web answering\n")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
			startAgent(t, m, "project-b", paths["project-b"], "zellij")
		},
		WhenTheDeveloperRunsTheHealthCheck: func() {
			start := time.Now()
			run = runHealth(t, m, &term)
			took = time.Since(start)
		},
		ThenThatSessionsAppIsMarkedMissingAsNotAnsweringWithinItsTime: func() {
			a := itemLines(t, run.out, "the app of project-a")
			if !strings.HasPrefix(a, "missing  the app of project-a: not answering: the status script did not end within 10s") {
				t.Fatalf("a status script that does not end is reported as:\n%s", a)
			}
			// Two runs (the terminal's and the plain one), each held only by the script's time.
			if took > 2*(health.StatusTimeout+5*time.Second) {
				t.Fatalf("the check took %s", took)
			}
		},
		AndEveryOtherItemIsStillReported: func() {
			if a := itemLines(t, run.out, "the app of project-b"); !strings.HasPrefix(a, "present  the app of project-b: running: web answering") {
				t.Fatalf("the other session's app is reported as:\n%s", a)
			}
			for _, name := range []string{"git", "stays awake on power", "stays awake with the lid closed", "Remote Control in project-a", "Remote Control in project-b"} {
				itemLines(t, run.out, name)
			}
		},
	}, term0(&term))
}

// HEALTH-FIX-001: the app's Fix for an app that is not running is `api run up <session>`, the action
// the report names. Its commands are planned (and shown) before any runs, the start is kept in the
// history with each command and its outcome, and nothing else runs: the other session's scripts
// and agents are untouched. Fails if asking runs something other than the named fix, or the run is
// hidden from the plan or the history.
func TestHealthFix001(t *testing.T) {
	m, paths := healthEngine(t, "project-a", "project-b")
	log := filepath.Join(m.Home, "stack.log")
	var term *termtest.Terminal
	var fix []string
	var done map[string]any
	var agents []string
	var before int
	// history is every action in the machine's history, newest first.
	history := func() []map[string]any {
		out, err := m.Run("engineroom", "history", "--json")
		var entries []map[string]any
		if err == nil {
			err = json.Unmarshal([]byte(out), &entries)
		}
		if err != nil {
			t.Fatalf("engineroom history: %v\n%s", err, out)
		}
		return entries
	}
	scenarios.Run(t, scenarios.HealthFix001{
		GivenTheHealthCheckReportsASessionsAppAsNotRunning: func() {
			for _, p := range []string{"project-a", "project-b"} {
				projectScript(t, m, p, "up", stackRecord("up", log))
				projectScript(t, m, p, "status", "echo web not answering\nexit 1\n")
				startAgent(t, m, p, paths[p], "zellij")
			}
			agents, _ = filepath.Glob(filepath.Join(m.Home, ".claude", "sessions", "*.json"))
			term = m.Start(t, "health")
			term.WaitExit()
			a := itemOf(t, healthJSON(t, m), "the app of project-a")
			if a.Present || strings.Join(a.Action, " ") != "up project-a" {
				t.Fatalf("the app of project-a is not missing with a fix the program runs: %+v", a)
			}
			fix = a.Action
		},
		WhenTheDeveloperAsksForThatFix: func() {
			if lines := readLog(t, log); len(lines) != 0 {
				t.Fatalf("an up script ran before the fix was asked for: %q", lines)
			}
			// The sessions' own starts are in the history already; the fix adds to it.
			before = len(history())
			done = mustRun(t, m, fix...)
		},
		ThenTheCommandsThatStartTheAppAreShownBeforeTheyRun: func() {
			events := asList(done["events"])
			if len(events) == 0 || asMap(events[0])["event"] != "plan" || !strings.Contains(fmt.Sprint(asMap(events[0])["lines"]), "projects/project-a/up") {
				t.Fatalf("the run does not begin with its plan naming the up script: %v", events)
			}
		},
		AndTheStartIsListedInTheHistoryWithEachOfItsCommandsAndTheirOutcomes: func() {
			entries := history()
			if len(entries) != before+1 {
				t.Fatalf("asking for the fix added %d actions to the history, want the one start: %v", len(entries)-before, entries)
			}
			e := entries[0]
			cmds := asList(e["commands"])
			if !strings.Contains(fmt.Sprint(e["title"]), "Start the stack of project-a") || len(cmds) == 0 {
				t.Fatalf("the start is not in the history with its commands: %v", e)
			}
			for _, c := range cmds {
				if cm := asMap(c); cm["status"] == nil || cm["status"] == "" {
					t.Fatalf("a command of the start has no outcome: %v", cm)
				}
			}
		},
		AndNothingElseIsChanged: func() {
			lines := readLog(t, log)
			if f := strings.Fields(strings.Join(lines, " ")); len(lines) != 1 || len(f) < 3 || f[2] != "project-a" {
				t.Fatalf("the up scripts ran as %q, want project-a's once", lines)
			}
			now, _ := filepath.Glob(filepath.Join(m.Home, ".claude", "sessions", "*.json"))
			if strings.Join(now, " ") != strings.Join(agents, " ") {
				t.Fatalf("the agents changed: %q, were %q", now, agents)
			}
		},
	}, term0(&term))
}

// HEALTH-FIX-002: a fix only the developer can run (an administrator's setting) is shown as a
// command to type, with no action for the program to run, and the check takes no request to run
// one. Fails if such a fix carries an action, or the check accepts a way to apply it.
func TestHealthFix002(t *testing.T) {
	m, _ := healthEngine(t, "project-a")
	var term *termtest.Terminal
	var power health.Item
	scenarios.Run(t, scenarios.HealthFix002{
		GivenTheHealthCheckReportsStayingAwakeOnPowerAsMissing: func() {
			standInPower(t, m, "10")
			if power = itemOf(t, healthJSON(t, m), "stays awake on power"); power.Present {
				t.Fatalf("staying awake on power is present: %+v", power)
			}
		},
		WhenTheDeveloperLooksAtItsFix: func() {
			term = m.Start(t, "health")
			term.WaitFor("sudo pmset -c sleep 0")
			term.WaitExit()
		},
		ThenTheFixIsShownAsACommandToRunThemselves: func() {
			if !strings.Contains(power.Fix, "sudo pmset -c sleep 0") {
				t.Fatalf("the fix is not a command to run: %q", power.Fix)
			}
		},
		AndThereIsNoWayToRunItFromTheCheck: func() {
			if len(power.Action) != 0 {
				t.Fatalf("the fix carries an action the program would run: %q", power.Action)
			}
			for _, args := range [][]string{{"fix"}, {"--fix"}, {"fix", "stays awake on power"}} {
				if out, code := plainHealth(t, m, args...); code != 2 {
					t.Fatalf("engineroom health %q was taken (exit %d):\n%s", args, code, out)
				}
			}
			if p := itemOf(t, healthJSON(t, m), "stays awake on power"); p.Present {
				t.Fatal("staying awake on power changed")
			}
		},
	}, term0(&term))
}

// HEALTH-READ-001: an engine with every item present (the throwaway engine, asked from a client)
// ends with success; one item taken away ends with a failure; as JSON each item has its name,
// state, how it was checked, and a fix exactly when it is missing. Fails if the exit status does
// not follow what is missing, or a field is absent.
func TestHealthRead001(t *testing.T) {
	client := journey.NewMachine(t)
	var engine *journey.Engine
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.HealthRead001{
		GivenEveryItemIsPresent:            func() { engine = readyEngine(t, client) },
		WhenTheDeveloperRunsTheHealthCheck: func() { run = runHealth(t, client, &term) },
		ThenItEndsWithSuccess: func() {
			if run.exit != 0 || !strings.Contains(run.out, "Ready: every item is present") {
				t.Fatalf("every item present: exit %d\n%s", run.exit, run.out)
			}
		},
		AndWhenOneItemIsMissingItEndsWithAFailure: func() {
			engine.MustExec(`printf '#!/bin/sh\nprintf "AC Power:\\n sleep                10\\n"\n' > bin/pmset`)
			again := runHealth(t, client, &term)
			if again.exit != 1 || !strings.Contains(again.out, "Not ready: 1 of ") || !strings.HasPrefix(itemLines(t, again.out, "stays awake on power"), "missing") {
				t.Fatalf("one item missing: exit %d\n%s", again.exit, again.out)
			}
		},
		AndAsJSONEachItemHasItsNameWhetherItIsPresentHowItWasCheckedAndItsFix: func() {
			out, _ := plainHealth(t, client, "--json")
			var raw struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw.Items) == 0 {
				t.Fatalf("not a report: %v\n%s", err, out)
			}
			for _, it := range raw.Items {
				name, _ := it["name"].(string)
				present, isBool := it["present"].(bool)
				checked, _ := it["checked"].(string)
				fix, _ := it["fix"].(string)
				if name == "" || !isBool || checked == "" || present == (fix != "") {
					t.Fatalf("an item lacks a field, or a fix that matches its state: %v", it)
				}
			}
		},
	}, term0(&term))
}

// HEALTH-PAGE-001: the Health page reads `engineroom health --json`, as the app's client does; each
// read runs the check (the status script runs each time), with every missing item carrying its
// fix, and none of the reads the app makes on its refresh runs it. That the app makes the read only
// on opening the page and on Check again, never on a timer, is the app's own and is proved by its
// Kit tests (Store.checkHealth). Fails if the check is cached, part of a refresh, or an item lacks
// its fix.
func TestHealthPage001(t *testing.T) {
	m, paths := healthEngine(t, "project-a")
	log := filepath.Join(m.Home, "status.log")
	var term *termtest.Terminal
	var r health.Report
	scenarios.Run(t, scenarios.HealthPage001{
		GivenTheAppIsRunning: func() {
			projectScript(t, m, "project-a", "status", `echo checked >> "`+log+`"`+"\necho web answering\n")
			standInPower(t, m, "10")
			startAgent(t, m, "project-a", paths["project-a"], "zellij")
		},
		WhenTheDeveloperOpensTheHealthPage: func() { r = healthJSON(t, m) },
		ThenTheEngineIsCheckedAndEveryItemIsShownWithItsFix: func() {
			if lines := readLog(t, log); len(lines) != 1 {
				t.Fatalf("opening the page ran the status script %d times", len(lines))
			}
			if r.Missing == 0 {
				t.Fatal("nothing is missing, so no fix is shown")
			}
			for _, it := range r.Items {
				if !it.Present && it.Fix == "" {
					t.Fatalf("a missing item has no fix: %+v", it)
				}
			}
		},
		AndItIsCheckedAgainOnlyWhenTheDeveloperAsks: func() {
			// The reads of the app's refresh (EngineRoomKit's status, sessions, repos and history).
			for _, read := range [][]string{{"api", "status"}, {"api", "sessions"}, {"api", "repos"}, {"history", "--json"}} {
				if out, err := m.Run("engineroom", read...); err != nil {
					t.Fatalf("engineroom %q: %v\n%s", read, err, out)
				}
			}
			if lines := readLog(t, log); len(lines) != 1 {
				t.Fatalf("a refresh ran the check: the status script ran %d times", len(lines))
			}
			healthJSON(t, m)
			if lines := readLog(t, log); len(lines) != 2 {
				t.Fatalf("checking again did not run the check: the status script ran %d times", len(lines))
			}
		},
	}, term0(&term))
}
