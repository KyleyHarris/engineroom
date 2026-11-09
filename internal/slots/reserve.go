package slots

import (
	"fmt"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/settings"
)

// Band is where the program hands out new blocks of ports: clear of the ports development tools
// pick by default (3000, 5173, 8080), of the storage emulator's 10000s, and of the ports the
// operating system hands out for outgoing connections (49152 and up).
var Band = engineroomfile.Range{Lo: 20000, Hi: 29999}

const (
	// BlockSize is one project's block: SlotCount slots of SlotSize ports; ten slots is plenty
	// for the worktrees one project runs at once.
	BlockSize = SlotCount * SlotSize
	SlotCount = 10
	SlotSize  = 10
)

// Problem is one thing wrong with the ports across every project, said with both sides named.
type Problem struct {
	Project string
	Text    string
}

func (p Problem) String() string { return p.Project + ": " + p.Text }

// owned is every port a project claims: its reservation and every port in its slots.
func owned(p settings.Project) []engineroomfile.Range {
	out := append([]engineroomfile.Range(nil), p.Reserve...)
	for _, r := range p.Rows {
		for _, port := range r.Ports {
			out = append(out, engineroomfile.Range{Lo: port.Number, Hi: port.Number})
		}
	}
	return out
}

// Check finds every clash across the projects: two projects' reservations or slot ports sharing
// a number, a slot port outside its own project's reservation, and the same port twice within
// one project's slots. It never renumbers anything; fixing a clash is a change to a project's
// settings (D-005).
func Check(projects []settings.Project) []Problem {
	var out []Problem
	for i, a := range projects {
		// Inside one project: each port number belongs to one slot only.
		seen := map[int]string{}
		for _, r := range a.Rows {
			for _, port := range r.Ports {
				where := fmt.Sprintf("slot %d %s", r.Slot, port.Name)
				if other, dup := seen[port.Number]; dup {
					out = append(out, Problem{a.Name, fmt.Sprintf("port %d is both %s and %s", port.Number, other, where)})
				}
				seen[port.Number] = where
				if len(a.Reserve) > 0 && !inAny(a.Reserve, port.Number) {
					out = append(out, Problem{a.Name, fmt.Sprintf("%s is port %d, outside its reservation %s", where, port.Number, ranges(a.Reserve))})
				}
			}
		}
		for _, b := range projects[i+1:] {
			if clash := overlap(owned(a), owned(b)); clash != "" {
				out = append(out, Problem{a.Name, fmt.Sprintf("%s also belongs to %s", clash, b.Name)})
			}
		}
	}
	return out
}

func inAny(rs []engineroomfile.Range, n int) bool {
	for _, r := range rs {
		if r.Contains(n) {
			return true
		}
	}
	return false
}

func ranges(rs []engineroomfile.Range) string {
	s := make([]string, len(rs))
	for i, r := range rs {
		s[i] = r.String()
	}
	return strings.Join(s, ", ")
}

// overlap names the ports two sets share, or "".
func overlap(a, b []engineroomfile.Range) string {
	var shared []string
	seen := map[string]bool{}
	for _, x := range a {
		for _, y := range b {
			if !x.Overlaps(y) {
				continue
			}
			lo, hi := max(x.Lo, y.Lo), min(x.Hi, y.Hi)
			s := engineroomfile.Range{Lo: lo, Hi: hi}.String()
			if !seen[s] {
				seen[s] = true
				shared = append(shared, s)
			}
		}
	}
	if len(shared) == 0 {
		return ""
	}
	sort.Strings(shared)
	word := "port"
	if len(shared) > 1 || strings.Contains(shared[0], "-") {
		word = "ports"
	}
	return word + " " + strings.Join(shared, ", ")
}

// NextBlock is the first block of BlockSize ports in the band that no project reserves or uses.
func NextBlock(projects []settings.Project) (engineroomfile.Range, error) {
	var taken []engineroomfile.Range
	for _, p := range projects {
		taken = append(taken, owned(p)...)
	}
	for lo := Band.Lo; lo+BlockSize-1 <= Band.Hi; lo += BlockSize {
		b := engineroomfile.Range{Lo: lo, Hi: lo + BlockSize - 1}
		free := true
		for _, t := range taken {
			if b.Overlaps(t) {
				free = false
				break
			}
		}
		if free {
			return b, nil
		}
	}
	return engineroomfile.Range{}, fmt.Errorf("no free block of %d ports is left in %s", BlockSize, Band)
}

// Plan is the slot table for a project given a block: SlotCount slots, each with the project's
// port names. Slot 1 keeps the ports the project has today, so the main clone's numbers (and
// anything registered against them, such as sign-in callbacks) never move; slot n from 2 takes
// the block's nth run of SlotSize ports, one per name in the order of today's numbers.
func Plan(p settings.Project, block engineroomfile.Range) ([]engineroomfile.Row, error) {
	if len(p.Ports) == 0 {
		return nil, fmt.Errorf("%s names no ports yet; add them under [ports] first, with the numbers the main clone uses", p.Name)
	}
	if len(p.Ports) > SlotSize {
		return nil, fmt.Errorf("%s has %d ports and a slot holds %d", p.Name, len(p.Ports), SlotSize)
	}
	rows := []engineroomfile.Row{{Slot: 1, Ports: p.Ports, Names: map[string]string{}}}
	for s := 2; s <= SlotCount; s++ {
		row := engineroomfile.Row{Slot: s, Names: map[string]string{}}
		for i, port := range p.Ports {
			row.Ports = append(row.Ports, engineroomfile.Port{Name: port.Name, Number: block.Lo + (s-1)*SlotSize + i})
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Add puts a reservation and its slot table into the text of a settings engineroom.toml. The reservation
// goes first, before any table, because a plain key written after a table header would belong to
// that table; the slot rows go last. Text that already reserves or has slots is refused, so a
// reservation is never written twice or over one the developer made.
func Add(existing string, block engineroomfile.Range, keep []engineroomfile.Range, rows []engineroomfile.Row, own engineroomfile.File) (string, error) {
	if own.Has["reserve"] || own.Has["slot"] {
		return "", fmt.Errorf("%s already has a reservation or slots; change them there by hand", own.Path)
	}
	res := []string{fmt.Sprintf("%q", block.String())}
	for _, k := range keep {
		res = append(res, fmt.Sprintf("%q", k.String()))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Reserved by engineroom ports reserve: a block of %d ports, %d slots of %d.\n", BlockSize, SlotCount, SlotSize)
	fmt.Fprintf(&b, "reserve = [%s]\n", strings.Join(res, ", "))
	if strings.TrimSpace(existing) != "" {
		b.WriteString("\n" + strings.TrimRight(existing, "\n") + "\n")
	}
	for _, r := range rows {
		parts := make([]string, len(r.Ports))
		for i, p := range r.Ports {
			parts[i] = fmt.Sprintf("%s = %d", p.Name, p.Number)
		}
		fmt.Fprintf(&b, "\n[[slot]]\nslot = %d\nports = { %s }\n", r.Slot, strings.Join(parts, ", "))
	}
	return b.String(), nil
}

// Owns reports whether port is one of project p's: in its reservation or one of its slots'.
func Owns(p settings.Project, port int) bool {
	return inAny(owned(p), port)
}
