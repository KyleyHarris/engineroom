package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The repository's own stack scripts, .engineroom/up and .engineroom/down, run for real against this
// repository: they build the site tool, start its dev server on a free port, and stop it. Their
// record and log go to a temporary folder (SITE_RUN_DIR) so the test never touches a server the
// developer is running.

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func runRepoScript(t *testing.T, root, name string, port int, runDir string) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, ".engineroom", name))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "ENGINEROOM_WORKTREE="+root, "ENGINEROOM_PORT_SITE="+strconv.Itoa(port), "SITE_RUN_DIR="+runDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func recordedPID(t *testing.T, runDir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runDir, "site.pid"))
	if err != nil {
		t.Fatalf("no process record: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("the process record %q is not a process id", b)
	}
	return pid
}

// Guards the stack scripts' contract: up starts the site and returns once it answers, up again
// leaves it alone, down stops exactly it, down again does nothing, and a port someone else holds
// is refused with the holder named rather than taken over.
func TestRepoScripts(t *testing.T) {
	for _, tool := range []string{"bash", "curl", "lsof", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the stack scripts need %s, which this machine lacks", tool)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	port := freePort(t)
	t.Cleanup(func() { _, _ = runRepoScript(t, root, "down", port, runDir) })

	out, err := runRepoScript(t, root, "up", port, runDir)
	if err != nil {
		t.Fatalf("up failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, fmt.Sprintf("The website is running: http://localhost:%d/", port)) {
		t.Errorf("up did not say where the site is:\n%s", out)
	}
	pid := recordedPID(t, runDir)
	if code, body := get(t, fmt.Sprintf("http://127.0.0.1:%d/", port)); code != 200 || !strings.Contains(body, "<title>") {
		t.Fatalf("the site does not answer after up: %d\n%s", code, body)
	}

	out, err = runRepoScript(t, root, "up", port, runDir)
	if err != nil || !strings.Contains(out, "already running") {
		t.Errorf("up again: err=%v, want it to leave the running site alone:\n%s", err, out)
	}
	if again := recordedPID(t, runDir); again != pid {
		t.Errorf("up again started another server: %d, was %d", again, pid)
	}

	out, err = runRepoScript(t, root, "down", port, runDir)
	if err != nil || !strings.Contains(out, "The website is stopped.") {
		t.Fatalf("down: err=%v\n%s", err, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for alive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if alive(pid) {
		t.Errorf("the server (process %d) is still running after down", pid)
	}
	if _, err := os.Stat(filepath.Join(runDir, "site.pid")); err == nil {
		t.Error("down left the process record behind")
	}

	out, err = runRepoScript(t, root, "down", port, runDir)
	if err != nil || !strings.Contains(out, "is not running here") {
		t.Errorf("down again: err=%v, want it to do nothing:\n%s", err, out)
	}

	// Someone else's server on the port: up refuses and names it.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	heldPort := held.Addr().(*net.TCPAddr).Port
	out, err = runRepoScript(t, root, "up", heldPort, runDir)
	if err == nil {
		t.Fatalf("up on a port in use succeeded:\n%s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("Port %d is already in use", heldPort)) || !strings.Contains(out, strconv.Itoa(os.Getpid())) {
		t.Errorf("up did not name the port's holder (this test, process %d):\n%s", os.Getpid(), out)
	}
}
