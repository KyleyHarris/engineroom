package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The join script run for real against Ghostty, in each state a session's tab can be in. The
// script's other test only compiles it, which cannot see what happens when a tab or window goes
// away while the script walks them; closing a dead tab that was its window's only tab did exactly
// that on a client ("Can't get item 2 of every window"), and no test ran the script to see it.
//
// It opens and closes Ghostty tabs and windows on the screen, so it runs only when asked for:
// ENGINEROOM_TEST_GHOSTTY=1 go test ./internal/terminal -run Live. Every tab it makes is titled
// "engineroom: live-test-..." and is closed again; the developer's own tabs are only read.
//
// Fails if a live tab is not brought forward as it is, if a dead tab (beside another, or alone in
// its window) is not replaced by exactly one live tab, or if the script errors.
func TestLiveGhosttyJoinReplacesOnlyDeadTabs(t *testing.T) {
	if os.Getenv("ENGINEROOM_TEST_GHOSTTY") == "" {
		t.Skip("opens Ghostty tabs on the screen; set ENGINEROOM_TEST_GHOSTTY=1 to run it")
	}
	if _, err := os.Stat("/Applications/Ghostty.app"); err != nil {
		t.Skip("Ghostty is not installed")
	}
	prefix := fmt.Sprintf("live-test-%d-", time.Now().UnixNano()%1_000_000)
	t.Cleanup(func() { closeLiveTestTabs(t, prefix) })

	// The join each case runs: long enough to be seen running, short enough to end by cleanup.
	join := func(session string) {
		t.Helper()
		c := Open(Terminal{Name: "Ghostty"}, session, []string{"/bin/sleep", "6"})
		if out, err := exec.Command(c.Argv[0], c.Argv[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("the join script failed for %s: %v\n%s", session, err, out)
		}
	}

	t.Run("a live tab is brought forward, not replaced", func(t *testing.T) {
		s := prefix + "live"
		ghostty(t, `set nw to new window with configuration {command:"/bin/sleep 6"}`, `set name override of (selected tab of nw) to "`+TabPrefix+s+`"`)
		before := liveTestTabs(t, TabPrefix+s)
		join(s)
		after := liveTestTabs(t, TabPrefix+s)
		if len(before) != 1 || len(after) != 1 || after[0].id != before[0].id {
			t.Fatalf("tabs before %v, after %v", before, after)
		}
	})

	t.Run("a dead tab beside another is replaced in its window", func(t *testing.T) {
		s := prefix + "beside"
		ghostty(t, `set nw to new window with configuration {command:"/bin/echo other"}`, `set name override of (selected tab of nw) to "`+TabPrefix+prefix+`other"`,
			`set dt to new tab in nw with configuration {command:"/bin/echo ended"}`, `set name override of dt to "`+TabPrefix+s+`"`)
		waitDead(t, TabPrefix+s)
		before := liveTestTabs(t, TabPrefix+s)
		join(s)
		after := liveTestTabs(t, TabPrefix+s)
		if len(after) != 1 || after[0].pid == 0 || after[0].window != before[0].window {
			t.Fatalf("tabs before %v, after %v: want one live tab in the same window", before, after)
		}
	})

	t.Run("a dead tab alone in its window is replaced without an error", func(t *testing.T) {
		s := prefix + "alone"
		ghostty(t, `set nw to new window with configuration {command:"/bin/echo ended"}`, `set name override of (selected tab of nw) to "`+TabPrefix+s+`"`)
		waitDead(t, TabPrefix+s)
		join(s)
		after := liveTestTabs(t, TabPrefix+s)
		if len(after) != 1 || after[0].pid == 0 {
			t.Fatalf("tabs after %v: want one live tab", after)
		}
	})
}

type liveTab struct {
	window, id string
	pid        int
}

// ghostty runs lines inside a tell to Ghostty.
func ghostty(t *testing.T, lines ...string) string {
	t.Helper()
	args := []string{"-e", `tell application "Ghostty"`}
	for _, l := range lines {
		args = append(args, "-e", l)
	}
	args = append(args, "-e", "end tell")
	out, err := exec.Command("osascript", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("osascript: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// liveTestTabs lists the tabs titled title: their window, id and the pid of what they run.
func liveTestTabs(t *testing.T, title string) []liveTab {
	t.Helper()
	out := ghostty(t, `set out to ""`, "repeat with w in windows", "repeat with tb in tabs of w", `set o to ""`, "try",
		"set o to (name override of tb) as text", "end try",
		`if o is "`+title+`" then set out to out & (id of w) & " " & (id of tb) & " " & (pid of focused terminal of tb) & linefeed`,
		"end repeat", "end repeat", "return out")
	var tabs []liveTab
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[2])
		tabs = append(tabs, liveTab{window: f[0], id: f[1], pid: pid})
	}
	return tabs
}

// waitDead waits until the tab titled title runs nothing.
func waitDead(t *testing.T, title string) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if tabs := liveTestTabs(t, title); len(tabs) == 1 && tabs[0].pid == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never ended: %v", title, liveTestTabs(t, title))
}

// closeLiveTestTabs closes every tab this test made, once what each runs has ended, so closing
// asks nothing. One tab per call: closing a tab while walking the windows can close a window
// under the walk, the very fault this test is about.
func closeLiveTestTabs(t *testing.T, prefix string) {
	time.Sleep(7 * time.Second)
	for i := 0; i < 20; i++ {
		out := ghostty(t, "repeat with w in windows", "repeat with tb in tabs of w", `set o to ""`, "try",
			"set o to (name override of tb) as text", "end try",
			`if o starts with "`+TabPrefix+prefix+`" then`, "close tab tb", `return "closed"`, "end if",
			"end repeat", "end repeat", `return "none"`)
		if out != "closed" {
			return
		}
	}
}
