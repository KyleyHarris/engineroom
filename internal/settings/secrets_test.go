package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/history"
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

// Learning obligation O10: a secret's value never appears on screen or in the history. The
// actions that carry secrets (saving them to the settings, putting them back in a checkout) are
// run for real and recorded the way the interface records them; then every line shown before
// they run, every note, every step's output and the history file itself are searched for the
// value. Today this holds because the plans name files and never their contents; the test fails
// if a plan ever carries a value in its arguments, or a command prints one.
func TestASecretsValueIsNeverShownOrRecorded(t *testing.T) {
	const value = "s3cr3t-value-that-must-stay-in-the-file"
	root := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", root)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	main := clone(t, "git@github.com:owner/app.git")
	mine := filepath.Join(root, "projects", "owner", "app")
	write(t, filepath.Join(mine, "engineroom.toml"), "secrets = [\"server/local.json\"]\n", 0o644)
	write(t, filepath.Join(main, "server", "local.json"), `{"token":"`+value+`"}`, 0o600)
	p, err := Load("app", main, main)
	if err != nil {
		t.Fatal(err)
	}

	var shown []string
	record := func(plan action.Plan) {
		t.Helper()
		shown = append(shown, plan.Title)
		shown = append(shown, plan.Lines()...)
		shown = append(shown, plan.Notes...)
		for _, c := range plan.Commands {
			shown = append(shown, c.Why, c.OnFail)
		}
		entry, err := history.Begin(plan)
		if err != nil {
			t.Fatal(err)
		}
		ok := action.Run(context.Background(), plan, func(s action.Step) {
			shown = append(shown, s.Output)
			entry.Update(s)
		})
		if !ok {
			t.Fatalf("%s failed: %+v", plan.Title, entry.Commands)
		}
		if err := entry.Finish(history.Done); err != nil {
			t.Fatal(err)
		}
	}
	save := SavePlan(p, main)
	if len(save.Commands) == 0 {
		t.Fatal("nothing to save: the test would search an empty action")
	}
	record(save)
	wt := t.TempDir()
	restore := RestorePlan(p, wt)
	if len(restore.Commands) == 0 {
		t.Fatal("nothing to restore: the test would search an empty action")
	}
	record(restore)
	// A checkout whose copy differs is named in a note, with both paths, never their contents.
	write(t, filepath.Join(wt, "server", "local.json"), `{"token":"edited-`+value+`"}`, 0o600)
	differs := RestorePlan(p, wt)
	if len(differs.Notes) == 0 {
		t.Fatal("a differing copy was not noted: the test would search no note")
	}
	record(differs)

	for _, s := range shown {
		if strings.Contains(s, value) {
			t.Fatalf("a secret's value is on screen: %q", s)
		}
	}
	b, err := os.ReadFile(history.File())
	if err != nil || len(b) == 0 {
		t.Fatalf("no history was written to search: %v", err)
	}
	if strings.Contains(string(b), value) {
		t.Fatalf("a secret's value is in the history:\n%s", b)
	}
}
