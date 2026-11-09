package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/procs"
	"github.com/KyleyHarris/engineroom/internal/shell"
)

// runSweep is `engineroom sweep [--dry-run | --watch | --uninstall]` (docs/decisions.md D-020). One
// sweep deletes what Zellij kept of ended sessions of registered checkouts, and stops what still
// runs for a checkout whose session has been gone for two minutes, each as an action in the
// history. It looks at nothing but this machine's Zellij sessions and processes, never the
// network, and is silent when there is nothing to do. --dry-run says what it would do and changes
// nothing. --watch sweeps every minute for as long as the app that started it runs (sweepWatch).
// --uninstall removes the launchd job an earlier build installed; there is no job any more.
func runSweep(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--uninstall" {
		return removeSweepJob(stdout, stderr)
	}
	if len(args) == 1 && args[0] == "--watch" {
		return sweepWatch(stdout, stderr)
	}
	dry := len(args) == 1 && args[0] == "--dry-run"
	if len(args) > 1 || (len(args) == 1 && !dry) {
		fmt.Fprintln(stderr, "usage: engineroom sweep [--dry-run | --watch | --uninstall]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	// A client's sessions are the engine's, swept there; a client sweeps only while it works local,
	// when the sessions are its own.
	if cfg.Role == config.RoleClient && !cfg.Offline {
		if dry {
			fmt.Fprintln(stdout, "This machine is a client working on the engine, so it has no sessions of its own to sweep.")
		}
		return 0
	}
	sw, err := ops.FindSweep(cfg, time.Now(), !dry)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if dry {
		if len(sw.Plans) == 0 && len(sw.Waiting) == 0 {
			fmt.Fprintln(stdout, "Nothing to sweep.")
		}
		for _, p := range sw.Plans {
			fmt.Fprintf(stdout, "%s:\n", p.Title)
			for _, line := range p.Lines() {
				fmt.Fprintf(stdout, "  %s\n", line)
			}
		}
		var waiting []string
		for n := range sw.Waiting {
			waiting = append(waiting, n)
		}
		sort.Strings(waiting)
		for _, n := range waiting {
			fmt.Fprintf(stdout, "%s has ended, but something started for it still runs; it is stopped in %s unless the session comes back.\n", n, sw.Waiting[n].Round(time.Second))
		}
		return 0
	}
	failed := false
	for _, p := range sw.Plans {
		if !runPlain(p, stdout, stderr) {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// sweepWatch sweeps now and then every minute, in this one process, for as long as the process
// that started it runs: the menu bar app starts it on the engine, and on a client only while the
// client works local, and stops it when that ends or the app quits (D-020 as amended): never a
// separate job that runs all the time. Should the app end
// without stopping it, the watch sees it has been left (its parent changed) and ends too, so it
// never outlives the app. Each sweep decides afresh from the machine's role, so a client that
// stopped working local does nothing even before the app stops the watch.
func sweepWatch(stdout, stderr io.Writer) int {
	parent := os.Getppid()
	for {
		runSweep(nil, stdout, stderr)
		// The parent is looked at every second, so the watch ends within a second of the app.
		for i := 0; i < 60; i++ {
			if os.Getppid() != parent {
				return 0
			}
			time.Sleep(time.Second)
		}
	}
}

// sweepLabel names the launchd job an earlier build installed to run the sweep, under the program's
// name then (zellij-claude-hub), which is why it is kept as it was.
const sweepLabel = "local.zellij-claude-hub.sweep"

// removeSweepJob takes down and deletes that job, printing each step before it runs. The sweep
// runs with the app now (sweepWatch), so the job only ever needs removing.
func removeSweepJob(stdout, stderr io.Writer) int {
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(stdout, "No launchd job on this system; nothing to remove.")
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", sweepLabel+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if exec.Command("launchctl", "print", domain+"/"+sweepLabel).Run() == nil {
		argv := []string{"launchctl", "bootout", domain + "/" + sweepLabel}
		fmt.Fprintf(stdout, "+ %s\n", shell.Join(argv))
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		writeOutput(stdout, string(out))
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: launchctl bootout failed: %v\n", err)
			return 1
		}
	}
	if _, err := os.Stat(plist); err == nil {
		fmt.Fprintf(stdout, "+ rm %s\n", plist)
		if err := os.Remove(plist); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
	}
	fmt.Fprintln(stdout, "No sweep job is installed on this machine; the engineroom app runs the sweep while it is needed.")
	return 0
}

// runProcess is `engineroom process stop <pid> --port <n>`: one process the developer chose, by its
// number, stopped only while it still listens on that port, so a number reused since it was
// shown is never stopped.
func runProcess(args []string, stdout, stderr io.Writer) int {
	if len(args) != 4 || args[0] != "stop" || args[2] != "--port" {
		fmt.Fprintln(stderr, "usage: engineroom process stop <pid> --port <n>")
		return 2
	}
	pid, err1 := strconv.Atoi(args[1])
	port, err2 := strconv.Atoi(args[3])
	if err1 != nil || err2 != nil || pid <= 1 {
		fmt.Fprintln(stderr, "engineroom: the pid and the port are numbers")
		return 2
	}
	if !procs.ListensOn(pid, port) {
		fmt.Fprintf(stderr, "engineroom: pid %d does not listen on %d now, so nothing was stopped\n", pid, port)
		return 1
	}
	if left := procs.Stop([]procs.Proc{{PID: pid}}, 5*time.Second); len(left) > 0 {
		fmt.Fprintf(stderr, "engineroom: pid %d would not stop\n", pid)
		return 1
	}
	fmt.Fprintf(stdout, "Stopped pid %d, which listened on %d.\n", pid, port)
	return 0
}
