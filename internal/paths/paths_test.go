package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// The old folders are moved to the new name once, keeping what is in them; a second run moves
// nothing; where the new folder already exists, the old one is left alone (D-026). Fails if the
// configuration is not found under the new name, is moved twice, or overwrites a newer folder.
func TestMigrateMovesTheOldFoldersOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("ENGINEROOM_CONFIG", "")
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".config", OldApp, "config.toml"), "role = \"engine\"\n")
	write(filepath.Join(home, ".local", "state", OldApp, "history.jsonl"), "{}\n")

	if moved := Migrate(); len(moved) != 2 {
		t.Fatalf("moved %v", moved)
	}
	if b, err := os.ReadFile(ConfigFile()); err != nil || string(b) != "role = \"engine\"\n" {
		t.Fatalf("the configuration under the new name: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(StateDir(), "history.jsonl")); err != nil {
		t.Fatalf("the history under the new name: %v", err)
	}
	if moved := Migrate(); len(moved) != 0 {
		t.Fatalf("moved again: %v", moved)
	}

	write(filepath.Join(home, ".config", OldApp, "config.toml"), "old\n")
	if moved := Migrate(); len(moved) != 0 {
		t.Fatalf("an old folder was moved over the new one: %v", moved)
	}
	if b, _ := os.ReadFile(ConfigFile()); string(b) != "role = \"engine\"\n" {
		t.Fatalf("the new configuration was replaced: %q", b)
	}
}
