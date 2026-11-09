//go:build journeys

package journeys

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// rootDir makes the folder a root is registered for, under the machine's home.
func rootDir(t *testing.T, m *journey.Machine, name string) string {
	t.Helper()
	dir := filepath.Join(m.Home, "roots", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withRoot is an engine with the GitHub stand-in, account-a signed in, and the root "owners" for
// owner-a listed with account-a.
func withRoot(t *testing.T) (*journey.Machine, *journey.GitHub, string) {
	t.Helper()
	m := journey.NewMachine(t)
	m.BeEngine()
	gh := m.FakeGitHub("account-a")
	dir := rootDir(t, m, "owners")
	if out, err := m.Run("engineroom", "root", "add", dir, "--owner", "owner-a", "--account", "account-a"); err != nil {
		t.Fatalf("engineroom root add: %v\n%s", err, out)
	}
	return m, gh, dir
}

// browseRoot opens the interface and goes to the root's repositories: the projects list (there
// are no sessions), g for GitHub, enter on the only root.
func browseRoot(t *testing.T, m *journey.Machine) *termtest.Terminal {
	t.Helper()
	term := m.Start(t)
	term.WaitFor("from GitHub")
	term.Type("g")
	term.WaitFor("enter browse its repositories") // shown once the roots are read
	term.Press(termtest.Enter)
	term.WaitFor("Repositories of owner-a on GitHub")
	return term
}

// PROJ-ROOT-001: a registered root is listed with its folder, owner and account. Fails if any
// of the three is lost or the root is not saved.
func TestProjRoot001(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	var dir string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjRoot001{
		GivenAFolderOnTheEngineMeantForOneGitHubOwnersRepositories: func() {
			dir = rootDir(t, m, "owners")
		},
		WhenTheDeveloperRegistersItAsARootForThatOwnerWithTheGitHubAccountThatListsIt: func() {
			term = run(t, m, "root", "add", dir, "--owner", "owner-a", "--account", "account-a")
			term.WaitFor("Registered the root owners")
		},
		ThenItIsListedAmongTheRootsWithItsFolderItsOwnerAndItsAccount: func() {
			out, err := m.Run("engineroom", "roots")
			if err != nil {
				t.Fatalf("engineroom roots: %v\n%s", err, out)
			}
			resolved, _ := filepath.EvalSymlinks(dir)
			if !strings.Contains(out, "owners\t"+resolved+"\towner owner-a\taccount account-a") {
				t.Fatalf("listed as:\n%s", out)
			}
		},
	}, term0(&term))
}

// PROJ-ROOT-002: a path with no folder is refused and nothing is registered. Fails if the root
// is saved anyway, or the refusal exits 0.
func TestProjRoot002(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	missing := filepath.Join(m.Home, "roots", "nowhere")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjRoot002{
		GivenThereIsNoFolderAtThePathTheDeveloperNames: func() {
			if _, err := os.Stat(missing); err == nil {
				t.Fatal("the folder exists")
			}
		},
		WhenTheDeveloperRegistersThatPathAsARoot: func() {
			term = m.Start(t, "root", "add", missing, "--owner", "owner-a")
		},
		ThenTheyAreToldTheFolderDoesNotExist: func() {
			term.WaitFor("does not exist")
			if code := term.WaitExit(); code == 0 {
				t.Fatal("the refusal exited 0")
			}
		},
		AndNoRootIsRegistered: func() {
			if out, _ := m.Run("engineroom", "roots"); strings.TrimSpace(out) != "" {
				t.Fatalf("a root was registered:\n%s", out)
			}
		},
	}, term0(&term))
}

// PROJ-BROWSE-001: the owner's repositories are listed from GitHub, as the root's account, and
// the one cloned and registered is marked with its project name. Fails if a repository is
// missing, the mark is missing or on the wrong one, or the listing used another account.
func TestProjBrowse001(t *testing.T) {
	m, gh, dir := withRoot(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjBrowse001{
		GivenARootWhoseOwnerHasThreeRepositoriesOnGitHub: func() {
			// "Widget" is capitalised so an order that minds case (capitals first) is told apart
			// from one that does not; the stand-in lists them in an order of its own.
			for _, name := range []string{"Widget", "gadget", "sprocket"} {
				gh.AddRepo("owner-a", name)
			}
		},
		AndOneOfThemIsClonedInTheRootAndRegisteredAsAProject: func() {
			clone := filepath.Join(dir, "sprocket")
			if out, err := m.Run("git", "clone", "-q", "git@github.com:owner-a/sprocket.git", clone); err != nil {
				t.Fatalf("clone: %v\n%s", err, out)
			}
			run(t, m, "project", "add", clone, "--name", "cogs")
		},
		WhenTheDeveloperBrowsesTheRoot: func() {
			term = browseRoot(t, m)
		},
		ThenAllThreeRepositoriesAreListed: func() {
			term.WaitFor("sprocket")
			for _, name := range []string{"Widget", "gadget", "sprocket"} {
				if lineWith(term.Screen(), name) == "" {
					t.Fatalf("%s is not listed:\n%s", name, term.Screen())
				}
			}
			if !strings.Contains(gh.Log(), "token-account-a repo list owner-a") {
				t.Fatalf("not listed as the root's account:\n%s", gh.Log())
			}
		},
		AndTheyAreListedInAlphabeticalOrderOfTheirNamesIgnoringCase: func() {
			lines := screenLines(term)
			g, s, w := lineOfContaining(lines, "gadget"), lineOfContaining(lines, "sprocket"), lineOfContaining(lines, "Widget")
			if !(g >= 0 && g < s && s < w) {
				t.Fatalf("not listed gadget, sprocket, Widget (lines %d, %d, %d):\n%s", g, s, w, term.Screen())
			}
		},
		AndTheOneAlreadyHereIsMarkedAsRegisteredUnderItsProjectName: func() {
			if !strings.Contains(lineWith(term.Screen(), "sprocket"), "registered as cogs") {
				t.Fatalf("sprocket is not marked:\n%s", term.Screen())
			}
			for _, name := range []string{"Widget", "gadget"} {
				if strings.Contains(lineWith(term.Screen(), name), "registered") {
					t.Fatalf("%s is marked though it is not here:\n%s", name, term.Screen())
				}
			}
		},
	}, term0(&term))
}

// PROJ-BROWSE-002: an account with no sign-in gives the reason and the command that signs it in.
// Fails if the failure is swallowed, shown without its reason, or without the fix.
func TestProjBrowse002(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	m.FakeGitHub("account-a")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjBrowse002{
		GivenARootWhoseGitHubAccountIsNotSignedInOnTheEngine: func() {
			dir := rootDir(t, m, "owners")
			if out, err := m.Run("engineroom", "root", "add", dir, "--owner", "owner-a", "--account", "account-b"); err != nil {
				t.Fatalf("engineroom root add: %v\n%s", err, out)
			}
		},
		WhenTheDeveloperBrowsesTheRoot: func() {
			term = browseRoot(t, m)
		},
		ThenTheyAreToldTheOwnersRepositoriesCouldNotBeListedAndWhy: func() {
			term.WaitFor("could not be listed")
			term.WaitFor("not signed in to the account account-b")
		},
		AndTheyAreShownTheCommandThatSignsTheAccountIn: func() {
			term.WaitFor("gh auth login --hostname github.com")
		},
	}, term0(&term))
}

// PROJ-CLONE-001: choosing a listed repository and a category clones it into the root under its
// own name and registers it in that category. Fails if it is cloned elsewhere, not cloned from
// the owner's repository, or registered without the category.
func TestProjClone001(t *testing.T) {
	m, gh, dir := withRoot(t)
	var term *termtest.Terminal
	clone := filepath.Join(dir, "widget")

	scenarios.Run(t, scenarios.ProjClone001{
		GivenARootWhoseOwnerHasARepositoryCalledWidgetThatIsNotOnTheEngine: func() {
			gh.AddRepo("owner-a", "gadget")
			gh.AddRepo("owner-a", "widget")
		},
		WhenTheDeveloperAddsWidgetFromTheListInTheCategoryTools: func() {
			term = browseRoot(t, m)
			term.WaitFor("widget")
			term.Type("widg")
			term.WaitGone("gadget")
			term.Press(termtest.Enter)
			term.WaitFor("Add owner-a/widget")
			term.Type("tools")
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenItIsClonedIntoTheRootAsWidget: func() {
			out, err := exec.Command("git", "-C", clone, "log", "--format=%H", "-1").Output()
			if err != nil || strings.TrimSpace(string(out)) != gh.Head("owner-a", "widget") {
				t.Fatalf("%s is not a clone of owner-a/widget: %v %s", clone, err, out)
			}
		},
		AndItIsRegisteredAsAProjectCalledWidgetInTheCategoryTools: func() {
			resolved, _ := filepath.EvalSymlinks(clone)
			if list := registered(t, m); !strings.Contains(list, "widget\t"+resolved+"\ttools") {
				t.Fatalf("registered as:\n%s", list)
			}
		},
	}, term0(&term))
}

// PROJ-CLONE-002: a folder already in the root under the repository's name is never cloned
// over; the developer is told so with the command that registers it. Fails if the folder's
// contents change or a clone is attempted.
func TestProjClone002(t *testing.T) {
	m, gh, dir := withRoot(t)
	var term *termtest.Terminal
	folder := filepath.Join(dir, "widget")
	note := filepath.Join(folder, "notes.txt")

	scenarios.Run(t, scenarios.ProjClone002{
		GivenAFolderCalledWidgetIsAlreadyInTheRoot: func() {
			gh.AddRepo("owner-a", "widget")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(note, []byte("mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperAddsTheRepositoryWidgetFromTheList: func() {
			term = browseRoot(t, m)
			term.WaitFor("folder here, not registered")
			term.Press(termtest.Enter)
		},
		ThenTheyAreToldTheFolderIsAlreadyThereAndHowToRegisterIt: func() {
			term.WaitFor("is already there and is not registered, so nothing was cloned")
			term.WaitFor("register it with: engineroom project add")
		},
		AndNothingIsClonedAndTheFolderIsUnchanged: func() {
			entries, _ := os.ReadDir(folder)
			b, _ := os.ReadFile(note)
			if len(entries) != 1 || string(b) != "mine\n" {
				t.Fatalf("the folder changed: %d entries, notes %q", len(entries), b)
			}
			if list := registered(t, m); strings.TrimSpace(list) != "" {
				t.Fatalf("something was registered:\n%s", list)
			}
		},
	}, term0(&term))
}

// PROJ-CREATE-001: a new repository is made in the root with a first commit, created private on
// GitHub as the root's account holding that commit, and registered in its category. Fails if
// any of the three is missing, the visibility is wrong, or GitHub holds a different commit.
func TestProjCreate001(t *testing.T) {
	m, gh, dir := withRoot(t)
	var term *termtest.Terminal
	folder := filepath.Join(dir, "gadget")

	scenarios.Run(t, scenarios.ProjCreate001{
		GivenTheOwnerOfTheRootHasNoRepositoryCalledGadget: func() {
			if gh.Has("owner-a", "gadget") {
				t.Fatal("the owner already has gadget")
			}
		},
		WhenTheDeveloperCreatesGadgetInTheRootAsAPrivateRepositoryInTheCategoryTools: func() {
			term = m.Start(t, "create", "owners", "gadget", "--category", "tools")
			// Creating publishes on GitHub, so the commands wait for enter.
			term.WaitFor("Nothing has run yet")
			term.WaitFor("--private")
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenItsFolderIsMadeInTheRootWithAFirstCommit: func() {
			if out, err := exec.Command("git", "-C", folder, "rev-parse", "HEAD").Output(); err != nil || len(strings.TrimSpace(string(out))) != 40 {
				t.Fatalf("%s has no first commit: %v %s", folder, err, out)
			}
		},
		AndAPrivateRepositoryGadgetIsCreatedForTheOwnerOnGitHubHoldingThatCommit: func() {
			local, _ := exec.Command("git", "-C", folder, "rev-parse", "HEAD").Output()
			if gh.Visibility("owner-a", "gadget") != "PRIVATE" || gh.Head("owner-a", "gadget") != strings.TrimSpace(string(local)) {
				t.Fatalf("on GitHub: visibility %q head %q, local %s", gh.Visibility("owner-a", "gadget"), gh.Head("owner-a", "gadget"), local)
			}
			if !strings.Contains(gh.Log(), "token-account-a repo create owner-a/gadget") {
				t.Fatalf("not created as the root's account:\n%s", gh.Log())
			}
		},
		AndItIsRegisteredAsAProjectCalledGadgetInTheCategoryTools: func() {
			resolved, _ := filepath.EvalSymlinks(folder)
			if list := registered(t, m); !strings.Contains(list, "gadget\t"+resolved+"\ttools") {
				t.Fatalf("registered as:\n%s", list)
			}
		},
	}, term0(&term))
}

// PROJ-CREATE-002: a name the owner already has is refused before any folder or repository is
// made. Fails if a folder appears, a create reaches GitHub, or the refusal does not say why.
func TestProjCreate002(t *testing.T) {
	m, gh, dir := withRoot(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjCreate002{
		GivenTheOwnerOfTheRootAlreadyHasARepositoryCalledWidgetOnGitHub: func() {
			gh.AddRepo("owner-a", "widget")
		},
		WhenTheDeveloperCreatesWidgetInTheRoot: func() {
			term = m.Start(t, "create", "owners", "widget")
		},
		ThenTheyAreToldTheOwnerAlreadyHasARepositoryOfThatName: func() {
			term.WaitFor("owner-a already has a repository called widget")
		},
		AndNoFolderIsMadeAndNothingIsCreatedOnGitHub: func() {
			if _, err := os.Stat(filepath.Join(dir, "widget")); err == nil {
				t.Fatal("a folder was made")
			}
			if strings.Contains(gh.Log(), "repo create") {
				t.Fatalf("a create reached GitHub:\n%s", gh.Log())
			}
		},
	}, term0(&term))
}

// PROJ-CLONE-001 from a client: the same choice made on a client reads the engine's roots and the
// engine's GitHub listing over SSH, and clones and registers on the engine. Fails if anything is read
// or cloned on the client instead, or the clone on the engine is not registered there.
func TestProjClone001FromClient(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjClone001{
		GivenARootWhoseOwnerHasARepositoryCalledWidgetThatIsNotOnTheEngine: func() {
			engine.BeEngine()
			engine.FakeGitHub("account-a")
			engine.AddGitHubRepo("owner-a", "widget")
			engine.MustExec("mkdir -p /home/dev/roots/owners && engineroom root add /home/dev/roots/owners --owner owner-a --account account-a")
		},
		WhenTheDeveloperAddsWidgetFromTheListInTheCategoryTools: func() {
			term = browseRoot(t, client)
			term.WaitFor("widget")
			term.Press(termtest.Enter)
			term.WaitFor("Add owner-a/widget")
			term.Type("tools")
			term.Press(termtest.Enter)
			term.WaitFor("Done.")
		},
		ThenItIsClonedIntoTheRootAsWidget: func() {
			engine.MustExec("test \"$(git -C /home/dev/roots/owners/widget rev-parse HEAD)\" = \"$(git -C /home/dev/github/owner-a/widget.git rev-parse main)\"")
			if _, err := os.Stat(filepath.Join(client.Home, "roots")); err == nil {
				t.Fatal("something was made on the client")
			}
		},
		AndItIsRegisteredAsAProjectCalledWidgetInTheCategoryTools: func() {
			if out := engine.MustExec("engineroom projects"); !strings.Contains(out, "widget\t/home/dev/roots/owners/widget\ttools") {
				t.Fatalf("registered on the engine as:\n%s", out)
			}
		},
	}, term0(&term))
}
