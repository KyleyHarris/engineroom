// Package settings finds what the developer has set for each project, in their own settings
// folder, and merges it with what a project's repository recommends (docs/decisions.md D-013).
//
// The settings folder is the developer's: one folder, outside every repository, that they may
// keep as a private git repository of their own so it is the same on every machine. Per project
// it holds an engineroom.toml and the project's scripts, at projects/<owner>/<repository>/, keyed by the
// project's GitHub owner and name so every worktree and every machine finds the same entry.
//
// The developer's settings decide. A repository's committed .engineroom/ only recommends: a section the
// developer's engineroom.toml leaves out is taken from the repository's, and a script the developer has
// not written is the repository's. That lets a repository the developer does not own carry
// nothing of theirs, and lets one they do own ship defaults for everyone.
package settings

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// Source says where a setting came from, in the words the developer sees.
type Source string

const (
	// Mine is the developer's own settings folder.
	Mine Source = "your settings"
	// Repo is the project's committed .engineroom folder.
	Repo Source = "repository"
)

// Root is the settings folder. ENGINEROOM_SETTINGS overrides it outright; otherwise it sits beside the
// configuration file, under XDG_CONFIG_HOME, so a test or a person can move it.
func Root() string {
	if p := os.Getenv("ENGINEROOM_SETTINGS"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(paths.ConfigFile()), "settings")
}

// Identity is a project's GitHub owner and name, read from its origin ("owner/name"), or "" when
// it has no origin. Case is kept, so the folder reads as the repository is named.
func Identity(main string) string {
	out, err := exec.Command("git", "-C", main, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(string(out)), "/"), ".git")
	parts := strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// Dir is the project's folder in the settings: by GitHub owner and name when the project has an
// origin, otherwise by its registered name. When both exist the owner and name one wins, because
// that is the one every machine agrees on.
func Dir(name, main string) string {
	base := filepath.Join(Root(), "projects")
	if id := Identity(main); id != "" {
		return filepath.Join(base, filepath.FromSlash(id))
	}
	return filepath.Join(base, name)
}

// Project is one project's settings after the merge, with where each part came from.
type Project struct {
	Name, Main string
	// Dir is the project's settings folder, which may not exist yet.
	Dir string
	// Own and Recommended are the two files as read.
	Own, Recommended engineroomfile.File

	Reserve     []engineroomfile.Range
	ReserveFrom Source
	Ports       []engineroomfile.Port
	PortsFrom   Source
	// Rows are the slots. A project that names ports and no slots has one slot, slot 1, with
	// those ports: the main clone.
	Rows     []engineroomfile.Row
	RowsFrom Source
	Slots    engineroomfile.SlotSettings
	Derived  map[string]string
	Render   []engineroomfile.Render
	// RenderDir is the folder templates are read from: the settings folder or the repository's
	// .engineroom, whichever the templates came from.
	RenderDir string
	// Secrets are the files kept in the settings folder's secrets/ (secrets.go).
	Secrets []string
	// Links are the addresses a checkout's stack serves, as templates (engineroomfile.File.Links).
	Links map[string]string
	// Tasks are the project's own named jobs, and TaskDir the folder a task's ./file is in.
	Tasks   []engineroomfile.Task
	TaskDir string
}

// Load reads the developer's settings for a project and the recommendation in checkout, and
// merges them section by section, the developer's winning. A wrong file in either place is an
// error naming the file, never a silent "nothing set".
func Load(name, main, checkout string) (Project, error) {
	p := Project{Name: name, Main: main, Dir: Dir(name, main)}
	var err error
	if p.Own, err = engineroomfile.ReadFile(filepath.Join(p.Dir, engineroomfile.Name)); err != nil {
		return Project{}, err
	}
	if p.Recommended, err = engineroomfile.Read(checkout); err != nil {
		return Project{}, err
	}
	pick := func(section string) (engineroomfile.File, Source) {
		if p.Own.Has[section] {
			return p.Own, Mine
		}
		return p.Recommended, Repo
	}
	f, src := pick("reserve")
	p.Reserve, p.ReserveFrom = f.Reserve, src
	f, src = pick("ports")
	p.Ports, p.PortsFrom = f.Ports, src
	// The file each kind of key was read from, so a clash names the file to change.
	portsPath, rowsPath := f.Path, f.Path
	if fr, srcr := pick("slot"); len(fr.Rows) > 0 {
		p.Rows, p.RowsFrom, rowsPath = fr.Rows, srcr, fr.Path
	} else if len(p.Ports) > 0 {
		p.Rows, p.RowsFrom = []engineroomfile.Row{{Slot: 1, Ports: p.Ports, Names: map[string]string{}}}, p.PortsFrom
	}
	f, _ = pick("slots")
	p.Slots = f.Slots
	// Each setting of [slots] falls back on its own, so the developer can rename the record and
	// keep the repository's store, or the other way round.
	if p.Slots.Record == "" {
		p.Slots.Record = p.Recommended.Slots.Record
	}
	if p.Slots.Store == "" {
		p.Slots.Store = p.Recommended.Slots.Store
	}
	if p.Slots.Record == "" {
		p.Slots.Record = ".engineroom-slot.json"
	}
	if p.Slots.Store == "" {
		p.Slots.Store = "engineroom-slots.json"
	}
	f, _ = pick("derived")
	p.Derived = f.Derived
	if err := checkKeys(p, portsPath, rowsPath, f.Path); err != nil {
		return Project{}, err
	}
	f, _ = pick("render")
	p.Render, p.RenderDir = f.Render, f.Dir
	f, _ = pick("secrets")
	p.Secrets = f.Secrets
	f, _ = pick("links")
	p.Links = f.Links
	f, _ = pick("task")
	p.Tasks, p.TaskDir = f.Tasks, f.Dir
	return p, nil
}

// key is one key of a slot's record as declared: what kind it is, as the developer would say it,
// and the file it is in (empty for the program's own built-ins).
type key struct{ kind, name, path string }

// checkKeys refuses a project whose keys would overwrite one another. Every key of a slot (the
// program's built-ins, each port, each name, each derived value) is one value in the slot's
// record and in a template's {{key}}, and is named in an environment variable from its folded
// form (engineroomfile.Fold). So a port, name or derived value may not be a built-in, nor another
// key of a different kind, nor a key that folds alike ("db-name" beside "db_name"), compared after
// the merge so a clash between the developer's file and the repository's is caught too. Without
// it, a port named "project" silently replaced the project's name in the record. The same key of
// the same kind in [ports] and in every [[slot]] row is one key, not a clash. The error names the
// file and the key, and which other key it clashes with.
func checkKeys(p Project, portsPath, rowsPath, derivedPath string) error {
	var keys []key
	for _, b := range engineroomfile.BuiltIns {
		keys = append(keys, key{"built-in", b, ""})
	}
	for _, port := range p.Ports {
		keys = append(keys, key{"port", port.Name, portsPath})
	}
	for _, row := range p.Rows {
		for _, port := range row.Ports {
			keys = append(keys, key{"port", port.Name, rowsPath})
		}
		names := make([]string, 0, len(row.Names))
		for n := range row.Names {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			keys = append(keys, key{"name", n, rowsPath})
		}
	}
	derived := make([]string, 0, len(p.Derived))
	for d := range p.Derived {
		derived = append(derived, d)
	}
	sort.Strings(derived)
	for _, d := range derived {
		keys = append(keys, key{"derived value", d, derivedPath})
	}

	seen := map[string]key{}
	for _, k := range keys {
		fold := engineroomfile.Fold(k.name)
		first, taken := seen[fold]
		if !taken {
			seen[fold] = k
			continue
		}
		if first.kind == k.kind && first.name == k.name {
			continue
		}
		if first.path == "" {
			return fmt.Errorf("%s: the %s %q is the program's own key %q, which every slot already has; give it another name", k.path, k.kind, k.name, first.name)
		}
		where := ""
		if first.path != k.path {
			where = " in " + first.path
		}
		return fmt.Errorf("%s: the %s %q and the %s %q%s are one key once case and punctuation are set aside (%s); give one of them another name", k.path, k.kind, k.name, first.kind, first.name, where, fold)
	}
	return nil
}

// HasSlots reports whether the project declares any ports, and so whether its worktrees claim
// slots.
func (p Project) HasSlots() bool { return len(p.Rows) > 0 }

// Script finds one of a project's lifecycle scripts for a checkout: the developer's own in the
// settings folder first, then the repository's .engineroom/<name>. Only an executable file counts, as
// before (D-006).
func Script(name, main, checkout, script string) (path string, from Source, ok bool) {
	mine := filepath.Join(Dir(name, main), script)
	if executable(mine) {
		return mine, Mine, true
	}
	repo := filepath.Join(checkout, ".engineroom", script)
	if executable(repo) {
		return repo, Repo, true
	}
	return "", "", false
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
