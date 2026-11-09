// Package stacklib is engineroom's stack helper for a project's up and down scripts (stack.sh): it
// starts each part of a worktree's stack in the background on the slot's ports, waits for it to
// answer, leaves alone a part already running for the worktree, and stops exactly what it
// started. It is part of the program, so every machine that has engineroom has the same helper,
// and a script reaches it with `. "$(engineroom stack-lib)"` wherever the script lives.
package stacklib

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"

	"github.com/KyleyHarris/engineroom/internal/paths"
)

//go:embed stack.sh
var script []byte

// Script is the helper's text.
func Script() []byte { return script }

// Path writes the helper to engineroom's state folder when it is missing or differs from this
// build's, and returns where it is. A script sources that file, so the helper it runs is always
// the installed build's.
func Path() (string, error) {
	path := filepath.Join(paths.StateDir(), "lib", "stack.sh")
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, script) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, script, 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}
