package ops

// GitHub links on a project's page (APP-PAGE-005 to 007): one line of shortcuts to the repository
// on GitHub. A project whose origin is on GitHub links to its code, issues and pull requests; a
// checkout links
// to its branch and the pull requests from it, but only once GitHub has the branch, so no link
// opens a page GitHub cannot show. Everything is read from the local clone (its origin's address
// and the branches it last fetched): no request is made to GitHub to draw a page.

import (
	"net/url"
	"os/exec"
	"strings"
)

// GitHubLinks are a repository's pages on GitHub.
type GitHubLinks struct {
	// Repo is owner/name, as GitHub writes it.
	Repo   string `json:"repo"`
	Code   string `json:"code"`
	Issues string `json:"issues"`
	Pulls  string `json:"pulls"`
}

// BranchLinks are one branch's pages on GitHub: its code, and the pull requests from it, which is
// left out for the repository's default branch, which pull requests go into rather than come from.
type BranchLinks struct {
	Code  string `json:"code"`
	Pulls string `json:"pulls,omitempty"`
}

// gitHubRepo is owner/name when origin is a GitHub repository, from any of the forms git writes
// its address in: git@github.com:o/r.git, ssh://git@github.com/o/r.git, https://github.com/o/r.
func gitHubRepo(origin string) (string, bool) {
	o := strings.TrimSpace(origin)
	var path string
	switch {
	case strings.Contains(o, "://"):
		u, err := url.Parse(o)
		if err != nil || !isGitHubHost(u.Hostname()) {
			return "", false
		}
		path = u.Path
	default:
		// The scp form, [user@]host:path.
		host, p, ok := strings.Cut(o, ":")
		if !ok {
			return "", false
		}
		if i := strings.LastIndex(host, "@"); i >= 0 {
			host = host[i+1:]
		}
		if !isGitHubHost(host) {
			return "", false
		}
		path = p
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "/" + parts[1], true
}

func isGitHubHost(h string) bool {
	h = strings.ToLower(h)
	return h == "github.com" || h == "www.github.com"
}

// repoLinks are repo's pages on GitHub.
func repoLinks(repo string) *GitHubLinks {
	base := "https://github.com/" + repo
	return &GitHubLinks{Repo: repo, Code: base, Issues: base + "/issues", Pulls: base + "/pulls"}
}

// branchLinks are branch's pages in repo; the pull request link is left out for the default branch.
func branchLinks(repo, branch, defaultBranch string) *BranchLinks {
	base := "https://github.com/" + repo
	segs := strings.Split(branch, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	l := &BranchLinks{Code: base + "/tree/" + strings.Join(segs, "/")}
	if branch != defaultBranch {
		l.Pulls = base + "/pulls?q=" + url.QueryEscape("is:pr head:"+branch)
	}
	return l
}

// originOf is the address of a clone's origin, or "" when it has none.
func originOf(main string) string {
	out, err := exec.Command("git", "-C", main, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitHubClone is what one clone knows of its repository on GitHub, as it last fetched it: which
// repository, its default branch (which pull requests go into), and every branch GitHub has. A
// branch sent with git push, or fetched from GitHub, has its refs/remotes/origin ref. Worktrees
// share their main clone's refs, so one read serves every checkout of a project.
type gitHubClone struct {
	repo          string
	defaultBranch string
	pushed        map[string]bool
}

// readGitHub reads what a clone knows of GitHub in at most two git commands, whatever the number
// of checkouts: its origin's address and, when that is on GitHub, origin's refs. It is false
// when origin is not a GitHub repository, so nothing is linked.
func readGitHub(main string) (gitHubClone, bool) {
	repo, ok := gitHubRepo(originOf(main))
	if !ok {
		return gitHubClone{}, false
	}
	c := gitHubClone{repo: repo, pushed: map[string]bool{}}
	// Each of origin's refs as its branch name and, for origin/HEAD alone, the branch it names.
	// A branch name holds no control character, so NUL cannot appear inside one.
	out, err := exec.Command("git", "-C", main, "for-each-ref", "--format=%(refname:lstrip=3)%00%(symref:lstrip=3)", "refs/remotes/origin").Output()
	if err != nil {
		// The repository's own links do not need the refs; only its branches go unlinked.
		return c, true
	}
	for _, line := range strings.Split(string(out), "\n") {
		name, target, _ := strings.Cut(line, "\x00")
		switch {
		case name == "HEAD":
			c.defaultBranch = target
		case name != "":
			c.pushed[name] = true
		}
	}
	return c, true
}

// branch is a branch's pages on GitHub, or nil when GitHub does not have it (APP-PAGE-006): a
// link to a branch GitHub does not have would open nothing.
func (c gitHubClone) branch(name string) *BranchLinks {
	if name == "" || name == "(detached)" || !c.pushed[name] {
		return nil
	}
	return branchLinks(c.repo, name, c.defaultBranch)
}

// RepoOnGitHub is a registered project's repository pages on GitHub, or nil when its origin is
// not there: one git command, for the repositories list the panel reads it from (APP-LIST-003).
func RepoOnGitHub(main string) *GitHubLinks {
	repo, ok := gitHubRepo(originOf(main))
	if !ok {
		return nil
	}
	return repoLinks(repo)
}
