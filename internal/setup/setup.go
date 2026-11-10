// Package setup checks what this machine needs for its role and lacks (docs/setup.md), and how to
// fix each item. It reports and never fixes: every check reads a file or runs a command that
// changes nothing, and each item says which, so the developer can check it again by hand
// (acceptance/setup, SETUP-CHECK-003 and 004). The `setup-machine` skill and the health view read
// the same report, so what counts as set up is decided here, once.
//
// A client mirrors the engine: it uses the engine, and works on its own when the engine does not
// answer, so every item for running sessions is one every machine needs. Only the engine needs to
// be reached by other machines, and only a client needs to reach the engine and open it in VS Code.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/shell"
	"github.com/KyleyHarris/engineroom/internal/source"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// For says which machines need an item.
type For string

const (
	// Every machine needs it: the items for running sessions, and a role.
	Every For = "every machine"
	// Engine is needed only by the engine: what other machines need to reach it.
	Engine For = "engine"
	// Client is needed only by a client: what it needs to reach the engine and open it in VS Code.
	Client For = "client"
)

// Item is one thing this machine needs, whether it is here, how that was found out and how to fix
// it. Its JSON is what the skill and the health view read (SETUP-READ-002).
type Item struct {
	Name string `json:"name"`
	For  For    `json:"for"`
	// Present is whether the check found it. An item that could not be checked (its check needs
	// something else that is missing) is not present: nothing is counted as set up unproved.
	Present bool `json:"present"`
	// Checked is the command run, or the file read, written so a person can do the same by hand.
	Checked string `json:"checked"`
	// Found is what the check saw: a version, a path, or what went wrong.
	Found string `json:"found"`
	// Fix is the exact command or settings change that would make it present; empty when present.
	Fix string `json:"fix,omitempty"`
}

// Report is the whole check of this machine.
type Report struct {
	// Role is the role read from the configuration: "engine", "client", or empty when none is
	// set or the configuration could not be read.
	Role string `json:"role"`
	// Config is the configuration file the role was read from.
	Config  string `json:"config"`
	Items   []Item `json:"items"`
	Missing int    `json:"missing"`
}

// timeout bounds each command a check runs, so one tool that hangs cannot hold the whole report;
// the engine check is bounded by its own connect limit as well.
const timeout = 15 * time.Second

// SSHServerAddress is where this machine's SSH server is looked for. A variable so a test can point
// it at an SSH server of its own; the program never changes it.
var SSHServerAddress = "127.0.0.1:22"

// PluginID is engineroom's Claude Code plugin as `claude plugin list` names it.
const PluginID = "engineroom@engineroom"

// Check checks this machine and returns every item for its role, present or missing. With no role
// (or a configuration that cannot be read), it checks what every machine needs and reports the
// role missing (SETUP-CHECK-005, 006). The checks run at once, since each only reads; the report
// keeps their order.
func Check(ctx context.Context) Report {
	r := Report{Config: paths.ConfigFile()}
	cfg, err := config.Load()
	valid := err == nil || errors.Is(err, config.ErrNoConfig)
	if valid {
		r.Role = string(cfg.Role)
	}

	checks := []func() Item{
		func() Item { return configItem(r.Config, err) },
		func() Item { return roleItem(r.Config, cfg, valid) },
		zellijItem,
		func() Item { return gitItem(ctx) },
		func() Item { return opensshItem(ctx) },
		func() Item { return claudeItem(ctx) },
		func() Item { return pluginItem(ctx) },
		func() Item { return projectItem(r.Config, cfg, valid) },
	}
	switch r.Role {
	case string(config.RoleEngine):
		checks = append(checks, func() Item { return sshCommandItem(ctx) }, sshServerItem)
	case string(config.RoleClient):
		checks = append(checks,
			func() Item { return engineItem(ctx, cfg) },
			func() Item { return codeItem(ctx, cfg) },
			func() Item { return remoteSSHItem(ctx, cfg) },
			func() Item { return editorSSHItem(ctx, cfg) })
	}

	r.Items = make([]Item, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Items[i] = c()
		}()
	}
	wg.Wait()
	for _, it := range r.Items {
		if !it.Present {
			r.Missing++
		}
	}
	return r
}

// run runs a command that changes nothing and returns what it wrote to standard output and to
// standard error, bounded by timeout.
func run(ctx context.Context, argv ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// failure is a failed command in words: not installed, or what it said.
func failure(err error, said string) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "not installed, or not on the PATH"
	}
	if said != "" {
		return said
	}
	return err.Error()
}

// firstLine keeps a tool's first line, where it says its version.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// roleFix is how to set each role, as CONN-SETUP-001 shows it.
const roleFix = "on the engine: engineroom role set engine; on a client: engineroom role set client --engine <the engine's SSH destination>"

func configItem(file string, err error) Item {
	it := Item{Name: "a valid configuration", For: Every, Checked: "read " + file}
	switch {
	case err == nil:
		it.Present, it.Found = true, "valid"
	case errors.Is(err, config.ErrNoConfig):
		// No file is a machine not set up yet, which is valid; the role item says what to do.
		it.Present, it.Found = true, "no file yet: nothing has been set"
	default:
		// Load names the file and the field (connection obligation O7); nothing is guessed past it.
		it.Found = err.Error()
		it.Fix = "correct the setting named above in " + file + ", or set the role again (" + roleFix + ")"
	}
	return it
}

func roleItem(file string, cfg *config.Config, valid bool) Item {
	it := Item{Name: "a role", For: Every, Checked: "role in " + file}
	switch {
	case !valid:
		it.Found = "not read: the configuration is not valid"
		it.Fix = roleFix
	case cfg.Role == config.RoleClient:
		it.Present, it.Found = true, "client of "+cfg.Engine.Host
	case cfg.Role != "":
		it.Present, it.Found = true, string(cfg.Role)
	default:
		it.Found = "no role set"
		it.Fix = roleFix
	}
	return it
}

func zellijItem() Item {
	it := Item{Name: "Zellij " + zj.NeedVersion() + " or later", For: Every, Checked: "zellij --version"}
	said, err := zj.Version()
	if err != nil {
		// Version's message says the version needed and the brew command that gets it.
		it.Found = err.Error()
		it.Fix = "brew install zellij (or brew upgrade zellij, for an older one)"
		return it
	}
	it.Present, it.Found = true, said
	return it
}

func gitItem(ctx context.Context) Item {
	it := Item{Name: "git", For: Every, Checked: "git --version"}
	out, errOut, err := run(ctx, "git", "--version")
	if err != nil {
		it.Found, it.Fix = failure(err, errOut), "xcode-select --install"
		return it
	}
	it.Present, it.Found = true, firstLine(out)
	return it
}

// opensshItem is ssh, the client every action on the engine and every git push runs through. It
// prints its version to standard error.
func opensshItem(ctx context.Context) Item {
	it := Item{Name: "OpenSSH", For: Every, Checked: "ssh -V"}
	out, errOut, err := run(ctx, "ssh", "-V")
	said := firstLine(errOut + out)
	if err != nil || !strings.Contains(said, "OpenSSH") {
		if err == nil {
			err = errors.New("not OpenSSH")
		}
		it.Found, it.Fix = failure(err, said), "macOS includes OpenSSH; restore /usr/bin/ssh, or brew install openssh"
		return it
	}
	it.Present, it.Found = true, said
	return it
}

func claudeItem(ctx context.Context) Item {
	it := Item{Name: "Claude Code", For: Every, Checked: "claude --version"}
	out, errOut, err := run(ctx, "claude", "--version")
	if err != nil {
		it.Found, it.Fix = failure(err, errOut), "install Claude Code, as its install page says (https://claude.com/claude-code)"
		return it
	}
	it.Present, it.Found = true, firstLine(out)
	return it
}

// pluginItem is engineroom's Claude Code plugin, whose hooks mark a session waiting for an answer
// and tell each session its slot. Installed for the user (not one project) and enabled, since a
// session runs in any project.
func pluginItem(ctx context.Context) Item {
	it := Item{Name: "engineroom's Claude Code plugin, with its hooks", For: Every, Checked: "claude plugin list --json"}
	install := "claude plugin marketplace add KyleyHarris/engineroom && claude plugin install " + PluginID
	out, errOut, err := run(ctx, "claude", "plugin", "list", "--json")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			it.Found, it.Fix = "not checked: Claude Code is not installed", "install Claude Code, then "+install
			return it
		}
		it.Found, it.Fix = failure(err, errOut), install
		return it
	}
	var plugins []struct {
		ID      string `json:"id"`
		Scope   string `json:"scope"`
		Enabled bool   `json:"enabled"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &plugins); err != nil {
		it.Found, it.Fix = "could not read the list: "+err.Error(), install
		return it
	}
	for _, p := range plugins {
		if p.ID != PluginID || p.Scope != "user" {
			continue
		}
		if !p.Enabled {
			it.Found, it.Fix = "installed but not enabled", "claude plugin enable "+PluginID
			return it
		}
		it.Present, it.Found = true, PluginID+" "+p.Version+", enabled"
		return it
	}
	it.Found, it.Fix = "not installed for this user", install
	return it
}

// projectItem is at least one registered project: on the engine what sessions open, on a client
// its own copies, which are what working locally opens.
func projectItem(file string, cfg *config.Config, valid bool) Item {
	it := Item{Name: "at least one project registered", For: Every, Checked: "the [[project]] entries in " + file}
	fix := "engineroom project add <the folder of a repository's main clone>"
	switch {
	case !valid:
		it.Found, it.Fix = "not read: the configuration is not valid", "correct the configuration, then "+fix
	case len(cfg.Projects) == 0:
		it.Found, it.Fix = "none registered", fix
	default:
		it.Present, it.Found = true, fmt.Sprintf("%d registered", len(cfg.Projects))
	}
	return it
}

// sshShellPath is the PATH a shell started by an SSH command begins with on macOS, before it reads
// ~/.zshenv; the check starts from nothing else, as install.sh's check does.
const sshShellPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// sshCommandItem is engineroom found by the shell an SSH command runs in: neither interactive nor
// a login shell, so zsh reads ~/.zshenv and not ~/.zprofile or ~/.zshrc (docs/setup.md, the most
// common first-install failure). Every client action reaches the engine through it.
func sshCommandItem(ctx context.Context) Item {
	home, _ := os.UserHomeDir()
	argv := []string{"env", "-i", "HOME=" + home, "PATH=" + sshShellPath, "/bin/zsh", "-c", "command -v engineroom"}
	it := Item{Name: "engineroom found by an SSH command", For: Engine,
		Checked: `env -i HOME="$HOME" PATH=` + sshShellPath + ` /bin/zsh -c 'command -v engineroom'`}
	out, _, err := run(ctx, argv...)
	if err == nil && out != "" {
		it.Present, it.Found = true, out
		return it
	}
	it.Found = "not found: its folder is not on the PATH of a shell that reads only ~/.zshenv"
	it.Fix = "add this line to ~/.zshenv: export PATH=\"" + withHome(installedFolder(), home) + ":$PATH\""
	return it
}

// installedFolder is the folder engineroom is found in on this shell's PATH, the one to put on an
// SSH command's PATH; where it is on none, the folder of the running program.
func installedFolder() string {
	if p, err := exec.LookPath("engineroom"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return filepath.Dir(abs)
		}
	}
	if p, err := os.Executable(); err == nil {
		return filepath.Dir(p)
	}
	return filepath.Join("$HOME", ".local", "bin")
}

// withHome writes a folder under the home folder as $HOME/..., so the line reads the same for
// anyone who copies it.
func withHome(dir, home string) string {
	if home != "" && (dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		return "$HOME" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// sshServerItem is this machine's SSH server answering, which clients reach the engine through.
// macOS's own check of Remote Login (systemsetup -getremotelogin) needs an administrator, so the
// check connects to the SSH port and reads the greeting every SSH server sends first: it proves an
// SSH server answers here, which is what a client needs.
func sshServerItem() Item {
	host, port, _ := net.SplitHostPort(SSHServerAddress)
	it := Item{Name: "the SSH server on (Remote Login)", For: Engine,
		Checked: "connect to " + host + " port " + port + " and read the server's greeting: nc " + host + " " + port}
	fix := "System Settings, General, Sharing: turn on Remote Login"
	conn, err := net.DialTimeout("tcp", SSHServerAddress, 3*time.Second)
	if err != nil {
		it.Found, it.Fix = "nothing answers: "+err.Error(), fix
		return it
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	greeting := firstLine(string(buf[:n]))
	if !strings.HasPrefix(greeting, "SSH-") {
		it.Found, it.Fix = fmt.Sprintf("something answers, but not as an SSH server (%q)", greeting), fix
		return it
	}
	it.Present, it.Found = true, greeting
	return it
}

func remoteOf(cfg *config.Config) action.Remote {
	return action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
}

// engineItem is the engine answering without a prompt, and running engineroom there: the same
// `engineroom version` over SSH every client action begins with. When it is not reached, the cause
// is said as every action says it (D-034), with what to do.
func engineItem(ctx context.Context, cfg *config.Config) Item {
	c := remoteOf(cfg).Engineroom("version")
	it := Item{Name: "the engine " + cfg.Engine.Host + " answers without a prompt", For: Client, Checked: c.Display()}
	out, errOut, err := run(ctx, c.Argv...)
	if source.SSHFailed(err) {
		u := source.SSHFailure(cfg.Engine.Host, err, errOut)
		it.Found = u.Headline()
		it.Fix = u.Error()
		if source.Retryable(u) {
			it.Fix += "; check the engine is on and both machines are on the network, then run the check again"
		}
		return it
	}
	if err != nil {
		// ssh connected and the command failed: engineroom is not found there, most often.
		it.Found = "the engine answered, but engineroom there failed: " + failure(err, errOut)
		it.Fix = "on the engine, run engineroom setup check and fix what it reports"
		return it
	}
	it.Present, it.Found = true, "the engine runs "+firstLine(out)
	return it
}

// codeItem is VS Code's command, which opens a session's folder on the engine.
func codeItem(ctx context.Context, cfg *config.Config) Item {
	it := Item{Name: "VS Code's " + cfg.Editor.Command + " command", For: Client, Checked: shell.Join([]string{cfg.Editor.Command, "--version"})}
	out, errOut, err := run(ctx, cfg.Editor.Command, "--version")
	if err != nil {
		it.Found = failure(err, errOut)
		it.Fix = "in VS Code's Command Palette: Shell Command: Install 'code' command in PATH"
		return it
	}
	it.Present, it.Found = true, "VS Code "+firstLine(out)
	return it
}

// RemoteSSH is VS Code's Remote - SSH extension, through which it opens the engine's folder.
const RemoteSSH = "ms-vscode-remote.remote-ssh"

func remoteSSHItem(ctx context.Context, cfg *config.Config) Item {
	it := Item{Name: "VS Code's Remote - SSH extension", For: Client, Checked: shell.Join([]string{cfg.Editor.Command, "--list-extensions"})}
	fix := shell.Join([]string{cfg.Editor.Command, "--install-extension", RemoteSSH})
	out, errOut, err := run(ctx, cfg.Editor.Command, "--list-extensions")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			it.Found, it.Fix = "not checked: the "+cfg.Editor.Command+" command is missing", "install the "+cfg.Editor.Command+" command, then "+fix
			return it
		}
		it.Found, it.Fix = failure(err, errOut), fix
		return it
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), RemoteSSH) {
			it.Present, it.Found = true, RemoteSSH+" installed"
			return it
		}
	}
	it.Found, it.Fix = "not installed", fix
	return it
}

// VSCodeSettings is VS Code's user settings file on this machine.
func VSCodeSettings() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "Code", "User", "settings.json")
}

// VSCodeSSHConfig is the SSH configuration file VS Code's Remote - SSH reads: its
// remote.SSH.configFile setting, or ~/.ssh/config when that is not set. It says which, for the
// report.
func VSCodeSSHConfig() (file, from string, err error) {
	home, _ := os.UserHomeDir()
	settings := VSCodeSettings()
	def := filepath.Join(home, ".ssh", "config")
	raw, err := os.ReadFile(settings)
	if errors.Is(err, os.ErrNotExist) {
		return def, "VS Code's default, as " + settings + " does not exist", nil
	}
	if err != nil {
		return "", "", err
	}
	var s map[string]any
	if err := json.Unmarshal(StripJSONC(raw), &s); err != nil {
		return "", "", fmt.Errorf("%s: %v", settings, err)
	}
	v, _ := s["remote.SSH.configFile"].(string)
	if strings.TrimSpace(v) == "" {
		return def, "VS Code's default, as remote.SSH.configFile is not set in " + settings, nil
	}
	if v == "~" || strings.HasPrefix(v, "~/") {
		v = filepath.Join(home, strings.TrimPrefix(v, "~"))
	}
	return v, "remote.SSH.configFile in " + settings, nil
}

// editorSSHItem is the engine's name resolving, in the SSH configuration VS Code reads, to the same
// machine as in the one engineroom uses: VS Code resolves the engine through its own configuration,
// which may not be engineroom's (engine.ssh_config). Compared with ssh's own reading of each file
// (ssh -G, which connects to nothing): the host name, user and port.
func editorSSHItem(ctx context.Context, cfg *config.Config) Item {
	host := cfg.Engine.Host
	it := Item{Name: "the engine's name " + host + " known to VS Code", For: Client}
	file, from, err := VSCodeSSHConfig()
	if err != nil {
		it.Checked = "read " + VSCodeSettings()
		it.Found = "VS Code's settings could not be read: " + err.Error()
		it.Fix = "correct VS Code's settings file, so remote.SSH.configFile can be read"
		return it
	}
	vscodeArgv := []string{"ssh", "-G", "-F", file, host}
	ourArgv := []string{"ssh", "-G", host}
	if cfg.Engine.SSHConfig != "" {
		ourArgv = []string{"ssh", "-G", "-F", cfg.Engine.SSHConfig, host}
	}
	it.Checked = shell.Join(vscodeArgv) + " (the file is " + from + "), compared with " + shell.Join(ourArgv)
	fix := "add the engine's Host " + host + " entry to " + file + ", or set remote.SSH.configFile in VS Code's settings to the file engineroom uses"
	if cfg.Engine.SSHConfig != "" {
		fix = "add the engine's Host " + host + " entry to " + file + ", or set remote.SSH.configFile in VS Code's settings to " + cfg.Engine.SSHConfig
	}
	if _, err := os.Stat(file); err != nil {
		it.Found, it.Fix = file+" does not exist", fix
		return it
	}
	theirs, errOut, err := run(ctx, vscodeArgv...)
	if err != nil {
		it.Found, it.Fix = "ssh could not read "+file+": "+failure(err, errOut), fix
		return it
	}
	ours, errOut, err := run(ctx, ourArgv...)
	if err != nil {
		it.Found, it.Fix = "ssh could not read engineroom's own SSH configuration: "+failure(err, errOut), "correct engineroom's SSH configuration first"
		return it
	}
	a, b := destination(theirs), destination(ours)
	if a != b {
		it.Found = "VS Code reaches " + a + ", engineroom reaches " + b
		it.Fix = fix
		return it
	}
	it.Present, it.Found = true, "both reach "+a
	return it
}

// destination is where ssh -G says a name leads: user@hostname:port.
func destination(g string) string {
	v := map[string]string{}
	for _, line := range strings.Split(g, "\n") {
		k, val, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && (k == "hostname" || k == "user" || k == "port") {
			v[k] = val
		}
	}
	return v["user"] + "@" + v["hostname"] + ":" + v["port"]
}

// StripJSONC turns VS Code's settings (JSON with comments and trailing commas) into JSON: it drops
// // and /* */ comments outside strings, and a comma before a closing bracket or brace.
func StripJSONC(in []byte) []byte {
	var out []byte
	inString, escaped := false, false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && (in[i] != '*' || in[i+1] != '/') {
				i++
			}
			i++
		case c == ']' || c == '}':
			// A trailing comma: the last thing written, past white space, is a comma.
			j := len(out) - 1
			for j >= 0 && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
