//go:build journeys

package journeys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// The handover redesign (D-017), driven through `engineroom api` as the app drives it: the client's
// "Work local" handover, its offers, the mirror of what the client lacks, and the engine's release.

// engineOther is the engine's clone of project-b, a project the client does not have.
const engineOther = "/home/dev/src/project-b"

// withProjectB gives the engine a second project, project-b, that the client does not have.
func withProjectB(t *testing.T, engine *journey.Engine) {
	t.Helper()
	engine.AddGitHubRepo("owner-a", "project-b")
	engine.MustExec("git clone -q git@github.com:owner-a/project-b.git " + engineOther)
	engine.WriteConfig("role = \"engine\"\n[agent]\ncommand = \"fake-agent\"\n[[project]]\nname = \"project-a\"\npath = \"" + engineCopy + "\"\n[[project]]\nname = \"project-b\"\npath = \"" + engineOther + "\"\n")
}

// planOf is the plan an api action would run on the client, as the app reads it.
func planOf(t *testing.T, client *journey.Machine, args ...string) map[string]any {
	t.Helper()
	reply := api(t, client, append([]string{"plan"}, args...)...)
	p, ok := reply["plan"].(map[string]any)
	if !ok {
		t.Fatalf("no plan: %v", reply)
	}
	return p
}

// onGitHub is the commit GitHub has for a branch of project-a, read on the engine.
func onGitHub(engine *journey.Engine, branch string) string {
	out, _ := engine.Exec("git ls-remote git@github.com:owner-a/project-a.git refs/heads/" + branch)
	if f := strings.Fields(out); len(f) > 0 {
		return f[0]
	}
	return ""
}

// clientHome is the client's home folder as git names it.
func clientHome(client *journey.Machine) string {
	if real, err := filepath.EvalSymlinks(client.Home); err == nil {
		return real
	}
	return client.Home
}

// CONN-HANDOFF-007: the handover pushes the engine's checked-out work (its main clone's branch) and
// only names a branch in no checkout, with the command to push it. Fails if the backup reaches
// GitHub, the main clone's commit does not, or the note with the command is missing.
func TestConnHandoff007(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var backup, main string
	var notes string

	scenarios.Run(t, scenarios.ConnHandoff007{
		GivenABranchInNoCheckoutOnTheEngineHasACommitThatGitHubDoesNot: func() {
			backup = strings.TrimSpace(engine.MustExec("cd " + engineCopy + " && git switch -q -c backup && echo b >> README.md && git commit -qam backup && git rev-parse HEAD && git switch -q main"))
		},
		AndTheEnginesMainCloneHasACommitOnItsOwnBranchThatGitHubDoesNot: func() {
			main = strings.TrimSpace(engine.MustExec("cd " + engineCopy + " && echo m >> README.md && git commit -qam main-work && git rev-parse HEAD"))
		},
		WhenTheDeveloperHandsTheWorkOverFromTheEngineToTheClientAndRunsIt: func() {
			b, _ := json.Marshal(planOf(t, client, "handoff", "from-engine")["notes"])
			notes = string(b)
			mustRun(t, client, "handoff", "from-engine")
		},
		ThenTheMainClonesBranchIsOnGitHubWithItsCommit: func() {
			if got := onGitHub(engine, "main"); got != main {
				t.Fatalf("GitHub has main at %q, want %q", got, main)
			}
		},
		AndTheBranchInNoCheckoutIsNotOnGitHub: func() {
			if got := onGitHub(engine, "backup"); got != "" {
				t.Fatalf("GitHub has the backup branch at %q (its commit %s)", got, backup)
			}
		},
		AndTheyAreToldTheCommandThatWouldPushIt: func() {
			// The notes are read as JSON text, where < and > are escaped, so the line is matched
			// around the command's shape.
			if !strings.Contains(notes, "in no checkout and with commits GitHub lacks (send one with git -C") || !strings.Contains(notes, "push origin") || !strings.Contains(notes, "project-a: backup") {
				t.Fatalf("the notes do not name the backup branch with its command: %s", notes)
			}
		},
	}, nil)
}

// CONN-HANDOFF-012: a release branch checked out on the engine is never pushed by a handover, and
// the developer is told so. Fails if it reaches GitHub or the note is missing.
func TestConnHandoff012(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var notes string

	scenarios.Run(t, scenarios.ConnHandoff012{
		GivenTheEnginesMainCloneHasAReleaseBranchCheckedOutWithACommitThatGitHubDoesNot: func() {
			engine.MustExec("cd " + engineCopy + " && git switch -q -c release/1.0.0 && echo r >> README.md && git commit -qam release")
		},
		WhenTheDeveloperHandsTheWorkOverFromTheEngineToTheClientAndRunsIt: func() {
			b, _ := json.Marshal(planOf(t, client, "handoff", "from-engine")["notes"])
			notes = string(b)
			mustRun(t, client, "handoff", "from-engine")
		},
		ThenTheReleaseBranchIsNotOnGitHub: func() {
			if got := onGitHub(engine, "release/1.0.0"); got != "" {
				t.Fatalf("GitHub has the release branch at %q", got)
			}
		},
		AndTheyAreToldAHandoverNeverPushesAReleaseBranch: func() {
			if !strings.Contains(notes, "A handover never pushes a release branch") {
				t.Fatalf("the notes do not say it: %s", notes)
			}
		},
	}, nil)
}

// CONN-HANDOFF-008: uncommitted work on the engine, a changed file and a new file, is carried to the
// client by the handover, straight over SSH: the client's checkout has both, the engine's no longer
// does but keeps them in its stash list, and GitHub has no ref of them. Fails if either file does
// not arrive, the engine keeps the changes in its folder or loses its copy, or GitHub gets the work.
func TestConnHandoff008(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var refsBefore string

	scenarios.Run(t, scenarios.ConnHandoff008{
		GivenTheEnginesCheckoutOfAProjectHasAChangedFileAndANewFileNeitherCommitted: func() {
			engine.MustExec("cd " + engineCopy + " && echo unfinished >> README.md && echo draft > notes.txt")
			refsBefore = engine.MustExec("git ls-remote git@github.com:owner-a/project-a.git")
		},
		WhenTheDeveloperHandsTheWorkOverFromTheEngineToTheClientAndRunsIt: func() {
			mustRun(t, client, "handoff", "from-engine")
		},
		ThenTheClientSCheckoutHasTheChangedFileAndTheNewFileUncommitted: func() {
			out, err := client.Run("sh", "-c", `cd "$1" && git status --porcelain && cat notes.txt && tail -1 README.md`, "sh", mine)
			if err != nil || !strings.Contains(out, " M README.md") || !strings.Contains(out, "?? notes.txt") || !strings.Contains(out, "draft") || !strings.Contains(out, "unfinished") {
				t.Fatalf("the client's checkout: %v\n%s", err, out)
			}
		},
		AndTheEnginesCheckoutNoLongerHasThemAndKeepsThemInItsStashList: func() {
			if st := strings.TrimSpace(engine.MustExec("git -C " + engineCopy + " status --porcelain")); st != "" {
				t.Fatalf("the engine's checkout still has: %s", st)
			}
			if list := engine.MustExec("git -C " + engineCopy + " stash list"); !strings.Contains(list, "engineroom: handover of main to the client") {
				t.Fatalf("the engine keeps no copy: %q", list)
			}
		},
		AndGitHubHasNothingOfThem: func() {
			if after := engine.MustExec("git ls-remote git@github.com:owner-a/project-a.git"); after != refsBefore {
				t.Fatalf("GitHub's refs changed:\nbefore %s\nafter %s", refsBefore, after)
			}
		},
	}, nil)
}

// CONN-HANDOFF-014: a new file made on the client while working local goes back to the engine
// with the handover. Fails if the engine's checkout lacks it or the client's still has it.
func TestConnHandoff014(t *testing.T) {
	client, engine, mine := handoffWorld(t)

	scenarios.Run(t, scenarios.ConnHandoff014{
		GivenTheClientSCheckoutOfAProjectHasANewFileNotCommitted: func() {
			if err := os.WriteFile(filepath.Join(mine, "local.txt"), []byte("made offline\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperHandsTheWorkBackToTheEngineAndRunsIt: func() {
			mustRun(t, client, "handoff", "to-engine")
		},
		ThenTheEnginesCheckoutHasTheNewFileUncommitted: func() {
			if got := engine.MustExec("cd " + engineCopy + " && git status --porcelain && cat local.txt"); !strings.Contains(got, "?? local.txt") || !strings.Contains(got, "made offline") {
				t.Fatalf("the engine's checkout: %q", got)
			}
		},
		AndTheClientSCheckoutNoLongerHasIt: func() {
			if _, err := os.Stat(filepath.Join(mine, "local.txt")); err == nil {
				t.Fatal("the client's checkout still has local.txt")
			}
		},
	}, nil)
}

// CONN-HANDOFF-015: one checkout that cannot be put in the engine's state (the client's main has a
// commit GitHub lacks) blocks the whole handover and the switch with it: the developer is told why,
// nothing changes on either machine, and the client is not working local. Fails if the plan has
// commands, the reason is not said, either checkout or GitHub changes, or the client switched.
func TestConnHandoff015(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var plan map[string]any
	var before string
	state := func() string {
		out, _ := client.Run("sh", "-c", `cd "$1" && git rev-parse HEAD && git status --porcelain && git stash list`, "sh", mine)
		return out + "|" + engine.MustExec("cd "+engineCopy+" && git rev-parse HEAD && git status --porcelain && git stash list") + "|" + engine.GitHubHead("owner-a", "project-a", "main")
	}

	scenarios.Run(t, scenarios.ConnHandoff015{
		GivenTheEnginesCheckoutOfAProjectHasUncommittedChanges: func() {
			engine.MustExec("cd " + engineCopy + " && echo engine-work >> README.md")
		},
		AndTheClientSBranchOfThatProjectHasACommitOfItsOwnThatGitHubDoesNot: func() {
			commitOnClient(t, client, mine)
			before = state()
		},
		WhenTheDeveloperSwitchesTheClientToWorkLocal: func() {
			plan = planOf(t, client, "handoff", "from-engine", "--switch")
			mustRun(t, client, "handoff", "from-engine", "--switch")
		},
		ThenTheyAreToldTheHandoverCannotRunBecauseOfTheClientSCommit: func() {
			b, _ := json.Marshal(plan["notes"])
			if plan["blocked"] != true || len(asList(plan["commands"])) != 0 || !strings.Contains(string(b), "This handover cannot run yet") || !strings.Contains(string(b), "main on this machine has 1 commit of its own that GitHub does not") {
				t.Fatalf("plan: %v", plan)
			}
		},
		AndNothingHasChangedOnEitherMachine: func() {
			if after := state(); after != before {
				t.Fatalf("something changed:\nbefore %s\nafter  %s", before, after)
			}
		},
		AndTheClientIsNotWorkingLocal: func() {
			if out, _ := client.Run("engineroom", "offline", "status"); strings.HasPrefix(out, "offline:") {
				t.Fatalf("the client switched: %s", out)
			}
		},
	}, nil)
}

// CONN-HANDOFF-016: the client's checkout, on main with a new file of its own, ends on the engine's
// branch at its commit with its uncommitted change, its own file set aside in its stash list
// labelled main. Fails if the branch, commit or change differ, or the client's file is lost.
func TestConnHandoff016(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var commit string

	scenarios.Run(t, scenarios.ConnHandoff016{
		GivenTheEnginesCheckoutOfAProjectIsOnAnotherBranchThanMainWithAnUncommittedChange: func() {
			commit = strings.TrimSpace(engine.MustExec("cd " + engineCopy + " && git switch -q -c feature && echo f >> README.md && git commit -qam feature && git rev-parse HEAD && echo unfinished >> README.md"))
		},
		AndTheClientSCheckoutOfThatProjectIsOnMainWithANewFileOfItsOwn: func() {
			if err := os.WriteFile(filepath.Join(mine, "own.txt"), []byte("the client's own\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperHandsTheWorkOverFromTheEngineToTheClientAndRunsIt: func() {
			mustRun(t, client, "handoff", "from-engine")
		},
		ThenTheClientSCheckoutIsOnTheEnginesBranchAtItsCommitWithItsChange: func() {
			out, err := client.Run("sh", "-c", `cd "$1" && git branch --show-current && git rev-parse HEAD && git status --porcelain && tail -1 README.md`, "sh", mine)
			if err != nil || out != "feature\n"+commit+"\n M README.md\nunfinished\n" {
				t.Fatalf("the client's checkout: %v\n%q", err, out)
			}
		},
		AndTheClientSNewFileIsInItsStashListLabelledMain: func() {
			out, _ := client.Run("sh", "-c", `cd "$1" && git stash list && git stash show --include-untracked --name-only stash@{0}`, "sh", mine)
			if !strings.Contains(out, "engineroom: work on main set aside before the handover from the engine") || !strings.Contains(out, "own.txt") {
				t.Fatalf("the client's stash list: %q", out)
			}
		},
	}, nil)
}

// CONN-HANDOFF-017: a handover shown in the terminal and then run after the engine's checkout was
// edited stops at its first step, the check, and changes nothing. Fails if it runs on, or GitHub
// or either checkout changes.
func TestConnHandoff017(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var term *termtest.Terminal
	var before string
	state := func() string {
		out, _ := client.Run("sh", "-c", `cd "$1" && git rev-parse HEAD && git status --porcelain`, "sh", mine)
		return out + "|" + engine.MustExec("cd "+engineCopy+" && git rev-parse HEAD && git stash list") + "|" + engine.GitHubHead("owner-a", "project-a", "main")
	}

	scenarios.Run(t, scenarios.ConnHandoff017{
		GivenTheDeveloperIsShownAHandoverFromTheEngineToTheClientWithACommitToPush: func() {
			engine.MustExec("cd " + engineCopy + " && echo engine-work >> README.md && git commit -qam engine-work")
			term = client.Start(t, "handoff", "from-engine")
			term.WaitFor("Nothing has run yet.")
		},
		WhenTheEnginesCheckoutIsEditedBeforeTheDeveloperRunsIt: func() {
			engine.MustExec("cd " + engineCopy + " && echo later >> README.md")
			before = state()
			term.Press(termtest.Enter)
		},
		ThenTheHandoverStopsAtItsFirstStep: func() {
			term.WaitFor("has changed since the handover was planned")
		},
		AndGitHubAndBothCheckoutsAreAsTheyWere: func() {
			if after := state(); after != before {
				t.Fatalf("something changed:\nbefore %s\nafter  %s", before, after)
			}
		},
	}, term0(&term))
}

// CONN-HANDOFF-009: a project the client lacks is copied to the same place under its home and
// registered. Fails if it lands elsewhere or is not registered.
func TestConnHandoff009(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var dest string

	scenarios.Run(t, scenarios.ConnHandoff009{
		GivenTheEngineHasAProjectTheClientDoesNot: func() {
			withProjectB(t, engine)
		},
		WhenTheDeveloperHandsTheWorkOverFromTheEngineToTheClientAndRunsIt: func() {
			mustRun(t, client, "handoff", "from-engine")
		},
		ThenTheClientHasThatProjectAtTheSamePlaceUnderItsHomeAsOnTheEngine: func() {
			dest = filepath.Join(clientHome(client), "src", "project-b")
			if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
				t.Fatalf("project-b is not at %s: %v", dest, err)
			}
		},
		AndItIsRegisteredOnTheClientUnderTheSameName: func() {
			out, _ := client.Run("engineroom", "project", "list")
			if !strings.Contains(out, "project-b\t"+dest) {
				t.Fatalf("not registered at %s:\n%s", dest, out)
			}
		},
	}, nil)
}

// CONN-HANDOFF-010: releasing the engine, from the app on the engine, pushes its checked-out work and is shown.
// Fails if the branch is left behind or the engine's status does not say when.
func TestConnHandoff010(t *testing.T) {
	_, engine, _ := handoffWorld(t)
	var commit string

	scenarios.Run(t, scenarios.ConnHandoff010{
		GivenTheEnginesMainCloneHasACommitOnItsOwnBranchThatGitHubDoesNot: func() {
			commit = strings.TrimSpace(engine.MustExec("cd " + engineCopy + " && echo s >> README.md && git commit -qam side && git rev-parse HEAD"))
		},
		WhenTheDeveloperReleasesTheEngineFromTheAppOnTheEngine: func() {
			out := engine.MustExec("engineroom api run handoff release")
			if !strings.Contains(out, `"ok":true`) {
				t.Fatalf("the release did not finish:\n%s", out)
			}
		},
		ThenThatBranchIsOnGitHubWithItsCommit: func() {
			if got := onGitHub(engine, "main"); got != commit {
				t.Fatalf("GitHub has main at %q, want %q", got, commit)
			}
		},
		AndTheEngineShowsWhenItWasReleased: func() {
			if out := engine.MustExec("engineroom api status"); !strings.Contains(out, `"released"`) {
				t.Fatalf("the engine's status does not say when it was released:\n%s", out)
			}
		},
	}, nil)
}

// CONN-HANDOFF-011: keeping the client ready copies the project it lacks and moves no worktree.
// Fails if the project is not copied, or the client's worktree is moved to GitHub's commit.
func TestConnHandoff011(t *testing.T) {
	client, engine, mine := handoffWorld(t)
	var before string

	scenarios.Run(t, scenarios.ConnHandoff011{
		GivenTheEngineHasAProjectTheClientDoesNot: func() {
			withProjectB(t, engine)
		},
		AndGitHubHasACommitTheClientSCheckoutOfAnotherProjectDoesNot: func() {
			engine.MustExec("cd " + engineCopy + " && echo pushed >> README.md && git commit -qam pushed && git push -q origin main")
			out, _ := client.Run("git", "-C", mine, "rev-parse", "HEAD")
			before = strings.TrimSpace(out)
		},
		WhenTheClientKeepsItselfReadyToWorkLocal: func() {
			mustRun(t, client, "mirror")
		},
		ThenTheMissingProjectIsCopiedToTheClientAndRegistered: func() {
			dest := filepath.Join(clientHome(client), "src", "project-b")
			out, _ := client.Run("engineroom", "project", "list")
			if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil || !strings.Contains(out, "project-b") {
				t.Fatalf("project-b not copied and registered: %v\n%s", err, out)
			}
		},
		AndTheClientSCheckoutIsNotMoved: func() {
			out, _ := client.Run("git", "-C", mine, "rev-parse", "HEAD")
			if strings.TrimSpace(out) != before {
				t.Fatalf("the client's worktree moved from %s to %s", before, out)
			}
		},
	}, nil)
}

// CONN-OFFLINE-004: showing the handover changes nothing; running it switches. Fails if the switch
// happens on showing, or not on running.
func TestConnOffline004(t *testing.T) {
	client, _, _ := handoffWorld(t)

	scenarios.Run(t, scenarios.ConnOffline004{
		GivenAClientWorkingOnTheEngine: func() {
			if api(t, client, "status")["offline"] != false {
				t.Fatal("the client is not working on the engine to begin with")
			}
		},
		WhenTheDeveloperAsksTheAppToWorkLocalAndIsShownTheHandover: func() {
			planOf(t, client, "handoff", "from-engine", "--switch")
		},
		ThenTheClientIsStillWorkingOnTheEngine: func() {
			if api(t, client, "status")["offline"] != false {
				t.Fatal("showing the handover switched the client to work local")
			}
		},
		AndOnceTheyRunTheHandoverItWorksLocal: func() {
			mustRun(t, client, "handoff", "from-engine", "--switch")
			if api(t, client, "status")["offline"] != true {
				t.Fatal("running the handover did not switch the client to work local")
			}
		},
	}, nil)
}

// CONN-HANDOFF-013: asking for the handover's plan, as the app does, says on standard error how far
// each machine's reading has got: the engine's project by project up to its total with the command
// doing it, then this machine's; the plan itself follows on standard output. Fails if the engine's
// count does not reach two of two, its command is missing, this machine is not reported, or the
// plan is spoiled by the progress lines.
func TestConnHandoff013(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var stdout, stderr strings.Builder

	scenarios.Run(t, scenarios.ConnHandoff013{
		GivenTheEngineHasTwoProjectsRegistered: func() {
			withProjectB(t, engine)
		},
		WhenTheDeveloperAsksForTheHandoverFromTheEngineToTheClient: func() {
			cmd := client.Command(client.Engineroom, "api", "plan", "handoff", "from-engine")
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("engineroom api plan handoff from-engine: %v\n%s\n%s", err, stdout.String(), stderr.String())
			}
		},
		ThenTheyAreToldTheEnginesReadingProjectByProjectUpToTwoOfTwoWithTheCommandDoingIt: func() {
			var counts []int
			for _, s := range progressLines(t, stderr.String()) {
				if s["side"] != "the engine" {
					continue
				}
				if c, _ := s["command"].(string); !strings.Contains(c, "engineroom handoff state --json") {
					t.Fatalf("an engine step without its command: %v", s)
				}
				if total, _ := s["total"].(float64); total == 2 {
					counts = append(counts, int(s["done"].(float64)))
				}
			}
			if len(counts) == 0 || counts[len(counts)-1] != 2 {
				t.Fatalf("the engine's reading did not count up to two of two: %v\n%s", counts, stderr.String())
			}
		},
		AndTheyAreToldThisMachinesReadingToo: func() {
			for _, s := range progressLines(t, stderr.String()) {
				if s["side"] == "this machine" {
					return
				}
			}
			t.Fatalf("no step for this machine:\n%s", stderr.String())
		},
		AndTheHandoversCommandsFollow: func() {
			var reply struct {
				Plan struct {
					Title string `json:"title"`
				} `json:"plan"`
			}
			if err := json.Unmarshal([]byte(stdout.String()), &reply); err != nil || !strings.Contains(reply.Plan.Title, "Hand over") {
				t.Fatalf("no plan on standard output (%v):\n%s", err, stdout.String())
			}
		},
	}, nil)
}

// progressLines is each {"progress": step} line of a program's standard error, as maps.
func progressLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(text, "\n") {
		var l struct {
			Progress map[string]any `json:"progress"`
		}
		if json.Unmarshal([]byte(line), &l) == nil && l.Progress != nil {
			out = append(out, l.Progress)
		}
	}
	return out
}

// CONN-HANDOFF-018: the engine has a commit on GitHub the client never took; handing back would put
// the engine on the client's older commit, so the handover blocks, says why, and leaves the engine
// where it is. Fails if it plans commands, does not say why, or the engine's checkout moves.
func TestConnHandoff018(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var plan map[string]any
	var commit string

	scenarios.Run(t, scenarios.ConnHandoff018{
		GivenACommitWasMadeInTheEnginesCheckoutOfAProjectAndPushedToGitHub: func() {
			commit = strings.TrimSpace(engine.MustExec("cd " + engineCopy + " && echo newer >> README.md && git commit -qam newer && git push -q origin main && git rev-parse HEAD"))
		},
		AndTheClientSCheckoutOfThatProjectHasNotTakenIt: func() {},
		WhenTheDeveloperHandsTheWorkBackToTheEngine: func() {
			plan = planOf(t, client, "handoff", "to-engine")
			mustRun(t, client, "handoff", "to-engine")
		},
		ThenTheyAreToldTheHandoverCannotRunBecauseItWouldMoveTheEngineBack: func() {
			b, _ := json.Marshal(plan["notes"])
			if plan["blocked"] != true || len(asList(plan["commands"])) != 0 || !strings.Contains(string(b), "so taking this machine's state would move the engine back") {
				t.Fatalf("plan: %v", plan)
			}
		},
		AndTheEnginesCheckoutIsStillAtItsCommit: func() {
			if got := engineHead(engine); got != commit {
				t.Fatalf("the engine's checkout is at %s, want %s", got, commit)
			}
		},
	}, nil)
}

// CONN-HANDOFF-019: the plan's notes give project-a one line, saying its commit is sent to GitHub
// and its change carried, and fold the branch in no checkout into the line of what is left. Fails
// if project-a's work is spread over several lines, or the branch has a line of its own.
func TestConnHandoff019(t *testing.T) {
	client, engine, _ := handoffWorld(t)
	var notes []string

	scenarios.Run(t, scenarios.ConnHandoff019{
		GivenTheEnginesCheckoutOfAProjectHasACommitGitHubDoesNotAndAnUncommittedChange: func() {
			engine.MustExec("cd " + engineCopy + " && echo work >> README.md && git commit -qam work && echo unfinished >> README.md")
		},
		AndABranchInNoCheckoutThereHasACommitGitHubDoesNot: func() {
			engine.MustExec("cd " + engineCopy + " && git branch backup && git log -1 --format=%H backup")
		},
		WhenTheDeveloperAsksForTheHandoverFromTheEngineToTheClient: func() {
			for _, n := range asList(planOf(t, client, "handoff", "from-engine")["notes"]) {
				notes = append(notes, n.(string))
			}
		},
		ThenOneLineForTheProjectSaysTheCommitIsSentToGitHubAndTheChangeCarried: func() {
			var lines []string
			for _, n := range notes {
				if strings.HasPrefix(n, "project-a:") {
					lines = append(lines, n)
				}
			}
			if len(lines) != 1 || !strings.Contains(lines[0], "1 commit of main sent to GitHub from the engine") || !strings.Contains(lines[0], "1 change (M README.md) carried from the engine") {
				t.Fatalf("project-a's lines: %q (all notes %q)", lines, notes)
			}
		},
		AndTheBranchInNoCheckoutIsNamedInOneLineOfWhatIsLeftAsItIs: func() {
			var left []string
			for _, n := range notes {
				if strings.Contains(n, "backup") {
					left = append(left, n)
				}
			}
			if len(left) != 1 || !strings.HasPrefix(left[0], "Left as they are on the engine, in no checkout") || !strings.Contains(left[0], "project-a: backup") {
				t.Fatalf("the branch in no checkout: %q (all notes %q)", left, notes)
			}
		},
	}, nil)
}
