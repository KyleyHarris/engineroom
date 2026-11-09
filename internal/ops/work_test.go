package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/config"
)

// New work, and a new role worktree, on a project whose settings are refused (here a port named
// for the program's own key "slot", D-028) are refused before anything is planned, with the
// settings' own refusal naming the file and the key (PORT-SET-002). It fails if either is planned
// as though the settings declared nothing, which makes a worktree with no slot and hides the
// refusal, or if the refusal loses the file or the key; and, through the control case, if a
// project whose settings are fine is refused too.
func TestNewWorkIsRefusedWhenTheSettingsAre(t *testing.T) {
	t.Setenv("ENGINEROOM_SETTINGS", t.TempDir())
	t.Setenv("ZELLIJ_SOCKET_DIR", t.TempDir())
	main := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(filepath.Join(main, ".engineroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q", "-b", "main")
	gitRun(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	file := filepath.Join(main, ".engineroom", "engineroom.toml")
	cfg := &config.Config{Role: config.RoleEngine, Projects: []config.Project{{Name: "app", Path: main}}}

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("[[slot]]\nslot = 1\nports = { ui = 4100 }\n\n[[slot]]\nslot = 2\nports = { ui = 4110 }\n")
	if _, err := NewWork(cfg, "app", "w1", Start{}); err != nil {
		t.Fatalf("new work on a project whose settings are fine was refused: %v", err)
	}
	if _, err := NewRoleWork(cfg, "app", "designer", "b1", Start{}); err != nil {
		t.Fatalf("a role worktree on a project whose settings are fine was refused: %v", err)
	}

	write("[ports]\nslot = 4100\n")
	want := file + `: the port "slot" is the program's own key "slot"`
	if _, err := NewWork(cfg, "app", "w1", Start{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("new work: got %v, want a refusal holding %q", err, want)
	}
	if _, err := NewRoleWork(cfg, "app", "designer", "b1", Start{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("role worktree: got %v, want a refusal holding %q", err, want)
	}
}
