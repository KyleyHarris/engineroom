package upgrade

import (
	"strings"
	"testing"
)

// D-040: a machine whose engineroom Homebrew installed is told to upgrade with brew, and never sent
// looking for a clone; one built from a clone keeps the clone's steps. Fails if a Homebrew install
// is sent to find a clone, or a clone build is told it came from Homebrew.
func TestTheBriefSaysHowThisMachineWasInstalled(t *testing.T) {
	brew := Context(Facts{Machine: "engine-a", Role: "local", Version: "v0.2.0", Homebrew: true, App: "/Applications/engineroom.app"})
	if !strings.Contains(brew, "- Installed by: Homebrew") || strings.Contains(brew, "Find the engineroom clone") {
		t.Fatalf("a Homebrew install's facts:\n%s", brew[strings.Index(brew, "## This machine"):])
	}
	if !strings.Contains(brew, "brew upgrade --cask kyleyharris/engineroom/engineroom") {
		t.Fatal("the brief does not say how to upgrade a Homebrew install")
	}
	clone := Context(Facts{Machine: "engine-a", Role: "engine", Version: "v0.1.0-3-gabc", Clone: "/src/engineroom"})
	if strings.Contains(clone, "- Installed by: Homebrew") || !strings.Contains(clone, "- Clone it was built from: /src/engineroom") {
		t.Fatalf("a clone build's facts:\n%s", clone[strings.Index(clone, "## This machine"):])
	}
}
