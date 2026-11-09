package projects

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/config"
)

func repo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("sh", "-c", `mkdir -p "$1" && cd "$1" && git init -q`, "sh", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return dir
}

func fresh(t *testing.T) *config.Config {
	t.Setenv("ENGINEROOM_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	return &config.Config{Role: config.RoleEngine}
}

// Projects obligation O3: a repository registered by path and again through a link, or from a
// folder inside it, is one project.
func TestSameRepositoryIsOneProject(t *testing.T) {
	cfg := fresh(t)
	r := repo(t, "project-a")
	link := filepath.Join(t.TempDir(), "link")
	_ = os.Symlink(r, link)
	sub := filepath.Join(r, "src")
	_ = os.MkdirAll(sub, 0o755)
	for _, p := range []string{r, link, sub} {
		if _, err := Add(cfg, p, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("%d projects: %+v", len(cfg.Projects), cfg.Projects)
	}
}

// Projects obligations O7 and O9: an unusable name, or one already taken by another
// repository, is refused and nothing is saved.
func TestNamesAreValidatedAndUnique(t *testing.T) {
	cfg := fresh(t)
	a, b := repo(t, "a"), repo(t, "b")
	if _, err := Add(cfg, a, Options{Name: "bad name"}); err == nil || !strings.Contains(err.Error(), "may only contain") {
		t.Fatalf("bad name accepted: %v", err)
	}
	if _, err := Add(cfg, a, Options{Name: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, b, Options{Name: "web"}); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("duplicate name accepted: %v", err)
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("a refused add changed the list: %+v", cfg.Projects)
	}
	saved, err := config.Load()
	if err != nil || len(saved.Projects) != 1 {
		t.Fatalf("saved: %+v %v", saved, err)
	}
}

func TestRemoveUnknownChangesNothing(t *testing.T) {
	cfg := fresh(t)
	if _, err := Add(cfg, repo(t, "a"), Options{}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(cfg, "nope"); err == nil {
		t.Fatal("removing an unknown name succeeded")
	}
	if len(cfg.Projects) != 1 {
		t.Fatal("removing an unknown name changed the list")
	}
}

// Projects obligation O8: a folder with spaces and non-ASCII characters registers under a name
// derived from it only when that name is valid; otherwise a name must be given.
func TestAwkwardFolderNeedsAName(t *testing.T) {
	cfg := fresh(t)
	r := repo(t, "My Prøject")
	if _, err := Add(cfg, r, Options{}); err == nil {
		t.Fatal("an unusable folder name was registered as a project name")
	}
	if res, err := Add(cfg, r, Options{Name: "my-project"}); err != nil || res.Path != r && !strings.HasSuffix(res.Path, "My Prøject") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestDeclaredScriptsMustBeExecutable(t *testing.T) {
	r := repo(t, "a")
	_ = os.MkdirAll(filepath.Join(r, ".engineroom"), 0o755)
	_ = os.WriteFile(filepath.Join(r, ".engineroom", "create"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(r, ".engineroom", "teardown"), []byte("#!/bin/sh\n"), 0o644)
	if got := Declared(r); len(got) != 1 || got[0] != "create" {
		t.Fatalf("declared %v", got)
	}
}

// PROJ-CAT-001: registering again with a category sets it on the one entry, registering again
// without one keeps it, and a category that would break its heading line is refused before
// anything is saved. Would catch a re-registration that drops the category or adds an entry.
func TestCategoryIsSetOnRegisteringAgain(t *testing.T) {
	cfg := fresh(t)
	r := repo(t, "project-a")
	if _, err := Add(cfg, r, Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := Add(cfg, r, Options{Category: "tools"})
	if err != nil || !res.Already || !res.Recategorised || res.Category != "tools" {
		t.Fatalf("%+v %v", res, err)
	}
	if res, err := Add(cfg, r, Options{}); err != nil || res.Recategorised || res.Category != "tools" {
		t.Fatalf("registering without a category changed it: %+v %v", res, err)
	}
	loaded, err := config.Load()
	if err != nil || len(loaded.Projects) != 1 || loaded.Projects[0].Category != "tools" {
		t.Fatalf("saved: %+v %v", loaded, err)
	}
	for _, bad := range []string{" tools", "a\nb", strings.Repeat("x", 41)} {
		if _, err := Add(cfg, repo(t, "project-b"), Options{Category: bad}); err == nil {
			t.Errorf("category %q was accepted", bad)
		}
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("a refused category registered something: %+v", cfg.Projects)
	}
}

// A project's category and pin are set and saved, and anything else is refused with what can be
// set. It fails if a pin were not saved, or an unknown key or a wrong value changed the file.
func TestCategoryAndPinAreSet(t *testing.T) {
	cfg := fresh(t)
	cfg.Projects = []config.Project{{Name: "app", Path: t.TempDir()}}
	if err := Set(cfg, "app", "pinned", "on"); err != nil || !cfg.Projects[0].Pinned {
		t.Fatalf("pin: %v %+v", err, cfg.Projects[0])
	}
	if err := Set(cfg, "app", "category", " tools "); err != nil || cfg.Projects[0].Category != "tools" {
		t.Fatalf("category: %v %+v", err, cfg.Projects[0])
	}
	for _, bad := range [][2]string{{"pinned", "yes"}, {"path", "/x"}} {
		if err := Set(cfg, "app", bad[0], bad[1]); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}
	if err := Set(cfg, "nope", "pinned", "on"); err == nil {
		t.Fatal("an unregistered project was set")
	}
	loaded, err := config.Load()
	if err != nil || !loaded.Projects[0].Pinned || loaded.Projects[0].Category != "tools" {
		t.Fatalf("not saved: %v %+v", err, loaded)
	}
}
