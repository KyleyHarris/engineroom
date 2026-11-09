//go:build journeys

package journeys

import (
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/shell"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// editedFolder is the one folder the editor stand-in was started on, with no remote, failing
// if it was started any other number of times or with other arguments.
func editedFolder(t *testing.T, m *journey.Machine) string {
	t.Helper()
	starts := m.EditorStarts()
	if len(starts) != 1 {
		t.Fatalf("the editor was started %d times: %q", len(starts), starts)
	}
	if strings.Contains(starts[0], "--remote") {
		t.Fatalf("the editor was given a remote for a folder on this machine: %q", starts[0])
	}
	return starts[0]
}

// editFromList opens the session list, selects the only session and asks for the editor.
func editFromList(t *testing.T, m *journey.Machine, name string) *termtest.Terminal {
	t.Helper()
	term := m.Start(t)
	term.WaitFor(name)
	term.Type("e")
	return term
}

// EDIT-OPEN-001: on the engine, the editor opens the session's worktree, and the command is shown.
// Fails if the editor is not started, is started on another folder, or the command is hidden.
func TestEditOpen001(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.EditOpen001{
		GivenASessionIsRunningInACheckoutOnTheEngine: func() {
			m.StartSession("project-a", clone)
		},
		WhenTheDeveloperOpensItInEditorFromTheSessionList: func() {
			term = editFromList(t, m, "project-a")
			term.WaitFor("Done.")
		},
		ThenEditorIsStartedOnThatCheckoutSFolder: func() {
			if got := editedFolder(t, m); resolve(got) != resolve(clone) {
				t.Fatalf("the editor was started on %q, not %q", got, clone)
			}
		},
		AndTheCommandThatStartedItIsShown: func() {
			term.WaitFor("fake-editor")
			out, _ := m.Run("engineroom", "history")
			if want := shell.Join([]string{"fake-editor", clone}); !strings.Contains(out, want) {
				t.Fatalf("the history does not hold %q:\n%s", want, out)
			}
		},
	}, term0(&term))
}

// EDIT-OPEN-002: on a client, the editor here opens the engine's folder over SSH, and nothing runs
// on the engine. Fails if the remote or the engine's folder is missing from the editor's arguments,
// or if anything is started on the engine.
func TestEditOpen002(t *testing.T) {
	client, engine := clientAndEngine(t)
	var term *termtest.Terminal
	var before string

	scenarios.Run(t, scenarios.EditOpen002{
		GivenTheDeveloperIsOnAClient: func() {},
		AndASessionIsRunningInACheckoutOnTheEngine: func() {
			engine.StartSession("project-a", engineProject)
			before = engine.Sessions()
		},
		WhenTheDeveloperOpensItInEditor: func() {
			term = client.Start(t, "edit", "project-a")
			term.WaitFor("Done.")
		},
		ThenEditorOnTheClientIsStartedOnTheEngineSFolderForThatCheckoutReachedOverSSH: func() {
			starts := client.EditorStarts()
			if want := "--remote ssh-remote+engine " + engineProject; len(starts) != 1 || starts[0] != want {
				t.Fatalf("the editor was started with %q, want one start with %q", starts, want)
			}
			engine.MustExec("test -d " + engineProject)
		},
		AndNothingIsStartedOnTheEngine: func() {
			if countSessions(engine.Sessions()) != countSessions(before) || journey.SessionState(engine.Sessions(), "project-a") != "running" {
				t.Fatalf("the engine's sessions changed:\nbefore:\n%s\nafter:\n%s", before, engine.Sessions())
			}
			if out, _ := engine.Exec("engineroom history"); strings.Contains(out, "editor") {
				t.Fatalf("an action ran on the engine:\n%s", out)
			}
		},
	}, term0(&term))
}

// EDIT-OPEN-003: an ended session's worktree opens, and the session is not brought back. Fails
// if the editor refuses an ended session or the session is resurrected.
func TestEditOpen003(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.EditOpen003{
		GivenASessionInACheckoutOnTheEngineHasEnded: func() {
			endedByRestart(t, m, clone)
		},
		WhenTheDeveloperOpensItInEditorFromTheSessionList: func() {
			term = editFromList(t, m, "project-a")
			term.WaitFor("Done.")
		},
		ThenEditorIsStartedOnThatCheckoutSFolder: func() {
			if got := editedFolder(t, m); resolve(got) != resolve(clone) {
				t.Fatalf("the editor was started on %q, not %q", got, clone)
			}
		},
		AndTheSessionIsStillEnded: func() {
			if state := journey.SessionState(m.Sessions(), "project-a"); state != "ended" {
				t.Fatalf("the session is %q:\n%s", state, m.Sessions())
			}
		},
	}, term0(&term))
}

// EDIT-OPEN-004: a session outside every registered project is refused with the reason, and the
// editor is not started. Fails if the editor is started on a guessed folder.
func TestEditOpen004(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.EditOpen004{
		GivenASessionIsRunningThatDoesNotBelongToARegisteredProject: func() {
			m.StartSession("scratch", m.Home)
		},
		WhenTheDeveloperOpensItInEditorFromTheSessionList: func() {
			term = editFromList(t, m, "scratch")
		},
		ThenTheyAreToldItsFolderIsNotKnownBecauseItIsNotInARegisteredProject: func() {
			term.WaitFor("its folder is not known because it is not in a registered project")
		},
		AndEditorIsNotStarted: func() {
			if starts := m.EditorStarts(); len(starts) != 0 {
				t.Fatalf("the editor was started: %q", starts)
			}
		},
	}, term0(&term))
}

// EDIT-OPEN-005: with no editor command on this machine, the developer is told how to install
// it and nothing runs. Fails if the missing command is run (and fails) instead of reported, or
// if the fix is not given.
func TestEditOpen005(t *testing.T) {
	m := journey.NewMachine(t)
	clone := m.NewRepo("project-a")
	m.BeEngine("project-a", clone)
	m.StartSession("project-a", clone)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.EditOpen005{
		GivenEditorSCommandIsNotInstalledOnThisMachine: func() {
			m.RemoveEditor()
		},
		WhenTheDeveloperOpensASessionInEditor: func() {
			term = m.Start(t, "edit", "project-a")
		},
		ThenTheyAreToldTheCommandWasNotFoundOnThisMachineAndHowToInstallIt: func() {
			term.WaitFor(`"fake-editor" was not found on this machine`)
			term.WaitFor("Shell Command: Install 'code' command in PATH")
		},
		AndNothingIsRun: func() {
			if strings.Contains(term.Screen(), "FAILED") {
				t.Fatalf("the missing command was run:\n%s", term.Screen())
			}
			if out, _ := m.Run("engineroom", "history"); strings.Contains(out, "in the editor") {
				t.Fatalf("an action is in the history:\n%s", out)
			}
		},
	}, term0(&term))
}

// EDIT-OPEN-006: working locally on a client, the editor opens this machine's copy, with no
// remote. Fails if the engine is used as the editor's remote while working locally.
func TestEditOpen006(t *testing.T) {
	client, engine := clientAndEngine(t)
	local := client.NewRepo("project-a")
	client.AddLocalProject("project-a", local)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.EditOpen006{
		GivenTheDeveloperIsWorkingLocallyOnAClientBecauseTheEngineDidNotAnswer: func() {
			engine.SetReachable(client, false)
			term = client.Start(t)
			term.WaitFor("did not answer")
			term.Type("l")
			// No session runs here yet, so working locally lands on this machine's projects.
			term.WaitFor("Registered projects")
			mustBeLocal(t, term)
		},
		AndASessionIsRunningInThisMachinesOwnCopyOfAProject: func() {
			client.StartSession("project-a", local)
		},
		WhenTheDeveloperOpensItInEditorFromTheSessionList: func() {
			term.Press(termtest.Esc)
			term.WaitFor("Sessions on this machine")
			term.WaitFor("running")
			term.Type("e")
			term.WaitFor("Done.")
		},
		ThenEditorIsStartedOnThisMachinesFolderNotTheEngineS: func() {
			if got := editedFolder(t, client); resolve(got) != resolve(local) {
				t.Fatalf("the editor was started on %q, not this machine's %q", got, local)
			}
		},
	}, term0(&term))
}
