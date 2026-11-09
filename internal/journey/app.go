package journey

import (
	"os"
	"path/filepath"
	"strings"
)

// launcherStandIn stands in for macOS's open and osascript, which the program uses to open a
// terminal window. It records the window that would have opened instead of opening one, so no
// journey puts a window on the screen of the machine running it.
const launcherStandIn = `#!/bin/sh
echo "$(basename "$0") $*" >> "$HOME/windows.log"
`

// FakeTerminals installs the named terminal applications (bundle folders such as
// "Ghostty.app") as the only ones this machine has, and the stand-ins that record the windows
// they would open.
func (m *Machine) FakeTerminals(bundles ...string) {
	m.t.Helper()
	apps := filepath.Join(m.Home, "Applications")
	for _, b := range bundles {
		if err := os.MkdirAll(filepath.Join(apps, b), 0o755); err != nil {
			m.t.Fatal(err)
		}
	}
	m.Extra = append(m.Extra, "ENGINEROOM_APP_DIRS="+apps)
	for _, name := range []string{"open", "osascript"} {
		m.write(filepath.Join(m.Bin, name), launcherStandIn)
		if err := os.Chmod(filepath.Join(m.Bin, name), 0o755); err != nil {
			m.t.Fatal(err)
		}
	}
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
