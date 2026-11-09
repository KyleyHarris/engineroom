//go:build journeys

package journeys

import (
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/journey"
	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// screenLines is the screen's lines with trailing spaces removed, the cursor marker blanked so
// a line reads the same whether or not it is selected.
func screenLines(term *termtest.Terminal) []string {
	var out []string
	for _, l := range strings.Split(term.Screen(), "\n") {
		l = strings.TrimRight(l, " ")
		if strings.HasPrefix(l, "> ") {
			l = "  " + l[2:]
		}
		out = append(out, l)
	}
	return out
}

// lineOf is the index of the first line equal to want, or -1.
func lineOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

// lineOfContaining is the index of the first line holding want, or -1.
func lineOfContaining(lines []string, want string) int {
	for i, l := range lines {
		if strings.Contains(l, want) {
			return i
		}
	}
	return -1
}

// PROJ-CAT-001: projects registered with categories are listed under a heading per category,
// the categories in order, and the one with no category after them. Fails if a project is
// listed under the wrong heading, a heading is missing, or the uncategorised project comes
// before a category.
func TestProjCat001(t *testing.T) {
	m := journey.NewMachine(t)
	m.BeEngine()
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjCat001{
		GivenAProjectRegisteredInTheCategoryToolsTwoInTheCategoryWebAndOneWithNoCategory: func() {
			// Registered through the program's own command, in an order that is not the list's,
			// so the list's order is the program's doing.
			// The web pair is registered second name first, and the second is capitalised, so
			// neither registration order nor an order that minds case gives the list's order.
			for _, p := range [][2]string{{"project-n", ""}, {"Project-w2", "web"}, {"project-t", "tools"}, {"project-w1", "web"}} {
				args := []string{"project", "add", m.NewRepo(p[0])}
				if p[1] != "" {
					args = append(args, "--category", p[1])
				}
				run(t, m, args...)
			}
		},
		WhenTheDeveloperOpensTheListOfProjects: func() {
			term = m.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("project-n")
		},
		ThenEachProjectIsShownUnderAHeadingForItsCategory: func() {
			lines := screenLines(term)
			want := []string{"tools", "    project-t", "web"}
			at := lineOf(lines, want[0])
			if at < 0 || at+len(want) > len(lines) {
				t.Fatalf("no tools heading:\n%s", term.Screen())
			}
			for i, w := range want {
				if lines[at+i] != w {
					t.Fatalf("line %d is %q, want %q:\n%s", at+i, lines[at+i], w, term.Screen())
				}
			}
		},
		AndUnderEachHeadingTheProjectsAreInAlphabeticalOrderOfTheirNamesIgnoringCase: func() {
			lines := screenLines(term)
			at := lineOf(lines, "web")
			if at < 0 || at+2 >= len(lines) || lines[at+1] != "    project-w1" || lines[at+2] != "    Project-w2" {
				t.Fatalf("web does not list project-w1 then Project-w2:\n%s", term.Screen())
			}
		},
		AndTheProjectWithNoCategoryIsListedAfterTheCategorys: func() {
			lines := screenLines(term)
			n, w := lineOf(lines, "  project-n"), lineOf(lines, "    Project-w2")
			if n < 0 || w < 0 || n < w {
				t.Fatalf("the project with no category is not after the categories, unindented:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}

// PROJ-CAT-002: a client of an engine that answers with names only (as an engine from before categories
// does) lists every project with no headings. The engine's program is wrapped so its project list
// is the names-only answer whatever it is asked; every other request reaches the real program.
// Fails if the client refuses that answer or invents headings.
func TestProjCat002(t *testing.T) {
	client := journey.NewMachine(t)
	engine := journey.NewEngine(t, client)
	client.BeClientOf(engine, "engine")
	var term *termtest.Terminal

	scenarios.Run(t, scenarios.ProjCat002{
		GivenAnEngineThatAnswersTheListOfProjectsWithTheirNamesOnly: func() {
			engine.BeEngine("project-a", "/home/dev/src/project-a", "project-b", "/home/dev/src/project-b")
			engine.MustExec(`mv /home/dev/bin/engineroom /home/dev/bin/engineroom-real && printf '%s\n' '#!/bin/sh' 'if [ "$1" = projects ]; then exec engineroom-real projects --json; fi' 'exec engineroom-real "$@"' > /home/dev/bin/engineroom && chmod 755 /home/dev/bin/engineroom`)
			if out := engine.MustExec("engineroom projects --json --detail"); !strings.Contains(out, `"project-a"`) || strings.Contains(out, "{") {
				t.Fatalf("the engine does not answer with names only:\n%s", out)
			}
		},
		WhenTheDeveloperOnAClientOpensTheListOfProjects: func() {
			term = client.Start(t)
			term.WaitFor("Registered projects")
			term.WaitFor("project-b")
		},
		ThenEveryProjectIsListedWithNoCategoryHeadings: func() {
			lines := screenLines(term)
			a, b := lineOf(lines, "  project-a"), lineOf(lines, "  project-b")
			if a < 0 || b != a+1 {
				t.Fatalf("the projects are not listed one after the other with no heading:\n%s", term.Screen())
			}
		},
	}, term0(&term))
}
