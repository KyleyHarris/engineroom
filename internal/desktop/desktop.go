// Package desktop starts a session's server inside the developer's desktop login on a Mac, whatever
// ran the program (docs/decisions.md D-031).
//
// A Zellij session belongs to the login that started its server, and so does everything that runs
// in it: Claude Code, git, gh, codesign. A server started from an SSH login stays in that login,
// where the login keychain is locked and macOS treats it as a background process; started from the
// desktop login it has the keychain and permissions the developer has at the screen. When the
// program runs outside the desktop login while one exists (logged in at the screen, the screen
// locked or not), it hands the start to launchd as a one-time job in that login, and the job removes
// itself once the start has returned. The server it started keeps running.
package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/paths"
)

// ModeVar chooses when a session is started through the desktop login: "off" never, "on" whenever
// a desktop login exists (even when the program already runs in it), and unset only when the
// program runs outside it. The journeys set it to keep their sessions out of the developer's real
// login, and to "on" for the one journey that proves the route.
const ModeVar = "ENGINEROOM_DESKTOP_SESSIONS"

// Domain is the launchd domain of the developer's desktop login ("gui/<uid>"), and whether a
// session should be started through it: on a Mac, when a desktop login exists and the program is
// not already running inside it (launchctl managername is "Aqua" only there). Without a desktop
// login (no one logged in at the screen) a session is started as the program runs, and lacks what
// only that login has.
func Domain() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	mode := os.Getenv(ModeVar)
	if mode == "off" {
		return "", false
	}
	if mode != "on" {
		if out, err := exec.Command("launchctl", "managername").Output(); err == nil && strings.TrimSpace(string(out)) == "Aqua" {
			return "", false
		}
	}
	if exec.Command("launchctl", "print", GUI()).Run() != nil {
		return "", false
	}
	return GUI(), true
}

// GUI is the launchd domain of this user's desktop login.
func GUI() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// Label is the launchd label of the job that starts session. It carries a short hash of the
// program's state folder, so two setups on one Mac (the developer's and a test's) never ask for
// one label at once.
func Label(session string) string {
	sum := sha256.Sum256([]byte(paths.StateDir()))
	return "engineroom." + hex.EncodeToString(sum[:])[:8] + "." + session
}

// JobFile is where the job that starts session is written: the program's own state, one file per
// session, replaced by the next start.
func JobFile(session string) string {
	return filepath.Join(paths.StateDir(), "jobs", Label(session)+".plist")
}

// LogFile is where the job's output goes, so a start that failed can say why.
func LogFile(session string) string {
	return filepath.Join(paths.StateDir(), "jobs", Label(session)+".log")
}

// loginVars are the variables that belong to the login the program runs in rather than to the
// developer: launchd gives the job the desktop login's own (its SSH agent, which lives as long as
// the login does, and its temporary folder), and nothing of an SSH connection that may close.
// The shell's own bookkeeping is left out too.
var loginVars = map[string]bool{
	"SSH_CONNECTION": true, "SSH_CLIENT": true, "SSH_TTY": true, "SSH_AUTH_SOCK": true,
	"TMPDIR": true, "XPC_SERVICE_NAME": true, "XPC_FLAGS": true,
	"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true,
}

// Env is what the job gives the session: environ (the program's own) without the login's variables
// and without the names in drop (Claude Code's session markers, which are never written to a file).
func Env(environ []string, drop []string) map[string]string {
	skip := map[string]bool{}
	for _, d := range drop {
		skip[d] = true
	}
	out := map[string]string{}
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" || loginVars[name] || skip[name] {
			continue
		}
		out[name] = value
	}
	return out
}

// Job is the launchd job that runs argv in dir with env, once, inside domain (GUI), and then removes
// itself: its shell boots the job out when argv returns. AbandonProcessGroup keeps launchd from
// stopping what argv left running (the session's server) when the job goes.
func Job(domain, session string, argv []string, dir string, env map[string]string) []byte {
	label := Label(session)
	var b bytes.Buffer
	str := func(s string) {
		b.WriteString("<string>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</string>")
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("<key>Label</key>")
	str(label)
	b.WriteString("\n<key>ProgramArguments</key>\n<array>\n")
	// argv is passed as the shell's own arguments, so nothing of it is quoted or parsed by the shell.
	for _, a := range append([]string{"/bin/sh", "-c", `"$@"; s=$?; /bin/launchctl bootout ` + domain + "/" + label + `; exit $s`, "sh"}, argv...) {
		str(a)
		b.WriteString("\n")
	}
	b.WriteString("</array>\n<key>WorkingDirectory</key>")
	str(dir)
	b.WriteString("\n<key>EnvironmentVariables</key>\n<dict>\n")
	names := make([]string, 0, len(env))
	for n := range env {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		b.WriteString("<key>")
		_ = xml.EscapeText(&b, []byte(n))
		b.WriteString("</key>")
		str(env[n])
		b.WriteString("\n")
	}
	b.WriteString("</dict>\n<key>RunAtLoad</key><true/>\n<key>AbandonProcessGroup</key><true/>\n")
	b.WriteString("<key>StandardOutPath</key>")
	str(LogFile(session))
	b.WriteString("\n<key>StandardErrorPath</key>")
	str(LogFile(session))
	b.WriteString("\n</dict>\n</plist>\n")
	return b.Bytes()
}

// Write writes the job that starts session (Job) to JobFile, readable only by the developer since
// the environment can hold tokens, and empties its log. It returns the file's path.
func Write(domain, session string, argv []string, dir string, env map[string]string) (string, error) {
	path := JobFile(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, Job(domain, session, argv, dir, env), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(LogFile(session), nil, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
