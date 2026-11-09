// Package procs finds the developer's own processes and what they were started for, read from
// each process's environment, so engineroom can stop exactly what ran for a session once the session
// has ended (docs/decisions.md D-020). A process is only ever matched by the session its
// environment names, never by its name or command line.
package procs

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proc is one running process of this user.
type Proc struct {
	PID, PPID int
	Started   string
	Command   string
	Env       map[string]string
	Ports     []int
}

// Owner is the session a process was started for: ENGINEROOM_SESSION, which engineroom sets on everything it
// runs for a checkout, wins; else the session of the checkout engineroom ran it for (ENGINEROOM_WORKTREE,
// what engineroom set before ENGINEROOM_SESSION existed), through sessionOf; else ZELLIJ_SESSION_NAME, which
// Zellij sets on everything started inside a session. Empty when the process names none.
func (p Proc) Owner(sessionOf func(worktree string) string) string {
	if s := p.Env["ENGINEROOM_SESSION"]; s != "" {
		return s
	}
	if w := p.Env["ENGINEROOM_WORKTREE"]; w != "" && sessionOf != nil {
		if s := sessionOf(w); s != "" {
			return s
		}
	}
	return p.Env["ZELLIJ_SESSION_NAME"]
}

// Describe is the process as a person reads it: pid, what it runs, and the ports it listens on.
func (p Proc) Describe() string {
	cmd := p.Command
	if len(cmd) > 70 {
		cmd = cmd[:67] + "..."
	}
	s := fmt.Sprintf("pid %d (%s)", p.PID, cmd)
	if len(p.Ports) > 0 {
		var ports []string
		for _, n := range p.Ports {
			ports = append(ports, strconv.Itoa(n))
		}
		s += " on " + strings.Join(ports, ", ")
	}
	return s
}

// List is every process of this user with its environment and listening ports. A process whose
// environment cannot be read (it ended meanwhile) is left out.
func List() ([]Proc, error) {
	out, err := exec.Command("ps", "-x", "-o", "pid=,ppid=,lstart=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}
	ports := listening()
	var all []Proc
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// pid ppid, then lstart's five fields (Sat Nov  7 14:28:07 2026), then the command.
		if len(f) < 8 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		env, err := Env(pid)
		if err != nil {
			continue
		}
		all = append(all, Proc{PID: pid, PPID: ppid, Started: strings.Join(f[2:7], " "), Command: strings.Join(f[7:], " "), Env: env, Ports: ports[pid]})
	}
	return all, nil
}

// listening maps each pid to the TCP ports it listens on, from lsof; empty when lsof fails.
func listening() map[int][]int {
	out, _ := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpn").Output()
	m := map[int][]int{}
	pid := 0
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid != 0:
			if i := strings.LastIndex(line, ":"); i >= 0 {
				if n, err := strconv.Atoi(line[i+1:]); err == nil && !contains(m[pid], n) {
					m[pid] = append(m[pid], n)
				}
			}
		}
	}
	for _, v := range m {
		sort.Ints(v)
	}
	return m
}

func contains(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

// Ancestors is this process and every process above it, which a stop must never reach: engineroom
// itself may be running inside the session it is stopping what was started for.
func Ancestors(all []Proc) map[int]bool {
	parent := map[int]int{}
	for _, p := range all {
		parent[p.PID] = p.PPID
	}
	keep := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !keep[pid]; pid = parent[pid] {
		keep[pid] = true
		if _, ok := parent[pid]; !ok {
			break
		}
	}
	return keep
}

// Stop ends each process: SIGTERM, then SIGKILL for any still running after grace. It returns
// those still running after that, which is none unless the system refused.
func Stop(ps []Proc, grace time.Duration) []Proc {
	for _, p := range ps {
		_ = syscall.Kill(p.PID, syscall.SIGTERM)
	}
	deadline := time.Now().Add(grace)
	alive := ps
	for len(alive) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		alive = running(alive)
	}
	for _, p := range alive {
		_ = syscall.Kill(p.PID, syscall.SIGKILL)
	}
	time.Sleep(200 * time.Millisecond)
	return running(alive)
}

func running(ps []Proc) []Proc {
	var out []Proc
	for _, p := range ps {
		if syscall.Kill(p.PID, 0) == nil {
			out = append(out, p)
		}
	}
	return out
}

// Cwd is the folder a process works in, from lsof; empty when it cannot be read.
func Cwd(pid int) string {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:]
		}
	}
	return ""
}

// ListensOn reports whether the process pid listens on the TCP port now: checked just before a
// process is stopped by its number, so a pid reused since it was shown is never stopped.
func ListensOn(pid, port int) bool {
	return exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN").Run() == nil
}
