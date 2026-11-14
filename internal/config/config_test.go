package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("ENGINEROOM_CONFIG", path)
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// A machine that was never set up must be told so, not treated as broken.
func TestMissingFileIsNoConfig(t *testing.T) {
	writeConfig(t, "")
	_, err := Load()
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("want ErrNoConfig, got %v", err)
	}
}

// Connection obligation O7: each invalid configuration is refused with the file and the field
// named, before anything runs. A test that only checked "an error happened" would pass if
// the wrong rule fired, so each case asserts the words that identify its rule.
// D-039: a local Mac's configuration loads with no engine to reach, and keeps an engine's
// address written before, so setting the client role again needs none. Fails if local is refused,
// needs an engine, or loses the address.
func TestALocalRoleLoadsWithOrWithoutAnEngine(t *testing.T) {
	writeConfig(t, "role = \"local\"\n")
	c, err := Load()
	if err != nil || c.Role != RoleLocal || !c.Role.RunsHere() || c.Describe() != "used on its own (local)" {
		t.Fatalf("local alone: %v %+v", err, c)
	}
	writeConfig(t, "role = \"local\"\n[engine]\nhost = \"engine.example\"\n")
	c, err = Load()
	if err != nil || c.Engine.Host != "engine.example" {
		t.Fatalf("local with an engine kept: %v %+v", err, c)
	}
	if RoleClient.RunsHere() || !RoleEngine.RunsHere() {
		t.Fatal("RunsHere is wrong for the engine or a client")
	}
}

func TestInvalidConfigurationsAreRefusedNamingTheProblem(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"client without an engine", `role = "client"`, "engine.host is empty"},
		{"unknown role", `role = "server"`, `role "server" is not one of "engine", "client" or "local"`},
		{"malformed toml", `role = "engine`, "config.toml"},
		{"unknown setting", "role = \"engine\"\ncolour = \"red\"", "unknown setting(s): colour"},
		{"bad timeout", "role = \"client\"\n[engine]\nhost = \"engine\"\nconnect_timeout = \"soon\"", "connect_timeout"},
		{"project name with shell syntax", "role = \"engine\"\n[[project]]\nname = \"a;rm -rf x\"\npath = \"/srv/a\"", `name "a;rm -rf x" may only contain`},
		{"project name with a space", "role = \"engine\"\n[[project]]\nname = \"two words\"\npath = \"/srv/a\"", "may only contain"},
		{"project name starting with a dash", "role = \"engine\"\n[[project]]\nname = \"-rf\"\npath = \"/srv/a\"", "may only contain"},
		{"duplicate project", "role = \"engine\"\n[[project]]\nname = \"a\"\npath = \"/srv/a\"\n[[project]]\nname = \"a\"\npath = \"/srv/b\"", `project "a" is registered twice`},
		{"project without a path", "role = \"engine\"\n[[project]]\nname = \"a\"", `project "a" has no path`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, c.content)
			_, err := Load()
			if err == nil {
				t.Fatalf("accepted:\n%s", c.content)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name the problem (%q)", err, c.want)
			}
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("error %q does not name the file", err)
			}
		})
	}
}

// An empty role is the state of a machine not yet set up, which is reported, not refused.
func TestEmptyRoleLoads(t *testing.T) {
	writeConfig(t, "[agent]\ncommand = \"claude\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Role != "" {
		t.Fatalf("role %q", c.Role)
	}
}

func TestDefaultsApply(t *testing.T) {
	writeConfig(t, "role = \"client\"\n[engine]\nhost = \"engine\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Engine.ConnectTimeout.Duration != DefaultConnectTimeout || c.Agent.Command != DefaultAgentCommand || c.Waiting.Duration != DefaultWaitingLimit {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

// Setting a role must keep every other setting; losing the registered projects on a role
// change would be a silent partial write.
func TestSaveKeepsEverythingAndRoundTrips(t *testing.T) {
	writeConfig(t, "role = \"engine\"\nwaiting_signal_limit = \"2h\"\n[agent]\ncommand = \"my-agent --flag\"\n[[project]]\nname = \"project-a\"\npath = \"/srv/project-a\"\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c.Role = RoleClient
	c.Engine.Host = "engine.example"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if again.Role != RoleClient || again.Engine.Host != "engine.example" || again.Agent.Command != "my-agent --flag" ||
		len(again.Projects) != 1 || again.Projects[0].Path != "/srv/project-a" || again.Waiting.Duration != 2*time.Hour {
		t.Fatalf("round trip lost something: %+v", again)
	}
}

// Save refuses an invalid configuration rather than writing one Load would then refuse.
func TestSaveRefusesInvalid(t *testing.T) {
	path := writeConfig(t, "")
	c := &Config{Role: RoleClient}
	if err := c.Save(); err == nil {
		t.Fatal("saved a client with no engine")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused save still wrote the file")
	}
}
