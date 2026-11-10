// Package terminal knows the terminal applications a session can be joined in from the menu bar
// app (APP-TERM-001): which of them are installed on this Mac, and the command that opens a new
// window of one running a given command. Like every other package it only builds commands; the
// command it builds is shown and run as part of a plan.
package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/launch"
	"github.com/KyleyHarris/engineroom/internal/shell"
)

// Terminal is one terminal application the program knows how to open.
type Terminal struct {
	// Name is how it is chosen and saved ([app] terminal).
	Name string `json:"name"`
	// Bundle is the application's folder name.
	Bundle string `json:"bundle"`
	// Path is where it is installed, empty when it is not.
	Path string `json:"path,omitempty"`
	// Note is anything the developer should know when choosing it.
	Note string `json:"note,omitempty"`
}

// Known lists the terminals in the order they are offered; the first one installed is the
// default. Ghostty comes first: it allows the clipboard requests copying uses without setup. Terminal.app comes last because it cannot take those requests.
var Known = []Terminal{
	{Name: "Ghostty", Bundle: "Ghostty.app", Note: "Zellij's Alt shortcuts need macos-option-as-alt = true in ~/.config/ghostty/config"},
	{Name: "iTerm2", Bundle: "iTerm.app", Note: "allow clipboard access: Settings, General, Selection"},
	{Name: "WezTerm", Bundle: "WezTerm.app"},
	{Name: "kitty", Bundle: "kitty.app"},
	{Name: "Alacritty", Bundle: "Alacritty.app"},
	{Name: "Terminal", Bundle: "Terminal.app", Note: "cannot take the clipboard requests engineroom uses to copy commands"},
}

// searchDirs is where applications are looked for. ENGINEROOM_APP_DIRS (a PATH-style list) replaces
// it, so a test can offer a known set without depending on the machine it runs on.
func searchDirs() []string {
	if v := os.Getenv("ENGINEROOM_APP_DIRS"); v != "" {
		return filepath.SplitList(v)
	}
	home, _ := os.UserHomeDir()
	return []string{"/Applications", filepath.Join(home, "Applications"), "/System/Applications/Utilities", "/Applications/Utilities"}
}

// Installed is Known with each terminal's Path filled in where it is installed. Only terminals
// found are offered to choose from; the others are listed so the developer can see what was
// looked for.
func Installed() []Terminal {
	out := make([]Terminal, len(Known))
	copy(out, Known)
	for i := range out {
		for _, d := range searchDirs() {
			p := filepath.Join(d, out[i].Bundle)
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				out[i].Path = p
				break
			}
		}
	}
	return out
}

// Choose is the terminal a join opens in: the saved choice when it is installed; with no
// choice, the first installed. A saved choice that is no longer installed is an error naming the
// ones that are, never a quiet switch to another.
func Choose(saved string) (Terminal, error) {
	all := Installed()
	var have []string
	for _, t := range all {
		if t.Path != "" {
			have = append(have, t.Name)
		}
	}
	for _, t := range all {
		if saved == "" && t.Path != "" {
			return t, nil
		}
		if saved != "" && strings.EqualFold(t.Name, saved) {
			if t.Path == "" {
				return Terminal{}, fmt.Errorf("the terminal %s is not installed on this Mac any more; choose one of: %s", t.Name, strings.Join(have, ", "))
			}
			return t, nil
		}
	}
	if saved != "" {
		return Terminal{}, fmt.Errorf("%q is not a terminal engineroom knows; choose one of: %s", saved, strings.Join(have, ", "))
	}
	return Terminal{}, fmt.Errorf("no terminal engineroom knows is installed on this Mac")
}

// TabPrefix starts the title of every tab engineroom opens in Ghostty. The window holding such a tab is
// the engineroom window, so every session joined goes into it as a tab of its own.
const TabPrefix = "engineroom: "

// ghosttyScript opens a session in the one Ghostty window that holds the sessions: the tab already
// joined to it when there is one, else a new tab in the window holding the engineroom tabs, else a new
// window, which is then that window. A tab whose join has ended (its session was stopped or
// restarted) runs nothing, which Ghostty reports as a pid of 0: a new tab joins in its window and
// the dead one is then closed, rather than the dead tab being brought forward. It is closed after
// the new tab is
// open and the windows are no longer being walked: closing it during the walk, when it was its
// window's only tab, closed the window under the walk ("Can't get item 2 of every window").
// Only a tab with nothing running is closed, so closing it asks nothing and loses nothing. Each tab is titled TabPrefix and the session's name, which
// is how the window and the tab are found again; Ghostty keeps a set title whatever the program in
// the tab sets. It brings Ghostty to the front. Its arguments are the tab's title, the command
// line, and the PATH the command runs with.
//
// When Ghostty was not running, starting it opens its own first window as well; that window is
// closed once the session's window is open, so a join never leaves an empty window behind. Only
// windows that existed
// right after Ghostty started, and are not the session's, are closed.
var ghosttyScript = []string{
	"on run argv",
	"set tabTitle to item 1 of argv",
	"set joinCommand to item 2 of argv",
	`set joinEnv to {"PATH=" & (item 3 of argv)}`,
	`set wasRunning to application "Ghostty" is running`,
	`tell application "Ghostty"`,
	"set startupWindows to {}",
	"if not wasRunning then",
	"launch",
	"repeat 50 times",
	"if (count of windows) > 0 then exit repeat",
	"delay 0.1",
	"end repeat",
	"repeat with w in windows",
	"set end of startupWindows to (id of w)",
	"end repeat",
	"end if",
	"set engineroomWindow to missing value",
	"set deadTab to missing value",
	"repeat with w in windows",
	"repeat with t in tabs of w",
	`set o to ""`,
	"try",
	"set o to (name override of t) as text",
	"end try",
	"if o is tabTitle then",
	"if (pid of focused terminal of t) is not 0 then",
	"select tab t",
	"activate window w",
	"activate",
	"return",
	"end if",
	"set deadTab to t",
	"set deadWindow to w",
	"end if",
	`if o starts with "` + TabPrefix + `" then set engineroomWindow to w`,
	"end repeat",
	"end repeat",
	"if deadTab is not missing value then set engineroomWindow to deadWindow",
	"if engineroomWindow is missing value then",
	"set engineroomWindow to new window with configuration {command:joinCommand, environment variables:joinEnv}",
	"set newTab to selected tab of engineroomWindow",
	"else",
	"set newTab to new tab in engineroomWindow with configuration {command:joinCommand, environment variables:joinEnv}",
	"end if",
	"set name override of newTab to tabTitle",
	// The dead tab goes only now, with the windows no longer being walked and its window holding
	// the new tab, so closing the last tab of a window never closes the window joined into.
	"if deadTab is not missing value then",
	"try",
	"close tab deadTab",
	"end try",
	"end if",
	"repeat with wid in startupWindows",
	"if (wid as text) is not (id of engineroomWindow as text) then",
	"try",
	"close window (first window whose id is (wid as text))",
	"end try",
	"end if",
	"end repeat",
	"activate window engineroomWindow",
	"activate",
	"end tell",
	"end run",
}

// Open is the command that opens session in t running argv: in Ghostty, a tab of the one window
// that holds the sessions (or the tab already joined to it); in the others, a new window.
//
// Each is run with an empty environment (launch.Clean), so a terminal it has to start gets the
// developer's own session environment, as one opened from the Dock does, whatever ran this program
// (D-030). The command in the window is given this program's PATH, as the app gives engineroom the
// login shell's (D-011): a window's shell may not have the folders zellij and ssh are installed in.
func Open(t Terminal, session string, argv []string) action.Command {
	why := "open a " + t.Name + " window joined to the session"
	path := os.Getenv("PATH")
	switch t.Name {
	case "Ghostty":
		// Through Ghostty's own scripting, so the session joins the running Ghostty rather than
		// a copy of it started for each join. Ghostty runs the command line through a shell,
		// which undoes the quoting. The tab is given the PATH as a variable of its own, the one
		// thing it takes from this program.
		args := []string{"osascript"}
		for _, l := range ghosttyScript {
			args = append(args, "-e", l)
		}
		return action.Command{
			Argv: launch.Clean(append(args, TabPrefix+session, shell.Join(argv), path)...),
			Why:  "join the session in a tab of the Ghostty window that holds the sessions",
			OnFail: "Ghostty did not take the request. The first time, macOS asks whether engineroom may control Ghostty; " +
				"if that was refused, allow it in System Settings, Privacy & Security, Automation. " +
				"Ghostty's macos-applescript setting must also be left on.",
		}
	}
	// The other terminals take only a command, so the PATH goes into it.
	withPath := append([]string{launch.Env, "PATH=" + path}, argv...)
	line := shell.Join(withPath)
	switch t.Name {
	case "iTerm2":
		return action.Command{Argv: launch.Clean("osascript", "-e", `tell application "iTerm" to create window with default profile command `+appleString(line)), Why: why}
	case "Terminal":
		return action.Command{Argv: launch.Clean("osascript", "-e", `tell application "Terminal" to do script `+appleString(line), "-e", `tell application "Terminal" to activate`), Why: why}
	case "WezTerm":
		return action.Command{Argv: launch.Clean(append([]string{"open", "-na", t.Path, "--args", "start", "--"}, withPath...)...), Why: why}
	case "kitty":
		return action.Command{Argv: launch.Clean(append([]string{"open", "-na", t.Path, "--args"}, withPath...)...), Why: why}
	default: // Alacritty
		return action.Command{Argv: launch.Clean(append([]string{"open", "-na", t.Path, "--args", "-e"}, withPath...)...), Why: why}
	}
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
