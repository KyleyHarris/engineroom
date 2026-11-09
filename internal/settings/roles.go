package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// RolesFile is the file in a project's settings folder that records which of its worktrees are
// kept for a role (docs/decisions.md D-024): a worktree named for what it is for (designer,
// tester, primary) rather than for one branch, that stays while branches come and go in it. It is
// in the developer's settings so every machine that syncs them knows the same roles, and it is the
// program's own file, written only through `engineroom roles`, so a hand-written engineroom.toml is never
// rewritten to record one.
const RolesFile = "roles.json"

type rolesFile struct {
	Roles []string `json:"roles"`
}

// Roles is the project's recorded roles, in name order; none when nothing is recorded.
func Roles(name, main string) ([]string, error) {
	path := filepath.Join(Dir(name, main), RolesFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f rolesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s is not readable: %w", path, err)
	}
	sort.Strings(f.Roles)
	return f.Roles, nil
}

// HasRole reports whether role is one of the project's recorded roles. A file that cannot be read
// counts as none: a worktree is then shown as branch work, and nothing is removed because of it.
func HasRole(name, main, role string) bool {
	roles, _ := Roles(name, main)
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// SetRole records role for the project (on) or forgets it (off). Doing either twice changes
// nothing the second time; forgetting the last role removes the file.
func SetRole(name, main, role string, on bool) error {
	roles, err := Roles(name, main)
	if err != nil {
		return err
	}
	var kept []string
	for _, r := range roles {
		if r != role {
			kept = append(kept, r)
		}
	}
	if on {
		kept = append(kept, role)
	}
	sort.Strings(kept)
	dir := Dir(name, main)
	path := filepath.Join(dir, RolesFile)
	if len(kept) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rolesFile{Roles: kept}, "", "  ")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
