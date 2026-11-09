//go:build journeys

package journeys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
)

// The repository page is `engineroom api project <name>`, read as the app reads it, and its organising
// actions are `engineroom api run pin|category|unregister`, run as the app runs them.

// pageOf is a project's page as the app reads it.
func pageOf(t *testing.T, m *journey.Machine, project string) map[string]any {
	t.Helper()
	return api(t, m, "project", project)
}

// APP-PAGE-001: the page lists the main clone first, running, with slot 1's ports and links, then
// the worktree with its branch and its own slot's ports, and each script with its source. Fails
// if a checkout is missing or out of order, a port or link is the wrong slot's, or the up script's
// source is lost.
func TestAppPage001(t *testing.T) {
	m, clone, _ := workProject(t)
	var page map[string]any
	var checkouts []any

	scenarios.Run(t, scenarios.AppPage001{
		GivenAProjectWithPortsARunningSessionInItsMainCloneAndACheckoutWithNoSession: func() {
			settingsOf(t, m, "project-a", slotsFile+"\n[links]\napp = \"https://localhost:{{web}}/\"\n")
			commitScript(t, m, clone, "up", "exit 0\n")
			newWork(t, m, "fix")
			// Its session ended fully, as the app's Stop does; the worktree stays.
			mustRun(t, m, "stop", "project-a-fix")
			m.StartSession("project-a", clone)
		},
		WhenTheDeveloperOpensTheProjectSPage: func() {
			page = pageOf(t, m, "project-a")
			checkouts = asList(page["checkouts"])
			if len(checkouts) != 2 {
				t.Fatalf("checkouts: %v", page)
			}
		},
		ThenTheMainCloneIsListedFirstAsRunningWithSlot1sPortsAndTheLinksItsStackServes: func() {
			c := checkouts[0].(map[string]any)
			links := asList(c["links"])
			if c["main"] != true || c["running"] != true || c["slot"] != float64(1) || len(links) != 1 || links[0].(map[string]any)["url"] != "https://localhost:21000/" {
				t.Fatalf("main clone: %v", c)
			}
		},
		AndTheCheckoutIsListedWithItsBranchAsNotRunningWithItsOwnSlotsPorts: func() {
			c := checkouts[1].(map[string]any)
			ports := asList(c["ports"])
			if c["main"] != false || c["branch"] != "fix" || c["running"] != false || len(ports) != 1 || ports[0].(map[string]any)["port"] != float64(21010) {
				t.Fatalf("worktree: %v", c)
			}
		},
		AndTheProjectSScriptsAreListedWithWhereEachComesFrom: func() {
			for _, s := range asList(page["scripts"]) {
				sc := s.(map[string]any)
				if sc["name"] == "up" && sc["from"] == "repository" {
					return
				}
			}
			t.Fatalf("the up script's source is not listed: %v", page["scripts"])
		},
	}, nil)
}

// APP-PAGE-002: pinning and a category from the page reach the list, and nothing on disk changes.
// Fails if either is not saved, or the project's folder is touched.
func TestAppPage002(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var before string

	scenarios.Run(t, scenarios.AppPage002{
		GivenARegisteredProjectWithNoCategory: func() {
			before = listing(t, clone)
		},
		WhenTheDeveloperPinsItAndGivesItTheCategoryToolsFromItsPage: func() {
			mustRun(t, m, "pin", "project-a", "on")
			mustRun(t, m, "category", "project-a", "tools")
		},
		ThenTheProjectIsListedAsPinnedUnderTools: func() {
			for _, p := range asList(api(t, m, "repos")["projects"]) {
				pv := p.(map[string]any)
				if pv["name"] == "project-a" {
					if pv["pinned"] != true || !strings.Contains(pv["group"].(string), "tools") {
						t.Fatalf("listed as %v", pv)
					}
					return
				}
			}
			t.Fatal("project-a is not listed")
		},
		AndNothingOnDiskIsChanged: func() {
			if after := listing(t, clone); after != before {
				t.Fatalf("the project's folder changed:\nbefore %s\nafter %s", before, after)
			}
		},
	}, nil)
}

// APP-PAGE-003: unregistering forgets the project and keeps its folder. Fails if it stays
// registered or the folder is removed.
func TestAppPage003(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)

	scenarios.Run(t, scenarios.AppPage003{
		GivenARegisteredProject: func() {
			if _, ok := api(t, m, "project", "project-a")["checkouts"]; !ok {
				t.Fatal("project-a has no page")
			}
		},
		WhenTheDeveloperUnregistersItFromItsPage: func() {
			mustRun(t, m, "unregister", "project-a")
		},
		ThenItIsNoLongerARegisteredProject: func() {
			if page := api(t, m, "project", "project-a"); page["error"] == nil {
				t.Fatalf("project-a still has a page: %v", page)
			}
		},
		AndItsFolderIsStillThere: func() {
			if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
				t.Fatalf("the folder is gone: %v", err)
			}
		},
	}, nil)
}

// APP-PAGE-004: a running session carries its links, filled from its slot. Fails if the link is
// missing, or names another slot's port.
func TestAppPage004(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var sessions []any

	scenarios.Run(t, scenarios.AppPage004{
		GivenAProjectWhoseSettingsNameTheLinkAppAsHttpsOnItsUiPort: func() {
			settingsOf(t, m, "project-a", "[ports]\nui = 21000\n[links]\napp = \"https://localhost:{{ui}}/\"\n")
			m.StartSession("project-a", clone)
		},
		WhenTheDeveloperListsTheSessions: func() {
			sessions = asList(api(t, m, "sessions")["sessions"])
		},
		ThenTheRunningSessionCarriesTheLinkAppWithItsSlotsUiPort: func() {
			for _, s := range sessions {
				sv := s.(map[string]any)
				if sv["name"] != "project-a" {
					continue
				}
				b, _ := json.Marshal(sv["links"])
				if !strings.Contains(string(b), `"name":"app","url":"https://localhost:21000/"`) {
					t.Fatalf("links %s", b)
				}
				return
			}
			t.Fatalf("project-a is not listed: %v", sessions)
		},
	}, nil)
}

// listing is every file under dir with its size, outside .git, to see that nothing changed.
func listing(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		b.WriteString(p + " " + info.ModTime().String() + "\n")
		return nil
	})
	return b.String()
}

// widgetOf is the root's repository "widget" as the app's list has it.
func widgetOf(t *testing.T, m *journey.Machine) map[string]any {
	t.Helper()
	for _, r := range asList(asMap(asList(api(t, m, "repos")["roots"])[0])["repos"]) {
		if asMap(r)["name"] == "widget" {
			return asMap(r)
		}
	}
	t.Fatal("widget is not listed")
	return nil
}

// APP-REPOS-003: a repository on GitHub, not here, is hidden and shown again. Fails if hiding does
// not mark it, or showing it again leaves it hidden.
func TestAppRepos003(t *testing.T) {
	m, gh, _ := withRoot(t)

	scenarios.Run(t, scenarios.AppRepos003{
		GivenARootWhoseOwnerHasARepositoryOnGitHubThatIsNotHere: func() {
			gh.AddRepo("owner-a", "widget")
			if widgetOf(t, m)["hidden"] != nil {
				t.Fatal("hidden before it was hidden")
			}
		},
		WhenTheDeveloperHidesItFromTheApp: func() {
			mustRun(t, m, "hide", "owners", "widget")
		},
		ThenItIsMarkedHiddenInTheList: func() {
			if widgetOf(t, m)["hidden"] != true {
				t.Fatalf("not hidden: %v", widgetOf(t, m))
			}
		},
		AndOnceTheyShowItAgainItIsNot: func() {
			mustRun(t, m, "unhide", "owners", "widget")
			if widgetOf(t, m)["hidden"] != nil {
				t.Fatalf("still hidden: %v", widgetOf(t, m))
			}
		},
	}, nil)
}

// APP-ASK-001: asking Claude about the main clone starts project-a-ai in it, beside project-a, with
// the context file naming the project, the checkout, its ports and its settings folder, the agent
// started with that file and the settings folder, and an opening question. Fails if the session
// is the main one, the context is missing any of those, or the agent is not given them.
func TestAppAsk001(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var settingsDir string

	scenarios.Run(t, scenarios.AppAsk001{
		GivenAProjectWithPortsWhoseMainCloneTheDeveloperIsLookingAtInTheApp: func() {
			settingsDir = filepath.Dir(settingsOf(t, m, "project-a", "[ports]\nui = 21000\n"))
			m.StartSession("project-a", clone)
		},
		WhenTheyAskClaudeAboutIt: func() {
			// Remote Control alone, so the plan opens no terminal window.
			mustRun(t, m, "assistant", "project-a", clone, "--mode", "remote")
		},
		ThenASessionOfItsOwnStartsInThatCheckoutBesideItsMainSession: func() {
			m.WaitSession("project-a-ai", "running")
			if journey.SessionState(m.Sessions(), "project-a") != "running" {
				t.Fatalf("the main session is not running beside it:\n%s", m.Sessions())
			}
		},
		AndClaudeIsToldTheProjectTheCheckoutItsPortsAndWhereItsSettingsAre: func() {
			out, err := m.Run("sh", "-c", `cat "$1"/.local/state/engineroom/assistant/project-a-ai.md`, "sh", m.Home)
			if err != nil {
				t.Fatalf("no context file: %v\n%s", err, out)
			}
			real, _ := filepath.EvalSymlinks(clone)
			for _, want := range []string{"Project: project-a", "main clone, " + real, "ui 21000", "Settings folder for this project: " + settingsDir} {
				if !strings.Contains(out, want) {
					t.Fatalf("the context lacks %q:\n%s", want, out)
				}
			}
		},
		AndClaudeIsAskedToSayWhatItSeesAndAskWhatToDo: func() {
			var start string
			for i := 0; i < 50 && start == ""; i++ {
				for _, s := range m.AgentStarts() {
					if strings.Contains(s, "--append-system-prompt-file") {
						start = s
					}
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !strings.Contains(start, "project-a-ai.md") || !strings.Contains(start, "--add-dir") || strings.Contains(start, "--continue") {
				t.Fatalf("the agent was started as %q (all starts %q)", start, m.AgentStarts())
			}
			// The question is the prompt Claude Code takes from those arguments, not merely among
			// them (one after --add-dir is taken as a folder).
			asked := false
			for _, p := range m.AgentPrompts() {
				asked = asked || strings.Contains(p, "ask me what I want to do")
			}
			if !asked {
				t.Fatalf("the agent was not asked what to do: prompts %q from start %q", m.AgentPrompts(), start)
			}
		},
	}, nil)
}

// APP-TASK-001: a task named in the settings runs in the main clone with the slot's ports, and the
// page lists it. Fails if it runs elsewhere, without the slot, or is not listed.
func TestAppTask001(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var session string

	scenarios.Run(t, scenarios.AppTask001{
		GivenAProjectWhoseSettingsNameATask: func() {
			settingsOf(t, m, "project-a", "[ports]\nui = 21000\n\n[[task]]\nname = \"Note the port\"\nabout = \"writes the slot's ui port\"\nrun = \"echo ui=$ENGINEROOM_PORT_UI > task.txt\"\n")
			// The main clone's slot record, as new work writes a worktree's, so the task can read it.
			if out, err := m.Run("engineroom", "slot", "claim", "--worktree", clone); err != nil {
				t.Fatalf("slot claim: %v\n%s", err, out)
			}
		},
		WhenTheDeveloperRunsThatTaskOnItsMainCloneFromTheApp: func() {
			session = asMap(asList(pageOf(t, m, "project-a")["checkouts"])[0])["session"].(string)
			mustRun(t, m, "task", session, "Note the port")
		},
		ThenTheTasksCommandRunsInThatCheckoutWithTheSlotsPorts: func() {
			b, err := os.ReadFile(filepath.Join(clone, "task.txt"))
			if err != nil || strings.TrimSpace(string(b)) != "ui=21000" {
				t.Fatalf("task.txt is %q (%v)", b, err)
			}
		},
		AndItIsListedOnTheProjectSPage: func() {
			b, _ := json.Marshal(pageOf(t, m, "project-a")["tasks"])
			if !strings.Contains(string(b), `"name":"Note the port"`) {
				t.Fatalf("tasks %s", b)
			}
		},
	}, nil)
}

// originOnGitHub makes clone's origin the GitHub repository acme/widget as the page reads it (its
// address), while anything sent with git push still goes to the run's own bare remote, so a
// branch is sent for real and git records it exactly as a push to GitHub does.
func originOnGitHub(t *testing.T, m *journey.Machine, clone, remote string) {
	t.Helper()
	m.Git(clone, "remote", "set-url", "origin", "git@github.com:acme/widget.git")
	m.Git(clone, "remote", "set-url", "--push", "origin", remote)
}

// githubOf is a page's or a checkout's GitHub links, or nil when it has none.
func githubOf(v map[string]any) map[string]any {
	g, _ := v["github"].(map[string]any)
	return g
}

// checkoutOn is the page's checkout on branch.
func checkoutOn(t *testing.T, page map[string]any, branch string) map[string]any {
	t.Helper()
	for _, c := range asList(page["checkouts"]) {
		if cv := c.(map[string]any); cv["branch"] == branch {
			return cv
		}
	}
	t.Fatalf("no checkout on %s: %v", branch, page["checkouts"])
	return nil
}

// APP-PAGE-005: a project whose origin is on GitHub links to its code, issues and pull requests.
// Fails if any of the three is missing or points anywhere but acme/widget.
func TestAppPage005(t *testing.T) {
	m, clone, remote := workProject(t)
	var page map[string]any

	scenarios.Run(t, scenarios.AppPage005{
		GivenAProjectWhoseOriginIsTheGitHubRepositoryAcmeWidget: func() {
			originOnGitHub(t, m, clone, remote)
		},
		WhenTheDeveloperOpensItsPage: func() {
			page = pageOf(t, m, "project-a")
		},
		ThenThePageLinksToTheCodeTheIssuesAndThePullRequestsOfAcmeWidgetOnGitHub: func() {
			g := githubOf(page)
			if g["repo"] != "acme/widget" || g["code"] != "https://github.com/acme/widget" ||
				g["issues"] != "https://github.com/acme/widget/issues" || g["pulls"] != "https://github.com/acme/widget/pulls" {
				t.Fatalf("github: %v", page["github"])
			}
		},
	}, nil)
}

// APP-PAGE-006: a checkout links to its branch only once GitHub has it. Fails if the pushed
// branch has no links or the wrong ones, or the branch GitHub does not have is linked.
func TestAppPage006(t *testing.T) {
	m, clone, remote := workProject(t)
	var page map[string]any

	scenarios.Run(t, scenarios.AppPage006{
		GivenAProjectOnGitHubWithACheckoutOnTheBranchFeatureXSentToGitHub: func() {
			// New work fetches from origin first, so the worktrees are made while origin is the
			// run's own remote, and only then is origin given GitHub's address.
			newWork(t, m, "feature-x")
			m.Git(clone+"-feature-x", "push", "-q", "origin", "feature-x")
		},
		AndACheckoutOnTheBranchLocalOnlyWhichGitHubDoesNotHave: func() {
			newWork(t, m, "local-only")
			originOnGitHub(t, m, clone, remote)
		},
		WhenTheDeveloperOpensTheProjectSPage: func() {
			page = pageOf(t, m, "project-a")
		},
		ThenTheCheckoutOnFeatureXLinksToThatBranchsCodeAndItsPullRequestsOnGitHub: func() {
			g := githubOf(checkoutOn(t, page, "feature-x"))
			if g["code"] != "https://github.com/acme/widget/tree/feature-x" || g["pulls"] != "https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afeature-x" {
				t.Fatalf("feature-x: %v", g)
			}
		},
		AndTheCheckoutOnLocalOnlyHasNoGitHubLinks: func() {
			if g := githubOf(checkoutOn(t, page, "local-only")); g != nil {
				t.Fatalf("local-only is linked to GitHub, which does not have it: %v", g)
			}
		},
	}, nil)
}

// APP-PAGE-007: a project with no origin has no GitHub links anywhere on its page. Fails if the
// page or a checkout offers one.
func TestAppPage007(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var page map[string]any

	scenarios.Run(t, scenarios.AppPage007{
		GivenAProjectWithNoOrigin: func() {},
		WhenTheDeveloperOpensItsPage: func() {
			page = pageOf(t, m, "project-a")
		},
		ThenNeitherThePageNorItsCheckoutLinksToGitHub: func() {
			if page["github"] != nil {
				t.Fatalf("the page links to GitHub: %v", page["github"])
			}
			for _, c := range asList(page["checkouts"]) {
				if g := githubOf(c.(map[string]any)); g != nil {
					t.Fatalf("a checkout links to GitHub: %v", g)
				}
			}
		},
	}, nil)
}

// APP-LIST-003: the panel's GitHub line. The repositories list carries the project's repository on
// GitHub, and a running session in a worktree whose branch GitHub has carries that branch's links.
// Fails if either is missing, or points anywhere but acme/widget and feature-x.
func TestAppList003(t *testing.T) {
	m, clone, remote := workProject(t)
	var sessions []any
	var repos map[string]any

	scenarios.Run(t, scenarios.AppList003{
		GivenAProjectWhoseOriginIsTheGitHubRepositoryAcmeWidget: func() {
			// Given GitHub's address below, once the worktree is made: new work fetches from
			// origin, which must be the run's own remote while it does.
		},
		AndARunningSessionInACheckoutOnTheBranchFeatureXSentToGitHub: func() {
			// New work fetches from origin first, so the worktree is made while origin is the
			// run's own remote, and only then is origin given GitHub's address.
			newWork(t, m, "feature-x")
			m.Git(clone+"-feature-x", "push", "-q", "origin", "feature-x")
			originOnGitHub(t, m, clone, remote)
		},
		WhenTheAppListsTheSessionsAndTheRepositories: func() {
			sessions = asList(api(t, m, "sessions")["sessions"])
			repos = api(t, m, "repos")
		},
		ThenTheProjectLinksToTheCodeTheIssuesAndThePullRequestsOfAcmeWidgetOnGitHub: func() {
			g := asMap(githubOf(repos)["project-a"])
			if g["repo"] != "acme/widget" || g["code"] != "https://github.com/acme/widget" ||
				g["issues"] != "https://github.com/acme/widget/issues" || g["pulls"] != "https://github.com/acme/widget/pulls" {
				t.Fatalf("github: %v", repos["github"])
			}
		},
		AndTheSessionLinksToTheBranchFeatureXAndThePullRequestsFromItOnGitHub: func() {
			s := sessionNamed(t, sessions, "project-a-feature-x")
			g := githubOf(s)
			if g["code"] != "https://github.com/acme/widget/tree/feature-x" || g["pulls"] != "https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afeature-x" {
				t.Fatalf("project-a-feature-x: %v", s)
			}
		},
	}, nil)
}
