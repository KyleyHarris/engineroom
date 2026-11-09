package ops

import (
	"reflect"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/config"
)

// PROJ-CAT-001's order: headings alphabetical whatever their case, names alphabetical whatever
// their case inside a heading, no heading last. Would catch projects left in registration order,
// a name order that minds case ("Web-c" before "web-a"), or the ungrouped put first.
func TestProjectsAreGroupedInListOrder(t *testing.T) {
	cfg := &config.Config{Projects: []config.Project{
		{Name: "loose", Path: "/p/loose"},
		{Name: "web-b", Path: "/p/web-b", Category: "web"},
		{Name: "Web-c", Path: "/p/web-c", Category: "web"},
		{Name: "tool", Path: "/p/tool", Category: "Tools"},
		{Name: "web-a", Path: "/p/web-a", Category: "web"},
		{Name: "app", Path: "/p/app", Category: "apps"},
	}}
	got := Names(Projects(cfg))
	want := []string{"app", "tool", "web-a", "web-b", "Web-c", "loose"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

// PROJ-ROOT-001 with PROJ-CAT-001: a project inside a root is listed under the root, then its
// category; one outside every root under its category alone. Would catch a heading that drops
// the root (mixing two owners' work) or matches a root by a shared name prefix.
func TestProjectsUnderARootAreHeadedByIt(t *testing.T) {
	cfg := &config.Config{
		Roots: []config.Root{{Name: "acct", Path: "/p/acct", Owner: "owner-a"}},
		Projects: []config.Project{
			{Name: "in-cat", Path: "/p/acct/in-cat", Category: "tools"},
			{Name: "in", Path: "/p/acct/in"},
			{Name: "beside", Path: "/p/acct-other/beside", Category: "tools"},
		},
	}
	got := map[string]string{}
	for _, v := range Projects(cfg) {
		got[v.Name] = v.Group
	}
	want := map[string]string{"in-cat": "acct / tools", "in": "acct", "beside": "tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("headings %v, want %v", got, want)
	}
}
