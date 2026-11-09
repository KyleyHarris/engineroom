package engineroomfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".engineroom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, Path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A checkout with no file declares nothing and is not an error: it would fail if a repository
// without .engineroom/ stopped every plan.
func TestNoFileDeclaresNothing(t *testing.T) {
	f, err := Read(t.TempDir())
	if err != nil || len(f.Ports) != 0 || len(f.Rows) != 0 {
		t.Fatalf("got %+v, %v; want nothing and no error", f, err)
	}
}

// Every section is read: the ports in number order whatever order the file names them in, the
// reservation, the slot rows in slot order with their ports and names, the derived values and the
// templates. It fails if a section is dropped, a row is misread, or the order follows the map.
func TestAWholeFileIsRead(t *testing.T) {
	dir := write(t, `reserve = ["20000-20099", "4100"]
[ports]
api = 4200
ui = 4100
[slots]
record = ".worktree-slot.json"
store = "app-slots.json"
[[slot]]
slot = 2
ports = { ui = 20010, api = 20011 }
names = { database = "app_wt2" }
[[slot]]
slot = 1
ports = { ui = 4100, api = 4200 }
names = { database = "app" }
[derived]
origin = "https://localhost:{{ui}}"
[[render]]
template = "a.tmpl"
to = "server/a.json"
`)
	f, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Port{{"ui", 4100}, {"api", 4200}}; !reflect.DeepEqual(f.Ports, want) {
		t.Fatalf("ports %+v, want %+v", f.Ports, want)
	}
	if want := []Range{{20000, 20099}, {4100, 4100}}; !reflect.DeepEqual(f.Reserve, want) {
		t.Fatalf("reserve %+v", f.Reserve)
	}
	if len(f.Rows) != 2 || f.Rows[0].Slot != 1 || f.Rows[1].Names["database"] != "app_wt2" || f.Rows[1].Ports[0] != (Port{"ui", 20010}) {
		t.Fatalf("rows %+v", f.Rows)
	}
	if f.Slots.Record != ".worktree-slot.json" || f.Slots.Store != "app-slots.json" || f.Derived["origin"] == "" || len(f.Render) != 1 {
		t.Fatalf("got %+v", f)
	}
	if !f.Has["ports"] || !f.Has["slot"] || f.Dir != filepath.Join(dir, ".engineroom") {
		t.Fatalf("sections or folder not recorded: %+v %s", f.Has, f.Dir)
	}
}

// Every way the file can be wrong is refused with the file and the entry named, never read as
// "nothing declared" (obligation O8). Boundaries 0, 1, 65535 and 65536 are covered.
func TestAWrongFileIsRefusedWithWhatIsWrong(t *testing.T) {
	for _, c := range []struct{ name, content, want string }{
		{"not toml", "[ports\nui = 1", "engineroom.toml"},
		{"text", "[ports]\nui = \"4100\"", "ports.ui is 4100, which is not a port number"},
		{"fraction", "[ports]\nui = 51.8", "ports.ui is 51.8, which is not a port number"},
		{"zero", "[ports]\nui = 0", "ports.ui is 0; a port number is from 1 to 65535"},
		{"too high", "[ports]\nui = 65536", "ports.ui is 65536"},
		{"twice", "[ports]\nweb = 4100\nui = 4100", "ports.ui and ports.web are both 4100"},
		{"not a table", "ports = 4100", "ports is a Integer; it must be a table"},
		{"bad range", "reserve = [\"20099-20000\"]", "low to high"},
		{"range text", "reserve = [\"lots\"]", "not a port or a range"},
		{"slot twice", "[[slot]]\nslot = 1\n[[slot]]\nslot = 1", "slot 1 is written twice"},
		{"slot zero", "[[slot]]\nslot = 0", "whole number from 1"},
		{"slot unknown", "[[slot]]\nslot = 1\nport = 3", "unknown entry \"port\""},
		{"name and port", "[[slot]]\nslot = 1\nports = { a = 1 }\nnames = { a = \"x\" }", "a is both a port and a name"},
		{"record path", "[slots]\nrecord = \"../x.json\"", "plain file name"},
		{"render outside", "[[render]]\ntemplate = \"a\"\nto = \"../x\"", "inside the worktree"},
		{"secret outside", "secrets = [\"../x\"]", "must be a path inside the worktree"},
		{"secret absolute", "secrets = [\"/etc/x\"]", "must be a path inside the worktree"},
		{"secret home up", "secrets = [\"~/../x\"]", "must be a path inside the worktree"},
		{"link scheme", "[links]\napp = \"localhost:1\"", "starts with http"},
		{"links not a table", "links = 1", "links is a Integer"},
		{"secret twice", "secrets = [\"a\", \"a\"]", "written twice"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Read(write(t, c.content))
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), Path) {
				t.Fatalf("got %v, want an error naming %s and saying %q", err, Path, c.want)
			}
		})
	}
	for _, ok := range []string{"[ports]\nlow = 1", "[ports]\nhigh = 65535", ""} {
		if _, err := Read(write(t, ok)); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
}

// A template is filled from its values, and a name with no value is refused with its name, so a
// half-filled file is never written. JSON's own braces are left alone.
func TestFill(t *testing.T) {
	got, err := Fill(`{"Urls": "https://localhost:{{ ui }}", "Db": "{{database}}"}`, map[string]string{"ui": "20010", "database": "app_wt2"})
	if err != nil || got != `{"Urls": "https://localhost:20010", "Db": "app_wt2"}` {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Fill("{{ui}} {{nope}}", map[string]string{"ui": "1"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a missing value was not named: %v", err)
	}
}
