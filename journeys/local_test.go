//go:build journeys

package journeys

import (
	"encoding/json"
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

// The local role's journeys (D-039): a Mac used on its own, checked, set, chosen in the app's
// Settings through the api the app calls, installed as local, and running its own sessions. Each
// runs the built program on an isolated machine with real Zellij, git and zsh.

const localConfig = "role = \"local\"\n[[project]]\nname = \"project-a\"\npath = \"/src/project-a\"\n"

// roleShown is what `engineroom role show` says on the machine.
func roleShown(t *testing.T, m *journey.Machine) string {
	t.Helper()
	out, err := m.Run("engineroom", "role", "show")
	if err != nil {
		t.Fatalf("role show: %v\n%s", err, out)
	}
	return strings.TrimSpace(out)
}

// SETUP-LOCAL-001: a Mac used on its own is checked for every item every machine needs, each
// marked, and for nothing of the engine's (SSH) or a client's. Fails if an every-machine item is
// left out, an SSH or client item is checked, or the report names the Mac as the engine.
func TestSetupLocal001(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupLocal001{
		GivenThisMachinesRoleIsLocal: func() { m.WriteConfig(localConfig); standInClaude(t, m) },
		WhenTheDeveloperRunsTheSetupCheck: func() {
			run = runCheck(t, m, &term)
			if !strings.Contains(run.out, "This machine is used on its own (local)") {
				t.Fatalf("the report does not say the role:\n%s", run.out)
			}
		},
		ThenEveryItemEveryMachineNeedsIsListedEachMarkedPresentOrMissing: func() {
			items := reported(t, run.out)
			for _, name := range everyMachine {
				if _, ok := items[name]; !ok {
					t.Fatalf("%q is not listed:\n%s", name, run.out)
				}
			}
			if len(items) != len(everyMachine) || items["git (every machine)"] != "present" || items["a role (every machine)"] != "present" {
				t.Fatalf("%d items, or the items this Mac has are not marked present:\n%s", len(items), run.out)
			}
		},
		AndNoItemOnlyTheEngineNeedsIsListed: func() {
			if e := forRole(reported(t, run.out), "engine"); len(e) != 0 || strings.Contains(run.out, "SSH command") {
				t.Fatalf("the engine's items are checked on a local Mac: %q\n%s", e, run.out)
			}
		},
		AndNoItemAClientNeedsIsListed: func() {
			if c := forRole(reported(t, run.out), "client"); len(c) != 0 || strings.Contains(run.out, "VS Code") {
				t.Fatalf("a client's items are checked on a local Mac: %q\n%s", c, run.out)
			}
		},
	}, term0(&term))
}

// SETUP-LOCAL-002: the role goes from the engine to local and back with one command each, and the
// registered project survives both. Fails if a set does not take, the role shown is wrong, or the
// configuration loses the project.
func TestSetupLocal002(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.SetupLocal002{
		GivenThisMachineIsTheEngineWithAProjectRegistered: func() {
			m.BeEngine("project-a", path)
			if got := roleShown(t, m); got != "engine" {
				t.Fatalf("role before: %q", got)
			}
		},
		WhenTheDeveloperSetsThisMachinesRoleToLocal: func() {
			term = m.Start(t, "role", "set", "local")
			if code := term.WaitExit(); code != 0 {
				t.Fatalf("role set local ended with %d", code)
			}
		},
		ThenTheRoleShownIsLocal: func() {
			if got := roleShown(t, m); got != "local" {
				t.Fatalf("role shown: %q", got)
			}
		},
		AndTheProjectIsStillRegistered: func() {
			out, err := m.Run("engineroom", "project", "list")
			if err != nil || !strings.Contains(out, "project-a") {
				t.Fatalf("the project is gone after the role changed: %v\n%s", err, out)
			}
		},
		AndWhenTheDeveloperSetsTheRoleToTheEngineAgainTheRoleShownIsTheEngine: func() {
			if out, err := m.Run("engineroom", "role", "set", "engine"); err != nil {
				t.Fatalf("role set engine: %v\n%s", err, out)
			}
			if got := roleShown(t, m); got != "engine" {
				t.Fatalf("role shown after setting the engine again: %q", got)
			}
			if out, _ := m.Run("engineroom", "project", "list"); !strings.Contains(out, "project-a") {
				t.Fatalf("the project is gone after the role changed back:\n%s", out)
			}
		},
	}, term0(&term))
}

// apiSetRole is the call the app's Settings make when a role is chosen: `engineroom api set role
// <value>`, answered with the machine's status, or an error with exit 3.
func apiSetRole(t *testing.T, m *journey.Machine, value string) (int, map[string]any) {
	t.Helper()
	cmd := m.Command("engineroom", "api", "set", "role", value)
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("api set role: %v", err)
	}
	var reply map[string]any
	if jerr := json.Unmarshal(out, &reply); jerr != nil {
		t.Fatalf("api set role %s answered %q: %v", value, out, jerr)
	}
	return code, reply
}

// SETUP-LOCAL-003: the app's Settings choose the engine or "this Mac only" through the api it
// calls, each saved, and a client is refused with the command that sets it. Fails if a choice is
// not saved, the reply's role is not the one saved, or a client is set without an engine.
func TestSetupLocal003(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.SetupLocal003{
		GivenThisMachinesRoleIsLocal: func() { m.WriteConfig(localConfig) },
		WhenTheDeveloperChoosesTheEngineAsThisMacsRoleInTheAppsSettings: func() {
			if code, reply := apiSetRole(t, m, "engine"); code != 0 || reply["role"] != "engine" {
				t.Fatalf("choosing the engine: %d %v", code, reply)
			}
		},
		ThenTheRoleSavedIsTheEngine: func() {
			if got := roleShown(t, m); got != "engine" {
				t.Fatalf("role saved: %q", got)
			}
		},
		AndWhenTheDeveloperChoosesThisMacOnlyTheRoleSavedIsLocal: func() {
			if code, reply := apiSetRole(t, m, "local"); code != 0 || reply["role"] != "local" {
				t.Fatalf("choosing this Mac only: %d %v", code, reply)
			}
			if got := roleShown(t, m); got != "local" {
				t.Fatalf("role saved: %q", got)
			}
		},
		AndChoosingAClientThereIsRefusedNamingTheCommandThatSetsIt: func() {
			code, reply := apiSetRole(t, m, "client")
			msg, _ := reply["error"].(string)
			if code != 3 || !strings.Contains(msg, "engineroom role set client --engine") {
				t.Fatalf("choosing a client: %d %v", code, reply)
			}
			if got := roleShown(t, m); got != "local" {
				t.Fatalf("a refused client changed the role to %q", got)
			}
		},
	}, term0(&term))
}

// goEnv is the Go toolchain's own locations, so the install builds with the developer's module
// and build caches rather than a cold cache under the machine's empty home.
func goEnv(t *testing.T) []string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is needed to run the install")
	}
	out, err := exec.Command(gobin, "env", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err != nil {
		t.Fatal(err)
	}
	v := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(v) != 3 {
		t.Fatalf("go env: %q", out)
	}
	return []string{"GOCACHE=" + v[0], "GOMODCACHE=" + v[1], "GOPATH=" + v[2], "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local", "goDir=" + filepath.Dir(gobin)}
}

// SETUP-LOCAL-004: the install with --local, on a machine where an SSH command would not find the
// program, ends with success and sets the role to local. Fails if the install stops on the SSH
// PATH, ends with a failure, or leaves the role anything but local.
func TestSetupLocal004(t *testing.T) {
	m := journey.NewMachine(t)
	bin := filepath.Join(m.Home, ".local", "bin")
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	ge := goEnv(t)
	goDir := strings.TrimPrefix(ge[len(ge)-1], "goDir=")
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.SetupLocal004{
		GivenAnSSHCommandOnThisMachineWouldNotFindTheProgram: func() {
			// Exactly the install's own test of it: a bare zsh with this machine's home.
			cmd := exec.Command("env", "-i", "HOME="+m.Home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "/bin/zsh", "-c", "command -v engineroom")
			if out, err := cmd.Output(); err == nil {
				t.Fatalf("an SSH command already finds the program: %s", out)
			}
		},
		WhenTheDeveloperInstallsWithTheLocalOption: func() {
			cmd := exec.Command("scripts/install.sh", "--local")
			cmd.Dir = repo
			env := []string{}
			for _, e := range m.Env() {
				if strings.HasPrefix(e, "PATH=") {
					e = "PATH=" + goDir + ":" + strings.TrimPrefix(e, "PATH=")
				}
				env = append(env, e)
			}
			cmd.Env = append(append(env, ge[:len(ge)-1]...), "ENGINEROOM_BIN="+bin)
			opts := journeyOptions(t)
			opts.Timeout = 5 * time.Minute
			term = termtest.Start(t, cmd, opts)
		},
		ThenTheInstallEndsWithSuccess: func() {
			if code := term.WaitExit(); code != 0 {
				t.Fatalf("the install ended with %d", code)
			}
			if _, err := os.Stat(filepath.Join(bin, "engineroom")); err != nil {
				t.Fatalf("the program was not installed: %v", err)
			}
		},
		AndThisMachinesRoleIsLocal: func() {
			// The installed program, reading this machine's configuration.
			out, err := m.Command(filepath.Join(bin, "engineroom"), "role", "show").Output()
			if err != nil || strings.TrimSpace(string(out)) != "local" {
				t.Fatalf("role after the install: %v %q", err, out)
			}
		},
	}, term0(&term))
}

// SETUP-LOCAL-005: on a local Mac, opening a project starts its session here, in its worktree, as
// on the engine. Fails if the open asks for an engine, or no session runs here with the agent in
// the project's folder.
func TestSetupLocal005(t *testing.T) {
	m := journey.NewMachine(t)
	path := m.NewRepo("project-a")
	var term *termtest.Terminal
	scenarios.Run(t, scenarios.SetupLocal005{
		GivenThisMachinesRoleIsLocalWithAProjectRegistered: func() {
			m.BeEngine("project-a", path)
			b, err := os.ReadFile(m.ConfigPath())
			if err != nil {
				t.Fatal(err)
			}
			m.WriteConfig(strings.Replace(string(b), `role = "engine"`, `role = "local"`, 1))
			if got := roleShown(t, m); got != "local" {
				t.Fatalf("role: %q", got)
			}
		},
		WhenTheDeveloperOpensTheProject: func() {
			term = m.Start(t, "open", "project-a")
		},
		ThenASessionIsCreatedInItsCheckoutOnThisMachine: func() {
			term.WaitFor("AGENT new conversation")
			m.WaitSession("project-a", "running")
			if !m.AgentStartedIn(path) {
				t.Fatalf("the agent did not start in %s: %q", path, m.AgentStarts())
			}
		},
	}, term0(&term))
}
