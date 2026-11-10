// Package handoff hands the work over between a client and its engine when the client switches to
// working local or back (CONN-HANDOFF-001 to 017): afterwards the receiving machine is in the
// giving machine's state, checkout by checkout.
//
// Each machine has its own worktrees. A handoff reads, on each machine, every registered project's
// branches against GitHub and its worktrees (with a fingerprint of each one's state), and from the
// two readings builds one plan of real git commands, or none, when anything would stop it (Build):
// check both machines are as read and GitHub takes every push, push the giving machine's
// checked-out branches, set each receiving checkout's own work aside in its stash list and put it on
// the giving branch at the giving commit, then carry each giving checkout's uncommitted work across
// as a stash, straight between the two machines over SSH and never through GitHub (D-017).
// The developer sees the plan and runs it, or not.
package handoff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
)

// State is what one machine holds: each registered project's repository against GitHub, and the
// sessions running there.
type State struct {
	// Home is the machine's home folder, so a project missing on the other machine is cloned to
	// the same place under that machine's home (the mirror).
	Home  string `json:"home,omitempty"`
	Repos []Repo `json:"repos"`
	// Sessions are the sessions running on the machine in a registered project's worktree. Going
	// back online names the client's, which keep running (CONN-HANDOFF-006).
	Sessions []ops.SessionView `json:"sessions"`
}

// Repo is one registered project's repository on one machine.
type Repo struct {
	Project  string `json:"project"`
	Path     string `json:"path"`
	Category string `json:"category,omitempty"`
	// Slots is true when the project's settings give it ports, so a worktree mirrored to the
	// other machine claims a slot there.
	Slots bool `json:"slots,omitempty"`
	// Remote is origin's address. Two machines' repositories are handed over between only when
	// their origins are the same, so a project registered under one name for two different
	// repositories is never mixed up.
	Remote string `json:"remote,omitempty"`
	// Leave is the files engineroom writes into each of the project's checkouts for this machine
	// (the slot's record, its environment file and the files rendered from it), as paths in the
	// checkout. They are this machine's own, never uncommitted work, so a handover never carries
	// them and they never count as changes.
	Leave     []string   `json:"leave,omitempty"`
	Branches  []Branch   `json:"branches"`
	Checkouts []Checkout `json:"checkouts"`
	// GitHubProblem is why GitHub's branches could not be read; the branches are then compared
	// with what this machine last fetched instead.
	GitHubProblem string `json:"githubProblem,omitempty"`
	// Problem is why nothing could be read for this project (no folder, not a repository, no
	// origin). Such a project is named, never handed over.
	Problem string `json:"problem,omitempty"`
}

// Branch is one local branch against GitHub's branch of the same name.
type Branch struct {
	Name string `json:"name"`
	Head string `json:"head"`
	// GitHub is GitHub's head for the branch, empty when GitHub has no such branch or could not
	// be read.
	GitHub string `json:"github,omitempty"`
	// Ahead counts the commits here that GitHub does not have.
	Ahead int `json:"ahead"`
	// Behind counts the commits GitHub has that are not here. Unknown is -1: GitHub's head has
	// never been fetched here, so it holds commits this machine has not seen, and whether the
	// two have diverged cannot be told without fetching, which a reading never does.
	Behind int `json:"behind"`
}

// Unknown is Behind when it cannot be counted.
const Unknown = -1

// Checkout is one worktree: the main clone or a linked worktree.
type Checkout struct {
	Path string `json:"path"`
	// Branch is the branch checked out, empty when the worktree is on a detached commit.
	Branch string `json:"branch,omitempty"`
	// Dirty is uncommitted changes to tracked files. Untracked files do not count: moving a
	// worktree forward never touches them (git refuses rather than overwrite one), and the
	// agent's own files would otherwise make every worktree with a session look dirty.
	Dirty bool `json:"dirty"`
	// Head is the commit checked out, the one uncommitted work sits on.
	Head string `json:"head,omitempty"`
	// Changes is the uncommitted work, one `git status --porcelain` line each ("M file",
	// "?? file"): tracked files changed and new files git does not ignore, without the project's
	// Leave files and what is never work (MachineOwn). A handover carries them to the other
	// machine as a stash, applied there on the same branch. Ignored files are never carried:
	// whatever of them must travel belongs in the developer's settings, as secret files.
	Changes []string `json:"changes,omitempty"`
	// Fingerprint is a digest of the checkout's state: its branch, its commit, and the content
	// of its uncommitted work. A handover checks it again before it changes anything (Verify), so
	// a commit or an edit made after the plan was read stops it rather than being handed over
	// half-seen.
	Fingerprint string `json:"fingerprint,omitempty"`
	// made marks a checkout a handover's own mirror will make on the receiving machine, not one
	// read there: "clone" for a fresh clone, "existing" for a clone already in place whose state
	// was not read.
	made string
}

// readTimeout bounds each git command of a reading that stays on this machine.
const readTimeout = 20 * time.Second

// lsRemoteTimeout bounds asking GitHub for its branches, the one part of a reading that leaves
// the machine, so a GitHub that does not answer makes the reading say so (compared with what was
// last fetched) rather than hold the screen that is waiting for it.
const lsRemoteTimeout = 10 * time.Second

// readAtOnce is how many projects are read side by side. Each reading waits mostly on GitHub
// (ls-remote), one after another took about two and a half seconds a project, so several at once
// turn a long wait into a short one while keeping the connections to GitHub few.
const readAtOnce = 6

// Read reads this machine's registered projects and sessions. It runs only git commands that
// read (remote get-url, ls-remote, for-each-ref, rev-list, worktree list, status): it fetches
// nothing and changes nothing, in a repository or anywhere else.
func Read(cfg *config.Config) State {
	return ReadWith(cfg, nil)
}

// ReadWith is Read telling report, when it is not nil, how many projects there are and each one
// as it is read. Projects are read several at a time (readAtOnce), and the reading keeps the
// projects in the order the configuration lists them, whatever order they finish in.
func ReadWith(cfg *config.Config, report func(done, total int, project string)) State {
	st := State{Repos: []Repo{}, Sessions: []ops.SessionView{}}
	// As git names folders, through any symbolic link, so a project's path is found under it.
	if home, err := os.UserHomeDir(); err == nil {
		if real, err := filepath.EvalSymlinks(home); err == nil {
			home = real
		}
		st.Home = home
	}
	total := len(cfg.Projects)
	if report != nil {
		report(0, total, "")
	}
	repos := make([]Repo, total)
	var mu sync.Mutex
	done := 0
	var wg sync.WaitGroup
	slots := make(chan struct{}, readAtOnce)
	for i, p := range cfg.Projects {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int, p config.Project) {
			defer wg.Done()
			defer func() { <-slots }()
			repos[i] = readRepo(p)
			if report != nil {
				mu.Lock()
				done++
				n := done
				report(n, total, p.Name)
				mu.Unlock()
			}
		}(i, p)
	}
	wg.Wait()
	st.Repos = append(st.Repos, repos...)
	// A machine whose Zellij cannot be read has no sessions to name; the reading of its
	// repositories still stands.
	if list, err := ops.Sessions(cfg); err == nil {
		for _, s := range list {
			if s.Project != "" && !s.Ended {
				st.Sessions = append(st.Sessions, s)
			}
		}
	}
	return st
}

func readRepo(p config.Project) Repo {
	r := Repo{Project: p.Name, Path: p.Path, Category: p.Category, Branches: []Branch{}, Checkouts: []Checkout{}}
	if sp, err := settings.Load(p.Name, p.Path, p.Path); err == nil {
		r.Slots = sp.HasSlots()
		r.Leave = machineFiles(sp)
	}
	if info, err := os.Stat(p.Path); err != nil || !info.IsDir() {
		r.Problem = "its folder " + p.Path + " does not exist"
		return r
	}
	if _, err := git(p.Path, "rev-parse", "--git-dir"); err != nil {
		r.Problem = p.Path + " is not a git repository"
		return r
	}
	// The address as configured, not as rewritten by this machine's own git settings
	// (insteadOf): the two machines rewrite it differently, and both name the same repository.
	remote, err := git(p.Path, "config", "--get", "remote.origin.url")
	if err != nil {
		r.Problem = "it has no remote called origin, so nothing in it can reach GitHub"
		return r
	}
	r.Remote = remote

	github := map[string]string{}
	if out, err := gitWithin(lsRemoteTimeout, p.Path, "ls-remote", "--heads", "origin"); err != nil {
		r.GitHubProblem = err.Error()
	} else {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 {
				github[strings.TrimPrefix(f[1], "refs/heads/")] = f[0]
			}
		}
	}

	refs, _ := git(p.Path, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
	for _, line := range strings.Split(refs, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		r.Branches = append(r.Branches, compare(p.Path, f[0], f[1], github[f[0]]))
	}

	wt, _ := git(p.Path, "worktree", "list", "--porcelain")
	var cur *Checkout
	flush := func() {
		if cur != nil {
			status, _ := git(cur.Path, "status", "--porcelain", "--untracked-files=no")
			cur.Dirty = status != ""
			all, _ := gitRaw(cur.Path, "status", "--porcelain", "--untracked-files=all")
			cur.Changes = ChangesWithout(all, r.Leave)
			cur.Fingerprint = Fingerprint(cur.Path, cur.Branch, cur.Head, cur.Changes, r.Leave)
			r.Checkouts = append(r.Checkouts, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(wt, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Checkout{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD ") && cur != nil:
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "bare" && cur != nil:
			// A bare main repository has no files to bring up to date.
			cur = nil
		}
	}
	flush()
	return r
}

// machineFiles is the files engineroom writes into a checkout of the project for this machine: the
// slot's record and environment file, and each file rendered from the slot. None when the project
// has no slots.
func machineFiles(sp settings.Project) []string {
	if !sp.HasSlots() {
		return nil
	}
	out := []string{sp.Slots.Record, slots.EnvFile(sp.Slots.Record)}
	for _, r := range sp.Render {
		out = append(out, filepath.ToSlash(r.To))
	}
	return out
}

// ChangesWithout is the lines of `git status --porcelain` output, each "XY path", without those
// naming one of leave or what is never work (MachineOwn), trimmed of the status's padding
// ("M file", "?? file").
func ChangesWithout(status string, leave []string) []string {
	skip := map[string]bool{}
	for _, l := range leave {
		skip[l] = true
	}
	var out []string
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		path := line[3:]
		if skip[path] || MachineOwn(path) {
			continue
		}
		out = append(out, strings.TrimSpace(line[:2])+" "+path)
	}
	return out
}

// MachineOwn reports whether a path in a checkout belongs to the machine rather than to the work,
// so a handover neither carries it nor sets it aside: Finder's .DS_Store files, and the .claude
// folder, where Claude Code keeps its agents' memory and this machine's settings for the
// project. Carried, they would move a running session's memory off the machine it runs on and
// pass Finder's files off as work.
func MachineOwn(path string) bool {
	path = strings.Trim(path, `"`)
	return filepath.Base(path) == ".DS_Store" || path == ".claude" || strings.HasPrefix(path, ".claude/")
}

// Excludes is the pathspec that leaves a handover's stash without the machine's own files: what
// MachineOwn names, and the project's leave files.
func Excludes(leave []string) []string {
	out := []string{":(exclude,glob)**/.DS_Store", ":(exclude).claude"}
	for _, l := range leave {
		out = append(out, ":(exclude)"+l)
	}
	return out
}

// Fingerprint digests a checkout's state: its branch and commit, the difference of its tracked
// files from the commit, and each new file's name and content (changes are its Changes lines).
// Two readings with the same fingerprint saw the same work.
func Fingerprint(dir, branch, head string, changes []string, leave []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", branch, head)
	diff, _ := gitRaw(dir, append([]string{"diff", "HEAD", "--binary", "--", "."}, Excludes(leave)...)...)
	h.Write([]byte(diff))
	for _, c := range changes {
		if !strings.HasPrefix(c, "?? ") {
			continue
		}
		path := strings.TrimPrefix(c, "?? ")
		fmt.Fprintf(h, "\x00%s\x00", path)
		if b, err := os.ReadFile(filepath.Join(dir, path)); err == nil {
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// compare counts a branch against GitHub's head of the same name.
func compare(dir, name, head, github string) Branch {
	b := Branch{Name: name, Head: head, GitHub: github}
	switch {
	case github == head:
	case github == "":
		// GitHub has no such branch (or could not be read): what is here and on no branch this
		// machine has fetched from origin is what GitHub lacks.
		b.Ahead = count(dir, head, "--not", "--remotes=origin")
	case has(dir, github):
		b.Ahead = count(dir, github+".."+head)
		b.Behind = count(dir, head+".."+github)
	default:
		b.Ahead = count(dir, head, "--not", "--remotes=origin")
		b.Behind = Unknown
	}
	return b
}

func count(dir string, args ...string) int {
	out, err := git(dir, append([]string{"rev-list", "--count"}, args...)...)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

func has(dir, commit string) bool {
	_, err := git(dir, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

// git runs one reading git command in dir and returns its trimmed output, or an error carrying
// what git said. GIT_TERMINAL_PROMPT=0 makes a GitHub that wants a password fail at once instead
// of waiting at a prompt nobody can see.
func git(dir string, args ...string) (string, error) {
	return gitWithin(readTimeout, dir, args...)
}

// gitRaw is git without trimming the output, for `status --porcelain`, whose first line starts
// with the status's own spaces.
func gitRaw(dir string, args ...string) (string, error) {
	return gitRun(readTimeout, false, dir, args...)
}

func gitWithin(limit time.Duration, dir string, args ...string) (string, error) {
	return gitRun(limit, true, dir, args...)
}

func gitRun(limit time.Duration, trim bool, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if ctx.Err() != nil {
			msg = "no answer within " + limit.String()
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", &gitError{args: args, msg: msg}
	}
	if !trim {
		return out.String(), nil
	}
	return strings.TrimSpace(out.String()), nil
}

type gitError struct {
	args []string
	msg  string
}

func (e *gitError) Error() string { return "git " + strings.Join(e.args, " ") + ": " + e.msg }
