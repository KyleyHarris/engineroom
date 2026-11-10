package source

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Cause is why the engine was not reached, read from what ssh said (connection obligations O10
// and dependency down: each cause is reported as itself).
type Cause int

const (
	// NoAnswer is an engine that did not answer: off, asleep, out of reach, or the network
	// between the two machines down. These look the same from the client, so they are not told
	// apart; a retry may find the engine.
	NoAnswer Cause = iota
	// NoName is an engine name that does not resolve to an address: the name is wrong, or the
	// network that names it (a private network's names) is down. A retry may find it.
	NoName
	// NoSSH is ssh missing from this machine. Nothing reaches the engine until it is installed,
	// so a retry is not offered.
	NoSSH
	// HostKeyChanged is an engine presenting a host key other than the one this machine knows.
	// ssh refuses to connect, and a retry would only be refused again; the developer checks the
	// new key before anything connects.
	HostKeyChanged
	// HostKeyUnknown is an engine whose host key this machine has never accepted. ssh is never
	// left to ask (BatchMode), so the developer connects once by hand to check and accept it.
	HostKeyUnknown
)

// Unreached is the engine not reached, with the cause and ssh's own words. It is ErrEngineDown,
// so every caller that asks "did the engine answer at all" still gets no; what is shown, and
// whether a retry is offered, follow from the cause.
type Unreached struct {
	Cause Cause
	// Host is the engine as this machine names it.
	Host string
	// Said is what ssh said, or why it could not be run.
	Said string
}

// Unwrap makes every cause ErrEngineDown.
func (u *Unreached) Unwrap() error { return ErrEngineDown }

// Error is the cause in words, with ssh's own, and what to do for a cause a retry cannot clear.
func (u *Unreached) Error() string {
	switch u.Cause {
	case NoName:
		return fmt.Sprintf("the engine's name %s does not resolve: %s", u.Host, u.Said)
	case NoSSH:
		return "ssh is not installed on this machine, so the engine cannot be reached; install it, then try again (" + u.Said + ")"
	case HostKeyChanged:
		return fmt.Sprintf("the engine %s presented a host key other than the one this machine knows, so ssh refused to connect. "+
			"If the engine was reinstalled or its key renewed, check its new key on the engine itself, then remove the old key "+
			"from the file and line ssh names below; until then nothing connects to it. ssh said: %s", u.Host, u.Said)
	case HostKeyUnknown:
		return fmt.Sprintf("this machine has never accepted the engine %s's host key, and the program never lets ssh ask. "+
			"Connect once by hand with: ssh %s, check the key it shows and accept it. ssh said: %s", u.Host, u.Host, u.Said)
	}
	return ErrEngineDown.Error() + ": " + u.Said
}

// Headline is the cause as the one line the engine-down screen leads with.
func (u *Unreached) Headline() string {
	switch u.Cause {
	case NoName:
		return "The engine's name " + u.Host + " does not resolve."
	case NoSSH:
		return "ssh is not installed on this machine."
	case HostKeyChanged:
		return "The engine " + u.Host + "'s host key has changed."
	case HostKeyUnknown:
		return "This machine does not know the engine " + u.Host + "'s host key."
	}
	return "The engine " + u.Host + " did not answer."
}

// Retryable reports whether asking the engine again could find it: only when it did not answer or
// its name did not resolve. An engine not reached for any other cause gets no retry, and an error
// that is not the engine being down is not retried either.
func Retryable(err error) bool {
	var u *Unreached
	if errors.As(err, &u) {
		return u.Cause == NoAnswer || u.Cause == NoName
	}
	return errors.Is(err, ErrEngineDown)
}

// SSHFailure reads why ssh to host failed: runErr is what running it returned, said what it wrote
// on its standard error. ssh writes a fixed sentence for each cause, which is what is matched.
func SSHFailure(host string, runErr error, said string) *Unreached {
	said = strings.TrimSpace(said)
	u := &Unreached{Host: host, Said: said}
	switch {
	case errors.Is(runErr, exec.ErrNotFound):
		u.Cause, u.Said = NoSSH, runErr.Error()
	case strings.Contains(said, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		u.Cause = HostKeyChanged
	case strings.Contains(said, "Host key verification failed"):
		u.Cause = HostKeyUnknown
	case strings.Contains(said, "Could not resolve hostname"):
		u.Cause = NoName
	default:
		u.Cause = NoAnswer
		if strings.Contains(said, "timed out") || strings.Contains(said, "No route to host") || strings.Contains(said, "Network is unreachable") || strings.Contains(said, "Connection refused") {
			u.Said = said + " (an engine that is off and a network between the machines that is down look the same from here)"
		}
	}
	return u
}

// SSHFailed reports whether running ssh ended in ssh's own failure rather than the engine's
// answer: ssh could not be run at all, or it exited 255, its status for a failure of its own.
func SSHFailed(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 255
	}
	return err != nil
}
