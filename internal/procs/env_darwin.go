package procs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// Env reads a process's environment from the kernel (kern.procargs2): its argument count, the
// path it was started from, its arguments, then its environment, each ending in a NUL. Only
// this user's processes can be read.
func Env(pid int) (map[string]string, error) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, errors.New("short procargs")
	}
	argc := int(binary.LittleEndian.Uint32(buf[:4]))
	rest := buf[4:]
	// The executable path, then the NULs that pad it.
	i := bytes.IndexByte(rest, 0)
	if i < 0 {
		return nil, errors.New("no executable path")
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for n := 0; n < argc && len(rest) > 0; n++ {
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			return map[string]string{}, nil
		}
		rest = rest[j+1:]
	}
	env := map[string]string{}
	for len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j <= 0 {
			break
		}
		kv := string(rest[:j])
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
		rest = rest[j+1:]
	}
	return env, nil
}
