package tui

import (
	"strings"
	"testing"
)

// A frame's markers are removed and each records the row it was on, counted from the first row
// Bubble Tea shows when the frame is taller than the screen. Would catch a click mapped to the
// wrong row (off by the rows scrolled away) or a marker left visible on screen.
func TestLayoutStripsMarkersAndRecordsRows(t *testing.T) {
	frame := "title\n" + mark(hitSession, 0) + "> one\n" + mark(hitSession, 1) + "  two\n" + mark(hitCommand, 3) + "  cmd " + mark(hitCopyAll, 0) + "all"
	l := &layout{}
	shown := l.strip(frame, 0)
	if strings.ContainsAny(shown, string([]rune{markerStart, markerEnd})) || shown != "title\n> one\n  two\n  cmd all" {
		t.Fatalf("shown %q", shown)
	}
	if h, ok := l.at(2); !ok || h.kind != hitSession || h.index != 1 {
		t.Fatalf("row 2: %+v %v", h, ok)
	}
	l.strip(frame, 2) // only the last two rows fit
	if h, ok := l.at(0); !ok || h.index != 1 || h.kind != hitSession {
		t.Fatalf("after scrolling, row 0: %+v %v", h, ok)
	}
	if _, ok := l.at(5); ok {
		t.Fatal("a row past the frame is clickable")
	}
}

// LEARN-COPY-002: all commands paste as one chain that stops at the first failure, one command
// per line. Would catch a join that keeps going after a failure (";" or bare newlines).
func TestJoinAllStopsAtTheFirstFailure(t *testing.T) {
	if got := JoinAll([]string{"a", "b c", "d"}); got != "a &&\nb c &&\nd\n" {
		t.Fatalf("%q", got)
	}
}

// A command wrapped over several rows is clickable on every row, and its marker takes no width,
// so the continuation is indented under the command as before.
func TestWrappedMarksEveryRow(t *testing.T) {
	out := wrapped(mark(hitCommand, 2)+"> ", strings.Repeat("x", 50), 30)
	l := &layout{}
	shown := l.strip(out, 0)
	rows := strings.Split(strings.TrimRight(shown, "\n"), "\n")
	if len(rows) < 2 || !strings.HasPrefix(rows[1], "  x") {
		t.Fatalf("rows %q", rows)
	}
	for y := range rows {
		if h, ok := l.at(y); !ok || h.index != 2 {
			t.Fatalf("row %d is not the command: %+v %v", y, h, ok)
		}
	}
}
