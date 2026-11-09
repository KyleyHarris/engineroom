// Package upgrade upgrades engineroom on one machine through a Claude session, so a step that
// fails is diagnosed and corrected rather than only reported. The app's Upgrade button starts
// the session, in the clone the running build was made from, with a brief that says the steps
// (pull, install, check the version, restart the app, say what changed) and what the session may
// fix by itself and what it must ask about first.
package upgrade

import (
	_ "embed"
	"fmt"
	"strings"
)

// SessionName is the upgrade session's name: one per machine. Pressing Upgrade again ends the
// one left from before and starts a new one (APP-UPGRADE-003).
const SessionName = "engineroom-upgrade"

// BundleID is the menu bar app's bundle identifier, which `engineroom app relaunch` quits and
// opens by.
const BundleID = "local.engineroom.menu"

//go:embed brief.md
var brief string

// Facts is what the brief is told about this machine.
type Facts struct {
	// Machine is the machine's own name and Role its role ("engine" or "client").
	Machine, Role string
	// Version is the running build, and Clone the folder it was built from (empty when the build
	// recorded none, as one not made by scripts/install.sh).
	Version, Clone string
	// App is the menu bar app's bundle when it is installed here, else empty.
	App string
}

// Context is the session's brief: how to upgrade, then this machine's facts.
func Context(f Facts) string {
	var b strings.Builder
	b.WriteString(brief)
	b.WriteString("\n## This machine\n\n")
	fmt.Fprintf(&b, "- Machine: %s, the %s\n", orUnknown(f.Machine), orUnknown(f.Role))
	fmt.Fprintf(&b, "- Running build: engineroom %s\n", orUnknown(f.Version))
	if f.Clone != "" {
		fmt.Fprintf(&b, "- Clone it was built from: %s\n", f.Clone)
	} else {
		b.WriteString("- Clone it was built from: not recorded (the build was not made by scripts/install.sh). Find the engineroom clone on this machine, or ask the developer where it is, before step 1.\n")
	}
	if f.App != "" {
		fmt.Fprintf(&b, "- Menu bar app: installed at %s (bundle %s)\n", f.App, BundleID)
	} else {
		b.WriteString("- Menu bar app: not installed here; install without --app and skip step 5\n")
	}
	return b.String()
}

// Opening is the session's first prompt: it starts the upgrade at once, since pressing the
// button was the developer asking for it.
func Opening(f Facts) string {
	return fmt.Sprintf("Upgrade engineroom on %s now, following your instructions.", orUnknown(f.Machine))
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
