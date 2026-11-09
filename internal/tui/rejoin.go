package tui

// Joining again after a dropped connection (CONN-REJOIN). A client joins an engine's session over
// SSH, and that connection ends whenever the client sleeps, wakes on another network or moves,
// while the session itself keeps running on the engine. A laptop is opened and closed often, and
// should keep its easy way back in. So a join that was attached and then lost its connection asks the engine, as often as it takes, and
// joins the same session again once the engine answers, in the same window.
//
// Only a lost connection is answered this way: ssh ends with its own failure status, 255, after
// the join had been attached. A join that never attached is a failed join and says why, as
// before. And only the same session is joined again: one that was stopped, or restarted (which
// makes a new session under the same name), ended the join, as SESS-BACK-004 has it.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// rejoinEvery is how long the window waits between asking the engine. Short, because a client
// waking from sleep wants its session back as soon as the network is; each ask is one ssh
// connection that gives up after the configured connect timeout.
const rejoinEvery = 3 * time.Second

// sameSessionSlack allows for the session list's ages being whole seconds and read a moment after
// the join began, when deciding whether the session is older than the join (the same session).
const sameSessionSlack = 5 * time.Second

// rejoinWait is the wait for the engine after a join's connection dropped.
type rejoinWait struct {
	session string
	// began is when the dropped join attached: a session younger than that was started anew
	// under the same name while the connection was down, and is not the one that was joined.
	began time.Time
	// checks counts the asks so far, read is the command each ask runs, and last is the latest
	// reason the engine did not answer, all shown so the waiting is never a blank screen.
	checks int
	read   string
	last   string
}

type rejoinMsg struct {
	list []ops.SessionView
	read string
	err  error
}

type rejoinTickMsg struct{}

// droppedJoin is the wait to start when msg is a join whose connection to the engine dropped: a
// join by name (m.joined) of one of the engine's sessions, over SSH, that had been attached
// (attachedFor) and ended with ssh's own failure status.
func (m Model) droppedJoin(msg handedBackMsg) (*rejoinWait, bool) {
	if m.joined == "" || m.local {
		return nil, false
	}
	if _, remote := m.src.(source.Remote); !remote {
		return nil, false
	}
	var ee *exec.ExitError
	if !errors.As(msg.err, &ee) || ee.ExitCode() != 255 || msg.lasted < attachedFor {
		return nil, false
	}
	return &rejoinWait{session: m.joined, began: time.Now().Add(-msg.lasted)}, true
}

// checkRejoin asks the engine for its sessions, which both tells whether it answers and whether the
// session is still the one that was joined.
func (m Model) checkRejoin() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		l, read, err := src.Sessions(context.Background())
		return rejoinMsg{list: l, read: read, err: err}
	}
}

// gotRejoin acts on the engine's answer while waiting: asks again later when it did not answer,
// joins the session again when it is the same one still running, and otherwise ends the wait as
// the session's end.
func (m Model) gotRejoin(msg rejoinMsg) (tea.Model, tea.Cmd) {
	w := m.rejoin
	if m.screen != screenRejoin || w == nil {
		// The developer stopped waiting while the engine was being asked.
		return m, nil
	}
	w.checks++
	w.read = msg.read
	if msg.err != nil {
		w.last = strings.TrimSpace(strings.SplitN(msg.err.Error(), "\n", 2)[0])
		return m, tea.Tick(rejoinEvery, func(time.Time) tea.Msg { return rejoinTickMsg{} })
	}
	m.rejoin = nil
	for _, s := range msg.list {
		if s.Name != w.session || s.Here {
			continue
		}
		if !s.Ended && sameSession(s.Created, w.began, time.Now()) {
			name, src := w.session, m.src
			m.screen = screenList
			m.message = "The engine answers again: joining " + name + " again."
			return m, m.makePlan(func(ctx context.Context) (action.Plan, error) {
				return src.OnSession(ctx, "join", name, ops.Start{})
			}, false)
		}
		break
	}
	m.joined = ""
	if m.quitAfter {
		return m, tea.Quit
	}
	m.screen = screenList
	m.message = w.session + " was stopped or started again while the connection to the engine was down, so it was not joined again."
	return m, m.loadSessions()
}

// rejoinKey is the waiting screen's keys: q or esc stops waiting and joins nothing
// (CONN-REJOIN-004); control-c ends the program, as everywhere.
func (m Model) rejoinKey(key string) (tea.Model, tea.Cmd) {
	if key != "q" && key != "esc" {
		return m, nil
	}
	m.rejoin, m.joined = nil, ""
	if m.quitAfter {
		return m, tea.Quit
	}
	m.screen = screenList
	m.message = ""
	return m, m.loadSessions()
}

func (m Model) rejoinView() string {
	w := m.rejoin
	if w == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The connection to the engine %s was lost while joined to %s: this machine slept, or its network changed.\n",
		m.cfg.Engine.Host, titleStyle.Render(w.session))
	b.WriteString("The session keeps running on the engine, and this window joins it again as soon as the engine answers.\n\n")
	if w.checks == 0 {
		b.WriteString(dimStyle.Render("Asking the engine ...") + "\n")
	} else {
		b.WriteString(dimStyle.Render(fmt.Sprintf("Asked %d times, every %s, with: %s", w.checks, rejoinEvery, w.read)) + "\n")
		if w.last != "" {
			b.WriteString(dimStyle.Render("It has not answered: "+w.last) + "\n")
		}
	}
	b.WriteString("\n" + keyStyle.Render("q") + " stop waiting\n")
	return b.String()
}

// ageRule reads one part of a session's age as Zellij writes it ("6days 17h 54m 50s ago").
var ageRule = regexp.MustCompile(`(\d+)\s*(days?|d|h|m|s)\b`)

// sessionAge reads a session's age from the session list; false when it cannot be read.
func sessionAge(created string) (time.Duration, bool) {
	parts := ageRule.FindAllStringSubmatch(created, -1)
	if len(parts) == 0 {
		return 0, false
	}
	var age time.Duration
	for _, p := range parts {
		n, err := strconv.Atoi(p[1])
		if err != nil {
			return 0, false
		}
		unit := map[string]time.Duration{"day": 24 * time.Hour, "days": 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second}[p[2]]
		age += time.Duration(n) * unit
	}
	return age, true
}

// sameSession reports whether a session of age created (as listed at now) is the one a join that
// attached at began was joined to: it was already running then. An age that cannot be read
// counts as the same session, since joining it again is what keeps the developer's access.
func sameSession(created string, began, now time.Time) bool {
	age, ok := sessionAge(created)
	if !ok {
		return true
	}
	return age+sameSessionSlack >= now.Sub(began)
}
