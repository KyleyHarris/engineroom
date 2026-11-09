// Package history keeps every action the program has run, with its commands and their
// outcomes, so a developer can find what was done and run it again by hand.
//
// The history is one append-only file of JSON lines in the program's state directory. An
// action is written twice: once before its first command runs (so a crash still leaves a
// record, learning obligation O1) and again when it ends. Readers keep the last line for each
// action. Appends hold an exclusive file lock, so two copies of the program writing at once
// never interleave a line (O4), and a line that cannot be parsed is skipped and counted rather
// than stopping the whole history from being read (O8).
package history

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// CommandRecord is one command of a recorded action.
type CommandRecord struct {
	Display string        `json:"display"`
	Why     string        `json:"why,omitempty"`
	Status  action.Status `json:"status"`
	Exit    int           `json:"exit"`
	Output  string        `json:"output,omitempty"`
}

// Entry is one action.
type Entry struct {
	ID      string    `json:"id"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	Machine string    `json:"machine"`
	Title   string    `json:"title"`
	Status  string    `json:"status"`
	// Notes are what the plan said before its commands: what it left alone and why, and what a
	// handover carried and set aside, so the record says afterwards what the action did beyond
	// its commands.
	Notes    []string        `json:"notes,omitempty"`
	Commands []CommandRecord `json:"commands"`
}

// Status values of a whole action.
const (
	Running     = "running"
	Done        = "done"
	Failed      = "failed"
	Interrupted = "interrupted"
)

// File is where the history lives.
func File() string {
	return filepath.Join(paths.StateDir(), "history.jsonl")
}

// Begin records an action before it runs and returns its entry. A failure to write is
// returned for the caller to report once; it never stops the action (O6).
func Begin(p action.Plan) (*Entry, error) {
	host, _ := os.Hostname()
	e := &Entry{
		ID:      newID(),
		Started: time.Now().UTC(),
		Machine: host,
		Title:   p.Title,
		Status:  Running,
		Notes:   p.Notes,
	}
	for _, c := range p.Commands {
		e.Commands = append(e.Commands, CommandRecord{Display: c.Display(), Why: c.Why, Status: action.StatusPending})
	}
	return e, appendEntry(e)
}

// Update applies one step to the entry in memory.
func (e *Entry) Update(s action.Step) {
	if s.Index < 0 || s.Index >= len(e.Commands) {
		return
	}
	r := &e.Commands[s.Index]
	r.Status = s.Status
	r.Exit = s.Exit
	if s.Output != "" {
		r.Output = s.Output
	}
}

// Finish records the action's outcome.
func (e *Entry) Finish(status string) error {
	e.Status = status
	e.Ended = time.Now().UTC()
	return appendEntry(e)
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func appendEntry(e *Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(File()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(File(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	// One write under an exclusive lock: O_APPEND alone does not guarantee a large line lands
	// in one piece when another process appends at the same moment.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	_, err = f.Write(line)
	return err
}

// Read returns every action, most recent first, and how many lines could not be read.
func Read() ([]Entry, int, error) {
	f, err := os.Open(File())
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	latest := map[string]Entry{}
	bad := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.ID == "" {
			bad++
			continue
		}
		latest[e.ID] = e
	}
	if err := sc.Err(); err != nil {
		bad++
	}
	out := make([]Entry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out, bad, nil
}

// Find returns the action with this id, or with an id starting with it.
func Find(id string) (Entry, bool, error) {
	all, _, err := Read()
	if err != nil {
		return Entry{}, false, err
	}
	for _, e := range all {
		if e.ID == id || (len(id) >= 6 && len(e.ID) >= len(id) && e.ID[:len(id)] == id) {
			return e, true, nil
		}
	}
	return Entry{}, false, nil
}
