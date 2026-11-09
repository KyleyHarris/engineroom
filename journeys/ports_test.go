//go:build journeys

package journeys

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/forward"
	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// settingsOf writes a project's engineroom.toml in this machine's own settings folder, found by asking
// the program where it looks, so the journey never guesses the folder's layout.
func settingsOf(t *testing.T, m *journey.Machine, project, content string) string {
	t.Helper()
	out, err := m.Run("engineroom", "settings", "where", project)
	if err != nil {
		t.Fatalf("engineroom settings where: %v\n%s", err, out)
	}
	path := filepath.Join(strings.TrimSpace(out), "engineroom.toml")
	m.WriteFile(path, content, 0o644)
	return path
}

// slotsFile is three slots of one port each, in the developer's settings.
const slotsFile = `[[slot]]
slot = 1
ports = { web = 21000 }

[[slot]]
slot = 2
ports = { web = 21010 }

[[slot]]
slot = 3
ports = { web = 21020 }
`

// record reads a worktree's slot record.
func record(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".engineroom-slot.json"))
	if err != nil {
		t.Fatalf("the worktree has no slot record: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("the slot record is not JSON: %v\n%s", err, b)
	}
	return rec
}

// newWork starts new work through the program, waits until its session is open, and leaves it.
func newWork(t *testing.T, m *journey.Machine, work string) {
	t.Helper()
	term := m.Start(t, "new", "project-a", work)
	term.WaitFor("AGENT new conversation")
	detach(term)
	term.WaitExit()
}

// PORT-SLOT-001: new work claims a slot of its own, and the worktree's record holds its ports.
func TestPortSlot001(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortSlot001{
		GivenAProjectWhoseSettingsGiveItThreeSlots: func() {
			settingsOf(t, m, "project-a", slotsFile)
		},
		WhenTheDeveloperStartsNewWorkOnIt: func() {
			newWork(t, m, "w1")
		},
		ThenTheNewCheckoutHoldsASlotThatIsNotTheMainClones: func() {
			out, _ := m.Run("engineroom", "slot", "status", "--worktree", clone+"-w1")
			if !strings.Contains(out, "Worktree slot 2 of project-a") {
				t.Fatalf("the new worktree does not hold slot 2:\n%s", out)
			}
		},
		AndItsRecordHoldsThatSlotsPorts: func() {
			if rec := record(t, clone+"-w1"); rec["web"] != float64(21010) || rec["slot"] != float64(2) {
				t.Fatalf("record %v", rec)
			}
		},
	}, term0(&term))
}

// PORT-SLOT-002: cleaning up gives the slot back, and the next new work gets it.
func TestPortSlot002(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortSlot002{
		GivenNewWorkHoldsASlot: func() {
			// The agent stand-in leaves a marker in the folder it runs in; the repository ignores
			// it, as a real one ignores its own tools' files, so clean up is not refused for it.
			commitFile(t, m, clone, ".gitignore", ".fake-agent-conversation\n")
			settingsOf(t, m, "project-a", slotsFile)
			newWork(t, m, "w1")
			if rec := record(t, clone+"-w1"); rec["slot"] != float64(2) {
				t.Fatalf("record %v", rec)
			}
		},
		WhenTheDeveloperCleansItUpAndConfirms: func() {
			term = m.Start(t, "clean", "project-a", "w1")
			term.WaitFor("Nothing has run yet.")
			term.WaitFor("engineroom slot release --worktree")
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenTheSlotIsFree: func() {
			b, _ := os.ReadFile(filepath.Join(clone, ".git", "engineroom-slots.json"))
			if strings.Contains(string(b), "-w1") {
				t.Fatalf("the cleaned-up worktree still holds its slot:\n%s", b)
			}
		},
		AndTheNextNewWorkIsGivenIt: func() {
			newWork(t, m, "w2")
			if rec := record(t, clone+"-w2"); rec["slot"] != float64(2) {
				t.Fatalf("the next new work got %v, want slot 2", rec["slot"])
			}
		},
	}, term0(&term))
}

// PORT-SLOT-003: a worktree's up script and its tasks are given the slot it holds, when the
// project's settings give its record a name of their own. Fails if either is given slot 1's
// port, or none, which is what a script that read only the default file name was given: it then
// reported success while starting nothing of the worktree's own.
func TestPortSlot003(t *testing.T) {
	m, clone, _ := workProject(t)
	log := filepath.Join(m.Home, "slot.log")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortSlot003{
		GivenAProjectWhoseSettingsNameTheirOwnSlotRecord: func() {
			commitFile(t, m, clone, ".gitignore", ".fake-agent-conversation\n.app-slot.*\n")
			commitScript(t, m, clone, "up", `echo "up $ENGINEROOM_PORT_WEB" >> "`+log+`"`+"\n")
			settingsOf(t, m, "project-a", "[slots]\nrecord = \".app-slot.json\"\n\n"+slotsFile+
				"\n[[task]]\nname = \"Show port\"\nrun = 'echo \"task $ENGINEROOM_PORT_WEB\" >> \""+log+"\"'\n")
		},
		AndNewWorkOnItHoldsSlot2: func() {
			newWork(t, m, "w1")
			if _, err := os.Stat(filepath.Join(clone+"-w1", ".app-slot.env")); err != nil {
				t.Fatalf("the slot's environment file is not under the record's name: %v", err)
			}
		},
		WhenTheDeveloperStartsTheStackOfThatNewWorkAndRunsOneOfItsTasksThere: func() {
			for _, args := range [][]string{{"up", "project-a-w1"}, {"task", "project-a-w1", "Show port"}} {
				if out, err := m.Run("engineroom", args...); err != nil {
					t.Fatalf("engineroom %v: %v\n%s", args, err, out)
				}
			}
		},
		ThenTheUpScriptIsGivenSlot2sPorts: func() {
			if lines := readLog(t, log); len(lines) < 1 || lines[0] != "up 21010" {
				t.Fatalf("the up script was given %q, want slot 2's port 21010", lines)
			}
		},
		AndTheTaskIsGivenThemToo: func() {
			if lines := readLog(t, log); len(lines) != 2 || lines[1] != "task 21010" {
				t.Fatalf("the task was given %q, want slot 2's port 21010", lines)
			}
		},
	}, term0(&term))
}

// PORT-SET-001: the developer's settings win over the ports the repository recommends.
func TestPortSet001(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortSet001{
		GivenTheRepositoryRecommendsOneSetOfPortsAndTheDevelopersSettingsGiveAnother: func() {
			commitFile(t, m, clone, ".engineroom/engineroom.toml", "[[slot]]\nslot = 1\nports = { web = 29990 }\n\n[[slot]]\nslot = 2\nports = { web = 29991 }\n")
			settingsOf(t, m, "project-a", slotsFile)
		},
		WhenTheDeveloperStartsNewWorkOnTheProject: func() {
			newWork(t, m, "w1")
		},
		ThenItsRecordHoldsThePortsFromTheDevelopersSettings: func() {
			if rec := record(t, clone+"-w1"); rec["web"] != float64(21010) {
				t.Fatalf("the record holds web %v, want 21010 from the developer's settings", rec["web"])
			}
		},
	}, term0(&term))
}

// PORT-SET-002 (D-028): a port named for one of the program's own keys is refused when the
// settings are read, naming the settings file and the key, and nothing is created. Fails if the
// port is read and silently replaces the slot number in the record, if the refusal does not name
// the file, or if a worktree or branch is made before it.
func TestPortSet002(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal
	var file string

	scenarios.Run(t, scenarios.PortSet002{
		GivenTheDevelopersSettingsForAProjectNameAPortSlotTheProgramsOwnKey: func() {
			file = settingsOf(t, m, "project-a", "[ports]\nslot = 21000\n")
		},
		WhenTheDeveloperStartsNewWorkOnTheProject: func() {
			term = m.Start(t, "new", "project-a", "w1")
		},
		ThenTheyAreToldThePortSlotIsTheProgramsOwnKeyNamingTheirSettingsFile: func() {
			term.WaitFor(file + `: the port "slot" is the program's own key "slot"`)
		},
		AndNoCheckoutIsCreated: func() {
			if _, err := os.Stat(clone + "-w1"); !os.IsNotExist(err) {
				t.Fatalf("a worktree was created: %v", err)
			}
			if strings.Contains(m.Git(clone, "branch", "--list", "w1"), "w1") {
				t.Fatalf("a branch was created")
			}
		},
	}, term0(&term))
}

// commitFile commits one file to the project's main and publishes it, so new work has it.
func commitFile(t *testing.T, m *journey.Machine, clone, rel, content string) {
	t.Helper()
	m.WriteFile(filepath.Join(clone, rel), content, 0o644)
	m.Git(clone, "add", rel)
	m.Git(clone, "commit", "-qm", "add "+rel)
	m.Publish(clone, "main")
}

// twoProjects is an engine with project-a and project-b registered.
func twoProjects(t *testing.T) *journey.Machine {
	t.Helper()
	m := journey.NewMachine(t)
	m.BeEngine("project-a", m.NewRepo("project-a"), "project-b", m.NewRepo("project-b"))
	return m
}

// PORT-RES-001: a port two projects both use is reported with both named, and nothing is changed.
func TestPortRes001(t *testing.T) {
	m := twoProjects(t)
	var term *termtest.Terminal
	var a, b string
	var before [2][]byte
	var out string
	var err error

	scenarios.Run(t, scenarios.PortRes001{
		GivenTwoProjectsWhoseSettingsBothUseOnePort: func() {
			a = settingsOf(t, m, "project-a", "[ports]\nweb = 21500\n")
			b = settingsOf(t, m, "project-b", "[ports]\napi = 21500\n")
			before[0], _ = os.ReadFile(a)
			before[1], _ = os.ReadFile(b)
		},
		WhenTheDeveloperChecksThePorts: func() {
			out, err = m.Run("engineroom", "ports", "check")
		},
		ThenTheClashIsReportedNamingBothProjectsAndThePort: func() {
			if err == nil || !strings.Contains(out, "clash: project-a: port 21500 also belongs to project-b") {
				t.Fatalf("the clash was not reported (err %v):\n%s", err, out)
			}
		},
		AndNeitherProjectSSettingsAreChanged: func() {
			for i, p := range []string{a, b} {
				if now, _ := os.ReadFile(p); string(now) != string(before[i]) {
					t.Fatalf("%s was changed:\n%s", p, now)
				}
			}
		},
	}, term0(&term))
}

// PORT-RES-002: a reservation takes the next free block of 100, slot 1 keeps the project's own
// numbers, and slots 2 to 10 come from the block.
func TestPortRes002(t *testing.T) {
	m := twoProjects(t)
	var term *termtest.Terminal
	var path string
	var f engineroomfile.File

	scenarios.Run(t, scenarios.PortRes002{
		GivenAnotherProjectAlreadyReservesTheFirstBlock: func() {
			settingsOf(t, m, "project-b", "reserve = [\"20000-20099\"]\n")
		},
		WhenTheDeveloperReservesPortsForAProjectThatNamesItsPorts: func() {
			path = settingsOf(t, m, "project-a", "[ports]\nui = 4100\napi = 4200\n")
			if out, err := m.Run("engineroom", "ports", "reserve", "project-a", "--write"); err != nil {
				t.Fatalf("reserve: %v\n%s", err, out)
			}
			var err error
			if f, err = engineroomfile.ReadFile(path); err != nil {
				t.Fatal(err)
			}
		},
		ThenTheProjectReservesTheNextFreeBlockOf100: func() {
			if len(f.Reserve) == 0 || f.Reserve[0] != (engineroomfile.Range{Lo: 20100, Hi: 20199}) {
				t.Fatalf("reserve %v", f.Reserve)
			}
		},
		AndItsSlot1KeepsThePortsItHad: func() {
			if len(f.Rows) != 10 || f.Rows[0].Slot != 1 || f.Rows[0].Ports[0] != (engineroomfile.Port{Name: "ui", Number: 4100}) || f.Rows[0].Ports[1] != (engineroomfile.Port{Name: "api", Number: 4200}) {
				t.Fatalf("slot 1 %+v", f.Rows)
			}
		},
		AndSlots2To10TakePortsFromTheBlock: func() {
			for _, r := range f.Rows[1:] {
				for _, p := range r.Ports {
					if !f.Reserve[0].Contains(p.Number) {
						t.Fatalf("slot %d %s %d is outside the block", r.Slot, p.Name, p.Number)
					}
				}
			}
			if out, err := m.Run("engineroom", "ports", "check"); err != nil {
				t.Fatalf("the reservation clashes:\n%s", out)
			}
		},
	}, term0(&term))
}

// freePort is a port nothing on this machine listens on now.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// forwardEngine is an engine with project-a's session running, a server on the engine answering on port
// with a greeting, and a client of it that turns forwarding off when the test ends, so the
// connection the journey opened never outlives it.
func forwardEngine(t *testing.T, port int) (*journey.Machine, *journey.Engine) {
	t.Helper()
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	dir := strings.TrimSpace(engine.MustExec("engineroom settings where project-a"))
	engine.MustExec(fmt.Sprintf("mkdir -p %s && printf '[ports]\\nweb = %d\\n' > %s/engineroom.toml", dir, port, dir))
	serveOnEngine(engine, port)
	engine.StartSession("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	t.Cleanup(func() { _, _ = client.Run("engineroom", "forward", "off") })
	return client, engine
}

// serveOnEngine starts a server on the engine that answers on port with a greeting, and waits until it
// listens, so a forward made at once never meets a server not yet up.
func serveOnEngine(engine *journey.Engine, port int) {
	engine.MustExec(fmt.Sprintf("nohup socat TCP-LISTEN:%[1]d,bind=127.0.0.1,fork,reuseaddr SYSTEM:'echo hello-from-engine' >/dev/null 2>&1 & for i in $(seq 50); do socat -u OPEN:/dev/null TCP:127.0.0.1:%[1]d 2>/dev/null && break; sleep 0.1; done", port))
}

// greeting connects to port on this machine and reads one line, or says why it could not.
func greeting(port int) string {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		return "no connection: " + err.Error()
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "no answer: " + err.Error()
	}
	return strings.TrimSpace(line)
}

// PORT-FWD-001: with forwarding on, the client's own port reaches the engine session's server, and
// the forward is in the history.
func TestPortFwd001(t *testing.T) {
	port := freePort(t)
	var client *journey.Machine
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd001{
		GivenASessionIsRunningOnTheEngineWithAServerOnItsSlotsPort: func() {
			client, _ = forwardEngine(t, port)
		},
		WhenTheDeveloperTurnsForwardingOnOnAClient: func() {
			if out, err := client.Run("engineroom", "forward", "on"); err != nil {
				t.Fatalf("forward on: %v\n%s", err, out)
			}
		},
		ThenTheClientSOwnPortOfThatNumberReachesTheEnginesServer: func() {
			if got := greeting(port); got != "hello-from-engine" {
				t.Fatalf("port %d on the client answered %q", port, got)
			}
		},
		AndTheForwardIsKeptInTheHistory: func() {
			out, _ := client.Run("engineroom", "history", "--json")
			if !strings.Contains(out, fmt.Sprintf("%d:localhost:%d", port, port)) || !strings.Contains(out, "forward") {
				t.Fatalf("the forward is not in the history:\n%s", out)
			}
		},
	}, term0(&term))
}

// PORT-FWD-002: when the session stops, the next check cancels its forward.
func TestPortFwd002(t *testing.T) {
	port := freePort(t)
	var client *journey.Machine
	var engine *journey.Engine
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd002{
		GivenTheClientForwardsARunningSessionSPort: func() {
			client, engine = forwardEngine(t, port)
			if out, err := client.Run("engineroom", "forward", "on"); err != nil {
				t.Fatalf("forward on: %v\n%s", err, out)
			}
			if got := greeting(port); got != "hello-from-engine" {
				t.Fatalf("the forward was not made: %q", got)
			}
		},
		WhenTheSessionStopsAndForwardingChecksAgain: func() {
			engine.MustExec("zellij kill-session project-a")
			if out, err := client.Run("engineroom", "forward"); err != nil || !strings.Contains(out, "-O cancel") {
				t.Fatalf("the check did not cancel the forward: %v\n%s", err, out)
			}
		},
		ThenTheClientSPortNoLongerReachesTheEngine: func() {
			if got := greeting(port); got == "hello-from-engine" {
				t.Fatalf("port %d still reaches the engine", port)
			}
		},
	}, term0(&term))
}

// PORT-FWD-003: a port a program on the client already holds is named with what holds it, and the
// program keeps it.
func TestPortFwd003(t *testing.T) {
	port := freePort(t)
	var client *journey.Machine
	var held net.Listener
	var out string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd003{
		GivenAProgramOnTheClientAlreadyListensOnARunningSessionSPort: func() {
			client, _ = forwardEngine(t, port)
			var err error
			if held, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { held.Close() })
			go func() {
				for {
					c, err := held.Accept()
					if err != nil {
						return
					}
					fmt.Fprintln(c, "hello-from-client")
					c.Close()
				}
			}()
		},
		WhenTheDeveloperTurnsForwardingOn: func() {
			out, _ = client.Run("engineroom", "forward", "on")
		},
		ThenTheyAreToldThePortIsInUseAndWhatHoldsIt: func() {
			if !strings.Contains(out, fmt.Sprintf("web %d is not forwarded: the port is already in use on this machine by", port)) {
				t.Fatalf("the port in use was not named:\n%s", out)
			}
		},
		AndThatProgramKeepsThePort: func() {
			if got := greeting(port); got != "hello-from-client" {
				t.Fatalf("port %d answered %q, want the client's own program", port, got)
			}
		},
	}, term0(&term))
}

// PORT-FWD-004: switching to work local closes the forwards, and a check while working local makes
// none. Fails if the port still reaches the engine, or a check forwards it again.
func TestPortFwd004(t *testing.T) {
	port := freePort(t)
	var client *journey.Machine
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd004{
		GivenTheClientForwardsARunningSessionSPort: func() {
			client, _ = forwardEngine(t, port)
			if out, err := client.Run("engineroom", "forward", "on"); err != nil {
				t.Fatalf("forward on: %v\n%s", err, out)
			}
			if got := greeting(port); got != "hello-from-engine" {
				t.Fatalf("the forward was not made: %q", got)
			}
		},
		WhenTheDeveloperSwitchesTheClientToWorkLocal: func() {
			if out, err := client.Run("engineroom", "offline", "on"); err != nil {
				t.Fatalf("offline on: %v\n%s", err, out)
			}
		},
		ThenTheClientSPortNoLongerReachesTheEngine: func() {
			if got := greeting(port); got == "hello-from-engine" {
				t.Fatalf("port %d still reaches the engine while working local", port)
			}
		},
		AndForwardingChecksAgainDoNotForwardItWhileTheClientWorksLocal: func() {
			out, _ := client.Run("engineroom", "forward")
			if !strings.Contains(out, "Working local") || greeting(port) == "hello-from-engine" {
				t.Fatalf("a check while working local forwarded again:\n%s", out)
			}
		},
	}, term0(&term))
}

// fwdStatus is the client's forwarding status as the app reads it: whether the connection is open,
// and the last pass's record as written.
func fwdStatus(t *testing.T, client *journey.Machine) (connected bool, last string) {
	t.Helper()
	out, err := client.Run("engineroom", "forward", "status", "--json")
	if err != nil {
		t.Fatalf("forward status: %v\n%s", err, out)
	}
	var s struct {
		Connected bool            `json:"connected"`
		Last      json.RawMessage `json:"last"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("forward status: %v\n%s", err, out)
	}
	return s.Connected, string(s.Last)
}

// watchForwards starts the client's forwarder watch, looking every second when there is no
// connection, and stops it, by its own process, when the test ends.
func watchForwards(t *testing.T, client *journey.Machine) {
	t.Helper()
	cmd := client.Command("engineroom", "forward", "--watch", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
}

// PORT-FWD-005: the forwarder follows the engine's sessions over the connection the forwards are on.
// Fails if a session started on the engine is not forwarded without a check being asked for, or if
// following needs a connection of its own (new connections are refused here).
func TestPortFwd005(t *testing.T) {
	port, other := freePort(t), freePort(t)
	var client *journey.Machine
	var engine *journey.Engine
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd005{
		GivenTheClientForwardsARunningSessionSPortAndItsForwarderIsWatching: func() {
			client, engine = forwardEngine(t, port)
			engine.NewRepo("/home/dev/src/project-b")
			engine.MustExec("engineroom project add /home/dev/src/project-b --name project-b")
			dir := strings.TrimSpace(engine.MustExec("engineroom settings where project-b"))
			engine.MustExec(fmt.Sprintf("mkdir -p %s && printf '[ports]\\nweb = %d\\n' > %s/engineroom.toml", dir, other, dir))
			if out, err := client.Run("engineroom", "forward", "on"); err != nil {
				t.Fatalf("forward on: %v\n%s", err, out)
			}
			if got := greeting(port); got != "hello-from-engine" {
				t.Fatalf("the forward was not made: %q", got)
			}
			watchForwards(t, client)
		},
		AndANewConnectionToTheEngineCouldNotBeMade: func() {
			engine.SetReachable(client, false)
			if out, err := client.Run("ssh", "-F", engine.SSHConfig, "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", "engine", "true"); err == nil {
				t.Fatalf("a new connection to the engine was still made:\n%s", out)
			}
		},
		WhenAnotherSessionStartsOnTheEngineWithAServerOnItsPort: func() {
			serveOnEngine(engine, other)
			engine.StartSession("project-b", "/home/dev/src/project-b")
		},
		ThenWithinSecondsTheClientSPortOfThatNumberReachesTheEnginesServer: func() {
			deadline := time.Now().Add(15 * time.Second)
			for {
				got := greeting(other)
				if got == "hello-from-engine" {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("port %d does not reach the engine after 15 seconds: %q", other, got)
				}
				time.Sleep(500 * time.Millisecond)
			}
		},
	}, term0(&term))
}

// PORT-FWD-006: with forwarding on but no connection, the forwarder does nothing. Fails if it
// opens the connection again, or reads the engine (which would write a new record of its pass).
func TestPortFwd006(t *testing.T) {
	port := freePort(t)
	var client *journey.Machine
	var engine *journey.Engine
	var term *termtest.Terminal
	var before, history string

	scenarios.Run(t, scenarios.PortFwd006{
		GivenForwardingIsOnAndASessionIsRunningOnTheEngine: func() {
			client, engine = forwardEngine(t, port)
			if out, err := client.Run("engineroom", "forward", "on"); err != nil {
				t.Fatalf("forward on: %v\n%s", err, out)
			}
		},
		AndTheClientHasNoConnectionToTheEngine: func() {
			m := forward.Master{R: action.Remote{Host: "engine", SSHConfig: engine.SSHConfig}}
			if out, err := client.Run(m.Stop().Argv[0], m.Stop().Argv[1:]...); err != nil {
				t.Fatalf("closing the connection: %v\n%s", err, out)
			}
			var connected bool
			if connected, before = fwdStatus(t, client); connected {
				t.Fatal("the connection is still open")
			}
			history, _ = client.Run("engineroom", "history", "--json")
		},
		WhenTheForwarderWatchesForAWhile: func() {
			watchForwards(t, client)
			time.Sleep(8 * time.Second)
			if out, err := client.Run("engineroom", "forward", "--check"); err != nil {
				t.Fatalf("a check failed: %v\n%s", err, out)
			}
		},
		ThenNoConnectionToTheEngineIsOpened: func() {
			connected, last := fwdStatus(t, client)
			if connected {
				t.Fatal("the forwarder opened the connection again")
			}
			if last != before {
				t.Fatalf("the forwarder read the engine: its record went from %s to %s", before, last)
			}
		},
		AndNothingIsAddedToTheHistory: func() {
			if now, _ := client.Run("engineroom", "history", "--json"); now != history {
				t.Fatalf("the history changed:\n%s", now)
			}
		},
	}, term0(&term))
}

// PORT-FWD-007: a session's own switch forwards that session alone, and switching it off again
// closes the connection, since nothing else is wanted. Fails if the other session is forwarded
// too, or the connection is left open with nothing to carry.
func TestPortFwd007(t *testing.T) {
	port, other := freePort(t), freePort(t)
	var client *journey.Machine
	var engine *journey.Engine
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortFwd007{
		GivenForwardingEverySessionIsOffAndTwoSessionSRunOnTheEngineEachWithAServerOnItsPort: func() {
			client, engine = forwardEngine(t, port)
			engine.NewRepo("/home/dev/src/project-b")
			engine.MustExec("engineroom project add /home/dev/src/project-b --name project-b")
			dir := strings.TrimSpace(engine.MustExec("engineroom settings where project-b"))
			engine.MustExec(fmt.Sprintf("mkdir -p %s && printf '[ports]\\nweb = %d\\n' > %s/engineroom.toml", dir, other, dir))
			serveOnEngine(engine, other)
			engine.StartSession("project-b", "/home/dev/src/project-b")
			if api(t, client, "status")["forward"] != false {
				t.Fatal("forwarding every session is on to begin with")
			}
		},
		WhenTheDeveloperSwitchesOneSessionSForwardingOn: func() {
			if out, err := client.Run("engineroom", "forward", "add", "project-b"); err != nil {
				t.Fatalf("forward add: %v\n%s", err, out)
			}
		},
		ThenThatSessionSPortReachesTheEnginesServer: func() {
			if got := greeting(other); got != "hello-from-engine" {
				t.Fatalf("project-b's port %d does not reach the engine: %q", other, got)
			}
		},
		AndTheOtherSessionSPortIsNotForwarded: func() {
			if got := greeting(port); got == "hello-from-engine" {
				t.Fatalf("project-a's port %d was forwarded too", port)
			}
		},
		AndOnceTheySwitchItOffAgainItsPortNoLongerReachesTheEngineAndTheConnectionToTheEngineIsClosed: func() {
			if out, err := client.Run("engineroom", "forward", "remove", "project-b"); err != nil {
				t.Fatalf("forward remove: %v\n%s", err, out)
			}
			if got := greeting(other); got == "hello-from-engine" {
				t.Fatalf("project-b's port %d still reaches the engine", other)
			}
			if connected, _ := fwdStatus(t, client); connected {
				t.Fatal("the connection to the engine is still open with nothing to forward")
			}
		},
	}, term0(&term))
}

// PORT-SLOT-004: a project with one slot, which its main clone runs on while it has claimed
// none, refuses new work before creating anything. Fails if the worktree is given slot 1 and so
// the main clone's ports.
func TestPortSlot004(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.PortSlot004{
		GivenAProjectWhoseSettingsGiveItOneSlotWhichItsMainCloneRunsOn: func() {
			settingsOf(t, m, "project-a", "[[slot]]\nslot = 1\nports = { web = 21000 }\n")
		},
		WhenTheDeveloperStartsNewWorkOnIt: func() {
			term = m.Start(t, "new", "project-a", "w1")
		},
		ThenTheyAreToldEverySlotIsHeldNamingTheMainClone: func() {
			term.WaitFor("every slot is held")
			if !strings.Contains(term.Screen(), "the main clone") {
				t.Fatalf("the main clone is not named:\n%s", term.Screen())
			}
		},
		AndNoCheckoutIsCreated: func() {
			if _, err := os.Stat(clone + "-w1"); !os.IsNotExist(err) {
				t.Fatalf("a worktree was created: %v", err)
			}
			if strings.Contains(m.Git(clone, "branch", "--list", "w1"), "w1") {
				t.Fatalf("a branch was created")
			}
		},
	}, term0(&term))
}

// PORT-SLOT-005: `engineroom slot ignore` adds the slot's record and environment file to the
// repository's .gitignore, and asking again adds nothing. Fails if git still sees either file,
// or a second request duplicates the lines.
func TestPortSlot005(t *testing.T) {
	m, clone, _ := workProject(t)
	dir := clone + "-w1"
	var first, second string

	scenarios.Run(t, scenarios.PortSlot005{
		GivenNewWorkHoldsASlotAndTheRepositorysGitignoreNamesNoneOfTheSlotsFiles: func() {
			settingsOf(t, m, "project-a", slotsFile)
			newWork(t, m, "w1")
			if out, err := m.Run("git", "-C", dir, "check-ignore", "-q", ".engineroom-slot.json"); err == nil {
				t.Fatalf("the record is already ignored: %s", out)
			}
		},
		WhenTheDeveloperAsksForTheSlotsFilesToBeIgnoredTwice: func() {
			var err error
			if first, err = m.Run("engineroom", "slot", "ignore", "--worktree", dir); err != nil {
				t.Fatalf("engineroom slot ignore: %v\n%s", err, first)
			}
			if second, err = m.Run("engineroom", "slot", "ignore", "--worktree", dir); err != nil {
				t.Fatalf("engineroom slot ignore again: %v\n%s", err, second)
			}
		},
		ThenGitIgnoresTheRecordAndItsEnvironmentFile: func() {
			for _, f := range []string{".engineroom-slot.json", ".engineroom-slot.env"} {
				if out, err := m.Run("git", "-C", dir, "check-ignore", "-q", f); err != nil {
					t.Fatalf("%s is not ignored: %v %s\nfirst run said: %s", f, err, out, first)
				}
			}
		},
		AndGitignoreNamesEachOfThemOnce: func() {
			b, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
			for _, line := range []string{"/.engineroom-slot.json", "/.engineroom-slot.env"} {
				if n := strings.Count(string(b), line+"\n"); n != 1 {
					t.Fatalf("%s appears %d times:\n%s", line, n, b)
				}
			}
			if !strings.Contains(second, "unchanged") {
				t.Fatalf("the second request said: %s", second)
			}
		},
	}, nil)
}
