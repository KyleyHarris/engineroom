// Package termtest drives a real program in a real pseudo-terminal and reads its screen the way
// a person would, for the journeys in journeys/ (docs/decisions.md D-003).
//
// The program is the built binary, unmodified. Keys are written to the terminal as a person
// types them, the output is interpreted by a terminal emulator, and assertions are made on the
// text that emulator shows. Every wait has a deadline and polls the screen; none is a fixed
// sleep, so a journey is as fast as the program and fails with the screen it was looking at.
//
// The same output stream is recorded as an asciicast, with each clause as a caption, so the run
// that passes or fails is the run that is filmed.
package termtest

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Options sets the terminal's size and how long a wait may take.
type Options struct {
	Cols, Rows int
	// Timeout bounds every WaitFor and WaitExit. A journey that needs longer for one step
	// passes its own deadline to WaitForWithin.
	Timeout time.Duration
	// Record, when set, is where the asciicast is written (plus a ".captions.cast" beside it).
	Record string
	// Title is written into the recording's header.
	Title string
}

// Terminal is one program running in a pseudo-terminal.
type Terminal struct {
	t    testing.TB
	opts Options
	cmd  *exec.Cmd
	pty  *os.File

	mu     sync.Mutex
	vt     vt10x.Terminal
	raw    strings.Builder
	exited chan struct{}
	exit   int

	rec *recorder
}

// Start runs cmd in a pseudo-terminal of the given size. The process is stopped, with its whole
// process group, when the test ends, whether it passed or not.
func Start(t testing.TB, cmd *exec.Cmd, opts Options) *Terminal {
	t.Helper()
	if opts.Cols == 0 {
		opts.Cols = 120
	}
	if opts.Rows == 0 {
		opts.Rows = 36
	}
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	// A real terminal type, so programs draw as they would for a person. TERM from the
	// developer's own shell is replaced because it varies between machines.
	cmd.Env = append(withoutKey(cmd.Env, "TERM"), "TERM=xterm-256color")

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(opts.Cols), Rows: uint16(opts.Rows)})
	if err != nil {
		t.Fatalf("termtest: start %v: %v", cmd.Args, err)
	}
	term := &Terminal{
		t:      t,
		opts:   opts,
		cmd:    cmd,
		pty:    f,
		exited: make(chan struct{}),
	}
	// The emulator answers terminal queries (cursor position, device attributes) through the
	// pty, as a real terminal does. Programs that wait for those answers hang without it.
	term.vt = vt10x.New(vt10x.WithSize(opts.Cols, opts.Rows), vt10x.WithWriter(f))
	if opts.Record != "" {
		term.rec, err = newRecorder(opts.Record, opts.Title, opts.Cols, opts.Rows)
		if err != nil {
			t.Fatalf("termtest: recording: %v", err)
		}
	}

	go term.read()
	go func() {
		err := cmd.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			code = -1
		}
		term.mu.Lock()
		term.exit = code
		term.mu.Unlock()
		close(term.exited)
	}()
	t.Cleanup(term.stop)
	return term
}

func withoutKey(env []string, key string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// read copies everything the program writes into the emulator and the recording. It ends when
// the pty closes, which happens when the program and its children have exited.
func (term *Terminal) read() {
	buf := make([]byte, 32*1024)
	for {
		n, err := term.pty.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			term.mu.Lock()
			_, _ = term.vt.Write(chunk)
			term.raw.Write(chunk)
			term.mu.Unlock()
			if term.rec != nil {
				term.rec.output(chunk)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				// EIO is how macOS and Linux report the slave side closing; anything else is
				// worth seeing when a journey fails.
				if !strings.Contains(err.Error(), "input/output error") {
					term.t.Logf("termtest: read: %v", err)
				}
			}
			return
		}
	}
}

// Close is the terminal window closing: its program and everything in its process group are
// told to end, as a window's closing tells them, and the terminal goes. Safe to call twice.
func (term *Terminal) Close() { term.stop() }

// stop ends the program and its process group, then closes the recording. It is safe to call
// after the program has exited by itself.
func (term *Terminal) stop() {
	select {
	case <-term.exited:
	default:
		if term.cmd.Process != nil {
			// The program was started as a session leader, so its pid is its group id. Signal
			// the group so child processes it started do not outlive the test.
			_ = syscall.Kill(-term.cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-term.exited:
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-term.cmd.Process.Pid, syscall.SIGKILL)
				<-term.exited
			}
		}
	}
	_ = term.pty.Close()
	if term.rec != nil {
		if err := term.rec.close(); err != nil {
			term.t.Logf("termtest: closing recording: %v", err)
		}
	}
	if term.t.Failed() {
		term.t.Logf("termtest: last screen of %v:\n%s", term.cmd.Args, term.Screen())
	}
}

// Screen is the visible text, each row with trailing blanks removed, rows joined by newlines.
func (term *Terminal) Screen() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	term.vt.Lock()
	defer term.vt.Unlock()
	cols, rows := term.vt.Size()
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < cols; x++ {
			c := term.vt.Cell(x, y).Char
			if c == 0 {
				c = ' '
			}
			b.WriteRune(c)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// Output is everything the program has written so far, escape sequences included. Useful for a
// program that prints and exits, where the screen may already have scrolled.
func (term *Terminal) Output() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.raw.String()
}

// onScreen reports whether text is on the screen, also when the screen has broken it across
// rows: a terminal breaks a long line at its width, mid-word, and the program's own wrapping
// breaks at a space. Without this, a phrase that holds a temporary path passes or fails with the
// length of the path, which differs from machine to machine.
func onScreen(screen, text string) bool {
	return strings.Contains(screen, text) ||
		strings.Contains(strings.ReplaceAll(screen, "\n", ""), text) ||
		strings.Contains(strings.ReplaceAll(screen, "\n", " "), text)
}

// WaitFor waits until text is on the screen, failing the test with the screen if the deadline
// passes first.
func (term *Terminal) WaitFor(text string) {
	term.t.Helper()
	term.WaitForWithin(text, term.opts.Timeout)
}

// WaitForWithin is WaitFor with its own deadline.
func (term *Terminal) WaitForWithin(text string, d time.Duration) {
	term.t.Helper()
	if !term.poll(d, func(s string) bool { return onScreen(s, text) }) {
		term.t.Fatalf("termtest: %q did not appear within %v. Screen:\n%s", text, d, term.Screen())
	}
}

// WaitUntil waits until ok holds for the screen, failing with what was awaited and the screen
// if the deadline passes first.
func (term *Terminal) WaitUntil(what string, ok func(screen string) bool) {
	term.t.Helper()
	if !term.poll(term.opts.Timeout, ok) {
		term.t.Fatalf("termtest: %s did not happen within %v. Screen:\n%s", what, term.opts.Timeout, term.Screen())
	}
}

// WaitGone waits until text is no longer on the screen.
func (term *Terminal) WaitGone(text string) {
	term.t.Helper()
	if !term.poll(term.opts.Timeout, func(s string) bool { return !onScreen(s, text) }) {
		term.t.Fatalf("termtest: %q was still on screen after %v. Screen:\n%s", text, term.opts.Timeout, term.Screen())
	}
}

// poll checks the screen until ok returns true or the deadline passes. The interval is short so
// a journey waits only as long as the program takes; it is a deadline, never a fixed pause.
func (term *Terminal) poll(d time.Duration, ok func(string) bool) bool {
	deadline := time.Now().Add(d)
	for {
		if ok(term.Screen()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-term.exited:
			// One last look: the final frame may have arrived just before the exit.
			time.Sleep(50 * time.Millisecond)
			return ok(term.Screen())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// WaitExit waits for the program to exit and returns its exit code.
func (term *Terminal) WaitExit() int {
	term.t.Helper()
	select {
	case <-term.exited:
	case <-time.After(term.opts.Timeout):
		term.t.Fatalf("termtest: %v did not exit within %v. Screen:\n%s", term.cmd.Args, term.opts.Timeout, term.Screen())
	}
	// Drain what the program wrote just before exiting.
	time.Sleep(50 * time.Millisecond)
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.exit
}

// Running reports whether the program is still running.
func (term *Terminal) Running() bool {
	select {
	case <-term.exited:
		return false
	default:
		return true
	}
}

// Type writes text as typed keys.
func (term *Terminal) Type(text string) {
	term.t.Helper()
	if _, err := term.pty.WriteString(text); err != nil {
		term.t.Fatalf("termtest: type %q: %v", text, err)
	}
}

// Key is a named key sequence.
type Key string

// Keys a person presses, as the bytes a terminal sends for them.
const (
	Enter  Key = "\r"
	Esc    Key = "\x1b"
	Tab    Key = "\t"
	CtrlC  Key = "\x03"
	CtrlD  Key = "\x04"
	Up     Key = "\x1b[A"
	Down   Key = "\x1b[B"
	Right  Key = "\x1b[C"
	Left   Key = "\x1b[D"
	Backsp Key = "\x7f"
)

// Press sends each key in turn.
func (term *Terminal) Press(keys ...Key) {
	term.t.Helper()
	for _, k := range keys {
		term.Type(string(k))
	}
}

// Click presses and releases the left mouse button on the screen cell at col, row (both from 0),
// as a terminal reports it to a program that asked for the mouse (SGR mouse reporting).
func (term *Terminal) Click(col, row int) {
	term.t.Helper()
	term.Type(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", col+1, row+1, col+1, row+1))
}

// Wheel turns the scroll wheel once over the cell at col, row: down when down is true.
func (term *Terminal) Wheel(down bool, col, row int) {
	term.t.Helper()
	button := 64
	if down {
		button = 65
	}
	term.Type(fmt.Sprintf("\x1b[<%d;%d;%dM", button, col+1, row+1))
}

// RowOf is the screen row (from 0) of the first line holding text, or -1.
func (term *Terminal) RowOf(text string) int {
	for i, line := range strings.Split(term.Screen(), "\n") {
		if strings.Contains(line, text) {
			return i
		}
	}
	return -1
}

// Caption marks the recording with the text a viewer should read at this point: the scenario,
// then each clause as it starts. It implements scenarios.Captioner.
func (term *Terminal) Caption(text string) {
	if term.rec != nil {
		term.rec.caption(text)
	}
}

// String describes the terminal for failure messages.
func (term *Terminal) String() string {
	return fmt.Sprintf("terminal(%v)", term.cmd.Args)
}
