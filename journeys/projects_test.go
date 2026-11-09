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

// registered lists the projects as the program reports them.
func registered(t *testing.T, m *journey.Machine) string {
	t.Helper()
	out, err := m.Run("engineroom", "projects")
	if err != nil {
		t.Fatalf("engineroom projects: %v\n%s", err, out)
	}
	return out
}

// run starts the program with arguments and waits for it to finish.
func run(t *testing.T, m *journey.Machine, args ...string) *termtest.Terminal {
	t.Helper()
	term := m.Start(t, args...)
	term.WaitExit()
	return term
}

// PROJ-ADD-001: a registered repository is known by its folder's name and opens by it.
func TestProjAdd001(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	var path string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjAdd001{
		GivenAGitRepositoryOnThisMachineThatIsNotRegistered: func() {
			path = m.NewRepo("project-a")
		},
		WhenTheDeveloperRegistersItsFolder: func() {
			term = run(t, m, "project", "add", path)
		},
		ThenItIsListedAmongTheRegisteredProjectsUnderItsFoldersName: func() {
			term.WaitFor("Registered")
			if !strings.Contains(registered(t, m), "project-a\t") {
				t.Fatalf("not listed:\n%s", registered(t, m))
			}
		},
		AndItCanBeOpenedByThatName: func() {
			open := m.Start(t, "open", "project-a")
			open.WaitFor("AGENT new conversation")
		},
	}, term0(&term))
}

// PROJ-ADD-002: a chosen name is used instead of the folder's.
func TestProjAdd002(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	var path string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjAdd002{
		GivenAGitRepositoryOnThisMachineThatIsNotRegistered: func() {
			path = m.NewRepo("project-a")
		},
		WhenTheDeveloperRegistersItsFolderWithTheNameWeb: func() {
			term = run(t, m, "project", "add", path, "--name", "web")
		},
		ThenItIsListedAmongTheRegisteredProjectsAsWeb: func() {
			list := registered(t, m)
			if !strings.HasPrefix(list, "web\t") || strings.Contains(list, "project-a\t") {
				t.Fatalf("listed as:\n%s", list)
			}
		},
	}, term0(&term))
}

// PROJ-ADD-003: a folder that is not a repository is refused and nothing is registered.
func TestProjAdd003(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	folder := filepath.Join(m.Home, "notes")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjAdd003{
		GivenAFolderOnThisMachineThatIsNotAGitRepository: func() {
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperRegistersIt: func() {
			term = m.Start(t, "project", "add", folder)
		},
		ThenTheyAreToldItIsNotAGitRepository: func() {
			term.WaitFor("is not a git repository")
			if code := term.WaitExit(); code == 0 {
				t.Fatal("refusal exited 0")
			}
		},
		AndNothingIsRegistered: func() {
			if strings.TrimSpace(registered(t, m)) != "" {
				t.Fatalf("something was registered:\n%s", registered(t, m))
			}
		},
	}, term0(&term))
}

// PROJ-ADD-004: registering the same repository again, even through a link, keeps one entry.
func TestProjAdd004(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	path := m.NewRepo("project-a")
	link := filepath.Join(m.Home, "link-to-a")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjAdd004{
		GivenARepositoryIsAlreadyRegistered: func() {
			run(t, m, "project", "add", path)
			if err := os.Symlink(path, link); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperRegistersTheSameFolderAgain: func() {
			term = m.Start(t, "project", "add", link)
		},
		ThenTheyAreToldItIsAlreadyRegisteredAndUnderWhichName: func() {
			term.WaitFor("Already registered as project-a")
			term.WaitExit()
		},
		AndItIsListedOnce: func() {
			if n := strings.Count(registered(t, m), "\n"); n != 1 {
				t.Fatalf("listed %d times:\n%s", n, registered(t, m))
			}
		},
	}, term0(&term))
}

// PROJ-ADD-005: registering reports the setup scripts the repository declares.
func TestProjAdd005(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	var path string
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjAdd005{
		GivenAGitRepositoryThatDeclaresACreateScriptAndATeardownScript: func() {
			path = m.NewRepo("project-a")
			for _, name := range []string{"create", "teardown"} {
				p := filepath.Join(path, ".engineroom", name)
				_ = os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
		},
		WhenTheDeveloperRegistersItsFolder: func() {
			term = run(t, m, "project", "add", path)
		},
		ThenTheyAreToldWhichSetupScriptsTheRepositoryDeclares: func() {
			term.WaitFor("It declares these setup scripts: create, teardown.")
		},
	}, term0(&term))
}

// PROJ-LIST-001: each project is listed with its name and folder.
func TestProjList001(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjList001{
		GivenTwoProjectsAreRegistered: func() {
			m.BeEngine("project-a", a, "project-b", b)
		},
		WhenTheDeveloperListsTheProjects: func() {
			term = run(t, m, "projects")
		},
		ThenEachIsListedWithItsNameAndItsFolder: func() {
			term.WaitFor("project-a")
			screen := term.Screen()
			for _, want := range []string{"project-a", a, "project-b", b} {
				if !strings.Contains(strings.ReplaceAll(screen, "\n", ""), want) {
					t.Fatalf("missing %s:\n%s", want, screen)
				}
			}
		},
	}, term0(&term))
}

// PROJ-REMOVE-001: removing a project forgets it and leaves its files alone.
func TestProjRemove001(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	var term *termtest.Terminal
	before := ""

	scenarios.Run(t, scenarios.ProjRemove001{
		GivenAProjectIsRegistered: func() {
			m.BeEngine("project-a", a)
			before, _ = m.Run("sh", "-c", `cd "$1" && git status --porcelain --ignored && find . -path ./.git -prune -o -type f -print | sort && git rev-parse HEAD`, "sh", a)
		},
		WhenTheDeveloperRemovesIt: func() {
			term = run(t, m, "project", "remove", "project-a")
		},
		ThenItIsNoLongerListedAmongTheRegisteredProjects: func() {
			term.WaitFor("Forgot project-a")
			if strings.Contains(registered(t, m), "project-a") {
				t.Fatalf("still listed:\n%s", registered(t, m))
			}
		},
		AndItsFolderAndEverythingInItAreUnchanged: func() {
			after, _ := m.Run("sh", "-c", `cd "$1" && git status --porcelain --ignored && find . -path ./.git -prune -o -type f -print | sort && git rev-parse HEAD`, "sh", a)
			if after != before || after == "" {
				t.Fatalf("the folder changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		},
	}, term0(&term))
}

// PROJ-HERE-001: `engineroom project here` answers by exit code whether a folder is in a registered
// project's main clone or one of its worktrees, printing the project's name. Fails if a
// worktree is not known as its project's, or a folder in no project is.
func TestProjHere001(t *testing.T) {
	m, clone, _ := workProject(t)
	var dir, other string
	var inWork, inOther string
	var workErr, otherErr error

	scenarios.Run(t, scenarios.ProjHere001{
		GivenARegisteredProjectWithASecondCheckoutAndAFolderThatIsInNoProject: func() {
			dir = clone + "-w1"
			m.Git(clone, "worktree", "add", "-q", "-b", "w1", dir, "origin/main")
			other = filepath.Join(m.Home, "elsewhere")
			if err := os.MkdirAll(other, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		WhenTheDeveloperAsksWhetherEachOfThatCheckoutAndTheOtherFolderIsAProjectS: func() {
			inWork, workErr = m.Run("engineroom", "project", "here", dir)
			inOther, otherErr = m.Run("engineroom", "project", "here", other)
		},
		ThenThatCheckoutIsAnsweredWithTheProjectSNameAndSuccess: func() {
			if workErr != nil || strings.TrimSpace(inWork) != "project-a" {
				t.Fatalf("worktree: %q %v", inWork, workErr)
			}
		},
		AndTheOtherFolderIsAnsweredWithFailureAndNothingPrinted: func() {
			if otherErr == nil || strings.TrimSpace(inOther) != "" {
				t.Fatalf("other folder: %q %v", inOther, otherErr)
			}
		},
	}, nil)
}
