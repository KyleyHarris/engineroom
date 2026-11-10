// Package sshtest starts a real ssh server for a test, so the connection obligations are proved
// against real ssh rather than a stand-in (acceptance/connection/connection-obligations.md).
//
// The server is this machine's own sshd, run by the test's own user on 127.0.0.1 with a port, a
// host key, an authorized key and a known-hosts file made for the test alone. The client side is
// an ssh configuration naming it "engine", with strict host key checking, so the developer's own
// keys, hosts and configuration are never read or written.
//
// sshd missing fails the test. It is never skipped: a skipped test would read as a pass for
// behaviour nobody ran.
package sshtest

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SSHD is where sshd is looked for. It must be named by its full path: sshd re-runs itself by the
// path it was started with.
var SSHD = "/usr/sbin/sshd"

// Host is the name the server is reached by, through SSHConfig.
const Host = "engine"

// Server is a running ssh server for one test.
type Server struct {
	t   testing.TB
	dir string
	// Port is the port it listens on, on 127.0.0.1.
	Port int
	// SSHConfig is the client's ssh configuration naming the server Host.
	SSHConfig string
	// KnownHosts is the client's known-hosts file, holding the server's first host key.
	KnownHosts string
	cmd        *exec.Cmd
	hostKey    string
}

// Start starts a server and stops it when the test ends. The client trusts the host key the server
// starts with; ChangeHostKey restarts it with another.
func Start(t testing.TB) *Server {
	t.Helper()
	if _, err := os.Stat(SSHD); err != nil {
		t.Fatalf("sshd is needed for the connection tests and is not at %s: %v", SSHD, err)
	}
	dir := t.TempDir()
	s := &Server{t: t, dir: dir, SSHConfig: filepath.Join(dir, "ssh_config"), KnownHosts: filepath.Join(dir, "known_hosts")}
	s.hostKey = s.keygen("host_key")
	client := s.keygen("client_key")
	pub, err := os.ReadFile(client + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	s.write("authorized_keys", string(pub))
	s.Port = freePort(t)
	hostPub, err := os.ReadFile(s.hostKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	// Written directly rather than learnt on first connection, so checking is strict from the first
	// connection on, as it is for an engine the developer has connected to before.
	s.write("known_hosts", fmt.Sprintf("[127.0.0.1]:%d %s", s.Port, hostPub))
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	s.write("ssh_config", fmt.Sprintf(`Host %s
  HostName 127.0.0.1
  Port %d
  User %s
  IdentityFile %s
  IdentitiesOnly yes
  UserKnownHostsFile %s
  GlobalKnownHostsFile /dev/null
  StrictHostKeyChecking yes
  LogLevel ERROR
`, Host, s.Port, me.Username, client, s.KnownHosts))
	s.start()
	t.Cleanup(s.Stop)
	return s
}

// ChangeHostKey restarts the server with a host key the client has never seen, as an engine that
// was reinstalled, or a machine standing in its place, presents.
func (s *Server) ChangeHostKey() {
	s.t.Helper()
	s.Stop()
	s.hostKey = s.keygen("other_host_key")
	s.start()
}

// Stop stops the server and every connection it holds, so a client's open connection drops as it
// does when the engine goes away. It is safe to call more than once.
func (s *Server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	killTree(s.cmd.Process.Pid)
	_ = s.cmd.Wait()
	s.cmd = nil
}

// start runs sshd in the foreground (-D), so it is this test's own child, and waits until it
// accepts connections.
func (s *Server) start() {
	s.t.Helper()
	config := s.write("sshd_config", fmt.Sprintf(`Port %d
ListenAddress 127.0.0.1
HostKey %s
AuthorizedKeysFile %s
PidFile %s
UsePAM no
StrictModes no
PasswordAuthentication no
KbdInteractiveAuthentication no
`, s.Port, s.hostKey, filepath.Join(s.dir, "authorized_keys"), filepath.Join(s.dir, "sshd.pid")))
	s.cmd = exec.Command(SSHD, "-D", "-e", "-f", config)
	log, err := os.Create(filepath.Join(s.dir, "sshd.log"))
	if err != nil {
		s.t.Fatal(err)
	}
	s.cmd.Stdout, s.cmd.Stderr = log, log
	if err := s.cmd.Start(); err != nil {
		s.t.Fatalf("starting sshd: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(filepath.Join(s.dir, "sshd.log"))
			s.t.Fatalf("sshd never listened on %d: %v\n%s", s.Port, err, b)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Log is what sshd has said, for a failing test's message.
func (s *Server) Log() string {
	b, _ := os.ReadFile(filepath.Join(s.dir, "sshd.log"))
	return strings.TrimSpace(string(b))
}

func (s *Server) keygen(name string) string {
	s.t.Helper()
	path := filepath.Join(s.dir, name)
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).CombinedOutput(); err != nil {
		s.t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	return path
}

func (s *Server) write(name, content string) string {
	s.t.Helper()
	path := filepath.Join(s.dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		s.t.Fatal(err)
	}
	return path
}

// freePort is a port nothing listens on, found by listening on port 0 and letting it go.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
