package forward

import (
	"reflect"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/ops"
)

func sess(name string, ended bool, ports ...int) ops.SessionView {
	s := ops.SessionView{Name: name, Ended: ended}
	for i, p := range ports {
		s.Ports = append(s.Ports, engineroomfile.Port{Name: "p" + string(rune('a'+i)), Number: p})
	}
	return s
}

// Running sessions' ports are wanted; an ended session, a skipped one and a port this machine
// cannot take are not, and the last two are said; a port two sessions both name goes to one and
// the other is said. A port that is already ours is kept even though it now reads as taken (we hold
// it). It fails if an ended or skipped session were forwarded, a clash were silently dropped, or our
// own forward were cancelled for being "taken".
func TestDesired(t *testing.T) {
	sessions := []ops.SessionView{
		sess("b-work", false, 20010, 20011),
		sess("a-main", false, 4100, 20010),
		sess("c-ended", true, 30000),
		sess("d-skipped", false, 30001),
		sess("e-busy", false, 30002),
		sess("f-ours", false, 30003),
	}
	taken := func(p int) string {
		if p == 30002 || p == 30003 {
			return "already in use on this machine by node (pid 1)"
		}
		return ""
	}
	want, notes := Desired(sessions, func(s string) bool { return s != "d-skipped" }, taken, Have{30003: "f-ours"})
	var got []int
	for n := range want {
		got = append(got, n)
	}
	if len(got) != 4 || want[20010].Session != "a-main" || want[20011].Session != "b-work" || want[4100].Session != "a-main" || want[30003].Session != "f-ours" {
		t.Fatalf("want %+v", want)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "b-work's pa 20010 is not forwarded: a-main already has that port") || !strings.Contains(joined, "e-busy's pa 30002 is not forwarded: the port is already in use") || len(notes) != 2 {
		t.Fatalf("notes %q", notes)
	}
}

// Only the difference is changed: a port no longer wanted is cancelled, a new one added, one that
// moved to another session cancelled and added again, and one unchanged left alone. It fails if a
// forward that is already right is made again (a needless command in the history) or a stale one
// is kept.
func TestChanges(t *testing.T) {
	want := map[int]Want{
		4100:  {Session: "a", Port: engineroomfile.Port{Name: "ui", Number: 4100}},
		20010: {Session: "b", Port: engineroomfile.Port{Name: "ui", Number: 20010}},
		20020: {Session: "c", Port: engineroomfile.Port{Name: "ui", Number: 20020}},
	}
	add, cancel := Changes(want, Have{4100: "a", 20010: "old", 4200: "a"})
	if !reflect.DeepEqual(cancel, []int{4200, 20010}) {
		t.Fatalf("cancel %v", cancel)
	}
	if len(add) != 2 || add[0].Port.Number != 20010 || add[1].Port.Number != 20020 {
		t.Fatalf("add %+v", add)
	}
}

// The commands are the ones ssh takes, on the master's socket, the same port on both sides, and
// the socket path is short enough for the operating system's limit whatever the home folder.
func TestCommands(t *testing.T) {
	m := Master{R: action.Remote{Host: "engine", SSHConfig: "/very/long/" + strings.Repeat("x", 120) + "/config"}}
	if s := m.Socket(); len(s) > 60 || !strings.HasPrefix(s, "/tmp/engineroom-") {
		t.Fatalf("socket %q", s)
	}
	add := strings.Join(m.Add(Want{Session: "a", Port: engineroomfile.Port{Name: "ui", Number: 4100}}).Argv, " ")
	if !strings.Contains(add, "-O forward -L 4100:localhost:4100 engine") || !strings.Contains(add, "ControlPath="+m.Socket()) {
		t.Fatalf("add %s", add)
	}
	if c := strings.Join(m.Cancel(4100, "a").Argv, " "); !strings.Contains(c, "-O cancel -L 4100:localhost:4100 engine") {
		t.Fatalf("cancel %s", c)
	}
	if s := strings.Join(m.Start().Argv, " "); !strings.Contains(s, "ControlMaster=yes") || !strings.Contains(s, "BatchMode=yes") || !strings.HasSuffix(s, "-N -f engine") {
		t.Fatalf("start %s", s)
	}
}
