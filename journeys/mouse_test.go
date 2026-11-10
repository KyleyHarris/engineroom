//go:build journeys

package journeys

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// selectedLine is the line the list's cursor is on.
func selectedLine(screen string) string {
	for _, l := range strings.Split(screen, "\n") {
		if strings.HasPrefix(l, "> ") {
			return l
		}
	}
	return ""
}

// waitSelected waits until the cursor is on the row naming name.
func waitSelected(t *testing.T, term *termtest.Terminal, name string) {
	t.Helper()
	term.WaitUntil(name+" being selected", func(s string) bool { return strings.Contains(selectedLine(s), name) })
}

// SESS-MOUSE-001: a click on a row selects it, and a click on the selected row joins it. Fails
// if the click selects another row, or the second click does anything but join that session.
func TestSessMouse001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var term *termtest.Terminal
	var row int

	scenarios.Run(t, scenarios.SessMouse001{
		GivenTwoSessionsAreRunning: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
		},
		WhenTheDeveloperClicksTheSecondSessionInTheList: func() {
			term = m.Start(t)
			term.WaitFor("project-b")
			waitSelected(t, term, "project-a")
			row = term.RowOf("project-b")
			term.Click(10, row)
		},
		ThenTheSecondSessionIsSelected: func() {
			waitSelected(t, term, "project-b")
		},
		AndClickingItAgainJoinsIt: func() {
			term.Click(10, row)
			term.WaitFor("Zellij (project-b)")
		},
	}, term0(&term))
}

// SESS-MOUSE-002: the wheel moves the selection a row per turn, down then up. Fails if a turn
// moves more or less than one row, or the wheel is ignored.
func TestSessMouse002(t *testing.T) {
	m := journey.NewMachine(t)
	a, b, c := m.NewRepo("project-a"), m.NewRepo("project-b"), m.NewRepo("project-c")
	m.BeEngine("project-a", a, "project-b", b, "project-c", c)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.SessMouse002{
		GivenThreeSessionsAreRunningAndTheFirstIsSelected: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
			m.StartSession("project-c", c)
			term = m.Start(t)
			term.WaitFor("project-c")
			waitSelected(t, term, "project-a")
		},
		WhenTheDeveloperScrollsDownTwiceOverTheList: func() {
			row := term.RowOf("project-b")
			term.Wheel(true, 10, row)
			waitSelected(t, term, "project-b")
			term.Wheel(true, 10, row)
		},
		ThenTheThirdSessionIsSelected: func() {
			waitSelected(t, term, "project-c")
		},
		AndScrollingUpOnceSelectsTheSecond: func() {
			term.Wheel(false, 10, term.RowOf("project-b"))
			waitSelected(t, term, "project-b")
		},
	}, term0(&term))
}

// openPreview shows the commands that would open project-a, which has no session yet: four
// commands, the second of them the session's creation.
func openPreview(t *testing.T, m *journey.Machine) (*termtest.Terminal, []string) {
	t.Helper()
	out, err := m.Run("engineroom", "plan", "open", "project-a", "--json")
	if err != nil {
		t.Fatalf("engineroom plan: %v\n%s", err, out)
	}
	var reply struct{ Plan action.Plan }
	if err := json.Unmarshal([]byte(out), &reply); err != nil || len(reply.Plan.Commands) < 3 {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	term := m.Start(t)
	term.WaitFor("Registered projects")
	term.Type("p")
	term.WaitFor("Nothing has run yet")
	return term, reply.Plan.Lines()
}

// LEARN-COPY-001: clicking the second command sends exactly it to the clipboard and says it was
// sent. Fails if another command, several commands, or nothing is sent, or the message claims a
// copy the terminal never confirmed.
func TestLearnCopy001(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine("project-a", m.NewRepo("project-a"))
	var term *termtest.Terminal
	var lines []string

	scenarios.Run(t, scenarios.LearnCopy001{
		GivenTheCommandsOfAnActionAreShown: func() {
			term, lines = openPreview(t, m)
		},
		WhenTheDeveloperCopiesTheSecondCommand: func() {
			term.Click(10, term.RowOf("2. "))
		},
		ThenExactlyThatCommandIsSentToTheTerminalsClipboard: func() {
			term.WaitFor("Sent to the terminal's clipboard")
			if got := clipboard(t, term.Output()); got != lines[1] {
				t.Fatalf("sent %q, want %q", got, lines[1])
			}
		},
		AndTheyAreToldItWasSentToTheTerminalsClipboard: func() {
			// The message names the command it sent; the command itself is checked above, from
			// the clipboard request, since a long one wraps on screen at both spaces and slashes.
			term.WaitFor("Sent to the terminal's clipboard: engineroom session start project-a")
			if strings.Contains(term.Screen(), "Copied") {
				t.Fatalf("the message claims a copy:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}

// LEARN-COPY-002: c sends every command joined with &&, and pasted into a shell where the first
// command cannot run, nothing after it runs. Fails if the commands are out of order, joined so a
// failure does not stop them, or the paste goes on to create the session.
func TestLearnCopy002(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine("project-a", m.NewRepo("project-a"))
	var term *termtest.Terminal
	var lines []string
	var copied string

	scenarios.Run(t, scenarios.LearnCopy002{
		GivenTheCommandsOfAnActionAreShown: func() {
			term, lines = openPreview(t, m)
		},
		WhenTheDeveloperCopiesAllOfThem: func() {
			term.Type("c")
			term.WaitFor("joined with &&")
			copied = clipboard(t, term.Output())
		},
		ThenTheCommandsAreSentToTheTerminalsClipboardInOrderEachJoinedToTheNextWith: func() {
			if want := strings.Join(lines, " &&\n") + "\n"; copied != want {
				t.Fatalf("sent %q, want %q", copied, want)
			}
		},
		AndPastedIntoAShellTheyStopAtTheFirstCommandThatFails: func() {
			// Without the program on PATH the first command fails, so the session must never be
			// created by the commands after it.
			cmd := m.Command("sh", "-c", copied)
			for i, e := range cmd.Env {
				if strings.HasPrefix(e, "PATH=") {
					cmd.Env[i] = "PATH=" + strings.SplitN(e[5:], ":", 2)[1]
				}
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the pasted commands succeeded though the first cannot run:\n%s", out)
			}
			if strings.Contains(m.Sessions(), "project-a") {
				t.Fatalf("a command after the failure ran: the session exists\n%s", m.Sessions())
			}
		},
	}, term0(&term))
}
