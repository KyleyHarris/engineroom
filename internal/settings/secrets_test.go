package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Saving copies a checkout's secret files into the settings' secrets folder; restoring puts them
// into a checkout that lacks them, a home-folder one included, readable by the owner alone; and
// a file the checkout already has is never overwritten, even when it differs. It fails if a
// restore clobbered a developer's edited file, a saved copy landed outside secrets/ (where it
// would be committed unencrypted), or a second save copied a file that had not changed.
func TestSecretsAreSavedAndPutBack(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	main := clone(t, "git@github.com:owner/app.git")
	mine := filepath.Join(root, "projects", "owner", "app")
	write(t, filepath.Join(mine, "engineroom.toml"), "secrets = [\"server/local.json\", \"~/.app/user.json\"]\n", 0o644)
	write(t, filepath.Join(main, "server", "local.json"), `{"key":"one"}`, 0o600)
	write(t, filepath.Join(home, ".app", "user.json"), `{"user":"secret"}`, 0o600)

	p, err := Load("app", main, main)
	if err != nil || len(p.Secrets) != 2 {
		t.Fatalf("%+v %v", p.Secrets, err)
	}
	run(t, SavePlan(p, main))
	for _, saved := range []string{"secrets/worktree/server/local.json", "secrets/home/.app/user.json"} {
		if _, err := os.Stat(filepath.Join(mine, saved)); err != nil {
			t.Fatalf("%s was not saved inside secrets/: %v", saved, err)
		}
	}
	if again := SavePlan(p, main); len(again.Commands) != 0 {
		t.Fatalf("an unchanged file was saved again: %v", again.Lines())
	}

	// A new worktree has neither; restore puts both in place (the home one is already there).
	wt := t.TempDir()
	run(t, RestorePlan(p, wt))
	got, err := os.ReadFile(filepath.Join(wt, "server", "local.json"))
	if err != nil || string(got) != `{"key":"one"}` {
		t.Fatalf("restore: %q %v", got, err)
	}
	if info, _ := os.Stat(filepath.Join(wt, "server", "local.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("a restored secret is %v, not readable by its owner alone", info.Mode().Perm())
	}

	// An edited file is left alone and named.
	write(t, filepath.Join(wt, "server", "local.json"), `{"key":"edited"}`, 0o600)
	plan := RestorePlan(p, wt)
	if len(plan.Commands) != 0 || !strings.Contains(strings.Join(plan.Notes, " "), "differs from your saved copy") {
		t.Fatalf("a differing file would be overwritten: %v %v", plan.Lines(), plan.Notes)
	}
}
