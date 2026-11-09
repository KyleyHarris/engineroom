package settings

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// run runs a plan for real and fails the test, with each step's output, when a step fails.
func run(t *testing.T, p action.Plan) {
	t.Helper()
	var failed []string
	ok := action.Run(context.Background(), p, func(s action.Step) {
		if s.Status == action.StatusFailed {
			failed = append(failed, p.Commands[s.Index].Display()+": "+s.Output)
		}
	})
	if !ok {
		t.Fatalf("%s failed: %s", p.Title, strings.Join(failed, "\n"))
	}
}

func gitOut(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func needCrypt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git-crypt"); err != nil {
		t.Skip("git-crypt is not installed")
	}
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// The whole round trip, for real: init commits the settings and pushes them, with a secret
// encrypted in what was pushed and the settings plain; a clone with the key reads the secret, one
// without it sees only encrypted bytes; a change made on one machine reaches the other by sync;
// and running init again only does what is missing. It fails if a secret ever reached the remote
// unencrypted, a clone with the key could not read it, or a sync lost a change.
func TestTheSettingsRepositoryRoundTrip(t *testing.T) {
	needCrypt(t)
	remote := filepath.Join(t.TempDir(), "settings.git")
	gitOut(t, ".", "init", "-q", "--bare", "-b", "main", remote)

	a := t.TempDir()
	write(t, filepath.Join(a, "projects", "owner", "app", "engineroom.toml"), "[ports]\nui = 4100\n", 0o644)
	write(t, filepath.Join(a, "projects", "owner", "app", "secrets", "local.json"), `{"token":"not-for-github"}`, 0o600)

	run(t, InitPlan(State(a), remote))
	st := State(a)
	if !st.IsRepo || !st.Crypt || !st.Attributes || st.Remote != remote || len(st.Changed) != 0 || len(st.Unprotected) != 0 {
		t.Fatalf("after init: %+v", st)
	}
	pushed := gitOut(t, remote, "show", "main:projects/owner/app/secrets/local.json")
	if !bytes.HasPrefix(pushed, []byte("\x00GITCRYPT")) || bytes.Contains(pushed, []byte("not-for-github")) {
		t.Fatalf("the secret reached the repository unencrypted: %q", pushed)
	}
	if !strings.Contains(string(gitOut(t, remote, "show", "main:projects/owner/app/engineroom.toml")), "ui = 4100") {
		t.Fatal("the settings should be plain in the repository")
	}

	// Running init again does only what is missing: no second key, no second repository.
	for _, c := range InitPlan(State(a), remote).Commands {
		if strings.Contains(strings.Join(c.Argv, " "), "init") {
			t.Fatalf("init again would run %v", c.Argv)
		}
	}

	key := filepath.Join(t.TempDir(), "key")
	if out, err := exec.Command("git-crypt", "export-key", key).CombinedOutput(); err == nil {
		t.Fatalf("export-key outside the repository should fail: %s", out)
	}
	cmd := exec.Command("git-crypt", "export-key", key)
	cmd.Dir = a
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("export-key: %v %s", err, out)
	}

	b := filepath.Join(t.TempDir(), "b")
	run(t, ClonePlan(remote, key, b))
	if got, _ := os.ReadFile(filepath.Join(b, "projects", "owner", "app", "secrets", "local.json")); !strings.Contains(string(got), "not-for-github") {
		t.Fatalf("a clone with the key cannot read the secret: %q", got)
	}
	locked := filepath.Join(t.TempDir(), "locked")
	run(t, ClonePlan(remote, "", locked))
	if got, _ := os.ReadFile(filepath.Join(locked, "projects", "owner", "app", "secrets", "local.json")); strings.Contains(string(got), "not-for-github") {
		t.Fatal("a clone without the key read the secret")
	}

	write(t, filepath.Join(b, "projects", "owner", "app", "engineroom.toml"), "[ports]\nui = 4200\n", 0o644)
	sync, err := SyncPlan(State(b), "machine-b")
	if err != nil {
		t.Fatal(err)
	}
	run(t, sync)
	sync, err = SyncPlan(State(a), "machine-a")
	if err != nil {
		t.Fatal(err)
	}
	run(t, sync)
	if got, _ := os.ReadFile(filepath.Join(a, "projects", "owner", "app", "engineroom.toml")); !strings.Contains(string(got), "4200") {
		t.Fatalf("machine b's change did not reach machine a: %q", got)
	}
}

// A sync refuses before anything runs when a secret would be committed without encryption, or
// when there is no repository with a remote to sync with. It fails if a secret could be pushed in
// the clear because git-crypt was never set up in this folder.
func TestASyncThatWouldLeakASecretIsRefused(t *testing.T) {
	needCrypt(t)
	root := t.TempDir()
	if _, err := SyncPlan(State(root), "m"); err == nil || !strings.Contains(err.Error(), "engineroom settings init") {
		t.Fatalf("a folder that is not a repository was synced: %v", err)
	}
	gitOut(t, root, "init", "-q", "-b", "main")
	gitOut(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "x.git"))
	write(t, filepath.Join(root, "projects", "app", "secrets", "token"), "plain", 0o600)
	write(t, filepath.Join(root, "projects", "app", "engineroom.toml"), "", 0o644)
	st := State(root)
	if len(st.Unprotected) != 1 || st.Unprotected[0] != "projects/app/secrets/token" {
		t.Fatalf("unprotected %v", st.Unprotected)
	}
	if _, err := SyncPlan(st, "m"); err == nil || !strings.Contains(err.Error(), "projects/app/secrets/token") {
		t.Fatalf("a sync that would commit a secret in the clear was planned: %v", err)
	}
}
