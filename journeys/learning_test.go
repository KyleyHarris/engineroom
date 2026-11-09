//go:build journeys

package journeys

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// LEARN-SHOW-001: the commands of an action can be read before it runs, in order, with
// nothing run. Fails if the preview runs anything, or lists the commands out of order.
func TestLearnShow001(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.LearnShow001{
		GivenTheDeveloperHasChosenAnAction: func() {
			term = m.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("project-a")
		},
		WhenTheyAskToSeeWhatItWillDo: func() {
			term.Type("p")
		},
		ThenTheCommandsItWillRunAreListedInTheOrderTheyWillRun: func() {
			term.WaitFor("These commands will run, in this order.")
			screen := term.Screen()
			first := strings.Index(screen, "1. engineroom layout write --out")
			second := strings.Index(screen, "2. cd /")
			third := strings.Index(screen, "4. zellij attach project-a")
			if first < 0 || second < first || third < second {
				t.Fatalf("the commands are not listed in order:\n%s", screen)
			}
		},
		AndNothingHasRunYet: func() {
			term.WaitFor("Nothing has run yet.")
			if n := countSessions(m.Sessions()); n != 0 {
				t.Fatalf("the preview started a session:\n%s", m.Sessions())
			}
			if _, err := os.Stat(filepath.Join(m.Home, ".local", "state", "engineroom", "layouts", "standard.kdl")); err == nil {
				t.Fatal("the preview wrote the layout")
			}
		},
	}, term0(&term))
}

// LEARN-SHOW-002: each command is shown as it starts and with its outcome when it finishes.
// The layout file is replaced by a named pipe, so the first command (writing the layout) and
// the second (Zellij reading it) each wait for the journey, and the screen can be read while
// each is running. Fails if a running command is not shown as running, or its outcome is not
// shown once it finishes.
func TestLearnShow002(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	layout := filepath.Join(m.Home, ".local", "state", "engineroom", "layouts", "standard.kdl")
	if err := os.MkdirAll(filepath.Dir(layout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(layout, 0o644); err != nil {
		t.Fatal(err)
	}
	var term *termtest.Terminal
	var content []byte

	scenarios.Run(t, scenarios.LearnShow002{
		GivenAnActionIsRunning: func() {
			term = m.Start(t, "open", "project-a")
		},
		WhenEachOfItsCommandsStartsAndThenFinishes: func() {
			term.WaitFor("engineroom layout write")
		},
		ThenTheCommandIsShownAsItStarts: func() {
			term.WaitFor("running")
			if !strings.Contains(lineWith(term.Screen(), "engineroom layout write"), "running") {
				t.Fatalf("the first command is not shown as running:\n%s", term.Screen())
			}
			// Release the first command by reading what it writes.
			var err error
			content, err = os.ReadFile(layout)
			if err != nil {
				t.Fatal(err)
			}
		},
		AndItsOutcomeIsShownWhenItFinishes: func() {
			term.WaitFor("ok, done")
			if !strings.Contains(lineWith(term.Screen(), "engineroom layout write"), "done") {
				t.Fatalf("the finished command does not show its outcome:\n%s", term.Screen())
			}
			if !strings.Contains(lineWith(term.Screen(), "cd /"), "running") {
				t.Fatalf("the second command is not shown as running:\n%s", term.Screen())
			}
			// Release the second command: meet Zellij at the pipe it is opening, put a real file
			// in the pipe's place (Zellij may read the layout more than once), then give it the
			// layout through the pipe.
			pipe, err := os.OpenFile(layout, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			tmp := layout + ".real"
			if err := os.WriteFile(tmp, content, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(tmp, layout); err != nil {
				t.Fatal(err)
			}
			_, _ = pipe.Write(content)
			_ = pipe.Close()
			term.WaitFor("AGENT new conversation")
		},
	}, term0(&term))
}

// LEARN-SHOW-003: a failing command stops the action, is named with its error, and the
// commands after it are shown as not run. The project's folder is removed after it is
// registered, so starting the session there fails for real.
func TestLearnShow003(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.LearnShow003{
		GivenAnActionIsRunning: func() {
			if err := os.RemoveAll(a); err != nil {
				t.Fatal(err)
			}
			term = m.Start(t, "open", "project-a")
		},
		WhenOneOfItsCommandsFails: func() {
			term.WaitFor("FAILED")
		},
		ThenTheActionStopsAtThatCommand: func() {
			if countSessions(m.Sessions()) != 0 {
				t.Fatalf("the action carried on past the failure:\n%s", m.Sessions())
			}
			term.WaitFor("Stopped at the failed command")
		},
		AndThatCommandAndItsErrorAreShown: func() {
			line := lineWith(term.Screen(), "FAILED")
			if !strings.Contains(line, "cd /") {
				t.Fatalf("the failed line does not name the command:\n%s", term.Screen())
			}
			term.WaitFor("no such file or directory")
		},
		AndTheCommandsAfterItAreShownAsNotRun: func() {
			if !strings.Contains(lineWith(term.Screen(), "zellij attach project-a"), "not run") {
				t.Fatalf("the later command is not shown as not run:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}

// LEARN-HIST-001: every action is in the history with when, where, its commands and their
// outcomes. Fails if either action is missing or lacks any of those.
func TestLearnHist001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	host, _ := os.Hostname()
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.LearnHist001{
		GivenTheDeveloperHasRunActionsOnThisMachine: func() {
			m.StartSession("project-b", b)
			first := m.Start(t)
			first.WaitFor("project-b")
			first.Type("x")
			first.WaitFor("Stop the session project-b?")
			first.Type("y")
			first.WaitFor("Done.")
			first.Type("q")
			first.WaitExit()
			second := m.Start(t, "open", "project-a")
			second.WaitFor("AGENT new conversation")
			detach(second)
			second.WaitExit()
		},
		WhenTheyOpenTheHistory: func() {
			term = m.Start(t)
			term.WaitFor("project-a")
			term.Type("h")
			term.WaitFor("History")
		},
		ThenEachActionIsListedWithWhenItRanWhichMachineItRanOnItsCommandsAndTheirOutcomes: func() {
			term.WaitFor("Open project-a")
			term.WaitFor("Stop project-b")
			when := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
			for _, title := range []string{"Open project-a", "Stop project-b"} {
				line := lineWith(term.Screen(), title)
				if !when.MatchString(line) || !strings.Contains(line, host[:min(len(host), 12)]) || !strings.Contains(line, "done") {
					t.Fatalf("the entry for %s lacks its time, machine or outcome:\n%s", title, line)
				}
			}
			// The newest action is selected, and its commands are shown with their outcomes.
			term.WaitFor("engineroom layout write --out")
			if !strings.Contains(lineWith(term.Screen(), "cd /"), "ok, done") {
				t.Fatalf("the commands are not shown with their outcomes:\n%s", term.Screen())
			}
			term.Press(termtest.Down)
			term.WaitFor("engineroom session end project-b")
		},
	}, term0(&term))
}

// LEARN-HIST-002: the copied commands of a past action run as they are. The copy is read from
// what the program sent to the terminal (the clipboard request), and run in a shell; fails if
// nothing was copied or the pasted commands do not do what the action did.
func TestLearnHist002(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var term *termtest.Terminal
	var copied string

	scenarios.Run(t, scenarios.LearnHist002{
		GivenAnActionIsInTheHistory: func() {
			m.StartSession("project-a", a)
			first := m.Start(t)
			first.WaitFor("project-a")
			first.Type("x")
			first.WaitFor("Stop the session project-a?")
			first.Type("y")
			first.WaitFor("Done.")
			first.Type("q")
			first.WaitExit()
		},
		WhenTheDeveloperCopiesItsCommands: func() {
			m.StartSession("project-a", a) // running again, so the pasted stop has work to do
			term = m.Start(t)
			term.WaitFor("project-a")
			term.Type("h")
			term.WaitFor("Stop project-a")
			term.Type("c")
			// Stop is the session's end, then stopping what still runs for it (D-020).
			term.WaitFor("Sent 2 command(s) to the terminal's clipboard")
			copied = clipboard(t, term.Output())
		},
		ThenTheyGetCommandsThatRunAsTheyAreWhenPastedIntoATerminal: func() {
			end, reap := strings.Index(copied, "engineroom session end project-a"), strings.Index(copied, "engineroom session reap project-a")
			if end < 0 || reap < end {
				t.Fatalf("copied %q", copied)
			}
			if out, err := m.Run("sh", "-c", copied); err != nil {
				t.Fatalf("the pasted commands failed: %v\n%s", err, out)
			}
			if journey.SessionState(m.Sessions(), "project-a") == "running" {
				t.Fatalf("the pasted stop did not stop the session:\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}

// clipboard decodes the last OSC 52 clipboard request in a terminal's output.
func clipboard(t *testing.T, output string) string {
	t.Helper()
	re := regexp.MustCompile(`\x1b\]52;c;([A-Za-z0-9+/=]+)\x07`)
	all := re.FindAllStringSubmatch(output, -1)
	if len(all) == 0 {
		t.Fatal("the program sent nothing to the clipboard")
	}
	b, err := base64.StdEncoding.DecodeString(all[len(all)-1][1])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// lineWith returns the first screen line containing text.
func lineWith(screen, text string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

// LEARN-HIST-003: a join that cannot attach is kept as failed, with Zellij's words. Zellij is made
// to refuse the attach by a stand-in in front of it on this machine's PATH that fails "attach" and
// passes everything else to the real Zellij. Fails if the history keeps the join as done.
func TestLearnHist003(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var entries []map[string]any

	scenarios.Run(t, scenarios.LearnHist003{
		GivenTheDeveloperJoinsASessionAndZellijCannotAttachToIt: func() {
			m.StartSession("project-a", a)
			real, err := exec.LookPath("zellij")
			if err != nil {
				t.Fatal(err)
			}
			m.WriteFile(filepath.Join(m.Bin, "zellij"), "#!/bin/sh\nif [ \"$1\" = attach ]; then echo 'There is no active session named project-a' >&2; exit 1; fi\nexec '"+real+"' \"$@\"\n", 0o755)
			term := m.Start(t, "join", "project-a")
			term.WaitFor("ended with")
			term.Type("q")
			term.WaitExit()
		},
		WhenTheyOpenTheHistory: func() {
			out, err := m.Run("engineroom", "history", "--json")
			if err != nil {
				t.Fatalf("history: %v\n%s", err, out)
			}
			if err := json.Unmarshal([]byte(out), &entries); err != nil || len(entries) == 0 {
				t.Fatalf("history: %v\n%s", err, out)
			}
		},
		ThenTheJoinIsListedAsFailedWithWhatZellijSaid: func() {
			e := entries[0]
			cmds := e["commands"].([]any)
			last := cmds[len(cmds)-1].(map[string]any)
			if e["status"] != "failed" || last["status"] != "failed" || !strings.Contains(fmt.Sprint(last["output"]), "no active session") {
				t.Fatalf("the join is kept as %v, its command as %v", e["status"], last)
			}
		},
	}, nil)
}
