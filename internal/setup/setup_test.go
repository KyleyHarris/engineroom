package setup

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/sshtest"
)

// machine is an isolated machine for the check: its own home (so VS Code's settings and
// ~/.zshenv are its own), its own configuration, and a PATH of the system's folders plus a folder
// of its own for stand-ins. Claude Code and VS Code are stood in, by scripts that answer as they
// do: the check only asks them for versions and lists, and the real ones need accounts and an
// application install this test cannot make.
type machine struct {
	t    *testing.T
	home string
	bin  string
}

func newMachine(t *testing.T, config string) *machine {
	t.Helper()
	home := t.TempDir()
	m := &machine{t: t, home: home, bin: filepath.Join(home, "bin")}
	if err := os.MkdirAll(m.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("ENGINEROOM_CONFIG", filepath.Join(home, "config.toml"))
	if config != "" {
		m.write("config.toml", config)
	}
	path := m.bin + ":/usr/bin:/bin:/usr/sbin:/sbin"
	for _, tool := range []string{"zellij", "git", "ssh"} {
		if p, err := exec.LookPath(tool); err == nil {
			path += ":" + filepath.Dir(p)
		}
	}
	t.Setenv("PATH", path)
	return m
}

func (m *machine) write(rel, content string) string {
	m.t.Helper()
	p := filepath.Join(m.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
	return p
}

func (m *machine) tool(name, script string) {
	m.t.Helper()
	p := m.write(filepath.Join("bin", name), "#!/bin/sh\n"+script)
	if err := os.Chmod(p, 0o755); err != nil {
		m.t.Fatal(err)
	}
}

// claudeWithPlugin stands in for Claude Code with engineroom's plugin installed for the user.
func (m *machine) claudeWithPlugin(enabled bool) {
	m.tool("claude", `case "$1" in
--version) echo "2.1.0 (Claude Code)" ;;
plugin) echo '[{"id":"engineroom@engineroom","version":"0.1.0","scope":"user","enabled":`+strconv.FormatBool(enabled)+`}]' ;;
esac
`)
}

// vscode stands in for VS Code's code command, with the extensions given.
func (m *machine) vscode(extensions ...string) {
	m.tool("code", `case "$1" in
--version) echo "1.100.0" ;;
--list-extensions) printf '%s\n' `+strings.Join(extensions, " ")+` ;;
esac
`)
}

func check(t *testing.T) Report {
	t.Helper()
	return Check(context.Background())
}

func item(t *testing.T, r Report, prefix string) Item {
	t.Helper()
	for _, it := range r.Items {
		if strings.HasPrefix(it.Name, prefix) {
			return it
		}
	}
	t.Fatalf("no item %q in the report:\n%s", prefix, dump(r))
	return Item{}
}

func dump(r Report) string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}

// names is each item's name, by whom it is for.
func names(r Report) map[For][]string {
	out := map[For][]string{}
	for _, it := range r.Items {
		out[it.For] = append(out[it.For], it.Name)
	}
	return out
}

var everyItem = []string{"a valid configuration", "a role", "Zellij 0.45.0 or later", "git", "OpenSSH", "Claude Code",
	"engineroom's Claude Code plugin, with its hooks", "at least one project registered"}

// SETUP-CHECK-001, 002, 005 at the package: each role gets every item every machine needs and its
// own, never the other role's, and no role gets the shared items with the role reported missing and
// how to set each. Fails if an item is left out, a role's items leak into the other's report, an
// item is not marked from a real check (each says how it was checked), or the role is not said.
func TestEachRoleIsCheckedForItsOwnItems(t *testing.T) {
	m := newMachine(t, "role = \"engine\"\n[[project]]\nname = \"project-a\"\npath = \"/src/project-a\"\n")
	m.claudeWithPlugin(true)
	engine := check(t)
	got := names(engine)
	if strings.Join(got[Every], "|") != strings.Join(everyItem, "|") {
		t.Fatalf("every-machine items on the engine: %q, want %q", got[Every], everyItem)
	}
	if want := []string{"engineroom found by an SSH command", "the SSH server on (Remote Login)"}; strings.Join(got[Engine], "|") != strings.Join(want, "|") || len(got[Client]) != 0 {
		t.Fatalf("the engine's own items: %q, client items %q", got[Engine], got[Client])
	}
	for _, it := range engine.Items {
		if it.Checked == "" || it.Found == "" || it.Present == (it.Fix != "") {
			t.Fatalf("item %q lacks how it was checked, what was found, or has a fix that does not match its state: %+v", it.Name, it)
		}
	}
	if engine.Role != "engine" || !item(t, engine, "at least one project").Present || !item(t, engine, "engineroom's Claude Code plugin").Present {
		t.Fatalf("the engine's set items are not present:\n%s", dump(engine))
	}

	m.write("config.toml", "role = \"client\"\n[engine]\nhost = \"engine.invalid\"\nconnect_timeout = \"1s\"\n")
	m.vscode(RemoteSSH)
	client := check(t)
	got = names(client)
	if strings.Join(got[Every], "|") != strings.Join(everyItem, "|") || len(got[Engine]) != 0 || len(got[Client]) != 4 {
		t.Fatalf("a client's items: %v", got)
	}
	if item(t, client, "at least one project").Present {
		t.Fatalf("a client with no project of its own is counted as having one:\n%s", dump(client))
	}

	// A Mac used on its own (D-039, SETUP-LOCAL-001): what every machine needs, and nothing of the
	// engine's or a client's.
	m.write("config.toml", "role = \"local\"\n[[project]]\nname = \"project-a\"\npath = \"/src/project-a\"\n")
	local := check(t)
	got = names(local)
	if strings.Join(got[Every], "|") != strings.Join(everyItem, "|") || len(got[Engine])+len(got[Client]) != 0 {
		t.Fatalf("a local Mac's items: %v", got)
	}
	if local.Role != "local" || !item(t, local, "a role").Present || item(t, local, "a role").Found != "local" {
		t.Fatalf("a local Mac's role is not read as set:\n%s", dump(local))
	}

	if err := os.Remove(filepath.Join(m.home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	none := check(t)
	got = names(none)
	if strings.Join(got[Every], "|") != strings.Join(everyItem, "|") || len(got[Engine])+len(got[Client]) != 0 {
		t.Fatalf("a machine with no role: %v", got)
	}
	role := item(t, none, "a role")
	if role.Present || !strings.Contains(role.Fix, "engineroom role set engine") || !strings.Contains(role.Fix, "engineroom role set client --engine") || !strings.Contains(role.Fix, "engineroom role set local") {
		t.Fatalf("no role is not reported missing with how to set each: %+v", role)
	}
	if !item(t, none, "a valid configuration").Present || !item(t, none, "git").Present {
		t.Fatalf("with no role, what can be checked is not:\n%s", dump(none))
	}
}

// snapshot is every file under a folder with its size and time, to see that nothing changed.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		lines = append(lines, p+" "+strconv.FormatInt(info.Size(), 10)+" "+info.ModTime().String()+" "+info.Mode().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// SETUP-CHECK-003 and dependency down: Zellij, Claude Code and VS Code not installed are each
// missing with the command that installs them, and the plugin that cannot be checked without
// Claude Code says so rather than passing; the check changes nothing in the machine's home. Fails
// if a missing tool reads as present, comes without its fix, or the check writes anything.
func TestAMissingToolIsReportedWithItsFixAndNothingChanges(t *testing.T) {
	m := newMachine(t, "role = \"client\"\n[engine]\nhost = \"engine.invalid\"\nconnect_timeout = \"1s\"\n")
	t.Setenv("PATH", m.bin+":/usr/bin:/bin:/usr/sbin:/sbin")
	before := snapshot(t, m.home)
	r := check(t)
	if after := snapshot(t, m.home); after != before {
		t.Fatalf("the check changed the machine:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	zellij := item(t, r, "Zellij")
	if zellij.Present || !strings.Contains(zellij.Found, "needs Zellij 0.45.0 or later") || !strings.Contains(zellij.Fix, "brew install zellij") {
		t.Fatalf("Zellij not installed: %+v", zellij)
	}
	if c := item(t, r, "Claude Code"); c.Present || !strings.Contains(c.Found, "not installed") || c.Fix == "" {
		t.Fatalf("Claude Code not installed: %+v", c)
	}
	if p := item(t, r, "engineroom's Claude Code plugin"); p.Present || !strings.Contains(p.Found, "not checked") {
		t.Fatalf("the plugin, with no Claude Code: %+v", p)
	}
	if c := item(t, r, "VS Code's code"); c.Present || !strings.Contains(c.Fix, "Install 'code' command in PATH") {
		t.Fatalf("VS Code's command not installed: %+v", c)
	}
	if e := item(t, r, "VS Code's Remote - SSH"); e.Present || !strings.Contains(e.Fix, "--install-extension "+RemoteSSH) {
		t.Fatalf("the extension, with no VS Code: %+v", e)
	}
	if r.Missing < 5 {
		t.Fatalf("missing counted as %d:\n%s", r.Missing, dump(r))
	}

	// The plugin installed but switched off is missing with the command that switches it on, and
	// the extension absent from VS Code's list is missing with the command that installs it.
	m.claudeWithPlugin(false)
	m.vscode("some.other-extension")
	r = check(t)
	if p := item(t, r, "engineroom's Claude Code plugin"); p.Present || p.Fix != "claude plugin enable "+PluginID {
		t.Fatalf("the plugin switched off: %+v", p)
	}
	if e := item(t, r, "VS Code's Remote - SSH"); e.Present || e.Found != "not installed" {
		t.Fatalf("the extension not installed: %+v", e)
	}
}

// SETUP-CHECK-006: a configuration that is not valid is missing, naming its file and the field
// that is wrong, and the role and the projects in it are not taken as read. Fails if a bad
// configuration is read past, or the report does not say where to look.
func TestAConfigurationThatIsNotValidNamesItsFileAndField(t *testing.T) {
	newMachine(t, "role = \"hub\"\n")
	r := check(t)
	c := item(t, r, "a valid configuration")
	if c.Present || !strings.Contains(c.Found, os.Getenv("ENGINEROOM_CONFIG")) || !strings.Contains(c.Found, `role "hub"`) {
		t.Fatalf("an unknown role: %+v", c)
	}
	if r.Role != "" || item(t, r, "a role").Present || item(t, r, "at least one project").Present {
		t.Fatalf("a configuration that is not valid was read past:\n%s", dump(r))
	}
}

// SETUP-CHECK-007: engineroom is found by an SSH command only through ~/.zshenv; a folder on the
// PATH of this shell alone is not enough, and the fix is the ~/.zshenv line naming that folder
// under $HOME. Fails if the check reads the PATH it was started with, or names the wrong line.
func TestTheProgramIsFoundByAnSSHCommandOnlyThroughZshenv(t *testing.T) {
	m := newMachine(t, "role = \"engine\"\n")
	m.tool("engineroom", "exit 0\n")
	ssh := item(t, check(t), "engineroom found by an SSH command")
	if ssh.Present || ssh.Fix != `add this line to ~/.zshenv: export PATH="$HOME/bin:$PATH"` {
		t.Fatalf("only on this shell's PATH: %+v", ssh)
	}
	m.write(".zshenv", `export PATH="$HOME/bin:$PATH"`+"\n")
	if ssh := item(t, check(t), "engineroom found by an SSH command"); !ssh.Present || ssh.Found != filepath.Join(m.bin, "engineroom") {
		t.Fatalf("on ~/.zshenv's PATH: %+v", ssh)
	}
}

// The SSH server is found by its greeting on the SSH port: a real sshd is present; a port nothing
// listens on, and a server that answers as something else, are missing with the Remote Login
// setting. Fails if anything that accepts a connection counts as an SSH server.
func TestTheSSHServerIsKnownByItsGreeting(t *testing.T) {
	newMachine(t, "role = \"engine\"\n")
	old := SSHServerAddress
	t.Cleanup(func() { SSHServerAddress = old })

	s := sshtest.Start(t)
	SSHServerAddress = "127.0.0.1:" + strconv.Itoa(s.Port)
	if it := item(t, check(t), "the SSH server"); !it.Present || !strings.HasPrefix(it.Found, "SSH-2.0-") {
		t.Fatalf("a real sshd: %+v", it)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			_ = c.Close()
		}
	}()
	SSHServerAddress = l.Addr().String()
	if it := item(t, check(t), "the SSH server"); it.Present || !strings.Contains(it.Found, "not as an SSH server") || !strings.Contains(it.Fix, "Remote Login") {
		t.Fatalf("a server that is not SSH: %+v", it)
	}
	_ = l.Close()
	if it := item(t, check(t), "the SSH server"); it.Present || !strings.Contains(it.Found, "nothing answers") {
		t.Fatalf("nothing listening: %+v", it)
	}
}

// SETUP-CHECK-008 at the package: an engine whose host key has changed is missing, said as a
// changed host key with what to do, as every action says it (D-034); not as an engine that did
// not answer, and with no "run it again". Against a real sshd restarted with another key.
func TestAnEngineNotReachedIsSaidWithItsCause(t *testing.T) {
	s := sshtest.Start(t)
	newMachine(t, "role = \"client\"\n[engine]\nhost = \""+sshtest.Host+"\"\nssh_config = \""+s.SSHConfig+"\"\nconnect_timeout = \"5s\"\n")
	s.ChangeHostKey()
	e := item(t, check(t), "the engine engine answers")
	if e.Present || !strings.Contains(e.Found, "host key has changed") || !strings.Contains(e.Fix, "check its new key") || strings.Contains(e.Fix, "run the check again") {
		t.Fatalf("a changed host key: %+v", e)
	}
	if !strings.Contains(e.Checked, "engineroom version") || !strings.Contains(e.Checked, s.SSHConfig) {
		t.Fatalf("the check is not the command a person can run: %s", e.Checked)
	}
}

// SETUP-CHECK-009 at the package: VS Code reads its own SSH configuration (remote.SSH.configFile
// in its settings, which may hold comments and trailing commas); when that file does not lead the
// engine's name where engineroom's does, the item is missing and names the file VS Code reads.
// Fails if the check reads engineroom's file in VS Code's place, cannot read VS Code's settings as
// VS Code writes them, or passes a file with no entry for the engine.
func TestVSCodeMustReachTheEngineAsEngineroomDoes(t *testing.T) {
	m := newMachine(t, "")
	ours := m.write("engineroom_ssh", "Host engine\n  HostName engine.example\n  User dev\n  Port 2222\n")
	m.write("config.toml", "role = \"client\"\n[engine]\nhost = \"engine\"\nssh_config = \""+ours+"\"\nconnect_timeout = \"1s\"\n")
	theirs := m.write("vscode_ssh", "Host other\n  HostName other.example\n")
	settingsFile := filepath.Join("Library", "Application Support", "Code", "User", "settings.json")
	m.write(settingsFile, "{\n  // VS Code's own comment, with a \"// in a string\"\n  \"remote.SSH.configFile\": \""+theirs+"\", /* and this */\n  \"x\": [1, 2,],\n}\n")
	if VSCodeSettings() != filepath.Join(m.home, settingsFile) {
		t.Fatalf("VS Code's settings are looked for at %s", VSCodeSettings())
	}

	it := item(t, check(t), "the engine's name engine known to VS Code")
	if it.Present || !strings.Contains(it.Fix, theirs) || !strings.Contains(it.Found, "engine.example") || !strings.Contains(it.Checked, "remote.SSH.configFile") {
		t.Fatalf("VS Code's file with no entry for the engine: %+v", it)
	}

	m.write("vscode_ssh", "Host engine\n  HostName engine.example\n  User dev\n  Port 2222\n")
	if it := item(t, check(t), "the engine's name engine known to VS Code"); !it.Present || it.Found != "both reach dev@engine.example:2222" {
		t.Fatalf("VS Code's file with the engine's entry: %+v", it)
	}

	// With the setting unset VS Code reads ~/.ssh/config, which this machine does not have.
	m.write(settingsFile, "{}\n")
	if it := item(t, check(t), "the engine's name engine known to VS Code"); it.Present || !strings.Contains(it.Found, filepath.Join(m.home, ".ssh", "config")+" does not exist") {
		t.Fatalf("VS Code's default file absent: %+v", it)
	}
}

// StripJSONC keeps what is inside strings, comment markers included, and drops comments and
// trailing commas outside them. Fails if a URL in a setting is cut at its "//".
func TestVSCodeSettingsAreReadAsVSCodeWritesThem(t *testing.T) {
	in := `{"a": "http://x/*y*/", // c
/* block */ "b": [1,
 2, ], "c": "\"//\"",}`
	var v map[string]any
	if err := json.Unmarshal(StripJSONC([]byte(in)), &v); err != nil {
		t.Fatalf("%v\n%s", err, StripJSONC([]byte(in)))
	}
	if v["a"] != "http://x/*y*/" || v["c"] != `"//"` || len(v["b"].([]any)) != 2 {
		t.Fatalf("read as %v", v)
	}
}
