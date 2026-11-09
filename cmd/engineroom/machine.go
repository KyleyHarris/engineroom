package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// machineName is the name this machine gives itself, so a client can name its engine as the
// machine is called rather than by the SSH name it reaches it with (CONN-ROLE-003). On macOS it is
// the computer's
// name from System Settings, Sharing; elsewhere, or when that cannot be read, the host name up to
// its first dot. Empty only when neither can be read.
func machineName() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("scutil", "--get", "ComputerName").Output(); err == nil {
			if name := strings.TrimSpace(string(out)); name != "" {
				return name
			}
		}
	}
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.SplitN(host, ".", 2)[0]
}
