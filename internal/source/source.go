// Package source is where the interface gets sessions and plans from: this machine directly
// (the engine, or a client working locally), or the engine over SSH (a client).
//
// Both kinds hand back plans of real commands. A remote plan is the engine's own plan with every
// command wrapped in the ssh command that runs it there, so what a client shows is exactly what
// it runs.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/editor"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/roots"
)

// Source reads sessions and builds plans.
type Source interface {
	// Sessions lists the sessions, and returns the command that read them, for the screen.
	Sessions(ctx context.Context) ([]ops.SessionView, string, error)
	// Open is the plan for opening a project; start says how a session it creates is started.
	Open(ctx context.Context, project string, start ops.Start) (action.Plan, error)
	// OnSession is the plan for join, stop, back or switch on one session.
	OnSession(ctx context.Context, kind, name string, start ops.Start) (action.Plan, error)
	// NewWork is the plan for starting a piece of work on a project.
	NewWork(ctx context.Context, project, work string, start ops.Start) (action.Plan, error)
	// Clean is the plan for cleaning up a piece of work, named by project and work, or by its
	// session when project is empty.
	Clean(ctx context.Context, project, work, session string) (action.Plan, error)
	// Edit is the plan for opening a listed session's worktree in the editor on this machine.
	// It is built from the session as listed, with no further request (editor obligation O9).
	Edit(s ops.SessionView) (action.Plan, error)
	// Projects lists the registered projects this source can open, in list order with the
	// heading each is listed under.
	Projects(ctx context.Context) ([]ops.ProjectView, error)
	// Stack is the plan for starting ("up") or stopping ("down") the app stack of a session's
	// checkout, with the project's up or down script (the developer's own, or the repository's).
	Stack(ctx context.Context, kind, session string, start ops.Start) (action.Plan, error)
	// Roots lists the registered roots.
	Roots(ctx context.Context) ([]config.Root, error)
	// Browse lists a root's owner's repositories on GitHub, marking the ones already here. A
	// failure to read GitHub is a *roots.Problem carrying its fix.
	Browse(ctx context.Context, root string) (roots.Listing, error)
	// Clone is the plan for cloning one of a root's owner's repositories and registering it.
	Clone(ctx context.Context, root, repo, category string) (action.Plan, error)
	// Create is the plan for making a new repository in a root and on GitHub.
	Create(ctx context.Context, root, name, category string, public bool) (action.Plan, error)
	// Register is the plan for registering a folder already on the machine as a project.
	Register(ctx context.Context, folder, category string) (action.Plan, error)
	// Where names the machine, for the screen.
	Where() string
}

// ErrEngineDown means the engine was not reached. An *Unreached says why (no answer, a name that does
// not resolve, no ssh, a changed or unknown host key); Retryable says whether asking again may help.
var ErrEngineDown = errors.New("the engine did not answer")

// Local is this machine.
type Local struct {
	Cfg *config.Config
	// Label is how the screen names this machine ("engine", or "this machine" when working
	// locally from a client).
	Label string
}

// Sessions implements Source.
func (l Local) Sessions(context.Context) ([]ops.SessionView, string, error) {
	s, err := ops.Sessions(l.Cfg)
	return s, "zellij list-sessions --no-formatting", err
}

// SessionsWithGitHub is this machine's sessions with each one's branch on GitHub, for the app's
// panel (APP-LIST-003).
func (l Local) SessionsWithGitHub(context.Context) ([]ops.SessionView, string, error) {
	s, err := ops.SessionsWithGitHub(l.Cfg)
	return s, "zellij list-sessions --no-formatting", err
}

// Open implements Source.
func (l Local) Open(_ context.Context, project string, start ops.Start) (action.Plan, error) {
	p, err := ops.Open(l.Cfg, project, start)
	p.Machine = l.Label
	return p, err
}

// OnSession implements Source.
func (l Local) OnSession(_ context.Context, kind, name string, start ops.Start) (action.Plan, error) {
	p, err := ops.OnSession(l.Cfg, kind, name, start)
	p.Machine = l.Label
	return p, err
}

// NewWork implements Source.
func (l Local) NewWork(_ context.Context, project, work string, start ops.Start) (action.Plan, error) {
	p, err := ops.NewWork(l.Cfg, project, work, start)
	p.Machine = l.Label
	return p, err
}

// Clean implements Source.
func (l Local) Clean(_ context.Context, project, work, session string) (action.Plan, error) {
	var p action.Plan
	var err error
	if project == "" {
		p, err = ops.CleanSession(l.Cfg, session)
	} else {
		p, err = ops.CleanWork(l.Cfg, project, work)
	}
	p.Machine = l.Label
	return p, err
}

// Edit implements Source: the folder is on this machine, so the editor opens it directly.
func (l Local) Edit(s ops.SessionView) (action.Plan, error) {
	p, err := editor.Plan(l.Cfg.Editor.Command, s, "", exec.LookPath)
	p.Machine = l.Label
	return p, err
}

// Projects implements Source.
func (l Local) Projects(context.Context) ([]ops.ProjectView, error) { return ops.Projects(l.Cfg), nil }

// Where implements Source.
func (l Local) Where() string { return l.Label }

// Stack implements Source.
func (l Local) Stack(_ context.Context, kind, session string, start ops.Start) (action.Plan, error) {
	p, err := ops.Stack(l.Cfg, kind, session, start)
	p.Machine = l.Label
	return p, err
}

// Register implements Source.
func (l Local) Register(_ context.Context, folder, category string) (action.Plan, error) {
	p, err := roots.Register(l.Cfg, folder, category)
	p.Machine = l.Label
	return p, err
}

// Roots implements Source.
func (l Local) Roots(context.Context) ([]config.Root, error) { return l.Cfg.Roots, nil }

// Browse implements Source.
func (l Local) Browse(_ context.Context, root string) (roots.Listing, error) {
	return roots.Browse(l.Cfg, root)
}

// Clone implements Source.
func (l Local) Clone(_ context.Context, root, repo, category string) (action.Plan, error) {
	p, err := roots.Clone(l.Cfg, root, repo, category)
	p.Machine = l.Label
	return p, err
}

// Create implements Source.
func (l Local) Create(_ context.Context, root, name, category string, public bool) (action.Plan, error) {
	p, err := roots.Create(l.Cfg, root, name, category, public)
	p.Machine = l.Label
	return p, err
}

// Remote is the engine, reached over SSH.
type Remote struct {
	R action.Remote
	// Editor is this machine's editor command, which opens the engine's folders over SSH.
	Editor string
}

// Where implements Source.
func (r Remote) Where() string { return "engine " + r.R.Host }

// Probe checks the engine answers. When it does not, the error is an *Unreached (ErrEngineDown)
// naming the cause: no answer, a name that does not resolve, no ssh here, or a host key this machine
// does not know or knows differently.
func (r Remote) Probe(ctx context.Context) error {
	c := r.R.Probe()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return SSHFailure(r.R.Host, err, stderr.String())
	}
	return nil
}

// engineroom runs `engineroom <args>` on the engine and returns its standard output. ssh's own failure
// (exit 255, or ssh not run at all) is an *Unreached naming the cause, which is ErrEngineDown; anything
// else is the engine's answer.
func (r Remote) engineroom(ctx context.Context, args ...string) ([]byte, int, error) {
	c := r.R.Engineroom(args...)
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if SSHFailed(err) {
		return nil, 255, SSHFailure(r.R.Host, err, stderr.String())
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout.Bytes(), ee.ExitCode(), fmt.Errorf("on the engine: %s", strings.TrimSpace(stderr.String()+" "+stdout.String()))
	}
	return stdout.Bytes(), 0, nil
}

// Call runs `engineroom <args>` on the engine and returns what it printed and its exit code. An engine that
// does not answer is ErrEngineDown. It is for reads whose answer is passed on as it is (the menu bar
// app's JSON), so the engine's own reply, refusal included, reaches the caller unchanged.
func (r Remote) Call(ctx context.Context, args ...string) ([]byte, int, error) {
	return r.engineroom(ctx, args...)
}

// Sessions implements Source.
func (r Remote) Sessions(ctx context.Context) ([]ops.SessionView, string, error) {
	return r.sessions(ctx, "sessions", "--json")
}

// SessionsWithGitHub is the engine's sessions with each one's branch on GitHub, for the app's panel
// (APP-LIST-003). An engine older than --github ignores it and answers without the links.
func (r Remote) SessionsWithGitHub(ctx context.Context) ([]ops.SessionView, string, error) {
	return r.sessions(ctx, "sessions", "--json", "--github")
}

func (r Remote) sessions(ctx context.Context, args ...string) ([]ops.SessionView, string, error) {
	shown := r.R.Engineroom(args...).Display()
	out, _, err := r.engineroom(ctx, args...)
	if err != nil {
		return nil, shown, err
	}
	var s []ops.SessionView
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, shown, fmt.Errorf("reading the engine's session list: %w", err)
	}
	return s, shown, nil
}

// planReply is what `engineroom plan ... --json` prints.
type planReply struct {
	Plan          *action.Plan            `json:"plan,omitempty"`
	Error         string                  `json:"error,omitempty"`
	NotRegistered *ops.NotRegisteredError `json:"notRegistered,omitempty"`
	// Offers come with a refusal that something would make safe (ops.RefusedError, D-024).
	Offers []action.Offer `json:"offers,omitempty"`
}

// PlanOnEngine asks the engine for the plan of an action its `engineroom plan` knows, and wraps it to run from
// here, for an action the Source interface does not name (the assistant).
func (r Remote) PlanOnEngine(ctx context.Context, args ...string) (action.Plan, error) {
	return r.plan(ctx, args...)
}

func (r Remote) plan(ctx context.Context, args ...string) (action.Plan, error) {
	out, code, err := r.engineroom(ctx, append(append([]string{"plan"}, args...), "--json")...)
	if errors.Is(err, ErrEngineDown) {
		return action.Plan{}, err
	}
	var reply planReply
	if jerr := json.Unmarshal(out, &reply); jerr != nil {
		if err != nil {
			return action.Plan{}, err
		}
		return action.Plan{}, fmt.Errorf("reading the engine's plan: %w", jerr)
	}
	if reply.NotRegistered != nil {
		return action.Plan{}, reply.NotRegistered
	}
	if reply.Error != "" && len(reply.Offers) > 0 {
		// The engine's refusal and what it offers, kept together, so the offers reach the app.
		return action.Plan{}, &ops.RefusedError{Reason: "on the engine: " + reply.Error, Offers: reply.Offers}
	}
	if reply.Error != "" || code != 0 || reply.Plan == nil {
		return action.Plan{}, fmt.Errorf("on the engine: %s", reply.Error)
	}
	return r.R.WrapPlan(*reply.Plan), nil
}

// Open implements Source.
// The client's own start choices travel with the request as flags, so the engine's configuration
// never decides them for a client (SESS-REMOTE-003).
func (r Remote) Open(ctx context.Context, project string, start ops.Start) (action.Plan, error) {
	return r.plan(ctx, append([]string{"open", project}, StartFlags(start)...)...)
}

// OnSession implements Source.
func (r Remote) OnSession(ctx context.Context, kind, name string, start ops.Start) (action.Plan, error) {
	return r.plan(ctx, append([]string{kind, name}, StartFlags(start)...)...)
}

// NewWork implements Source.
func (r Remote) NewWork(ctx context.Context, project, work string, start ops.Start) (action.Plan, error) {
	return r.plan(ctx, append([]string{"new", project, work}, StartFlags(start)...)...)
}

// StartFlags is how start is written on the command line of `engineroom plan`, which reads it back
// with ParseStartFlags. The two live together so the writer and the reader cannot disagree.
func StartFlags(start ops.Start) []string {
	var out []string
	if start.RemoteControl {
		out = append(out, "--remote-control")
	}
	if start.NoJoin {
		out = append(out, "--no-join")
	}
	return out
}

// ParseStartFlags takes the start flags out of args and returns them with what is left.
func ParseStartFlags(args []string) (ops.Start, []string) {
	var start ops.Start
	var rest []string
	for _, a := range args {
		switch a {
		case "--remote-control":
			start.RemoteControl = true
		case "--no-join":
			start.NoJoin = true
		default:
			rest = append(rest, a)
		}
	}
	return start, rest
}

// Clean implements Source.
func (r Remote) Clean(ctx context.Context, project, work, session string) (action.Plan, error) {
	if project == "" {
		return r.plan(ctx, "clean-session", session)
	}
	return r.plan(ctx, "clean", project, work)
}

// Edit implements Source: the editor runs here, on the client, and reaches the engine's folder
// through its own SSH remote. Nothing runs on the engine, so the plan is not wrapped in ssh.
func (r Remote) Edit(s ops.SessionView) (action.Plan, error) {
	p, err := editor.Plan(r.Editor, s, r.R.Host, exec.LookPath)
	p.Machine = "this machine"
	return p, err
}

// Projects implements Source.
func (r Remote) Projects(ctx context.Context) ([]ops.ProjectView, error) {
	out, _, err := r.engineroom(ctx, "projects", "--json", "--detail")
	if err != nil {
		return nil, err
	}
	return ParseProjects(out)
}

// Stack implements Source.
func (r Remote) Stack(ctx context.Context, kind, session string, start ops.Start) (action.Plan, error) {
	// The client's own Remote Control choice goes with the request, as for any start
	// (SESS-REMOTE-003), for the session starting the stack may have to start.
	return r.plan(ctx, append([]string{kind, session}, StartFlags(start)...)...)
}

// Register implements Source.
func (r Remote) Register(ctx context.Context, folder, category string) (action.Plan, error) {
	args := []string{"register", folder}
	if category != "" {
		args = append(args, "--category", category)
	}
	return r.plan(ctx, args...)
}

// Roots implements Source.
func (r Remote) Roots(ctx context.Context) ([]config.Root, error) {
	out, _, err := r.engineroom(ctx, "roots", "--json")
	if err != nil {
		return nil, err
	}
	var list []config.Root
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("reading the engine's roots: %w", err)
	}
	return list, nil
}

// Browse implements Source. The engine reads GitHub with its own gh and accounts; a problem it
// found comes back with its fix, which is a fix to run on the engine.
func (r Remote) Browse(ctx context.Context, root string) (roots.Listing, error) {
	out, _, err := r.engineroom(ctx, "browse", root, "--json")
	if errors.Is(err, ErrEngineDown) {
		return roots.Listing{}, err
	}
	var reply struct {
		Listing *roots.Listing `json:"listing"`
		Error   string         `json:"error"`
		Problem *roots.Problem `json:"problem"`
	}
	if jerr := json.Unmarshal(out, &reply); jerr != nil {
		if err != nil {
			return roots.Listing{}, err
		}
		return roots.Listing{}, fmt.Errorf("reading the engine's list of repositories: %w", jerr)
	}
	switch {
	case reply.Problem != nil:
		reply.Problem.Reason = "On the engine: " + reply.Problem.Reason
		return roots.Listing{}, reply.Problem
	case reply.Error != "" || reply.Listing == nil:
		return roots.Listing{}, fmt.Errorf("on the engine: %s", reply.Error)
	}
	reply.Listing.Read = r.R.Engineroom("browse", root, "--json").Display()
	roots.SortRepos(reply.Listing.Repos)
	return *reply.Listing, nil
}

// Clone implements Source.
func (r Remote) Clone(ctx context.Context, root, repo, category string) (action.Plan, error) {
	args := []string{"clone", root, repo}
	if category != "" {
		args = append(args, "--category", category)
	}
	return r.plan(ctx, args...)
}

// Create implements Source.
func (r Remote) Create(ctx context.Context, root, name, category string, public bool) (action.Plan, error) {
	args := []string{"create", root, name}
	if category != "" {
		args = append(args, "--category", category)
	}
	if public {
		args = append(args, "--public")
	}
	return r.plan(ctx, args...)
}

// ParseProjects reads the engine's answer to `engineroom projects --json --detail`: the projects with
// their headings, or, from an engine older than categories (which ignores --detail), the names
// alone, listed with no heading (PROJ-CAT-002).
func ParseProjects(out []byte) ([]ops.ProjectView, error) {
	var views []ops.ProjectView
	if err := json.Unmarshal(out, &views); err == nil {
		ops.SortProjects(views)
		return views, nil
	}
	var names []string
	if err := json.Unmarshal(out, &names); err != nil {
		return nil, fmt.Errorf("reading the engine's projects: %w", err)
	}
	views = make([]ops.ProjectView, len(names))
	for i, n := range names {
		views[i] = ops.ProjectView{Name: n}
	}
	return views, nil
}

// HereSessions is, for a client working on the engine, the sessions still running on this machine
// in a worktree of one of its own registered projects, marked Here. They were started while it
// was offline and keep running when it goes back online (CONN-OFFLINE-003), so they are listed
// with the engine's rather than lost from sight (CONN-HANDOFF-006). Other Zellij sessions on the
// machine are not the program's and are not listed. A Zellij that cannot be read here lists
// none: the engine's list is what was asked for.
func HereSessions(cfg *config.Config) []ops.SessionView {
	if cfg == nil || cfg.Role != config.RoleClient || cfg.Offline || len(cfg.Projects) == 0 {
		return nil
	}
	list, err := ops.Sessions(cfg)
	if err != nil {
		return nil
	}
	var out []ops.SessionView
	for _, s := range list {
		if s.Project != "" && !s.Ended {
			s.Here = true
			out = append(out, s)
		}
	}
	return out
}

// HereRead is how HereSessions reads, for the screen that lists them.
const HereRead = "zellij list-sessions --no-formatting"

// Here is this machine, for acting on a session listed as Here.
func Here(cfg *config.Config) Local { return Local{Cfg: cfg, Label: "this machine"} }

// For is where this machine's actions are done: the engine over SSH for a client, unless the
// client is switched offline, in which case it is this machine as though it were the engine
// (CONN-OFFLINE-001); this machine for an engine.
func For(cfg *config.Config) Source {
	if cfg.Role == config.RoleClient && !cfg.Offline {
		return Remote{R: action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}, Editor: cfg.Editor.Command}
	}
	if cfg.Role == config.RoleClient {
		return Local{Cfg: cfg, Label: "this machine"}
	}
	return Local{Cfg: cfg, Label: "engine"}
}
