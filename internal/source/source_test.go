package source

import (
	"reflect"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/ops"
)

// A client writes its start choices as flags and the engine reads them back (SESS-REMOTE-003).
// Fails if any combination is lost or changed on the way, or if the flags swallow the
// arguments around them.
func TestStartFlagsRoundTrip(t *testing.T) {
	for _, start := range []ops.Start{{}, {RemoteControl: true}, {NoJoin: true}, {RemoteControl: true, NoJoin: true}} {
		args := append([]string{"open", "project-a"}, StartFlags(start)...)
		got, rest := ParseStartFlags(append(args, "--json"))
		if got != start {
			t.Fatalf("%+v came back as %+v", start, got)
		}
		if want := []string{"open", "project-a", "--json"}; !reflect.DeepEqual(rest, want) {
			t.Fatalf("the other arguments came back as %q, want %q", rest, want)
		}
	}
}

// PROJ-CAT-001 and 002: the detailed answer keeps each heading, and the names-only answer of an
// older engine is listed without headings rather than refused. Would catch a client that fails on
// an older engine, or one that drops the headings a newer engine sends.
func TestParseProjectsReadsBothAnswers(t *testing.T) {
	views, err := ParseProjects([]byte(`[{"name":"b","group":"web"},{"name":"a"}]`))
	if err != nil || len(views) != 2 || views[0].Name != "b" || views[0].Group != "web" || views[1].Group != "" {
		t.Fatalf("detailed: %+v %v", views, err)
	}
	views, err = ParseProjects([]byte(`["project-a","project-b"]`))
	if err != nil || len(views) != 2 || views[1].Name != "project-b" || views[0].Group != "" {
		t.Fatalf("names only: %+v %v", views, err)
	}
	if _, err := ParseProjects([]byte(`{"oops":1}`)); err == nil {
		t.Fatal("a malformed answer was accepted")
	}
}
