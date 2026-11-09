//go:build journeys

package journeys

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// What runs for a session lives as long as the session (docs/decisions.md D-020). The stand-in
// server is `listen <port>` (internal/journey/listen); each checkout's up starts one on the port
// in its .port file, detached as a real server is, and down stops it by the pid up recorded.

const lifeUp = `#!/bin/sh
port=$(cat .port)
nohup listen "$port" >/dev/null 2>&1 &
echo $! > .up.pid
`

const lifeDown = `#!/bin/sh
echo "down $(pwd)" >> "$HOME/down.log"
[ -f .up.pid ] && kill "$(cat .up.pid)" 2>/dev/null
rm -f .up.pid
`

// lifeProject is an engine with project-a, whose up and down are on the remote's main, and whose
// main clone's stack runs on port.
func lifeProject(t *testing.T, port int) (*journey.Machine, string) {
	t.Helper()
	m, clone, _ := workProject(t)
	commitScript(t, m, clone, "up", strings.TrimPrefix(lifeUp, "#!/bin/sh\n"))
	commitScript(t, m, clone, "down", strings.TrimPrefix(lifeDown, "#!/bin/sh\n"))
	m.WriteFile(filepath.Join(clone, ".port"), fmt.Sprint(port), 0o644)
	return m, clone
}

// endStandIns stops, when the test ends, any stand-in server still listening on these ports:
// they are started detached, as real servers are, so nothing else would end them. Found by the
// test's own ports, and only a process running the stand-in is stopped.
func endStandIns(t *testing.T, ports ...int) {
	t.Cleanup(func() {
		for _, port := range ports {
			out, _ := exec.Command("lsof", "-nP", "-t", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN").Output()
			for _, pid := range strings.Fields(string(out)) {
				if cmd, _ := exec.Command("ps", "-o", "command=", "-p", pid).Output(); strings.HasPrefix(strings.TrimSpace(string(cmd)), "listen ") || strings.Contains(string(cmd), "/listen ") {
					_ = exec.Command("kill", pid).Run()
				}
			}
		}
	})
}

func listeningOn(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func waitPort(t *testing.T, port int, want bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for listeningOn(port) != want {
		if time.Now().After(deadline) {
			t.Fatalf("port %d listening=%v, wanted %v", port, !want, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// inSession starts command inside a running session, in a pane of its own, as a developer or
// Claude starting something from inside the session does. The pane's processes carry the
// session's name (ZELLIJ_SESSION_NAME), as Zellij gives them. The command runs in a process
// session of its own (setsid, through perl, which macOS has), as a server started from a
// session's shell or by Claude outlives the pane it came from; a plain background job would end
// with the pane.
func inSession(t *testing.T, m *journey.Machine, session, command string) {
	t.Helper()
	log := filepath.Join(m.Home, "in-session.log")
	detached := "perl -MPOSIX -e 'POSIX::setsid(); exec @ARGV' sh -c '" + command + "' 2>>" + log + " &"
	if out, err := m.Run("zellij", "--session", session, "run", "--", "sh", "-c", detached+" echo \"ran in $ZELLIJ_SESSION_NAME\" >>"+log+"; sleep 1"); err != nil {
		t.Fatalf("running in %s: %v\n%s", session, err, out)
	}
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := os.ReadFile(log)
			t.Logf("in-session log:\n%s", b)
		}
	})
}

// outsideAnySession starts a server from no session at all, as one started by hand in a shell.
func outsideAnySession(t *testing.T, m *journey.Machine, port int) {
	t.Helper()
	cmd := exec.Command(filepath.Join(m.Bin, "listen"), fmt.Sprint(port))
	cmd.Env = m.Env()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	waitPort(t, port, true)
}

func planLines(done map[string]any) string {
	var all string
	for _, e := range asList(done["events"]) {
		if ev := asMap(e); ev["event"] == "plan" {
			all = fmt.Sprint(ev["lines"])
		}
	}
	return all
}

// WORK-STACK-006: stopping a session runs down while the session is there, ends it, then stops
// what was started inside it, and leaves alone a server that belongs to no session. Fails if
// down runs after the session ended or not at all, if the in-session server outlives the
// session, or if the stop reaches a server it has no claim on.
func TestWorkStack006(t *testing.T) {
	m, _ := lifeProject(t, 21601)
	endStandIns(t, 21601, 21602, 21603)
	var done map[string]any

	scenarios.Run(t, scenarios.WorkStack006{
		GivenACheckoutSSessionIsRunningWithItsStackStarted: func() {
			mustRun(t, m, "up", "project-a", "--mode", "zellij")
			m.WaitSession("project-a", "running")
			waitPort(t, 21601, true)
		},
		AndAServerWasStartedFromInsideThatSession: func() {
			inSession(t, m, "project-a", "exec listen 21602")
			waitPort(t, 21602, true)
		},
		AndAnotherServerOnTheProjectSPortsWasStartedOutsideAnySession: func() {
			outsideAnySession(t, m, 21603)
		},
		WhenTheDeveloperStopsThatSessionAndConfirms: func() {
			done = mustRun(t, m, "stop", "project-a")
		},
		ThenTheDownScriptRunsBeforeTheSessionEnds: func() {
			all := planLines(done)
			down, end, reap := strings.Index(all, ".engineroom/down"), strings.Index(all, "engineroom session end project-a"), strings.Index(all, "engineroom session reap project-a")
			if down < 0 || end < 0 || reap < 0 || !(down < end && end < reap) {
				t.Fatalf("not down, then end, then reap:\n%s", all)
			}
			waitPort(t, 21601, false)
			if journey.SessionState(m.Sessions(), "project-a") != "" {
				t.Fatalf("the session is still there:\n%s", m.Sessions())
			}
		},
		AndTheServerStartedInsideTheSessionIsStopped: func() {
			waitPort(t, 21602, false)
		},
		AndTheServerStartedOutsideAnySessionIsStillRunning: func() {
			if !listeningOn(21603) {
				t.Fatalf("the server started outside any session was stopped")
			}
		},
	}, nil)
}

// WORK-STACK-007: closing the window a session is joined in ends nothing. Fails if the session
// or its stack goes with the window.
func TestWorkStack007(t *testing.T) {
	m, _ := lifeProject(t, 21621)
	endStandIns(t, 21621)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkStack007{
		GivenASessionIsRunningWithItsStackStartedAndTheDeveloperHasJoinedIt: func() {
			mustRun(t, m, "up", "project-a", "--mode", "zellij")
			waitPort(t, 21621, true)
			term = m.Start(t, "open", "project-a")
			term.WaitFor("Zellij (project-a)")
		},
		WhenTheDeveloperClosesTheTerminalWindow: func() {
			term.Close()
		},
		ThenTheSessionIsStillRunning: func() {
			time.Sleep(time.Second)
			if journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("the session did not survive the window:\n%s", m.Sessions())
			}
		},
		AndSoIsItsStack: func() {
			if !listeningOn(21621) {
				t.Fatalf("the stack did not survive the window")
			}
		},
	}, nil)
}

// WORK-STACK-010: restart brings the same session back and leaves its stack running. Fails if
// restart runs down or stops what was started for the session.
func TestWorkStack010(t *testing.T) {
	m, _ := lifeProject(t, 21631)
	endStandIns(t, 21631)

	scenarios.Run(t, scenarios.WorkStack010{
		GivenASessionIsRunningWithItsStackStarted: func() {
			mustRun(t, m, "up", "project-a", "--mode", "zellij")
			waitPort(t, 21631, true)
		},
		WhenTheDeveloperRestartsItAndConfirms: func() {
			mustRun(t, m, "restart", "project-a", "--mode", "zellij")
			m.WaitSession("project-a", "running")
		},
		ThenItsStackIsStillRunning: func() {
			time.Sleep(time.Second)
			if !listeningOn(21631) {
				t.Fatalf("the restart stopped the stack")
			}
			if b, _ := os.ReadFile(filepath.Join(m.Home, "down.log")); len(b) > 0 {
				t.Fatalf("restart ran down: %s", b)
			}
		},
	}, nil)
}

// WORK-STACK-012: a stack started for one checkout from inside another checkout's session
// belongs to its own checkout's session, so stopping the session it was started from leaves it.
// Fails if the stop reaches the other checkout's stack.
func TestWorkStack012(t *testing.T) {
	m, clone := lifeProject(t, 21641)
	endStandIns(t, 21641, 21642)
	var w1 string

	scenarios.Run(t, scenarios.WorkStack012{
		GivenTwoCheckoutsEachHaveARunningSession: func() {
			mustRun(t, m, "start", "project-a", "--mode", "zellij")
			m.WaitSession("project-a", "running")
			w1 = startWork(t, m, clone, "w1")
			m.WriteFile(filepath.Join(w1, ".port"), "21642", 0o644)
		},
		AndFromInsideTheFirstTheDeveloperStartedTheStackOfTheSecond: func() {
			inSession(t, m, "project-a", "engineroom api run up project-a-w1 --mode zellij >/dev/null")
			waitPort(t, 21642, true)
		},
		WhenTheDeveloperStopsTheFirstSessionAndConfirms: func() {
			mustRun(t, m, "stop", "project-a")
		},
		ThenTheSecondCheckoutSStackIsStillRunning: func() {
			time.Sleep(time.Second)
			if !listeningOn(21642) {
				t.Fatalf("stopping project-a stopped the stack of project-a-w1")
			}
		},
	}, nil)
}

// seedGone records that the session was first seen gone, with something still running for it,
// ago before now: what the sweep's earlier runs would have recorded as time passed.
func seedGone(t *testing.T, m *journey.Machine, session string, ago time.Duration) {
	t.Helper()
	m.WriteFile(filepath.Join(m.Home, ".local", "state", "engineroom", "sweep.json"),
		fmt.Sprintf(`{%q:%d}`, session, time.Now().Add(-ago).Unix()), 0o644)
}

// WORK-STACK-008: a session ended from inside Zellij, gone for over two minutes, has its stack
// stopped by the sweep, as an action in the history. Fails if the sweep leaves the stack, runs
// nothing, or acts without a history entry.
func TestWorkStack008(t *testing.T) {
	m, _ := lifeProject(t, 21651)
	endStandIns(t, 21651)

	scenarios.Run(t, scenarios.WorkStack008{
		GivenASessionSStackWasStarted: func() {
			mustRun(t, m, "up", "project-a", "--mode", "zellij")
			waitPort(t, 21651, true)
		},
		AndTheSessionWasEndedFromInsideZellijMoreThanTwoMinutesAgo: func() {
			if out, err := m.Run("zellij", "kill-session", "project-a"); err != nil {
				t.Fatalf("ending the session: %v\n%s", err, out)
			}
			seedGone(t, m, "project-a", 3*time.Minute)
		},
		WhenTheEngineChecksItsSessions: func() {
			if out, err := m.Run("engineroom", "sweep"); err != nil {
				t.Fatalf("engineroom sweep: %v\n%s", err, out)
			}
		},
		ThenTheDownScriptHasRunInThatCheckout: func() {
			if b, _ := os.ReadFile(filepath.Join(m.Home, "down.log")); !strings.Contains(string(b), "down ") {
				t.Fatalf("down did not run: %q", b)
			}
			waitPort(t, 21651, false)
		},
		AndTheHistorySaysTheStackWasStoppedBecauseItsSessionHadEnded: func() {
			out, _ := m.Run("engineroom", "history")
			if !strings.Contains(out, "Stop what ran for project-a: its session has ended") {
				t.Fatalf("no history entry:\n%s", out)
			}
		},
	}, nil)
}

// WORK-STACK-009: a session that ended a moment ago keeps its stack: the sweep only notes it.
// Fails if the sweep stops anything inside the grace period.
func TestWorkStack009(t *testing.T) {
	m, _ := lifeProject(t, 21661)
	endStandIns(t, 21661)

	scenarios.Run(t, scenarios.WorkStack009{
		GivenASessionSStackWasStarted: func() {
			mustRun(t, m, "up", "project-a", "--mode", "zellij")
			waitPort(t, 21661, true)
		},
		AndTheSessionWasEndedFromInsideZellijLessThanTwoMinutesAgo: func() {
			if out, err := m.Run("zellij", "kill-session", "project-a"); err != nil {
				t.Fatalf("ending the session: %v\n%s", err, out)
			}
			seedGone(t, m, "project-a", 30*time.Second)
		},
		WhenTheEngineChecksItsSessions: func() {
			if out, err := m.Run("engineroom", "sweep"); err != nil {
				t.Fatalf("engineroom sweep: %v\n%s", err, out)
			}
		},
		ThenNothingIsStopped: func() {
			time.Sleep(500 * time.Millisecond)
			if !listeningOn(21661) {
				t.Fatalf("the sweep stopped the stack within the grace period")
			}
			if b, _ := os.ReadFile(filepath.Join(m.Home, "down.log")); len(b) > 0 {
				t.Fatalf("down ran within the grace period: %s", b)
			}
		},
	}, nil)
}

// WORK-STACK-013: an app opened from inside a session, which serves on no port, is the
// developer's to close: stopping the session leaves it. Fails if Stop closes it.
func TestWorkStack013(t *testing.T) {
	m, _ := lifeProject(t, 21671)
	endStandIns(t, 21671)
	marker := filepath.Join(m.Home, "app.pid")

	scenarios.Run(t, scenarios.WorkStack013{
		GivenACheckoutSSessionIsRunning: func() {
			mustRun(t, m, "start", "project-a", "--mode", "zellij")
			m.WaitSession("project-a", "running")
		},
		AndAnAppThatServesNothingWasOpenedFromInsideThatSession: func() {
			inSession(t, m, "project-a", "listen idle & echo $! > "+marker+"; wait")
			deadline := time.Now().Add(10 * time.Second)
			for {
				if b, err := os.ReadFile(marker); err == nil && len(strings.TrimSpace(string(b))) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("the app did not start")
				}
				time.Sleep(100 * time.Millisecond)
			}
		},
		WhenTheDeveloperStopsThatSessionAndConfirms: func() {
			mustRun(t, m, "stop", "project-a")
		},
		ThenTheAppIsStillRunning: func() {
			b, _ := os.ReadFile(marker)
			pid := strings.TrimSpace(string(b))
			t.Cleanup(func() { _ = exec.Command("kill", pid).Run() })
			time.Sleep(time.Second)
			if err := exec.Command("kill", "-0", pid).Run(); err != nil {
				t.Fatalf("Stop closed the app (pid %s) opened from inside the session", pid)
			}
		},
	}, nil)
}

// WORK-STACK-011: a server on the project's ports started outside any session is left running
// by the sweep, and the project's page lists it with its port, process, start and folder, with
// a Stop for that one process. Fails if the sweep stops it, the page leaves it out or names it
// wrongly, or its Stop is not for exactly that process.
func TestWorkStack011(t *testing.T) {
	m, clone := lifeProject(t, 21681)
	endStandIns(t, 21682)
	var page map[string]any
	var started int

	scenarios.Run(t, scenarios.WorkStack011{
		GivenAServerOnAProjectSPortsWasStartedOutsideAnySession: func() {
			settingsOf(t, m, "project-a", "reserve = [\"21680-21689\"]\n\n[[slot]]\nslot = 1\nports = { web = 21681 }\n")
			// Started from a shell that then exits, so it is nobody's child, as a server started by
			// hand in another terminal is (a child of this test would linger unreaped once stopped).
			cmd := exec.Command("sh", "-c", "listen 21682 >/dev/null 2>&1 & echo $!")
			cmd.Env, cmd.Dir = m.Env(), clone
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if started, err = strconv.Atoi(strings.TrimSpace(string(out))); err != nil {
				t.Fatalf("no pid: %q", out)
			}
			waitPort(t, 21682, true)
		},
		WhenTheEngineChecksItsSessions: func() {
			if out, err := m.Run("engineroom", "sweep"); err != nil {
				t.Fatalf("engineroom sweep: %v\n%s", err, out)
			}
		},
		ThenTheServerIsStillRunning: func() {
			if !listeningOn(21682) {
				t.Fatalf("the sweep stopped a server that belongs to no session")
			}
		},
		AndTheProjectSPageListsItWithItsPortItsProcessWhenItStartedAndItsFolderAndOffersToStopThatOne: func() {
			page = api(t, m, "project", "project-a")
			loose := asList(page["loose"])
			if len(loose) != 1 {
				t.Fatalf("loose servers %v", page["loose"])
			}
			s := asMap(loose[0])
			ports := asList(s["ports"])
			if len(ports) != 1 || ports[0] != float64(21682) || s["pid"] != float64(started) || !strings.Contains(fmt.Sprint(s["command"]), "listen") ||
				s["started"] == "" || resolve(strings.Replace(fmt.Sprint(s["folder"]), "~", m.Home, 1)) != resolve(clone) {
				t.Fatalf("not named rightly: %v (clone %s)", s, clone)
			}
			pid := fmt.Sprint(int(s["pid"].(float64)))
			plan := api(t, m, "plan", "stop-server", "project-a", pid, "21682")
			if !strings.Contains(fmt.Sprint(plan["lines"]), "engineroom process stop "+pid+" --port 21682") {
				t.Fatalf("the Stop is not for that process: %v", plan["lines"])
			}
			mustRun(t, m, "stop-server", "project-a", pid, "21682")
			waitPort(t, 21682, false)
		},
	}, nil)
}

// WORK-STACK-014: the sweep's watch sweeps at once when started, and ends when what started it
// ends, as the app's does: it is never a job of its own. Fails if the first sweep waits, or the
// watch outlives its starter.
func TestWorkStack014(t *testing.T) {
	m, _ := lifeProject(t, 21691)
	var watch int

	scenarios.Run(t, scenarios.WorkStack014{
		GivenASessionOfACheckoutHasEndedLeavingWhatZellijKeepsOfIt: func() {
			mustRun(t, m, "start", "project-a", "--mode", "zellij")
			m.WaitSerialized(t, "project-a", `name="agent"`)
			if out, err := m.Run("zellij", "kill-session", "project-a"); err != nil {
				t.Fatalf("ending the session: %v\n%s", err, out)
			}
			m.WaitSession("project-a", "ended")
		},
		WhenTheSweepsWatchIsStarted: func() {
			// The starter stands in for the app: a shell that starts the watch, says its pid, and
			// ends a few seconds later.
			cmd := m.Command("sh", "-c", "engineroom sweep --watch >/dev/null 2>&1 & echo $!; sleep 3")
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var pid int
			if _, err := fmt.Fscan(out, &pid); err != nil {
				t.Fatalf("no pid from the starter: %v", err)
			}
			watch = pid
			t.Cleanup(func() { _ = exec.Command("kill", strconv.Itoa(watch)).Run(); _ = cmd.Wait() })
		},
		ThenTheEndedSessionIsForgottenAtOnce: func() {
			m.WaitSession("project-a", "")
		},
		AndWhenWhatStartedTheWatchEndsTheWatchEndsToo: func() {
			deadline := time.Now().Add(10 * time.Second)
			for exec.Command("kill", "-0", strconv.Itoa(watch)).Run() == nil {
				if time.Now().After(deadline) {
					t.Fatalf("the watch (pid %d) outlived what started it", watch)
				}
				time.Sleep(200 * time.Millisecond)
			}
		},
	}, nil)
}
