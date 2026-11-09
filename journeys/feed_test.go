//go:build journeys

package journeys

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// feed is `engineroom api feed` running as the app runs it: its standard input held open (a line there
// is a request to look now) and each list it sends read as it arrives.
type feed struct {
	t     *testing.T
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan map[string]any
	ended chan struct{}
}

// followFeed starts the feed on m, as the app does, and ends it when the test ends.
func followFeed(t *testing.T, m *journey.Machine, args ...string) *feed {
	t.Helper()
	cmd := m.Command("engineroom", append([]string{"api", "feed"}, args...)...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	f := &feed{t: t, cmd: cmd, in: in, lines: make(chan map[string]any, 64), ended: make(chan struct{})}
	go func() {
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for s.Scan() {
			if strings.TrimSpace(s.Text()) == "" {
				continue // a keep-alive, which carries no list
			}
			var v map[string]any
			if err := json.Unmarshal(s.Bytes(), &v); err != nil {
				v = map[string]any{"unreadable": s.Text()}
			}
			f.lines <- v
		}
		_ = cmd.Wait()
		close(f.ended)
	}()
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill() })
	return f
}

// next is the next list the feed sends within d; the test fails without one, or on an error.
func (f *feed) next(d time.Duration) []any {
	f.t.Helper()
	return asList(f.nextReply(d)["sessions"])
}

// nextReply is the next whole line the feed sends within d: the sessions and the checkouts' facts.
func (f *feed) nextReply(d time.Duration) map[string]any {
	f.t.Helper()
	select {
	case v := <-f.lines:
		if v["sessions"] == nil {
			f.t.Fatalf("the feed sent something other than a list: %v", v)
		}
		return v
	case <-f.ended:
		f.t.Fatal("the feed ended")
	case <-time.After(d):
		f.t.Fatalf("the feed sent nothing within %s", d)
	}
	return nil
}

// none fails if the feed sends a list within d.
func (f *feed) none(d time.Duration) {
	f.t.Helper()
	select {
	case v := <-f.lines:
		f.t.Fatalf("the feed sent again though nothing changed: %v", v)
	case <-f.ended:
		f.t.Fatal("the feed ended")
	case <-time.After(d):
	}
}

// look asks the feed to look now, as the app does after an action.
func (f *feed) look() {
	f.t.Helper()
	if _, err := io.WriteString(f.in, "\n"); err != nil {
		f.t.Fatal(err)
	}
}

// feedingClient is a client of an engine with project-a running on it, every ssh call it makes
// recorded.
func feedingClient(t *testing.T) (*journey.Machine, *journey.Engine) {
	t.Helper()
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	engine.BeEngine("project-a", "/home/dev/src/project-a")
	engine.StartSession("project-a", "/home/dev/src/project-a")
	client.BeClientOf(engine, "engine")
	realSSH, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	client.RecordSSH(realSSH)
	return client, engine
}

// APP-FEED-001: a session on the engine starting to wait reaches a client's feed without the client
// asking. Fails if it is not sent within seconds, sent without its question, or if the client
// opened more than the one connection to be told.
func TestAppFeed001(t *testing.T) {
	var client *journey.Machine
	var engine *journey.Engine
	var f *feed
	var term *termtest.Terminal
	var list []any

	scenarios.Run(t, scenarios.AppFeed001{
		GivenTheAppOnAClientFollowsTheEnginesSessions: func() {
			client, engine = feedingClient(t)
			f = followFeed(t, client)
			if a := sessionNamed(t, f.next(15*time.Second), "project-a"); a["waiting"] == "waiting" {
				t.Fatalf("project-a is waiting before anything asked it to: %v", a)
			}
		},
		WhenASessionOnTheEngineStartsWaitingForTheDeveloper: func() {
			engine.MustExec("engineroom signal waiting --session project-a --message 'Approve the plan?'")
		},
		ThenWithinSecondsTheAppIsToldItIsWaitingWithItsQuestion: func() {
			list = f.next(10 * time.Second)
			if a := sessionNamed(t, list, "project-a"); a["waiting"] != "waiting" || a["question"] != "Approve the plan?" {
				t.Fatalf("project-a: %v", a)
			}
		},
		AndItWasToldOverTheOneConnectionTheAppOpened: func() {
			if calls := client.SSHCalls(); len(calls) != 1 {
				t.Fatalf("the client made %d ssh calls, not one: %q", len(calls), calls)
			}
		},
	}, term0(&term))
}

// APP-FEED-002: with nothing changing, the feed sends one list and then nothing, though it looks
// every few seconds and each session's age (Zellij's "...s ago") changes at every look. Fails if
// any second list is sent.
func TestAppFeed002(t *testing.T) {
	m := journey.NewMachine(t)
	a := m.NewRepo("project-a")
	m.BeEngine("project-a", a)
	var f *feed
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppFeed002{
		GivenTheAppFollowsTheSessionsOneOfThemRunning: func() {
			m.StartSession("project-a", a)
			f = followFeed(t, m)
			sessionNamed(t, f.next(10*time.Second), "project-a")
		},
		WhenNothingChangesForAWhile: func() {},
		ThenTheAppIsSentNoListAfterTheFirst: func() {
			f.none(10 * time.Second)
		},
	}, term0(&term))
}

// APP-FEED-003: a request to look now is answered at once, though the feed would look by itself
// only after a minute. Fails if the new session is not sent within seconds of the request.
func TestAppFeed003(t *testing.T) {
	m := journey.NewMachine(t)
	a, b := m.NewRepo("project-a"), m.NewRepo("project-b")
	m.BeEngine("project-a", a, "project-b", b)
	var f *feed
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.AppFeed003{
		GivenTheAppFollowsTheSessionsLookingByItselfOnlyOnceAMinute: func() {
			m.StartSession("project-a", a)
			f = followFeed(t, m, "--every", "60")
			sessionNamed(t, f.next(10*time.Second), "project-a")
		},
		WhenASessionStartsAndTheAppAsksToLookNow: func() {
			m.StartSession("project-b", b)
			f.look()
		},
		ThenTheAppIsToldOfTheNewSessionWithinSeconds: func() {
			sessionNamed(t, f.next(5*time.Second), "project-b")
		},
	}, term0(&term))
}

// APP-FEED-004: the client closing its end ends the engine's feed too. Fails if a feed is still
// running on the engine seconds after, which would be left looking there for nobody.
func TestAppFeed004(t *testing.T) {
	var engine *journey.Engine
	var f *feed
	var term *termtest.Terminal
	following := func() bool {
		out, _ := engine.Exec("ps -eo args")
		return strings.Contains(out, "engineroom api feed")
	}

	scenarios.Run(t, scenarios.AppFeed004{
		GivenTheAppOnAClientFollowsTheEnginesSessions: func() {
			var client *journey.Machine
			client, engine = feedingClient(t)
			f = followFeed(t, client)
			f.next(15 * time.Second)
			if !following() {
				t.Fatal("no feed is running on the engine while the client follows it")
			}
		},
		WhenTheAppClosesItsEnd: func() {
			_ = f.in.Close()
		},
		ThenNothingIsLeftFollowingOnTheEngine: func() {
			deadline := time.Now().Add(10 * time.Second)
			for following() {
				if time.Now().After(deadline) {
					out, _ := engine.Exec("ps -eo args")
					t.Fatalf("a feed is still running on the engine:\n%s", out)
				}
				time.Sleep(200 * time.Millisecond)
			}
			select {
			case <-f.ended:
			case <-time.After(5 * time.Second):
				t.Fatal("the client's feed is still running")
			}
		},
	}, term0(&term))
}

// APP-FEED-006: the feed carries each checkout's facts for a pinned project, read from git: what
// is uncommitted and unpushed, and whether a branch is merged. Fails if a count is wrong, if a
// merged branch is not said to be, or if a branch with no commits of its own is called merged.
func TestAppFeed006(t *testing.T) {
	m, clone, remote := keepWorkProject(t)
	m.WriteConfig(strings.Replace(readConfig(t, m), "name = \"project-a\"", "name = \"project-a\"\npinned = true", 1))
	var term *termtest.Terminal
	var facts map[string]map[string]any
	scenarios.Run(t, scenarios.AppFeed006{
		GivenAPinnedProjectHasACheckoutWithAFileChangedAndACommitThatIsNotOnTheRemote: func() {
			dir := publishWork(t, m, clone, "busy")
			m.WriteFile(filepath.Join(dir, "more.txt"), "more\n", 0o644)
			m.Git(dir, "add", ".")
			m.Git(dir, "commit", "-qm", "more")
			m.WriteFile(filepath.Join(dir, "draft.txt"), "draft\n", 0o644)
		},
		AndAnotherCheckoutWhoseBranchWasMergedIntoTheRemotesMain: func() {
			publishWork(t, m, clone, "done")
			mergeOnRemote(t, m, remote, "done")
			m.Git(clone, "fetch", "-q", "origin")
		},
		AndAThirdOnANewBranchWithNoCommitsOfItsOwn: func() {
			m.Git(clone, "worktree", "add", "-q", "-b", "fresh", clone+"-fresh", "origin/main")
			m.Git(clone+"-fresh", "push", "-q", "-u", "origin", "fresh")
		},
		WhenTheAppFollowsTheSessions: func() {
			reply := followFeed(t, m).nextReply(15 * time.Second)
			facts = map[string]map[string]any{}
			for _, c := range asList(asMap(reply["checkouts"])["project-a"]) {
				cm := asMap(c)
				work, _ := cm["work"].(string)
				facts[work] = cm
			}
		},
		ThenTheFirstIsSentWithOneChangeAndOneCommitNotPushed: func() {
			if b := facts["busy"]; b == nil || b["changes"] != 1.0 || b["unpushed"] != 1.0 {
				t.Fatalf("busy: %v (all: %v)", b, facts)
			}
		},
		AndTheSecondIsSentAsMerged: func() {
			if d := facts["done"]; d == nil || d["merged"] != true {
				t.Fatalf("done: %v", d)
			}
		},
		AndTheThirdIsNot: func() {
			if f := facts["fresh"]; f == nil || f["merged"] == true {
				t.Fatalf("fresh: %v", f)
			}
		},
	}, term0(&term))
}

// readConfig is the machine's configuration file as it is.
func readConfig(t *testing.T, m *journey.Machine) string {
	t.Helper()
	b, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
