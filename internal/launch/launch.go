// Package launch starts macOS applications as the developer would start them from the Dock or
// Finder, whatever ran the program (docs/decisions.md D-030).
//
// macOS hands an application the whole environment of whatever asked for it to be started:
// open, osascript's tell or launch, and NSWorkspace all pass the caller's variables on. An
// application started from a shell, a Claude Code session, an SSH login or a test would keep that
// caller's home, configuration folders, Zellij variables and session markers for as long as it
// runs, and so would everything it opens afterwards. Started with an empty environment instead,
// macOS gives it the developer's own session environment (home, user, shell, temporary folder,
// SSH agent and anything set with launchctl setenv), the same as one opened by hand.
package launch

import "os/exec"

// Env is the command that empties the environment for what follows it. Named by its full path,
// where every Mac has it, since it is what clears PATH.
const Env = "/usr/bin/env"

// Clean is argv run with an empty environment, so an application it starts gets the developer's
// own session environment rather than this program's. The command is found on this program's
// PATH first, because the empty environment has none to find it on: a stand-in earlier on PATH
// (as in the journeys) is then still the one run.
func Clean(argv ...string) []string {
	name := argv[0]
	if p, err := exec.LookPath(name); err == nil {
		name = p
	}
	return append([]string{Env, "-i", name}, argv[1:]...)
}
