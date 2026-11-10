package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureConfig is the fixture's site.toml, named so a case can change one line of it.
const fixtureConfig = `product = "tool-a"
command = "tool-a"
folder = "tool-a"
tagline = "A tagline."
platform = "Built for testing."
repository = "https://git.example/owner-a/repo-a"
branch = "main"
url = "https://site.example/tool-a/"
images = "docs/images"
share = "images/shot.png"
style = "plain"

[styles]
plain = "Plain"

[aliases]
"README.md" = "index.html"

[[doc]]
source = "docs/guide.md"
slug = "guide"
nav = "Guide"
about = "The guide."
split = 3

[[doc]]
source = "docs/notes.md"
slug = "notes"
nav = "Notes"
about = "The notes."
split = 2
`

// fixture writes a small repository with a site in it and returns its root. files overrides or
// adds to the defaults, keyed by repository path.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	defaults := map[string]string{
		"site/site.toml":                 fixtureConfig,
		"site/templates/layout.html":     `{{define "layout"}}<html><head><title>{{.Title}}</title><meta name="description" content="{{.Description}}">{{if .Canonical}}<link rel="canonical" href="{{.Canonical}}">{{end}}<link href="{{.Root}}assets/site.css"></head><body>{{block "main" .}}{{end}}</body></html>{{end}}`,
		"site/templates/parts/part.html": `{{define "part"}}<p id="part">{{.Site.Command}}</p>{{end}}`,
		"site/pages/about.html":          `{{define "title"}}About it{{end}}{{define "main"}}{{template "part" .}}<span id="section">{{.Section}}</span>{{end}}`,
		"site/templates/doc.html":        `{{define "main"}}<main>{{.Body}}</main>{{range .Nav}}<a href="{{.Href}}">{{.Title}}</a>{{end}}{{end}}`,
		"site/pages/index.html":          `{{define "main"}}<h1 id="top">{{.Site.Product}}</h1>{{range .Docs}}<a href="{{.Config.Slug}}/index.html">{{.Config.Nav}}</a>{{end}}{{end}}`,
		"site/assets/site.css":           `body { color: black; }`,
		"site/assets/styles/plain.css":   `body { color: blue; }`,
		"README.md":                      "# Repo\n",
		"CONTRIBUTING.md":                "# Contributing\n",
		"docs/images/shot.png":           "not really a png",
		"docs/guide.md": "# The guide\n\nIntro, see the [README](../README.md).\n\n" +
			"## Set up\n\nSet-up text, see [the slots](#ports-and-slots) and [contributing](../CONTRIBUTING.md#contributing).\n\n" +
			"### `tool-a` itself\n\nInstall it.\n\n#### A detail\n\nDetail.\n\n" +
			"## Use\n\nUse it, as [the first note](notes.md#the-first) says.\n\n![A shot](images/shot.png)\n\n" +
			"### Ports and slots\n\nSlots, by `notes.md` (code, not a link).\n",
		"docs/notes.md": "# Notes\n\nThe notes.\n\n" +
			"## The first\n\nA note, see [the guide's slots](guide.md#ports-and-slots).\n\n" +
			"## The second\n\nAnother note.\n",
	}
	for k, v := range files {
		defaults[k] = v
	}
	for p, content := range defaults {
		if content == "" {
			continue // an empty override removes a default file
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func read(t *testing.T, out, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(file)))
	if err != nil {
		t.Fatalf("%s was not built: %v", file, err)
	}
	return string(b)
}

func mustContain(t *testing.T, file, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s does not contain %q:\n%s", file, w, got)
		}
	}
}

// Guards the heading ids: a link written for GitHub (usage.md#ports-and-slots) only lands on the
// site if the site gives each heading the id GitHub gives it.
func TestGitHubSlug(t *testing.T) {
	cases := map[string]string{
		"Ports and slots":                         "ports-and-slots",
		"engineroom itself":                       "engineroom-itself",
		"Your settings and a repository's recipe": "your-settings-and-a-repositorys-recipe",
		"Two  spaces, kept as two hyphens":        "two--spaces-kept-as-two-hyphens",
		"Ask Claude, and ✻ marks":                 "ask-claude-and--marks",
		"snake_case stays":                        "snake_case-stays",
	}
	for in, want := range cases {
		if got := githubSlug(in); got != want {
			t.Errorf("githubSlug(%q) = %q, want %q", in, got, want)
		}
	}
	seen := map[string]int{}
	for i, want := range []string{"notes", "notes-1", "notes-2"} {
		if got := uniqueID("notes", seen); got != want {
			t.Errorf("repeat %d: uniqueID = %q, want %q", i, got, want)
		}
	}
}

// Guards the whole build on a doc of each kind: pages split at the configured level, headings
// moved up to match, every kind of link resolved (within a page's doc, to another doc's heading,
// to a file the site does not render) and images copied.
func TestBuildRendersDocsIntoPages(t *testing.T) {
	root := fixture(t, nil)
	out := filepath.Join(t.TempDir(), "out")
	n, err := Build(root, out)
	if err != nil {
		t.Fatal(err)
	}
	// guide: index, Set up, tool-a itself, Use, Ports and slots; notes: index, two; and the
	// two hand-written pages.
	if n != 10 {
		t.Errorf("built %d pages, want 10", n)
	}
	if problems := Check(out); len(problems) > 0 {
		t.Errorf("the built fixture has problems: %v", problems)
	}

	idx := read(t, out, "guide/index.html")
	mustContain(t, "guide/index.html", idx, `<h1 id="the-guide">The guide</h1>`, `href="../index.html"`)

	setup := read(t, out, "guide/set-up.html")
	mustContain(t, "guide/set-up.html", setup,
		`<h1 id="set-up">Set up</h1>`,
		`href="ports-and-slots.html#ports-and-slots"`,
		`href="https://git.example/owner-a/repo-a/blob/main/CONTRIBUTING.md#contributing"`)
	if strings.Contains(setup, "Install it") {
		t.Error("set-up.html holds the next page's text: a level 3 heading did not start a page")
	}

	itself := read(t, out, "guide/tool-a-itself.html")
	// A level 3 page's h4 moves up to h2, so each page starts at h1.
	mustContain(t, "guide/tool-a-itself.html", itself, `<h1 id="tool-a-itself">`, `<h2 id="a-detail">A detail</h2>`)

	use := read(t, out, "guide/use.html")
	// A link to a heading in another doc goes to the page that heading starts.
	mustContain(t, "guide/use.html", use, `<a href="../notes/the-first.html#the-first">the first note</a> says`,
		`src="../files/docs/images/shot.png"`)
	if _, err := os.Stat(filepath.Join(out, "files", "docs", "images", "shot.png")); err != nil {
		t.Errorf("the image was not copied: %v", err)
	}

	first := read(t, out, "notes/the-first.html")
	mustContain(t, "notes/the-first.html", first, `<h1 id="the-first">The first</h1>`,
		`<a href="../guide/ports-and-slots.html#ports-and-slots">the guide's slots</a>`)

	// A hand-written page names its own title, uses a shared part, and is marked in the
	// navigation by its file name.
	about := read(t, out, "about.html")
	mustContain(t, "about.html", about, `<title>About it</title>`, `<p id="part">tool-a</p>`,
		`<span id="section">about</span>`)

	home := read(t, out, "index.html")
	mustContain(t, "index.html", home, `<h1 id="top">tool-a</h1>`, `href="guide/index.html"`)
	if _, err := os.Stat(filepath.Join(out, "assets", "site.css")); err != nil {
		t.Errorf("the assets were not copied: %v", err)
	}
}

// Guards what a search engine and a link preview read. Each would fail silently otherwise: a page
// described by another page's words, an address that is not the page's own, a page missing from
// the sitemap, and a page for a missing address whose links only work from the site's root.
func TestBuildDescribesAndListsEveryPage(t *testing.T) {
	long := strings.Repeat("word ", 60)
	root := fixture(t, map[string]string{
		"site/pages/404.html": `{{define "title"}}Not here{{end}}{{define "description"}}Nothing at
  this   address.{{end}}{{define "main"}}<a href="{{.Root}}index.html">home</a>{{end}}`,
		"docs/notes.md": "# Notes\n\nThe notes, with `code` and a [link](guide.md) in them, long enough to stand alone.\n\n" +
			"## The first\n\n" + long + "\n\n" +
			"## The second\n\nRun:\n\n```sh\ntool-a\n```\n",
	})
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(root, out); err != nil {
		t.Fatal(err)
	}
	if problems := Check(out); len(problems) > 0 {
		t.Errorf("the built fixture has problems: %v", problems)
	}

	// A doc page is described by its own first paragraph, as plain text.
	mustContain(t, "notes/index.html", read(t, out, "notes/index.html"),
		`content="The notes, with code and a link in them, long enough to stand alone."`,
		`<link rel="canonical" href="https://site.example/tool-a/notes/index.html">`)
	// A long one is cut at a word, within the limit.
	first := read(t, out, "notes/the-first.html")
	cut := strings.TrimRight(strings.Repeat("word ", 32), " ") + "..."
	mustContain(t, "notes/the-first.html", first, `content="`+cut+`"`)
	// A lead-in line says nothing alone, so the page takes its title and the doc's sentence.
	mustContain(t, "notes/the-second.html", read(t, out, "notes/the-second.html"),
		`content="The second. The notes."`)

	// A hand-written page with no description of its own takes the tagline; the front page's
	// address is the site's.
	mustContain(t, "about.html", read(t, out, "about.html"), `content="A tagline."`,
		`href="https://site.example/tool-a/about.html"`)
	mustContain(t, "index.html", read(t, out, "index.html"), `<link rel="canonical" href="https://site.example/tool-a/">`)

	// The page for a missing address: its own description with the line breaks folded, whole
	// links, no address of its own, and no place in the sitemap.
	missing := read(t, out, "404.html")
	mustContain(t, "404.html", missing, `<title>Not here</title>`, `content="Nothing at this address."`,
		`<a href="https://site.example/tool-a/index.html">home</a>`, `href="https://site.example/tool-a/assets/site.css"`)
	if strings.Contains(missing, "canonical") {
		t.Error("404.html names a canonical address; it is served from every missing address and has none")
	}

	sitemap := read(t, out, "sitemap.xml")
	mustContain(t, "sitemap.xml", sitemap,
		"<loc>https://site.example/tool-a/</loc>",
		"<loc>https://site.example/tool-a/about.html</loc>",
		"<loc>https://site.example/tool-a/guide/ports-and-slots.html</loc>",
		"<loc>https://site.example/tool-a/notes/the-second.html</loc>")
	if strings.Contains(sitemap, "404") || strings.Contains(sitemap, "tool-a/index.html") {
		t.Errorf("the sitemap lists a page it should not:\n%s", sitemap)
	}
	if n := strings.Count(sitemap, "<loc>"); n != 10 {
		t.Errorf("the sitemap lists %d pages, want the 10 that have an address", n)
	}
	mustContain(t, "robots.txt", read(t, out, "robots.txt"), "Sitemap: https://site.example/tool-a/sitemap.xml")

	// The pictures folder is copied for the hand-written pages and the link preview.
	if _, err := os.Stat(filepath.Join(out, "images", "shot.png")); err != nil {
		t.Errorf("the pictures folder was not copied: %v", err)
	}
}

// Guards the build's refusals: each of these would otherwise reach a reader as a broken page.
func TestBuildRefuses(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"an anchor no heading has", map[string]string{"docs/guide.md": "# G\n\nSee [x](#nowhere).\n"},
			"has no heading #nowhere"},
		{"a file not in the repository", map[string]string{"docs/guide.md": "# G\n\nSee [x](missing.md).\n"},
			"docs/missing.md is not in the repository"},
		{"a link out of the repository", map[string]string{"docs/guide.md": "# G\n\nSee [x](../../etc/hosts).\n"},
			"leaves the repository"},
		{"raw HTML", map[string]string{"docs/guide.md": "# G\n\nText <b>bold</b>.\n"},
			"docs/guide.md:3: raw HTML"},
		{"a heading another doc does not have", map[string]string{"docs/guide.md": "# G\n\nSee [x](notes.md#absent).\n"},
			"docs/notes.md has no heading #absent"},
		{"an anchor on an aliased file", map[string]string{"docs/guide.md": "# G\n\nSee [x](../README.md#repo).\n"},
			"is shown as index.html, which has no #repo"},
		{"an image from the web", map[string]string{"docs/guide.md": "# G\n\n![x](https://img.example/x.png)\n"},
			"is not in the repository; the site serves only its own files"},
		{"a doc with no title", map[string]string{"docs/guide.md": "## Only a section\n\nText.\n"},
			"has no top heading"},
		{"a section named as the doc's own index page", map[string]string{"docs/guide.md": "# G\n\n## Index\n\nText.\n"},
			"two pages would both be guide/index.html"},
		{"an unset product name", map[string]string{"site/site.toml": "command = \"x\"\n"},
			`"product" is not set`},
		{"a site address that page paths cannot be joined to", map[string]string{"site/site.toml": strings.Replace(fixtureConfig, "https://site.example/tool-a/", "https://site.example/tool-a", 1)},
			"must be the site's whole address, ending in a slash"},
		{"a site address with no scheme", map[string]string{"site/site.toml": strings.Replace(fixtureConfig, "https://site.example/tool-a/", "site.example/", 1)},
			"must be the site's whole address"},
		{"a link preview picture that is not in the site", map[string]string{"site/site.toml": strings.Replace(fixtureConfig, "images/shot.png", "images/absent.png", 1)},
			`share "images/absent.png" is not in the built site`},
		{"a link preview picture with no folder to take it from", map[string]string{"site/site.toml": strings.Replace(fixtureConfig, "images = \"docs/images\"\n", "", 1)},
			"images names no folder"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture(t, c.files)
			_, err := Build(root, filepath.Join(t.TempDir(), "out"))
			if err == nil {
				t.Fatalf("built, want an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q, want it to contain %q", err, c.want)
			}
		})
	}
}

// Guards the check on the built site, which is what catches a hand-written page's broken link.
func TestCheckFindsProblems(t *testing.T) {
	out := t.TempDir()
	dash := string([]rune{0x2014})
	files := map[string]string{
		"index.html":       `<a href="guide/index.html#there">ok</a><a href="gone.html">x</a><a href="guide/index.html#absent">x</a><a href="/root.html">x</a><a href="guide">x</a><a href="https://elsewhere.example/">ok</a><a href="#top">x</a>`,
		"guide/index.html": `<h1 id="there">Here ` + dash + ` there</h1>`,
		"assets/site.css":  `@font-face { src: url("fonts/missing.woff2"); }`,
	}
	for p, c := range files {
		full := filepath.Join(out, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(Check(out), "\n")
	for _, want := range []string{
		`gone.html is not in the site`,
		`guide/index.html has no #absent`,
		`starts at the root`,
		`is a folder`,
		`index.html has no #top`,
		`guide/index.html:1: an em or en dash`,
		`assets/fonts/missing.woff2 is not in the site`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the check missed %q; it reported:\n%s", want, got)
		}
	}
	if strings.Contains(got, "elsewhere.example") || strings.Contains(got, "#there") {
		t.Errorf("the check reported a good link:\n%s", got)
	}
}

// Guards the one destructive step: the build empties its folder only when it built it.
func TestBuildRefusesToEmptyAFolderItDidNotBuild(t *testing.T) {
	root := fixture(t, nil)
	out := t.TempDir()
	keep := filepath.Join(out, "someone-elses.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, out); err == nil || !strings.Contains(err.Error(), "refusing to empty it") {
		t.Fatalf("Build into a folder it did not build: err = %v, want a refusal", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("the file that was there is gone: %v", err)
	}

	// A folder it built is emptied and built again: a file from the last build does not linger.
	built := filepath.Join(t.TempDir(), "out")
	if _, err := Build(root, built); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(built, "stale.html")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, built); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file from an earlier build survived the rebuild")
	}
}

// Guards the real site: the repository's own docs and pages build with no problem, so go test
// fails on a broken doc link even where scripts/verify.sh is not run.
func TestRepositorySiteBuilds(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(root, out); err != nil {
		t.Fatal(err)
	}
	if problems := Check(out); len(problems) > 0 {
		t.Fatalf("the site has problems:\n%s", strings.Join(problems, "\n"))
	}
}
