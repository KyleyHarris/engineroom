package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/assistant"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/mux"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/upgrade"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// upgradeFacts is what the upgrade brief is told about this machine: its name and role, the
// running build, the clone it was built from (recorded by scripts/install.sh) and where the app is.
func upgradeFacts(cfg *config.Config) upgrade.Facts {
	return upgrade.Facts{Machine: machineName(), Role: string(cfg.Role), Version: version, Clone: built, App: appBundle()}
}

// appBundle is the menu bar app installed on this machine, where scripts/install.sh --app puts it
// (ENGINEROOM_APPS, else ~/Applications), or empty when it is not installed or this is not a Mac.
func appBundle() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	dirs := []string{os.Getenv("ENGINEROOM_APPS")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		app := filepath.Join(d, "engineroom.app")
		if info, err := os.Stat(app); err == nil && info.IsDir() {
			return app
		}
	}
	return ""
}

// runUpgrade is `engineroom upgrade prepare [--remote-control]`: the first step of the app's
// Upgrade, run on the machine being upgraded. It writes the upgrade session's brief and layout and
// prints where.
func runUpgrade(args []string, stdout, stderr io.Writer) int {
	remote := len(args) == 2 && args[1] == "--remote-control"
	if len(args) == 0 || args[0] != "prepare" || (len(args) == 2 && !remote) || len(args) > 2 {
		fmt.Fprintln(stderr, "usage: engineroom upgrade prepare [--remote-control]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	f := upgradeFacts(cfg)
	contextFile := filepath.Join(assistantDir(), upgrade.SessionName+".md")
	opening := upgrade.Opening(f)
	layout := assistant.Layout(cfg.Agent.Command, contextFile, settings.Root(), opening, remote)
	agent := assistant.Agent(cfg.Agent.Command, contextFile, settings.Root(), opening, remote)
	contextFile, layoutFile, err := assistant.Write(assistantDir(), upgrade.SessionName, upgrade.Context(f), layout, agent)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\nwrote %s\n", contextFile, layoutFile)
	return 0
}

// upgradePlan starts a new upgrade session on this machine (internal/upgrade): it ends one left
// from an earlier upgrade, writes the brief and layout, starts the session in the background in
// the clone the running build was made from (the home folder when none was recorded), and waits
// for it. Each press is a new upgrade from the build now installed: joining the session left
// from before would show an idle Claude, its upgrade done or never asked (APP-UPGRADE-003).
func upgradePlan(cfg *config.Config, remote bool) action.Plan {
	name := upgrade.SessionName
	plan := action.Plan{Title: "Upgrade engineroom on " + machineName() + ", with Claude", Machine: "this machine"}
	if list, err := mux.Active().List(); err == nil {
		for _, s := range list {
			if s.Name != name {
				continue
			}
			if !s.Ended {
				plan.Notes = append(plan.Notes, "An upgrade session from before is still here: it is ended, and a new one starts from the build now installed.")
			}
			plan.Commands = append(plan.Commands, mux.End(name))
		}
	}
	dir := built
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	prepare := []string{"engineroom", "upgrade", "prepare"}
	if remote {
		prepare = append(prepare, "--remote-control")
	}
	plan.Commands = append(plan.Commands,
		action.Command{Argv: prepare, Why: "write the upgrade session's brief (the steps, and what it may fix or must ask about) and its layout"},
	)
	plan.Commands = append(plan.Commands, mux.Active().Create(name, dir, mux.AgentOnly(filepath.Join(assistantDir(), name+".kdl")))...)
	plan.Commands = append(plan.Commands,
		action.Command{Argv: []string{"engineroom", "session", "wait", name}, Why: "wait until the upgrade session is ready"},
	)
	plan.Notes = append(plan.Notes, "Claude pulls the clone, installs the new build, restarts the app and says what changed, in the session "+name+". It asks before anything that could lose work.")
	return plan
}

// runApp is `engineroom app relaunch`: quits the menu bar app and opens it again, so a build just
// installed runs. The app may be the one that asked (its Upgrade), so the restart happens a moment
// later, from a process of its own, after this command has returned and its answer has reached the
// app. The app is opened without the markers of any Claude Code session (zj.AgentSessionVars), so
// what it starts never thinks it runs inside one.
func runApp(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "relaunch" || len(args) > 2 || (len(args) == 2 && args[1] != "--now") {
		fmt.Fprintln(stderr, "usage: engineroom app relaunch")
		return 2
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(stderr, "engineroom: the menu bar app is macOS only")
		return 1
	}
	if appBundle() == "" {
		fmt.Fprintln(stderr, "engineroom: the menu bar app is not installed here (scripts/install.sh --app installs it)")
		return 1
	}
	if len(args) == 2 {
		if err := relaunchNow(); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	cmd := exec.Command(self, "app", "relaunch", "--now")
	cmd.Env = withoutAgentMarkers(os.Environ())
	// A session of its own, with nothing attached, so it outlives this command, the app that ran
	// it, and an SSH connection that ran it on the engine.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	_ = cmd.Process.Release()
	fmt.Fprintln(stdout, "the app quits and opens again in a moment")
	return 0
}

// relaunchNow waits a moment, quits the app by its bundle identifier, waits until it has gone,
// and opens it again.
func relaunchNow() error {
	time.Sleep(2 * time.Second)
	running := func() bool {
		out, err := exec.Command("osascript", "-e", `application id "`+upgrade.BundleID+`" is running`).Output()
		return err == nil && strings.TrimSpace(string(out)) == "true"
	}
	if running() {
		if out, err := exec.Command("osascript", "-e", `tell application id "`+upgrade.BundleID+`" to quit`).CombinedOutput(); err != nil {
			return fmt.Errorf("quitting the app: %v: %s", err, strings.TrimSpace(string(out)))
		}
		for i := 0; i < 40 && running(); i++ {
			time.Sleep(250 * time.Millisecond)
		}
		if running() {
			return errors.New("the app did not quit within ten seconds; quit it from its Settings and open it again")
		}
	}
	if out, err := exec.Command("open", "-b", upgrade.BundleID).CombinedOutput(); err != nil {
		return fmt.Errorf("opening the app: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// withoutAgentMarkers is env without Claude Code's session markers (zj.AgentSessionVars).
func withoutAgentMarkers(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, v := range zj.AgentSessionVars {
			if name == v {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}
