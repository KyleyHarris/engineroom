package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
)

func apps(t *testing.T, bundles ...string) {
	dir := t.TempDir()
	for _, b := range bundles {
		if err := os.MkdirAll(filepath.Join(dir, b), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ENGINEROOM_APP_DIRS", dir)
}

// APP-TERM-001 below the journeys: Ghostty is the default when installed, the first installed
// otherwise; a saved choice is kept; a saved choice no longer installed is refused with the ones
// that are, never quietly replaced.
func TestChoose(t *testing.T) {
	apps(t, "Ghostty.app", "Terminal.app")
	if c, err := Choose(""); err != nil || c.Name != "Ghostty" {
		t.Fatalf("default: %+v %v", c, err)
	}
	if c, err := Choose("terminal"); err != nil || c.Name != "Terminal" {
		t.Fatalf("saved: %+v %v", c, err)
	}
	if _, err := Choose("iTerm2"); err == nil || !strings.Contains(err.Error(), "Ghostty, Terminal") {
		t.Fatalf("uninstalled choice: %v", err)
	}
	apps(t, "Terminal.app")
	if c, err := Choose(""); err != nil || c.Name != "Terminal" {
		t.Fatalf("no Ghostty: %+v %v", c, err)
	}
	apps(t)
	if _, err := Choose(""); err == nil {
		t.Fatal("chose a terminal when none is installed")
	}
}

// The window runs exactly the command given, quoted so a name with spaces survives, with this
// program's PATH, for each way of opening; and every way is run with an empty environment, so a
// terminal it starts gets the developer's own session environment (D-030). Fails if a terminal
// could be started with this program's environment, or the command lost its PATH or its quoting.
func TestOpenCommands(t *testing.T) {
	argv := []string{"/x/engineroom", "open", "my project"}
	t.Setenv("PATH", "/tools-a:/usr/bin")
	g := Open(Terminal{Name: "Ghostty", Path: "/A/Ghostty.app"}, "my project", argv)
	n := len(g.Argv)
	// The command line is quoted for a shell, with no prefix: Ghostty's scripting runs it as
	// given through the login shell, and a "shell:" prefix was taken as part of the path. The
	// PATH goes to the tab as a variable of its own.
	if g.Argv[2] != "/usr/bin/osascript" || g.Argv[n-3] != "engineroom: my project" || g.Argv[n-2] != "/x/engineroom open 'my project'" || g.Argv[n-1] != "/tools-a:/usr/bin" {
		t.Fatal(g.Argv)
	}
	withPath := "/usr/bin/env PATH=/tools-a:/usr/bin /x/engineroom open 'my project'"
	tm := Open(Terminal{Name: "Terminal"}, "my project", argv)
	if !strings.Contains(tm.Argv[4], `do script "`+withPath+`"`) {
		t.Fatal(tm.Argv)
	}
	it := Open(Terminal{Name: "iTerm2"}, "my project", argv)
	if !strings.Contains(it.Argv[4], `command "`+withPath+`"`) {
		t.Fatal(it.Argv)
	}
	al := Open(Terminal{Name: "Alacritty", Path: "/A/Alacritty.app"}, "my project", argv)
	if got := strings.Join(al.Argv[2:], " "); got != "/usr/bin/open -na /A/Alacritty.app --args -e /usr/bin/env PATH=/tools-a:/usr/bin /x/engineroom open my project" {
		t.Fatal(al.Argv)
	}
	for _, c := range []action.Command{
		g, tm, it, al,
		Open(Terminal{Name: "WezTerm", Path: "/A/WezTerm.app"}, "my project", argv),
		Open(Terminal{Name: "kitty", Path: "/A/kitty.app"}, "my project", argv),
	} {
		if c.Argv[0] != "/usr/bin/env" || c.Argv[1] != "-i" {
			t.Fatalf("not started with an empty environment: %q", c.Argv)
		}
	}
}

// Ghostty's script finds the engineroom window by its tabs' titles, adds a tab to it, and starts a
// window only when there is none, replacing a tab whose join has ended; it compiles against the
// installed Ghostty's dictionary where Ghostty is installed. Fails if a join could start a copy of
// Ghostty, add a window while the engineroom window is open, bring a dead tab forward, or if the script
// does not compile.
func TestGhosttyJoinsIntoOneWindow(t *testing.T) {
	g := Open(Terminal{Name: "Ghostty"}, "project-a", []string{"/x/engineroom", "join", "project-a"})
	script := strings.Join(ghosttyScript, "\n")
	for _, want := range []string{`if o is tabTitle then`, `select tab t`, `if o starts with "engineroom: " then set engineroomWindow to w`,
		"new tab in engineroomWindow with configuration {command:joinCommand, environment variables:joinEnv}", "if engineroomWindow is missing value then",
		// A tab whose join ended is closed and replaced in its window, never brought forward.
		"if (pid of focused terminal of t) is not 0 then", "set deadTab to t", "if deadTab is not missing value then set engineroomWindow to deadWindow", "close tab deadTab"} {
		if !strings.Contains(script, want) {
			t.Fatalf("the script lacks %q", want)
		}
	}
	// Closed only after the new tab is open, never while the windows are walked: closing a
	// window's only tab during the walk closed the window under it.
	if strings.Index(script, "close tab deadTab") < strings.Index(script, "set name override of newTab to tabTitle") || strings.Contains(script, "close tab t\n") {
		t.Fatal("a dead tab is closed before the new tab is open")
	}
	for _, a := range g.Argv {
		if strings.Contains(a, "open -n") || a == "-na" {
			t.Fatalf("starts a copy of Ghostty: %q", g.Argv)
		}
	}
	if _, err := os.Stat("/Applications/Ghostty.app/Contents/Resources/Ghostty.sdef"); err != nil {
		t.Skip("Ghostty is not installed here, so the script cannot be compiled against its dictionary")
	}
	args := []string{"-o", filepath.Join(t.TempDir(), "join.scpt")}
	for _, l := range ghosttyScript {
		args = append(args, "-e", l)
	}
	if out, err := exec.Command("osacompile", args...).CombinedOutput(); err != nil {
		t.Fatalf("the script does not compile: %v\n%s", err, out)
	}
}
