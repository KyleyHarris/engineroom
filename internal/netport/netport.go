// Package netport says whether this machine can listen on a port, and what holds it when not.
package netport

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Taken says why this machine cannot listen on a port, or "" when it can. A server or
// a forward may bind either loopback address, so the port is tried on each; only a refusal that means
// "taken" or "not allowed" counts, so a machine without IPv6 is not read as every port taken.
func Taken(port int) string {
	for _, addr := range []string{"127.0.0.1", "::1"} {
		l, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err == nil {
			_ = l.Close()
			continue
		}
		switch {
		case errors.Is(err, syscall.EADDRINUSE):
			return "already in use on this machine by " + listener(port)
		case errors.Is(err, syscall.EACCES):
			return "below 1024, and this machine allows only an administrator to listen there"
		}
	}
	return ""
}

// Answers reports whether something accepts a connection on a port of this machine, on either
// loopback address, within a moment: a server is running there. It asks by connecting, not by
// trying to listen, since a server listening on every address leaves the loopback port free to
// bind on some systems.
func Answers(port int) bool {
	for _, addr := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(addr, strconv.Itoa(port)), 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// listener names what is listening on a port on this machine, read with lsof, so the developer
// knows what to stop. Without lsof, or with no answer, it says only that something is.
func listener(port int) string {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return "another program"
	}
	var pid, name string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p") && pid == "":
			pid = line[1:]
		case strings.HasPrefix(line, "c") && name == "":
			name = line[1:]
		}
	}
	if name == "" {
		return "another program"
	}
	return fmt.Sprintf("%s (pid %s)", name, pid)
}
