package ops

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// CheckoutFacts is what the panel's compact card says about one checkout of a project, whether
// or not its session runs (docs/decisions.md D-025): which checkout it is, the branch it holds,
// and whether work in it is at risk or done. All of it is read from the checkout on the engine's own
// disk, as git knows it now; nothing is fetched to find it, so "behind" and "merged" are as of
// the last fetch.
type CheckoutFacts struct {
	Session string `json:"session"`
	Path    string `json:"path"`
	Main    bool   `json:"main"`
	// Work is a worktree's name (its folder after <repo>-); Role is set when it is kept for a role.
	Work   string `json:"work,omitempty"`
	Role   string `json:"role,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Changes counts uncommitted paths; Unpushed the commits on no remote; Behind the commits the
	// remote's main has that the checkout lacks.
	Changes  int `json:"changes"`
	Unpushed int `json:"unpushed"`
	Behind   int `json:"behind"`
	// Merged is set for a branch other than main whose every commit is in the remote's main: the
	// branch is done, so a branch worktree can be closed and a role worktree moved on.
	Merged bool `json:"merged,omitempty"`
	// Slot, Ports and Links are the checkout's own ports and the addresses its stack serves on
	// them, whether or not its session runs, so every card has its links. Read from its slot
	// files.
	Slot  int                   `json:"slot,omitempty"`
	Ports []engineroomfile.Port `json:"ports,omitempty"`
	Links []slots.Link          `json:"links,omitempty"`
	// Up and Down say the project has an up or a down script, so the card offers Run app and
	// Stop app near Join.
	Up   bool `json:"up,omitempty"`
	Down bool `json:"down,omitempty"`
}

// Facts is CheckoutFacts for every checkout of each named project, by project name. A project
// that is not registered is left out. It costs about five git commands per checkout, all local.
func Facts(cfg *config.Config, projects []string) map[string][]CheckoutFacts {
	out := map[string][]CheckoutFacts{}
	for _, name := range projects {
		p, ok := cfg.Project(name)
		if !ok {
			continue
		}
		roles, _ := settings.Roles(p.Name, p.Path)
		base, hasRemote := remote(p.Path)
		_, _, up := settings.Script(p.Name, p.Path, p.Path, "up")
		_, _, down := settings.Script(p.Name, p.Path, p.Path, "down")
		var list []CheckoutFacts
		for _, wt := range worktrees(p.Path) {
			f := CheckoutFacts{
				Session: zj.SessionName(wt.path), Path: wt.path, Branch: wt.branch,
				Main: resolvePath(wt.path) == resolvePath(p.Path), Work: workOf(p.Path, wt.path),
				Up: up, Down: down,
			}
			for _, r := range roles {
				if !f.Main && f.Work == r {
					f.Role = r
				}
			}
			f.Slot, f.Ports, f.Links = slotOf(p, wt.path)
			f.Changes = len(gitLines(wt.path, "status", "--porcelain"))
			f.Unpushed = count(wt.path, "rev-list", "--count", "HEAD", "--not", "--remotes")
			if hasRemote {
				f.Behind = count(wt.path, "rev-list", "--count", "HEAD.."+base)
				if f.Branch != "" && !protected.MatchString(f.Branch) {
					f.Merged = mergedInto(wt.path, f.Branch, base)
				}
			}
			list = append(list, f)
		}
		out[p.Name] = list
	}
	return out
}

// mergedInto reports whether branch was merged into base: the remote has the branch, its tip is in
// base, and that tip is not simply one of base's own commits. A branch made from main and never
// given a commit of its own points at a commit on main's first-parent line, which a merge (the
// way pull requests are merged here, with a merge commit) never leaves it on; it is not done.
// Only the last 500 commits of base are looked through, which bounds the cost of each look.
func mergedInto(dir, branch, base string) bool {
	tip := strings.TrimSpace(gitOut(dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch))
	if tip == "" || exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", tip, base).Run() != nil {
		return false
	}
	for _, c := range strings.Fields(gitOut(dir, "rev-list", "--first-parent", "--max-count=500", base)) {
		if c == tip {
			return false
		}
	}
	return true
}

// count is a git command's printed number, or 0 when it fails (a checkout with no remote branch to
// compare with has nothing behind and nothing to count).
func count(dir string, args ...string) int {
	n, err := strconv.Atoi(strings.TrimSpace(gitOut(dir, args...)))
	if err != nil {
		return 0
	}
	return n
}
