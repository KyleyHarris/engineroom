package handoff

import (
	"context"
	"encoding/json"
	"io"
	"sync"
)

// Step is how far a reading of one machine has got, so a screen waiting for a handover's plan can
// say what it is doing, with a meter, instead of showing nothing. Total is 0 until the machine has
// said how many
// projects it reads.
type Step struct {
	// Side is the machine being read, as the plan names it ("the engine", "this machine").
	Side    string `json:"side"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Project string `json:"project,omitempty"`
	// Command is the command doing the reading, shown with the meter: the program never hides
	// what it does.
	Command string `json:"command,omitempty"`
}

// Progress is told each Step as it happens. It may be called from several goroutines at once.
type Progress func(Step)

type progressKey struct{}

// WithProgress returns a context whose handover readings report each Step to fn.
func WithProgress(ctx context.Context, fn Progress) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// progressOf is the context's Progress, or one that does nothing.
func progressOf(ctx context.Context) Progress {
	if fn, ok := ctx.Value(progressKey{}).(Progress); ok && fn != nil {
		return fn
	}
	return func(Step) {}
}

// ProgressLines is a Progress that writes each Step to w as one JSON line, {"progress": step}, the
// form an engine's `handoff state` writes to its standard error for the client reading it, and
// the app reads from `engineroom api plan`. Lines from several goroutines never interleave.
func ProgressLines(w io.Writer) Progress {
	var mu sync.Mutex
	return func(s Step) {
		b, err := json.Marshal(map[string]Step{"progress": s})
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write(append(b, '\n'))
	}
}
