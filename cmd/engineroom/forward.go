package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/forward"
	"github.com/KyleyHarris/engineroom/internal/netport"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// runForward is `engineroom forward ...` on a client: the engine's sessions' ports reachable here, under
// the same numbers, while each session runs (D-013).
//
//	engineroom forward                 bring the forwards in line with the engine's sessions, once
//	engineroom forward --check         the same, only over the connection already open; with none, nothing
//	engineroom forward --watch [secs]  follow the engine's sessions over the open connection, forwarding
//	                             as they start and stop; with none, look again every secs (30)
//	engineroom forward on|off          the remembered switch; off also closes every forward
//	engineroom forward skip|unskip <s> leave one session's ports out, or put them back
//	engineroom forward status          what is forwarded now
func runForward(args []string, stdout, stderr io.Writer) int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if cfg.Role != config.RoleClient {
		fmt.Fprintln(stderr, "engineroom: only a client forwards the engine's ports; on the engine they are already here")
		return 1
	}
	m := forward.Master{R: action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "on", "off":
		cfg.Forward.Enabled = sub == "on"
		if sub == "off" {
			// Off is every forward closed, the chosen ones too.
			cfg.Forward.Sessions = nil
		}
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		if sub == "off" {
			return stopForwards(m, stdout, stderr)
		}
		fmt.Fprintln(stdout, "Forwarding is on: the ports of the engine's running sessions are reached here while they run.")
		return syncForwards(cfg, m, stdout, stderr)
	case "add", "remove":
		// One session's switch: its ports forwarded here or not, whichever way forwarding is set.
		if len(args) != 2 {
			fmt.Fprintf(stderr, "usage: engineroom forward %s <session>\n", sub)
			return 2
		}
		setSessionForward(&cfg.Forward, args[1], sub == "add")
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return syncForwards(cfg, m, stdout, stderr)
	case "skip", "unskip":
		if len(args) != 2 {
			fmt.Fprintf(stderr, "usage: engineroom forward %s <session>\n", sub)
			return 2
		}
		var kept []string
		for _, s := range cfg.Forward.Skip {
			if s != args[1] {
				kept = append(kept, s)
			}
		}
		if sub == "skip" {
			kept = append(kept, args[1])
		}
		sort.Strings(kept)
		cfg.Forward.Skip = kept
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return syncForwards(cfg, m, stdout, stderr)
	case "status":
		if len(args) == 2 && args[1] == "--json" {
			l, _ := forward.LoadLast()
			return writeJSON(stdout, map[string]any{"enabled": cfg.Forward.Enabled, "connected": m.Alive(), "forwarded": forwardedOf(forward.Load(m)), "last": l, "skip": cfg.Forward.Skip, "sessions": cfg.Forward.Sessions})
		}
		have := forward.Load(m)
		if len(have) == 0 {
			fmt.Fprintln(stdout, "Nothing is forwarded.")
		}
		ports := make([]int, 0, len(have))
		for n := range have {
			ports = append(ports, n)
		}
		sort.Ints(ports)
		for _, n := range ports {
			fmt.Fprintf(stdout, "%d  %s\n", n, have[n])
		}
		if !cfg.Forward.Enabled {
			fmt.Fprintln(stdout, "Forwarding is off (engineroom forward on).")
		}
		if l, ok := forward.LoadLast(); ok {
			fmt.Fprintf(stdout, "Last check %s: the engine had %d running session(s) naming %d port(s).\n", l.At.Format("15:04:05"), l.Sessions, l.Ports)
			for _, n := range l.Notes {
				fmt.Fprintln(stdout, "  "+n)
			}
			if l.Error != "" {
				fmt.Fprintln(stdout, "  FAILED: "+l.Error)
			}
		}
		return 0
	case "--watch":
		every := 30 * time.Second
		if len(args) == 2 {
			var secs int
			if _, err := fmt.Sscanf(args[1], "%d", &secs); err != nil || secs < 5 {
				fmt.Fprintln(stderr, "usage: engineroom forward --watch [seconds, at least 5]")
				return 2
			}
			every = time.Duration(secs) * time.Second
		}
		return watchForwards(m, every, stdout, stderr)
	case "--check":
		return checkForwards(cfg, m, stdout, stderr)
	case "":
		return syncForwards(cfg, m, stdout, stderr)
	}
	fmt.Fprintln(stderr, "usage: engineroom forward [--check | --watch [seconds]] | engineroom forward on|off|status | engineroom forward add|remove|skip|unskip <session>")
	return 2
}

// setSessionForward turns one session's forwarding on or off: with forwarding on for every
// session, by leaving it out or putting it back (Skip); otherwise by choosing it (Sessions).
func setSessionForward(f *config.Forward, session string, on bool) {
	without := func(list []string) []string {
		var kept []string
		for _, s := range list {
			if s != session {
				kept = append(kept, s)
			}
		}
		return kept
	}
	if f.Enabled {
		f.Skip = without(f.Skip)
		if !on {
			f.Skip = append(f.Skip, session)
			sort.Strings(f.Skip)
		}
		return
	}
	f.Sessions = without(f.Sessions)
	if on {
		f.Sessions = append(f.Sessions, session)
		sort.Strings(f.Sessions)
	}
}

// watchForwards keeps the forwards following the engine's running sessions while forwarding is on,
// so a session running on the engine is reachable here without a check every minute. Over the
// connection the forwards are already
// on, it follows the engine's session list (`engineroom sessions --follow`), which the engine sends only when
// a session starts or stops or its ports change: nothing crosses the network in between, and a
// session started on the engine any way at all is forwarded within seconds. With the connection gone
// (the Mac slept, or the network moved), or forwarding off, or working local, it opens nothing:
// every `every` it looks only at the local control socket and the configuration, until the app
// opens the connection again (engineroom forward, at launch, on wake, when the panel opens, after an
// action). An engine too old to follow is checked once each time instead.
func watchForwards(m forward.Master, every time.Duration, stdout, stderr io.Writer) int {
	for {
		cfg, err := loadConfig()
		if err != nil || !cfg.Forward.Active() || cfg.Offline || !m.Alive() {
			time.Sleep(every)
			continue
		}
		began := time.Now()
		followSessions(m, func(sessions []ops.SessionView) {
			// Read the configuration again for each change, so a session skipped since the
			// follow began stays skipped, and working local stops it at once.
			if c, err := loadConfig(); err == nil && c.Forward.Active() && !c.Offline {
				applyForwards(c, m, sessions, stdout, stderr)
			}
		}, stderr)
		if time.Since(began) < 5*time.Second {
			// The follow could not run: an engine without it, or a connection that just went.
			checkForwards(cfg, m, stdout, stderr)
			time.Sleep(every)
		}
	}
}

// followSessions runs `engineroom sessions --follow` on the engine through the open connection and calls
// onChange with each list it sends, until the connection or the engine's side ends.
func followSessions(m forward.Master, onChange func([]ops.SessionView), stderr io.Writer) {
	c := m.Through().Engineroom("sessions", "--follow")
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		var list []ops.SessionView
		if json.Unmarshal(lines.Bytes(), &list) == nil {
			onChange(list)
		}
	}
	_ = cmd.Wait()
}

// checkForwards is one timed pass: it brings the forwards in line only while forwarding is on and
// the connection is open, and then through that connection. With the connection gone (the Mac
// slept, or the network moved) it does nothing at all, rather than connect again on a timer; a
// wake, the app's panel opening or an action brings the connection back (engineroom forward).
func checkForwards(cfg *config.Config, m forward.Master, stdout, stderr io.Writer) int {
	if !cfg.Forward.Active() || cfg.Offline || !m.Alive() {
		return 0
	}
	return syncForwards(cfg, m, stdout, stderr)
}

// syncForwards reads the engine's sessions and makes the forwards match them, running only the
// commands that change something. With nothing to change it runs nothing and records nothing, so
// a watch that finds all well leaves the history alone.
func syncForwards(cfg *config.Config, m forward.Master, stdout, stderr io.Writer) int {
	if cfg.Offline {
		fmt.Fprintln(stdout, "Working local: the engine's ports are not forwarded until this machine hands back to the engine.")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Through the open connection when there is one; with none, ssh connects afresh.
	sessions, _, err := source.Remote{R: m.Through()}.Sessions(ctx)
	if err != nil {
		last := forward.Last{At: time.Now()}
		last.Error = "the engine's sessions could not be read, so the forwards were left as they are: " + err.Error()
		last.Forwarded = forwardedOf(forward.Load(m))
		forward.SaveLast(last)
		fmt.Fprintf(stderr, "engineroom: %s\n", last.Error)
		return 1
	}
	return applyForwards(cfg, m, sessions, stdout, stderr)
}

// applyForwards makes the forwards match the engine's sessions as read, running only the commands
// that change something.
func applyForwards(cfg *config.Config, m forward.Master, sessions []ops.SessionView, stdout, stderr io.Writer) int {
	last := forward.Last{At: time.Now()}
	for _, s := range sessions {
		if !s.Ended {
			last.Sessions++
			last.Ports += len(s.Ports)
		}
	}
	have := forward.Load(m)
	// Recorded however the pass ends: what it found, and what is forwarded once it is done.
	defer func() {
		last.Forwarded = forwardedOf(have)
		forward.SaveLast(last)
	}()
	want, notes := forward.Desired(sessions, cfg.Forward.Wants, netport.Taken, have)
	last.Notes = notes
	for _, n := range notes {
		fmt.Fprintln(stdout, n)
	}
	add, drop := forward.Changes(want, have)
	if len(add) == 0 && len(drop) == 0 {
		last.OK = true
		// Nothing chosen and nothing forwarded: close the connection rather than keep it alive
		// for nothing, which would cost the client's battery a packet every 15 seconds.
		if !cfg.Forward.Active() && len(have) == 0 && m.Alive() {
			_ = stopForwards(m, io.Discard, io.Discard)
		}
		return 0
	}
	plan := action.Plan{Title: "Forward the engine's ports", Machine: "this machine"}
	if !m.Alive() {
		plan.Commands = append(plan.Commands, m.Start())
	}
	first := len(plan.Commands)
	for _, n := range drop {
		plan.Commands = append(plan.Commands, m.Cancel(n, have[n]))
	}
	for _, w := range add {
		plan.Commands = append(plan.Commands, m.Add(w))
	}
	// Only what ran is remembered: a cancel that ran forgets its port, an add that ran remembers
	// its session. A plan that stopped part way leaves the rest as it was, and the next pass
	// tries again.
	ok := runPlainWith(plan, stdout, stderr, func(s action.Step) {
		if s.Status != action.StatusOK || s.Index < first {
			return
		}
		i := s.Index - first
		if i < len(drop) {
			delete(have, drop[i])
			return
		}
		w := add[i-len(drop)]
		have[w.Port.Number] = w.Session
	})
	if err := forward.Save(have); err != nil {
		fmt.Fprintf(stderr, "engineroom: the forwards made could not be remembered: %v\n", err)
	}
	if !ok {
		last.Error = "a forward could not be made or closed; see the history for the command and what ssh said"
		return 1
	}
	last.OK = true
	if !cfg.Forward.Active() && len(have) == 0 {
		_ = stopForwards(m, io.Discard, io.Discard)
	}
	return 0
}

// forwardedOf is the forwards as the record keeps them: port numbers as text.
func forwardedOf(have forward.Have) map[string]string {
	out := map[string]string{}
	for n, s := range have {
		out[fmt.Sprint(n)] = s
	}
	return out
}

// closeForwardsForLocal closes every forward when a client starts working local (the independent
// review, 2026-11-07): this machine then runs its own stacks on the same port numbers, and a
// forward left open would answer for them with the engine's servers, so a local up script would find
// the port taken and start nothing. Forwarding stays switched on and resumes when the client
// hands back to the engine. A failure to close is not the switch's failure.
func closeForwardsForLocal(cfg *config.Config) {
	if cfg.Role != config.RoleClient {
		return
	}
	m := forward.Master{R: action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}}
	if m.Alive() {
		stopForwards(m, io.Discard, io.Discard)
	}
}

// stopForwards closes the master and with it every forward.
func stopForwards(m forward.Master, stdout, stderr io.Writer) int {
	if !m.Alive() {
		_ = forward.Save(forward.Have{})
		fmt.Fprintln(stdout, "Forwarding is off; nothing was forwarded.")
		return 0
	}
	ok := runPlain(action.Plan{Title: "Stop forwarding the engine's ports", Machine: "this machine", Commands: []action.Command{m.Stop()}}, stdout, stderr)
	_ = forward.Save(forward.Have{})
	if !ok {
		return 1
	}
	return 0
}
