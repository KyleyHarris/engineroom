package journey

import (
	"os"
	"path/filepath"
	"strings"
)

// NewRepoWithRemote creates a bare repository standing in for the project's remote, with one
// commit on main, and clones it under src/<name>. It returns the clone's path and the
// remote's. Everything is inside this machine's temporary home.
func (m *Machine) NewRepoWithRemote(name string) (string, string) {
	m.t.Helper()
	remote := filepath.Join(m.Home, "remotes", name+".git")
	seed := filepath.Join(m.Home, "seed-"+name)
	clone := filepath.Join(m.Home, "src", name)
	m.Git(m.Home, "init", "-q", "--bare", "-b", "main", remote)
	m.Git(m.Home, "init", "-q", "-b", "main", seed)
	m.WriteFile(filepath.Join(seed, "README.md"), "readme\n", 0o644)
	m.Git(seed, "add", ".")
	m.Git(seed, "commit", "-qm", "init")
	m.Git(seed, "remote", "add", "origin", remote)
	m.Publish(seed, "main")
	m.Git(m.Home, "clone", "-q", remote, clone)
	return clone, remote
}

// Publish sends a branch from a repository on this machine to its remote, which in a journey
// is always a bare repository inside the machine's temporary home.
func (m *Machine) Publish(dir, branch string) {
	m.t.Helper()
	m.Git(dir, "push", "-q", "origin", branch)
}

// Git runs git in dir on this machine and returns its trimmed output, failing the test on
// error.
func (m *Machine) Git(dir string, args ...string) string {
	m.t.Helper()
	cmd := m.Command("git", append([]string{"-C", dir, "-c", "user.email=dev@engine.example", "-c", "user.name=dev"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		m.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// WriteFile writes a file on this machine, creating its folder, with the given mode.
func (m *Machine) WriteFile(path, content string, mode os.FileMode) {
	m.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		m.t.Fatal(err)
	}
}
