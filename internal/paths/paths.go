// Package paths says where the program keeps its configuration and its own state.
//
// Both follow the XDG base directory convention on every platform, including macOS, so a
// person (or a test) can move them with XDG_CONFIG_HOME and XDG_STATE_HOME, and nothing is
// written anywhere else.
package paths

import (
	"os"
	"path/filepath"
)

// App is the directory name used under the configuration and state roots.
const App = "engineroom"

// OldApp is the name the program had before (docs/decisions.md D-026). Its folders are moved to
// App's once (Migrate), so a machine keeps its configuration, settings, history and state.
const OldApp = "zellij-claude-hub"

// Migrate moves the program's configuration and state folders from the old name to the new one,
// where the old one exists and the new one does not yet, and returns what it moved, each as
// "old -> new". They are the program's own folders, so it moves them itself; moving is atomic, so
// two copies of the program starting at once cannot both move one. Where both exist, nothing is
// moved: the new one is used, and the old one is left for the developer to look at.
func Migrate() []string {
	root := func(env string, def ...string) string {
		if r := os.Getenv(env); r != "" {
			return r
		}
		return filepath.Join(append([]string{home()}, def...)...)
	}
	roots := []string{root("XDG_STATE_HOME", ".local", "state")}
	// A configuration file named outright (ENGINEROOM_CONFIG) is the developer's own place: its
	// folder is not the program's to move.
	if os.Getenv("ENGINEROOM_CONFIG") == "" {
		roots = append(roots, root("XDG_CONFIG_HOME", ".config"))
	}
	var moved []string
	for _, r := range roots {
		from, to := filepath.Join(r, OldApp), filepath.Join(r, App)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if os.Rename(from, to) == nil {
			moved = append(moved, from+" -> "+to)
		}
	}
	return moved
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}

// ConfigFile is the per-user configuration file. ENGINEROOM_CONFIG overrides it outright, for
// someone who keeps their configuration elsewhere.
func ConfigFile() string {
	if p := os.Getenv("ENGINEROOM_CONFIG"); p != "" {
		return p
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		root = filepath.Join(home(), ".config")
	}
	return filepath.Join(root, App, "config.toml")
}

// StateDir holds what the program itself writes: its history, layouts and session signals.
func StateDir() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		root = filepath.Join(home(), ".local", "state")
	}
	return filepath.Join(root, App)
}
