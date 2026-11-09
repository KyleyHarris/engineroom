// Package roots brings repositories in from GitHub (acceptance/projects, PROJ-ROOT, BROWSE,
// CLONE and CREATE). A root is a folder on this machine tied to one GitHub owner; the owner's
// repositories are listed from GitHub, cloned into the root one folder each, and registered as
// projects, and new repositories are created there and on GitHub in one action.
//
// Registering a root changes only the program's own configuration. Cloning and creating are
// plans of real commands, run like every other action; the only things this package runs
// itself are reads (listing an owner's repositories, checking a name is free), and each read
// is reported with the command that made it.
package roots

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/shell"
)

// Add registers folder as a root for owner, listed with account (gh's active account when
// empty), under name (the folder's name when empty). The folder must exist: a root is where
// repositories are cloned, so a mistyped path is refused now rather than at the first clone
// (PROJ-ROOT-002). Nothing on disk is created or changed.
func Add(cfg *config.Config, folder, owner, account, name string) (config.Root, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return config.Root{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return config.Root{}, fmt.Errorf("the folder %s does not exist; create it first, then register it as a root", abs)
	}
	if !info.IsDir() {
		return config.Root{}, fmt.Errorf("%s is not a folder", abs)
	}
	abs = resolve(abs)
	if name == "" {
		name = filepath.Base(abs)
	}
	for _, r := range cfg.Roots {
		if resolve(r.Path) == abs {
			return config.Root{}, fmt.Errorf("%s is already the root %s (owner %s)", abs, r.Name, r.Owner)
		}
	}
	if _, taken := cfg.Root(name); taken {
		return config.Root{}, fmt.Errorf("the name %q is already used by another root; choose one with --name", name)
	}
	r := config.Root{Name: name, Path: abs, Owner: owner, Account: account}
	cfg.Roots = append(cfg.Roots, r)
	if err := cfg.Save(); err != nil {
		cfg.Roots = cfg.Roots[:len(cfg.Roots)-1]
		return config.Root{}, err
	}
	return r, nil
}

// Remove forgets a root. Nothing on disk changes, and the projects cloned into it stay
// registered.
func Remove(cfg *config.Config, name string) error {
	for i, r := range cfg.Roots {
		if r.Name == name {
			cfg.Roots = append(cfg.Roots[:i:i], cfg.Roots[i+1:]...)
			return cfg.Save()
		}
	}
	return fmt.Errorf("%q is not a registered root", name)
}

// Of returns the root a folder is inside, if any.
func Of(cfg *config.Config, folder string) (config.Root, bool) {
	f := resolve(folder)
	for _, r := range cfg.Roots {
		rel, err := filepath.Rel(resolve(r.Path), f)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			return r, true
		}
	}
	return config.Root{}, false
}

// GH is the command that runs gh as a root's account. With an account it is `engineroom gh --account
// <account> ...`, which runs gh with that account's token (from `gh auth token --user`), so the
// right account is used without switching gh's active account for everything else on the
// machine. Without one it is gh itself.
func GH(account string, args ...string) []string {
	if account == "" {
		return append([]string{"gh"}, args...)
	}
	return append([]string{"engineroom", "gh", "--account", account}, args...)
}

// Problem is a read that failed, with the command that would put it right.
type Problem struct {
	Reason string `json:"reason"`
	Fix    string `json:"fix,omitempty"`
}

func (p *Problem) Error() string {
	if p.Fix == "" {
		return p.Reason
	}
	return p.Reason + " To fix it, run: " + p.Fix
}

// RunGH runs gh as account and returns its standard output. A gh that is missing, an account
// with no sign-in on this machine, or gh's own failure is a Problem naming the reason and, where
// there is one, the fix (PROJ-BROWSE-002).
func RunGH(account string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, &Problem{Reason: "GitHub's command line tool, gh, is not installed on this machine.", Fix: "brew install gh"}
	}
	cmd := exec.Command("gh", args...)
	cmd.Env = os.Environ()
	if account != "" {
		token, err := exec.Command("gh", "auth", "token", "--user", account).Output()
		if err != nil || strings.TrimSpace(string(token)) == "" {
			return nil, &Problem{Reason: fmt.Sprintf("gh is not signed in to the account %s on this machine.", account), Fix: "gh auth login --hostname github.com"}
		}
		cmd.Env = append(cmd.Env, "GH_TOKEN="+strings.TrimSpace(string(token)))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		p := &Problem{Reason: "gh failed: " + msg}
		if strings.Contains(msg, "auth login") || strings.Contains(strings.ToLower(msg), "authentication") {
			p.Fix = "gh auth login --hostname github.com"
		}
		return stdout.Bytes(), p
	}
	return stdout.Bytes(), nil
}

// Repo is one of an owner's repositories as the browse list shows it.
type Repo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	Archived    bool   `json:"archived,omitempty"`
	// Here is "registered" when the repository's folder in the root is a registered project
	// (Project names it), "folder" when a folder of that name is in the root but is not
	// registered, and empty when it is not on this machine.
	Here    string `json:"here,omitempty"`
	Project string `json:"project,omitempty"`
	// Category is the registered project's category, for listing it under its heading.
	Category string `json:"category,omitempty"`
	// Pinned is the registered project's pin, for listing it in the pinned section.
	Pinned bool `json:"pinned,omitempty"`
	// Local is true for a repository found only in the root's folder, not in the owner's
	// list on GitHub (a local-only repository, or one under another name there).
	Local bool `json:"local,omitempty"`
	// Hidden is a repository on GitHub, not here, that the developer hid from the list.
	Hidden bool `json:"hidden,omitempty"`
}

// Listing is a root's repositories with the command that read them.
type Listing struct {
	Root  config.Root `json:"root"`
	Read  string      `json:"read"`
	Repos []Repo      `json:"repos"`
	// Problem is why the owner's list on GitHub could not be read, in an Overview; the
	// repositories found in the folder are listed all the same.
	Problem *Problem `json:"problem,omitempty"`
}

// Overview is everything in a root: the owner's repositories on GitHub, marked as Browse marks
// them, and every git repository in the root's folder that GitHub's list does not have, marked
// registered or not (APP-REPOS-001, 002). A GitHub that cannot be read is reported in Problem and
// the folder is still listed, so working offline or with a signed-out account still shows what
// is here.
func Overview(cfg *config.Config, rootName string) (Listing, error) {
	l, err := Browse(cfg, rootName)
	if err != nil {
		var p *Problem
		if !errors.As(err, &p) {
			return l, err
		}
		l.Problem, l.Repos = p, []Repo{}
	}
	listed := map[string]bool{}
	for i, r := range l.Repos {
		listed[r.Name] = true
		// Only a repository on GitHub and not here can be hidden: one that is here is work.
		l.Repos[i].Hidden = r.Here == "" && cfg.IsHidden(l.Root.Name, r.Name)
		if r.Project != "" {
			if p, ok := cfg.Project(r.Project); ok {
				l.Repos[i].Category, l.Repos[i].Pinned = p.Category, p.Pinned
			}
		}
	}
	entries, _ := os.ReadDir(l.Root.Path)
	for _, e := range entries {
		dir := filepath.Join(l.Root.Path, e.Name())
		if listed[e.Name()] || !isRepo(dir) {
			continue
		}
		repo := Repo{Name: e.Name(), Here: "folder", Local: true}
		if p, ok := projectAt(cfg, dir); ok {
			repo.Here, repo.Project = "registered", p
			if pr, ok := cfg.Project(p); ok {
				repo.Category, repo.Pinned = pr.Category, pr.Pinned
			}
		}
		l.Repos = append(l.Repos, repo)
	}
	// The folder's repositories join GitHub's in one list, so it is sorted again as a whole.
	SortRepos(l.Repos)
	return l, nil
}

// NameLess is the order every list of repositories and projects is in: alphabetical, ignoring
// case, with names that differ only in case kept in a fixed order so a list never shuffles
// between two reads.
func NameLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// SortRepos puts repositories in NameLess order. GitHub lists an owner's repositories in an
// order of its own, by recent activity, which changes under the developer from one look to the
// next; by name, a repository is where its name says it is. A client applies it to what the engine sent
// too, so the order never depends on which version of the engine answered.
func SortRepos(repos []Repo) {
	sort.SliceStable(repos, func(i, j int) bool { return NameLess(repos[i].Name, repos[j].Name) })
}

// isRepo reports whether dir is the top of a git repository: it holds .git (a folder, or a file
// for a worktree or submodule).
func isRepo(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Register is the plan that registers a folder already on this machine as a project in a
// category (APP-REPOS-002): one command, the same one a developer would type.
func Register(cfg *config.Config, folder, category string) (action.Plan, error) {
	if err := config.ValidCategory(category); err != nil {
		return action.Plan{}, err
	}
	if !isRepo(folder) {
		return action.Plan{}, fmt.Errorf("%s is not a git repository", folder)
	}
	if p, ok := projectAt(cfg, folder); ok {
		return action.Plan{}, fmt.Errorf("%s is already registered as %s", folder, p)
	}
	if _, taken := cfg.Project(filepath.Base(folder)); taken {
		return action.Plan{}, fmt.Errorf("the name %q is already used by another project; register it with: engineroom project add %s --name <name>", filepath.Base(folder), shell.Quote(folder))
	}
	return action.Plan{Title: "Register " + filepath.Base(folder), Commands: []action.Command{register(folder, category)}}, nil
}

// listFields is what is read of each repository.
const listFields = "name,description,visibility,isArchived"

// ListCommand is the command that lists an owner's repositories.
func ListCommand(r config.Root) []string {
	return GH(r.Account, "repo", "list", r.Owner, "--limit", "1000", "--json", listFields)
}

// Browse lists the root's owner's repositories from GitHub, marking each one already here
// (PROJ-BROWSE-001). The read is reported with the command that made it.
func Browse(cfg *config.Config, rootName string) (Listing, error) {
	r, ok := cfg.Root(rootName)
	if !ok {
		return Listing{}, fmt.Errorf("%q is not a registered root", rootName)
	}
	l := Listing{Root: r, Read: shell.Join(ListCommand(r))}
	out, err := RunGH(r.Account, "repo", "list", r.Owner, "--limit", "1000", "--json", listFields)
	if err != nil {
		var p *Problem
		if errors.As(err, &p) {
			p.Reason = fmt.Sprintf("The repositories of %s could not be listed. %s", r.Owner, p.Reason)
		}
		return l, err
	}
	var raw []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
		IsArchived  bool   `json:"isArchived"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return l, fmt.Errorf("reading gh's list of repositories: %w", err)
	}
	for _, x := range raw {
		repo := Repo{Name: x.Name, Description: x.Description, Visibility: strings.ToLower(x.Visibility), Archived: x.IsArchived}
		dir := filepath.Join(r.Path, x.Name)
		if p, ok := projectAt(cfg, dir); ok {
			repo.Here, repo.Project = "registered", p
		} else if _, err := os.Stat(dir); err == nil {
			repo.Here = "folder"
		}
		l.Repos = append(l.Repos, repo)
	}
	if l.Repos == nil {
		l.Repos = []Repo{}
	}
	SortRepos(l.Repos)
	return l, nil
}

func projectAt(cfg *config.Config, dir string) (string, bool) {
	d := resolve(dir)
	for _, p := range cfg.Projects {
		if resolve(p.Path) == d {
			return p.Name, true
		}
	}
	return "", false
}

// repoName is GitHub's rule for a repository name, which is also the folder and the project
// name, so it must satisfy the project name rule as well.
var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// checkNew refuses a repository that cannot be brought into the root as a new folder and a new
// project: a bad name, a folder already in the way (PROJ-CLONE-002), or a project of that name.
func checkNew(cfg *config.Config, r config.Root, name, category string) (string, error) {
	if !repoName.MatchString(name) || strings.HasSuffix(name, ".git") {
		return "", fmt.Errorf("%q is not a repository name: letters, digits, '.', '_' and '-', starting with a letter or digit", name)
	}
	if err := config.ValidCategory(category); err != nil {
		return "", err
	}
	dir := filepath.Join(r.Path, name)
	if _, err := os.Stat(dir); err == nil {
		fix := "engineroom project add " + shell.Quote(dir)
		if category != "" {
			fix += " --category " + shell.Quote(category)
		}
		return "", fmt.Errorf("%s is already there, so nothing was cloned or created and it was left as it is. If it is the repository, register it with: %s", dir, fix)
	}
	if p, ok := cfg.Project(name); ok {
		return "", fmt.Errorf("a project called %s is already registered (%s); nothing was done", name, p.Path)
	}
	return dir, nil
}

// register is the command that registers a folder the plan has just made.
func register(dir, category string) action.Command {
	argv := []string{"engineroom", "project", "add", dir}
	if category != "" {
		argv = append(argv, "--category", category)
	}
	return action.Command{Argv: argv, Why: "register it as a project"}
}

// CloneURL is where an owner's repository is cloned from. Which SSH key reaches it is the
// machine's own git and SSH setup, never the program's.
func CloneURL(owner, repo string) string {
	return "git@github.com:" + owner + "/" + repo + ".git"
}

// Clone is the plan that clones one of the root's owner's repositories into the root and
// registers it in category (PROJ-CLONE-001). Anything in the way is refused before a command
// runs (PROJ-CLONE-002).
func Clone(cfg *config.Config, rootName, repo, category string) (action.Plan, error) {
	r, ok := cfg.Root(rootName)
	if !ok {
		return action.Plan{}, fmt.Errorf("%q is not a registered root", rootName)
	}
	dir, err := checkNew(cfg, r, repo, category)
	if err != nil {
		return action.Plan{}, err
	}
	return action.Plan{Title: fmt.Sprintf("Add %s/%s", r.Owner, repo), Commands: []action.Command{
		{
			Argv:   []string{"git", "clone", CloneURL(r.Owner, repo), dir},
			Why:    "clone the repository into the root " + r.Name,
			OnFail: "Nothing was registered. git removes a clone it could not finish; if a folder was left at " + dir + ", remove it before trying again.",
		},
		withFail(register(dir, category), "The repository was cloned to "+dir+" but not registered. Register it with: "+shell.Join(register(dir, category).Argv)),
	}}, nil
}

// Exists says whether the owner already has a repository of this name on GitHub, read with the
// root's account. "Could not resolve" from gh is the answer no; any other failure is a Problem.
func Exists(r config.Root, name string) (bool, error) {
	_, err := RunGH(r.Account, "repo", "view", r.Owner+"/"+name, "--json", "name")
	if err == nil {
		return true, nil
	}
	var p *Problem
	if errors.As(err, &p) && strings.Contains(p.Reason, "Could not resolve to a Repository") {
		return false, nil
	}
	return false, err
}

// Create is the plan that makes a new repository: its folder in the root with an empty first
// commit, the repository on GitHub with that commit pushed, and the project registered
// (PROJ-CREATE-001). A name the owner already has, or one that is otherwise in the way, is
// refused before anything is made (PROJ-CREATE-002).
func Create(cfg *config.Config, rootName, name, category string, public bool) (action.Plan, error) {
	r, ok := cfg.Root(rootName)
	if !ok {
		return action.Plan{}, fmt.Errorf("%q is not a registered root", rootName)
	}
	dir, err := checkNew(cfg, r, name, category)
	if err != nil {
		return action.Plan{}, err
	}
	taken, err := Exists(r, name)
	if err != nil {
		return action.Plan{}, fmt.Errorf("could not check whether %s/%s exists on GitHub, so nothing was created: %w", r.Owner, name, err)
	}
	if taken {
		return action.Plan{}, fmt.Errorf("%s already has a repository called %s on GitHub, so nothing was created; choose another name, or add it from the list instead", r.Owner, name)
	}
	visibility := "--private"
	if public {
		visibility = "--public"
	}
	create := GH(r.Account, "repo", "create", r.Owner+"/"+name, visibility, "--source", dir, "--remote", "origin", "--push")
	return action.Plan{Title: fmt.Sprintf("Create %s/%s", r.Owner, name), Commands: []action.Command{
		{Argv: []string{"git", "init", "-q", "-b", "main", dir}, Why: "make the repository's folder in the root " + r.Name},
		{
			Argv:   []string{"git", "-C", dir, "commit", "--allow-empty", "-q", "-m", "First commit"},
			Why:    "an empty first commit, so there is something to push",
			OnFail: dir + " was made but has no commit, and nothing was created on GitHub. Set git's user.name and user.email if it asked for them, then remove the folder and try again.",
		},
		{
			Argv:   create,
			Why:    "create the repository on GitHub and push the first commit",
			OnFail: dir + " was made with its first commit, but the repository was not created on GitHub. Run this command again, or remove the folder.",
		},
		withFail(register(dir, category), "The repository was created and pushed but not registered. Register it with: "+shell.Join(register(dir, category).Argv)),
	}}, nil
}

func withFail(c action.Command, onFail string) action.Command {
	c.OnFail = onFail
	return c
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
