package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/source"
)

// The engine's name rides on the version check (CONN-ROLE-003). The engine is reached through a
// stand-in ssh on PATH, which stands for the network only: it answers as an engine of each kind
// would. Fails if an engine that names itself is not named, if an engine older than `api machine`
// is reported down or unversioned instead of falling back to `engineroom version` with no name,
// or if an engine that does not answer is read as an old one.
func TestTheEngineIsNamedByItsOwnMachineName(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	engine := func(script string) source.Remote {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
		return source.Remote{R: action.Remote{Host: "alias-not-a-name"}}
	}
	ctx := context.Background()

	r := engine(`case "$*" in *"api machine"*) echo '{"version":"abc1234","name":"Studio Mac"}';; *) exit 9;; esac` + "\n")
	if v, name, err := engineMachine(ctx, r); err != nil || v != "abc1234" || name != "Studio Mac" {
		t.Errorf("an engine that names itself: got %q, %q, %v", v, name, err)
	}

	r = engine(`case "$*" in *"api machine"*) echo 'usage: engineroom api ...' >&2; exit 2;; *"engineroom version"*) echo 'engineroom abc1234';; *) exit 9;; esac` + "\n")
	if v, name, err := engineMachine(ctx, r); err != nil || v != "abc1234" || name != "" {
		t.Errorf("an engine older than api machine: got %q, %q, %v", v, name, err)
	}

	r = engine("echo 'ssh: connect to host alias-not-a-name: Operation timed out' >&2; exit 255\n")
	if _, _, err := engineMachine(ctx, r); !errors.Is(err, source.ErrEngineDown) {
		t.Errorf("an engine that does not answer: got %v, want ErrEngineDown", err)
	}
}

// This machine's name is never empty on a machine with a host name, and never carries the domain
// a host name may have. Fails if machineName returns nothing or a dotted host name.
func TestThisMachineHasAName(t *testing.T) {
	name := machineName()
	if name == "" {
		t.Fatal("no name")
	}
	if host, _ := os.Hostname(); name == host && strings.Contains(host, ".") {
		t.Errorf("the name keeps the host name's domain: %q", name)
	}
}

// A client newer than its engine was told to update itself, because the banner always named the
// client. behindBuild decides which build is older from git's ancestry in a real clone. Fails if
// the direction is reversed, if two builds from different branches are called ordered, or if a
// commit the clone lacks (or a build with no commit) is taken as an answer rather than "unknown".
func TestTheOlderBuildIsTheOneToldToUpdate(t *testing.T) {
	clone := filepath.Join(t.TempDir(), "engineroom")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", clone}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=dev", "GIT_AUTHOR_EMAIL=dev@example.com",
			"GIT_COMMITTER_NAME=dev", "GIT_COMMITTER_EMAIL=dev@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "init", "-q", "-b", "main", clone).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// Each commit has its own message: two empty commits on the same parent in the same second
	// would otherwise be the same commit, and "different branches" would compare a build with itself.
	commit := func(message string) string {
		git("commit", "-q", "--allow-empty", "-m", message)
		return git("rev-parse", "--short", "HEAD")
	}
	older := commit("older")
	newer := commit("newer")
	git("switch", "-q", "-c", "other", older)
	sideways := commit("on another branch")

	cases := []struct{ name, here, engine, want string }{
		{"same commit", newer, newer, "none"},
		{"same commit, one with changes not committed", newer + "-dirty", newer, "none"},
		{"this machine is older", older, newer, "here"},
		{"the engine is older", newer, older, "engine"},
		{"the engine is older and this machine has changes", newer + "-dirty", older, "engine"},
		{"different branches", newer, sideways, "apart"},
		{"a commit this clone does not have", newer, "0123abc", "unknown"},
		{"a build from no commit", "dev", newer, "unknown"},
	}
	for _, c := range cases {
		if got := behindBuild(clone, c.here, c.engine); got != c.want {
			t.Errorf("%s: behindBuild(here %s, engine %s) = %q, want %q", c.name, c.here, c.engine, got, c.want)
		}
	}
	if got := behindBuild("", older, newer); got != "unknown" {
		t.Errorf("no clone recorded: got %q, want unknown", got)
	}
}
