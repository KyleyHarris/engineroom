//go:build journeys

package journeys

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// The menu bar app runs nothing itself: it shows and does what `engineroom api` answers. These
// journeys call those commands as the app does and check what really happened.

// api runs `engineroom api <args>` on m and decodes its JSON answer. For run, the answer is one event
// per line; the last (the outcome) is returned, with every event in "events".
func api(t *testing.T, m *journey.Machine, args ...string) map[string]any {
	t.Helper()
	// Standard output only, as the app reads it: a plan says how far its reading has got on
	// standard error (CONN-HANDOFF-013), which is not part of the answer.
	cmd := m.Command("engineroom", append([]string{"api"}, args...)...)
	var errOut strings.Builder
	cmd.Stderr = &errOut
	raw, _ := cmd.Output()
	out := string(raw)
	if len(args) > 0 && args[0] == "run" {
		var events []any
		var last map[string]any
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			var e map[string]any
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatalf("engineroom api %v: not one JSON event per line: %v\n%s", args, err, out)
			}
			events, last = append(events, e), e
		}
		last["events"] = events
		return last
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("engineroom api %v: not JSON: %v\n%s", args, err, out)
	}
	return v
}

// mustRun runs an api action and fails unless it finished ok.
func mustRun(t *testing.T, m *journey.Machine, args ...string) map[string]any {
	t.Helper()
	done := api(t, m, append([]string{"run"}, args...)...)
	if done["ok"] != true {
		// The events are printed one by one: the outcome is itself among them, so printing it
		// whole would print itself without end.
		var b strings.Builder
		for _, e := range asList(done["events"]) {
			ev := asMap(e)
			fmt.Fprintf(&b, "\n  %v %v %v %v", ev["event"], ev["status"], ev["output"], ev["error"])
		}
		t.Fatalf("engineroom api run %v did not finish ok:%s", args, b.String())
	}
	return done
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// APP-LIST-001: the app is told which session is waiting, with its question, and the other is
// listed once and not marked. Fails if the mark or the question is not carried, a session is
// listed twice, or the one that is not waiting is marked.
func TestAppList001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var list []any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppList001{
		GivenTheEngineHasASessionWaitingForTheDeveloperAndOneThatIsNot: func() {
			m.StartSession("project-a", a)
			m.StartSession("project-b", b)
			if out, err := m.Run("engineroom", "signal", "waiting", "--session", "project-b", "--message", "Approve the plan?"); err != nil {
				t.Fatalf("signal: %v\n%s", err, out)
			}
		},
		WhenTheAppListsTheSessions: func() {
			list = asList(api(t, m, "sessions")["sessions"])
		},
		ThenTheWaitingSessionIsMarkedAsWaitingWithItsQuestion: func() {
			b := sessionNamed(t, list, "project-b")
			if b["waiting"] != "waiting" || b["question"] != "Approve the plan?" {
				t.Fatalf("project-b: %v", b)
			}
		},
		AndTheOtherIsListedOnceNotMarked: func() {
			if len(list) != 2 {
				t.Fatalf("listed %v", list)
			}
			if a := sessionNamed(t, list, "project-a"); a["waiting"] == "waiting" || a["question"] != nil {
				t.Fatalf("project-a: %v", a)
			}
		},
	}, term0(&term))
}

// sessionNamed is the listed session named name; the list's order is the app's to lay out
// (APP-LIST-002), so a session is found by its name, never by its place.
func sessionNamed(t *testing.T, list []any, name string) map[string]any {
	t.Helper()
	for _, s := range list {
		if v := asMap(s); v["name"] == name {
			return v
		}
	}
	t.Fatalf("no session %s in %v", name, list)
	return nil
}

// APP-REPOS-001: one root's repositories on GitHub and in its folder, each marked for where it
// is. Fails if the local-only repository is missing, or any mark or category is wrong.
func TestAppRepos001(t *testing.T) {
	m, gh, dir := withRoot(t)
	var repos map[string]map[string]any
	var root map[string]any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppRepos001{
		GivenARootWhoseOwnerHasOneRepositoryOnGitHubThatIsNotHere: func() {
			gh.AddRepo("owner-a", "widget")
		},
		AndOneClonedInTheRootAndRegisteredInTheCategoryTools: func() {
			gh.AddRepo("owner-a", "sprocket")
			clone := filepath.Join(dir, "sprocket")
			if out, err := m.Run("git", "clone", "-q", "git@github.com:owner-a/sprocket.git", clone); err != nil {
				t.Fatalf("clone: %v\n%s", err, out)
			}
			run(t, m, "project", "add", clone, "--category", "tools")
		},
		AndAGitRepositoryInTheFolderOfTheRootThatIsNotRegistered: func() {
			if out, err := m.Run("git", "init", "-q", filepath.Join(dir, "scratch")); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
		},
		WhenTheAppListsTheRepositories: func() {
			roots := asList(api(t, m, "repos")["roots"])
			if len(roots) != 1 {
				t.Fatalf("roots %v", roots)
			}
			root = asMap(roots[0])
			repos = map[string]map[string]any{}
			for _, r := range asList(root["repos"]) {
				repos[asMap(r)["name"].(string)] = asMap(r)
			}
		},
		ThenEachIsListedUnderTheRoot: func() {
			if asMap(root["root"])["name"] != "owners" || len(repos) != 3 || repos["widget"] == nil || repos["sprocket"] == nil || repos["scratch"] == nil {
				t.Fatalf("under %v: %v", root["root"], repos)
			}
		},
		AndTheyAreMarkedOnGitHubButNotHereRegisteredInToolsAndHereButNotRegistered: func() {
			if repos["widget"]["here"] != nil || repos["sprocket"]["here"] != "registered" || repos["sprocket"]["category"] != "tools" || repos["scratch"]["here"] != "folder" || repos["scratch"]["local"] != true {
				t.Fatalf("marked %v", repos)
			}
		},
	}, term0(&term))
}

// APP-REPOS-002: registering a folder from the app makes it a project in the category, and the
// command is in the history. Fails if it is registered without the category or not recorded.
func TestAppRepos002(t *testing.T) {
	m, _, dir := withRoot(t)
	folder := filepath.Join(dir, "scratch")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppRepos002{
		GivenAGitRepositoryInTheFolderOfARootThatIsNotRegistered: func() {
			if out, err := m.Run("git", "init", "-q", folder); err != nil {
				t.Fatalf("git init: %v\n%s", err, out)
			}
		},
		WhenTheDeveloperRegistersItFromTheAppInTheCategoryTools: func() {
			mustRun(t, m, "register", folder, "--category", "tools")
		},
		ThenItIsAProjectInTheCategoryTools: func() {
			if list := registered(t, m); !strings.Contains(list, "scratch\t"+resolve(folder)+"\ttools") && !strings.Contains(list, "scratch\t"+folder+"\ttools") {
				t.Fatalf("registered as:\n%s", list)
			}
		},
		AndTheCommandThatRegisteredItIsInTheHistory: func() {
			out, _ := m.Run("engineroom", "history", "--json")
			if !strings.Contains(out, "Register scratch") || !strings.Contains(out, "engineroom project add") {
				t.Fatalf("not in the history:\n%s", out)
			}
		},
	}, term0(&term))
}

// APP-ADD-001: getting a repository from the app clones it into the root and registers it.
func TestAppAdd001(t *testing.T) {
	m, gh, dir := withRoot(t)
	clone := filepath.Join(dir, "widget")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppAdd001{
		GivenARootWhoseOwnerHasARepositoryCalledWidgetThatIsNotHere: func() {
			gh.AddRepo("owner-a", "widget")
		},
		WhenTheDeveloperGetsWidgetFromTheAppIntoThatRootInTheCategoryTools: func() {
			mustRun(t, m, "clone", "owners", "widget", "--category", "tools")
		},
		ThenItIsClonedIntoTheRootAsWidget: func() {
			out, err := exec.Command("git", "-C", clone, "rev-parse", "HEAD").Output()
			if err != nil || strings.TrimSpace(string(out)) != gh.Head("owner-a", "widget") {
				t.Fatalf("%s is not a clone of owner-a/widget: %v %s", clone, err, out)
			}
		},
		AndItIsAProjectInTheCategoryTools: func() {
			if list := registered(t, m); !strings.Contains(list, "widget\t"+resolve(clone)+"\ttools") {
				t.Fatalf("registered as:\n%s", list)
			}
		},
	}, term0(&term))
}

// APP-ADD-002: creating from the app makes a public repository holding the first commit, as the
// root's account, and registers it. Fails on the wrong visibility, a different commit, or no
// project.
func TestAppAdd002(t *testing.T) {
	m, gh, dir := withRoot(t)
	folder := filepath.Join(dir, "gadget")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppAdd002{
		GivenTheOwnerOfARootHasNoRepositoryCalledGadget: func() {
			if gh.Has("owner-a", "gadget") {
				t.Fatal("the owner already has gadget")
			}
		},
		WhenTheDeveloperCreatesGadgetFromTheAppAsAPublicRepositoryInTheCategoryTools: func() {
			mustRun(t, m, "create", "owners", "gadget", "--category", "tools", "--public")
		},
		ThenAPublicRepositoryGadgetIsCreatedForTheOwnerOnGitHubHoldingItsFirstCommit: func() {
			local, _ := exec.Command("git", "-C", folder, "rev-parse", "HEAD").Output()
			if gh.Visibility("owner-a", "gadget") != "PUBLIC" || gh.Head("owner-a", "gadget") == "" || gh.Head("owner-a", "gadget") != strings.TrimSpace(string(local)) {
				t.Fatalf("on GitHub: %q %q, local %s", gh.Visibility("owner-a", "gadget"), gh.Head("owner-a", "gadget"), local)
			}
		},
		AndItIsAProjectInTheCategoryTools: func() {
			if list := registered(t, m); !strings.Contains(list, "gadget\t"+resolve(folder)+"\ttools") {
				t.Fatalf("registered as:\n%s", list)
			}
		},
	}, term0(&term))
}

// startsWithApps is an engine with project-a and Ghostty installed, the terminal stand-ins in place.
func startsWithApps(t *testing.T) *journey.Machine {
	t.Helper()
	m := journey.NewMachine(t)
	m.BeEngine("project-a", m.NewRepo("project-a"))
	m.FakeTerminals("Ghostty.app", "Terminal.app")
	return m
}

// joinedIn checks Ghostty was asked once, through its scripting, to join the session in a tab
// titled for it, running this program's join of the session.
func joinedIn(t *testing.T, m *journey.Machine, session string) {
	t.Helper()
	w := m.Windows()
	if len(w) != 1 || !strings.HasPrefix(w[0], "osascript ") || !strings.Contains(w[0], `tell application "Ghostty"`) ||
		!strings.Contains(w[0], " engineroom: "+session+" ") || !strings.Contains(w[0], " join "+session+" ") {
		t.Fatalf("windows opened: %q", w)
	}
}

// APP-START-001: Zellij starts the session without Remote Control and opens one window joined
// to it. Fails if Remote Control is on, no session runs, or no window (or two) is opened.
func TestAppStart001(t *testing.T) {
	m := startsWithApps(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppStart001{
		GivenAProjectWithNoSession: func() {},
		WhenTheDeveloperStartsItFromTheAppInZellij: func() {
			mustRun(t, m, "start", "project-a", "--mode", "zellij")
		},
		ThenASessionRunsClaudeCodeInsideZellijWithoutRemoteControl: func() {
			m.WaitSession("project-a", "running")
			waitAgent(t, m)
			if strings.Contains(strings.Join(m.AgentStarts(), "\n"), "--remote-control") {
				t.Fatalf("started with Remote Control: %q", m.AgentStarts())
			}
		},
		AndAWindowOfTheChosenTerminalIsOpenedJoinedToThatSession: func() {
			joinedIn(t, m, "project-a")
		},
	}, term0(&term))
}

// waitAgent waits until the agent stand-in has started in the session.
func waitAgent(t *testing.T, m *journey.Machine) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for len(m.AgentStarts()) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if len(m.AgentStarts()) == 0 {
		t.Fatal("the agent never started in the session")
	}
}

// APP-START-002: Remote Control starts the agent under the session's name and opens nothing.
func TestAppStart002(t *testing.T) {
	m := startsWithApps(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppStart002{
		GivenAProjectWithNoSession: func() {},
		WhenTheDeveloperStartsItFromTheAppWithRemoteControl: func() {
			mustRun(t, m, "start", "project-a", "--mode", "remote")
		},
		ThenASessionRunsClaudeCodeInsideZellijWithRemoteControlUnderTheNameOfTheSession: func() {
			m.WaitSession("project-a", "running")
			waitAgent(t, m)
			if !strings.Contains(lastStart(m.AgentStarts()), "--remote-control project-a") {
				t.Fatalf("not started with Remote Control as project-a: %q", m.AgentStarts())
			}
		},
		AndNoTerminalWindowIsOpened: func() {
			if w := m.Windows(); len(w) != 0 {
				t.Fatalf("windows opened: %q", w)
			}
		},
	}, term0(&term))
}

// APP-START-003: both starts with Remote Control and opens one window.
func TestAppStart003(t *testing.T) {
	m := startsWithApps(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppStart003{
		GivenAProjectWithNoSession: func() {},
		WhenTheDeveloperStartsItFromTheAppWithBoth: func() {
			mustRun(t, m, "start", "project-a", "--mode", "both")
		},
		ThenASessionRunsClaudeCodeInsideZellijWithRemoteControlUnderTheNameOfTheSession: func() {
			m.WaitSession("project-a", "running")
			waitAgent(t, m)
			if !strings.Contains(lastStart(m.AgentStarts()), "--remote-control project-a") {
				t.Fatalf("not started with Remote Control as project-a: %q", m.AgentStarts())
			}
		},
		AndAWindowOfTheChosenTerminalIsOpenedJoinedToThatSession: func() {
			joinedIn(t, m, "project-a")
		},
	}, term0(&term))
}

// APP-EDIT-001: on the engine the editor opens the folder itself; on a client it opens the engine's
// folder over SSH. Fails if either opens the wrong folder or the client opens a folder of its own.
func TestAppEdit001(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppEdit001{
		GivenAProjectRegisteredOnTheEngine: func() {
			m.BeEngine("project-a", path)
			engine.BeEngine("project-a", "/home/dev/src/project-a")
		},
		WhenTheDeveloperOpensItInEditorFromTheAppOnTheEngineAndFromTheAppOnAClient: func() {
			mustRun(t, m, "edit-project", "project-a")
			mustRun(t, client, "edit-project", "project-a")
		},
		ThenOnTheEngineEditorOpensTheFolderOfTheProjectItself: func() {
			if s := m.EditorStarts(); len(s) != 1 || s[0] != path {
				t.Fatalf("the editor on the engine started as %q, want %s", s, path)
			}
		},
		AndOnTheClientEditorOpensTheFolderOnTheEngineOverSSH: func() {
			if s := client.EditorStarts(); len(s) != 1 || s[0] != "--remote ssh-remote+engine /home/dev/src/project-a" {
				t.Fatalf("the editor on the client started as %q", s)
			}
		},
	}, term0(&term))
}

// APP-TERM-001: only installed terminals are offered, Ghostty by default; a chosen Terminal is
// remembered and used for joins. Fails if an absent terminal is offered, the default is not
// Ghostty, or the choice does not hold for the next join.
func TestAppTerm001(t *testing.T) {
	m := startsWithApps(t)
	var status map[string]any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppTerm001{
		GivenGhosttyAndTerminalAreTheTerminalsInstalledOnThisMac: func() {},
		WhenTheDeveloperLooksAtWhichTerminalSessionsAreJoinedIn: func() {
			status = api(t, m, "status")
		},
		ThenOnlyGhosttyAndTerminalAreOfferedAndGhosttyIsChosen: func() {
			var installed []string
			for _, x := range asList(status["terminals"]) {
				if asMap(x)["path"] != nil {
					installed = append(installed, asMap(x)["name"].(string))
				}
			}
			if strings.Join(installed, ",") != "Ghostty,Terminal" || status["terminal"] != "Ghostty" {
				t.Fatalf("offered %v, chosen %v", installed, status["terminal"])
			}
		},
		AndOnceTheDeveloperChoosesTerminalSessionsAreJoinedInTerminalFromThenOn: func() {
			if got := api(t, m, "set", "terminal", "Terminal")["terminal"]; got != "Terminal" {
				t.Fatalf("chose %v", got)
			}
			m.StartSession("project-a", filepath.Join(m.Home, "src", "project-a"))
			mustRun(t, m, "join", "project-a")
			if w := m.Windows(); len(w) != 1 || !strings.HasPrefix(w[0], "osascript ") || !strings.Contains(w[0], `tell application "Terminal" to do script`) || !strings.Contains(w[0], "join project-a") {
				t.Fatalf("windows opened: %q", w)
			}
			if api(t, m, "status")["terminal"] != "Terminal" {
				t.Fatal("the choice was not remembered")
			}
		},
	}, term0(&term))
}

// APP-PREVIEW-001: asking to create shows the four commands in order and makes nothing. Fails if
// a command is missing or out of order, or anything is made.
func TestAppPreview001(t *testing.T) {
	m, gh, dir := withRoot(t)
	var plan map[string]any
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppPreview001{
		GivenTheOwnerOfARootHasNoRepositoryCalledGadget: func() {},
		WhenTheDeveloperAsksTheAppToCreateGadget: func() {
			plan = asMap(api(t, m, "plan", "create", "owners", "gadget")["plan"])
		},
		ThenTheCommandsItWouldRunAreShownInOrder: func() {
			var argv0 []string
			for _, c := range asList(plan["commands"]) {
				a := asList(asMap(c)["argv"])
				argv0 = append(argv0, a[0].(string)+" "+a[1].(string))
			}
			if strings.Join(argv0, ", ") != "git init, git -C, engineroom gh, engineroom project" {
				t.Fatalf("commands %v", argv0)
			}
		},
		AndNoFolderIsMadeAndNothingIsCreatedOnGitHub: func() {
			if _, err := os.Stat(filepath.Join(dir, "gadget")); err == nil {
				t.Fatal("a folder was made")
			}
			if gh.Has("owner-a", "gadget") || strings.Contains(gh.Log(), "repo create") {
				t.Fatalf("something was created on GitHub:\n%s", gh.Log())
			}
		},
	}, term0(&term))
}

// APP-HIST-001: a start from the app is in the history with each command and its outcome.
func TestAppHist001(t *testing.T) {
	m := startsWithApps(t)
	var out string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppHist001{
		GivenTheDeveloperStartedASessionFromTheApp: func() {
			mustRun(t, m, "start", "project-a", "--mode", "remote")
		},
		WhenTheyOpenTheHistory: func() {
			out, _ = m.Run("engineroom", "history")
		},
		ThenTheStartIsListedWithEachOfItsCommandsAndTheirOutcomes: func() {
			for _, want := range []string{"done", "Start project-a", "ok          engineroom layout write --remote-control", "ok          engineroom session start project-a"} {
				if !strings.Contains(out, want) {
					t.Fatalf("history lacks %q:\n%s", want, out)
				}
			}
		},
	}, term0(&term))
}
