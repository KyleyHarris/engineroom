// Package engineroomfile reads the file that describes one project to the program: engineroom.toml. The same
// format is read from two places (docs/decisions.md D-013): the developer's own settings
// repository, which decides, and a repository's committed .engineroom/engineroom.toml, which only recommends.
// The program reads these files and, for a reservation it is asked to make, writes them; it
// never changes a number in them by itself.
//
// The format, every section optional:
//
//	reserve = ["20000-20099"]          # the ports this project owns on every machine
//
//	[ports]                            # the ports of the main clone, by name
//	ui = 4100
//
//	[slots]
//	record = ".engineroom-slot.json"         # written in each worktree that holds a slot
//	store = "engineroom-slots.json"          # who holds which slot, in the clone's .git folder
//
//	[[slot]]                           # one row per slot, written out, never calculated
//	slot = 1
//	ports = { ui = 4100 }
//	names = { database = "app" }       # names only, never a password
//
//	[derived]                          # more values for the record, built from the row
//	origin = "https://localhost:{{ui}}"
//
//	[[render]]                         # a template filled in for each worktree
//	template = "appsettings.Worktree.json.tmpl"
//	to = "server/appsettings.Worktree.json"
package engineroomfile

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Name is the file's name in a settings folder and in a repository's .engineroom folder.
const Name = "engineroom.toml"

// Path is where a repository keeps it, relative to a checkout.
var Path = filepath.Join(".engineroom", Name)

// BuiltIns are the keys the program gives every slot's record itself (slots.Values): its number,
// the worktree's folder, whether it is the main clone, the project's name and a temporary folder.
// A project's port, name or derived value may not take one of them (settings.Load).
var BuiltIns = []string{"slot", "worktreeRoot", "isMainClone", "project", "tmp"}

// Fold is a key as it appears in an environment variable's name: in capitals, anything that is
// not a letter or digit made an underscore. Keys that fold alike ("db-name", "db_name", "DB_Name")
// read as one name, so a project may hold only one of them (settings.Load).
func Fold(k string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(k) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// Port is one named port.
type Port struct {
	Name   string `json:"name"`
	Number int    `json:"port"`
}

// Range is an inclusive run of port numbers.
type Range struct {
	Lo, Hi int
}

func (r Range) String() string {
	if r.Lo == r.Hi {
		return strconv.Itoa(r.Lo)
	}
	return fmt.Sprintf("%d-%d", r.Lo, r.Hi)
}

// Contains reports whether n is in the range.
func (r Range) Contains(n int) bool { return n >= r.Lo && n <= r.Hi }

// Overlaps reports whether two ranges share a port.
func (r Range) Overlaps(o Range) bool { return r.Lo <= o.Hi && o.Lo <= r.Hi }

// Row is one slot: its number, its ports and its names.
type Row struct {
	Slot  int
	Ports []Port
	Names map[string]string
}

// Render is one template filled in for each worktree.
type Render struct {
	Template string `toml:"template"`
	To       string `toml:"to"`
}

// SlotSettings says where a slot's record and the claims are kept.
type SlotSettings struct {
	Record string `toml:"record"`
	Store  string `toml:"store"`
}

// File is one engineroom.toml, with which sections it holds, so a merge can tell a section left out from
// one written empty.
type File struct {
	// Path is the file read, and Dir its folder: templates are found relative to it.
	Path, Dir string
	Reserve   []Range
	Ports     []Port
	Slots     SlotSettings
	Rows      []Row
	Derived   map[string]string
	Render    []Render
	// Secrets are the files a checkout needs that are never committed (a local settings file
	// with keys, an environment file): paths inside the worktree, or "~/..." for one in the home
	// folder that every checkout of the project shares (a .NET user-secrets file). Their content
	// is kept in the settings folder's secrets/, encrypted in its repository (D-014).
	Secrets []string
	// Links are the addresses a checkout's stack serves, by label, as templates over the slot's
	// values ("https://localhost:{{ui}}/"), shown as links to open. Without them, each port is
	// shown as http://localhost:<port>.
	Links map[string]string
	// Tasks are a project's own named jobs beyond its stack, run in a checkout from the app or
	// by Claude, such as copying a database for testing.
	Tasks []Task
	Has   map[string]bool
}

// Task is one named job: what it is called, what it does, and the command that does it, run in
// the checkout by sh with the slot's environment. A command naming a file in the settings
// folder's own folder for the project (./name) runs that file.
type Task struct {
	Name  string `toml:"name"`
	About string `toml:"about"`
	Run   string `toml:"run"`
}

// placeholder is {{name}} in a derived value or a template. Double braces, so a JSON template's
// own braces are never taken for one.
var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// Read reads a repository's recommendation, <checkout>/.engineroom/engineroom.toml. A checkout without the file
// declares nothing: an empty File and no error.
func Read(checkout string) (File, error) {
	return ReadFile(filepath.Join(checkout, Path))
}

// ReadFile reads one engineroom.toml. A missing file is an empty File and no error, because most
// projects need nothing; a file that is there but wrong is an error naming the file and the
// entry, so a mistake is never read as "nothing declared".
func ReadFile(path string) (File, error) {
	f := File{Path: path, Dir: filepath.Dir(path), Has: map[string]bool{}}
	var raw struct {
		Reserve []string          `toml:"reserve"`
		Ports   map[string]any    `toml:"ports"`
		Slots   SlotSettings      `toml:"slots"`
		Slot    []map[string]any  `toml:"slot"`
		Derived map[string]string `toml:"derived"`
		Render  []Render          `toml:"render"`
		Secrets []string          `toml:"secrets"`
		Links   map[string]string `toml:"links"`
		Task    []Task            `toml:"task"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return File{Has: map[string]bool{}}, nil
		}
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	fail := func(format string, args ...any) (File, error) {
		return File{}, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...))
	}
	// The decoder leaves a map empty, with no error, when a table is written as a plain value,
	// which would read a mistake as "nothing declared".
	for _, table := range []string{"ports", "slots", "derived", "links"} {
		if md.IsDefined(table) && md.Type(table) != "Hash" {
			return fail("%s is a %s; it must be a table, [%s]", table, md.Type(table), table)
		}
		f.Has[table] = md.IsDefined(table)
	}
	f.Has["reserve"], f.Has["slot"], f.Has["render"] = md.IsDefined("reserve"), md.IsDefined("slot"), md.IsDefined("render")
	f.Has["secrets"] = md.IsDefined("secrets")
	f.Has["task"] = md.IsDefined("task")

	for _, s := range raw.Reserve {
		r, err := parseRange(s)
		if err != nil {
			return fail("reserve: %v", err)
		}
		f.Reserve = append(f.Reserve, r)
	}
	if f.Ports, err = ports("ports", raw.Ports); err != nil {
		return fail("%v", err)
	}
	f.Slots = raw.Slots
	for _, v := range []string{f.Slots.Record, f.Slots.Store} {
		if v != "" && (filepath.IsAbs(v) || strings.Contains(v, "..") || strings.ContainsRune(v, filepath.Separator)) {
			return fail("slots: %q must be a plain file name, not a path", v)
		}
	}
	seenSlot := map[int]bool{}
	for i, s := range raw.Slot {
		row, err := parseRow(s)
		if err != nil {
			return fail("slot %d: %v", i+1, err)
		}
		if seenSlot[row.Slot] {
			return fail("slot %d is written twice", row.Slot)
		}
		seenSlot[row.Slot] = true
		f.Rows = append(f.Rows, row)
	}
	sort.Slice(f.Rows, func(i, j int) bool { return f.Rows[i].Slot < f.Rows[j].Slot })
	f.Derived = raw.Derived
	for label, url := range raw.Links {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return fail("links.%s is %q; a link starts with http:// or https://", label, url)
		}
	}
	f.Links = raw.Links
	seenTask := map[string]bool{}
	for _, t := range raw.Task {
		if strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Run) == "" {
			return fail("task: every task needs a name and a run command")
		}
		if seenTask[strings.ToLower(t.Name)] {
			return fail("task %q is written twice", t.Name)
		}
		seenTask[strings.ToLower(t.Name)] = true
		f.Tasks = append(f.Tasks, t)
	}
	for _, r := range raw.Render {
		if r.Template == "" || r.To == "" {
			return fail("render needs both template and to")
		}
		if filepath.IsAbs(r.To) || strings.Contains(r.To, "..") {
			return fail("render to %q must be a path inside the worktree", r.To)
		}
		f.Render = append(f.Render, r)
	}
	seenSecret := map[string]bool{}
	for _, s := range raw.Secrets {
		rest := strings.TrimPrefix(s, "~/")
		if rest == "" || filepath.IsAbs(rest) || strings.HasPrefix(rest, "~") || slices.Contains(strings.Split(filepath.ToSlash(rest), "/"), "..") {
			return fail("secrets: %q must be a path inside the worktree, or ~/ and a path in the home folder", s)
		}
		if seenSecret[s] {
			return fail("secrets: %q is written twice", s)
		}
		seenSecret[s] = true
		f.Secrets = append(f.Secrets, s)
	}
	return f, nil
}

// ports reads a table of name = number. TOML tables have no order, so the ports are put in
// number order: whatever is built from them reads the same every time.
func ports(where string, table map[string]any) ([]Port, error) {
	var out []Port
	byNumber := map[int]string{}
	for name, v := range table {
		n, ok := v.(int64)
		if !ok {
			return nil, fmt.Errorf("%s.%s is %v, which is not a port number", where, name, v)
		}
		if n < 1 || n > 65535 {
			return nil, fmt.Errorf("%s.%s is %d; a port number is from 1 to 65535", where, name, n)
		}
		// Two names on one number would be one port under two names, most likely a copy and
		// paste mistake the developer wants to know about.
		if other, taken := byNumber[int(n)]; taken {
			a, b := other, name
			if b < a {
				a, b = b, a
			}
			return nil, fmt.Errorf("%s.%s and %s.%s are both %d", where, a, where, b, n)
		}
		byNumber[int(n)] = name
		out = append(out, Port{Name: name, Number: int(n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func parseRow(s map[string]any) (Row, error) {
	n, ok := s["slot"].(int64)
	if !ok || n < 1 {
		return Row{}, fmt.Errorf("slot must be a whole number from 1")
	}
	row := Row{Slot: int(n), Names: map[string]string{}}
	for k, v := range s {
		switch k {
		case "slot":
		case "ports":
			t, ok := v.(map[string]any)
			if !ok {
				return Row{}, fmt.Errorf("ports must be a table of name = number")
			}
			p, err := ports("ports", t)
			if err != nil {
				return Row{}, err
			}
			row.Ports = p
		case "names":
			t, ok := v.(map[string]any)
			if !ok {
				return Row{}, fmt.Errorf("names must be a table of name = text")
			}
			for nk, nv := range t {
				str, ok := nv.(string)
				if !ok {
					return Row{}, fmt.Errorf("names.%s is %v; a name is text", nk, nv)
				}
				row.Names[nk] = str
			}
		default:
			return Row{}, fmt.Errorf("unknown entry %q; a slot holds slot, ports and names", k)
		}
	}
	for _, p := range row.Ports {
		if _, clash := row.Names[p.Name]; clash {
			return Row{}, fmt.Errorf("%s is both a port and a name", p.Name)
		}
	}
	return row, nil
}

func parseRange(s string) (Range, error) {
	lo, hi, found := strings.Cut(strings.TrimSpace(s), "-")
	a, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return Range{}, fmt.Errorf("%q is not a port or a range like 20000-20099", s)
	}
	b := a
	if found {
		if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
			return Range{}, fmt.Errorf("%q is not a port or a range like 20000-20099", s)
		}
	}
	if a < 1 || b > 65535 || a > b {
		return Range{}, fmt.Errorf("%q is not a range of port numbers from 1 to 65535, low to high", s)
	}
	return Range{Lo: a, Hi: b}, nil
}

// Fill replaces each {{name}} in text with its value. A name with no value is an error naming it,
// so a template never writes a half-filled file.
func Fill(text string, values map[string]string) (string, error) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(text, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		v, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("no value for %s", strings.Join(missing, ", "))
	}
	return out, nil
}
