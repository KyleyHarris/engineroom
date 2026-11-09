package tui

import (
	"strings"
	"sync"
)

// tailMax is how much of a handed-over command's standard error is kept: enough for the few lines
// that say why it failed.
const tailMax = 4096

// tailWriter keeps the end of what a command writes, so its last words can be said after the
// terminal has been redrawn over them.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = t.buf[len(t.buf)-tailMax:]
	}
	return len(p), nil
}

// Said is the command's last words on one line: its last three lines with text, leaving out a
// Rust panic's header and its backtrace hint, which say where the program stopped rather than
// why.
func (t *tailWriter) Said() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(string(t.buf), "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "note: run with `RUST_BACKTRACE") || (strings.HasPrefix(l, "thread '") && strings.Contains(l, "panicked at")) {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " ")
}
