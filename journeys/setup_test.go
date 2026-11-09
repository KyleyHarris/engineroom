//go:build journeys

package journeys

import (
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// CONN-SETUP-001: a machine with no role must say so and offer nothing, because any action it
// took would have to guess which machine to act on. This fails if the program starts with no
// configuration and offers actions, or says nothing about the missing role.
func TestConnSetup001(t *testing.T) {
	m := journey.NewMachine(t)
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ConnSetup001{
		GivenThisMachineHasNotBeenSetUpAsTheEngineOrAsAClient: func() {
			// No configuration file is written: a fresh machine.
		},
		WhenTheDeveloperStartsTheTool: func() {
			term = m.Start(t)
		},
		ThenTheyAreToldThisMachineHasNoRoleYet: func() {
			term.WaitFor("This machine has no role yet")
		},
		AndTheyAreShownHowToSetOne: func() {
			term.WaitFor("engineroom role set engine")
			term.WaitFor("engineroom role set client")
		},
		AndNoActionIsOfferedUntilARoleIsSet: func() {
			screen := term.Screen()
			for _, offered := range []string{"Open a project", "Sessions", "History"} {
				if strings.Contains(screen, offered) {
					t.Fatalf("%q is offered on a machine with no role:\n%s", offered, screen)
				}
			}
			// The keys that would open a project or the session list do nothing here.
			term.Type("o")
			term.Type("s")
			if s := m.Sessions(); strings.Contains(s, "[Created") {
				t.Fatalf("a session was started on a machine with no role:\n%s", s)
			}
		},
	}, term0(&term))
}
