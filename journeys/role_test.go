//go:build journeys

package journeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// keepWorkProject is workProject with a git identity, as a developer's machine has: saving work
// commits it.
func keepWorkProject(t *testing.T) (*journey.Machine, string, string) {
	t.Helper()
	m, clone, remote := workProject(t)
	m.WriteFile(filepath.Join(m.Home, ".gitconfig"), "[user]\n\tname = dev\n\temail = dev@engine.example\n", 0o644)
	// The stand-in agent of a session keeps a file in its folder, which a real agent does not;
	// the repository ignores it, so it is not taken for the developer's uncommitted work.
	commitFile(t, m, clone, ".gitignore", ".fake-agent-conversation\n")
	return m, clone, remote
}

// refusal is the app's plan for an action that is refused: its error and what it offers, by label.
func refusal(t *testing.T, m *journey.Machine, args ...string) (string, map[string][]any) {
	t.Helper()
	reply := api(t, m, append([]string{"plan"}, args...)...)
	msg, _ := reply["error"].(string)
	if msg == "" {
		t.Fatalf("engineroom api plan %v was not refused: %v", args, reply)
	}
	offers := map[string][]any{}
	for _, o := range asList(reply["offers"]) {
		om := asMap(o)
		label, _ := om["label"].(string)
		offers[label] = asList(om["action"])
	}
	return msg, offers
}

// offered is the action of the offer whose label holds want, failing without one.
func offered(t *testing.T, offers map[string][]any, want string) []string {
	t.Helper()
	for label, act := range offers {
		if strings.Contains(label, want) {
			var out []string
			for _, a := range act {
				out = append(out, a.(string))
			}
			return out
		}
	}
	t.Fatalf("nothing offered to %q: %v", want, offers)
	return nil
}

// WORK-CLEAN-008: a clean up refused for uncommitted changes names the file and offers to save
// the changes to the branch or put them aside. Fails if either offer is missing.
func TestWorkClean008(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var msg string
	var offers map[string][]any
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkClean008{
		GivenNewWorkHasAFileChangedButNotCommitted: func() {
			dir := publishWork(t, m, clone, "dirty")
			m.WriteFile(filepath.Join(dir, "draft.txt"), "draft\n", 0o644)
		},
		WhenTheDeveloperCleansItUp: func() {
			msg, offers = refusal(t, m, "clean-session", "project-a-dirty")
		},
		ThenTheyAreToldItHasUncommittedChangesNamingTheFile: func() {
			if !strings.Contains(msg, "uncommitted") || !strings.Contains(msg, "draft.txt") {
				t.Fatalf("refused with: %s", msg)
			}
		},
		AndTheyAreOfferedToSaveTheChangesToTheBranchOrToPutThemAside: func() {
			offered(t, offers, "Save them")
			offered(t, offers, "Put them aside")
		},
	}, term0(&term))
}

// WORK-CLEAN-009: saving the changes commits them on the branch and pushes it, after which the
// clean up is planned. Fails if the change is not on the remote's branch or clean up still refuses.
func TestWorkClean009(t *testing.T) {
	m, clone, remote := keepWorkProject(t)
	var term *termtest.Terminal
	var offers map[string][]any
	scenarios.Run(t, scenarios.WorkClean009{
		GivenNewWorkHasAFileChangedButNotCommitted: func() {
			dir := publishWork(t, m, clone, "dirty")
			m.WriteFile(filepath.Join(dir, "draft.txt"), "draft\n", 0o644)
			_, offers = refusal(t, m, "clean-session", "project-a-dirty")
		},
		WhenTheDeveloperSavesItsChangesToTheBranch: func() {
			mustRun(t, m, offered(t, offers, "Save them")...)
		},
		ThenTheChangeIsCommittedOnTheBranchOnThisMachineAndOnTheRemote: func() {
			if got := m.Git(remote, "show", "dirty:draft.txt"); got != "draft" {
				t.Fatalf("the remote's dirty branch has draft.txt as %q", got)
			}
		},
		AndCleaningTheWorkUpIsNoLongerRefused: func() {
			if reply := api(t, m, "plan", "clean-session", "project-a-dirty"); reply["error"] != nil {
				t.Fatalf("still refused: %v", reply["error"])
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-010: changes put aside survive the worktree's removal, in the repository's stash.
// Fails if the clean up is still refused after putting aside, or the stash does not hold the file.
func TestWorkClean010(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var dir string
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkClean010{
		GivenNewWorkHasAFileChangedButNotCommitted: func() {
			dir = publishWork(t, m, clone, "dirty")
			m.WriteFile(filepath.Join(dir, "draft.txt"), "draft\n", 0o644)
		},
		WhenTheDeveloperPutsTheChangesAsideAndCleansTheWorkUp: func() {
			_, offers := refusal(t, m, "clean-session", "project-a-dirty")
			mustRun(t, m, offered(t, offers, "Put them aside")...)
			mustRun(t, m, "clean-session", "project-a-dirty")
		},
		ThenItsCheckoutIsRemoved: func() {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the worktree is still there: %v", err)
			}
		},
		AndTheChangeCanBeBroughtBackFromTheRepositorysStash: func() {
			list := m.Git(clone, "stash", "list")
			if !strings.Contains(list, "put aside from project-a-dirty") {
				t.Fatalf("no stash for it: %q", list)
			}
			if got := m.Git(clone, "show", "stash@{0}^3:draft.txt"); got != "draft" {
				t.Fatalf("the stash holds draft.txt as %q", got)
			}
		},
	}, term0(&term))
}

// WORK-CLEAN-011: unpushed commits refuse the clean up with an offer to push them; pushed, the
// clean up is planned. Fails if there is no push offer or the clean up still refuses after it.
func TestWorkClean011(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var offers map[string][]any
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkClean011{
		GivenNewWorkHasACommitThatIsNotOnTheRemote: func() {
			dir := publishWork(t, m, clone, "ahead")
			m.WriteFile(filepath.Join(dir, "more.txt"), "more\n", 0o644)
			m.Git(dir, "add", ".")
			m.Git(dir, "commit", "-qm", "more")
		},
		WhenTheDeveloperCleansItUp: func() {
			_, offers = refusal(t, m, "clean-session", "project-a-ahead")
		},
		ThenTheyAreOfferedToPushTheCommits: func() {
			offered(t, offers, "Push")
		},
		AndOnceTheyArePushedCleaningTheWorkUpIsNoLongerRefused: func() {
			mustRun(t, m, offered(t, offers, "Push")...)
			if reply := api(t, m, "plan", "clean-session", "project-a-ahead"); reply["error"] != nil {
				t.Fatalf("still refused: %v", reply["error"])
			}
		},
	}, term0(&term))
}

// designerAt makes the designer role worktree on nav-rework through the app's action.
func designerAt(t *testing.T, m *journey.Machine, clone string) string {
	t.Helper()
	mustRun(t, m, "new-role", "project-a", "designer", "nav-rework")
	return clone + "-designer"
}

// WORK-ROLE-001: a role worktree is named for its role, on the branch given from the remote's
// main, recorded as a role, and shown on the page as the role's. Fails if the folder is named for
// the branch, the branch starts elsewhere, the role is not recorded, or the page does not say it.
func TestWorkRole001(t *testing.T) {
	m, clone, remote := keepWorkProject(t)
	var dir string
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkRole001{
		GivenAProjectIsRegistered: func() {
			// The remote's main moves on after the clone was made, so "from the remote's main"
			// is checked against a commit the clone had not fetched.
			other := filepath.Join(m.Home, "other")
			m.Git(m.Home, "clone", "-q", remote, other)
			m.WriteFile(filepath.Join(other, "later.txt"), "later\n", 0o644)
			m.Git(other, "add", ".")
			m.Git(other, "commit", "-qm", "later")
			m.Publish(other, "main")
		},
		WhenTheDeveloperMakesACheckoutForTheRoleDesignerOnTheBranchNavRework: func() {
			dir = designerAt(t, m, clone)
		},
		ThenACheckoutNamedAfterTheProjectAndDesignerExistsBesideTheMainOneOnANewBranchNavReworkFromTheRemotesMain: func() {
			if got := m.Git(dir, "branch", "--show-current"); got != "nav-rework" {
				t.Fatalf("%s is on %q", dir, got)
			}
			if m.Git(dir, "rev-parse", "HEAD") != m.Git(remote, "rev-parse", "main") {
				t.Fatal("nav-rework does not start from the remote's main")
			}
		},
		AndDesignerIsRecordedAsOneOfTheProjectSRolesInTheDevelopersSettings: func() {
			if out, err := m.Run("engineroom", "roles", "project-a"); err != nil || strings.TrimSpace(out) != "designer" {
				t.Fatalf("roles: %v %q", err, out)
			}
		},
		AndTheProjectSPageShowsTheCheckoutAsTheDesignersOnNavRework: func() {
			for _, c := range asList(api(t, m, "project", "project-a")["checkouts"]) {
				if cm := asMap(c); cm["work"] == "designer" {
					if cm["role"] != "designer" || cm["branch"] != "nav-rework" {
						t.Fatalf("the designer checkout: %v", cm)
					}
					return
				}
			}
			t.Fatal("the page has no designer checkout")
		},
	}, term0(&term))
}

// WORK-ROLE-002: a role worktree moves on to a new branch from the remote's main in the same
// folder, and its earlier branch stays on the remote. Fails if the folder changes, the new branch
// starts elsewhere, or the old branch is lost.
func TestWorkRole002(t *testing.T) {
	m, clone, remote := keepWorkProject(t)
	var dir string
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkRole002{
		GivenTheDesignerCheckoutSBranchIsPushedAndNothingInItIsUncommitted: func() {
			dir = designerAt(t, m, clone)
			m.WriteFile(filepath.Join(dir, "nav.txt"), "nav\n", 0o644)
			m.Git(dir, "add", ".")
			m.Git(dir, "commit", "-qm", "nav")
			m.Git(dir, "push", "-q", "-u", "origin", "nav-rework")
		},
		WhenTheDeveloperMovesItOnToTheBranchSearchFix: func() {
			mustRun(t, m, "next-branch", "project-a", "designer", "search-fix")
		},
		ThenTheSameCheckoutIsOnANewBranchSearchFixFromTheRemotesMain: func() {
			if got := m.Git(dir, "branch", "--show-current"); got != "search-fix" {
				t.Fatalf("%s is on %q", dir, got)
			}
			if m.Git(dir, "rev-parse", "HEAD") != m.Git(remote, "rev-parse", "main") {
				t.Fatal("search-fix does not start from the remote's main")
			}
		},
		AndTheEarlierBranchIsStillOnTheRemote: func() {
			if !remoteHas(m, remote, "nav-rework") {
				t.Fatal("nav-rework is gone from the remote")
			}
		},
	}, term0(&term))
}

// WORK-ROLE-003: moving on is refused while a change is uncommitted, naming it and offering to
// keep it, and nothing moves. Fails if the branch changes or the change is lost.
func TestWorkRole003(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var dir, msg string
	var offers map[string][]any
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkRole003{
		GivenTheDesignerCheckoutHasAFileChangedButNotCommitted: func() {
			dir = designerAt(t, m, clone)
			m.WriteFile(filepath.Join(dir, "draft.txt"), "draft\n", 0o644)
		},
		WhenTheDeveloperMovesItOnToAnotherBranch: func() {
			msg, offers = refusal(t, m, "next-branch", "project-a", "designer", "search-fix")
		},
		ThenTheyAreToldItHasUncommittedChangesNamingTheFileAndOfferedToSaveOrPutThemAside: func() {
			if !strings.Contains(msg, "draft.txt") {
				t.Fatalf("refused with: %s", msg)
			}
			offered(t, offers, "Save them")
			offered(t, offers, "Put them aside")
		},
		AndTheCheckoutIsStillOnItsBranchWithTheChange: func() {
			if got := m.Git(dir, "branch", "--show-current"); got != "nav-rework" {
				t.Fatalf("moved to %q", got)
			}
			if b, err := os.ReadFile(filepath.Join(dir, "draft.txt")); err != nil || string(b) != "draft\n" {
				t.Fatalf("the change: %v %q", err, b)
			}
		},
	}, term0(&term))
}

// WORK-ROLE-004: a role cannot take the name of existing branch work. Fails if it is accepted
// or the role is recorded.
func TestWorkRole004(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var msg string
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkRole004{
		GivenNewWorkCalledDesignerExistsOnTheProject: func() {
			publishWork(t, m, clone, "designer")
		},
		WhenTheDeveloperMakesACheckoutForTheRoleDesigner: func() {
			msg, _ = refusal(t, m, "new-role", "project-a", "designer", "nav-rework")
		},
		ThenTheyAreToldTheNameIsTakenByBranchWork: func() {
			if !strings.Contains(msg, "branch work") {
				t.Fatalf("refused with: %s", msg)
			}
		},
		AndNoRoleIsRecorded: func() {
			if out, _ := m.Run("engineroom", "roles", "project-a"); strings.TrimSpace(out) != "" {
				t.Fatalf("roles recorded: %q", out)
			}
		},
	}, term0(&term))
}

// WORK-ROLE-005: cleaning up a role worktree removes it and forgets the role. Fails if the role
// is still recorded afterwards.
func TestWorkRole005(t *testing.T) {
	m, clone, _ := keepWorkProject(t)
	var dir string
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.WorkRole005{
		GivenTheDesignerCheckoutHasNothingUncommittedOrUnpushed: func() {
			dir = designerAt(t, m, clone)
			m.Git(dir, "push", "-q", "-u", "origin", "nav-rework")
		},
		WhenTheDeveloperCleansItUpAndConfirms: func() {
			mustRun(t, m, "clean-session", "project-a-designer")
		},
		ThenItsCheckoutIsRemoved: func() {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("the worktree is still there: %v", err)
			}
		},
		AndDesignerIsNoLongerOneOfTheProjectSRoles: func() {
			if out, _ := m.Run("engineroom", "roles", "project-a"); strings.TrimSpace(out) != "" {
				t.Fatalf("roles still recorded: %q", out)
			}
		},
	}, term0(&term))
}
