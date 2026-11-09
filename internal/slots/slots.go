// Package slots gives each worktree of a project its own numbered slot: its own ports and names,
// so several worktrees of one project run side by side without reaching each other's servers or
// data (docs/decisions.md D-005, D-013).
//
// A slot belongs to a worktree folder, never a branch: two sessions can share a branch, and a
// folder survives switching branches inside it. Who holds which slot is kept in one file in the
// clone's shared .git folder, keyed by the worktree's top-level path, so every worktree of the
// clone sees every claim and the file goes when the clone goes. Claims for worktrees git no
// longer lists are dropped whenever the file is read, so a removed worktree never keeps a slot.
//
// The file's shape is {"assignments": {"<worktree path>": <slot>}}, the shape a repository's own
// slot script may already use, so the program and that script can share one file and never
// disagree about who holds what.
package slots

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/settings"
)

// ErrNoSlots is a slot request for a project that declares no ports.
var ErrNoSlots = errors.New("the project declares no ports, so its worktrees have no slots")

// AllTaken is a claim when every slot is held.
type AllTaken struct{ Held map[int]string }

func (e *AllTaken) Error() string {
	var who []string
	for s, p := range e.Held {
		who = append(who, fmt.Sprintf("slot %d by %s", s, p))
	}
	sort.Strings(who)
	return "every slot is held (" + strings.Join(who, "; ") + "). Clean up a worktree that is finished, or add a slot to the project's settings."
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Top is the worktree's own top-level folder as git names it, which is the key a claim is kept
// under.
func Top(checkout string) (string, error) { return git(checkout, "rev-parse", "--show-toplevel") }

// commonDir is the clone's shared .git folder. The absolute form is asked for because the plain
// one is relative in the main clone and absolute in a linked worktree.
func commonDir(checkout string) (string, error) {
	return git(checkout, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// IsMainClone reports whether checkout is the clone itself rather than a linked worktree: only
// there is the worktree's own .git folder the shared one.
func IsMainClone(checkout string) bool {
	own, err1 := git(checkout, "rev-parse", "--path-format=absolute", "--git-dir")
	common, err2 := commonDir(checkout)
	return err1 == nil && err2 == nil && own == common
}

func liveWorktrees(checkout string) (map[string]bool, error) {
	out, err := git(checkout, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			live[p] = true
		}
	}
	return live, nil
}

// update reads the claims under an exclusive lock, drops claims for worktrees git no longer
// lists, lets change alter them, and writes them back before the lock is let go, so two claims at
// once can never take the same slot. A file that cannot be read is treated as empty with a
// warning, rather than leaving every worktree unable to claim: the cost is at most a claim made
// again.
func update(checkout, store string, change func(map[string]int) error) (map[string]int, string, error) {
	common, err := commonDir(checkout)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(common, store)
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, "", err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	var warning string
	claims := map[string]int{}
	if b, err := os.ReadFile(path); err == nil {
		var f struct {
			Assignments map[string]int `json:"assignments"`
		}
		if json.Unmarshal(b, &f) != nil {
			warning = path + " could not be read, so it was started again empty."
		} else if f.Assignments != nil {
			claims = f.Assignments
		}
	}
	live, err := liveWorktrees(checkout)
	if err != nil {
		return nil, "", err
	}
	for p := range claims {
		if !live[p] {
			delete(claims, p)
		}
	}
	before, _ := json.Marshal(claims)
	if change != nil {
		if err := change(claims); err != nil {
			return nil, warning, err
		}
	}
	after, _ := json.Marshal(claims)
	if string(before) != string(after) || change != nil {
		b, _ := json.MarshalIndent(map[string]any{"assignments": claims}, "", "  ")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
			return nil, warning, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, warning, err
		}
	}
	return claims, warning, nil
}

// order is the order a claim tries slots in: ascending, except that slot 1 is taken first by the
// main clone and last by a linked worktree, so the clone worked in most keeps the numbers it has
// always had.
func order(rows []engineroomfile.Row, main bool) []int {
	var out, one []int
	for _, r := range rows {
		if r.Slot == 1 && !main {
			one = append(one, 1)
			continue
		}
		out = append(out, r.Slot)
	}
	return append(out, one...)
}

func rowOf(p settings.Project, slot int) (engineroomfile.Row, bool) {
	for _, r := range p.Rows {
		if r.Slot == slot {
			return r, true
		}
	}
	return engineroomfile.Row{}, false
}

// Claim gives checkout a slot of project p and returns it. Asking again returns the slot already
// held, so a claim can be repeated safely.
func Claim(p settings.Project, checkout string) (engineroomfile.Row, string, error) {
	if !p.HasSlots() {
		return engineroomfile.Row{}, "", ErrNoSlots
	}
	top, err := Top(checkout)
	if err != nil {
		return engineroomfile.Row{}, "", err
	}
	main := IsMainClone(checkout)
	var got int
	_, warning, err := update(checkout, p.Slots.Store, func(c map[string]int) error {
		if held, ok := c[top]; ok {
			if _, exists := rowOf(p, held); exists {
				got = held
				return nil
			}
			// The project's settings no longer have that slot: the claim is moved to one they do.
			delete(c, top)
		}
		used := held(c, checkout, main)
		for _, s := range order(p.Rows, main) {
			if _, taken := used[s]; !taken {
				c[top], got = s, s
				return nil
			}
		}
		return &AllTaken{Held: used}
	})
	if err != nil {
		return engineroomfile.Row{}, warning, err
	}
	row, _ := rowOf(p, got)
	return row, warning, nil
}

// held is who holds each slot, as a claim for a worktree sees it: every claim, and, while the
// main clone has claimed nothing, slot 1 for it, since a main clone with no claim runs on slot 1
// (Effective). Without that, the first worktree of a project with one slot would be given the
// main clone's ports and its stack would be the main clone's (PORT-SLOT-004).
func held(c map[string]int, checkout string, main bool) map[int]string {
	used := map[int]string{}
	for path, s := range c {
		used[s] = path
	}
	if main {
		return used
	}
	if mainTop, err := mainClone(checkout); err == nil {
		if _, claimed := c[mainTop]; !claimed {
			if _, taken := used[1]; !taken {
				used[1] = mainTop + " (the main clone, which runs on slot 1 until it claims another)"
			}
		}
	}
	return used
}

// mainClone is the folder of the clone a checkout belongs to: the parent of the shared .git.
func mainClone(checkout string) (string, error) {
	common, err := commonDir(checkout)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(common, string(filepath.Separator)+".git") {
		return "", fmt.Errorf("%s is not inside a clone's .git", common)
	}
	return filepath.Dir(common), nil
}

// NextFor is the slot new work on project p would be given, read from the claims of the clone at
// main without claiming it, so new work can be refused before anything is created when every
// slot is held.
func NextFor(p settings.Project, main string) (int, error) {
	if !p.HasSlots() {
		return 0, ErrNoSlots
	}
	claims, _, err := update(main, p.Slots.Store, nil)
	if err != nil {
		return 0, err
	}
	used := held(claims, main, false)
	for _, s := range order(p.Rows, false) {
		if _, taken := used[s]; !taken {
			return s, nil
		}
	}
	return 0, &AllTaken{Held: used}
}

// Release gives back checkout's slot. Releasing a worktree that holds none does nothing.
func Release(p settings.Project, checkout string) (int, bool, error) {
	top, err := Top(checkout)
	if err != nil {
		return 0, false, err
	}
	var slot int
	var held bool
	_, _, err = update(checkout, p.Slots.Store, func(c map[string]int) error {
		slot, held = c[top]
		delete(c, top)
		return nil
	})
	return slot, held, err
}

// Held is the slot checkout holds, if any.
func Held(p settings.Project, checkout string) (engineroomfile.Row, bool, error) {
	top, err := Top(checkout)
	if err != nil {
		return engineroomfile.Row{}, false, err
	}
	claims, _, err := update(checkout, p.Slots.Store, nil)
	if err != nil {
		return engineroomfile.Row{}, false, err
	}
	if s, ok := claims[top]; ok {
		if row, exists := rowOf(p, s); exists {
			return row, true, nil
		}
	}
	return engineroomfile.Row{}, false, nil
}

// Effective is the slot checkout runs on: the one it holds, or, for a main clone that has not
// claimed, slot 1, which is by definition what the project used before slots. A linked worktree
// that holds none has no slot: it would collide with the main clone.
func Effective(p settings.Project, checkout string) (engineroomfile.Row, bool, error) {
	row, held, err := Held(p, checkout)
	if err != nil || held {
		return row, held, err
	}
	if IsMainClone(checkout) {
		if r, ok := rowOf(p, 1); ok {
			return r, false, nil
		}
	}
	return engineroomfile.Row{}, false, nil
}

// Values is everything a slot's record holds, as text: the slot number, the worktree, the
// project, a temporary folder, each port and name of the row, then each derived value filled in
// from those.
func Values(p settings.Project, row engineroomfile.Row, checkout string) (map[string]string, error) {
	top, err := Top(checkout)
	if err != nil {
		top = checkout
	}
	v := map[string]string{
		"slot":         strconv.Itoa(row.Slot),
		"worktreeRoot": top,
		"isMainClone":  strconv.FormatBool(IsMainClone(checkout)),
		"project":      p.Name,
		"tmp":          strings.TrimRight(os.TempDir(), string(filepath.Separator)),
	}
	for _, port := range row.Ports {
		v[port.Name] = strconv.Itoa(port.Number)
	}
	for k, n := range row.Names {
		v[k] = n
	}
	names := make([]string, 0, len(p.Derived))
	for k := range p.Derived {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		filled, err := engineroomfile.Fill(p.Derived[k], v)
		if err != nil {
			return nil, fmt.Errorf("derived %s: %w", k, err)
		}
		v[k] = filled
	}
	return v, nil
}

// recordJSON is the record as JSON: ports and the slot as numbers, so code reading a port gets a
// number, and everything else as text.
func recordJSON(row engineroomfile.Row, values map[string]string) []byte {
	out := map[string]any{}
	for k, s := range values {
		out[k] = s
	}
	out["slot"] = row.Slot
	out["isMainClone"] = values["isMainClone"] == "true"
	for _, port := range row.Ports {
		out[port.Name] = port.Number
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return append(b, '\n')
}

// EnvName is a record key as an environment variable. The prefix says what kind of value it is, so
// a project's own key can never take a name the program uses for itself: a built-in is
// ENGINEROOM_<KEY>, a port ENGINEROOM_PORT_<KEY>, a name ENGINEROOM_NAME_<KEY> and a derived value
// ENGINEROOM_DERIVED_<KEY>, each key folded (engineroomfile.Fold). A key of none of those kinds is
// the project's own and reads as a derived value would, the last kind Values fills in. The kinds
// are checked in the reverse of the order Values fills them, so were a key ever named twice it
// would be named for the value it holds; settings.Load refuses such a key before it gets here.
func EnvName(p settings.Project, row engineroomfile.Row, k string) string {
	fold := engineroomfile.Fold(k)
	if _, ok := p.Derived[k]; ok {
		return "ENGINEROOM_DERIVED_" + fold
	}
	if _, ok := row.Names[k]; ok {
		return "ENGINEROOM_NAME_" + fold
	}
	for _, port := range row.Ports {
		if port.Name == k {
			return "ENGINEROOM_PORT_" + fold
		}
	}
	if slices.Contains(engineroomfile.BuiltIns, k) {
		return "ENGINEROOM_" + fold
	}
	return "ENGINEROOM_DERIVED_" + fold
}

// EnvFile is the record's environment file beside it: the record's name with .env in place of
// its extension.
func EnvFile(record string) string {
	return strings.TrimSuffix(record, filepath.Ext(record)) + ".env"
}

// Write writes a slot's files into the worktree: the record (JSON), the same values as an
// environment file, and each template the project's settings name, filled in. Every file is
// derived from the same values in one pass, so they cannot disagree. Writing again writes the
// same files. It returns the files written and a warning for each one git does not ignore,
// because a slot's files belong to this worktree and must never be committed.
func Write(p settings.Project, row engineroomfile.Row, checkout string) (written, warnings []string, err error) {
	values, err := Values(p, row, checkout)
	if err != nil {
		return nil, nil, err
	}
	top := values["worktreeRoot"]
	files := map[string][]byte{}
	files[p.Slots.Record] = recordJSON(row, values)

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var env strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&env, "%s=%s\n", EnvName(p, row, k), strconv.Quote(values[k]))
	}
	files[EnvFile(p.Slots.Record)] = []byte(env.String())

	for _, r := range p.Render {
		tmpl, err := os.ReadFile(filepath.Join(p.RenderDir, r.Template))
		if err != nil {
			return nil, nil, fmt.Errorf("template %s: %w", r.Template, err)
		}
		filled, err := engineroomfile.Fill(string(tmpl), values)
		if err != nil {
			return nil, nil, fmt.Errorf("template %s: %w", r.Template, err)
		}
		files[r.To] = []byte(filled)
	}

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		path := filepath.Join(top, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, warnings, err
		}
		if err := os.WriteFile(path, files[rel], 0o644); err != nil {
			return written, warnings, err
		}
		written = append(written, path)
		if exec.Command("git", "-C", top, "check-ignore", "-q", rel).Run() != nil {
			warnings = append(warnings, rel+" is not ignored by git, so it could be committed. `engineroom slot ignore` adds it to the repository's .gitignore.")
		}
	}
	return written, warnings, nil
}

// Remove deletes the files Write writes, for a worktree giving its slot back.
func Remove(p settings.Project, checkout string) []string {
	top, err := Top(checkout)
	if err != nil {
		return nil
	}
	var removed []string
	rels := []string{p.Slots.Record, EnvFile(p.Slots.Record)}
	for _, r := range p.Render {
		rels = append(rels, r.To)
	}
	for _, rel := range rels {
		path := filepath.Join(top, rel)
		if os.Remove(path) == nil {
			removed = append(removed, path)
		}
	}
	return removed
}

// Link is one address a checkout's stack serves, to open in a browser.
type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Links are the addresses a checkout running on row serves: the project's links filled from the
// slot's values, in label order, or, when the project names none, http://localhost:<port> for
// each port. A link that cannot be filled is left out rather than shown half-filled.
func Links(p settings.Project, row engineroomfile.Row, checkout string) []Link {
	if len(p.Links) == 0 {
		out := make([]Link, 0, len(row.Ports))
		for _, port := range row.Ports {
			out = append(out, Link{Name: port.Name, URL: "http://localhost:" + strconv.Itoa(port.Number)})
		}
		return out
	}
	values, err := Values(p, row, checkout)
	if err != nil {
		return nil
	}
	labels := make([]string, 0, len(p.Links))
	for k := range p.Links {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	var out []Link
	for _, k := range labels {
		if url, err := engineroomfile.Fill(p.Links[k], values); err == nil {
			out = append(out, Link{Name: k, URL: url})
		}
	}
	return out
}

// Files are the files a slot writes into a worktree, relative to its top: the record, its
// environment file and each rendered template.
func Files(p settings.Project) []string {
	out := []string{p.Slots.Record, EnvFile(p.Slots.Record)}
	for _, r := range p.Render {
		out = append(out, r.To)
	}
	return out
}

// Ignore adds to the .gitignore at checkout's top each slot file git does not ignore there yet,
// under a comment saying why, and returns those it added. Done only when the developer asks
// (`engineroom slot ignore`): it changes a file of the repository, which they then commit. Asking
// again adds nothing.
func Ignore(p settings.Project, checkout string) ([]string, error) {
	top, err := Top(checkout)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, rel := range Files(p) {
		if exec.Command("git", "-C", top, "check-ignore", "-q", "--no-index", rel).Run() != nil {
			missing = append(missing, rel)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	path := filepath.Join(top, ".gitignore")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var b strings.Builder
	b.Write(old)
	if len(old) > 0 && !strings.HasSuffix(string(old), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n# engineroom: a worktree's slot (its ports and names) and the files made from it. Which slot a\n# worktree holds is a fact about one machine, never part of the code.\n")
	for _, rel := range missing {
		b.WriteString("/" + filepath.ToSlash(rel) + "\n")
	}
	return missing, os.WriteFile(path, []byte(b.String()), 0o644)
}
