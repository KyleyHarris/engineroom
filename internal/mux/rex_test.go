package mux

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// standIn writes a stand-in rex that records each command line it is given, one per line, in
// log, and answers `ls --json` with list (or exits with status code when it is not 0). It stands
// for the real one, which needs the Rex app and an invite (docs/rex.md); it answers in the shape
// Rex's documentation gives.
func standIn(t *testing.T, list string, code int) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin, log = filepath.Join(dir, "rex"), filepath.Join(dir, "log")
	script := "#!/bin/sh\n" +
		"echo \"$*\" >> '" + log + "'\n" +
		"[ " + strconv.Itoa(code) + " -ne 0 ] && { echo 'rex: failed' >&2; exit " + strconv.Itoa(code) + "; }\n" +
		"case \"$1\" in ls) printf '%s' '" + list + "';; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// The documented `rex ls --json` is read as one session per label, none ended. Fails if a label is
// lost, an ID is taken for a name, or a session is listed as ended.
func TestRexListReadsTheDocumentedShape(t *testing.T) {
	bin, log := standIn(t, `{"sessions":[{"session_id":"session:0a1b","label":"project-a"},{"session_id":"session:2c3d","label":"project-a-fix-login"}]}`, 0)
	got, err := Rex{Bin: bin}.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []Session{{Name: "project-a"}, {Name: "project-a-fix-login"}}
	if !slices.Equal(got, want) {
		t.Fatalf("listed %+v, want %+v", got, want)
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "ls --json" {
		t.Fatalf("ran %q, want rex ls --json", b)
	}
}

// An answer that is not the documented JSON is an error, never an empty list that would read as
// "no sessions" and let a second session be created under a name in use.
func TestRexListRefusesAnAnswerItCannotRead(t *testing.T) {
	if _, err := ParseList([]byte("project-a  running")); err == nil {
		t.Fatal("an unreadable list was taken as a list")
	}
}

// Exit 4 is Rex's "server unreachable", said as such rather than as a bare exit status.
func TestRexListSaysTheServerIsNotRunning(t *testing.T) {
	bin, _ := standIn(t, "", 4)
	_, err := Rex{Bin: bin}.List()
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("exit 4 read as %v", err)
	}
}

// A checkout's session is created in Rex labelled with its name, in its folder, with Claude's
// window running the agent script through the login shell and a terminal window beside it; both
// carry ENGINEROOM_SESSION and are free of another Claude Code session's markers. Fails if any of
// those is missing, or the variable is set after the program it is meant for.
func TestRexCreatesClaudeAndATerminal(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	cmds := Rex{Bin: "rex"}.Create("project-a", "/work/project-a", Standard("/state/layouts/standard.kdl"))
	if len(cmds) != 2 {
		t.Fatalf("%d commands, want the session and its terminal window: %v", len(cmds), cmds)
	}
	agent, term := strings.Join(cmds[0].Argv, " "), strings.Join(cmds[1].Argv, " ")
	for _, want := range []string{"rex session new project-a --cwd /work/project-a --window claude --shell /bin/zsh --interactive --json -- env ", " ENGINEROOM_SESSION=project-a sh /state/layouts/standard.sh"} {
		if !strings.Contains(agent, want) {
			t.Errorf("the session's command %q lacks %q", agent, want)
		}
	}
	for _, want := range []string{"rex window new -s project-a terminal --focus=false --cwd /work/project-a --shell /bin/zsh --interactive -- env ", " ENGINEROOM_SESSION=project-a /bin/zsh"} {
		if !strings.Contains(term, want) {
			t.Errorf("the terminal window's command %q lacks %q", term, want)
		}
	}
	for _, c := range cmds {
		for _, v := range zj.AgentSessionVars {
			if !slices.Contains(c.Argv, v) || c.Argv[slices.Index(c.Argv, v)-1] != "-u" {
				t.Errorf("%v does not unset %s", c.Argv, v)
			}
		}
	}
}

// An assistant or upgrade session is the agent alone, as its Zellij layout is.
func TestRexCreatesAnAssistantWithoutATerminal(t *testing.T) {
	cmds := Rex{Bin: "rex"}.Create("project-a-ask", "/work/project-a", AgentOnly("/state/assistant/project-a-ask.kdl"))
	if len(cmds) != 1 || !strings.HasSuffix(strings.Join(cmds[0].Argv, " "), "sh /state/assistant/project-a-ask.sh") {
		t.Fatalf("an assistant session was created as %v", cmds)
	}
}

// Joining in Rex is one Viewer command in the one shape a client runs on its own machine, so a
// client's plan from the engine shows the session on the client's screen, not the engine's; and
// nothing takes over a terminal. Fails if the command would be sent over SSH, or is interactive.
func TestRexJoinRunsWhereTheDeveloperIs(t *testing.T) {
	j := Rex{}.Join("project-a")
	if !j.Viewer || j.Interactive || !action.IsShow(j.Argv) {
		t.Fatalf("join is %+v", j)
	}
	r := action.Remote{Host: "engine.example"}
	if got := r.Wrap(j); !slices.Equal(got.Argv, j.Argv) {
		t.Fatalf("a client would run the join over SSH: %v", got.Argv)
	}
	// Any other command marked Viewer still runs on the engine: a client runs only the show
	// command on its own machine.
	other := action.Command{Argv: []string{"engineroom", "rex", "show", "project-a", "--extra"}, Viewer: true}
	if got := r.Wrap(other); got.Argv[0] != "ssh" {
		t.Fatalf("a Viewer command of another shape ran on the client: %v", got.Argv)
	}
}

// Showing a session on the engine is Go to Session; on a client, Switch Host to the engine's Rex
// first, then Go to Session there. Fails if a client's show skips the switch or picks the session
// on its own server.
func TestRexShowSwitchesAClientToTheEngine(t *testing.T) {
	r := Rex{Bin: "rex"}
	if got := r.ShowArgv("project-a", ""); len(got) != 1 || strings.Join(got[0], " ") != "rex do session.select session=project-a" {
		t.Fatalf("on the engine: %v", got)
	}
	got := r.ShowArgv("project-a", "engine")
	if len(got) != 2 || strings.Join(got[0], " ") != "rex do client.host.switch host=engine" || strings.Join(got[1], " ") != "rex -S engine do session.select session=project-a" {
		t.Fatalf("on a client: %v", got)
	}
}

// Ending a session in Rex is session kill, and nothing is left to forget.
func TestRexStopsAndKeepsNothing(t *testing.T) {
	r := Rex{Bin: "rex"}
	if got := strings.Join(r.Stop("project-a"), " "); got != "rex session kill project-a" {
		t.Fatalf("stop is %q", got)
	}
	if r.Forget("project-a") != nil {
		t.Fatal("Rex was asked to forget a session it does not keep")
	}
}

// Each documented exit status that engineroom can say more about is said; 3 never reads as a
// failure to retry, and 4 names the engine's host when the command went to it.
func TestRexExitSaysWhatEachStatusMeans(t *testing.T) {
	if !strings.Contains(RexExit(3, ""), "nothing was changed") {
		t.Error("exit 3")
	}
	if !strings.Contains(RexExit(4, "engine"), "engine") || !strings.Contains(RexExit(4, "engine"), "Tailscale") {
		t.Error("exit 4 on a client")
	}
	if !strings.Contains(RexExit(4, ""), "this machine") {
		t.Error("exit 4 here")
	}
	if !strings.Contains(RexExit(2, ""), "defect") {
		t.Error("exit 2")
	}
	if RexExit(1, "") != "" {
		t.Error("exit 1 has nothing to add to Rex's own message")
	}
}

// The configured kind picks the backend; Zellij is the default, and an unknown kind is refused.
func TestUsePicksTheConfiguredBackend(t *testing.T) {
	t.Cleanup(func() { _ = Use("", "") })
	if err := Use("rex", "/opt/rex"); err != nil || Active().Kind() != "rex" || Active().(Rex).Bin != "/opt/rex" {
		t.Fatalf("rex: %v %+v", err, Active())
	}
	if err := Use("", ""); err != nil || Active().Kind() != "zellij" {
		t.Fatalf("default: %v %+v", err, Active())
	}
	if err := Use("tmux", ""); err == nil {
		t.Fatal("an unknown multiplexer was taken")
	}
}

// Inside a session, engineroom's own variable names it; Zellij's is read only for a session
// started before engineroom set its own.
func TestCurrentPrefersEngineroomsOwnName(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "from-zellij")
	t.Setenv(SessionVar, "project-a")
	if Current() != "project-a" {
		t.Fatalf("current is %q", Current())
	}
	t.Setenv(SessionVar, "")
	if Current() != "from-zellij" {
		t.Fatalf("current without engineroom's own is %q", Current())
	}
}
