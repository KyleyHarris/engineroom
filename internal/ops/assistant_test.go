package ops

import (
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/zj"
)

// An Ask Claude session is matched to the checkout it was opened for, by its name, including one
// whose checkout's name was cut to leave room for "-ai"; a session that only ends in "-ai", or is
// a checkout's own session, is not one. Fails if the helper session is listed as a project of its
// own (the app then navigates to a project with no name), or an ordinary session is taken for one.
func TestAssistantSessionsAreMatchedToTheirCheckout(t *testing.T) {
	long := strings.Repeat("a", zj.MaxNameLength)
	known := map[string]checkout{
		"project-a": {project: "project-a", path: "/src/project-a"},
		long:        {project: "project-b", path: "/src/" + long},
	}
	if c, ok := assistantOf("project-a-ai", known); !ok || c.project != "project-a" {
		t.Fatalf("project-a-ai: %+v %v", c, ok)
	}
	if c, ok := assistantOf(long[:zj.MaxNameLength-3]+"-ai", known); !ok || c.project != "project-b" {
		t.Fatalf("cut name: %+v %v", c, ok)
	}
	for _, name := range []string{"other-ai", "-ai", "project-a", "project-a-ai-x"} {
		if _, ok := assistantOf(name, known); ok {
			t.Fatalf("%q was taken for an Ask Claude session", name)
		}
	}
}
