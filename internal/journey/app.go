package journey

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/terminal"
)

// launcherStandIn stands in for macOS's open and osascript, which the program uses to open a
// terminal window, quit and open the menu bar app, and reach the clipboard. It records what would
// have run instead of running it, so no journey puts a window on the screen of the machine running
// it, or starts an application there: an application started from a journey keeps the journey's
// environment (its home, configuration and Zellij folders) for as long as it runs, and everything
// it opens afterwards inherits them.
const launcherStandIn = `#!/bin/sh
echo "$(basename "$0") $*" >> "$HOME/windows.log"
`

// DefaultTerminals is what every machine has installed until a journey says otherwise: Ghostty, the
// terminal the program chooses first.
var DefaultTerminals = []string{"Ghostty.app"}

// standInLaunchers puts the stand-ins for open and osascript on this machine's PATH in front of
// the real ones. Every machine has them from the start (NewMachine), so a journey that asks the
// program to open a window cannot reach the real terminal, whether or not it looks at the
// windows.
func (m *Machine) standInLaunchers() {
	m.t.Helper()
	for _, name := range []string{"open", "osascript"} {
		m.write(filepath.Join(m.Bin, name), launcherStandIn)
		if err := os.Chmod(filepath.Join(m.Bin, name), 0o755); err != nil {
			m.t.Fatal(err)
		}
	}
}

// FakeTerminals installs the named terminal applications (bundle folders such as
// "Ghostty.app") as the only terminals this machine has, removing the others it had; other
// applications in its folder (the menu bar app) are left. Applications are looked for only in
// this machine's own folder, never the real machine's.
func (m *Machine) FakeTerminals(bundles ...string) {
	m.t.Helper()
	apps := m.AppsDir()
	for _, t := range terminal.Known {
		if err := os.RemoveAll(filepath.Join(apps, t.Bundle)); err != nil {
			m.t.Fatal(err)
		}
	}
	for _, b := range bundles {
		if err := os.MkdirAll(filepath.Join(apps, b), 0o755); err != nil {
			m.t.Fatal(err)
		}
	}
	setting := "ENGINEROOM_APP_DIRS=" + apps
	for _, kv := range m.Extra {
		if kv == setting {
			return
		}
	}
	m.Extra = append(m.Extra, setting)
}

// AppsDir is where FakeTerminals put the applications.
func (m *Machine) AppsDir() string { return filepath.Join(m.Home, "Applications") }

// Windows is every terminal window the program asked to open, one line each.
func (m *Machine) Windows() []string {
	b, err := os.ReadFile(filepath.Join(m.Home, "windows.log"))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// sshRecorder passes every ssh call on to the real ssh, recording it first, so a journey can
// check whether the engine was asked anything at all.
const sshRecorder = `#!/bin/sh
echo "ssh $*" >> "$HOME/ssh.log"
exec %s "$@"
`

// RecordSSH puts the ssh recorder on this machine's PATH in front of the real ssh.
func (m *Machine) RecordSSH(realSSH string) {
	m.t.Helper()
	m.write(filepath.Join(m.Bin, "ssh"), strings.Replace(sshRecorder, "%s", realSSH, 1))
	if err := os.Chmod(filepath.Join(m.Bin, "ssh"), 0o755); err != nil {
		m.t.Fatal(err)
	}
}

// SSHCalls is every ssh call recorded on this machine.
func (m *Machine) SSHCalls() []string {
	b, err := os.ReadFile(filepath.Join(m.Home, "ssh.log"))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}
