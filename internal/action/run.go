package action

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Status is what happened to one command.
type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusOK          Status = "ok"
	StatusFailed      Status = "failed"
	StatusNotRun      Status = "not run"
	StatusInterrupted Status = "interrupted"
	// StatusHandedOver marks the interactive command: the program hands the terminal to it,
	// so its result is the developer's session rather than captured output.
	StatusHandedOver Status = "handed over"
)

// Step is the live state of one command while a plan runs.
type Step struct {
	Index  int
	Status Status
	Exit   int
	Output string
}

// outputLimit bounds what is kept of a command's output in memory and in the history; the
// tail is what explains a failure, and an unbounded copy of a chatty command would grow the
// history without limit (learning obligation O9).
const outputLimit = 8 * 1024

// Recorder is told about each step as it changes. The history implements it; so can a test.
type Recorder interface {
	Step(Step)
}

// Run executes every non-interactive command of the plan in order and reports each step to
// onStep (which may be nil). It stops at the first failure, reporting that command's output
// and marking every later command not run, so a partial action is never reported as complete
// (learning obligation O2, LEARN-SHOW-003). Cancelling ctx stops the running command and
// marks it interrupted (O5).
//
// An interactive last command is not run here; it is reported as handed over when everything
// before it succeeded, and the caller gives it the terminal.
func Run(ctx context.Context, p Plan, onStep func(Step)) (ok bool) {
	emit := func(s Step) {
		if onStep != nil {
			onStep(s)
		}
	}
	for i, c := range p.Commands {
		if ctx.Err() != nil {
			emit(Step{Index: i, Status: StatusInterrupted})
			markRest(p, i+1, emit)
			return false
		}
		if c.Interactive {
			emit(Step{Index: i, Status: StatusHandedOver})
			return true
		}
		emit(Step{Index: i, Status: StatusRunning})
		idx := i
		exit, out, err := runOne(ctx, c, func(partial string) {
			// What a long command prints is shown while it runs, not only after.
			emit(Step{Index: idx, Status: StatusRunning, Output: partial})
		})
		switch {
		case ctx.Err() != nil:
			emit(Step{Index: i, Status: StatusInterrupted, Exit: exit, Output: out})
			markRest(p, i+1, emit)
			return false
		case err != nil || exit != 0:
			if err != nil && out == "" {
				out = err.Error()
			}
			emit(Step{Index: i, Status: StatusFailed, Exit: exit, Output: out})
			markRest(p, i+1, emit)
			return false
		default:
			emit(Step{Index: i, Status: StatusOK, Exit: 0, Output: out})
		}
	}
	return true
}

func markRest(p Plan, from int, emit func(Step)) {
	for j := from; j < len(p.Commands); j++ {
		emit(Step{Index: j, Status: StatusNotRun})
	}
}

// runOne runs a command directly (never through a shell, so what is shown is what runs) and
// returns its exit code and the tail of its combined output.
func runOne(ctx context.Context, c Command, onOutput func(string)) (int, string, error) {
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	buf := tailBuffer{notify: onOutput}
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	// A command that leaves something running in the background with its output still open (an up
	// script starting a server without redirecting it) would otherwise hold the step open for as long
	// as that runs; once the command itself has exited, its output is waited for this long, then the
	// step ends with what it said.
	cmd.WaitDelay = outputGrace
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	out := strings.TrimRight(buf.String(), "\n")
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), out, nil
	}
	if err != nil {
		return -1, out, err
	}
	return 0, out, nil
}

// outputGrace is how long a finished command's output is waited for (runOne).
var outputGrace = 5 * time.Second

// tailBuffer keeps only the last outputLimit bytes written to it.
type tailBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	cut    bool
	notify func(string)
	last   time.Time
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > outputLimit {
		b := t.buf.Bytes()
		keep := append([]byte(nil), b[len(b)-outputLimit:]...)
		t.buf.Reset()
		t.buf.Write(keep)
		t.cut = true
	}
	// Report progress at most ten times a second, so a chatty command cannot flood the screen.
	if t.notify != nil && time.Since(t.last) > 100*time.Millisecond {
		t.last = time.Now()
		t.notify(strings.TrimRight(t.buf.String(), "\n"))
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cut {
		return "[earlier output cut]\n" + t.buf.String()
	}
	return t.buf.String()
}
