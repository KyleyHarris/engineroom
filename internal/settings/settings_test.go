package settings

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func clone(t *testing.T, origin string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	return dir
}

// A project is found in the settings by its GitHub owner and name, whatever the address form of
// its origin, and by its registered name when it has no origin. It fails if two clones of one
// repository reached by different addresses were given different settings.
func TestTheSettingsFolderIsFoundByOwnerAndName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", root)
	for _, origin := range []string{"git@github.com:Owner-A/App.git", "https://github.com/Owner-A/App", "git@github-work:Owner-A/App.git"} {
		if got := Dir("app", clone(t, origin)); got != filepath.Join(root, "projects", "Owner-A", "App") {
			t.Fatalf("%s: %s", origin, got)
		}
	}
	if got := Dir("app", t.TempDir()); got != filepath.Join(root, "projects", "app") {
		t.Fatalf("no origin: %s", got)
	}
}

// The developer's settings decide section by section and the repository fills only what they
// leave out; a section the developer writes empty still wins. Scripts are found in the settings
// first. It fails if the repository's numbers override the developer's, or a section the
// developer did not write is lost.
func TestYourSettingsDecideAndTheRepositoryRecommends(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", root)
	main := clone(t, "git@github.com:owner/app.git")
	mine := filepath.Join(root, "projects", "owner", "app")
	write(t, filepath.Join(mine, "engineroom.toml"), "[ports]\nui = 4100\n[slots]\nrecord = \".mine.json\"\n", 0o644)
	write(t, filepath.Join(main, ".engineroom", "engineroom.toml"), "reserve = [\"20000-20099\"]\n[ports]\nui = 9999\napi = 9998\n[slots]\nstore = \"repo-store.json\"\n[derived]\no = \"x{{ui}}\"\n", 0o644)
	write(t, filepath.Join(mine, "up"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(main, ".engineroom", "up"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(main, ".engineroom", "down"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(main, ".engineroom", "create"), "not executable\n", 0o644)

	p, err := Load("app", main, main)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Ports) != 1 || p.Ports[0].Number != 4100 || p.PortsFrom != Mine {
		t.Fatalf("ports %+v from %s", p.Ports, p.PortsFrom)
	}
	if len(p.Reserve) != 1 || p.ReserveFrom != Repo || p.Derived["o"] == "" {
		t.Fatalf("the repository's other sections were lost: %+v", p)
	}
	if p.Slots.Record != ".mine.json" || p.Slots.Store != "repo-store.json" {
		t.Fatalf("slots %+v", p.Slots)
	}
	if len(p.Rows) != 1 || p.Rows[0].Slot != 1 || p.RowsFrom != Mine {
		t.Fatalf("a project with ports and no slots should have slot 1: %+v", p.Rows)
	}
	if path, from, ok := Script("app", main, main, "up"); !ok || from != Mine || path != filepath.Join(mine, "up") {
		t.Fatalf("up: %s %s %v", path, from, ok)
	}
	if _, from, ok := Script("app", main, main, "down"); !ok || from != Repo {
		t.Fatalf("down from %s %v", from, ok)
	}
	if _, _, ok := Script("app", main, main, "create"); ok {
		t.Fatal("a script that is not executable was used")
	}
	write(t, filepath.Join(main, ".engineroom", "engineroom.toml"), "[ports]\nui = \"x\"\n", 0o644)
	if _, err := Load("app", main, main); err == nil {
		t.Fatal("a wrong recommendation was read as nothing")
	}
}

// A project's keys may not overwrite one another: a port, name or derived value that is one of the
// program's built-ins, or that is another key of a different kind, or that folds to the same name
// as another key, is refused when the settings are read, naming the file and both keys. The same
// port in [ports] and in every [[slot]] row is one key and is read. It fails if any clash below is
// read (a port named "project" replaced the project's name in the record before this check), if
// the error does not name the file holding the key, or if a project whose keys are distinct is
// refused.
func TestKeysThatWouldOverwriteOneAnotherAreRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENGINEROOM_SETTINGS", root)
	main := clone(t, "git@github.com:owner/app.git")
	mine := filepath.Join(root, "projects", "owner", "app", "engineroom.toml")
	repo := filepath.Join(main, ".engineroom", "engineroom.toml")

	for _, c := range []struct {
		name, own, recommended string
		file                   string // the file the error must name
		want                   []string
	}{
		{"a port named for a built-in", "[ports]\nproject = 4100\n", "", mine,
			[]string{`the port "project" is the program's own key "project"`}},
		{"a built-in in other capitals", "[ports]\nSlot = 4100\n", "", mine,
			[]string{`the port "Slot" is the program's own key "slot"`}},
		{"a name named for a built-in", "[[slot]]\nslot = 1\nports = { ui = 4100 }\nnames = { tmp = \"x\" }\n", "", mine,
			[]string{`the name "tmp" is the program's own key "tmp"`}},
		{"a derived value named for a built-in", "[ports]\nui = 4100\n[derived]\nWorktreeRoot = \"x\"\n", "", mine,
			[]string{`the derived value "WorktreeRoot" is the program's own key "worktreeRoot"`}},
		{"a derived value named as a port", "[ports]\nui = 4100\n[derived]\nui = \"x\"\n", "", mine,
			[]string{`the derived value "ui" and the port "ui"`, "(UI)"}},
		{"two keys that fold alike", "[[slot]]\nslot = 1\nports = { db-port = 4100 }\nnames = { db_name = \"a\", \"db-name\" = \"b\" }\n", "", mine,
			[]string{`the name "db_name" and the name "db-name"`, "(DB_NAME)"}},
		{"a port in one row and a name in another", "[[slot]]\nslot = 1\nports = { api = 4100 }\n\n[[slot]]\nslot = 2\nports = { ui = 4110 }\nnames = { api = \"x\" }\n", "", mine,
			[]string{`the name "api" and the port "api"`}},
		{"a clash between your settings and the repository's", "[ports]\nui = 4100\n", "[derived]\nUI = \"x\"\n", repo,
			[]string{`the derived value "UI" and the port "ui" in ` + mine}},
	} {
		t.Run(c.name, func(t *testing.T) {
			write(t, mine, c.own, 0o644)
			write(t, repo, c.recommended, 0o644)
			_, err := Load("app", main, main)
			if err == nil {
				t.Fatal("read without an error")
			}
			if !strings.HasPrefix(err.Error(), c.file+": ") {
				t.Fatalf("the error does not start with the file %s: %v", c.file, err)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("the error lacks %q: %v", w, err)
				}
			}
		})
	}

	t.Run("distinct keys and a port repeated in every row are read", func(t *testing.T) {
		write(t, mine, "[ports]\nui = 4100\n\n[[slot]]\nslot = 1\nports = { ui = 4100 }\nnames = { database = \"app\" }\n\n[[slot]]\nslot = 2\nports = { ui = 4110 }\nnames = { database = \"app_2\" }\n\n[derived]\norigin = \"https://localhost:{{ui}}\"\n", 0o644)
		write(t, repo, "", 0o644)
		if _, err := Load("app", main, main); err != nil {
			t.Fatal(err)
		}
	})
}
