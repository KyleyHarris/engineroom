// Package signal records that a session is waiting for the developer, and what it asked.
//
// The AI agent reports it through `engineroom signal waiting` (from its notification hook) and clears
// it through `engineroom signal clear` when the developer answers. A signal can be missed, for
// example when the agent is stopped before it clears it, so a signal older than the configured
// limit is reported as unknown rather than believed (sessions obligation O6). Writing a signal
// never fails the agent: errors are reported on stderr and the command still exits 0.
package signal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/KyleyHarris/engineroom/internal/paths"
)

// Signal is one session's last report.
type Signal struct {
	Session string    `json:"session"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// State is what the session list shows about waiting.
type State string

const (
	// None means the session has not said it is waiting.
	None State = ""
	// Waiting means the session asked something and is waiting for the developer.
	Waiting State = "waiting"
	// Unknown means a waiting report exists but is too old to trust.
	Unknown State = "unknown"
)

var safeName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func dir() string { return filepath.Join(paths.StateDir(), "signals") }

func file(session string) (string, error) {
	if !safeName.MatchString(session) {
		return "", fmt.Errorf("session name %q is not a session this program names", session)
	}
	return filepath.Join(dir(), session+".json"), nil
}

// SetWaiting records that session is waiting, with the question it asked. The write is a
// temporary file renamed into place, so a reader never sees half a signal.
func SetWaiting(session, message string) error {
	path, err := file(session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(Signal{Session: session, Message: message, At: time.Now().UTC()})
	tmp, err := os.CreateTemp(dir(), ".signal-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Clear removes the session's waiting report. Clearing one that does not exist is not an error.
func Clear(session string) error {
	path, err := file(session)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Read returns the session's state and question, judging age against limit.
func Read(session string, limit time.Duration, now time.Time) (State, string) {
	path, err := file(session)
	if err != nil {
		return None, ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return None, ""
	}
	var s Signal
	if err := json.Unmarshal(b, &s); err != nil {
		// A damaged report is not trusted either way.
		return Unknown, ""
	}
	if now.Sub(s.At) > limit {
		return Unknown, s.Message
	}
	return Waiting, s.Message
}
