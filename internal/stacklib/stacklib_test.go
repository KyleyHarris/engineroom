package stacklib

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freePort is a port nothing listens on: one the system just handed out and took back.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// run sources the helper in worktree, as an up or down script does, and runs body there.
func run(t *testing.T, helper, worktree, body string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", `. "$1"; `+body, "bash", helper)
	cmd.Dir = worktree
	cmd.Env = append(os.Environ(), "ENGINEROOM_WORKTREE="+worktree, "START_WAIT=20")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func listens(port int) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 300*time.Millisecond)
	if err == nil {
		c.Close()
	}
	return err == nil
}

// The helper, as engineroom hands it out, starts a part in the background and waits for it to
// answer, leaves it alone when started again for the same worktree, refuses a port another
// worktree's part holds, and stops exactly what it started. It fails if the part does not start,
// is started twice, starts on a port another worktree holds, or keeps running after stop.
func TestTheHelperStartsLeavesAndStopsAPart(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 serves the part in this test")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	helper, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(helper); string(b) != string(Script()) {
		t.Fatal("the helper written is not this build's")
	}
	root := t.TempDir()
	a, b := filepath.Join(root, "project-a"), filepath.Join(root, "project-a-other")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	port := freePort(t)
	p := strconv.Itoa(port)
	part := `start site ` + p + ` "http://127.0.0.1:` + p + `/" . -- python3 -m http.server ` + p + ` --bind 127.0.0.1`
	t.Cleanup(func() { run(t, helper, a, "stop site") })

	if out, err := run(t, helper, a, part); err != nil || !strings.Contains(out, "site is running") || !listens(port) {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if out, err := run(t, helper, a, part); err != nil || !strings.Contains(out, "already runs here") {
		t.Fatalf("started again: %v\n%s", err, out)
	}
	if out, err := run(t, helper, b, part); err == nil || !strings.Contains(out, "which is not this worktree's") {
		t.Fatalf("another worktree's port: %v\n%s", err, out)
	}
	if out, err := run(t, helper, a, "stop site"); err != nil || !strings.Contains(out, "+ stop site") {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	for i := 0; i < 30 && listens(port); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if listens(port) {
		t.Fatal("the part still answers after stop")
	}
	if out, _ := run(t, helper, a, "stop site"); !strings.Contains(out, "not started by up here") {
		t.Fatalf("stopping again: %s", out)
	}
}
