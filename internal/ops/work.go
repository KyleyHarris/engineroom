package ops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// workName is the rule for a piece of work's name. It is also the branch name and the end of
// the worktree's folder name, so it carries no slashes or spaces: the three can then be read off
// each other without any transformation.
var workName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// WorktreePath is where a piece of work's worktree lives: beside the project's main clone,
// named <project folder>-<work>, never inside the main clone where file watchers, test
// searches and git status would pick it up.
func WorktreePath(main, work string) string {
	return filepath.Join(filepath.Dir(main), filepath.Base(main)+"-"+work)
}

// openCheckout is the plan for opening the session of one checkout: join it if it is running,
// start it fresh if it ended (D-015: what Zellij kept of it is deleted, so it never comes back
// with an old layout), otherwise create it from the standard layout and join it.
//
// start applies only to a session created here. A running session is joined as it is, never
// restarted to apply it (SESS-REMOTE-004). Starting without joining has nothing to do for a
// session that exists, so it is refused with the reason rather than joining unasked.
func openCheckout(name, dir string, start Start) ([]action.Command, error) {
	existing, err := find(name)
	if err != nil {
		return nil, err
	}
	switch {
	case existing != nil && start.NoJoin && !existing.Ended:
		return nil, fmt.Errorf("the session %s is already running; join it to work in it", name)
	case existing != nil && !existing.Ended:
		return []action.Command{zj.Attach(name)}, nil
	case existing != nil && existing.Ended:
		return append([]action.Command{zj.End(name)}, createCommands(name, dir, start)...), nil
	}
	return createCommands(name, dir, start), nil
}

// createCommands create a session from the current standard layout in dir and, unless start says
// not to, join it. The agent tab runs claude --continue, so the conversation carries on.
func createCommands(name, dir string, start Start) []action.Command {
	write := []string{"engineroom", "layout", "write"}
	why := "write the standard layout"
	if start.RemoteControl {
		write = append(write, "--remote-control")
		why = "write the standard layout, with the agent started under Remote Control"
	}
	cmds := []action.Command{{Argv: append(write, "--out", LayoutFile(start.RemoteControl)), Why: why}}
	cmds = append(cmds, zj.Start(name, LayoutFile(start.RemoteControl), dir)...)
	// Zellij returns from a background create before the session accepts clients (and a start
	// through the desktop login returns before its job has run), so joining straight away can
	// find no session. Waiting is a command of its own, so it is seen like any other step.
	cmds = append(cmds, action.Command{Argv: []string{"engineroom", "session", "wait", name}, Why: "wait until the session is ready"})
	if !start.NoJoin {
		cmds = append(cmds, zj.Attach(name))
	}
	return cmds
}

// isWorktree reports whether dir is one of the repository's worktrees, asking git.
func isWorktree(main, dir string) bool {
	out, err := exec.Command("git", "-C", main, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return false
	}
	want := resolvePath(dir)
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok && resolvePath(p) == want {
			return true
		}
	}
	return false
}

func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// startPoint is what new work branches from: the remote's default branch when the repository
// has a remote called origin, otherwise its local main. It is decided before fetching, from
// what git already knows about the remote, and the fetch then brings that branch up to date.
func startPoint(main string) (base string, fetch bool) {
	if exec.Command("git", "-C", main, "remote", "get-url", "origin").Run() != nil {
		return "main", false
	}
	if out, err := exec.Command("git", "-C", main, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out)), true
	}
	return "origin/main", true
}

// Script is the command that runs one of a project's lifecycle scripts in a worktree, with the
// program's details in ENGINEROOM_* variables (docs/decisions.md D-006). The variables are part of the
// command itself, so the command shown is the whole of what runs. path is the script: the
// developer's own from their settings, or the repository's ./.engineroom/<name> (D-013).
//
// Every script must be safe to run again: run twice, it finds what it made or started the first
// time and leaves it alone. That is the script's job, not the program's; the program may run it
// whenever it is asked to.
//
// slotEnv is the project's slot environment file (slots.EnvFile of its record), or empty when the
// project has no slots. When it is set, the script runs with the values of the slot its checkout
// holds, read from that file in the checkout, so a worktree's up script starts its stack on its
// own ports whatever the project calls its record (PORT-SLOT-003). A checkout that holds no slot
// has no file, and its script runs with slot 1's defaults, as before.
func Script(name, path string, from settings.Source, project, work, main, dir, slotEnv string) action.Command {
	why := "the repository's own " + name + " script"
	if from == settings.Mine {
		why = "your " + name + " script for " + project + ", from your settings"
	}
	// ENGINEROOM_SESSION names the session of the checkout the script runs for, so whatever it starts
	// is known to belong to that session and is stopped when the session ends (D-020).
	argv := []string{"env",
		"ENGINEROOM_PROJECT=" + project,
		"ENGINEROOM_WORK=" + work,
		"ENGINEROOM_MAIN=" + main,
		"ENGINEROOM_WORKTREE=" + dir,
		"ENGINEROOM_SESSION=" + zj.SessionName(dir),
	}
	if slotEnv != "" {
		argv = append(argv, "sh", "-c", slotEnvPrefix+`exec "$@"`, name, slotEnv)
		why += ", with the slot's values from " + slotEnv
	}
	return action.Command{Argv: append(argv, path), Dir: dir, Why: why}
}

// slotEnvPrefix is the shell that reads a slot's environment file, given as the first argument,
// into the environment when the checkout has one, then drops that argument. Every value in it is
// exported (set -a), so the script and anything it starts see them.
const slotEnvPrefix = `if [ -f "$1" ]; then set -a; . "./$1"; set +a; fi; shift; `

// slotEnvFile is the name of a project's slot environment file in a checkout, or empty when its
// settings declare no slots or cannot be read.
func slotEnvFile(p config.Project, found string) string {
	sp, err := settings.Load(p.Name, p.Path, found)
	if err != nil || !sp.HasSlots() {
		return ""
	}
	return slots.EnvFile(sp.Slots.Record)
}

// script finds a project's lifecycle script for a checkout, the developer's own first (D-013).
// A repository's script is run by its path in the checkout, so a branch that changes it runs its
// own version. found reads the scripts; dir is where it will run.
func script(p config.Project, found, dir, work, name string) (action.Command, bool) {
	path, from, ok := settings.Script(p.Name, p.Path, found, name)
	if !ok {
		return action.Command{}, false
	}
	if from == settings.Repo {
		path = "./.engineroom/" + name
	}
	return Script(name, path, from, p.Name, work, p.Path, dir, slotEnvFile(p, found)), true
}

// claimCommand is the command that gives a new worktree its own slot and writes the slot's
// files, when the project declares ports. It is a command of the plan, so the developer sees
// the claim among the other steps.
func claimCommand(p config.Project, dir string) (action.Command, bool) {
	sp, err := settings.Load(p.Name, p.Path, p.Path)
	if err != nil || !sp.HasSlots() {
		return action.Command{}, false
	}
	return action.Command{
		Argv:   []string{"engineroom", "slot", "claim", "--worktree", dir},
		Why:    "give the worktree its own slot: its own ports and names, written to " + sp.Slots.Record,
		OnFail: "The worktree was created but has no slot, so its stack would collide with another worktree's. Clean up a finished worktree to free a slot, then run: engineroom slot claim --worktree " + dir,
	}, true
}

// NewWork is the plan for starting a piece of work on a project: bring the remote up to date,
// create a worktree beside the main clone on a new branch from the remote's main, run the
// repository's create script there if it declares one, then open a session in it. Work that
// already exists is opened instead, so asking twice never makes a second copy.
func NewWork(cfg *config.Config, project, work string, start Start) (action.Plan, error) {
	p, ok := cfg.Project(project)
	if !ok {
		return action.Plan{}, &NotRegisteredError{Name: project, Registered: cfg.ProjectNames()}
	}
	if !workName.MatchString(work) {
		return action.Plan{}, fmt.Errorf("the name %q may only hold letters, digits, '.', '_' and '-', starting with a letter or digit", work)
	}
	dir := WorktreePath(p.Path, work)
	name := zj.SessionName(dir)
	plan := action.Plan{Title: fmt.Sprintf("New work %s on %s", work, p.Name)}

	if isWorktree(p.Path, dir) {
		cmds, err := openCheckout(name, dir, start)
		if err != nil {
			return action.Plan{}, err
		}
		plan.Title = fmt.Sprintf("Open existing work %s on %s", work, p.Name)
		plan.Commands = cmds
		return plan, nil
	}
	if _, err := os.Stat(dir); err == nil {
		return action.Plan{}, fmt.Errorf("%s already exists and is not a worktree of %s; move it aside or choose another name", dir, p.Name)
	}

	// Settings that cannot be read, or every slot held (one counts as the main clone's while it has
	// claimed none): refuse before anything is created (PORT-SLOT-004, PORT-SET-002).
	if err := slotsLeft(p); err != nil {
		return action.Plan{}, fmt.Errorf("new work on %s was not started, because %w", p.Name, err)
	}

	base, fetch := startPoint(p.Path)
	if fetch {
		plan.Commands = append(plan.Commands, action.Command{
			Argv: []string{"git", "-C", p.Path, "fetch", "origin"},
			Why:  "bring the remote's branches up to date",
		})
	}
	plan.Commands = append(plan.Commands, action.Command{
		Argv: []string{"git", "-C", p.Path, "worktree", "add", "-b", work, dir, base},
		Why:  "create the worktree on a new branch from " + base,
	})
	rest, err := setUpWorktree(p, base, dir, work, name, start)
	if err != nil {
		return action.Plan{}, err
	}
	plan.Commands = append(plan.Commands, rest...)
	return plan, nil
}

// setUpWorktree is what a new worktree needs once git has made it, in order: its slot, the
// project's secret files, the create script of the commit it starts from (ref), then its session.
// Shared by branch work (NewWork) and role worktrees (NewRoleWork), so both are set up alike.
func setUpWorktree(p config.Project, ref, dir, work, name string, start Start) ([]action.Command, error) {
	var out []action.Command
	if claim, ok := claimCommand(p, dir); ok {
		out = append(out, claim)
	}
	// The project's secret files, from the developer's settings, before its create script, which
	// may need them (D-014). Only files the new worktree lacks, which is all of them.
	if sp, err := settings.Load(p.Name, p.Path, p.Path); err == nil && len(sp.Secrets) > 0 {
		out = append(out, action.Command{
			Argv:   []string{"engineroom", "secrets", "restore", p.Name, "--worktree", dir},
			Why:    "put the project's secret files in place from your settings",
			OnFail: "The worktree was created without its secret files. Save them first (engineroom secrets save " + p.Name + "), then run: engineroom secrets restore " + p.Name + " --worktree " + dir,
		})
	}
	if c, ok := newWorkCreate(p, ref, dir, work); ok {
		c.OnFail = fmt.Sprintf("The worktree %s was created, but its setup failed, so no session was opened. Fix the script and run it again, or clean the work up.", dir)
		out = append(out, c)
	}
	cmds, err := openCheckout(name, dir, start)
	if err != nil {
		return nil, err
	}
	return append(out, cmds...), nil
}

// repoScriptAt reports whether the commit at ref holds the repository's .engineroom/<name> as an
// executable file: what a worktree made from ref will have. Read from git, since the worktree
// does not exist yet when its plan is made, and the main clone may have another branch out.
func repoScriptAt(main, ref, name string) bool {
	out, err := exec.Command("git", "-C", main, "ls-tree", ref, ".engineroom/"+name).Output()
	return err == nil && strings.HasPrefix(string(out), "100755 ")
}

// newWorkCreate is the create script new work runs: the developer's own from their settings, or
// else the repository's as of the commit the new worktree starts from. The main clone may have
// another branch checked out, whose .engineroom/create the new worktree would not have, or lack one
// that the start point has (WORK-NEW-007).
func newWorkCreate(p config.Project, base, dir, work string) (action.Command, bool) {
	if _, from, found := settings.Script(p.Name, p.Path, p.Path, "create"); found && from != settings.Repo {
		return script(p, p.Path, dir, work, "create")
	}
	if !repoScriptAt(p.Path, base, "create") {
		return action.Command{}, false
	}
	return Script("create", "./.engineroom/create", settings.Repo, p.Name, work, p.Path, dir, slotEnvFile(p, p.Path)), true
}

// ProjectAt is the registered project whose main clone or worktree holds dir: the clone a folder
// belongs to is the parent of its shared .git, which one git call answers, so a hook can ask it
// on every command.
func ProjectAt(cfg *config.Config, dir string) (string, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", false
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) != ".git" {
		return "", false
	}
	clone := resolvePath(filepath.Dir(common))
	for _, p := range cfg.Projects {
		if resolvePath(p.Path) == clone {
			return p.Name, true
		}
	}
	return "", false
}
