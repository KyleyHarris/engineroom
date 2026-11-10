package sshtest

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// killTree ends the process pid and every process descended from it: the sshd listener, and the
// processes of each connection it accepted. A connection's processes start a session of their own
// (setsid), so they are found by their parents rather than by a process group; nothing is matched
// by name, so only this test's own server is reached.
func killTree(pid int) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output()
	children := map[int][]int{}
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) != 2 {
				continue
			}
			c, err1 := strconv.Atoi(f[0])
			p, err2 := strconv.Atoi(f[1])
			if err1 == nil && err2 == nil {
				children[p] = append(children[p], c)
			}
		}
	}
	var all []int
	var walk func(int)
	walk = func(p int) {
		all = append(all, p)
		for _, c := range children[p] {
			walk(c)
		}
	}
	walk(pid)
	for _, p := range all {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
}
