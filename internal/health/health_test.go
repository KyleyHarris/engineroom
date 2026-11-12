package health

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/sshtest"
)

// tools puts stand-ins for macOS's power tools first on the PATH. pmset and ioreg describe this
// machine's own hardware and settings, which a test can neither choose nor change, so each test
// gives the description it is about; the check only reads what they print.
func tools(t *testing.T, scripts map[string]string) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range scripts {
		p := filepath.Join(bin, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin:/usr/sbin:/sbin")
}

// pmsetCustom is `pmset -g custom` as a laptop prints it, with sleep set for each power source.
func pmsetCustom(battery, ac string) string {
	return "cat <<'EOF'\nBattery Power:\n Sleep On Power Button 1\n sleep                " + battery +
		"\n displaysleep         10\nAC Power:\n Sleep On Power Button 1\n sleep                " + ac + "\n displaysleep         30\nEOF\n"
}

// HEALTH-AWAKE-001 at the package: the AC Power section decides, never the battery's. Fails if an
// engine set to sleep on power passes, if one that never sleeps on power is missing, or if the
// battery's sleep 0 is read as the power's.
func TestStayingAwakeOnPowerIsReadFromTheACSection(t *testing.T) {
	cases := []struct {
		name, battery, ac string
		present           bool
		found             string
	}{
		{"never sleeps on power", "1", "0", true, "never sleeps"},
		{"sleeps after 10 minutes on power", "1", "10", false, "sleeps after 10 minutes"},
		{"never sleeps on battery only", "0", "10", false, "sleeps after 10 minutes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tools(t, map[string]string{"pmset": pmsetCustom(c.battery, c.ac)})
			it := onPowerItem(context.Background())
			if it.Present != c.present || !strings.Contains(it.Found, c.found) {
				t.Fatalf("%+v", it)
			}
			if !c.present && !strings.Contains(it.Fix, "pmset -c sleep 0") {
				t.Fatalf("no fix: %+v", it)
			}
			if it.Checked == "" {
				t.Fatal("does not say how it was checked")
			}
		})
	}
	// A desktop Mac's pmset lists one section with no heading for the power source.
	tools(t, map[string]string{"pmset": "echo ' sleep                0'\n"})
	if it := onPowerItem(context.Background()); !it.Present {
		t.Fatalf("a Mac with no power sections: %+v", it)
	}
	// pmset failing is missing, never present.
	tools(t, map[string]string{"pmset": "echo 'pmset: no power' >&2; exit 1\n"})
	if it := onPowerItem(context.Background()); it.Present || !strings.Contains(it.Found, "pmset: no power") {
		t.Fatalf("pmset failing: %+v", it)
	}
}

// HEALTH-AWAKE-002 at the package: a laptop whose lid would sleep it now is missing, with the fix
// said as unconfirmed; one that would not, and a Mac with no lid, are present. Fails if a lid that
// sleeps the machine passes, or if a Mac with no lid is reported missing.
func TestStayingAwakeWithTheLidClosed(t *testing.T) {
	ioreg := func(causes string) string {
		return "cat <<'EOF'\n+-o AppleACPIPlatformExpert  <class IOPlatformExpertDevice>\n  {\n    \"AppleClamshellCausesSleep\" = " + causes + "\n    \"AppleClamshellState\" = No\n  }\nEOF\n"
	}
	tools(t, map[string]string{"ioreg": ioreg("Yes")})
	if it := lidItem(context.Background()); it.Present || !strings.Contains(it.Found, "would sleep") || !strings.HasPrefix(it.Fix, "unconfirmed") {
		t.Fatalf("a lid that sleeps it: %+v", it)
	}
	tools(t, map[string]string{"ioreg": ioreg("No")})
	if it := lidItem(context.Background()); !it.Present {
		t.Fatalf("a lid that does not sleep it: %+v", it)
	}
	tools(t, map[string]string{"ioreg": "exit 0\n"})
	if it := lidItem(context.Background()); !it.Present || !strings.Contains(it.Found, "no lid") {
		t.Fatalf("no lid: %+v", it)
	}
}

// remoteChoice reads engineroom's choice first, then Claude Code's remoteControlAtStartup. Fails if
// a machine where nothing chose Remote Control is counted as having chosen it, or one of the three
// choices is missed.
func TestRemoteControlIsChosenByEngineroomOrClaudeCode(t *testing.T) {
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	if c := remoteChoice(&config.Config{}); c.on {
		t.Fatalf("nothing chose it: %+v", c)
	}
	if c := remoteChoice(&config.Config{Agent: config.Agent{RemoteControl: true}}); !c.on || !strings.Contains(c.where, "agent.remote_control") {
		t.Fatalf("engineroom's agent choice: %+v", c)
	}
	if c := remoteChoice(&config.Config{App: config.App{Start: "both"}}); !c.on {
		t.Fatalf("the app's start: %+v", c)
	}
	if c := remoteChoice(&config.Config{App: config.App{Start: "zellij"}}); c.on {
		t.Fatalf("the app starting without it: %+v", c)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"remoteControlAtStartup": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := remoteChoice(&config.Config{}); !c.on || !strings.Contains(c.where, "remoteControlAtStartup") {
		t.Fatalf("Claude Code's choice: %+v", c)
	}
}

// HEALTH-REMOTE-001 at the package: an agent is found by the session its environment names and the
// file Claude Code keeps for its pid, and is on while that file carries bridgeSessionId. Real
// processes stand in for the agents, each with the session in its environment; Claude Code's files
// are written as it writes them. Fails if an agent with no bridgeSessionId is read as on, if a file
// left by another pid is believed, or if an agent is matched to the wrong session.
func TestAnAgentsRemoteControlIsReadFromClaudeCodesFile(t *testing.T) {
	claude := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	if err := os.MkdirAll(filepath.Join(claude, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentIn := func(session, bridge string, pidInFile func(int) int) int {
		// This test's own program, waiting: macOS does not show another program the environment of
		// its own system tools (sleep, sh), and the agent's environment is what is read.
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWaits$")
		cmd.Env = append(os.Environ(), "ZELLIJ_SESSION_NAME="+session, "HEALTH_HELPER_WAIT=1")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		pid := cmd.Process.Pid
		body := fmt.Sprintf(`{"pid": %d, "kind": "interactive"`, pidInFile(pid))
		if bridge != "" {
			body += `, "bridgeSessionId": "` + bridge + `"`
		}
		if err := os.WriteFile(filepath.Join(claude, "sessions", fmt.Sprintf("%d.json", pid)), []byte(body+"}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return pid
	}
	same := func(pid int) int { return pid }
	on := agentIn("project-a", "session_one", same)
	off := agentIn("project-b", "", same)
	agentIn("project-c", "session_stale", func(pid int) int { return pid + 100000 })

	got := agentsBySession()
	if a := got["project-a"]; len(a) != 1 || a[0].pid != on || !a[0].remote {
		t.Fatalf("project-a: %+v", a)
	}
	if a := got["project-b"]; len(a) != 1 || a[0].pid != off || a[0].remote {
		t.Fatalf("project-b: %+v", a)
	}
	if a := got["project-c"]; len(a) != 0 {
		t.Fatalf("a file naming another pid was believed: %+v", a)
	}
}

// TestHelperWaits is not a test: it is the process that stands in for an agent, run by
// TestAnAgentsRemoteControlIsReadFromClaudeCodesFile, and returns at once otherwise.
func TestHelperWaits(t *testing.T) {
	if os.Getenv("HEALTH_HELPER_WAIT") == "" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// HEALTH-REMOTE-001 and 002 at the package: on is on however it was turned on; off, or not read, is
// missing only where Remote Control is chosen, with the fix typed inside the session, never a
// restart. Fails if an off session passes while chosen, fails while not chosen, or is offered a
// restart.
func TestRemoteControlIsMissingOnlyWhereChosen(t *testing.T) {
	chosen, not := choice{true, "chosen here"}, choice{}
	if it := remoteItem("project-a", []agent{{pid: 10, remote: true}}, chosen); !it.Present || !strings.Contains(it.Found, "on") {
		t.Fatalf("on: %+v", it)
	}
	off := remoteItem("project-a", []agent{{pid: 10}}, chosen)
	if off.Present || !strings.Contains(off.Fix, "/remote-control") || strings.Contains(off.Fix, "restart") || len(off.Action) != 0 {
		t.Fatalf("off while chosen: %+v", off)
	}
	if it := remoteItem("project-a", []agent{{pid: 10}}, not); !it.Present || !strings.Contains(it.Found, "off") || it.Fix != "" {
		t.Fatalf("off while not chosen: %+v", it)
	}
	if it := remoteItem("project-a", nil, chosen); it.Present || !strings.Contains(it.Found, "not read") {
		t.Fatalf("no agent found while chosen: %+v", it)
	}
}

// status writes a status script that runs body, and returns the app check for it as the project's.
func status(t *testing.T, body string, hasUp bool) ops.AppCheck {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "status")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return ops.AppCheck{Project: "project-a", Status: action.Command{Argv: []string{p}, Dir: dir}, HasStatus: true, HasUp: hasUp}
}

// HEALTH-APP-001 to 005 at the package: the status script decides, and what it printed is the
// reason; a project that starts an app and cannot say whether it runs is not counted running; a
// project with no app is not missing; a script that does not end is stopped and reported while the
// rest still comes back. Fails if a failing script passes, if no status script reads as running,
// if a hanging script holds the report, or if the fix for a stopped app is not the up action.
func TestTheAppIsWhatItsStatusScriptSays(t *testing.T) {
	ctx := context.Background()
	if it := appItem(ctx, "project-a", status(t, "echo 'web: running (pid 1, on 5173).'\n", true)); !it.Present || !strings.Contains(it.Found, "web: running") {
		t.Fatalf("running: %+v", it)
	}
	stopped := appItem(ctx, "project-a", status(t, "echo 'web: stopped (pid 1 is gone).'\nexit 1\n", true))
	if stopped.Present || !strings.Contains(stopped.Found, "web: stopped") || stopped.Fix != "start the app: engineroom up project-a" ||
		strings.Join(stopped.Action, " ") != "up project-a" {
		t.Fatalf("stopped: %+v", stopped)
	}
	if it := appItem(ctx, "project-a", ops.AppCheck{Project: "project-a", HasUp: true}); it.Present || !strings.Contains(it.Found, "no status script") ||
		!strings.Contains(it.Fix, "settings where project-a") || len(it.Action) != 0 {
		t.Fatalf("no status script: %+v", it)
	}
	if it := appItem(ctx, "project-a", ops.AppCheck{Project: "project-a"}); !it.Present || !strings.Contains(it.Found, "no app") {
		t.Fatalf("no app: %+v", it)
	}

	StatusTimeout = time.Second
	t.Cleanup(func() { StatusTimeout = 10 * time.Second })
	marker := filepath.Join(t.TempDir(), "child")
	start := time.Now()
	hung := appItem(ctx, "project-a", status(t, "echo checking\n(sleep 2; touch "+marker+") &\nsleep 30\n", true))
	if time.Since(start) > 5*time.Second {
		t.Fatalf("a hanging script held the check for %s", time.Since(start))
	}
	if hung.Present || !strings.Contains(hung.Found, "did not end within") || len(hung.Action) != 0 {
		t.Fatalf("hanging: %+v", hung)
	}
	// What the script started is stopped with it: nothing it left behind goes on to act.
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a process the script started survived it")
	}
}

// HEALTH-CHECK-003 at the package: from a client, an engine that is not reached is said with its
// cause and no item is present. Against a real sshd restarted with another host key. Fails if the
// report carries items, counts as ready, or does not say the host key changed.
func TestAnEngineNotReachedGivesNoItems(t *testing.T) {
	s := sshtest.Start(t)
	s.ChangeHostKey()
	cfg := &config.Config{Role: config.RoleClient, Engine: config.Engine{Host: sshtest.Host, SSHConfig: s.SSHConfig, ConnectTimeout: config.Duration{Duration: 5 * time.Second}}}
	r := OfEngine(context.Background(), cfg)
	if r.Ready() || len(r.Items) != 0 || !strings.Contains(r.Unreached, "host key has changed") {
		t.Fatalf("%+v", r)
	}
}
