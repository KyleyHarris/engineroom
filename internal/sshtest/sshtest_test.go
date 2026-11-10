package sshtest

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The server answers a command through its configuration, and Stop drops a connection that is
// open, as an engine going away does. Fails if a test built on the server could fail because the
// server itself does not work, or if a dropped connection would hang rather than end.
func TestTheServerAnswersAndStopDropsAConnection(t *testing.T) {
	s := Start(t)
	out, err := exec.Command("ssh", "-F", s.SSHConfig, "-o", "BatchMode=yes", Host, "--", "echo", "answered").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "answered" {
		t.Fatalf("ssh to the server: %v %q\n%s", err, out, s.Log())
	}

	cmd := exec.Command("ssh", "-F", s.SSHConfig, "-o", "BatchMode=yes", Host, "--", "echo up; sleep 30")
	var said strings.Builder
	cmd.Stderr = &said
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The first line is read before the stop, so the connection is known to be open, not still
	// being made.
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "up\n" {
		t.Fatalf("the long command never started: %q %v", line, err)
	}
	start := time.Now()
	s.Stop()
	err = cmd.Wait()
	var ee *exec.ExitError
	if took := time.Since(start); took > 5*time.Second || !asExit(err, &ee) || ee.ExitCode() != 255 {
		t.Fatalf("after Stop: %v after %s, %q", err, took, said.String())
	}
}

func asExit(err error, ee **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	*ee = e
	return ok
}
