package ops

import (
	"os"
	"os/exec"
	"testing"
)

// Every form git writes a GitHub origin in is read as owner/name, and nothing else is taken for
// GitHub. Fails if a GitHub address is missed (no links where there should be), or another host
// or a malformed address is read as GitHub (links to a page that is not there).
func TestGitHubRepo(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:acme/widget.git":           "acme/widget",
		"git@github.com:acme/widget":               "acme/widget",
		"ssh://git@github.com/acme/widget.git":     "acme/widget",
		"ssh://git@github.com:22/acme/widget.git":  "acme/widget",
		"https://github.com/acme/widget":           "acme/widget",
		"https://github.com/acme/widget.git":       "acme/widget",
		"https://user@github.com/acme/widget.git/": "acme/widget",
		"https://GitHub.com/acme/widget":           "acme/widget",
	} {
		if got, ok := gitHubRepo(in); !ok || got != want {
			t.Errorf("%q read as %q (%v), want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", "git@gitlab.com:acme/widget.git", "https://example.com/acme/widget", "git@github.com.example:acme/widget",
		"https://github.com/acme", "https://github.com/acme/widget/tree/main", "/home/dev/src/widget", "engine:src/widget",
	} {
		if got, ok := gitHubRepo(in); ok {
			t.Errorf("%q was read as the GitHub repository %q", in, got)
		}
	}
}

// A branch links to its code, its name kept whole and escaped, and to the pull requests from it;
// the default branch, which pull requests go into, has no pull request link. Fails if a branch
// name with a slash or a space breaks its link, or the default branch is offered pull requests
// from it.
func TestBranchLinks(t *testing.T) {
	l := branchLinks("acme/widget", "feature/x y", "main")
	if l.Code != "https://github.com/acme/widget/tree/feature/x%20y" {
		t.Errorf("code %q", l.Code)
	}
	if l.Pulls != "https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afeature%2Fx+y" {
		t.Errorf("pulls %q", l.Pulls)
	}
	if d := branchLinks("acme/widget", "main", "main"); d.Pulls != "" || d.Code != "https://github.com/acme/widget/tree/main" {
		t.Errorf("default branch %+v", d)
	}
	r := repoLinks("acme/widget")
	if r.Code != "https://github.com/acme/widget" || r.Issues != "https://github.com/acme/widget/issues" || r.Pulls != "https://github.com/acme/widget/pulls" {
		t.Errorf("repo %+v", r)
	}
}

// A clone's GitHub read gives the repository, origin's default branch and the branches GitHub has,
// from origin's refs alone. Fails if origin/HEAD is taken for a branch, a branch with a slash is
// cut short, a branch GitHub does not have is linked, or a clone whose origin is elsewhere is
// read as GitHub.
func TestReadGitHub(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "first")
	git("remote", "add", "origin", "https://example.com/acme/widget.git")
	if _, ok := readGitHub(dir); ok {
		t.Fatal("an origin elsewhere was read as GitHub")
	}
	git("remote", "set-url", "origin", "git@github.com:acme/widget.git")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("update-ref", "refs/remotes/origin/feature/x", "HEAD")
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	c, ok := readGitHub(dir)
	if !ok || c.repo != "acme/widget" || c.defaultBranch != "main" {
		t.Fatalf("read %+v (%v)", c, ok)
	}
	if b := c.branch("feature/x"); b == nil || b.Code != "https://github.com/acme/widget/tree/feature/x" || b.Pulls == "" {
		t.Errorf("feature/x: %+v", b)
	}
	if b := c.branch("main"); b == nil || b.Pulls != "" {
		t.Errorf("the default branch: %+v", b)
	}
	for _, name := range []string{"local-only", "HEAD", "", "(detached)"} {
		if b := c.branch(name); b != nil {
			t.Errorf("%q is linked: %+v", name, b)
		}
	}
}
