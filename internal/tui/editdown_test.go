package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// unreachableAddress is reserved for documentation (RFC 5737): nothing answers on it, so a real
// ssh to it times out exactly as an engine that is off or out of reach does, with no stand-in.
const unreachableAddress = "192.0.2.1"

// Editor obligation O6: `engineroom edit <session>` on a client whose engine does not answer gets
// the same retry, work locally or stop question as every other action, and each answer keeps the
// edit as the request. The engine is checked for real: ssh to an address nothing answers on, with
// an ssh configuration of the test's own so the developer's is never read. Fails if the edit
// skips the engine check, shows a question other than the one an open gets, loses the edit on
// retry or on working locally, or stopping does not end the program with a failure.
func TestEditWithTheEngineDownAsksTheSameQuestion(t *testing.T) {
	dir := t.TempDir()
	sshConfig := filepath.Join(dir, "ssh_config")
	if err := os.WriteFile(sshConfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Working locally reads the configuration again, so it is the test's own file.
	configFile := filepath.Join(dir, "config.toml")
	text := "role = \"client\"\n[engine]\nhost = \"" + unreachableAddress + "\"\nssh_config = \"" + sshConfig + "\"\nconnect_timeout = \"1s\"\n"
	if err := os.WriteFile(configFile, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENGINEROOM_CONFIG", configFile)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	// downModel starts the program for intent and lets it check the engine for real.
	downModel := func(intent Intent) Model {
		t.Helper()
		m := New(cfg, intent)
		if m.screen != screenConnecting {
			t.Fatalf("%+v: the engine is not checked first (screen %v)", intent, m.screen)
		}
		cmd := m.Init()
		if cmd == nil {
			t.Fatalf("%+v: nothing checks the engine", intent)
		}
		start := time.Now()
		msg := cmd()
		probe, ok := msg.(probeMsg)
		if !ok || !errors.Is(probe.err, source.ErrEngineDown) {
			t.Fatalf("%+v: the check of an engine that does not answer gave %#v", intent, msg)
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Fatalf("the check took %s, past its 1s connect limit", took)
		}
		next, _ := m.Update(msg)
		return next.(Model)
	}

	edit := downModel(Intent{Edit: "project-a"})
	if edit.screen != screenEngineDown {
		t.Fatalf("edit with the engine down: screen %v, want the engine-down question", edit.screen)
	}
	open := downModel(Intent{Open: "project-a"})
	if got, want := edit.engineDownView(), open.engineDownView(); got != want {
		t.Fatalf("edit asks a different question from open:\n%s\nwant:\n%s", got, want)
	}
	for _, want := range []string{"retry the engine", "work locally", "stop", "Nothing has been started"} {
		if !strings.Contains(edit.View(), want) {
			t.Fatalf("the question lacks %q:\n%s", want, edit.View())
		}
	}

	// Retry checks the engine again, still for the edit.
	next, cmd := edit.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	retried := next.(Model)
	if retried.screen != screenConnecting || cmd == nil || retried.intent.Edit != "project-a" {
		t.Fatalf("retry: screen %v, check %v, edit %q", retried.screen, cmd != nil, retried.intent.Edit)
	}

	// Working locally carries on with the edit on this machine.
	next, cmd = edit.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	local := next.(Model)
	if _, here := local.src.(source.Local); !here || !local.local || cmd == nil || local.intent.Edit != "project-a" {
		t.Fatalf("work locally: source %T, local %v, next step %v, edit %q", local.src, local.local, cmd != nil, local.intent.Edit)
	}

	// Stop starts nothing and ends the program with a failure.
	next, cmd = edit.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if stopped := next.(Model); !isQuit(cmd) || stopped.exitCode != 1 {
		t.Fatalf("stop: quit %v, exit %d", isQuit(cmd), stopped.exitCode)
	}
}
