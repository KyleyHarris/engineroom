package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/mux"
)

// Opening a project whose machine keeps sessions in Rex (D-029) plans the same steps as with
// Zellij, in Rex's commands: write the layout and agent script, create the session with Claude
// and a terminal, wait for it, and show it in the Rex app where the developer is. A running one is
// shown, never created again. Fails if any Zellij command is planned, the order changes, or a
// second session is created under a name already listed.
func TestOpenPlansRexSessions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	rex, list := filepath.Join(dir, "rex"), filepath.Join(dir, "list.json")
	if err := os.WriteFile(list, []byte(`{"sessions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rex, []byte("#!/bin/sh\n[ \"$1\" = ls ] && cat '"+list+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mux.Use("rex", rex); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Use("", "") })
	project := filepath.Join(dir, "project-a")
	cfg := &config.Config{Role: config.RoleEngine, Projects: []config.Project{{Name: "project-a", Path: project}}}

	plan, err := Open(cfg, "project-a", Start{})
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, c := range plan.Commands {
		steps = append(steps, strings.Join(c.Argv, " "))
	}
	all := strings.Join(steps, "\n")
	if strings.Contains(all, "zellij") {
		t.Fatalf("a Rex machine planned Zellij:\n%s", all)
	}
	want := []string{"engineroom layout write --out ", rex + " session new project-a --cwd " + project, rex + " window new -s project-a terminal", "engineroom session wait project-a", "engineroom rex show project-a"}
	if len(steps) != len(want) {
		t.Fatalf("planned:\n%s", all)
	}
	for i, w := range want {
		if !strings.HasPrefix(steps[i], w) {
			t.Fatalf("step %d is %q, want %q\n%s", i+1, steps[i], w, all)
		}
	}
	if !plan.Commands[4].Viewer {
		t.Fatal("showing the session would run on the engine, not where the developer is")
	}

	if err := os.WriteFile(list, []byte(`{"sessions":[{"session_id":"session:0a1b","label":"project-a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err = Open(cfg, "project-a", Start{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Commands) != 1 || strings.Join(plan.Commands[0].Argv, " ") != "engineroom rex show project-a" {
		t.Fatalf("a running session was not just shown: %+v", plan.Commands)
	}
}
