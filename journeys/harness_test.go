//go:build journeys

package journeys

import (
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/journey"
)

// These are not scenarios. They prove the harness itself (issue #1): that the engine is reached
// over real SSH, that Zellij there and here are real and private to the run, and that an
// unreachable address really does not answer. A journey built on a harness that silently
// faked any of these would prove nothing.

func TestHarnessEngineIsReachedOverSSHAndRunsZellij(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)

	out, err := client.Run("ssh", "-F", engine.SSHConfig, "-o", "BatchMode=yes", "engine", "hostname && zellij --version")
	if err != nil {
		t.Fatalf("ssh to the engine: %v\n%s", err, out)
	}
	if !strings.Contains(out, "zellij 0.45") {
		t.Fatalf("Zellij is not on the engine's PATH over SSH:\n%s", out)
	}
	engine.MustExec("zellij attach --create-background harness-probe")
	if s := engine.Sessions(); !strings.Contains(s, "harness-probe") {
		t.Fatalf("a background session created on the engine is not listed:\n%s", s)
	}
}

func TestHarnessLocalZellijIsPrivateToTheRun(t *testing.T) {
	a := journey.NewMachine(t)
	b := journey.NewMachine(t)
	if out, err := a.Run("zellij", "attach", "--create-background", "only-on-a"); err != nil {
		t.Fatalf("create on a: %v\n%s", err, out)
	}
	if !strings.Contains(a.Sessions(), "only-on-a") {
		t.Fatalf("machine a does not see its own session:\n%s", a.Sessions())
	}
	if strings.Contains(b.Sessions(), "only-on-a") {
		t.Fatalf("machine b sees machine a's session; the runs are not isolated:\n%s", b.Sessions())
	}
}

func TestHarnessUnreachableAddressDoesNotAnswer(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	start := time.Now()
	out, err := client.Run("ssh", "-F", engine.SSHConfig, "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", "unreachable-engine", "true")
	if err == nil {
		t.Fatalf("the unreachable address answered:\n%s", out)
	}
	if took := time.Since(start); took < 2*time.Second || took > 10*time.Second {
		t.Fatalf("expected a connect timeout of about 2s, took %v:\n%s", took, out)
	}
}

// Fails if a journey that never chose its terminals can reach the real open or osascript, or if
// the program asks for a window with its own environment: an application started that way keeps
// the caller's home and configuration for as long as it runs, and every window it opens afterwards
// inherits them. The PATH is checked before anything is
// asked to open, so a broken harness fails here without putting a window on the screen.
func TestHarnessNoJourneyOpensARealWindow(t *testing.T) {
	m := journey.NewMachine(t)
	for _, name := range []string{"open", "osascript"} {
		out, err := m.Run("sh", "-c", "command -v "+name)
		if got := strings.TrimSpace(out); err != nil || got != m.Bin+"/"+name {
			t.Fatalf("%s on a journey machine is %q (%v), not the stand-in in %s", name, got, err, m.Bin)
		}
	}
	m.BeEngine("project-a", m.NewRepo("project-a"))
	mustRun(t, m, "start", "project-a", "--mode", "zellij")
	if w := m.Windows(); len(w) != 1 || !strings.Contains(w[0], `tell application "Ghostty"`) || !strings.Contains(w[0], " join project-a ") {
		t.Fatalf("the join was not recorded by the stand-in as a Ghostty window: %q", w)
	}
	// Ghostty would have been asked with an empty environment, so one it started would have the
	// developer's own session environment, not this machine's home and folders (D-030).
	envs := m.WindowEnvs()
	if len(envs) != 1 {
		t.Fatalf("expected the variables of one call, got %q", envs)
	}
	for _, name := range envs[0] {
		if name == "HOME" || strings.HasPrefix(name, "XDG_") || strings.HasPrefix(name, "ZELLIJ") || strings.HasPrefix(name, "FAKE_") {
			t.Fatalf("osascript was run with %s from the program's environment: %q", name, envs[0])
		}
	}
}
