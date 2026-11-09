package journey

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FakeGH stands in for GitHub's command line tool. It answers from bare repositories under
// $HOME/github/<owner>/<repository>.git, which git reaches as git@github.com:<owner>/... through
// the machine's git configuration, so a clone or a push is real git against a real repository
// and no test ever reaches GitHub. Accounts are signed in by a file under $HOME/github/accounts.
// Every call is logged with the token it ran with, so a journey can check which account acted.
const FakeGH = `#!/bin/sh
gh_home="$HOME/github"
echo "${GH_TOKEN:-no-token} $*" >> "$gh_home/log"
case "$1 $2" in
"auth token")
  [ "$3" = "--user" ] && [ -f "$gh_home/accounts/$4" ] && { echo "token-$4"; exit 0; }
  echo "no oauth token found for github.com account $4" >&2; exit 1 ;;
esac
if [ -z "$GH_TOKEN" ]; then
  echo "To get started with GitHub CLI, please run:  gh auth login" >&2; exit 4
fi
case "$1 $2" in
"repo list")
  owner="$3"; first=1; printf '['
  # GitHub lists in an order of its own, not by name; reverse byte order stands in for it, so
  # a list in alphabetical order is the program's doing.
  for r in $(ls -d "$gh_home/$owner"/*.git 2>/dev/null | LC_ALL=C sort -r); do
    [ -d "$r" ] || continue
    name=$(basename "$r" .git); vis=$(cat "$r/engineroom-visibility" 2>/dev/null || echo PRIVATE)
    [ $first = 1 ] || printf ','; first=0
    printf '{"name":"%s","description":"","visibility":"%s","isArchived":false}' "$name" "$vis"
  done
  printf ']\n' ;;
"repo view")
  if [ -d "$gh_home/$3.git" ]; then printf '{"name":"%s"}\n' "${3#*/}"; exit 0; fi
  echo "GraphQL: Could not resolve to a Repository with the name '$3'. (repository)" >&2; exit 1 ;;
"repo create")
  full="$3"; shift 3; vis=PRIVATE; src=""; remote=origin; send=""
  while [ $# -gt 0 ]; do
    case "$1" in
    --public) vis=PUBLIC ;; --private) vis=PRIVATE ;;
    --source) src="$2"; shift ;; --remote) remote="$2"; shift ;; --push) send=1 ;;
    esac; shift
  done
  if [ -d "$gh_home/$full.git" ]; then echo "GraphQL: Name already exists on this account (createRepository)" >&2; exit 1; fi
  git init -q --bare "$gh_home/$full.git" && echo "$vis" > "$gh_home/$full.git/engineroom-visibility" || exit 1
  git -C "$src" remote add "$remote" "git@github.com:$full.git" || exit 1
  if [ -n "$send" ]; then git -C "$src" push -q -u "$remote" HEAD || exit 1; fi
  echo "https://github.com/$full" ;;
*) echo "fake gh: unsupported: $*" >&2; exit 2 ;;
esac
`

// GitHub is this machine's stand-in GitHub.
type GitHub struct {
	m    *Machine
	Home string
}

// FakeGitHub puts the gh stand-in on this machine's PATH, signs in the given accounts, and
// configures git (an identity, and git@github.com: rewritten to the stand-in's repositories).
func (m *Machine) FakeGitHub(accounts ...string) *GitHub {
	m.t.Helper()
	g := &GitHub{m: m, Home: filepath.Join(m.Home, "github")}
	if err := os.MkdirAll(filepath.Join(g.Home, "accounts"), 0o755); err != nil {
		m.t.Fatal(err)
	}
	for _, a := range accounts {
		m.write(filepath.Join(g.Home, "accounts", a), "")
	}
	m.write(filepath.Join(g.Home, "log"), "")
	m.write(filepath.Join(m.Bin, "gh"), FakeGH)
	if err := os.Chmod(filepath.Join(m.Bin, "gh"), 0o755); err != nil {
		m.t.Fatal(err)
	}
	m.write(filepath.Join(m.Home, ".gitconfig"), "[user]\n\tname = dev\n\temail = dev@engine.example\n[url \""+g.Home+"/\"]\n\tinsteadOf = git@github.com:\n[init]\n\tdefaultBranch = main\n")
	return g
}

// AddRepo gives owner a repository on the stand-in, with one commit on main.
func (g *GitHub) AddRepo(owner, name string) {
	g.m.t.Helper()
	bare := filepath.Join(g.Home, owner, name+".git")
	work := filepath.Join(g.m.t.TempDir(), name)
	// The commit is written straight into the bare repository's main branch, from a scratch
	// clone, so the stand-in holds real history for a clone to fetch.
	script := `git init -q --bare "$1" && git init -q "$2" && cd "$2" && echo "$3" > README.md && git add . && git commit -qm init && git push -q "$1" HEAD:main`
	if out, err := g.m.Run("sh", "-c", script, "sh", bare, work, name); err != nil {
		g.m.t.Fatalf("stand-in repository %s/%s: %v\n%s", owner, name, err, out)
	}
}

// Has reports whether owner has the repository on the stand-in.
func (g *GitHub) Has(owner, name string) bool {
	_, err := os.Stat(filepath.Join(g.Home, owner, name+".git"))
	return err == nil
}

// Visibility is the repository's visibility as it was created.
func (g *GitHub) Visibility(owner, name string) string {
	b, _ := os.ReadFile(filepath.Join(g.Home, owner, name+".git", "engineroom-visibility"))
	return strings.TrimSpace(string(b))
}

// Head is the commit the repository's main branch points at, or empty.
func (g *GitHub) Head(owner, name string) string {
	out, err := exec.Command("git", "-C", filepath.Join(g.Home, owner, name+".git"), "rev-parse", "main").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Log is every call made to the stand-in, one per line, each starting with its token.
func (g *GitHub) Log() string {
	b, _ := os.ReadFile(filepath.Join(g.Home, "log"))
	return string(b)
}

// FakeGitHub puts the gh stand-in on the engine, with the given accounts signed in and git
// configured as on a machine (see Machine.FakeGitHub). Its repositories live in the engine's
// /home/dev/github.
func (h *Engine) FakeGitHub(accounts ...string) {
	h.t.Helper()
	h.writeFile("/home/dev/bin/gh", FakeGH, "0755")
	h.writeFile("/home/dev/.gitconfig", "[user]\n\tname = dev\n\temail = dev@engine.example\n[url \"/home/dev/github/\"]\n\tinsteadOf = git@github.com:\n[init]\n\tdefaultBranch = main\n", "0644")
	script := "mkdir -p /home/dev/github/accounts && : > /home/dev/github/log"
	for _, a := range accounts {
		script += " && : > /home/dev/github/accounts/" + a
	}
	h.MustExec(script)
}

// AddGitHubRepo gives owner a repository on the engine's stand-in, with one commit on main.
func (h *Engine) AddGitHubRepo(owner, name string) {
	h.t.Helper()
	h.MustExec(`set -e; bare=/home/dev/github/` + owner + `/` + name + `.git; work=$(mktemp -d); git init -q --bare "$bare"; cd "$work"; git init -q; echo ` + name + ` > README.md; git add .; git commit -qm init; git push -q "$bare" HEAD:main`)
}

// SharedGitHub stands in for the GitHub a client and its engine both push to and fetch from, which
// a handoff needs (CONN-HANDOFF): bare repositories on the engine, which the engine reaches as files
// and the client over the run's own SSH to the engine, each under the address git@github.com:...
// It sets up git on both (identity, and the address rewritten), so a push or a fetch on either
// machine is real git against the same real repository and no journey reaches GitHub.
func SharedGitHub(client *Machine, h *Engine) {
	client.t.Helper()
	h.FakeGitHub()
	client.write(filepath.Join(client.Home, ".gitconfig"), "[user]\n\tname = dev\n\temail = dev@engine.example\n[url \"engine:/home/dev/github/\"]\n\tinsteadOf = git@github.com:\n[init]\n\tdefaultBranch = main\n[core]\n\tsshCommand = ssh -F "+h.SSHConfig+"\n")
}

// GitHubHead is the commit a branch points at on the engine's stand-in GitHub, or empty.
func (h *Engine) GitHubHead(owner, name, branch string) string {
	out, err := h.Exec("git -C /home/dev/github/" + owner + "/" + name + ".git rev-parse " + branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
