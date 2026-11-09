package ops

// A project's page: everything the app's repository page shows for one registered project, read
// in one call on the machine that holds it (the engine, for a client). The project's own settings
// (scripts, ports, secrets), and each of its checkouts (the main clone and every worktree) with
// its branch, its session, its slot, its ports and the addresses its stack serves. Every action
// the page offers is an existing action, named by the session a checkout has or would have.

import (
	"os/exec"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/mux"
	"github.com/KyleyHarris/engineroom/internal/procs"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/signal"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// ScriptView is one lifecycle script and where it comes from; From is empty when the project has
// none.
type ScriptView struct {
	Name string          `json:"name"`
	From settings.Source `json:"from,omitempty"`
	Path string          `json:"path,omitempty"`
}

// TaskView is one of a project's named tasks: its name and what it does.
type TaskView struct {
	Name  string `json:"name"`
	About string `json:"about,omitempty"`
}

// SecretView is one secret file and how it compares with its saved copy (engineroom secrets status).
type SecretView struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// CheckoutView is one checkout of a project: the main clone or a worktree.
type CheckoutView struct {
	// Session is the name of the session this checkout has or would have; every session action
	// (join, up, down, clean-session) takes it.
	Session  string `json:"session"`
	Path     string `json:"path"`
	Checkout string `json:"checkout"`
	Branch   string `json:"branch,omitempty"`
	Main     bool   `json:"main"`
	// Work is the piece of work a worktree holds, for `new <project> <work>`, which opens it.
	Work string `json:"work,omitempty"`
	// Role is set for a worktree kept for a role (D-024): its name, which is also Work. Such a
	// worktree is titled by its role, with the branch it holds now beneath, and moves on to its
	// next branch rather than being closed when one is done.
	Role string `json:"role,omitempty"`
	// Running and Ended say whether its session exists; neither means it has none.
	Running  bool         `json:"running"`
	Ended    bool         `json:"ended"`
	Waiting  signal.State `json:"waiting,omitempty"`
	Question string       `json:"question,omitempty"`
	// Changes counts uncommitted paths.
	Changes int                   `json:"changes"`
	Slot    int                   `json:"slot,omitempty"`
	Ports   []engineroomfile.Port `json:"ports,omitempty"`
	Links   []slots.Link          `json:"links,omitempty"`
	// Clash is set when this checkout's folder has the same name as an earlier checkout's, so
	// both would have the one session name: session and stack actions would reach the other
	// checkout's session, so they are not offered for this one, and this says why.
	Clash string `json:"clash,omitempty"`
	// GitHub is the checkout's branch on GitHub, set only when the project is on GitHub and
	// GitHub has the branch (APP-PAGE-006).
	GitHub *BranchLinks `json:"github,omitempty"`
}

// ProjectPage is one project's page.
type ProjectPage struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Checkout string `json:"checkout"`
	Category string `json:"category,omitempty"`
	Pinned   bool   `json:"pinned,omitempty"`
	Identity string `json:"identity,omitempty"`
	// GitHub is the repository's pages on GitHub, when its origin is there (APP-PAGE-005, 007).
	GitHub   *GitHubLinks `json:"github,omitempty"`
	Settings string       `json:"settings"`
	Reserve  []string     `json:"reserve,omitempty"`
	Slots    int          `json:"slots"`
	Scripts  []ScriptView `json:"scripts"`
	Secrets  []SecretView `json:"secrets,omitempty"`
	// Tasks are the project's own named jobs (engineroomfile.Task), run in a checkout.
	Tasks     []TaskView     `json:"tasks,omitempty"`
	Checkouts []CheckoutView `json:"checkouts"`
	// Problem is a settings file that cannot be read, said instead of leaving the page empty.
	Problem string `json:"problem,omitempty"`
	// Loose are servers on the project's ports that belong to no running session: started by
	// hand outside any session, or for a session that has ended and not yet been swept. They
	// are shown, each with a Stop for that one, and never stopped for the developer (D-020,
	// WORK-STACK-011).
	Loose []LooseServer `json:"loose,omitempty"`
}

// LooseServer is one server on a project's port with no running session to belong to.
type LooseServer struct {
	PID int `json:"pid"`
	// Ports are the project's ports it listens on; a Stop names the first.
	Ports   []int  `json:"ports"`
	Command string `json:"command"`
	Started string `json:"started"`
	Folder  string `json:"folder,omitempty"`
	// Session is the ended session it was started for, when its environment names one; the
	// sweep stops it two minutes after that session went.
	Session string `json:"session,omitempty"`
}

// looseServers finds the servers on project sp's ports whose session is not running.
func looseServers(cfg *config.Config, sp settings.Project, running map[string]bool) []LooseServer {
	all, err := procs.List()
	if err != nil {
		return nil
	}
	keep := procs.Ancestors(all)
	of := sessionOfWorktree(cfg)
	var out []LooseServer
	for _, pr := range all {
		if keep[pr.PID] {
			continue
		}
		owner := pr.Owner(of)
		if owner != "" && running[owner] {
			continue
		}
		var ours []int
		for _, port := range pr.Ports {
			if slots.Owns(sp, port) {
				ours = append(ours, port)
			}
		}
		if len(ours) == 0 {
			continue
		}
		cmd := pr.Command
		if len(cmd) > 90 {
			cmd = cmd[:87] + "..."
		}
		out = append(out, LooseServer{PID: pr.PID, Ports: ours, Command: cmd, Started: pr.Started, Folder: tildeHome(procs.Cwd(pr.PID)), Session: owner})
	}
	return out
}

// Page reads one registered project's page.
func Page(cfg *config.Config, name string) (ProjectPage, error) {
	p, ok := cfg.Project(name)
	if !ok {
		return ProjectPage{}, &NotRegisteredError{Name: name, Registered: cfg.ProjectNames()}
	}
	page := ProjectPage{
		Name: p.Name, Path: p.Path, Checkout: tildeHome(p.Path), Category: p.Category, Pinned: p.Pinned,
		Identity: settings.Identity(p.Path), Settings: tildeHome(settings.Dir(p.Name, p.Path)),
		Scripts: []ScriptView{}, Checkouts: []CheckoutView{},
	}
	for _, s := range []string{"create", "up", "down", "teardown"} {
		v := ScriptView{Name: s}
		if path, from, ok := settings.Script(p.Name, p.Path, p.Path, s); ok {
			v.From, v.Path = from, tildeHome(path)
		}
		page.Scripts = append(page.Scripts, v)
	}
	sp, err := settings.Load(p.Name, p.Path, p.Path)
	if err != nil {
		page.Problem = err.Error()
	} else {
		for _, r := range sp.Reserve {
			page.Reserve = append(page.Reserve, r.String())
		}
		page.Slots = len(sp.Rows)
		for _, t := range sp.Tasks {
			page.Tasks = append(page.Tasks, TaskView{Name: t.Name, About: t.About})
		}
		for _, s := range settings.Secrets(sp, p.Path) {
			page.Secrets = append(page.Secrets, SecretView{Name: s.Name, State: secretState(s)})
		}
	}

	sessions := map[string]zj.Session{}
	if list, err := mux.Active().List(); err == nil {
		for _, s := range list {
			sessions[s.Name] = s
		}
	}
	// The repository's GitHub pages, and the branches GitHub has, read once for every checkout.
	gh, onGitHub := readGitHub(p.Path)
	if onGitHub {
		page.GitHub = repoLinks(gh.repo)
	}
	named := map[string]string{}
	roles, _ := settings.Roles(p.Name, p.Path)
	for _, wt := range worktrees(p.Path) {
		c := CheckoutView{
			Session: zj.SessionName(wt.path), Path: wt.path, Checkout: tildeHome(wt.path), Branch: wt.branch,
			Main: resolvePath(wt.path) == resolvePath(p.Path), Work: workOf(p.Path, wt.path),
		}
		for _, r := range roles {
			if !c.Main && c.Work == r {
				c.Role = r
			}
		}
		if other, taken := named[c.Session]; taken {
			c.Clash = "Its folder has the same name as " + tildeHome(other) + ", so both would use the session name " + c.Session + ". Its session and stack cannot be started from here; rename its folder to give it its own."
		} else {
			named[c.Session] = wt.path
		}
		if s, ok := sessions[c.Session]; ok && c.Clash == "" {
			c.Running, c.Ended = !s.Ended, s.Ended
			if !s.Ended {
				c.Waiting, c.Question = signal.Read(s.Name, cfg.Waiting.Duration, time.Now())
			}
		}
		if out, err := exec.Command("git", "-C", wt.path, "status", "--porcelain").Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					c.Changes++
				}
			}
		}
		c.Slot, c.Ports, c.Links = slotOf(p, wt.path)
		if onGitHub {
			c.GitHub = gh.branch(wt.branch)
		}
		page.Checkouts = append(page.Checkouts, c)
	}
	if err == nil && (len(sp.Reserve) > 0 || len(sp.Rows) > 0) {
		running := map[string]bool{}
		for n, s := range sessions {
			running[n] = !s.Ended
		}
		page.Loose = looseServers(cfg, sp, running)
	}
	return page, nil
}

// secretState is a secret file's comparison in a few words, as engineroom secrets status says it.
func secretState(s settings.SecretState) string {
	switch {
	case !s.IsSaved && !s.InCheckout:
		return "missing"
	case !s.IsSaved:
		return "not saved"
	case !s.InCheckout:
		return "saved, not here"
	case !s.Same:
		return "differs from the saved copy"
	}
	return "saved"
}

type worktree struct{ path, branch string }

// worktrees lists a main clone's checkouts as git does, the main clone first.
func worktrees(main string) []worktree {
	out, err := exec.Command("git", "-C", main, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return []worktree{{path: main}}
	}
	var list []worktree
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, worktree{path: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(list) > 0:
			list[len(list)-1].branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached" && len(list) > 0:
			list[len(list)-1].branch = "(detached)"
		}
	}
	return list
}
