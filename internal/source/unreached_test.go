package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/sshtest"
)

// unreachableAddress is reserved for documentation (RFC 5737): nothing answers on it, so ssh to it
// times out as an engine that is off, or a network that is down, does.
const unreachableAddress = "192.0.2.1"

// emptySSHConfig is an ssh configuration of the test's own, so the developer's is never read.
func emptySSHConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// cause checks the engine for real and returns why it was not reached.
func cause(t *testing.T, r action.Remote) *Unreached {
	t.Helper()
	err := Remote{R: r}.Probe(context.Background())
	var u *Unreached
	if !errors.As(err, &u) || !errors.Is(err, ErrEngineDown) {
		t.Fatalf("the check of %s gave %v, want an engine not reached", r.Host, err)
	}
	return u
}

// Connection obligation O10: a changed host key is reported as a changed host key, and is not
// offered as a retry; a host key this machine has never accepted is said as that, also with no
// retry. Proved against a real sshd that the client trusts, restarted with another host key.
// Fails if either is read as an engine that did not answer, offered a retry, or stops counting as
// the engine not reached (callers that pass on the engine's answer would then pass on nothing).
func TestAChangedHostKeyIsReportedAsItself(t *testing.T) {
	s := sshtest.Start(t)
	r := action.Remote{Host: sshtest.Host, SSHConfig: s.SSHConfig, ConnectTimeout: 5 * time.Second}
	if err := (Remote{R: r}).Probe(context.Background()); err != nil {
		t.Fatalf("the engine with the key this machine knows: %v\n%s", err, s.Log())
	}

	s.ChangeHostKey()
	u := cause(t, r)
	if u.Cause != HostKeyChanged || Retryable(u) {
		t.Fatalf("a changed host key: cause %v, retry %v: %v", u.Cause, Retryable(u), u)
	}
	if !strings.Contains(u.Headline(), "host key has changed") || !strings.Contains(u.Error(), "check its new key") || !strings.Contains(u.Error(), s.KnownHosts) {
		t.Fatalf("a changed host key is not said with what to do and where the old key is:\n%s\n%s", u.Headline(), u)
	}
	// A read of the engine's own answer is not reached in the same way, not mistaken for an answer.
	if _, _, err := (Remote{R: r}).Call(context.Background(), "version"); !errors.Is(err, ErrEngineDown) || Retryable(err) {
		t.Fatalf("a read with a changed host key: %v (retry %v)", err, Retryable(err))
	}

	if err := os.WriteFile(s.KnownHosts, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if u := cause(t, r); u.Cause != HostKeyUnknown || Retryable(u) || !strings.Contains(u.Error(), "ssh "+sshtest.Host) {
		t.Fatalf("an unknown host key: cause %v, retry %v: %v", u.Cause, Retryable(u), u)
	}
}

// Connection obligation, dependency down: ssh not installed, a name that does not resolve and an
// engine (or network) that does not answer are each reported as themselves, and a retry is offered
// only where asking again could find the engine. Each is real: a PATH with no ssh, a name in the
// reserved .invalid domain, an address nothing answers on. Fails if a cause is read as another,
// loses ssh's own words, or a missing ssh is offered a retry.
func TestEachDependencyDownIsReportedAsItself(t *testing.T) {
	config := emptySSHConfig(t)

	noAnswer := cause(t, action.Remote{Host: unreachableAddress, SSHConfig: config, ConnectTimeout: time.Second})
	if noAnswer.Cause != NoAnswer || !Retryable(noAnswer) || !strings.Contains(noAnswer.Headline(), "did not answer") ||
		!strings.Contains(noAnswer.Error(), "timed out") || !strings.Contains(noAnswer.Error(), "network between the machines that is down") {
		t.Fatalf("no answer: cause %v, retry %v: %s / %v", noAnswer.Cause, Retryable(noAnswer), noAnswer.Headline(), noAnswer)
	}

	noName := cause(t, action.Remote{Host: "engine.invalid", SSHConfig: config, ConnectTimeout: time.Second})
	if noName.Cause != NoName || !Retryable(noName) || !strings.Contains(noName.Headline(), "engine.invalid does not resolve") ||
		!strings.Contains(noName.Error(), "Could not resolve hostname") {
		t.Fatalf("a name that does not resolve: cause %v, retry %v: %s / %v", noName.Cause, Retryable(noName), noName.Headline(), noName)
	}

	t.Setenv("PATH", t.TempDir())
	noSSH := cause(t, action.Remote{Host: unreachableAddress, SSHConfig: config, ConnectTimeout: time.Second})
	if noSSH.Cause != NoSSH || Retryable(noSSH) || !strings.Contains(noSSH.Headline(), "ssh is not installed") || !strings.Contains(noSSH.Error(), "executable file not found") {
		t.Fatalf("no ssh: cause %v, retry %v: %s / %v", noSSH.Cause, Retryable(noSSH), noSSH.Headline(), noSSH)
	}
	if _, _, err := (Remote{R: action.Remote{Host: unreachableAddress}}).Call(context.Background(), "version"); !errors.Is(err, ErrEngineDown) || Retryable(err) {
		t.Fatalf("a read with no ssh is not the engine not reached: %v", err)
	}
}
