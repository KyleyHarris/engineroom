package settings

// The settings folder as a private git repository of the developer's own (docs/decisions.md
// D-014), so the same settings are on every machine. Secrets a project needs live in that
// project's secrets/ folder and are encrypted in the repository by git-crypt: they are plain
// files on a machine that holds the key, and unreadable bytes everywhere else, GitHub included.
//
// Everything here is a plan of real git and git-crypt commands, shown before they run and kept
// in the history, like every other action.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// Attributes is the settings repository's .gitattributes: every file in any secrets folder is
// encrypted by git-crypt, and the attributes file itself never is, or git-crypt could not read it.
const Attributes = `# Written by engineroom settings init. Every file in a secrets folder is encrypted in the repository
# by git-crypt; on a machine holding the key it is a plain file.
**/secrets/** filter=git-crypt diff=git-crypt
.gitattributes !filter !diff
`

// RepoState is what the settings folder is as a repository, read without changing anything.
type RepoState struct {
	Root string
	// IsRepo is true when the folder is a git repository.
	IsRepo bool
	// Crypt is true when git-crypt has been set up here: the folder holds a key, so its secrets
	// are readable and anything added to a secrets folder is encrypted when committed.
	Crypt bool
	// Attributes is true when .gitattributes says secrets folders are encrypted.
	Attributes bool
	Remote     string
	// Changed is every path with changes not yet committed (git status --porcelain).
	Changed []string
	// Ahead and Behind count commits against the remote's branch, as of the last fetch.
	Ahead, Behind int
	// Unprotected is every file in a secrets folder that git-crypt would not encrypt, which a
	// sync refuses to commit.
	Unprotected []string
}

func git(root string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// KeyFile is where git-crypt keeps this repository's key once it is set up or unlocked.
func KeyFile(root string) string {
	dir, err := git(root, "rev-parse", "--git-dir")
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, "git-crypt", "keys", "default")
}

// State reads the settings folder's state as a repository.
func State(root string) RepoState {
	st := RepoState{Root: root}
	if top, err := git(root, "rev-parse", "--show-toplevel"); err != nil || !sameDir(top, root) {
		return st
	}
	st.IsRepo = true
	if k := KeyFile(root); k != "" {
		_, err := os.Stat(k)
		st.Crypt = err == nil
	}
	if b, err := os.ReadFile(filepath.Join(root, ".gitattributes")); err == nil {
		st.Attributes = strings.Contains(string(b), "**/secrets/** filter=git-crypt")
	}
	st.Remote, _ = git(root, "remote", "get-url", "origin")
	if out, err := git(root, "status", "--porcelain", "--untracked-files=all"); err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			if len(line) > 3 {
				st.Changed = append(st.Changed, strings.TrimSpace(line[3:]))
			}
		}
	}
	if out, err := git(root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); err == nil {
		if f := strings.Fields(out); len(f) == 2 {
			st.Ahead, _ = strconv.Atoi(f[0])
			st.Behind, _ = strconv.Atoi(f[1])
		}
	}
	st.Unprotected = unprotected(root)
	return st
}

// sameDir is true when a and b are the same folder, whatever symbolic links lead to them.
func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// unprotected lists every file in a secrets folder, committed or not, that git-crypt would not
// encrypt: what git check-attr says, not what the patterns are meant to say.
func unprotected(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		if !d.IsDir() && inSecrets(rel) {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(files) == 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", root, "check-attr", "--stdin", "filter")
	cmd.Stdin = strings.NewReader(strings.Join(files, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return files
	}
	var bad []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		// "<path>: filter: <value>"
		line := sc.Text()
		i := strings.LastIndex(line, ": filter: ")
		if i < 0 {
			continue
		}
		if line[i+len(": filter: "):] != "git-crypt" {
			bad = append(bad, line[:i])
		}
	}
	return bad
}

// inSecrets is true when a file sits anywhere below a folder named secrets.
func inSecrets(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "secrets" {
			return true
		}
	}
	return false
}

// InitPlan makes the settings folder a repository with git-crypt set up, commits what is there,
// and, given a remote, pushes it. Each step is left out when it is already done, so running it
// again only does what is missing.
func InitPlan(st RepoState, remote string) action.Plan {
	root := st.Root
	p := action.Plan{Title: "Keep your settings in a private repository", Machine: "this machine"}
	add := func(why string, argv ...string) {
		p.Commands = append(p.Commands, action.Command{Argv: argv, Dir: root, Why: why})
	}
	if !st.IsRepo {
		add("make the settings folder a git repository", "git", "init", "-b", "main")
	}
	if !st.Crypt {
		add("create the key that encrypts every secrets folder", "git-crypt", "init")
	}
	if !st.Attributes {
		add("say which files are encrypted: everything in a secrets folder",
			"sh", "-c", `printf '%s' "$1" > .gitattributes`, "sh", Attributes)
	}
	add("record the settings as they are", "git", "add", "-A")
	p.Commands = append(p.Commands, action.Command{
		Argv: []string{"sh", "-c", `git diff --cached --quiet || git commit -q -m "$1"`, "sh", "Settings for engineroom"},
		Dir:  root, Why: "commit them, when there is anything new",
	})
	if remote != "" && st.Remote == "" {
		add("name the private repository on GitHub", "git", "remote", "add", "origin", remote)
	}
	if remote != "" || st.Remote != "" {
		p.Commands = append(p.Commands, action.Command{
			Argv: []string{"git", "push", "-u", "origin", "main"}, Dir: root, Why: "send them to it",
			OnFail: "The settings are committed here but not sent. Check the repository exists and is private, then run: engineroom settings sync",
		})
	}
	p.Notes = append(p.Notes,
		"The key is in "+KeyFile(root)+" once this has run. Save a copy where you keep secrets: git-crypt export-key <file>, store the file, then delete it. Another machine needs it to read the secrets: engineroom settings clone <repository> <key file>.")
	return p
}

// SyncPlan commits what changed here, takes what other machines sent, and sends this machine's.
// It refuses, before anything runs, when a secret would be committed unencrypted or the folder is
// not a repository with a remote.
func SyncPlan(st RepoState, machine string) (action.Plan, error) {
	if !st.IsRepo || st.Remote == "" {
		return action.Plan{}, fmt.Errorf("your settings in %s are not a repository with a remote yet; set one up with: engineroom settings init <repository>", st.Root)
	}
	if len(st.Unprotected) > 0 {
		return action.Plan{}, fmt.Errorf("these files in a secrets folder would be committed without encryption, so nothing was synced: %s. Run engineroom settings init to set up git-crypt here", strings.Join(st.Unprotected, ", "))
	}
	p := action.Plan{Title: "Sync your settings", Machine: "this machine"}
	if len(st.Changed) > 0 {
		p.Commands = append(p.Commands,
			action.Command{Argv: []string{"git", "add", "-A"}, Dir: st.Root, Why: "record the changes made on this machine"},
			action.Command{Argv: []string{"git", "commit", "-q", "-m", "Settings from " + machine}, Dir: st.Root, Why: "commit them"})
	}
	p.Commands = append(p.Commands,
		action.Command{Argv: []string{"git", "pull", "--rebase", "-q"}, Dir: st.Root, Why: "take what other machines sent",
			OnFail: "Another machine changed the same lines. Resolve the conflict in " + st.Root + " (git status says where), then run: engineroom settings sync"},
		action.Command{Argv: []string{"git", "push", "-q"}, Dir: st.Root, Why: "send this machine's"})
	return p, nil
}

// ClonePlan brings the settings repository to a machine that has none, and unlocks its secrets
// with the key file saved when it was set up.
func ClonePlan(remote, key, root string) action.Plan {
	p := action.Plan{Title: "Bring your settings to this machine", Machine: "this machine"}
	p.Commands = append(p.Commands,
		action.Command{Argv: []string{"git", "clone", "-q", remote, root}, Why: "copy the settings repository here"})
	if key != "" {
		p.Commands = append(p.Commands, action.Command{
			Argv: []string{"git-crypt", "unlock", key}, Dir: root, Why: "decrypt the secrets folders with your key",
			OnFail: "The settings are here but the secrets stay encrypted. Check the key file is the one saved when the repository was set up."})
	}
	return p
}
