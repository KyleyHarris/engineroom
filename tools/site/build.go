package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// builtMarker is written into every folder the tool builds into. The tool empties a folder before
// building into it only when the marker shows it built that folder itself, so a mistyped -out
// can never delete anything else.
const builtMarker = ".site-build"

// View is what every template is given.
type View struct {
	Site  *Config
	Root  string // the way back to the site's root from this page: "" or "../"
	Title string // for the browser's title bar
	Docs  []*Doc // every rendered doc, in the order the site lists them
	Doc   *Doc   // the doc this page belongs to; nil on a hand-written page
	// Section names where this page sits, for marking it in the top navigation (templates
	// compare strings, not the docs themselves): a doc's slug, or a hand-written page's file name
	// without ".html".
	Section string
	Page    *Page // nil on a hand-written page
	Body    template.HTML
	Nav     []NavItem // the doc's pages, for the sidebar
	Prev    *NavItem
	Next    *NavItem
	File    string // this page's path in the built site
}

// NavItem is one entry in a doc's sidebar.
type NavItem struct {
	Title   string
	Href    string
	Depth   int // 0 for the doc's index, then one more for each heading level below the first
	Current bool
}

// Build renders the whole site from the repository at root into out and returns how many pages
// it wrote.
func Build(root, out string) (int, error) {
	cfg, err := loadConfig(filepath.Join(root, "site", "site.toml"))
	if err != nil {
		return 0, err
	}
	s := &site{root: root, cfg: cfg, bySrc: map[string]*Doc{}, assets: map[string]string{}}
	for _, dc := range cfg.Docs {
		d, err := loadDoc(root, dc)
		if err != nil {
			return 0, err
		}
		s.docs = append(s.docs, d)
		s.bySrc[dc.Source] = d
	}
	for _, d := range s.docs {
		for _, p := range d.Pages {
			if err := s.resolveLinks(p); err != nil {
				return 0, err
			}
		}
	}
	for _, d := range s.docs {
		for _, p := range d.Pages {
			if err := renderPage(p); err != nil {
				return 0, err
			}
		}
	}

	tdir := filepath.Join(root, "site", "templates")
	if err := prepareOut(out); err != nil {
		return 0, err
	}
	count := 0

	// Every page is parsed with the layout and the shared parts (site/templates/parts), pieces of
	// page more than one page shows, so each is written once.
	parts, err := filepath.Glob(filepath.Join(tdir, "parts", "*.html"))
	if err != nil {
		return 0, err
	}
	sort.Strings(parts)
	base := append([]string{filepath.Join(tdir, "layout.html")}, parts...)
	docTmpl, err := template.ParseFiles(append(base, filepath.Join(tdir, "doc.html"))...)
	if err != nil {
		return 0, err
	}
	for _, d := range s.docs {
		nav := navFor(d)
		for i, p := range d.Pages {
			v := View{Site: cfg, Root: rootFrom(p.File), Title: p.Title, Docs: s.docs, Doc: d, Page: p,
				Section: d.Config.Slug, Body: template.HTML(p.Body), File: p.File}
			for j, n := range nav {
				n.Href = relative(p.File, n.Href)
				n.Current = j == i
				v.Nav = append(v.Nav, n)
			}
			if i > 0 {
				prev := v.Nav[i-1]
				v.Prev = &prev
			}
			if i+1 < len(v.Nav) {
				next := v.Nav[i+1]
				v.Next = &next
			}
			if err := writePage(docTmpl, out, p.File, v); err != nil {
				return 0, err
			}
			count++
		}
	}

	// The hand-written pages: each file in site/pages is one page at the site's root, laid out
	// like every other page.
	pages, err := filepath.Glob(filepath.Join(root, "site", "pages", "*.html"))
	if err != nil {
		return 0, err
	}
	sort.Strings(pages)
	for _, pf := range pages {
		t, err := template.ParseFiles(append(append([]string{}, base...), pf)...)
		if err != nil {
			return 0, err
		}
		file := filepath.Base(pf)
		// A hand-written page names itself in the navigation by its file name, and may define a
		// "title" template for the browser's title bar; the front page defines none.
		v := View{Site: cfg, Root: "", Docs: s.docs, File: file, Section: strings.TrimSuffix(file, ".html")}
		if tt := t.Lookup("title"); tt != nil {
			var title bytes.Buffer
			if err := tt.Execute(&title, v); err != nil {
				return 0, fmt.Errorf("%s: title: %w", file, err)
			}
			v.Title = strings.TrimSpace(title.String())
		}
		if err := writePage(t, out, file, v); err != nil {
			return 0, err
		}
		count++
	}

	if err := copyTree(filepath.Join(root, "site", "assets"), filepath.Join(out, "assets")); err != nil {
		return 0, err
	}
	for repoPath, file := range s.assets {
		if err := copyFile(filepath.Join(root, filepath.FromSlash(repoPath)), filepath.Join(out, filepath.FromSlash(file))); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// renderPage turns a page's Markdown blocks into HTML. The blocks are moved into a document of
// their own, so the page renders exactly what it holds and nothing of its neighbours.
func renderPage(p *Page) error {
	doc := ast.NewDocument()
	for _, n := range p.nodes {
		doc.AppendChild(doc, n)
	}
	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, p.Doc.src, doc); err != nil {
		return fmt.Errorf("%s: %s: %w", p.Doc.Config.Source, p.Title, err)
	}
	p.Body = buf.Bytes()
	return nil
}

// navFor lists a doc's pages for its sidebar, with addresses from the site's root; each page
// makes them relative to itself.
func navFor(d *Doc) []NavItem {
	first := 0
	for _, p := range d.Pages[1:] {
		if first == 0 || p.Level < first {
			first = p.Level
		}
	}
	var nav []NavItem
	for i, p := range d.Pages {
		depth := 0
		if i > 0 {
			depth = p.Level - first + 1
		}
		nav = append(nav, NavItem{Title: p.Title, Href: p.File, Depth: depth})
	}
	return nav
}

// rootFrom is the relative way back to the site's root from a page at file.
func rootFrom(file string) string {
	return strings.Repeat("../", strings.Count(file, "/"))
}

func writePage(t *template.Template, out, file string, v View) error {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	dst := filepath.Join(out, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, buf.Bytes(), 0o644)
}

// prepareOut empties the folder the site is built into, but only a folder this tool built
// before (it carries the marker) or one that is empty or absent.
func prepareOut(out string) error {
	entries, err := os.ReadDir(out)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(out, builtMarker)); err != nil {
			return fmt.Errorf("%s is not empty and was not built by this tool; refusing to empty it", out)
		}
		if err := os.RemoveAll(out); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, builtMarker), []byte("built by tools/site; emptied on each build\n"), 0o644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(p, filepath.Join(dst, rel))
	})
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
