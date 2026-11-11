//go:build journeys

package journeys

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/setup"
	"github.com/KyleyHarris/engineroom/internal/sshtest"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// The setup check's journeys run the built program, as a developer would, on an isolated machine
// with real Zellij, git, ssh and zsh. Claude Code and VS Code are stood in by scripts that answer
// the questions the check asks them (their version, the plugins, the extensions): the real ones
// need an account and an application install a journey cannot make.

// standInClaude puts a Claude Code on the machine with engineroom's plugin installed and enabled.
func standInClaude(t *testing.T, m *journey.Machine) {
	t.Helper()
	tool(t, m, "claude", `case "$1" in
--version) echo "2.1.0 (Claude Code)" ;;
plugin) echo '[{"id":"engineroom@engineroom","version":"0.1.0","scope":"user","enabled":true}]' ;;
esac
`)
}

// standInCode puts VS Code's code command on the machine, with the Remote - SSH extension.
func standInCode(t *testing.T, m *journey.Machine) {
	t.Helper()
	tool(t, m, "code", `case "$1" in
--version) echo "1.100.0" ;;
--list-extensions) echo "`+setup.RemoteSSH+`" ;;
esac
`)
}

func tool(t *testing.T, m *journey.Machine, name, script string) {
	t.Helper()
	p := filepath.Join(m.Bin, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// vscodeSSHConfig sets VS Code's remote.SSH.configFile on the machine.
func vscodeSSHConfig(t *testing.T, m *journey.Machine, file string) {
	t.Helper()
	dir := filepath.Join(m.Home, "Library", "Application Support", "Code", "User")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("{\n  // where Remote - SSH reads hosts\n  \"remote.SSH.configFile\": %q,\n}\n", file)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkRun is one run of the setup check in a terminal: what it printed and how it ended.
type checkRun struct {
	out  string
	exit int
}

// runCheck runs `engineroom setup check` (with args) in a terminal, so a recording shows the report,
// and waits for it to end. What it printed is read from a second run outside the terminal, which
// the check allows since it changes nothing: the terminal wraps long lines (paths), and the report
// is read line by line. The two must end the same way.
func runCheck(t *testing.T, m *journey.Machine, term **termtest.Terminal, args ...string) checkRun {
	t.Helper()
	argv := append([]string{"setup", "check"}, args...)
	*term = m.Start(t, argv...)
	shown := (*term).WaitExit()
	cmd := m.Command("engineroom", argv...)
	out, err := cmd.Output()
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("the check did not run: %v", err)
		}
		exit = ee.ExitCode()
	}
	if exit != shown {
		t.Fatalf("the check ended with %d in the terminal and %d outside it", shown, exit)
	}
	return checkRun{out: string(out), exit: exit}
}

// reported is each item on the screen, name to "present" or "missing", read from the report's
// lines ("present  <name> (<for>): <found>").
func reported(t *testing.T, out string) map[string]string {
	t.Helper()
	items := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		state, rest, ok := strings.Cut(line, "  ")
		if !ok || (state != "present" && state != "missing") {
			continue
		}
		name, _, _ := strings.Cut(rest, ": ")
		items[name] = state
	}
	return items
}

// itemLines is the report's line for an item and the lines under it, up to the next item.
func itemLines(t *testing.T, out, name string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "present  "+name) || strings.HasPrefix(l, "missing  "+name) {
			j := i + 1
			for j < len(lines) && strings.HasPrefix(lines[j], "         ") {
				j++
			}
			return strings.Join(lines[i:j], "\n")
		}
	}
	t.Fatalf("no item %q in the report:\n%s", name, out)
	return ""
}

var (
	everyMachine = []string{"a valid configuration (every machine)", "a role (every machine)", "Zellij 0.45.0 or later (every machine)",
		"git (every machine)", "OpenSSH (every machine)", "Claude Code (every machine)",
		"engineroom's Claude Code plugin, with its hooks (every machine)", "at least one project registered (every machine)"}
	engineOnly = []string{"engineroom found by an SSH command (engine)", "the SSH server on (Remote Login) (engine)"}
)

const engineConfig = "role = \"engine\"\n[[project]]\nname = \"project-a\"\npath = \"/src/project-a\"\n"

// forRole is how many reported items are for the role, from the "(role)" each line names.
func forRole(items map[string]string, role string) []string {
	var out []string
	for name := range items {
		if strings.HasSuffix(name, "("+role+")") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// SETUP-CHECK-001: the engine is checked for every item every machine needs and every item the
// engine needs, each marked, and nothing a client needs. Fails if an item is left out silently, an
// item is not marked present or missing, or a client's item is checked on the engine.
func TestSetupCheck001(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck001{
		GivenThisMachineIsTheEngine: func() { m.WriteConfig(engineConfig); standInClaude(t, m) },
		WhenTheDeveloperRunsTheSetupCheck: func() {
			run = runCheck(t, m, &term)
			if !strings.Contains(run.out, "This machine is the engine") {
				t.Fatalf("the report does not say the role:\n%s", run.out)
			}
		},
		ThenEveryItemEveryMachineNeedsAndEveryItemTheEngineNeedsIsListed: func() {
			items := reported(t, run.out)
			for _, name := range append(append([]string{}, everyMachine...), engineOnly...) {
				if _, ok := items[name]; !ok {
					t.Fatalf("%q is not listed:\n%s", name, run.out)
				}
			}
		},
		AndEachIsMarkedPresentOrMissing: func() {
			items := reported(t, run.out)
			if len(items) != len(everyMachine)+len(engineOnly) {
				t.Fatalf("%d items marked, want %d:\n%s", len(items), len(everyMachine)+len(engineOnly), run.out)
			}
			if items["git (every machine)"] != "present" || items["at least one project registered (every machine)"] != "present" {
				t.Fatalf("items this machine has are not marked present:\n%s", run.out)
			}
		},
		AndNoItemAClientNeedsIsListed: func() {
			if c := forRole(reported(t, run.out), "client"); len(c) != 0 || strings.Contains(run.out, "VS Code") {
				t.Fatalf("a client's items are checked on the engine: %q\n%s", c, run.out)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-002: a client is checked for what every machine needs (it mirrors the engine, and
// works on its own when the engine does not answer) and what a client needs, never the engine's
// own items. Fails if a client's item is missing from its report or an engine's item is in it.
func TestSetupCheck002(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck002{
		GivenThisMachineIsAClient: func() {
			m.WriteConfig("role = \"client\"\n[engine]\nhost = \"engine.invalid\"\nconnect_timeout = \"1s\"\n[editor]\ncommand = \"code\"\n")
			standInClaude(t, m)
			standInCode(t, m)
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenEveryItemEveryMachineNeedsAndEveryItemAClientNeedsIsListed: func() {
			items := reported(t, run.out)
			for _, name := range append(append([]string{}, everyMachine...),
				"the engine engine.invalid answers without a prompt (client)", "VS Code's code command (client)",
				"VS Code's Remote - SSH extension (client)", "the engine's name engine.invalid known to VS Code (client)") {
				if _, ok := items[name]; !ok {
					t.Fatalf("%q is not listed:\n%s", name, run.out)
				}
			}
		},
		AndNoItemOnlyTheEngineNeedsIsListed: func() {
			if e := forRole(reported(t, run.out), "engine"); len(e) != 0 {
				t.Fatalf("the engine's items are checked on a client: %q\n%s", e, run.out)
			}
		},
	}, term0(&term))
}

// tree is every file and folder under the machine's home, with its size, time and mode.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		lines = append(lines, p+" "+strconv.FormatInt(info.Size(), 10)+" "+info.ModTime().String()+" "+info.Mode().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// SETUP-CHECK-003: Zellij not installed is missing, with the version needed and the command that
// installs it, and the check changes nothing on the machine (its whole home is compared before and
// after). Fails if a missing Zellij reads as present, its fix is not the install command, or the
// check writes, moves or changes any file.
func TestSetupCheck003(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	var before string
	scenarios.Run(t, scenarios.SetupCheck003{
		GivenZellijIsNotInstalledOnThisMachine: func() {
			m.WriteConfig(engineConfig)
			// The machine's PATH without the folder Zellij is installed in.
			m.Extra = append(m.Extra, "PATH="+m.Bin+":/usr/bin:/bin:/usr/sbin:/sbin")
			if out, err := m.Run("sh", "-c", "command -v zellij"); err == nil {
				t.Fatalf("zellij is still found: %s", out)
			}
			before = tree(t, m.Home)
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenZellijIsMarkedMissingWithTheVersionNeededAndTheCommandThatInstallsIt: func() {
			z := itemLines(t, run.out, "Zellij")
			if !strings.HasPrefix(z, "missing") || !strings.Contains(z, "needs Zellij 0.45.0 or later") || !strings.Contains(z, "fix: brew install zellij") {
				t.Fatalf("Zellij not installed is reported as:\n%s", z)
			}
		},
		AndNothingOnTheMachineHasChanged: func() {
			if after := tree(t, m.Home); after != before {
				t.Fatalf("the check changed the machine:\nbefore:\n%s\nafter:\n%s", before, after)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-004: every item says the command it was checked with or the file it was read from,
// and that command, run by hand, is a real command on this machine. Fails if an item does not say
// how it was checked, or names a command that does not exist.
func TestSetupCheck004(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck004{
		GivenThisMachineIsTheEngine:       func() { m.WriteConfig(engineConfig); standInClaude(t, m) },
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenEachItemShowsTheCommandItWasCheckedWithOrTheFileItWasReadFrom: func() {
			for name := range reported(t, run.out) {
				block := itemLines(t, run.out, strings.TrimSuffix(strings.TrimSuffix(name, " (every machine)"), " (engine)"))
				_, how, ok := strings.Cut(block, "checked with: ")
				how, _, _ = strings.Cut(how, "\n")
				if !ok || strings.TrimSpace(how) == "" {
					t.Fatalf("%q does not say how it was checked:\n%s", name, block)
				}
				if strings.HasPrefix(how, "read ") || strings.HasPrefix(how, "role in ") || strings.HasPrefix(how, "the [[project]] entries in ") {
					if !strings.Contains(how, m.ConfigPath()) {
						t.Fatalf("%q names a file other than the configuration: %s", name, how)
					}
					continue
				}
				if strings.HasPrefix(how, "connect to ") {
					continue
				}
				first := strings.Fields(how)[0]
				if _, err := m.Run("sh", "-c", "command -v "+first); err != nil {
					t.Fatalf("%q was checked with %q, which is not a command here", name, first)
				}
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-005: a machine with no role is told the command for each role, and what every
// machine needs is still checked. Fails if the role is not reported missing with both commands, or
// a role's absence stops the other checks.
func TestSetupCheck005(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck005{
		GivenThisMachineHasNoRoleSet: func() {
			// No configuration file: a machine never set up.
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenTheRoleIsMarkedMissingWithTheCommandThatSetsEachRole: func() {
			r := itemLines(t, run.out, "a role")
			if !strings.HasPrefix(r, "missing") || !strings.Contains(r, "engineroom role set engine") || !strings.Contains(r, "engineroom role set client --engine") {
				t.Fatalf("no role is reported as:\n%s", r)
			}
			if !strings.Contains(run.out, "This machine has no role") || run.exit != 1 {
				t.Fatalf("exit %d:\n%s", run.exit, run.out)
			}
		},
		AndEveryItemEveryMachineNeedsIsStillListedPresentOrMissing: func() {
			items := reported(t, run.out)
			for _, name := range everyMachine {
				if _, ok := items[name]; !ok {
					t.Fatalf("%q is not listed:\n%s", name, run.out)
				}
			}
			if items["git (every machine)"] != "present" || len(items) != len(everyMachine) {
				t.Fatalf("with no role the machine is not checked for what every machine needs:\n%s", run.out)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-006: a configuration naming a role that does not exist is missing, naming its file
// and the field. Fails if the check reads past it or the report does not say where to look.
func TestSetupCheck006(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck006{
		GivenThisMachinesConfigurationNamesAnUnknownRole: func() { m.WriteConfig("role = \"hub\"\n") },
		WhenTheDeveloperRunsTheSetupCheck:                func() { run = runCheck(t, m, &term) },
		ThenTheConfigurationIsMarkedMissingNamingTheFileAndTheFieldThatIsWrong: func() {
			c := itemLines(t, run.out, "a valid configuration")
			if !strings.HasPrefix(c, "missing") || !strings.Contains(c, m.ConfigPath()) || !strings.Contains(c, `role "hub" is not one of`) {
				t.Fatalf("an unknown role is reported as:\n%s", c)
			}
			if !strings.Contains(run.out, "configuration is not valid") || run.exit != 1 {
				t.Fatalf("exit %d:\n%s", run.exit, run.out)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-007: the program on the PATH of interactive shells only (here: this machine's PATH,
// with no ~/.zshenv) is missing for SSH commands, with the line for ~/.zshenv; with that line in
// ~/.zshenv, it is present. Fails if the check reads the PATH it was started with, or the line it
// gives does not make it present.
func TestSetupCheck007(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	scenarios.Run(t, scenarios.SetupCheck007{
		GivenThisMachineIsTheEngine: func() { m.WriteConfig(engineConfig) },
		AndTheProgramsFolderIsOnThePATHOnlyForInteractiveShells: func() {
			if out, err := m.Run("sh", "-c", "command -v engineroom"); err != nil {
				t.Fatalf("engineroom is not on this shell's PATH: %s", out)
			}
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenTheProgramIsMarkedMissingForSSHCommandsWithTheLineToAddToTheFileEveryShellReads: func() {
			s := itemLines(t, run.out, "engineroom found by an SSH command")
			const line = `export PATH="$HOME/bin:$PATH"`
			if !strings.HasPrefix(s, "missing") || !strings.Contains(s, "fix: add this line to ~/.zshenv: "+line) {
				t.Fatalf("engineroom not found by SSH commands is reported as:\n%s", s)
			}
			// The line it gives is the one that works.
			if err := os.WriteFile(filepath.Join(m.Home, ".zshenv"), []byte(line+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			again := runCheck(t, m, &term)
			if s := itemLines(t, again.out, "engineroom found by an SSH command"); !strings.HasPrefix(s, "present") {
				t.Fatalf("with the line in ~/.zshenv:\n%s", s)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-008: on a client whose engine's host key has changed (a real sshd, restarted with
// another key), the engine is missing, said as a changed host key with what to do, as every
// action says it (D-034). Fails if it is said as an engine that did not answer, or with no way on.
func TestSetupCheck008(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	var s *sshtest.Server
	scenarios.Run(t, scenarios.SetupCheck008{
		GivenThisMachineIsAClient: func() {
			s = sshtest.Start(t)
			m.WriteConfig(fmt.Sprintf("role = \"client\"\n[engine]\nhost = %q\nssh_config = %q\nconnect_timeout = \"5s\"\n", sshtest.Host, s.SSHConfig))
		},
		AndTheEngineSHostKeyHasChanged:    func() { s.ChangeHostKey() },
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenTheEngineIsMarkedMissingSaidAsAChangedHostKeyWithWhatToDo: func() {
			e := itemLines(t, run.out, "the engine engine answers")
			if !strings.HasPrefix(e, "missing") || !strings.Contains(e, "host key has changed") || !strings.Contains(e, "check its new key on the engine itself") ||
				strings.Contains(e, "did not answer") {
				t.Fatalf("a changed host key is reported as:\n%s", e)
			}
		},
	}, term0(&term))
}

// SETUP-CHECK-009: a client whose VS Code reads an SSH configuration with no entry for the engine
// has the engine's name missing for VS Code, naming the file VS Code reads. Fails if the check
// reads engineroom's SSH configuration in VS Code's place, or does not name VS Code's file.
func TestSetupCheck009(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	var theirs string
	scenarios.Run(t, scenarios.SetupCheck009{
		GivenThisMachineIsAClient: func() {
			ours := filepath.Join(m.Home, "engineroom_ssh_config")
			if err := os.WriteFile(ours, []byte("Host engine\n  HostName engine.example\n  Port 2222\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			m.WriteConfig(fmt.Sprintf("role = \"client\"\n[engine]\nhost = \"engine\"\nssh_config = %q\nconnect_timeout = \"1s\"\n", ours))
		},
		AndTheSSHConfigurationEditorUsesHasNoEntryForTheEngine: func() {
			theirs = filepath.Join(m.Home, "vscode_ssh_config")
			if err := os.WriteFile(theirs, []byte("Host other\n  HostName other.example\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			vscodeSSHConfig(t, m, theirs)
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenTheEngineSNameIsMarkedMissingForEditorNamingTheFileEditorReads: func() {
			v := itemLines(t, run.out, "the engine's name engine known to VS Code")
			if !strings.HasPrefix(v, "missing") || !strings.Contains(v, "fix: add the engine's Host engine entry to "+theirs) {
				t.Fatalf("the engine unknown to VS Code is reported as:\n%s", v)
			}
		},
	}, term0(&term))
}

// SETUP-READ-001: a client with every item present (a real engine in a container, answering
// engineroom version over SSH) ends with success; take one item away and it ends with a failure.
// Fails if the exit status does not follow what is missing.
func TestSetupRead001(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var run checkRun
	var client string
	scenarios.Run(t, scenarios.SetupRead001{
		GivenEveryItemForThisMachinesRoleIsPresent: func() {
			h := journey.NewEngine(t, m)
			client = fmt.Sprintf("role = \"client\"\n[engine]\nhost = \"engine\"\nssh_config = %q\nconnect_timeout = \"5s\"\n[editor]\ncommand = \"code\"\n", h.SSHConfig)
			m.WriteConfig(client + fmt.Sprintf("[[project]]\nname = \"project-a\"\npath = %q\n", filepath.Join(m.Home, "project-a")))
			vscodeSSHConfig(t, m, h.SSHConfig)
			standInClaude(t, m)
			standInCode(t, m)
		},
		WhenTheDeveloperRunsTheSetupCheck: func() { run = runCheck(t, m, &term) },
		ThenItEndsWithSuccess: func() {
			if run.exit != 0 || !strings.Contains(run.out, "Every item is present (12 checked)") {
				t.Fatalf("every item present: exit %d\n%s", run.exit, run.out)
			}
		},
		AndWhenOneItemIsMissingItEndsWithAFailure: func() {
			// The client's own copy of its project is forgotten: nothing to open when working locally.
			m.WriteConfig(client)
			again := runCheck(t, m, &term)
			if again.exit != 1 || !strings.HasPrefix(itemLines(t, again.out, "at least one project registered"), "missing") || !strings.Contains(again.out, "1 of 12 items is missing") {
				t.Fatalf("one item missing: exit %d\n%s", again.exit, again.out)
			}
		},
	}, term0(&term))
}

// SETUP-READ-002: --json gives each item's name, whether it is present, how it was checked and its
// fix, and the same items, in the same states, as the report on screen. Fails if a field is absent
// or the two reports disagree.
func TestSetupRead002(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal
	var report setup.Report
	scenarios.Run(t, scenarios.SetupRead002{
		GivenThisMachineIsTheEngine: func() { m.WriteConfig(engineConfig) },
		WhenTheDeveloperRunsTheSetupCheckAsJSON: func() {
			run := runCheck(t, m, &term, "--json")
			start := strings.Index(run.out, "{")
			if start < 0 {
				t.Fatalf("no JSON:\n%s", run.out)
			}
			if err := json.Unmarshal([]byte(run.out[start:]), &report); err != nil {
				t.Fatalf("%v\n%s", err, run.out)
			}
		},
		ThenEachItemHasItsNameWhetherItIsPresentHowItWasCheckedAndItsFix: func() {
			missing := 0
			for _, it := range report.Items {
				if it.Name == "" || it.Checked == "" || it.Present == (it.Fix != "") {
					t.Fatalf("an item lacks a field, or a fix that matches its state: %+v", it)
				}
				if !it.Present {
					missing++
				}
			}
			// Claude Code is not on this machine, so at least it is missing, with its fix.
			if missing == 0 || report.Missing != missing {
				t.Fatalf("missing %d, counted %d", missing, report.Missing)
			}
		},
		AndTheItemsAreTheSameAsTheReportOnScreen: func() {
			screen := reported(t, runCheck(t, m, &term).out)
			if len(screen) != len(report.Items) {
				t.Fatalf("%d items on screen, %d as JSON", len(screen), len(report.Items))
			}
			for _, it := range report.Items {
				want := "missing"
				if it.Present {
					want = "present"
				}
				if got := screen[it.Name+" ("+string(it.For)+")"]; got != want {
					t.Fatalf("%q is %q on screen and %q as JSON", it.Name, got, want)
				}
			}
		},
	}, term0(&term))
}
