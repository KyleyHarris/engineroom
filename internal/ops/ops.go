// Package ops is what the machine that holds the sessions knows: which sessions exist, which
// project and checkout each belongs to, whether one is waiting, and the plan for each action.
//
// It runs on the engine (and on a client working locally). A client never computes an engine plan
// itself; it asks the engine for one (`engineroom plan ... --json`) and runs it over SSH, so the project
// registry lives in one place and the commands shown are the commands the engine would run.
package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/mux"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/signal"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// SessionView is one session as the list shows it.
type SessionView struct {
	Name     string `json:"name"`
	Project  string `json:"project"`
	Checkout string `json:"checkout"`
	// Path is the checkout's absolute folder on the machine that holds the session, for
	// opening it in the editor. Checkout is the same folder written for the screen.
	Path     string       `json:"path,omitempty"`
	Ended    bool         `json:"ended"`
	Waiting  signal.State `json:"waiting"`
	Question string       `json:"question,omitempty"`
	Created  string       `json:"created"`
	// Here marks a session on this machine listed among the engine's: on a client working on the
	// engine, one started while it was offline and still running (CONN-HANDOFF-006). Its actions
	// are done here, not on the engine.
	Here bool `json:"here,omitempty"`
	// Slot and Ports are the slot the session's checkout runs on and that slot's ports (PORT-FWD):
	// what a client forwards while the session runs. Empty for a project that declares no ports,
	// and for a worktree that holds no slot, whose ports would be the main clone's.
	Slot  int                   `json:"slot,omitempty"`
	Ports []engineroomfile.Port `json:"ports,omitempty"`
	// Links are the addresses the checkout's stack serves, to open from the app: a terminal over
	// SSH cannot copy them out.
	Links []slots.Link `json:"links,omitempty"`
	// Assistant marks a checkout's Ask Claude session (<session>-ai, D-018): it belongs to that
	// checkout's project and folder, has no ports of its own, and is opened or ended, never
	// navigated to as a project.
	Assistant bool `json:"assistant,omitempty"`
	// GitHub is the checkout's branch on GitHub, for the panel's GitHub line (APP-LIST-003): set
	// only for a running session whose project is on GitHub and whose branch GitHub has, as a
	// project page's checkout has it (APP-PAGE-006). The repository's own links come with the
	// repositories list, which is read far less often.
	GitHub *BranchLinks `json:"github,omitempty"`
}

// checkout is one working copy of a registered project, and the branch it has checked out.
type checkout struct {
	project string
	path    string
	branch  string
}

// checkouts maps each session name a checkout would have to that checkout, for every
// registered project's main clone and its worktrees. git is asked rather than the folder
// layout guessed, so worktrees anywhere on disk are found.
func checkouts(cfg *config.Config) map[string]checkout {
	out := map[string]checkout{}
	for _, p := range cfg.Projects {
		// git lists the main clone first; the main clone keeps the registered path, which may be
		// written differently from git's (a symbolic link), and takes its branch from git's entry.
		list := worktrees(p.Path)
		main := checkout{project: p.Name, path: p.Path}
		if len(list) > 0 {
			main.branch = list[0].branch
		}
		out[zj.SessionName(p.Path)] = main
		for _, wt := range list {
			if _, taken := out[zj.SessionName(wt.path)]; !taken {
				out[zj.SessionName(wt.path)] = checkout{project: p.Name, path: wt.path, branch: wt.branch}
			}
		}
	}
	return out
}

// assistantOf finds the checkout an Ask Claude session belongs to: its name is the checkout's
// session name, cut to leave room, followed by "-ai" (assistant.SessionName, which this package
// cannot import, as it imports this one).
func assistantOf(name string, known map[string]checkout) (checkout, bool) {
	base, ok := strings.CutSuffix(name, "-ai")
	if !ok || base == "" {
		return checkout{}, false
	}
	if c, found := known[base]; found {
		return c, true
	}
	if len(base) == zj.MaxNameLength-3 {
		for k, c := range known {
			if strings.HasPrefix(k, base) {
				return c, true
			}
		}
	}
	return checkout{}, false
}

// Sessions lists every session on this machine in its one fixed order (Order). Nothing is read
// from git beyond what finds each session's checkout: the forwarder asks for this list every few
// seconds and the terminal list on each refresh, and neither shows GitHub links.
func Sessions(cfg *config.Config) ([]SessionView, error) {
	return sessions(cfg, nil)
}

// SessionsWithGitHub is Sessions with each running session's branch on GitHub, for the app's
// panel (APP-LIST-003). What each project's clone knows of GitHub is read once per project with a
// running session (two git commands), never once per session.
func SessionsWithGitHub(cfg *config.Config) ([]SessionView, error) {
	read := map[string]*gitHubClone{}
	return sessions(cfg, func(p config.Project) *gitHubClone {
		if g, ok := read[p.Path]; ok {
			return g
		}
		read[p.Path] = gitHubOf(p.Path)
		return read[p.Path]
	})
}

// SessionReader lists the sessions again and again for one long-lived reader, the app's feed
// (D-023): the list itself is read every time, which is cheap (Zellij's list and each project's
// worktrees), while what a project's clone knows of GitHub is kept for maxAge, because it changes
// only when a branch is pushed and costs two git commands per project. A branch pushed a moment
// ago is linked within maxAge.
type SessionReader struct {
	maxAge time.Duration
	at     map[string]time.Time
	github map[string]*gitHubClone
}

// NewSessionReader is a reader that reads each project's GitHub links at most once per maxAge.
func NewSessionReader(maxAge time.Duration) *SessionReader {
	return &SessionReader{maxAge: maxAge, at: map[string]time.Time{}, github: map[string]*gitHubClone{}}
}

// Read is SessionsWithGitHub, with each project's GitHub links as last read when that was less
// than maxAge ago.
func (r *SessionReader) Read(cfg *config.Config) ([]SessionView, error) {
	now := time.Now()
	return sessions(cfg, func(p config.Project) *gitHubClone {
		if at, ok := r.at[p.Path]; ok && now.Sub(at) < r.maxAge {
			return r.github[p.Path]
		}
		r.at[p.Path], r.github[p.Path] = now, gitHubOf(p.Path)
		return r.github[p.Path]
	})
}

// gitHubOf is what the clone at main knows of GitHub, or nil for one that is not on GitHub.
func gitHubOf(main string) *gitHubClone {
	if c, ok := readGitHub(main); ok {
		return &c
	}
	return nil
}

// sessions lists the sessions; github, when given, is how a project's GitHub links are found, and
// a running session in a project's checkout is given its branch's links from it.
func sessions(cfg *config.Config, github func(config.Project) *gitHubClone) ([]SessionView, error) {
	raw, err := mux.Active().List()
	if err != nil {
		return nil, err
	}
	known := checkouts(cfg)
	now := time.Now()
	views := make([]SessionView, 0, len(raw))
	for _, s := range raw {
		v := SessionView{Name: s.Name, Ended: s.Ended, Created: s.Created}
		c, ok := known[s.Name]
		if !ok {
			if a, isAssistant := assistantOf(s.Name, known); isAssistant {
				v.Assistant, v.Project, v.Checkout, v.Path = true, a.project, tildeHome(a.path), a.path
			}
		} else {
			v.Project, v.Checkout, v.Path = c.project, tildeHome(c.path), c.path
			if p, ok := cfg.Project(c.project); ok {
				v.Slot, v.Ports, v.Links = slotOf(p, c.path)
				if github != nil && !s.Ended {
					if g := github(p); g != nil {
						v.GitHub = g.branch(c.branch)
					}
				}
			}
		}
		if !s.Ended {
			v.Waiting, v.Question = signal.Read(s.Name, cfg.Waiting.Duration, now)
		}
		views = append(views, v)
	}
	Order(cfg, views)
	return views, nil
}

// Order puts sessions in the one order every list shows them in, which never depends on what a
// session is doing, so a session waiting on Claude does not jump about (D-022): the pinned
// projects' sessions
// first, then every other session, then Ask Claude sessions, each group by name ignoring case. A
// session keeps its place when Claude starts or stops waiting in it, or when it ends, so the
// cursor and the eye find it where they left it. A worktree's session (<repo>-<work>) follows its
// main clone's (<repo>).
func Order(cfg *config.Config, views []SessionView) {
	pinned := func(project string) bool {
		p, ok := cfg.Project(project)
		return ok && p.Pinned
	}
	group := func(v SessionView) int {
		switch {
		case v.Assistant:
			return 2
		case v.Project != "" && pinned(v.Project):
			return 0
		default:
			return 1
		}
	}
	sort.SliceStable(views, func(i, j int) bool {
		gi, gj := group(views[i]), group(views[j])
		if gi != gj {
			return gi < gj
		}
		li, lj := strings.ToLower(views[i].Name), strings.ToLower(views[j].Name)
		if li != lj {
			return li < lj
		}
		return views[i].Name < views[j].Name
	})
}

// slotOf is the slot a checkout runs on and its ports, or nothing when the project has no ports
// or its settings cannot be read (the list still shows the session; the slot commands say why).
func slotOf(p config.Project, dir string) (int, []engineroomfile.Port, []slots.Link) {
	sp, err := settings.Load(p.Name, p.Path, dir)
	if err != nil || !sp.HasSlots() {
		return 0, nil, nil
	}
	row, _, err := slots.Effective(sp, dir)
	if err != nil || row.Slot == 0 {
		return 0, nil, nil
	}
	return row.Slot, row.Ports, slots.Links(sp, row, dir)
}

// tildeHome writes a path under the home directory as ~/..., which is shorter on screen and
// reads the same on every machine.
func tildeHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return filepath.Join("~", rel)
	}
	return path
}

// NotRegisteredError is opening a project the configuration does not know.
type NotRegisteredError struct {
	Name       string   `json:"name"`
	Registered []string `json:"registered"`
}

func (e *NotRegisteredError) Error() string {
	if len(e.Registered) == 0 {
		return fmt.Sprintf("%q is not a registered project, and no projects are registered", e.Name)
	}
	return fmt.Sprintf("%q is not a registered project. Registered: %s", e.Name, strings.Join(e.Registered, ", "))
}

// Start is how a session that does not exist yet is started. It changes nothing for a session
// that is already running or has ended: those are joined or brought back as they are
// (SESS-REMOTE-004).
type Start struct {
	// RemoteControl starts the agent with Remote Control under the session's own name
	// (SESS-REMOTE-002).
	RemoteControl bool
	// NoJoin creates the session and leaves it running without joining it (SESS-NEW-004).
	NoJoin bool
}

// LayoutFile is where the standard layout is written for new sessions. The layout with Remote
// Control is a file of its own, so the plan names which one a session is created from.
func LayoutFile(remoteControl bool) string {
	name := "standard.kdl"
	if remoteControl {
		name = "standard-remote-control.kdl"
	}
	return filepath.Join(paths.StateDir(), "layouts", name)
}

// Open is the plan for opening a project: join its session if it is running, bring it back
// if it ended, otherwise create it from the standard layout in the project's main clone and
// join it (or leave it running, with start.NoJoin). Opening never creates a second session for
// the same checkout (SESS-NEW-002).
func Open(cfg *config.Config, project string, start Start) (action.Plan, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return action.Plan{}, &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	cmds, err := openCheckout(zj.SessionName(p.Path), p.Path, start)
	if err != nil {
		return action.Plan{}, err
	}
	plan := action.Plan{Title: "Open " + p.Name, Commands: cmds}
	if start.NoJoin {
		plan.Title = "Start " + p.Name
	}
	return plan, nil
}

func find(name string) (*zj.Session, error) {
	all, err := mux.Active().List()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, nil
}

// ErrNoSuchSession is an action on a session that is not on this machine.
var ErrNoSuchSession = errors.New("no such session")

// OnSession is the plan for an action on one session: join, stop, restart, back or switch.
//
// A session is running or it is not there (D-015). Stop ends it fully, deleting what Zellij keeps
// of it. Joining one that ended anyway
// (quit from inside Zellij, or the engine restarted) starts it fresh from the current layout in its
// checkout, never as the old snapshot; Claude Code continues its conversation (SESS-BACK-002).
// Restart is stop and start in one, for a running session on an old layout. start applies to the
// session a join or restart creates.
func OnSession(cfg *config.Config, kind, name string, start Start) (action.Plan, error) {
	s, err := find(name)
	if err != nil {
		return action.Plan{}, err
	}
	if s == nil {
		return action.Plan{}, fmt.Errorf("%w: %q", ErrNoSuchSession, name)
	}
	fresh := func(title string) (action.Plan, error) {
		c, ok := checkouts(cfg)[name]
		if !ok {
			return action.Plan{}, fmt.Errorf("%q is not the session of a registered project's checkout, so engineroom does not know where to start it; end it with: zellij delete-session %s", name, name)
		}
		return action.Plan{Title: title, Commands: append([]action.Command{mux.End(name)}, createCommands(name, c.path, start)...)}, nil
	}
	switch kind {
	case "join", "back":
		if s.Ended {
			return fresh("Start " + name + " again, from the current layout")
		}
		if kind == "back" {
			return action.Plan{}, fmt.Errorf("%q is running; join it instead", name)
		}
		return action.Plan{Title: "Join " + name, Commands: []action.Command{mux.Active().Join(name)}}, nil
	case "restart":
		return fresh("Restart " + name + " with the current layout")
	case "stop":
		// Stopping a checkout's session stops what was started for it too: its down script, then
		// the session, then anything still running in its name (D-020).
		return action.Plan{Title: "Stop " + name, Commands: endCheckoutSession(cfg, name)}, nil
	case "switch":
		if s.Ended {
			return action.Plan{}, fmt.Errorf("%q has ended; bring it back first", name)
		}
		return action.Plan{Title: "Switch to " + name, Commands: []action.Command{mux.Active().Switch(name)}}, nil
	}
	return action.Plan{}, fmt.Errorf("unknown action %q", kind)
}

// ProjectOf is the registered project dir is a checkout of, and that checkout's own folder: the
// main clone or one of its worktrees, as git lists them.
func ProjectOf(cfg *config.Config, dir string) (config.Project, string, bool) {
	want := resolvePath(dir)
	for _, c := range checkouts(cfg) {
		if resolvePath(c.path) == want {
			p, ok := cfg.Project(c.project)
			return p, c.path, ok
		}
	}
	return config.Project{}, "", false
}
