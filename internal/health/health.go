// Package health says whether the engine is ready to be worked on from away (acceptance/health,
// docs/decisions.md D-037): its setup items (internal/setup), and for each running session whether
// Remote Control is on and whether its app is running. Whether the engine sleeps is never checked:
// that is the decision of the person running it (D-042). Like the setup check it reports and never fixes: every check reads a file or runs a
// command that changes nothing, each item says which, and a missing one carries its fix. A fix the
// program can run is named as an action the developer may ask for; nothing here runs it.
package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/procs"
	"github.com/KyleyHarris/engineroom/internal/setup"
	"github.com/KyleyHarris/engineroom/internal/source"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// Group says what an item is about, so the view can lay the items out under their headings.
type Group string

const (
	// Setup is one of the engine's setup items, as `engineroom setup check` reports it.
	Setup Group = "setup"
	// Session is a running session's Remote Control or its app.
	Session Group = "session"
)

// Item is one thing the engine needs to be worked on from away. It carries the setup check's
// fields (name, present, how it was checked, what was found, the fix) and says which group and
// session it belongs to.
type Item struct {
	setup.Item
	Group   Group  `json:"group"`
	Session string `json:"session,omitempty"`
	// Action is the `engineroom api run` action that applies the fix, when the program can apply
	// it (HEALTH-FIX-001): run only when the developer asks. Empty for a fix the developer runs
	// themselves (HEALTH-FIX-002), and for a present item.
	Action []string `json:"action,omitempty"`
}

// Report is the whole health check of the engine.
type Report struct {
	// Machine is where the items were checked: "this machine", or the engine's SSH destination
	// when a client asked the engine (HEALTH-CHECK-003).
	Machine string `json:"machine"`
	// Unreached is the engine not reached from a client, said with its cause (D-034); there are no
	// items then, and nothing counts as present.
	Unreached string `json:"unreached,omitempty"`
	Items     []Item `json:"items"`
	Missing   int    `json:"missing"`
}

// Ready reports whether nothing is missing and the engine was reached.
func (r Report) Ready() bool { return r.Missing == 0 && r.Unreached == "" }

// StatusTimeout bounds each status script (D-037): a script that does not end is reported, and the
// rest of the report still comes back (HEALTH-APP-005). A variable so a test need not wait as long.
var StatusTimeout = 10 * time.Second

// Check checks this machine as the engine: its setup items, then each running session.
func Check(ctx context.Context) Report {
	r := Report{Machine: "this machine"}
	s := setup.Check(ctx)
	for _, it := range s.Items {
		r.Items = append(r.Items, Item{Item: it, Group: Setup})
	}

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfig) {
		// The setup items already report the configuration as not valid; the sessions' items need
		// it, so they are not checked, and nothing is counted present unproved.
		cfg = nil
	}

	r.Items = append(r.Items, sessionItems(ctx, cfg)...)
	for _, it := range r.Items {
		if !it.Present {
			r.Missing++
		}
	}
	return r
}

// OfEngine is the engine's report, asked for from a client: `engineroom health --json` run on the
// engine over SSH, the way every client action reaches it. An engine not reached is said with its
// cause, and no item is marked present (HEALTH-CHECK-003).
func OfEngine(ctx context.Context, cfg *config.Config) Report {
	remote := action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
	c := remote.Engineroom("health", "--json")
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := Report{Machine: cfg.Engine.Host}
	if source.SSHFailed(err) && !isReport(stdout.Bytes()) {
		u := source.SSHFailure(cfg.Engine.Host, err, stderr.String())
		r.Unreached = u.Headline() + " " + u.Error()
		return r
	}
	// The engine's check ends with a failure when an item is missing, and its report is still on
	// standard output; only a report that cannot be read is a failure of its own.
	var theirs Report
	if jerr := json.Unmarshal(stdout.Bytes(), &theirs); jerr != nil {
		said := strings.TrimSpace(stderr.String())
		if said == "" && err != nil {
			said = err.Error()
		}
		r.Unreached = fmt.Sprintf("the engine %s answered, but its health report could not be read (%v): %s; on the engine, run engineroom health and engineroom setup check", cfg.Engine.Host, jerr, said)
		return r
	}
	theirs.Machine = cfg.Engine.Host
	return theirs
}

// isReport reports whether out is a health report: the engine answered, and its check ended with a
// failure because something is missing.
func isReport(out []byte) bool {
	var r Report
	return json.Unmarshal(out, &r) == nil && r.Items != nil
}

// sessionItems is each running session's Remote Control and app, in name order. The processes, the
// checkouts and the Remote Control choice are each read once for every session.
func sessionItems(ctx context.Context, cfg *config.Config) []Item {
	if cfg == nil {
		return nil
	}
	all, err := zj.List()
	if err != nil {
		return []Item{{Group: Session, Item: setup.Item{Name: "running sessions", For: setup.Engine,
			Checked: strings.Join(zj.ListCommand(), " "), Found: "could not be listed: " + err.Error(),
			Fix: "run engineroom setup check and fix what it reports for Zellij"}}}
	}
	var names []string
	for _, s := range all {
		if !s.Ended {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}

	choice := remoteChoice(cfg)
	agents := agentsBySession()
	apps := ops.Apps(cfg, names)

	items := make([][]Item, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items[i] = append(items[i], remoteItem(name, agents[name], choice))
			if a, ok := apps[name]; ok {
				items[i] = append(items[i], appItem(ctx, name, a))
			}
		}()
	}
	wg.Wait()
	var out []Item
	for _, its := range items {
		out = append(out, its...)
	}
	return out
}

// choice is whether the developer has chosen Remote Control on this machine, and where that was
// read, for the item's words.
type choice struct {
	on    bool
	where string
}

// ClaudeDir is Claude Code's own folder: CLAUDE_CONFIG_DIR when set, as Claude Code reads it, else
// ~/.claude.
func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// remoteChoice is Remote Control chosen on this machine (D-037): engineroom's own choice
// (agent.remote_control, or the app starting sessions with Remote Control), or Claude Code's
// remoteControlAtStartup, which starts every agent with it.
func remoteChoice(cfg *config.Config) choice {
	switch {
	case cfg.Agent.RemoteControl:
		return choice{true, "agent.remote_control is on in engineroom's configuration"}
	case cfg.App.Start == "remote" || cfg.App.Start == "both":
		return choice{true, "the app starts sessions with Remote Control (app.start = " + cfg.App.Start + ")"}
	}
	settings := filepath.Join(ClaudeDir(), "settings.json")
	if raw, err := os.ReadFile(settings); err == nil {
		var s struct {
			RemoteControlAtStartup bool `json:"remoteControlAtStartup"`
		}
		if json.Unmarshal(raw, &s) == nil && s.RemoteControlAtStartup {
			return choice{true, "remoteControlAtStartup is on in " + settings}
		}
	}
	return choice{false, ""}
}

// agent is a Claude Code agent running in a session, and whether it has Remote Control on.
type agent struct {
	pid    int
	remote bool
}

// agentsBySession finds the Claude Code agents running in each Zellij session: a process whose
// environment names the session (ZELLIJ_SESSION_NAME, which Zellij sets on everything started in
// it) and for which Claude Code keeps a file, sessions/<pid>.json in its folder. The file carries
// bridgeSessionId while Remote Control is on (D-037). A process is never picked by its name.
func agentsBySession() map[string][]agent {
	out := map[string][]agent{}
	ps, err := procs.List()
	if err != nil {
		return out
	}
	dir := filepath.Join(ClaudeDir(), "sessions")
	for _, p := range ps {
		session := p.Env["ZELLIJ_SESSION_NAME"]
		if session == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.json", p.PID)))
		if err != nil {
			continue
		}
		var f struct {
			PID    int    `json:"pid"`
			Bridge string `json:"bridgeSessionId"`
		}
		// A file left from an earlier process with the same pid names another pid, or none.
		if json.Unmarshal(raw, &f) != nil || f.PID != p.PID {
			continue
		}
		out[session] = append(out[session], agent{pid: p.PID, remote: f.Bridge != ""})
	}
	return out
}

// remoteItem is a running session's Remote Control (HEALTH-REMOTE-001 and 002): on when an agent in
// it has Remote Control, however it was turned on. It is missing only where Remote Control is
// chosen on this machine; otherwise it is shown, on or off. A running session is never restarted
// for it: the fix is typed inside the session.
func remoteItem(session string, agents []agent, c choice) Item {
	it := Item{Group: Session, Session: session, Item: setup.Item{Name: "Remote Control in " + session, For: setup.Engine,
		Checked: "the Claude Code processes in the session (ZELLIJ_SESSION_NAME=" + session + ") and bridgeSessionId in " +
			filepath.Join(ClaudeDir(), "sessions", "<pid>.json")}}
	var on []string
	for _, a := range agents {
		if a.remote {
			on = append(on, fmt.Sprintf("pid %d", a.pid))
		}
	}
	switch {
	case len(on) > 0:
		it.Present, it.Found = true, "on (Claude "+strings.Join(on, ", ")+")"
		return it
	case len(agents) == 0:
		// No process in it has a Claude Code session file: either no Claude runs in it, or one runs
		// that keeps no file, which cannot be told apart without matching processes by name. So it
		// is said as not read, and not counted as on.
		it.Found = "not read: no process in the session has a Claude Code session file"
		it.Fix = "in the session, type /remote-control to Claude, or if Claude is not running there, start it: claude --continue --remote-control " + session
	default:
		it.Found = "off"
		it.Fix = "in the session, type /remote-control to Claude"
	}
	if !c.on {
		// Not chosen here: shown as it is, never counted missing.
		it.Present, it.Fix = true, ""
		it.Found += "; Remote Control is not chosen on this machine"
		return it
	}
	it.Found += "; chosen because " + c.where
	return it
}

// appItem is a session's app, as its project's status script says (HEALTH-APP-001 to 005).
func appItem(ctx context.Context, session string, a ops.AppCheck) Item {
	it := Item{Group: Session, Session: session, Item: setup.Item{Name: "the app of " + session, For: setup.Engine}}
	switch {
	case !a.HasStatus && !a.HasUp:
		it.Present, it.Checked, it.Found = true, "the project's scripts (no up script, no status script)", "no app to run: "+a.Project+" has no up script"
		return it
	case !a.HasStatus:
		it.Checked = "the project's scripts (an up script, no status script)"
		it.Found = "not checked: " + a.Project + " has no status script"
		it.Fix = strings.ReplaceAll(ops.StatusScriptHelp, "<project>", a.Project)
		return it
	}
	it.Checked = a.Status.Display()
	out, err := runStatus(ctx, a.Status)
	if out == "" {
		out = "(it printed nothing)"
	}
	switch {
	case errors.Is(err, errTimedOut):
		it.Found = fmt.Sprintf("not answering: the status script did not end within %s", StatusTimeout)
		it.Fix = "make the status script end promptly (it must change nothing and end within " + StatusTimeout.String() + "); run it by hand: " + a.Status.Display()
	case err != nil:
		it.Found = "not running: " + out
		if a.HasUp {
			it.Fix = "start the app: engineroom up " + session
			it.Action = []string{"up", session}
		} else {
			it.Fix = "start the app as the project starts it; it has no up script for engineroom to run: " + (&ops.NoScriptError{Project: a.Project, Script: "up"}).Error()
		}
	default:
		it.Present, it.Found = true, "running: "+out
	}
	return it
}

// errTimedOut is a status script that did not end within StatusTimeout.
var errTimedOut = errors.New("timed out")

// runStatus runs a status script and returns the last lines it printed, both streams together. It
// runs in a process group of its own, so a script that does not end is stopped with everything it
// started, and nothing it left behind holds the report.
func runStatus(ctx context.Context, c action.Command) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, StatusTimeout)
	defer cancel()
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A process the script started in a group of its own could keep the output open after the
	// script ends; the output is read for at most this long after that.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return err.Error(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return lastLines(out.String(), 6), err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return lastLines(out.String(), 6), errTimedOut
	}
}

// lastLines keeps the last n non-empty lines of a script's output, joined by "; ", so a long
// status stays one readable line.
func lastLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
}
