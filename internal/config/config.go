// Package config reads and writes the per-user configuration: which role this machine has,
// how to reach the engine, which command starts the AI agent, and the registered projects.
//
// The file lives outside every repository (paths.ConfigFile), because it holds one person's
// setup: host names, paths and project names that never belong in a shared codebase.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/KyleyHarris/engineroom/internal/paths"
)

// Role is what this machine is in the setup.
type Role string

const (
	// RoleEngine is the always-on machine where sessions run.
	RoleEngine Role = "engine"
	// RoleClient is a machine that works on the engine's sessions.
	RoleClient Role = "client"
)

// Config is the whole configuration file.
type Config struct {
	Role     Role      `toml:"role"`
	Engine   Engine    `toml:"engine"`
	Agent    Agent     `toml:"agent"`
	Editor   Editor    `toml:"editor"`
	Projects []Project `toml:"project"`
	Roots    []Root    `toml:"root"`
	// Waiting bounds how long a "waiting for you" signal is believed. A signal can be missed
	// (the agent may not report that it was answered), so an old one is shown as unknown
	// rather than as a session still waiting.
	Waiting Duration `toml:"waiting_signal_limit"`
	// Offline, on a client, means everything is done on this machine as though it were the engine,
	// until it is switched off (CONN-OFFLINE-001, 002). It is the developer's switch: nothing
	// turns it on or off by itself. An engine ignores it.
	Offline bool `toml:"offline,omitempty"`
	App     App  `toml:"app"`
	// Forward is, on a client, whether the engine's sessions' ports are kept reachable here.
	Forward Forward `toml:"forward"`
	// Hidden are repositories on GitHub left out of the repositories list, as "<root>/<name>".
	// Hiding is for repositories, never registered projects: only the list of GitHub's
	// repositories not here hides them; nothing is changed on GitHub or on disk.
	Hidden []string `toml:"hidden,omitempty"`
	// Multiplexer is what keeps this machine's sessions and shows them (D-029, docs/rex.md):
	// "zellij", the default, joined through a terminal app; or "rex", whose app is the terminal.
	// The engine's choice decides where its sessions live; a client is set the same, so it joins
	// them the same way.
	Multiplexer string `toml:"multiplexer,omitempty"`
	// Rex is where this machine's Rex is and, on a client, how it names the engine's.
	Rex Rex `toml:"rex,omitempty"`
}

// Multiplexers are the values Multiplexer may take; "" is Zellij.
var Multiplexers = []string{"zellij", "rex"}

// Rex is this machine's Rex (docs/rex.md), read only when Multiplexer is "rex".
type Rex struct {
	// Path is Rex's command line, when it is not on PATH (Rex puts it there only inside its own
	// terminals, and engineroom also runs from its app and over SSH).
	Path string `toml:"path,omitempty"`
	// Host is, on a client, the label of the engine's Rex server among this machine's Rex hosts
	// (added once with `rex hosts add <label> <tailnet endpoint>`). Joining switches this machine's
	// Rex app to it before showing the session.
	Host string `toml:"host,omitempty"`
}

// IsHidden reports whether a root's repository is hidden from the list.
func (c *Config) IsHidden(root, name string) bool {
	for _, h := range c.Hidden {
		if strings.EqualFold(h, root+"/"+name) {
			return true
		}
	}
	return false
}

// SetHidden hides a root's repository from the list, or shows it again, and saves.
func (c *Config) SetHidden(root, name string, hide bool) error {
	key := root + "/" + name
	var kept []string
	for _, h := range c.Hidden {
		if !strings.EqualFold(h, key) {
			kept = append(kept, h)
		}
	}
	if hide {
		kept = append(kept, key)
	}
	sort.Strings(kept)
	c.Hidden = kept
	return c.Save()
}

// Forward is a client's port forwarding (D-013): while it is on, each running session on the
// engine that has ports has them reachable on this machine's localhost under the same numbers. It is
// off until the developer turns it on, because it listens on ports of this machine.
type Forward struct {
	// Enabled forwards every running session's ports, except those in Skip.
	Enabled bool `toml:"enabled,omitempty"`
	// Skip names sessions whose ports are not forwarded, by the developer's choice.
	Skip []string `toml:"skip,omitempty"`
	// Sessions names the sessions forwarded by choice while Enabled is off: the worktree the
	// developer is testing, switched on from its page or on demand by opening one of its links.
	Sessions []string `toml:"sessions,omitempty"`
}

// Wants reports whether a session's ports are to be forwarded: with Enabled, every session not
// skipped; without, only those chosen.
func (f Forward) Wants(session string) bool {
	if f.Enabled {
		for _, s := range f.Skip {
			if s == session {
				return false
			}
		}
		return true
	}
	for _, s := range f.Sessions {
		if s == session {
			return true
		}
	}
	return false
}

// Active reports whether anything is to be forwarded at all, so the forwarder runs only then.
func (f Forward) Active() bool { return f.Enabled || len(f.Sessions) > 0 }

// App is this machine's choices for the menu bar app.
type App struct {
	// Terminal is the terminal application a session is joined in (APP-TERM-001): one of
	// Terminals' names. Empty means the default, the first of them that is installed.
	Terminal string `toml:"terminal,omitempty"`
	// Start is how the app starts a session unless told otherwise: "zellij" (start and join),
	// "remote" (start with Remote Control, no window) or "both".
	Start string `toml:"start,omitempty"`
}

// StartModes are the ways the app may start a session.
var StartModes = []string{"zellij", "remote", "both"}

// Engine is how a client reaches the engine.
type Engine struct {
	// Host is the SSH destination: an alias from the SSH configuration, or user@host.
	Host string `toml:"host"`
	// SSHConfig, when set, is passed to ssh with -F instead of the user's default file.
	SSHConfig string `toml:"ssh_config"`
	// ConnectTimeout bounds how long the engine has to answer before it counts as unreachable.
	ConnectTimeout Duration `toml:"connect_timeout"`
}

// Agent is the program started on a session's first tab, alone.
type Agent struct {
	Command string `toml:"command"`
	// RemoteControl starts the agent in each new session with Remote Control, under the
	// session's own name. It is this machine's remembered choice (SESS-REMOTE-001): flipped in
	// the interface and saved here, and sent with a client's request, so the engine's own value
	// never decides for a client (SESS-REMOTE-003).
	RemoteControl bool `toml:"remote_control"`
}

// Editor is the code editor a session's worktree is opened in, on this machine.
type Editor struct {
	// Command is the editor's command line program. It is run on the machine the developer is
	// at; on a client it is given the engine as a remote (docs/decisions.md D-007), so it must
	// accept VS Code's "--remote ssh-remote+<host> <folder>" arguments.
	Command string `toml:"command"`
}

// Project is a registered repository: its name and its main clone on this machine.
type Project struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
	// Category is the label the project is listed under (PROJ-CAT-001). It lives here only:
	// nothing is inferred from where the folder is, and changing it moves nothing on disk.
	Category string `toml:"category,omitempty"`
	// Pinned projects are listed first, in their own section, wherever they would otherwise be.
	Pinned bool `toml:"pinned,omitempty"`
}

// Root is a folder on this machine tied to one GitHub owner: that owner's repositories are
// listed from GitHub and cloned into it, one folder per repository (PROJ-ROOT-001). One root per
// owner keeps each account's or organisation's work apart on disk.
type Root struct {
	// Name is how the root is referred to and the heading its projects are listed under.
	Name string `toml:"name" json:"name"`
	Path string `toml:"path" json:"path"`
	// Owner is the GitHub user or organisation whose repositories belong here.
	Owner string `toml:"owner" json:"owner"`
	// Account is the gh account used to list and create the owner's repositories; empty uses
	// whichever account gh has active.
	Account string `toml:"account,omitempty" json:"account,omitempty"`
}

// Duration is a time.Duration written as text ("5s") in the file.
type Duration struct{ time.Duration }

// UnmarshalText reads "5s", "2m" and the like.
func (d *Duration) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		d.Duration = 0
		return nil
	}
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText writes the duration back as text.
func (d Duration) MarshalText() ([]byte, error) {
	if d.Duration == 0 {
		return []byte(""), nil
	}
	return []byte(d.String()), nil
}

// Defaults applied to anything the file leaves out.
const (
	DefaultConnectTimeout = 5 * time.Second
	DefaultAgentCommand   = "claude"
	DefaultEditorCommand  = "code"
	DefaultWaitingLimit   = 6 * time.Hour
)

// ErrNoConfig means this machine has never been set up.
var ErrNoConfig = errors.New("no configuration")

// Load reads the configuration file. A missing file is ErrNoConfig; a file that exists but is
// malformed or invalid is an error naming the file and the field, because guessing past a bad
// configuration would act on the wrong machine or the wrong project.
func Load() (*Config, error) {
	path := paths.ConfigFile()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, ErrNoConfig
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var c Config
	md, err := toml.Decode(string(raw), &c)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown setting(s): %s", path, strings.Join(keys, ", "))
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.applyDefaults()
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Engine.ConnectTimeout.Duration == 0 {
		c.Engine.ConnectTimeout.Duration = DefaultConnectTimeout
	}
	if c.Agent.Command == "" {
		c.Agent.Command = DefaultAgentCommand
	}
	if c.Editor.Command == "" {
		c.Editor.Command = DefaultEditorCommand
	}
	if c.Waiting.Duration == 0 {
		c.Waiting.Duration = DefaultWaitingLimit
	}
}

// nameRule is what a project name may contain. Names become session and folder names, so
// anything a shell or a terminal multiplexer would treat specially is refused here, once,
// rather than escaped in every place a name is used.
var nameRule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidCategory checks a category label. It is shown as a heading on one line, so a line break
// or a label of spaces alone is refused rather than drawn as a broken heading. Empty means no
// category.
func ValidCategory(c string) error {
	if c == "" {
		return nil
	}
	if strings.TrimSpace(c) != c || strings.ContainsAny(c, "\n\r\t") {
		return fmt.Errorf("category %q may not start or end with a space or hold a line break or tab", c)
	}
	if len([]rune(c)) > 40 {
		return fmt.Errorf("category %q is longer than 40 characters", c)
	}
	return nil
}

// Validate checks the configuration as a whole. An empty role is valid: it is the state of a
// machine that has not been set up yet, which the program reports rather than refuses.
func (c *Config) Validate() error {
	switch c.Role {
	case "", RoleEngine:
	case RoleClient:
		if strings.TrimSpace(c.Engine.Host) == "" {
			return errors.New(`role is "client" but engine.host is empty: set the SSH destination of the engine`)
		}
	default:
		return fmt.Errorf(`role %q is not one of "engine" or "client"`, c.Role)
	}
	seen := map[string]bool{}
	for i, p := range c.Projects {
		if !nameRule.MatchString(p.Name) {
			return fmt.Errorf("project %d: name %q may only contain letters, digits, '.', '_' and '-', starting with a letter or digit", i+1, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("project %q is registered twice", p.Name)
		}
		seen[p.Name] = true
		if strings.TrimSpace(p.Path) == "" {
			return fmt.Errorf("project %q has no path", p.Name)
		}
		if err := ValidCategory(p.Category); err != nil {
			return fmt.Errorf("project %q: %w", p.Name, err)
		}
	}
	if c.App.Start != "" {
		ok := false
		for _, m := range StartModes {
			ok = ok || m == c.App.Start
		}
		if !ok {
			return fmt.Errorf(`app.start %q is not one of "zellij", "remote" or "both"`, c.App.Start)
		}
	}
	if c.Multiplexer != "" {
		ok := false
		for _, m := range Multiplexers {
			ok = ok || m == c.Multiplexer
		}
		if !ok {
			return fmt.Errorf(`multiplexer %q is not one of "zellij" or "rex"`, c.Multiplexer)
		}
	}
	roots := map[string]bool{}
	for i, r := range c.Roots {
		if !nameRule.MatchString(r.Name) {
			return fmt.Errorf("root %d: name %q may only contain letters, digits, '.', '_' and '-', starting with a letter or digit", i+1, r.Name)
		}
		if roots[r.Name] {
			return fmt.Errorf("root %q is registered twice", r.Name)
		}
		roots[r.Name] = true
		if strings.TrimSpace(r.Path) == "" {
			return fmt.Errorf("root %q has no path", r.Name)
		}
		if !ownerRule.MatchString(r.Owner) {
			return fmt.Errorf("root %q: owner %q is not a GitHub user or organisation name", r.Name, r.Owner)
		}
		if r.Account != "" && !ownerRule.MatchString(r.Account) {
			return fmt.Errorf("root %q: account %q is not a GitHub account name", r.Name, r.Account)
		}
	}
	return nil
}

// ownerRule is GitHub's rule for user and organisation names: letters, digits and single
// hyphens, not starting or ending with one. It is checked because the owner is placed in
// clone addresses and gh arguments.
var ownerRule = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9])*$`)

// Root returns the root with this name.
func (c *Config) Root(name string) (Root, bool) {
	for _, r := range c.Roots {
		if r.Name == name {
			return r, true
		}
	}
	return Root{}, false
}

// Project returns the registered project with this name.
func (c *Config) Project(name string) (Project, bool) {
	for _, p := range c.Projects {
		if p.Name == name {
			return p, true
		}
	}
	return Project{}, false
}

// ProjectNames lists the registered projects in the order they are configured.
func (c *Config) ProjectNames() []string {
	names := make([]string, len(c.Projects))
	for i, p := range c.Projects {
		names[i] = p.Name
	}
	return names
}

// Save writes the configuration to its file, creating the directory if needed. It writes to a
// temporary file and renames it, so an interrupted save never leaves a half-written file.
func (c *Config) Save() error {
	if err := c.Validate(); err != nil {
		return err
	}
	path := paths.ConfigFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(c); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
