package tui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// exitWith is a real exit error with status code, as a command handed the terminal returns.
func exitWith(t *testing.T, code string) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+code).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("no exit error for %s: %v", code, err)
	}
	return err
}

func joinedModel() Model {
	cfg := &config.Config{Role: config.RoleClient}
	cfg.Engine.Host = "engine"
	return Model{cfg: cfg, src: source.Remote{}, joined: "project-a", quitAfter: true}
}

// Ages are read as Zellij writes them. Fails if a unit is misread or an unreadable age is taken
// for a reading.
func TestSessionAge(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"3s ago":                3 * time.Second,
		"1m 2s ago":             time.Minute + 2*time.Second,
		"17h 55m 29s ago":       17*time.Hour + 55*time.Minute + 29*time.Second,
		"6days 17h 54m 50s ago": 6*24*time.Hour + 17*time.Hour + 54*time.Minute + 50*time.Second,
		"1day 2s ago":           24*time.Hour + 2*time.Second,
	} {
		got, ok := sessionAge(in)
		if !ok || got != want {
			t.Errorf("%q read as %v (%v), want %v", in, got, ok, want)
		}
	}
	if _, ok := sessionAge("just now"); ok {
		t.Error("an age with no number was read")
	}
}

// The session joined is the one running since before the join attached; one started after it
// (a restart under the same name) is not. Fails if a restarted session counts as the same, or
// the same session as a new one.
func TestSameSession(t *testing.T) {
	now := time.Now()
	began := now.Add(-10 * time.Minute)
	if !sameSession("2h 0m 0s ago", began, now) {
		t.Error("a session older than the join was not the same")
	}
	if !sameSession("10m 0s ago", began, now) {
		t.Error("a session started as the join attached was not the same")
	}
	if sameSession("30s ago", began, now) {
		t.Error("a session started after the join attached counted as the same")
	}
	if !sameSession("unreadable", began, now) {
		t.Error("an unreadable age did not count as the same, which would end the developer's access")
	}
}

// Only a join by name of an engine session, over SSH, attached and then ended with ssh's own failure
// status, is a dropped connection. Fails if any other ending starts the wait, or that one does not.
func TestDroppedJoin(t *testing.T) {
	m := joinedModel()
	drop := handedBackMsg{err: exitWith(t, "255"), command: "ssh -t engine -- zellij attach project-a", said: "Connection to engine closed by remote host.", lasted: attachedFor}
	w, ok := m.droppedJoin(drop)
	if !ok || w.session != "project-a" || time.Since(w.began) < attachedFor {
		t.Fatalf("a dropped connection did not start the wait: %+v %v", w, ok)
	}
	cases := map[string]func() (Model, handedBackMsg){
		"never attached": func() (Model, handedBackMsg) { d := drop; d.lasted = time.Second; return m, d },
		"session's end":  func() (Model, handedBackMsg) { d := drop; d.err = exitWith(t, "1"); return m, d },
		"left":           func() (Model, handedBackMsg) { d := drop; d.err = nil; return m, d },
		"not by name":    func() (Model, handedBackMsg) { n := m; n.joined = ""; return n, drop },
		"working local":  func() (Model, handedBackMsg) { n := m; n.local = true; return n, drop },
		"on the engine":  func() (Model, handedBackMsg) { n := m; n.src = source.Local{}; return n, drop },
	}
	for name, c := range cases {
		mm, msg := c()
		if _, ok := mm.droppedJoin(msg); ok {
			t.Errorf("%s started the wait", name)
		}
	}
}

// A dropped join waits on its own screen and asks the engine (CONN-REJOIN-002). Fails if it quits,
// as a session's end did before, or shows nothing of the wait.
func TestADroppedJoinWaits(t *testing.T) {
	next, cmd := joinedModel().Update(handedBackMsg{err: exitWith(t, "255"), command: "ssh -t engine -- zellij attach project-a", lasted: attachedFor})
	m := next.(Model)
	if m.screen != screenRejoin || m.rejoin == nil || cmd == nil || isQuit(cmd) {
		t.Fatalf("screen %v, wait %+v, quit %v", m.screen, m.rejoin, isQuit(cmd))
	}
	if v := m.rejoinView(); !strings.Contains(v, "joins it again as soon as the engine answers") || !strings.Contains(v, "stop waiting") {
		t.Fatalf("the wait does not say what it does:\n%s", v)
	}
}

// The engine's answer decides: not answering asks again later; the same session running joins it
// again; a session stopped, gone or started anew ends the wait as the session's end, joining
// nothing (CONN-REJOIN-001, 002, 003). Fails if any answer is acted on wrongly.
func TestTheEnginesAnswerDecides(t *testing.T) {
	waiting := func() Model {
		m := joinedModel()
		m.screen = screenRejoin
		m.rejoin = &rejoinWait{session: "project-a", began: time.Now().Add(-time.Hour)}
		return m
	}

	next, cmd := waiting().Update(rejoinMsg{err: errors.New("the engine did not answer: ssh: connect to host engine port 22: Operation timed out"), read: "ssh engine engineroom sessions --json"})
	m := next.(Model)
	if m.screen != screenRejoin || m.rejoin.checks != 1 || cmd == nil || isQuit(cmd) || !strings.Contains(m.rejoinView(), "Operation timed out") {
		t.Fatalf("an engine that did not answer: screen %v, wait %+v, quit %v", m.screen, m.rejoin, isQuit(cmd))
	}

	next, cmd = waiting().Update(rejoinMsg{list: []ops.SessionView{{Name: "project-a", Created: "3h 0m 0s ago"}}})
	m = next.(Model)
	if m.rejoin != nil || m.joined != "project-a" || cmd == nil || isQuit(cmd) || !strings.Contains(m.message, "joining project-a again") {
		t.Fatalf("the same session running was not joined again: %q quit %v", m.message, isQuit(cmd))
	}

	for name, list := range map[string][]ops.SessionView{
		"stopped":       nil,
		"ended":         {{Name: "project-a", Ended: true, Created: "3h 0m 0s ago"}},
		"started anew":  {{Name: "project-a", Created: "5s ago"}},
		"only here":     {{Name: "project-a", Here: true, Created: "3h 0m 0s ago"}},
		"another named": {{Name: "project-b", Created: "3h 0m 0s ago"}},
	} {
		next, cmd = waiting().Update(rejoinMsg{list: list})
		if !isQuit(cmd) || next.(Model).joined != "" {
			t.Errorf("%s: the wait did not end as the session's end", name)
		}
	}

	// From the list rather than a window opened to join, the wait ends back on the list, saying
	// why the session was not joined again.
	w := waiting()
	w.quitAfter = false
	next, cmd = w.Update(rejoinMsg{list: nil})
	if m = next.(Model); m.screen != screenList || isQuit(cmd) || !strings.Contains(m.message, "was not joined again") {
		t.Fatalf("from the list: screen %v, message %q", m.screen, m.message)
	}
}

// q stops waiting and joins nothing (CONN-REJOIN-004); an answer arriving after it is ignored.
// Fails if waiting carries on, or a late answer joins the session.
func TestStopWaiting(t *testing.T) {
	m := joinedModel()
	m.screen = screenRejoin
	m.rejoin = &rejoinWait{session: "project-a", began: time.Now().Add(-time.Hour)}
	next, cmd := m.rejoinKey("q")
	if !isQuit(cmd) || next.(Model).rejoin != nil {
		t.Fatal("q did not stop the wait")
	}
	late := next.(Model)
	late.screen = screenList
	if _, cmd := late.Update(rejoinMsg{list: []ops.SessionView{{Name: "project-a", Created: "3h 0m 0s ago"}}}); cmd != nil {
		t.Fatal("an answer after stopping joined the session")
	}
}
