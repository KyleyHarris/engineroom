package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// A project's page lists the main clone first and every worktree after it, each with the session
// it would have, its branch, the work name that reopens it, its uncommitted changes, its slot,
// ports and links; and the scripts with where each comes from. It fails if a worktree were
// missing, the main clone not marked, a link not filled from the slot, or a script's source
// misread.
func TestAProjectsPageListsEveryCheckout(t *testing.T) {
	t.Setenv("ENGINEROOM_SETTINGS", t.TempDir())
	t.Setenv("ZELLIJ_SOCKET_DIR", t.TempDir())
	root := t.TempDir()
	main := filepath.Join(root, "app")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q", "-b", "main")
	gitRun(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	wt := filepath.Join(root, "app-fix")
	gitRun(t, main, "worktree", "add", "-q", "-b", "fix", wt)
	if err := os.WriteFile(filepath.Join(wt, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(main, ".engineroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "[ports]\nui = 4100\n[links]\napp = \"https://localhost:{{ui}}/\"\n"
	if err := os.WriteFile(filepath.Join(main, ".engineroom", "engineroom.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".engineroom", "up"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Role: config.RoleEngine, Projects: []config.Project{{Name: "app", Path: main, Pinned: true}}}

	page, err := Page(cfg, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Checkouts) != 2 || !page.Checkouts[0].Main || page.Checkouts[1].Main {
		t.Fatalf("checkouts %+v", page.Checkouts)
	}
	w := page.Checkouts[1]
	if w.Branch != "fix" || w.Work != "fix" || w.Changes != 1 || w.Session == "" {
		t.Fatalf("worktree %+v", w)
	}
	m := page.Checkouts[0]
	if m.Slot != 1 || len(m.Links) != 1 || m.Links[0].URL != "https://localhost:4100/" {
		t.Fatalf("main clone's slot and links %+v", m)
	}
	var up ScriptView
	for _, s := range page.Scripts {
		if s.Name == "up" {
			up = s
		}
	}
	if up.From == "" || !page.Pinned {
		t.Fatalf("scripts %+v pinned %v", page.Scripts, page.Pinned)
	}
	// The panel's cards say the same (Run app, no Stop app): the project has an up script and no
	// down script, for every checkout.
	for _, f := range Facts(cfg, []string{"app"})["app"] {
		if !f.Up || f.Down {
			t.Fatalf("facts for %s say up=%v down=%v, want up and no down", f.Path, f.Up, f.Down)
		}
	}
	if _, err := Page(cfg, "nope"); err == nil {
		t.Fatal("an unregistered project had a page")
	}
}

// A worktree whose folder has the main clone's name would share its session name, so its
// session and stack actions would reach the main clone's session; the page marks it, and does
// not count the main clone's session as its. Fails if the clash is not marked, or the main
// clone is.
func TestAWorktreeNamedLikeTheMainCloneIsMarked(t *testing.T) {
	t.Setenv("ENGINEROOM_SETTINGS", t.TempDir())
	root := t.TempDir()
	main := filepath.Join(root, "app")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q", "-b", "main")
	gitRun(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	elsewhere := filepath.Join(root, "proof", "app")
	gitRun(t, main, "worktree", "add", "-q", "--detach", elsewhere)
	cfg := &config.Config{Role: config.RoleEngine, Projects: []config.Project{{Name: "app", Path: main}}}
	page, err := Page(cfg, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Checkouts) != 2 || page.Checkouts[0].Clash != "" || !strings.Contains(page.Checkouts[1].Clash, "same name") {
		t.Fatalf("checkouts %+v", page.Checkouts)
	}
}

// A project's named task runs in the session's checkout with the slot's environment, and a ./file
// runs from the folder its engineroom.toml is in; the page lists the tasks. It fails if the task runs
// anywhere but the checkout, cannot read the slot's ports, a ./file is looked for in the checkout,
// or an unknown task is not refused with the names there are.
func TestATaskRunsInItsCheckoutWithTheSlot(t *testing.T) {
	settingsRoot := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", settingsRoot)
	root := t.TempDir()
	main := filepath.Join(root, "app")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q", "-b", "main")
	gitRun(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	engine := filepath.Join(settingsRoot, "projects", "app")
	if err := os.MkdirAll(engine, 0o755); err != nil {
		t.Fatal(err)
	}
	toml := "[ports]\nui = 4100\n\n[[task]]\nname = \"Fetch the database\"\nabout = \"a copy for testing\"\nrun = \"./fetch\"\n"
	if err := os.WriteFile(filepath.Join(engine, "engineroom.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engine, "fetch"), []byte("#!/bin/sh\necho \"in $(pwd) port ${ENGINEROOM_PORT_UI:-none} work ${ENGINEROOM_PROJECT}\" > ran.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".engineroom-slot.env"), []byte("ENGINEROOM_PORT_UI=\"4100\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Role: config.RoleEngine, Projects: []config.Project{{Name: "app", Path: main}}}

	page, err := Page(cfg, "app")
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Name != "Fetch the database" {
		t.Fatalf("tasks %+v %v", page.Tasks, err)
	}
	session := page.Checkouts[0].Session
	plan, err := Task(cfg, session, "fetch the DATABASE")
	if err != nil {
		t.Fatal(err)
	}
	if ok := action.Run(context.Background(), plan, func(action.Step) {}); !ok {
		t.Fatalf("the task failed: %v", plan.Lines())
	}
	got, _ := os.ReadFile(filepath.Join(main, "ran.txt"))
	real, _ := filepath.EvalSymlinks(main)
	if !strings.Contains(string(got), "port 4100 work app") || !(strings.Contains(string(got), real) || strings.Contains(string(got), main)) {
		t.Fatalf("the task ran as %q", got)
	}
	if _, err := Task(cfg, session, "nope"); err == nil || !strings.Contains(err.Error(), "Fetch the database") {
		t.Fatalf("an unknown task: %v", err)
	}
}
