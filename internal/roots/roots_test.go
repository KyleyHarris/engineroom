package roots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/config"
)

func fresh(t *testing.T) *config.Config {
	t.Setenv("ENGINEROOM_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	return &config.Config{Role: config.RoleEngine}
}

// PROJ-ROOT-001 and 002 below the journeys: a root is saved with its owner and account and named
// after its folder; a missing folder, the same folder twice, a taken name and a malformed owner
// are refused without saving anything. Would catch a root saved for a path that does not exist.
func TestAddChecksTheFolderAndNames(t *testing.T) {
	cfg := fresh(t)
	dir := filepath.Join(t.TempDir(), "owner-a")
	if _, err := Add(cfg, dir, "owner-a", "", ""); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing folder: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := Add(cfg, dir, "owner-a", "account-a", "")
	if err != nil || r.Name != "owner-a" || r.Account != "account-a" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Add(cfg, dir, "owner-a", "", "other"); err == nil {
		t.Fatal("the same folder was registered twice")
	}
	second := filepath.Join(t.TempDir(), "x")
	_ = os.MkdirAll(second, 0o755)
	if _, err := Add(cfg, second, "owner-b", "", "owner-a"); err == nil {
		t.Fatal("a taken root name was accepted")
	}
	if _, err := Add(cfg, second, "bad owner", "", ""); err == nil {
		t.Fatal("a malformed owner was accepted")
	}
	loaded, err := config.Load()
	if err != nil || len(loaded.Roots) != 1 {
		t.Fatalf("saved roots: %+v %v", loaded, err)
	}
}

// PROJ-CLONE-001 and 002: the plan clones from the owner's address into <root>/<repo> and then
// registers it with the category; a folder in the way, a taken project name or a bad name is
// refused before any command. Would catch a plan that clones over an existing folder.
func TestClonePlan(t *testing.T) {
	cfg := fresh(t)
	root := t.TempDir()
	cfg.Roots = []config.Root{{Name: "r", Path: root, Owner: "owner-a"}}
	p, err := Clone(cfg, "r", "widget", "tools")
	if err != nil {
		t.Fatal(err)
	}
	lines := p.Lines()
	dir := filepath.Join(root, "widget")
	if len(lines) != 2 || lines[0] != "git clone git@github.com:owner-a/widget.git "+dir || !strings.HasSuffix(lines[1], "--category tools") {
		t.Fatalf("%q", lines)
	}
	_ = os.MkdirAll(dir, 0o755)
	if _, err := Clone(cfg, "r", "widget", ""); err == nil || !strings.Contains(err.Error(), "engineroom project add") {
		t.Fatalf("a folder in the way: %v", err)
	}
	cfg.Projects = []config.Project{{Name: "gadget", Path: "/elsewhere"}}
	if _, err := Clone(cfg, "r", "gadget", ""); err == nil {
		t.Fatal("a taken project name was accepted")
	}
	for _, bad := range []string{"", "-x", "a b", "x.git", "../x"} {
		if _, err := Clone(cfg, "r", bad, ""); err == nil {
			t.Errorf("name %q was accepted", bad)
		}
	}
	if _, err := Clone(cfg, "nope", "widget", ""); err == nil {
		t.Fatal("an unknown root was accepted")
	}
}

// The gh command uses the root's account through engineroom gh only when an account is set, so a
// root without one never depends on engineroom being on PATH.
func TestGHUsesTheAccountWhenSet(t *testing.T) {
	if got := strings.Join(GH("", "repo", "list"), " "); got != "gh repo list" {
		t.Fatal(got)
	}
	if got := strings.Join(GH("account-a", "repo", "list"), " "); got != "engineroom gh --account account-a repo list" {
		t.Fatal(got)
	}
}
