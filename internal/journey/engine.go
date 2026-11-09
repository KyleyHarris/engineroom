package journey

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// EngineImage is the throwaway engine's image, built from testenv/engine.
const EngineImage = "engineroom-testengine:dev"

// UnreachableAddress is a real address reserved for documentation (RFC 5737). Nothing answers
// on it, so a connection attempt to it times out exactly as an engine that is off or out of reach
// would, with no stub in between.
const UnreachableAddress = "192.0.2.1"

// Engine is a throwaway engine: a container running sshd, git and Zellij, holding the program built
// for Linux. Journeys reach it over real SSH, as a client would reach a real engine.
type Engine struct {
	t         testing.TB
	Container string
	Port      string
	Key       string
	SSHConfig string
}

var (
	linuxOnce sync.Once
	linuxBin  string
	linuxErr  error
)

// linuxBinary builds the program for the container's architecture, once per run.
func linuxBinary(t testing.TB) string {
	t.Helper()
	linuxOnce.Do(func() {
		arch, err := exec.Command("docker", "version", "--format", "{{.Server.Arch}}").Output()
		if err != nil {
			linuxErr = fmt.Errorf("docker is needed for the engine journeys and did not answer: %v", err)
			return
		}
		dir, err := os.MkdirTemp("", "engineroom-linux-")
		if err != nil {
			linuxErr = err
			return
		}
		linuxBin = filepath.Join(dir, "engineroom")
		if out, err := goBuild(linuxBin, "linux", strings.TrimSpace(string(arch))); err != nil {
			linuxErr = fmt.Errorf("building engineroom for linux: %v\n%s", err, out)
		}
	})
	if linuxErr != nil {
		t.Fatal(linuxErr)
	}
	return linuxBin
}

// NewEngine starts a throwaway engine for one test and removes it when the test ends. The client
// machine gets an SSH configuration naming it "engine", with a key generated for this run and a
// known-hosts file of its own, so the developer's own keys and hosts are never consulted.
//
// Docker being unavailable fails the journey. It is never skipped: a skipped journey would
// read as a pass for behaviour nobody ran.
func NewEngine(t testing.TB, client *Machine) *Engine {
	t.Helper()
	bin := linuxBinary(t)

	keyDir := filepath.Join(client.Home, ".ssh")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(keyDir, "engine_key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("docker", "run", "-d", "--rm", "-P", "-e", "AUTHORIZED_KEY="+strings.TrimSpace(string(pub)), EngineImage).CombinedOutput()
	if err != nil {
		t.Fatalf("starting the engine container (build it with: docker build -t %s testenv/engine): %v\n%s", EngineImage, err, out)
	}
	h := &Engine{t: t, Container: strings.TrimSpace(string(out)), Key: key}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", h.Container).Run() })

	portOut, err := exec.Command("docker", "port", h.Container, "22/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	// "0.0.0.0:49153" (and possibly an IPv6 line); the port is after the last colon.
	first := strings.SplitN(strings.TrimSpace(string(portOut)), "\n", 2)[0]
	h.Port = first[strings.LastIndex(first, ":")+1:]

	h.SSHConfig = filepath.Join(keyDir, "config")
	client.write(h.SSHConfig, fmt.Sprintf(`Host engine
  HostName 127.0.0.1
  Port %s
  User dev
  IdentityFile %s
  IdentitiesOnly yes
  UserKnownHostsFile %s
  StrictHostKeyChecking accept-new
  LogLevel ERROR

Host unreachable-engine
  HostName %s
  User dev
  IdentityFile %s
  IdentitiesOnly yes
  UserKnownHostsFile %s
  LogLevel ERROR
`, h.Port, key, filepath.Join(keyDir, "known_hosts"), UnreachableAddress, key, filepath.Join(keyDir, "known_hosts")))

	h.copyIn(bin, "/home/dev/bin/engineroom", "0755")
	h.writeFile("/home/dev/bin/fake-agent", FakeAgent, "0755")
	h.writeFile("/home/dev/.config/zellij/config.kdl", ZellijConfig, "0644")
	h.waitForSSH(client)
	return h
}

// Exec runs a shell command inside the engine as the developer account, to set up or check state
// directly rather than through the program under test.
func (h *Engine) Exec(script string) (string, error) {
	out, err := exec.Command("docker", "exec", "-u", "dev", "-w", "/home/dev",
		"-e", "HOME=/home/dev", "-e", "PATH=/home/dev/bin:/usr/local/bin:/usr/bin:/bin",
		"-e", "FAKE_AGENT_LOG=/home/dev/agent.log",
		h.Container, "sh", "-c", script).CombinedOutput()
	return string(out), err
}

// MustExec is Exec that fails the test on error.
func (h *Engine) MustExec(script string) string {
	h.t.Helper()
	out, err := h.Exec(script)
	if err != nil {
		h.t.Fatalf("engine: %s: %v\n%s", script, err, out)
	}
	return out
}

// Sessions lists the engine's Zellij sessions, read from Zellij on the engine.
func (h *Engine) Sessions() string {
	out, _ := h.Exec("zellij list-sessions --no-formatting 2>&1")
	return out
}

// WriteConfig writes the program's configuration on the engine.
func (h *Engine) WriteConfig(toml string) {
	h.t.Helper()
	h.writeFile("/home/dev/.config/engineroom/config.toml", toml, "0644")
}

// AgentStarts is every start of the agent stand-in on the engine, one line each:
// "start <dir> <args>".
func (h *Engine) AgentStarts() []string {
	out, _ := h.Exec("cat /home/dev/agent.log 2>/dev/null")
	if strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

// NewRepo creates a git repository on the engine at path, with one commit.
func (h *Engine) NewRepo(path string) {
	h.t.Helper()
	h.MustExec(fmt.Sprintf(`mkdir -p %[1]s && cd %[1]s && git init -q -b main && echo readme > README.md && git add . && git -c user.email=dev@engine.example -c user.name=dev commit -qm init`, path))
}

func (h *Engine) writeFile(path, content, mode string) {
	h.t.Helper()
	tmp := filepath.Join(h.t.TempDir(), "f")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.copyIn(tmp, path, mode)
}

func (h *Engine) copyIn(src, dst, mode string) {
	h.t.Helper()
	h.MustExec("mkdir -p " + filepath.Dir(dst))
	if out, err := exec.Command("docker", "cp", src, h.Container+":"+dst).CombinedOutput(); err != nil {
		h.t.Fatalf("docker cp %s: %v\n%s", dst, err, out)
	}
	if out, err := exec.Command("docker", "exec", "-u", "root", h.Container, "sh", "-c",
		fmt.Sprintf("chown dev:dev %s && chmod %s %s", dst, mode, dst)).CombinedOutput(); err != nil {
		h.t.Fatalf("chmod %s: %v\n%s", dst, err, out)
	}
}

// waitForSSH waits, with a deadline, until the engine accepts the client's key.
func (h *Engine) waitForSSH(client *Machine) {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		out, err := client.Run("ssh", "-F", h.SSHConfig, "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", "engine", "true")
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("the engine container never accepted SSH: %v\n%s", err, out)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// BeEngine configures the engine with the given projects (name, path pairs), using the agent
// stand-in, and creates each project's repository there.
func (h *Engine) BeEngine(projects ...string) {
	h.t.Helper()
	var b strings.Builder
	b.WriteString("role = \"engine\"\n[agent]\ncommand = \"fake-agent\"\n")
	for i := 0; i+1 < len(projects); i += 2 {
		fmt.Fprintf(&b, "[[project]]\nname = %q\npath = %q\n", projects[i], projects[i+1])
		h.NewRepo(projects[i+1])
	}
	h.WriteConfig(b.String())
}

// StartSession starts a session on the engine directly with Zellij, from the standard layout
// with the agent running, to set up a journey's starting state.
func (h *Engine) StartSession(name, dir string) {
	h.t.Helper()
	h.MustExec(fmt.Sprintf(`engineroom layout write --out /home/dev/layout.kdl >/dev/null && cd %s && zellij attach --create-background %s options --default-layout /home/dev/layout.kdl --default-cwd %s`, dir, name, dir))
	deadline := time.Now().Add(15 * time.Second)
	for SessionState(h.Sessions(), name) != "running" {
		if time.Now().After(deadline) {
			h.t.Fatalf("engine session %s never started:\n%s", name, h.Sessions())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// BeClientOf configures this machine as a client of the engine, through the run's own SSH
// configuration, with a short connection timeout so an unreachable engine is found quickly.
func (m *Machine) BeClientOf(h *Engine, host string) {
	m.t.Helper()
	m.WriteConfig(fmt.Sprintf("role = \"client\"\n%s[engine]\nhost = %q\nssh_config = %q\nconnect_timeout = \"2s\"\n", editorSetting, host, h.SSHConfig))
}

// SetReachable makes the engine answer or not, as the client sees it, by pointing the client's
// "engine" entry at the container or at an address that does not answer. Nothing is stubbed: an
// unreachable engine is a real connection attempt that times out.
func (h *Engine) SetReachable(client *Machine, reachable bool) {
	h.t.Helper()
	b, err := os.ReadFile(h.SSHConfig)
	if err != nil {
		h.t.Fatal(err)
	}
	from, to := "HostName "+UnreachableAddress+"\n  Port "+h.Port, "HostName 127.0.0.1\n  Port "+h.Port
	if !reachable {
		from, to = to, from
	}
	text := strings.Replace(string(b), from, to, 1)
	client.write(h.SSHConfig, text)
}
