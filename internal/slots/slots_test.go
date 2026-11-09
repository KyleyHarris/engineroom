package slots

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/settings"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// repo makes a real clone with a commit, and a project with three slots whose settings are in a
// settings folder of the test's own.
func repo(t *testing.T) (string, settings.Project) {
	t.Helper()
	t.Setenv("ENGINEROOM_SETTINGS", t.TempDir())
	base := t.TempDir()
	main := filepath.Join(base, "app")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, main, "git", "init", "-q", "-b", "main")
	run(t, main, "git", "-c", "user.email=dev@engine.example", "-c", "user.name=dev", "commit", "-q", "--allow-empty", "-m", "init")
	p := settings.Project{Name: "app", Main: main, Slots: engineroomfile.SlotSettings{Record: ".engineroom-slot.json", Store: "engineroom-slots.json"}}
	for s := 1; s <= 3; s++ {
		p.Rows = append(p.Rows, engineroomfile.Row{Slot: s, Ports: []engineroomfile.Port{{Name: "ui", Number: 20000 + s*10}}, Names: map[string]string{"database": "app" + strings.Repeat("_", s-1)}})
	}
	return main, p
}

func worktree(t *testing.T, main, name string) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(main), "app-"+name)
	run(t, main, "git", "worktree", "add", "-q", "-b", name, dir)
	return dir
}

// The main clone takes slot 1 and each worktree the lowest free other slot; asking again gives the
// slot already held; a removed worktree's slot is free again; when all are held the claim is
// refused naming who holds each. It fails if a worktree took slot 1 ahead of a free one, two
// worktrees shared a slot, or a removed worktree kept its slot (PORT-SLOT-001, 002, 003).
func TestClaims(t *testing.T) {
	main, p := repo(t)
	a, b := worktree(t, main, "a"), worktree(t, main, "b")

	if r, _, err := Claim(p, a); err != nil || r.Slot != 2 {
		t.Fatalf("first worktree got %d, %v; want 2", r.Slot, err)
	}
	if r, _, _ := Claim(p, a); r.Slot != 2 {
		t.Fatalf("a second claim moved the worktree to slot %d", r.Slot)
	}
	if r, _, _ := Claim(p, b); r.Slot != 3 {
		t.Fatalf("second worktree got %d, want 3", r.Slot)
	}
	if r, _, _ := Claim(p, main); r.Slot != 1 {
		t.Fatalf("main clone got %d, want 1", r.Slot)
	}
	c := worktree(t, main, "c")
	var full *AllTaken
	if _, _, err := Claim(p, c); err == nil || !asAllTaken(err, &full) || len(full.Held) != 3 {
		t.Fatalf("a fourth claim of three slots was not refused with who holds them: %v", err)
	}
	run(t, main, "git", "worktree", "remove", a)
	if r, _, err := Claim(p, c); err != nil || r.Slot != 2 {
		t.Fatalf("the removed worktree's slot was not given out again: %d, %v", r.Slot, err)
	}
	if s, held, err := Release(p, c); err != nil || !held || s != 2 {
		t.Fatalf("release said %d %v %v", s, held, err)
	}
	if _, held, _ := Held(p, c); held {
		t.Fatal("a released worktree still holds a slot")
	}
	if _, held, _ := Release(p, c); held {
		t.Fatal("releasing twice reported a slot the second time")
	}
}

func asAllTaken(err error, out **AllTaken) bool {
	at, ok := err.(*AllTaken)
	*out = at
	return ok
}

// Two worktrees claiming at the same moment get different slots. It fails if the claims file is
// read and written without a lock, which is when two claims would both see a slot free.
func TestClaimsAtOnceNeverShare(t *testing.T) {
	main, p := repo(t)
	dirs := []string{worktree(t, main, "a"), worktree(t, main, "b")}
	got := make([]int, 2)
	var wg sync.WaitGroup
	for i, d := range dirs {
		wg.Add(1)
		go func(i int, d string) {
			defer wg.Done()
			r, _, err := Claim(p, d)
			if err != nil {
				t.Error(err)
			}
			got[i] = r.Slot
		}(i, d)
	}
	wg.Wait()
	if got[0] == got[1] {
		t.Fatalf("both worktrees got slot %d", got[0])
	}
}

// A claims file that cannot be read is started again empty, with a warning, rather than leaving
// every worktree unable to claim.
func TestAnUnreadableClaimsFileIsStartedAgain(t *testing.T) {
	main, p := repo(t)
	common := filepath.Join(main, ".git")
	if err := os.WriteFile(filepath.Join(common, p.Slots.Store), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, warning, err := Claim(p, worktree(t, main, "a"))
	if err != nil || r.Slot != 2 || !strings.Contains(warning, "could not be read") {
		t.Fatalf("got %d %q %v", r.Slot, warning, err)
	}
}

// The record holds ports as numbers, names and derived values as text; the environment file holds
// the same values; a template is filled in; and a file git does not ignore is warned about. It
// fails if a port is written as text (code reading it would get a string), a derived value is
// left unfilled, or the warning is missing.
func TestTheSlotsFilesAreWritten(t *testing.T) {
	main, p := repo(t)
	p.Derived = map[string]string{"origin": "https://localhost:{{ui}}"}
	tmplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmplDir, "a.tmpl"), []byte(`{"Urls": "{{origin}}", "Db": "{{database}}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p.Render, p.RenderDir = []engineroomfile.Render{{Template: "a.tmpl", To: "server/a.json"}}, tmplDir
	if err := os.WriteFile(filepath.Join(main, ".gitignore"), []byte(".engineroom-slot.*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := worktree(t, main, "a")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".engineroom-slot.*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row, _, _ := Claim(p, dir)
	written, warnings, err := Write(p, row, dir)
	if err != nil || len(written) != 3 {
		t.Fatalf("wrote %v, %v", written, err)
	}
	var rec map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, ".engineroom-slot.json"))
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["ui"] != float64(20020) || rec["slot"] != float64(2) || rec["origin"] != "https://localhost:20020" || rec["database"] != "app_" || rec["isMainClone"] != false {
		t.Fatalf("record %v", rec)
	}
	// Each value is named for its kind: the program's own built-ins under ENGINEROOM_ alone, a
	// port, a name and a derived value each under a prefix of its own, so a project's key can
	// never be read as one of the program's variables.
	env, _ := os.ReadFile(filepath.Join(dir, ".engineroom-slot.env"))
	for _, want := range []string{
		`ENGINEROOM_SLOT="2"`, `ENGINEROOM_PROJECT="app"`, `ENGINEROOM_ISMAINCLONE="false"`, `ENGINEROOM_WORKTREEROOT="`, `ENGINEROOM_TMP="`,
		`ENGINEROOM_PORT_UI="20020"`, `ENGINEROOM_NAME_DATABASE="app_"`, `ENGINEROOM_DERIVED_ORIGIN="https://localhost:20020"`,
	} {
		if !strings.Contains(string(env), want) {
			t.Fatalf("env lacks %s:\n%s", want, env)
		}
	}
	for _, unwanted := range []string{"ENGINEROOM_UI=", "ENGINEROOM_DATABASE=", "ENGINEROOM_ORIGIN="} {
		if strings.Contains(string(env), unwanted) {
			t.Fatalf("env names a project key without its kind (%s):\n%s", unwanted, env)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, "server", "a.json"))
	if string(got) != `{"Urls": "https://localhost:20020", "Db": "app_"}` {
		t.Fatalf("template %s", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "server/a.json is not ignored") {
		t.Fatalf("warnings %v", warnings)
	}
	if removed := Remove(p, dir); len(removed) != 3 {
		t.Fatalf("removed %v", removed)
	}
}

// A main clone that has not claimed runs on slot 1; a linked worktree that has not claimed has no
// slot. It fails if a worktree with no claim were given slot 1, which is the main clone's.
func TestEffective(t *testing.T) {
	main, p := repo(t)
	if r, held, _ := Effective(p, main); held || r.Slot != 1 {
		t.Fatalf("main clone: %d %v", r.Slot, held)
	}
	if r, _, _ := Effective(p, worktree(t, main, "a")); r.Slot != 0 {
		t.Fatalf("an unclaimed worktree got slot %d", r.Slot)
	}
}

func project(name string, reserve []engineroomfile.Range, ports ...int) settings.Project {
	p := settings.Project{Name: name, Reserve: reserve}
	row := engineroomfile.Row{Slot: 1}
	for i, n := range ports {
		row.Ports = append(row.Ports, engineroomfile.Port{Name: "p" + string(rune('a'+i)), Number: n})
	}
	if len(ports) > 0 {
		p.Rows = []engineroomfile.Row{row}
		p.Ports = row.Ports
	}
	return p
}

// Clashes are found and both sides named: two projects on one port, overlapping reservations, a
// slot port outside its own reservation. Projects that do not clash produce nothing. It fails if
// any of the three goes unreported, which is the case the reservation exists to prevent.
func TestCheck(t *testing.T) {
	a := project("app-a", nil, 4101)
	b := project("app-b", nil, 4101, 4110)
	c := project("app-c", []engineroomfile.Range{{Lo: 20000, Hi: 20099}}, 20050)
	d := project("app-d", []engineroomfile.Range{{Lo: 20090, Hi: 20199}}, 30000)
	got := Check([]settings.Project{a, b, c, d})
	want := []string{
		"app-a: port 4101 also belongs to app-b",
		"app-c: ports 20090-20099 also belongs to app-d",
		"app-d: slot 1 pa is port 30000, outside its reservation 20090-20199",
	}
	var text []string
	for _, p := range got {
		text = append(text, p.String())
	}
	if strings.Join(text, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(text, "\n"), strings.Join(want, "\n"))
	}
	if p := Check([]settings.Project{a, c}); len(p) != 0 {
		t.Fatalf("no clash, but got %v", p)
	}
}

// The next block skips every reserved or used port; the plan keeps slot 1 at today's numbers and
// gives slots 2 to 10 the block's runs; the text written back reads as the same table, with the
// project's own ports kept and the reservation ahead of every table. It fails if slot 1 moved
// (sign-in callbacks registered on it would break) or the written file read differently.
func TestReserve(t *testing.T) {
	taken := project("other", []engineroomfile.Range{{Lo: 20000, Hi: 20099}})
	app := project("app", nil, 4100, 4200)
	block, err := NextBlock([]settings.Project{taken, app})
	if err != nil || block != (engineroomfile.Range{Lo: 20100, Hi: 20199}) {
		t.Fatalf("block %v %v", block, err)
	}
	rows, err := Plan(app, block)
	if err != nil || len(rows) != SlotCount || rows[0].Ports[0].Number != 4100 || rows[1].Ports[0].Number != 20110 || rows[9].Ports[1].Number != 20191 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	path := filepath.Join(t.TempDir(), "engineroom.toml")
	existing := "[ports]\npa = 4100\npb = 4200\n"
	text, err := Add(existing, block, []engineroomfile.Range{{Lo: 4100, Hi: 4100}, {Lo: 4200, Hi: 4200}}, rows, engineroomfile.File{Path: path, Has: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := engineroomfile.ReadFile(path)
	if err != nil || len(f.Reserve) != 3 || len(f.Rows) != SlotCount || len(f.Ports) != 2 {
		t.Fatalf("written file reads back as %+v, %v\n%s", f, err, text)
	}
	if _, err := Add(text, block, nil, rows, f); err == nil {
		t.Fatal("a second reservation was written over the first")
	}
	if _, err := Plan(project("none", nil), block); err == nil {
		t.Fatal("a project with no ports was given slots")
	}
}

// A key is folded into a variable name the same way for every kind: capitals, and anything that is
// not a letter or digit an underscore. It fails if "db-name" and "db.name" were named differently
// from "db_name", which would let two keys that read as one variable both be written.
func TestEnvNamesFoldKeysByKind(t *testing.T) {
	p := settings.Project{Derived: map[string]string{"api-origin": "x"}}
	row := engineroomfile.Row{Slot: 1, Ports: []engineroomfile.Port{{Name: "web.ui", Number: 1}}, Names: map[string]string{"db-name": "n"}}
	for k, want := range map[string]string{
		"web.ui":       "ENGINEROOM_PORT_WEB_UI",
		"db-name":      "ENGINEROOM_NAME_DB_NAME",
		"api-origin":   "ENGINEROOM_DERIVED_API_ORIGIN",
		"worktreeRoot": "ENGINEROOM_WORKTREEROOT",
		"isMainClone":  "ENGINEROOM_ISMAINCLONE",
	} {
		if got := EnvName(p, row, k); got != want {
			t.Errorf("EnvName(%q) = %s, want %s", k, got, want)
		}
	}
}
