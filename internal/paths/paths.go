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
