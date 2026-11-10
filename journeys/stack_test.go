//go:build journeys

package journeys

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// stackRecord is a script body that appends where it ran and what it was told to a log, so a
// journey can check the script ran, in which folder, in what order and with which details.
func stackRecord(name, log string) string {
	return `echo "` + name + ` $(pwd) $ENGINEROOM_PROJECT $ENGINEROOM_WORK $ENGINEROOM_WORKTREE" >> "` + log + `"
echo "` + name + ` script says hello"
`
}

// readLog is the log's lines, or none when nothing was written.
func readLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// WORK-STACK-001: starting the stack from the session list runs the repository's up script in
// the session's worktree with the details it is owed, shows its command and output, and keeps
// them in the history. Fails if the script runs elsewhere or without the details, or its output
// is shown nowhere or lost from the history.
func TestWorkStack001(t *testing.T) {
	m, clone, _ := workProject(t)
	log := filepath.Join(m.Home, "stack.log")
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkStack001{
		GivenTheProjectDeclaresAnUpScriptAndHasASessionInACheckout: func() {
			commitScript(t, m, clone, "up", stackRecord("up", log))
			m.Git(clone, "fetch", "-q", "origin")
			dir = startWork(t, m, clone, "stacked")
		},
		WhenTheDeveloperStartsTheStackOfThatSession: func() {
			term = m.Start(t)
			term.WaitFor("project-a-stacked")
			term.Type("u")
			term.WaitFor("Done.")
		},
		ThenTheUpScriptRunsInThatCheckoutToldWhichProjectWorkAndCheckoutItIs: func() {
			// Folders are compared resolved: the worktree may be reached through a link (a
			// temporary folder on macOS), and either spelling is the same folder.
			lines := readLog(t, log)
			f := strings.Fields(strings.Join(lines, " "))
			if len(lines) != 1 || len(f) != 5 || f[0] != "up" || resolve(f[1]) != resolve(dir) || f[2] != "project-a" || f[3] != "stacked" || resolve(f[4]) != resolve(dir) {
				t.Fatalf("the up script ran as %q, in and for %s", lines, dir)
			}
		},
		AndItsCommandAndOutputAreShownAndKeptInTheHistory: func() {
			term.WaitFor("./.engineroom/up")
			term.WaitFor("up script says hello")
			out, err := m.Run("engineroom", "history", "--json")
			if err != nil || !strings.Contains(out, "Start the stack of project-a-stacked") || !strings.Contains(out, "up script says hello") {
				t.Fatalf("not kept in the history: %v\n%s", err, out)
			}
		},
	}, term0(&term))
}

// WORK-STACK-002: stopping the stack runs the down script in the worktree. This journey runs it
// as the agent inside the session would, with `engineroom down` and no terminal, so the plain path is
// covered too. Fails if the script does not run, runs elsewhere, or the plain run hides it.
func TestWorkStack002(t *testing.T) {
	m, clone, _ := workProject(t)
	log := filepath.Join(m.Home, "stack.log")
	var dir, out string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkStack002{
		GivenTheProjectDeclaresADownScriptAndHasASessionInACheckout: func() {
			commitScript(t, m, clone, "down", stackRecord("down", log))
			m.Git(clone, "fetch", "-q", "origin")
			dir = startWork(t, m, clone, "stacked")
		},
		WhenTheDeveloperStopsTheStackOfThatSession: func() {
			cmd := m.Command("engineroom", "down")
			cmd.Env = append(cmd.Env, "ZELLIJ_SESSION_NAME=project-a-stacked")
			b, err := cmd.CombinedOutput()
			out = string(b)
			if err != nil {
				t.Fatalf("engineroom down: %v\n%s", err, out)
			}
		},
		ThenTheDownScriptRunsInThatCheckout: func() {
			lines := readLog(t, log)
			f := strings.Fields(strings.Join(lines, " "))
			if len(lines) != 1 || len(f) != 5 || f[0] != "down" || resolve(f[1]) != resolve(dir) || f[2] != "project-a" || f[3] != "stacked" {
				t.Fatalf("the down script ran as %q", lines)
			}
			for _, want := range []string{"Stop the stack of project-a-stacked", "$ cd ", "./.engineroom/down", "down script says hello", "ok, done"} {
				if !strings.Contains(out, want) {
					t.Fatalf("the plain run does not show %q:\n%s", want, out)
				}
			}
		},
	}, term0(&term))
}

// WORK-STACK-003: clean up runs down, then teardown, both before the worktree is removed. Fails
// if down is skipped, comes after teardown, or either runs after the worktree is gone.
func TestWorkStack003(t *testing.T) {
	m, clone, _ := workProject(t)
	log := filepath.Join(m.Home, "stack.log")
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkStack003{
		GivenTheProjectDeclaresADownScriptAndATeardownScript: func() {
			// Each script also records whether the worktree's own file was still there.
			for _, name := range []string{"down", "teardown"} {
				commitScript(t, m, clone, name, `echo "`+name+` $(test -f done.txt && echo still-there)" >> "`+log+`"
`)
			}
			m.Git(clone, "fetch", "-q", "origin")
			dir = startWork(t, m, clone, "done")
		},
		WhenTheDeveloperCleansUpNewWorkOnItAndConfirms: func() {
			term = cleanAndConfirm(t, m, "done")
		},
		ThenTheDownScriptRunsBeforeTheTeardownScript: func() {
			lines := readLog(t, log)
			if len(lines) != 2 || !strings.HasPrefix(lines[0], "down") || !strings.HasPrefix(lines[1], "teardown") {
				t.Fatalf("the scripts ran as %q", lines)
			}
		},
		AndBothRunBeforeTheCheckoutIsRemoved: func() {
			for _, l := range readLog(t, log) {
				if !strings.HasSuffix(l, "still-there") {
					t.Fatalf("a script ran after the worktree was removed: %q", l)
				}
			}
			if _, err := os.Stat(dir); err == nil {
				t.Fatal("the worktree was not removed")
			}
		},
	}, term0(&term))
}

// WORK-STACK-004: a repository with no up script is told what one must do, and nothing runs or
// is recorded as run. Fails if the tool runs anything in its place.
func TestWorkStack004(t *testing.T) {
	m, clone, _ := workProject(t)
	var term *termtest.Terminal
	var before string

	scenarios.Run(t, scenarios.WorkStack004{
		GivenTheProjectDeclaresNoUpScriptAndHasASessionInACheckout: func() {
			m.Git(clone, "fetch", "-q", "origin")
			startWork(t, m, clone, "plain")
			before, _ = m.Run("engineroom", "history", "--json")
		},
		WhenTheDeveloperStartsTheStackOfThatSession: func() {
			term = m.Start(t)
			term.WaitFor("project-a-plain")
			term.Type("u")
		},
		ThenTheyAreToldTheRepositoryDeclaresNoUpScriptAndWhatOneMustDo: func() {
			term.WaitFor("has no up script, so nothing was run")
			term.WaitFor("returns once it is running")
		},
		AndNothingIsRun: func() {
			if after, _ := m.Run("engineroom", "history", "--json"); after != before {
				t.Fatalf("something was run:\n%s", after)
			}
		},
	}, term0(&term))
}

// WORK-STACK-005: Run from the app on a checkout with no session starts the session first, in the
// background and as the app starts sessions (here with Remote Control), and then the up script.
// Fails if the up script runs with no session, if the session is started after it, without the
// app's Remote Control choice, or if a terminal window is opened for it.
func TestWorkStack005(t *testing.T) {
	// As startsWithApps, keeping the main clone's folder for the up script.
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	m.FakeTerminals("Ghostty.app", "Terminal.app")
	log := filepath.Join(m.Home, "stack.log")
	var done map[string]any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.WorkStack005{
		GivenTheProjectDeclaresAnUpScriptAndItsCheckoutHasNoRunningSession: func() {
			// The up script records whether its session was running when it ran.
			m.WriteFile(filepath.Join(clone, ".engineroom", "up"), "#!/bin/sh\n"+
				`echo "up $(zellij list-sessions --no-formatting 2>/dev/null | grep -c '^project-a ')" >> "`+log+`"`+"\n", 0o755)
			if s := journey.SessionState(m.Sessions(), "project-a"); s != "" {
				t.Fatalf("project-a already has a session: %s", s)
			}
		},
		WhenTheDeveloperStartsTheStackOfThatCheckoutFromTheApp: func() {
			done = mustRun(t, m, "up", "project-a", "--mode", "both")
		},
		ThenItsSessionIsStartedInTheBackgroundAsTheAppStartsSessionSBeforeTheUpScriptRuns: func() {
			m.WaitSession("project-a", "running")
			if lines := readLog(t, log); len(lines) != 1 || lines[0] != "up 1" {
				t.Fatalf("the up script did not run once with the session running: %q", lines)
			}
			waitAgent(t, m)
			if !strings.Contains(lastStart(m.AgentStarts()), "--remote-control project-a") {
				t.Fatalf("not started as the app starts sessions, with Remote Control as project-a: %q", m.AgentStarts())
			}
			// The run finished ok (mustRun), so every command of its plan ran, in the plan's order.
			var all string
			for _, e := range asList(done["events"]) {
				if ev := asMap(e); ev["event"] == "plan" {
					all = fmt.Sprint(ev["lines"])
				}
			}
			create, up := strings.Index(all, "engineroom session start project-a"), strings.Index(all, ".engineroom/up")
			if create < 0 || up < 0 || create > up {
				t.Fatalf("the session is not started before the up script, as commands of the run:\n%s", all)
			}
		},
		AndNoTerminalWindowIsOpened: func() {
			if w := m.Windows(); len(w) != 0 {
				t.Fatalf("windows opened: %q", w)
			}
		},
	}, term0(&term))
}

// WORK-STACK-015: opening a link, the app first asks for `serve <session> <port>`, as the app's
// links do: with nothing answering on the port, the checkout's up script runs there (the app opens
// the link once it is done); with something answering, nothing is planned. Fails if the up script
// does not run, runs elsewhere, or anything is planned while the port answers.
func TestWorkStack015(t *testing.T) {
	m, clone, _ := workProject(t)
	log := filepath.Join(m.Home, "stack.log")
	var dir, port string

	scenarios.Run(t, scenarios.WorkStack015{
		GivenACheckoutOfAProjectWithAnUpScriptHasASessionAndNothingAnswersOnItsPort: func() {
			commitScript(t, m, clone, "up", stackRecord("up", log))
			m.Git(clone, "fetch", "-q", "origin")
			dir = startWork(t, m, clone, "served")
			// A port nothing answers on: one the system just handed out and took back.
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port = strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
			l.Close()
		},
		WhenTheDeveloperOpensOneOfItsLinks: func() {
			mustRun(t, m, "serve", "project-a-served", port)
		},
		ThenItsUpScriptRunsInThatCheckoutBeforeTheLinkIsOpened: func() {
			lines := readLog(t, log)
			if f := strings.Fields(strings.Join(lines, " ")); len(lines) != 1 || len(f) < 2 || f[0] != "up" || resolve(f[1]) != resolve(dir) {
				t.Fatalf("the up script ran as %q, want once in %s", lines, dir)
			}
		},
		AndOpeningALinkWhenSomethingAnswersOnItsPortStartsNothing: func() {
			l, err := net.Listen("tcp", "127.0.0.1:"+port)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					c.Close()
				}
			}()
			p := planOf(t, m, "serve", "project-a-served", port)
			if cmds := asList(p["commands"]); len(cmds) != 0 {
				t.Fatalf("something is planned while the port answers: %v", cmds)
			}
			mustRun(t, m, "serve", "project-a-served", port)
			if lines := readLog(t, log); len(lines) != 1 {
				t.Fatalf("the up script ran again: %q", lines)
			}
		},
	}, nil)
}
