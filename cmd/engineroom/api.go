package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/assistant"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/forward"
	"github.com/KyleyHarris/engineroom/internal/handoff"
	"github.com/KyleyHarris/engineroom/internal/history"
	"github.com/KyleyHarris/engineroom/internal/mux"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/roots"
	"github.com/KyleyHarris/engineroom/internal/source"
	"github.com/KyleyHarris/engineroom/internal/terminal"
	"github.com/KyleyHarris/engineroom/internal/upgrade"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// The menu bar app's interface to the program (docs/decisions.md D-008). The app runs nothing itself: every list it
// shows and every action it takes is one of these commands, which print JSON. Each one acts where
// this machine's actions are done (source.For): the engine over SSH for a client that is online, this
// machine otherwise. So the app on the Air and the app on the engine behave as the terminal
// interface does on each, and every action is a plan in the history like any other.

const apiUsage = `usage:
  engineroom api status                          this machine: role, engine, offline, terminals, choices
  engineroom api sessions                        the sessions where actions are done
  engineroom api feed                            the same, as one line now and one on each change; a line in asks for a look now
  engineroom api repos                           every root's repositories, and projects in no root
  engineroom api project <name>                  one project's page: its settings and every checkout
  engineroom api engine-version                  this machine's version and the engine's, which is behind, and the engine's machine name
  engineroom api machine                         this machine's version and its own name
  engineroom api plan <action>                   the commands an action would run, run nothing
  engineroom api run <action>                    run it, printing one JSON event per line
  engineroom api set terminal <name>|start <zellij|remote|both>|offline <on|off>|forward <on|off>
actions:
  start <project> [--mode zellij|remote|both]    new <project> <work> [--mode ...]
  join <session>   stop <session>   restart <session> [--mode ...]   up <session>   down <session>   clean-session <session>
  stop-server <project> <pid> <port>   (a server on the project's port with no running session)
  serve <session> <port>   (start the session's app when nothing answers on its port, before opening a link)
  edit-session <session>   edit-project <project>
    (a session action with --here acts on the session on this machine, listed with "here")
  edit-path <project> <folder>   unregister <project>
  pin <project> on|off   category <project> <category>   hide|unhide <root> <repository>
  upgrade [--engine]   handoff to-engine|from-engine|release   mirror   send-image   assistant <project> <checkout folder> [--mode ...]   task <session> <task name>   commit-partial <project> <folder> [--here]
  clone <root> <repo> [--category c]   create <root> <name> [--category c] [--public]
  register <folder> [--category c]`

func runAPI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, apiUsage)
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	ctx := context.Background()
	src := source.For(cfg)
	switch args[0] {
	case "status":
		return writeJSON(stdout, apiStatus(cfg, src))
	case "sessions":
		// The app's panel shows each session's branch on GitHub (APP-LIST-003), so it asks for
		// the list with those links; nothing else that lists sessions reads them.
		var list []ops.SessionView
		var read string
		var err error
		switch s := src.(type) {
		case source.Remote:
			list, read, err = s.SessionsWithGitHub(ctx)
		case source.Local:
			list, read, err = s.SessionsWithGitHub(ctx)
		default:
			list, read, err = src.Sessions(ctx)
		}
		if err != nil {
			return apiError(stdout, err)
		}
		if _, remote := src.(source.Remote); remote {
			// The engine's sessions, then this machine's own still running (CONN-HANDOFF-006).
			list = append(list, source.HereSessions(cfg)...)
			read += "; on this machine: " + source.HereRead
		}
		if list == nil {
			list = []ops.SessionView{}
		}
		return writeJSON(stdout, map[string]any{"sessions": list, "read": read, "where": src.Where()})
	case "feed":
		return apiFeed(cfg, args[1:], os.Stdin, stdout, stderr)
	case "repos":
		if r, remote := src.(source.Remote); remote {
			// The engine lists its own roots and folders; its answer is passed on whole.
			out, code, err := r.Call(ctx, "api", "repos")
			if errors.Is(err, source.ErrEngineDown) {
				return apiError(stdout, err)
			}
			_, _ = stdout.Write(out)
			return code
		}
		return writeJSON(stdout, repos(cfg))
	case "engine-version":
		// The engine's version, for a client to compare with its own: a client and engine on different
		// builds can disagree about what an answer holds. The engine also gives its own machine name,
		// which the app shows in place of the SSH name this client reaches it by (CONN-ROLE-003); it
		// rides on this read, which the app already makes when its panel opens, so naming the engine
		// costs no connection of its own.
		r, remote := src.(source.Remote)
		if !remote {
			return writeJSON(stdout, map[string]any{"here": version, "engine": version, "behind": "none", "name": machineName()})
		}
		engine, name, err := engineMachine(ctx, r)
		if err != nil {
			return apiError(stdout, err)
		}
		return writeJSON(stdout, map[string]any{"here": version, "engine": engine, "behind": behindBuild(built, version, engine), "name": name})
	case "machine":
		// This machine's own version and name, asked of the engine by a client's engine-version.
		return writeJSON(stdout, map[string]any{"version": version, "name": machineName()})
	case "project":
		if len(args) != 2 {
			fmt.Fprintln(stderr, apiUsage)
			return 2
		}
		if r, remote := src.(source.Remote); remote {
			// The project's page is read where its checkouts are; the engine's answer is passed on.
			out, code, err := r.Call(ctx, "api", "project", args[1])
			if errors.Is(err, source.ErrEngineDown) {
				return apiError(stdout, err)
			}
			_, _ = stdout.Write(out)
			return code
		}
		page, err := ops.Page(cfg, args[1])
		if err != nil {
			return apiError(stdout, err)
		}
		return writeJSON(stdout, page)
	case "plan", "run":
		if len(args) < 2 {
			fmt.Fprintln(stderr, apiUsage)
			return 2
		}
		// A plan that reads machines first (a handover) says how far it has got while the app
		// waits: a preview writes {"progress": step} lines to standard error, so its answer on
		// standard output stays one JSON reply; a run writes {"event": "progress"} events before
		// the plan, in the stream the app already reads.
		if args[0] == "plan" {
			ctx = handoff.WithProgress(ctx, handoff.ProgressLines(stderr))
		} else {
			var mu sync.Mutex
			ctx = handoff.WithProgress(ctx, func(s handoff.Step) {
				mu.Lock()
				defer mu.Unlock()
				_ = json.NewEncoder(stdout).Encode(map[string]any{"event": "progress", "progress": s})
			})
		}
		p, err := apiPlan(ctx, cfg, src, args[1], args[2:])
		if err != nil && args[0] == "run" {
			// A run is read as a stream of events, so a plan that cannot be made is said as the
			// run's outcome, with its reason; a bare error reply would be skipped by a reader of
			// events, and the run would end with no word of why.
			return apiRunRefused(stdout, err)
		}
		if err != nil {
			return apiError(stdout, err)
		}
		if args[0] == "plan" {
			// The lines are the commands as they are shown and pasted, so the app shows exactly
			// what the terminal interface shows.
			return writeJSON(stdout, map[string]any{"plan": p, "lines": p.Lines()})
		}
		return apiRun(p, stdout)
	case "set":
		return apiSet(cfg, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, apiUsage)
	return 2
}

// apiError prints a refusal or failure as JSON with exit 3, as `engineroom plan` does, marking an engine
// that did not answer so the app can offer retry or offline.
func apiError(stdout io.Writer, err error) int {
	reply := map[string]any{"error": err.Error(), "engineDown": errors.Is(err, source.ErrEngineDown)}
	var p *roots.Problem
	if errors.As(err, &p) {
		reply["problem"] = p
	}
	var refused *ops.RefusedError
	if errors.As(err, &refused) && len(refused.Offers) > 0 {
		reply["offers"] = refused.Offers
	}
	writeJSON(stdout, reply)
	return 3
}

func apiStatus(cfg *config.Config, src source.Source) map[string]any {
	chosen, cerr := terminal.Choose(cfg.App.Terminal)
	st := map[string]any{
		"role":      cfg.Role,
		"engine":    cfg.Engine.Host,
		"offline":   cfg.Role == config.RoleClient && cfg.Offline,
		"where":     src.Where(),
		"version":   version,
		"terminals": terminal.Installed(),
		"start":     startMode(cfg, ""),
		// Whether this client forwards every running session's ports here (D-013), and whether it
		// forwards any (every session, or the ones chosen); the app runs `engineroom forward --watch`
		// while it forwards any.
		"forward":    cfg.Role == config.RoleClient && cfg.Forward.Enabled,
		"forwarding": cfg.Role == config.RoleClient && cfg.Forward.Active(),
	}
	if at, ok := handoff.Released(); ok && cfg.Role == config.RoleEngine {
		// Information only (D-017): when the engine last pushed everything for a client to take.
		st["released"] = at
	}
	if cerr != nil {
		st["terminalProblem"] = cerr.Error()
	} else {
		st["terminal"] = chosen.Name
	}
	return st
}

// startMode is how a session is started: the one asked for, else this machine's saved choice,
// else "zellij".
func startMode(cfg *config.Config, asked string) string {
	switch {
	case asked != "":
		return asked
	case cfg.App.Start != "":
		return cfg.App.Start
	}
	return "zellij"
}

// reposReply is every root's overview and the projects in no root (APP-REPOS-001).
type reposReply struct {
	Roots    []roots.Listing   `json:"roots"`
	Projects []ops.ProjectView `json:"projects"`
	// GitHub is each registered project's repository on GitHub, by project name, for the
	// panel's GitHub line (APP-LIST-003); a project not on GitHub is left out. It is read here,
	// with the repositories (hourly or when asked), rather than with the sessions, which the feed
	// looks at every few seconds, as a repository's address hardly ever changes.
	GitHub map[string]*ops.GitHubLinks `json:"github,omitempty"`
}

func repos(cfg *config.Config) reposReply {
	r := reposReply{Roots: []roots.Listing{}, Projects: []ops.ProjectView{}}
	for _, root := range cfg.Roots {
		l, err := roots.Overview(cfg, root.Name)
		if err != nil {
			l = roots.Listing{Root: root, Repos: []roots.Repo{}, Problem: &roots.Problem{Reason: err.Error()}}
		}
		r.Roots = append(r.Roots, l)
	}
	for _, p := range ops.Projects(cfg) {
		if _, inRoot := roots.Of(cfg, p.Path); !inRoot {
			r.Projects = append(r.Projects, p)
		}
		if g := ops.RepoOnGitHub(p.Path); g != nil {
			if r.GitHub == nil {
				r.GitHub = map[string]*ops.GitHubLinks{}
			}
			r.GitHub[p.Name] = g
		}
	}
	return r
}

// takeFlag removes --name value from args and returns the value.
func takeFlag(args []string, name string) (string, []string) {
	var rest []string
	val := ""
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			val = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return val, rest
}

// joinNewRole ends a new role worktree's plan by joining its session in a terminal window here,
// as new branch work does, unless it was started with Remote Control alone ("remote"), which opens
// no window. Any other action's plan is returned as it is.
func joinNewRole(ctx context.Context, cfg *config.Config, src source.Source, act string, rest []string, mode string, p action.Plan, err error) (action.Plan, error) {
	if err != nil || act != "new-role" || startMode(cfg, mode) == "remote" {
		return p, err
	}
	proj, err := findProject(ctx, src, rest[0])
	if err != nil {
		return action.Plan{}, err
	}
	open, err := joinCommand(cfg, zj.SessionName(ops.WorktreePath(proj.Path, rest[1])), false)
	if err != nil {
		return action.Plan{}, err
	}
	p.Commands = append(p.Commands, open)
	return p, nil
}

// apiPlan builds the plan for one action where this machine's actions are done.
func apiPlan(ctx context.Context, cfg *config.Config, src source.Source, act string, args []string) (action.Plan, error) {
	mode, args := takeFlag(args, "--mode")
	category, args := takeFlag(args, "--category")
	public, here, switching := false, false, false
	var rest []string
	for _, a := range args {
		switch a {
		case "--public":
			public = true
			continue
		case "--here":
			here = true
			continue
		case "--switch":
			switching = true
			continue
		}
		rest = append(rest, a)
	}
	if here {
		// A session listed as here is on this machine, whatever this machine's actions
		// usually act on (CONN-HANDOFF-006).
		src = source.Here(cfg)
	}
	need := func(n int) error {
		if len(rest) != n {
			return fmt.Errorf("%s takes %d argument(s)", act, n)
		}
		return nil
	}
	switch act {
	case "start", "new":
		n := 1
		if act == "new" {
			n = 2
		}
		if err := need(n); err != nil {
			return action.Plan{}, err
		}
		return startPlan(ctx, cfg, src, act, rest, startMode(cfg, mode))
	case "join":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		open, err := joinCommand(cfg, rest[0], here)
		if err != nil {
			return action.Plan{}, err
		}
		return action.Plan{Title: "Join " + rest[0], Machine: "this machine", Commands: []action.Command{open}}, nil
	case "handoff":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		if rest[0] == handoff.FromEngine || rest[0] == handoff.ToEngine {
			// With --switch, the switch to working local (from-engine) or back (to-engine) is the
			// handover's last step, so it happens only when the handover is run.
			p, err := handoff.ForClient(ctx, cfg, rest[0])
			// A handover that cannot run blocks the switch too: working local, or back on the
			// engine, starts only once the state it starts from has been handed over.
			if err != nil || !switching || p.Blocked {
				return p, err
			}
			to := map[string]string{handoff.FromEngine: "on", handoff.ToEngine: "off"}[rest[0]]
			p.Commands = append(p.Commands, action.Command{Argv: []string{"engineroom", "offline", "set", to},
				Why: map[string]string{"on": "then work local on this machine", "off": "then work on the engine again"}[to]})
			if len(p.Notes) > 0 && p.Notes[0] == "Nothing needs handing over." {
				p.Notes[0] = "Nothing needs handing over; running this only switches."
			}
			return p, nil
		}
		if rest[0] == "release" {
			// The engine's own release, run on the engine (the app there shows it in Settings).
			if cfg.Role != config.RoleEngine {
				return action.Plan{}, errors.New("a release is the engine's: run it from the app on the engine")
			}
			return releasePlan(cfg), nil
		}
		return handoff.ForClient(ctx, cfg, rest[0])
	case "mirror":
		return handoff.Mirror(ctx, cfg)
	case "send-image":
		return sendImagePlan(cfg)
	case "task":
		// One of a project's own named tasks, run in a session's checkout where it is: on the engine,
		// over SSH from a client.
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		if r, remote := src.(source.Remote); remote {
			return r.PlanOnEngine(ctx, "task", rest[0], rest[1])
		}
		p, err := ops.Task(cfg, rest[0], rest[1])
		p.Machine = src.Where()
		return p, err
	case "assistant":
		// Ask Claude about a project or one of its checkouts (internal/assistant): started on the
		// engine as this machine starts sessions, then joined here in a terminal window unless it
		// started with Remote Control alone.
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		m := startMode(cfg, mode)
		var plan action.Plan
		var err error
		if r, remote := src.(source.Remote); remote {
			plan, err = r.PlanOnEngine(ctx, append([]string{"assistant", rest[0], rest[1]}, source.StartFlags(ops.Start{RemoteControl: m != "zellij"})...)...)
		} else {
			plan, err = assistantPlan(cfg, rest[0], rest[1], m != "zellij")
		}
		if err != nil || m == "remote" {
			return plan, err
		}
		open, err := joinCommand(cfg, assistant.SessionName(rest[1]), false)
		if err != nil {
			return action.Plan{}, err
		}
		plan.Commands = append(plan.Commands, open)
		return plan, nil
	case "upgrade":
		// Upgrade engineroom through a Claude session (internal/upgrade), on this machine, or with
		// --engine on a client, on the engine; started as this machine starts sessions and joined
		// here in a terminal window unless it started with Remote Control alone.
		engine := len(rest) == 1 && rest[0] == "--engine"
		if len(rest) > 1 || (len(rest) == 1 && !engine) {
			return action.Plan{}, errors.New("upgrade takes no argument but --engine")
		}
		m := startMode(cfg, mode)
		var plan action.Plan
		if engine {
			r, remote := src.(source.Remote)
			if !remote {
				return action.Plan{}, errors.New("this machine is the engine, or works local: upgrade it without --engine")
			}
			var err error
			if plan, err = r.PlanOnEngine(ctx, append([]string{"upgrade"}, source.StartFlags(ops.Start{RemoteControl: m != "zellij"})...)...); err != nil {
				return action.Plan{}, err
			}
		} else {
			plan = upgradePlan(cfg, m != "zellij")
		}
		if m == "remote" {
			return plan, nil
		}
		// The engine's session is joined over SSH as any other; this machine's, on a client, is
		// joined here with --here.
		open, err := joinCommand(cfg, upgrade.SessionName, !engine && cfg.Role == config.RoleClient)
		if err != nil {
			return action.Plan{}, err
		}
		plan.Commands = append(plan.Commands, open)
		return plan, nil
	case "new-role", "next-branch", "save-work", "put-aside", "push-work":
		// Worktrees kept for a role, and the ways to keep work that Close and Next branch offer
		// (D-024): planned where the worktrees are, the engine's over SSH from a client.
		n := map[string]int{"new-role": 3, "next-branch": 3}[act]
		if n == 0 {
			n = 2
		}
		if err := need(n); err != nil {
			return action.Plan{}, err
		}
		var p action.Plan
		var err error
		if r, remote := src.(source.Remote); remote {
			args := append([]string{act}, rest...)
			if act == "new-role" {
				args = append(args, source.StartFlags(ops.Start{RemoteControl: startMode(cfg, mode) != "zellij", NoJoin: true})...)
			}
			p, err = r.PlanOnEngine(ctx, args...)
			return joinNewRole(ctx, cfg, src, act, rest, mode, p, err)
		}
		switch act {
		case "new-role":
			p, err = ops.NewRoleWork(cfg, rest[0], rest[1], rest[2], ops.Start{RemoteControl: startMode(cfg, mode) != "zellij", NoJoin: true})
		case "next-branch":
			p, err = ops.NextBranch(cfg, rest[0], rest[1], rest[2])
		case "save-work":
			p, err = ops.SaveWork(cfg, rest[0], rest[1])
		case "put-aside":
			p, err = ops.PutAside(cfg, rest[0], rest[1])
		default:
			p, err = ops.PushWork(cfg, rest[0], rest[1])
		}
		p.Machine = src.Where()
		return joinNewRole(ctx, cfg, src, act, rest, mode, p, err)
	case "commit-partial":
		// A handover's offer: run where the worktree is, the engine's over SSH from a client, this
		// machine's with --here.
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		if r, remote := src.(source.Remote); remote {
			return r.R.WrapPlan(handoff.PartialCommit(rest[1], "the engine")), nil
		}
		host, _ := os.Hostname()
		return handoff.PartialCommit(rest[1], strings.SplitN(host, ".", 2)[0]), nil
	case "stop":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		return src.OnSession(ctx, "stop", rest[0], ops.Start{})
	case "restart":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		// Started as this machine starts sessions; the app joins through a terminal window, so
		// the plan itself starts the session without joining it.
		m := startMode(cfg, mode)
		plan, err := src.OnSession(ctx, "restart", rest[0], ops.Start{RemoteControl: m != "zellij", NoJoin: true})
		if err != nil || m == "remote" {
			return plan, err
		}
		open, err := joinCommand(cfg, rest[0], here)
		if err != nil {
			return action.Plan{}, err
		}
		plan.Commands = append(plan.Commands, open)
		return plan, nil
	case "serve":
		// Opening a session's link (the app's links): start its app first when nothing answers
		// on the link's port where the stack runs, the engine's over SSH from a client.
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		port, err := strconv.Atoi(rest[1])
		if err != nil {
			return action.Plan{}, fmt.Errorf("serve: %q is not a port", rest[1])
		}
		start := ops.Start{RemoteControl: startMode(cfg, mode) != "zellij", NoJoin: true}
		if r, remote := src.(source.Remote); remote {
			return r.PlanOnEngine(ctx, append([]string{"serve", rest[0], rest[1]}, source.StartFlags(start)...)...)
		}
		p, err := ops.Serve(cfg, rest[0], port, start)
		p.Machine = src.Where()
		return p, err
	case "up", "down":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		// A session the stack has to start is started as this machine starts sessions from the
		// app, in the background (WORK-STACK-005).
		return src.Stack(ctx, act, rest[0], ops.Start{RemoteControl: startMode(cfg, mode) != "zellij", NoJoin: true})
	case "stop-server":
		// One server on a project's port that belongs to no running session, chosen by the
		// developer on the project's page (D-020, WORK-STACK-011). Done where the sessions are.
		if err := need(3); err != nil {
			return action.Plan{}, err
		}
		return action.Plan{Title: "Stop the server on port " + rest[2], Machine: src.Where(), Commands: []action.Command{{
			Argv:   []string{"engineroom", "process", "stop", rest[1], "--port", rest[2]},
			Why:    "stop the process listening on " + rest[2] + " for " + rest[0] + ", which belongs to no running session; only if it still listens there",
			OnFail: "It was not stopped: it no longer listens on that port (so it may be another process now), or it would not stop.",
		}}}, nil
	case "clean-session":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		return src.Clean(ctx, "", "", rest[0])
	case "edit-session":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		list, _, err := src.Sessions(ctx)
		if err != nil {
			return action.Plan{}, err
		}
		for _, s := range list {
			if s.Name == rest[0] {
				return src.Edit(s)
			}
		}
		return action.Plan{}, fmt.Errorf("there is no session called %q", rest[0])
	case "edit-project":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		p, err := findProject(ctx, src, rest[0])
		if err != nil {
			return action.Plan{}, err
		}
		// A project's main clone opens like a session's worktree (APP-EDIT-001): directly on the
		// machine that has it, or over SSH from a client.
		return src.Edit(ops.SessionView{Name: p.Name, Project: p.Name, Path: p.Path})
	case "edit-path":
		// Any checkout of a project opens in the editor, with or without a session (the
		// repository page's VS Code button on each worktree).
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		return src.Edit(ops.SessionView{Name: rest[0], Project: rest[0], Path: rest[1]})
	case "hide", "unhide":
		// A repository on GitHub left out of the list, or put back, where the roots are: the
		// engine's, over SSH from a client.
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		title := "Hide " + rest[1] + " from the list"
		if act == "unhide" {
			title = "List " + rest[1] + " again"
		}
		p := action.Plan{Title: title, Machine: src.Where(), Commands: []action.Command{{Argv: []string{"engineroom", "root", act, rest[0], rest[1]}, Why: title + "; nothing changes on GitHub or on disk"}}}
		if r, remote := src.(source.Remote); remote {
			return r.R.WrapPlan(p), nil
		}
		return p, nil
	case "unregister", "pin", "category":
		// The project's registration on the machine that holds it: the engine's, reached over SSH
		// from a client. Nothing on disk is touched.
		argv := []string{"engineroom", "project", "remove", ""}
		title := ""
		switch act {
		case "unregister":
			if err := need(1); err != nil {
				return action.Plan{}, err
			}
			argv[3], title = rest[0], "Unregister "+rest[0]+" (its folder is left as it is)"
		case "pin":
			if err := need(2); err != nil {
				return action.Plan{}, err
			}
			argv, title = []string{"engineroom", "project", "set", rest[0], "pinned", rest[1]}, "Pin "+rest[0]+": "+rest[1]
		default:
			if err := need(2); err != nil {
				return action.Plan{}, err
			}
			argv, title = []string{"engineroom", "project", "set", rest[0], "category", rest[1]}, "List "+rest[0]+" under "+rest[1]
		}
		p := action.Plan{Title: title, Machine: src.Where(), Commands: []action.Command{{Argv: argv, Why: title}}}
		if r, remote := src.(source.Remote); remote {
			return r.R.WrapPlan(p), nil
		}
		return p, nil
	case "clone":
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		return src.Clone(ctx, rest[0], rest[1], category)
	case "create":
		if err := need(2); err != nil {
			return action.Plan{}, err
		}
		return src.Create(ctx, rest[0], rest[1], category, public)
	case "register":
		if err := need(1); err != nil {
			return action.Plan{}, err
		}
		return src.Register(ctx, rest[0], category)
	}
	return action.Plan{}, fmt.Errorf("unknown action %q", act)
}

func findProject(ctx context.Context, src source.Source, name string) (ops.ProjectView, error) {
	views, err := src.Projects(ctx)
	if err != nil {
		return ops.ProjectView{}, err
	}
	for _, v := range views {
		if v.Name == name {
			if v.Path == "" {
				return v, fmt.Errorf("the engine did not report where %s is; update engineroom on the engine", name)
			}
			return v, nil
		}
	}
	return ops.ProjectView{}, &ops.NotRegisteredError{Name: name, Registered: ops.Names(views)}
}

// startPlan starts a project's session (or new work) the chosen way (APP-START-001 to 003):
// Claude Code always runs inside Zellij where the actions are done, so the session can be joined
// from any machine later. "zellij" starts it and opens a terminal window joined to it, "remote"
// starts it with Remote Control and opens nothing, "both" does the two. A session that is
// already running is never restarted (SESS-REMOTE-004): it is joined, or refused for "remote",
// whose Remote Control can only be set when a session starts.
func startPlan(ctx context.Context, cfg *config.Config, src source.Source, act string, args []string, mode string) (action.Plan, error) {
	ok := false
	for _, m := range config.StartModes {
		ok = ok || m == mode
	}
	if !ok {
		return action.Plan{}, fmt.Errorf(`--mode %q is not one of "zellij", "remote" or "both"`, mode)
	}
	p, err := findProject(ctx, src, args[0])
	if err != nil {
		return action.Plan{}, err
	}
	dir := p.Path
	if act == "new" {
		dir = ops.WorktreePath(p.Path, args[1])
	}
	name := zj.SessionName(dir)
	sessions, _, err := src.Sessions(ctx)
	if err != nil {
		return action.Plan{}, err
	}
	for _, s := range sessions {
		// An ended session is not there (D-015): starting it creates it fresh from the current
		// layout, with this start's mode, instead of joining what Zellij kept of it.
		if s.Name != name || s.Ended {
			continue
		}
		if mode == "remote" {
			return action.Plan{}, fmt.Errorf("the session %s already exists, and Remote Control is set when a session starts; join it, or turn Remote Control on inside it with /remote-control", name)
		}
		open, err := joinCommand(cfg, name, false)
		if err != nil {
			return action.Plan{}, err
		}
		return action.Plan{Title: "Join " + name, Machine: "this machine", Commands: []action.Command{open}}, nil
	}
	start := ops.Start{RemoteControl: mode != "zellij", NoJoin: true}
	var plan action.Plan
	if act == "new" {
		plan, err = src.NewWork(ctx, args[0], args[1], start)
	} else {
		plan, err = src.Open(ctx, args[0], start)
	}
	if err != nil {
		return action.Plan{}, err
	}
	if mode != "remote" {
		open, err := joinCommand(cfg, name, false)
		if err != nil {
			return action.Plan{}, err
		}
		plan.Commands = append(plan.Commands, open)
	}
	return plan, nil
}

// joinCommand opens a window of this machine's chosen terminal running `engineroom join <session>`,
// which joins it here or over SSH as this machine's actions are done (APP-JOIN, APP-TERM-001).
// engineroom is named by its full path because a new terminal window's shell may not have it on PATH.
// here joins the session on this machine, for one listed as here (CONN-HANDOFF-006).
//
// When sessions are kept in Rex there is no terminal window to open: the Rex app is the terminal,
// and `engineroom rex show` brings the session up in it (D-029).
func joinCommand(cfg *config.Config, session string, here bool) (action.Command, error) {
	if !mux.Active().InTerminal() {
		argv := []string{"engineroom", "rex", "show", session}
		if here {
			argv = append(argv, "--here")
		}
		return action.Command{Argv: argv, Why: "show the session in the Rex app"}, nil
	}
	t, err := terminal.Choose(cfg.App.Terminal)
	if err != nil {
		return action.Command{}, err
	}
	self, err := os.Executable()
	if err != nil {
		return action.Command{}, err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	argv := []string{self, "join", session}
	if here {
		argv = append(argv, "--here")
	}
	return terminal.Open(t, session, argv), nil
}

// apiRun runs a plan and prints one JSON event per line: the plan, each step as it changes, and
// the outcome, recording it in the history as the terminal interface does. A plan that would
// take over a terminal is refused: the app joins sessions through a terminal window instead.
func apiRun(p action.Plan, stdout io.Writer) int {
	enc := json.NewEncoder(stdout)
	emit := func(v any) { _ = enc.Encode(v) }
	if err := p.Validate(); err != nil {
		emit(map[string]any{"event": "done", "ok": false, "error": err.Error()})
		return 3
	}
	for _, c := range p.Commands {
		if c.Interactive {
			emit(map[string]any{"event": "done", "ok": false, "error": "this action takes over a terminal; join through a terminal window"})
			return 3
		}
	}
	emit(map[string]any{"event": "plan", "plan": p, "lines": p.Lines()})
	entry, herr := history.Begin(p)
	ok := action.Run(context.Background(), p, func(s action.Step) {
		if entry != nil {
			entry.Update(s)
		}
		emit(map[string]any{"event": "step", "index": s.Index, "status": s.Status, "exit": s.Exit, "output": s.Output, "onFail": failText(p, s)})
	})
	done := map[string]any{"event": "done", "ok": ok}
	if entry != nil {
		status := history.Done
		if !ok {
			status = history.Failed
		}
		if err := entry.Finish(status); err != nil {
			done["historyProblem"] = err.Error()
		}
		done["history"] = entry.ID
	}
	if herr != nil {
		done["historyProblem"] = herr.Error()
	}
	emit(done)
	if !ok {
		return 1
	}
	return 0
}

// apiRunRefused ends a run whose plan could not be made: one done event, not ok, carrying the
// reason, so the app shows why nothing ran.
func apiRunRefused(stdout io.Writer, err error) int {
	_ = json.NewEncoder(stdout).Encode(map[string]any{"event": "done", "ok": false, "error": err.Error(), "engineDown": errors.Is(err, source.ErrEngineDown)})
	return 3
}

func failText(p action.Plan, s action.Step) string {
	if s.Status == action.StatusFailed && s.Index >= 0 && s.Index < len(p.Commands) {
		return p.Commands[s.Index].OnFail
	}
	return ""
}

// apiSet saves one of this machine's choices for the app.
func apiSet(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, apiUsage)
		return 2
	}
	switch args[0] {
	case "terminal":
		t, err := terminal.Choose(args[1])
		if err != nil {
			return apiError(stdout, err)
		}
		cfg.App.Terminal = t.Name
	case "start":
		cfg.App.Start = args[1]
	case "offline":
		if cfg.Role != config.RoleClient {
			return apiError(stdout, errors.New("only a client can go offline; this machine is the engine"))
		}
		cfg.Offline = args[1] == "on"
		if cfg.Offline {
			closeForwardsForLocal(cfg)
		}
	case "forward":
		if cfg.Role != config.RoleClient {
			return apiError(stdout, errors.New("only a client forwards the engine's ports; on the engine they are already here"))
		}
		cfg.Forward.Enabled = args[1] == "on"
		if !cfg.Forward.Enabled {
			cfg.Forward.Sessions = nil
			// Turning it off closes every forward now, as `engineroom forward off` does, so no port
			// stays forwarded behind a switch that reads off.
			if err := cfg.Save(); err != nil {
				return apiError(stdout, err)
			}
			m := forward.Master{R: action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}}
			if stopForwards(m, io.Discard, io.Discard) != 0 {
				return apiError(stdout, errors.New("forwarding is off, but closing the forwards failed; see engineroom history"))
			}
		}
	default:
		fmt.Fprintln(stderr, apiUsage)
		return 2
	}
	if err := cfg.Save(); err != nil {
		return apiError(stdout, err)
	}
	return writeJSON(stdout, apiStatus(cfg, source.For(cfg)))
}

// engineMachine asks the engine for its version and its own machine name. An engine whose build
// has no `api machine` (one older than this client, the case the version check exists to report)
// answers with an error there, so its version is read from `engineroom version` instead and its
// name is left empty, and the app falls back to the SSH name. An engine that does not answer at
// all is the error returned.
func engineMachine(ctx context.Context, r source.Remote) (engine, name string, err error) {
	out, _, err := r.Call(ctx, "api", "machine")
	if errors.Is(err, source.ErrEngineDown) {
		return "", "", err
	}
	var m struct {
		Version string `json:"version"`
		Name    string `json:"name"`
	}
	if err == nil && json.Unmarshal(out, &m) == nil && m.Version != "" {
		return m.Version, m.Name, nil
	}
	out, _, err = r.Call(ctx, "version")
	if err != nil {
		return "", "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "engineroom "), "", nil
}

// behindBuild says which of two builds is the older one, so a client tells the machine that is
// behind to update rather than always itself (a client newer than its engine was told to update).
// Versions are commit ids, which have no order of their own, so the answer comes from git's
// ancestry in the clone this binary was built from:
//
//   - "none": the same commit;
//   - "here": this machine's commit is an ancestor of the engine's, so this machine is behind;
//   - "engine": the engine's commit is an ancestor of this machine's;
//   - "apart": neither is an ancestor of the other, as when they were built from different branches;
//   - "unknown": git cannot say: no clone recorded, a build not from a commit ("dev"), or a commit
//     the clone does not have yet. The caller then names both versions and tells neither to update.
//
// A build with uncommitted changes ("-dirty") is compared by the commit it was built on.
func behindBuild(clone, here, engine string) string {
	here, engine = strings.TrimSuffix(here, "-dirty"), strings.TrimSuffix(engine, "-dirty")
	if here == engine {
		return "none"
	}
	if clone == "" || here == "dev" || engine == "dev" {
		return "unknown"
	}
	// Each version is resolved to a commit first: a version git cannot find is "unknown", never
	// mistaken below for "not an ancestor".
	resolve := func(v string) bool {
		return exec.Command("git", "-C", clone, "rev-parse", "--quiet", "--verify", v+"^{commit}").Run() == nil
	}
	if !resolve(here) || !resolve(engine) {
		return "unknown"
	}
	ancestor := func(a, b string) bool {
		return exec.Command("git", "-C", clone, "merge-base", "--is-ancestor", a, b).Run() == nil
	}
	switch {
	case ancestor(here, engine):
		return "here"
	case ancestor(engine, here):
		return "engine"
	default:
		return "apart"
	}
}
