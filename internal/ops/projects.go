package ops

import (
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/roots"
)

// ProjectView is one registered project as a list shows it.
type ProjectView struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	// Group is the heading the project is listed under: the root its folder is in, then its
	// category ("root / category"), either alone, or empty for neither.
	Group string `json:"group,omitempty"`
	// Pinned projects are listed first.
	Pinned bool `json:"pinned,omitempty"`
}

// Projects lists the registered projects in the order a list shows them: grouped by heading,
// the headings in alphabetical order (ignoring case), and the projects with no heading last
// (PROJ-CAT-001). Within a heading the projects are in alphabetical order of their names,
// ignoring case, so a project is found where its name says it is whatever order it was
// registered in.
func Projects(cfg *config.Config) []ProjectView {
	out := make([]ProjectView, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		out = append(out, ProjectView{Name: p.Name, Path: p.Path, Group: group(cfg, p), Pinned: p.Pinned})
	}
	SortProjects(out)
	return out
}

// group is the heading for a project: a root separates one owner's work from another's, so it
// comes first, and the category groups within it.
func group(cfg *config.Config, p config.Project) string {
	r, ok := roots.Of(cfg, p.Path)
	switch {
	case ok && p.Category != "":
		return r.Name + " / " + p.Category
	case ok:
		return r.Name
	}
	return p.Category
}

// SortProjects puts views in list order (see Projects). A client applies it to what the engine
// sent too, so the order never depends on which version of the engine answered.
func SortProjects(views []ProjectView) {
	sort.SliceStable(views, func(i, j int) bool {
		gi, gj := views[i].Group, views[j].Group
		if (gi == "") != (gj == "") {
			return gj == ""
		}
		if !strings.EqualFold(gi, gj) {
			return strings.ToLower(gi) < strings.ToLower(gj)
		}
		return roots.NameLess(views[i].Name, views[j].Name)
	})
}

// Names is the project names of views, in their order.
func Names(views []ProjectView) []string {
	out := make([]string, len(views))
	for i, v := range views {
		out[i] = v.Name
	}
	return out
}
