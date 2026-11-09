// Package forward keeps the engine's sessions' ports reachable on a client (docs/decisions.md
// D-013). One SSH connection to the engine is held open as a master; each port is a forward added to
// it or cancelled on it, so joining or leaving a session changes nothing, and a session used only
// from the Claude app still has its ports here.
//
// Every change is a command (ssh -O forward / -O cancel on the master), run as a plan the
// developer can read in the history, like any other action. ssh does not list a master's
// forwards, so the ones made are remembered in a small state file beside the history.
package forward

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// Want is one port to have forwarded, and the session it belongs to.
type Want struct {
	Session string
	Port    engineroomfile.Port
}

// Have is the forwards made, by port number, with the session each was made for.
type Have map[int]string

// Desired is the forwards there should be: every port of every running session on the engine that
// wanted says is to be forwarded (config.Forward.Wants). A port two sessions both name goes to the first by name and the
// other is said in a note; a port this machine cannot take is left out with what holds it, unless
// it is already one of ours (then it is ours that holds it).
func Desired(sessions []ops.SessionView, wanted func(string) bool, taken func(int) string, have Have) (map[int]Want, []string) {
	sorted := append([]ops.SessionView(nil), sessions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	want := map[int]Want{}
	var notes []string
	for _, s := range sorted {
		if s.Ended || !wanted(s.Name) || s.Here {
			continue
		}
		for _, p := range s.Ports {
			if other, dup := want[p.Number]; dup {
				notes = append(notes, fmt.Sprintf("%s's %s %d is not forwarded: %s already has that port. Give the projects different ports (engineroom ports check).", s.Name, p.Name, p.Number, other.Session))
				continue
			}
			if _, ours := have[p.Number]; !ours {
				if why := taken(p.Number); why != "" {
					notes = append(notes, fmt.Sprintf("%s's %s %d is not forwarded: the port is %s.", s.Name, p.Name, p.Number, why))
					continue
				}
			}
			want[p.Number] = Want{Session: s.Name, Port: p}
		}
	}
	return want, notes
}

// Changes is what to cancel and what to add to go from have to want, each in port order. A port
// that moves from one session to another is cancelled and added again, so the history says so.
func Changes(want map[int]Want, have Have) (add []Want, cancel []int) {
	for n, s := range have {
		if w, ok := want[n]; !ok || w.Session != s {
			cancel = append(cancel, n)
		}
	}
	for n, w := range want {
		if s, ok := have[n]; !ok || s != w.Session {
			add = append(add, w)
		}
	}
	sort.Ints(cancel)
	sort.Slice(add, func(i, j int) bool { return add[i].Port.Number < add[j].Port.Number })
	return add, cancel
}

// Master is the SSH connection forwards are made on.
type Master struct {
	R action.Remote
}

// Socket is the master's control socket. It is under /tmp with a short name on purpose: a socket's
// path has a length limit of about 104 bytes, which a path under a long home or temporary folder
// can pass. The name is per user and per engine, so two engineroom, or two people, never share one.
func (m Master) Socket() string {
	h := sha256.Sum256([]byte(m.R.Host + "\x00" + m.R.SSHConfig))
	return filepath.Join("/tmp", fmt.Sprintf("engineroom-%d-%s.sock", os.Getuid(), hex.EncodeToString(h[:])[:12]))
}

// Through is the engine as reached through this master's open connection: a command sent this way
// is a few packets on a connection already kept alive, rather than a new connection of its own,
// which is what makes checking the engine's sessions every minute cheap: forwarding follows
// running sessions without costing energy while nobody looks.
func (m Master) Through() action.Remote {
	r := m.R
	r.ControlPath = m.Socket()
	return r
}

func (m Master) base() []string {
	args := []string{"ssh"}
	if m.R.SSHConfig != "" {
		args = append(args, "-F", m.R.SSHConfig)
	}
	return append(args, "-o", "ControlPath="+m.Socket())
}

// Alive reports whether the master is running.
func (m Master) Alive() bool {
	args := append(m.base(), "-O", "check", m.R.Host)
	return exec.Command(args[0], args[1:]...).Run() == nil
}

// Start is the command that opens the master and leaves it running in the background with no
// forwards. BatchMode means it never stops to ask for a password: an engine that cannot be reached
// with the configured key fails here and the plan stops.
func (m Master) Start() action.Command {
	secs := int(m.R.ConnectTimeout.Round(time.Second) / time.Second)
	if secs < 1 {
		secs = 1
	}
	args := append(m.base(), "-o", "BatchMode=yes", "-o", "ConnectTimeout="+strconv.Itoa(secs),
		"-o", "ControlMaster=yes", "-o", "ControlPersist=yes", "-o", "ExitOnForwardFailure=no",
		// A connection the network dropped (the client asleep, or moved) ends within about 45
		// seconds instead of lingering dead; the next check then sees it gone and opens a new one.
		"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-N", "-f", m.R.Host)
	return action.Command{Argv: args, Why: "open one connection to the engine for the forwards, kept in the background",
		OnFail: "The engine did not answer, so no ports were forwarded. They are tried again at the next check."}
}

// Add is the command that forwards a port of the engine to the same port here.
func (m Master) Add(w Want) action.Command {
	n := strconv.Itoa(w.Port.Number)
	return action.Command{Argv: append(m.base(), "-O", "forward", "-L", n+":localhost:"+n, m.R.Host),
		Why: fmt.Sprintf("reach %s's %s on this machine's %s", w.Session, w.Port.Name, n)}
}

// Cancel is the command that stops forwarding a port.
func (m Master) Cancel(port int, session string) action.Command {
	n := strconv.Itoa(port)
	return action.Command{Argv: append(m.base(), "-O", "cancel", "-L", n+":localhost:"+n, m.R.Host),
		Why: fmt.Sprintf("stop forwarding %s, %s's port", n, session)}
}

// Stop is the command that closes the master and every forward on it.
func (m Master) Stop() action.Command {
	return action.Command{Argv: append(m.base(), "-O", "exit", m.R.Host), Why: "close the connection and every forward on it"}
}

// statePath is where the forwards made are remembered.
func statePath() string { return filepath.Join(paths.StateDir(), "forwards.json") }

// Load reads the forwards made. With no master running there are none, whatever the file says:
// they ended with the connection.
func Load(m Master) Have {
	have := Have{}
	if !m.Alive() {
		return have
	}
	b, err := os.ReadFile(statePath())
	if err != nil {
		return have
	}
	var raw map[string]string
	if json.Unmarshal(b, &raw) != nil {
		return have
	}
	for k, v := range raw {
		if n, err := strconv.Atoi(k); err == nil {
			have[n] = v
		}
	}
	return have
}

// Save remembers the forwards made.
func Save(have Have) error {
	raw := map[string]string{}
	for n, s := range have {
		raw[strconv.Itoa(n)] = s
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.MkdirAll(filepath.Dir(statePath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(statePath(), append(b, '\n'), 0o644)
}

// Last is what the forwarder's most recent pass found and did, kept so the app and `engineroom forward
// status` can say why a port is or is not reachable here: the forwarder runs in the background,
// where nobody reads its output.
type Last struct {
	At    time.Time `json:"at"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	// Notes are the ports not forwarded and why (taken here, skipped, two sessions on one port).
	Notes []string `json:"notes,omitempty"`
	// Forwarded is every port forwarded after the pass, with its session.
	Forwarded map[string]string `json:"forwarded"`
	// Sessions counts the engine's running sessions the pass read, and Ports the ports they named.
	Sessions int `json:"sessions"`
	Ports    int `json:"ports"`
}

func lastPath() string { return filepath.Join(paths.StateDir(), "forward-last.json") }

// SaveLast records a pass's outcome; a failure to record it is not the pass's failure.
func SaveLast(l Last) {
	b, _ := json.MarshalIndent(l, "", "  ")
	if os.MkdirAll(filepath.Dir(lastPath()), 0o755) == nil {
		_ = os.WriteFile(lastPath(), b, 0o644)
	}
}

// LoadLast reads the most recent pass's outcome, if there was one.
func LoadLast() (Last, bool) {
	var l Last
	b, err := os.ReadFile(lastPath())
	if err != nil || json.Unmarshal(b, &l) != nil {
		return Last{}, false
	}
	return l, true
}
