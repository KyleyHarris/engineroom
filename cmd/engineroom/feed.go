package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// The app's feed (docs/decisions.md D-023): `engineroom api feed` sends the sessions the app shows as
// one line of JSON, the same reply `engineroom api sessions` gives, now and again each time anything in
// it changes, and nothing in between. The app holds it open for as long as it runs, so the panel
// opens on what is true at once, and the menu bar can say Claude is waiting with the panel closed,
// with no timer on the app's side and no new connection to the engine for each look: the engine,
// plugged in, does the watching, and a laptop's battery is spared.
//
// Where the sessions are on this machine (the engine, or a client working local), it looks at them
// itself every few seconds. On a client that is online, it runs the engine's own feed over one SSH
// connection and passes on each list with this machine's own sessions added, as `api sessions`
// does. Any line the app writes asks it to look again now (after an action, so what the action
// changed shows at once); the app closing its end ends the feed, on the engine too.

// feedEvery is how often the feed looks at this machine's sessions: Zellij's list, each project's
// worktrees and the waiting marks, all on the machine's own disk.
const feedEvery = 3 * time.Second

// feedGitHubEvery is how long a project's GitHub links are kept before they are read again: they
// change only when a branch is pushed.
const feedGitHubEvery = 30 * time.Second

// feedFactsEvery is how long each checkout's facts (unsaved, unpushed, behind, merged) are kept
// before they are read again: about five git commands a checkout, on the engine's own disk.
const feedFactsEvery = 30 * time.Second

// feedKeepAlive is how long the feed stays silent before it sends an empty line. A connection that
// died without closing (a client that slept or left the network) is noticed by the engine only when it
// writes, so this is what ends a feed nobody reads any more.
const feedKeepAlive = time.Minute

// apiFeed is `engineroom api feed [--every <seconds>]`. --every changes how often a local feed looks,
// for a test that proves a request to look now is answered without waiting for the next look.
func apiFeed(cfg *config.Config, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	every := feedEvery
	if len(args) == 2 && args[0] == "--every" {
		secs, err := strconv.Atoi(args[1])
		if err != nil || secs < 1 {
			fmt.Fprintln(stderr, "usage: engineroom api feed [--every <seconds, at least 1>]")
			return 2
		}
		every = time.Duration(secs) * time.Second
	} else if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: engineroom api feed [--every <seconds, at least 1>]")
		return 2
	}
	src := source.For(cfg)
	if r, remote := src.(source.Remote); remote {
		return relayFeed(cfg, r, args, stdin, stdout)
	}
	return localFeed(src.Where(), every, stdin, stdout)
}

// lookRequests reads the app's end: each line is a request to look now. It closes gone when the
// app's end closes, which is how the feed knows nobody reads it any more.
func lookRequests(stdin io.Reader) (look <-chan struct{}, gone <-chan struct{}) {
	l, g := make(chan struct{}, 1), make(chan struct{})
	go func() {
		in := bufio.NewScanner(stdin)
		for in.Scan() {
			select {
			case l <- struct{}{}:
			default: // a look is already asked for; one is enough
			}
		}
		close(g)
	}()
	return l, g
}

// localFeed looks at this machine's sessions every `every`, or at once when asked, and writes the
// list when it differs from the last one written. The configuration is read again for each look,
// so a project pinned or registered since the feed began is laid out as it now is.
func localFeed(where string, every time.Duration, stdin io.Reader, stdout io.Writer) int {
	look, gone := lookRequests(stdin)
	reader := ops.NewSessionReader(feedGitHubEvery)
	sent, wrote := "", time.Now()
	// The checkouts' facts (D-025) are read again every feedFactsEvery, after a request to look
	// now (an action may have committed, pushed or made a worktree), and when the projects they
	// cover change; in between the last are sent again.
	var facts map[string][]ops.CheckoutFacts
	var factsFor string
	var factsAt time.Time
	looked := true
	for {
		// A feed whose SSH session has gone is handed to launchd (parent 1): nobody will read it.
		if os.Getppid() == 1 {
			return 0
		}
		var line, key []byte
		cfg, err := loadConfig()
		if err == nil {
			var list []ops.SessionView
			if list, err = reader.Read(cfg); err == nil {
				if list == nil {
					list = []ops.SessionView{}
				}
				projects := factsProjects(cfg, list)
				if want := strings.Join(projects, "\x00"); looked || want != factsFor || time.Since(factsAt) >= feedFactsEvery {
					facts, factsFor, factsAt = ops.Facts(cfg, projects), want, time.Now()
				}
				line, _ = json.Marshal(map[string]any{"sessions": list, "checkouts": facts, "read": "zellij list-sessions --no-formatting", "where": where})
				key, _ = json.Marshal(map[string]any{"sessions": withoutAge(list), "checkouts": facts})
			}
		}
		if err != nil {
			line, _ = json.Marshal(map[string]any{"error": err.Error(), "engineDown": false})
			key = line
		}
		switch {
		case string(key) != sent:
			if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
				return 0
			}
			sent, wrote = string(key), time.Now()
		case time.Since(wrote) >= feedKeepAlive:
			if _, err := fmt.Fprintln(stdout); err != nil {
				return 0
			}
			wrote = time.Now()
		}
		looked = false
		select {
		case <-gone:
			return 0
		case <-look:
			looked = true
		case <-time.After(every):
		}
	}
}

// factsProjects is the projects whose checkouts the panel shows (D-025): every pinned project, and
// every project with a session running, by name.
func factsProjects(cfg *config.Config, list []ops.SessionView) []string {
	seen := map[string]bool{}
	for _, p := range cfg.Projects {
		if p.Pinned {
			seen[p.Name] = true
		}
	}
	for _, s := range list {
		if s.Project != "" && !s.Ended {
			seen[s.Project] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// withoutAge is the list without each session's age, which Zellij gives as words relative to now
// ("1h 49m 55s ago"): it changes every second, so a list compared with it would differ at every
// look and be sent every time, though nothing the app shows has changed.
func withoutAge(list []ops.SessionView) []ops.SessionView {
	out := make([]ops.SessionView, len(list))
	for i, s := range list {
		s.Created = ""
		out[i] = s
	}
	return out
}

// relayFeed runs the engine's feed over one SSH connection and passes each list on with this
// machine's own sessions added (CONN-HANDOFF-006) and the read said as the client makes it. The
// app's requests to look now go through to the engine, and its closing its end closes the engine's. An
// engine that cannot be reached is said as `api sessions` says it (engineDown), and the feed ends, for
// the app to start again later; so does an engine too old to have a feed.
func relayFeed(cfg *config.Config, r source.Remote, args []string, stdin io.Reader, stdout io.Writer) int {
	c := r.R.Engineroom(append([]string{"api", "feed"}, args...)...)
	shown := r.R.Engineroom("sessions", "--json", "--github").Display()
	cmd := exec.Command(c.Argv[0], c.Argv[1:]...)
	cmd.Stdin = stdin
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return feedError(stdout, err)
	}
	if err := cmd.Start(); err != nil {
		return feedError(stdout, err)
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for lines.Scan() {
		var reply struct {
			Sessions  *[]ops.SessionView `json:"sessions"`
			Checkouts json.RawMessage    `json:"checkouts"`
		}
		if len(lines.Bytes()) == 0 || json.Unmarshal(lines.Bytes(), &reply) != nil || reply.Sessions == nil {
			// A keep-alive, or the engine's own error, passed on as it is.
			if _, err := fmt.Fprintf(stdout, "%s\n", lines.Bytes()); err != nil {
				break
			}
			continue
		}
		list := append(*reply.Sessions, source.HereSessions(cfg)...)
		line, _ := json.Marshal(map[string]any{"sessions": list, "checkouts": reply.Checkouts, "read": shown + "; on this machine: " + source.HereRead, "where": r.Where()})
		if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
			break
		}
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 255 {
		return feedError(stdout, fmt.Errorf("%w: %s", source.ErrEngineDown, strings.TrimSpace(stderr.String())))
	}
	if err != nil {
		return 1
	}
	return 0
}

// feedError is apiError's reply on one line, as every line of the feed is one reply.
func feedError(stdout io.Writer, err error) int {
	line, _ := json.Marshal(map[string]any{"error": err.Error(), "engineDown": errors.Is(err, source.ErrEngineDown)})
	fmt.Fprintf(stdout, "%s\n", line)
	return 3
}

// limitedBuffer keeps the first few kilobytes written to it: ssh's reason for failing.
type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := 4096 - len(l.b); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.b = append(l.b, p[:room]...)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return string(l.b) }
