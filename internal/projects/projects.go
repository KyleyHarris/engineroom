// Package projects registers repositories with the program, so every other action can refer to
// them by name (acceptance/projects).
//
// Registering changes only the program's own configuration. It never creates, moves or edits
// anything in the repository; it reads the repository to check it is one and to report the
// setup scripts it declares (docs/decisions.md D-006).
package projects

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/config"
)

// Scripts are the lifecycle scripts a repository may declare in its .engineroom folder, in the order
// of a worktree's life: status sits between up and down, as it asks whether what up started still
// runs (docs/decisions.md D-037).
var Scripts = []string{"create", "up", "status", "down", "teardown"}

// Declared lists which lifecycle scripts the repository at root declares: an executable file
// .engineroom/<name>.
func Declared(root string) []string {
	var out []string
	for _, name := range Scripts {
		info, err := os.Stat(filepath.Join(root, ".engineroom", name))
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			out = append(out, name)
		}
	}
	return out
}

// Result says what Add did.
type Result struct {
	Name     string
	Path     string
	Category string
	Already  bool
	// Recategorised is true when an already registered project was given a new category.
	Recategorised bool
	Declared      []string
}

// Options are the developer's choices when registering.
type Options struct {
	// Name is the project's name; its folder's name when empty.
	Name string
	// Category is the label it is listed under; none when empty.
	Category string
}

// Add registers the repository containing folder, under opt.Name (its folder's name when
// empty). The same repository, reached by any path or link, is registered once. Registering an
// already registered repository with a category sets that category and nothing else, so a
// category can be given or changed later with the same command (PROJ-CAT-001).
func Add(cfg *config.Config, folder string, opt Options) (Result, error) {
	name := opt.Name
	if err := config.ValidCategory(opt.Category); err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(folder)
	if err != nil {
		return Result{}, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a folder", abs)
	}
	out, err := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Result{}, fmt.Errorf("%s is not a git repository", abs)
	}
	root := resolve(strings.TrimSpace(string(out)))
	if name == "" {
		name = filepath.Base(root)
	}
	for i, p := range cfg.Projects {
		if resolve(p.Path) != root {
			continue
		}
		r := Result{Name: p.Name, Path: p.Path, Category: p.Category, Already: true, Declared: Declared(root)}
		if opt.Category != "" && opt.Category != p.Category {
			cfg.Projects[i].Category = opt.Category
			if err := cfg.Save(); err != nil {
				cfg.Projects[i].Category = p.Category
				return Result{}, err
			}
			r.Category, r.Recategorised = opt.Category, true
		}
		return r, nil
	}
	if _, taken := cfg.Project(name); taken {
		return Result{}, fmt.Errorf("the name %q is already used by another project; choose one with --name", name)
	}
	cfg.Projects = append(cfg.Projects, config.Project{Name: name, Path: root, Category: opt.Category})
	if err := cfg.Validate(); err != nil {
		cfg.Projects = cfg.Projects[:len(cfg.Projects)-1]
		return Result{}, err
	}
	if err := cfg.Save(); err != nil {
		cfg.Projects = cfg.Projects[:len(cfg.Projects)-1]
		return Result{}, err
	}
	return Result{Name: name, Path: root, Category: opt.Category, Declared: Declared(root)}, nil
}

// Remove forgets a project. Nothing on disk is touched.
func Remove(cfg *config.Config, name string) error {
	for i, p := range cfg.Projects {
		if p.Name == name {
			cfg.Projects = append(cfg.Projects[:i:i], cfg.Projects[i+1:]...)
			return cfg.Save()
		}
	}
	return fmt.Errorf("%q is not a registered project", name)
}

// Set changes one thing about a registered project: its category ("" for none), or whether it
// is pinned. Only the configuration changes; nothing on disk is touched.
func Set(cfg *config.Config, name, key, value string) error {
	for i, p := range cfg.Projects {
		if p.Name != name {
			continue
		}
		switch key {
		case "category":
			cfg.Projects[i].Category = strings.TrimSpace(value)
		case "pinned":
			if value != "on" && value != "off" {
				return fmt.Errorf("pinned is on or off, not %q", value)
			}
			cfg.Projects[i].Pinned = value == "on"
		default:
			return fmt.Errorf("a project's %q cannot be set; category and pinned can", key)
		}
		return cfg.Save()
	}
	return fmt.Errorf("%q is not a registered project", name)
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
