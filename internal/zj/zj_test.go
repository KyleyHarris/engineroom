package zj

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"

	"github.com/KyleyHarris/engineroom/internal/desktop"
)

func TestParseSessionList(t *testing.T) {
	text := "project-a [Created 3s ago] (current)\nproject-b [Created 1h 2m ago] (EXITED - attach to resurrect)\nnoise line\nproject-c [Created 0s ago] \n"
	got := Parse(text)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if !got[0].Current || got[0].Ended || !got[1].Ended || got[1].Created != "1h 2m ago" || got[2].Name != "project-c" {
		t.Fatalf("%+v", got)
	}
}

// Sessions obligations O8 and O9: any folder name gives a valid name, and long names are
// shortened deterministically while staying distinct.
func TestSessionNames(t *testing.T) {
	cases := map[string]string{
		"/srv/project-a":         "project-a",
		"/srv/My Project (copy)": "My-Project-copy",
		"/srv/ünï.code":          "n-.code",
		"/srv/...":               "session",
	}
	for in, want := range cases {
		if got := SessionName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	long1 := "/srv/" + strings.Repeat("a", 50) + "-one"
	long2 := "/srv/" + strings.Repeat("a", 50) + "-two"
	n1, n2 := SessionName(long1), SessionName(long2)
	if len(n1) != MaxNameLength || n1 == n2 || SessionName(long1) != n1 {
		t.Fatalf("long names: %q %q", n1, n2)
	}
	exact := "/srv/" + strings.Repeat("b", MaxNameLength)
	if SessionName(exact) != strings.Repeat("b", MaxNameLength) {
		t.Fatal("a name exactly at the limit was changed")
	}
	// The limit itself, as measured on macOS (24 starts, 25 is refused): fails if it is raised
	// past what Zellij accepts there, as 36 once was.
	if n := SessionName("/srv/project-a-twenty-four-c"); len(n) > 24 {
		t.Fatalf("a 28-character folder gave the %d-character name %q; macOS takes 24 at most", len(n), n)
	}
	if n := SessionName("/srv/project-alpha-docs-viewer"); len(n) > 24 {
		t.Fatalf("%q is longer than macOS takes", n)
	}
}

// The standard layout is the agent alone on its tab plus a git tab (SESS-NEW-001). Fails if the
// agent command is lost or unquoted, if continuing is not tried first, if the git tab is
// missing, or if any of the old dev server, tests or logs panes comes back.
func TestLayoutCarriesTheAgentAndQuotesIt(t *testing.T) {
	l := Layout(`my-agent --model "x"`, false, "")
	for _, want := range []string{`my-agent --model \"x\" --continue || my-agent --model \"x\"`, `tab name="agent"`, `tab name="git"`} {
		if !strings.Contains(l, want) {
			t.Fatalf("layout lacks %s:\n%s", want, l)
		}
	}
	for _, gone := range []string{"dev server", `"tests"`, `"logs"`, "remote-control"} {
		if strings.Contains(l, gone) {
			t.Fatalf("layout still has %s:\n%s", gone, l)
		}
	}
}

// With Remote Control on, both starts carry it under the session's own name (SESS-REMOTE-002),
// and name the session so (--name), so a session that continues a conversation shows the session's
// name rather than the old conversation's. Fails if either start lacks either flag, or the name is
// anything but Zellij's session name variable.
func TestLayoutWithRemoteControlNamesTheSession(t *testing.T) {
	l := Layout("my-agent", true, "")
	want := `my-agent --continue --name \"$ZELLIJ_SESSION_NAME\" --remote-control \"$ZELLIJ_SESSION_NAME\" || my-agent --name \"$ZELLIJ_SESSION_NAME\" --remote-control \"$ZELLIJ_SESSION_NAME\"`
	if !strings.Contains(l, want) {
		t.Fatalf("layout lacks %s:\n%s", want, l)
	}
}

// With a shims folder, the agent runs with it first on its PATH, quoted, so its pbcopy is the one
// that copies to the viewing terminal; without one, nothing is added. Fails if the folder is not
// first, or unquoted.
func TestLayoutPutsTheClipboardShimFirst(t *testing.T) {
	l := Layout("my-agent", false, "/state dir/shims")
	if !strings.Contains(l, `PATH='/state dir/shims':\"$PATH\"; export PATH; my-agent --continue`) {
		t.Fatalf("the shims folder is not first on the agent's PATH:\n%s", l)
	}
	if strings.Contains(Layout("my-agent", false, ""), "PATH=") {
		t.Fatal("a layout without shims changed the PATH")
	}
}

// The shim puts its input, byte for byte, trailing newlines included, in an OSC 52 request on
// the terminal, and still succeeds with no terminal at all. Fails if the text is cut or changed,
// or a missing terminal fails the copy (the agent would report a failed copy).
func TestTheClipboardShimSendsTheTextToTheTerminal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pbcopy")
	if err := os.WriteFile(p, []byte(ClipboardShim), 0o755); err != nil {
		t.Fatal(err)
	}
	tty := filepath.Join(dir, "tty")
	for _, target := range []string{tty, filepath.Join(dir, "no", "such", "tty")} {
		cmd := exec.Command(p)
		cmd.Stdin = strings.NewReader("text with ü\n\n")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "ENGINEROOM_REAL_PBCOPY=/usr/bin/true", "ENGINEROOM_CLIPBOARD_TTY=" + target}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the shim failed writing to %s: %v %s", target, err, out)
		}
	}
	raw, err := os.ReadFile(tty)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("\x1b\\]52;c;([A-Za-z0-9+/=]+)\x07").FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no OSC 52 request: %q", raw)
	}
	got, err := base64.StdEncoding.DecodeString(string(m[1]))
	if err != nil || string(got) != "text with ü\n\n" {
		t.Fatalf("the clipboard would hold %q (%v)", got, err)
	}
}

// A session is started without the markers of a Claude Code session that started it, so its agent
// is the developer's own and saves its transcript. The command is run for real, with a shell in
// place of Zellij reporting the environment it was given. Fails if a marker reaches the session,
// if the developer's own Claude Code settings are taken away, or if Create or Resurrect start
// Zellij without it.
func TestASessionStartsWithoutAnotherSessionsMarkers(t *testing.T) {
	markers := []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_SESSION_ATTENDED",
		"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_BRIDGE_SESSION_ID"}
	for _, v := range markers {
		t.Setenv(v, "from-another-session")
	}
	t.Setenv("CLAUDE_CODE_MAX_OUTPUT_TOKENS", "kept")
	argv := withoutAgentSession("sh", "-c", "env")
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range markers {
		if strings.Contains(string(out), v+"=") {
			t.Errorf("%s reached the session", v)
		}
	}
	if !strings.Contains(string(out), "CLAUDE_CODE_MAX_OUTPUT_TOKENS=kept") {
		t.Error("the developer's own Claude Code setting was taken away")
	}
	prefix := strings.Join(withoutAgentSession(), " ") + " zellij attach "
	for name, c := range map[string]action.Command{"Create": Create("project-a", "/tmp/l.kdl", "/tmp"), "Resurrect": Resurrect("project-a")} {
		if !strings.HasPrefix(strings.Join(c.Argv, " "), prefix) {
			t.Errorf("%s starts Zellij with the markers: %v", name, c.Argv)
		}
	}
}

// Fails if a start that should go through the desktop login runs Zellij directly (the session
// would stay in the login that ran the program), or one that should not adds a launchd job.
func TestStartGoesThroughTheDesktopLoginWhenAsked(t *testing.T) {
	t.Setenv(desktop.ModeVar, "off")
	if got := Start("project-a", "/l.kdl", "/src/a"); len(got) != 1 || !strings.Contains(strings.Join(got[0].Argv, " "), "zellij attach --create-background project-a") {
		t.Fatalf("off: %+v", got)
	}
	t.Setenv(desktop.ModeVar, "on")
	domain, ok := desktop.Domain()
	if !ok {
		t.Skip("no desktop login on this machine")
	}
	got := Start("project-a", "/l.kdl", "/src/a")
	if len(got) != 2 || strings.Join(got[0].Argv, " ") != "engineroom session job project-a /l.kdl /src/a" ||
		strings.Join(got[1].Argv, " ") != "launchctl bootstrap "+domain+" "+desktop.JobFile("project-a") {
		t.Fatalf("on: %+v", got)
	}
	if !strings.Contains(got[0].Why, "zellij attach --create-background project-a") {
		t.Fatalf("the job's command is not shown: %q", got[0].Why)
	}
}

// Sessions obligation, dependency down: Zellij missing, or older than MinVersion, is refused where
// every action on sessions starts (the session list), saying which version is needed and how to
// get it; one new enough is used. Missing is a PATH without zellij. An old one is a stand-in on
// PATH that answers --version as that release does: older releases cannot be installed beside
// the current one for a test, and the version answer is the only part of them the check reads.
// Fails if a missing or old Zellij is listed from, or its refusal does not name what is needed.
func TestAMissingOrOldZellijIsRefusedWithWhatIsNeeded(t *testing.T) {
	need := "0.45.0 or later"
	stand := func(t *testing.T, version string) {
		t.Helper()
		bin := t.TempDir()
		script := "#!/bin/sh\ncase \"$1\" in --version) echo 'zellij " + version + "';; *) echo 'No active zellij sessions found.'; exit 1;; esac\n"
		if err := os.WriteFile(filepath.Join(bin, "zellij"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
		versionChecked.Store(false)
	}
	t.Cleanup(func() { versionChecked.Store(false) })

	t.Setenv("PATH", t.TempDir())
	versionChecked.Store(false)
	if _, err := List(); err == nil || !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), need) {
		t.Fatalf("a missing Zellij: %v", err)
	}

	for _, old := range []string{"0.43.1", "0.44.3"} {
		stand(t, old)
		if _, err := List(); err == nil || !strings.Contains(err.Error(), old) || !strings.Contains(err.Error(), need) || !strings.Contains(err.Error(), "brew upgrade zellij") {
			t.Fatalf("Zellij %s: %v", old, err)
		}
	}
	for _, ok := range []string{"0.45.0", "0.45.1", "0.46.0", "1.0.0"} {
		stand(t, ok)
		if list, err := List(); err != nil || len(list) != 0 {
			t.Fatalf("Zellij %s: %v %v", ok, list, err)
		}
	}
}

// A plan starts a session with the one locked start (D-035), and what that start runs is named in
// what the plan shows. Fails if the plan's command is not the locked start, or its description
// hides the Zellij command it runs.
func TestAPlanStartsASessionOnceAndShowsWhatThatRuns(t *testing.T) {
	t.Setenv(desktop.ModeVar, "off")
	c := StartOnce("project-a", "/l.kdl", "/src/a b")
	if got := c.Display(); got != "engineroom session start project-a /l.kdl '/src/a b'" {
		t.Fatalf("the start is shown as %q", got)
	}
	if !strings.Contains(c.Why, "zellij attach --create-background project-a") || !strings.Contains(c.Why, "lock") {
		t.Fatalf("the start does not say what it runs: %q", c.Why)
	}
}
