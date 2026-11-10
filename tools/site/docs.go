package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Doc is one Markdown file turned into pages: its index page first, then a page for every
// heading that starts one, in the order they appear.
type Doc struct {
	Config DocConfig
	Title  string  // the doc's own top heading, as plain text
	Pages  []*Page // Pages[0] is the index page
	src    []byte
}

// Page is one page of a doc. Its nodes are the Markdown blocks it holds, moved out of the parsed
// doc so each page renders on its own; Body is filled in when it is rendered.
type Page struct {
	Doc     *Doc
	Title   string          // plain text, for the browser's title and the navigation
	File    string          // path in the built site, such as "guide/ports-and-slots.html"
	Level   int             // the heading level it started at; 1 for a doc's index page
	Anchors map[string]bool // every heading id on the page, its own included
	Body    []byte

	id    string // the id of the heading that starts it (empty for an index page)
	nodes []ast.Node
}

// markdown is the one Markdown dialect the site reads: CommonMark with GitHub's tables,
// strikethrough, task lists and bare links, which is what the docs are written in and what
// GitHub shows. The typographer is left off on purpose: it would turn "--" into an en dash.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// loadDoc parses one doc, gives every heading the id GitHub would give it (so a link written
// for GitHub, such as usage.md#ports-and-slots, lands in the same place on the site), and splits
// it into pages.
func loadDoc(root string, dc DocConfig) (*Doc, error) {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dc.Source)))
	if err != nil {
		return nil, err
	}
	tree := markdown.Parser().Parse(text.NewReader(src))
	if err := refuseRawHTML(tree, src, dc.Source); err != nil {
		return nil, err
	}

	d := &Doc{Config: dc, src: src}
	index := &Page{Doc: d, File: path.Join(dc.Slug, "index.html"), Level: 1, Anchors: map[string]bool{}}
	d.Pages = append(d.Pages, index)
	cur := index
	ids := map[string]int{}
	files := map[string]bool{index.File: true}

	// The children are collected first because moving a node out of the tree while walking it
	// would skip its next sibling.
	var blocks []ast.Node
	for n := tree.FirstChild(); n != nil; n = n.NextSibling() {
		blocks = append(blocks, n)
	}
	for _, n := range blocks {
		tree.RemoveChild(tree, n)
		h, isHeading := n.(*ast.Heading)
		if isHeading {
			id := uniqueID(githubSlug(plainText(h, src)), ids)
			h.SetAttributeString("id", []byte(id))
			switch {
			case h.Level == 1 && d.Title == "":
				d.Title = plainText(h, src)
				index.Title = d.Title
			case h.Level >= 2 && h.Level <= dc.Split:
				file := path.Join(dc.Slug, id+".html")
				if files[file] {
					return nil, fmt.Errorf("%s: two pages would both be %s", dc.Source, file)
				}
				files[file] = true
				cur = &Page{Doc: d, Title: plainText(h, src), File: file, Level: h.Level, id: id,
					Anchors: map[string]bool{}}
				d.Pages = append(d.Pages, cur)
			}
			cur.Anchors[id] = true
		}
		cur.nodes = append(cur.nodes, n)
	}
	if d.Title == "" {
		return nil, fmt.Errorf("%s: has no top heading (# Title) to name it", dc.Source)
	}

	// A page's own heading becomes its h1 and the headings under it move up to match, so every
	// page reads as a document of its own rather than starting at h3.
	for _, p := range d.Pages[1:] {
		shift := p.Level - 1
		for _, n := range p.nodes {
			_ = ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if h, ok := n.(*ast.Heading); ok && entering {
					h.Level -= shift
				}
				return ast.WalkContinue, nil
			})
		}
	}
	return d, nil
}

// refuseRawHTML fails a doc that holds raw HTML. The renderer leaves raw HTML out for safety,
// so without this check it would vanish from the site silently while still showing on GitHub.
func refuseRawHTML(tree ast.Node, src []byte, source string) error {
	var found error
	_ = ast.Walk(tree, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var at int
		switch v := n.(type) {
		case *ast.HTMLBlock:
			at = v.Lines().At(0).Start
		case *ast.RawHTML:
			at = v.Segments.At(0).Start
		default:
			return ast.WalkContinue, nil
		}
		found = fmt.Errorf("%s:%d: raw HTML would be left out of the site; write it as Markdown",
			source, bytes.Count(src[:at], []byte("\n"))+1)
		return ast.WalkStop, nil
	})
	return found
}

// plainText is a heading's text as a reader sees it: code spans keep their text, emphasis and
// link markup is dropped.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := c.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
			if v.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(v.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// githubSlug is the id GitHub gives a heading: lower case, everything but letters, digits,
// underscores, hyphens and spaces dropped, then each space a hyphen. Runs of spaces are not
// collapsed, which is why "Ports  and slots" becomes "ports--and-slots", as on GitHub.
func githubSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// uniqueID numbers a repeated heading id the way GitHub does: the second is "-1", the third "-2".
func uniqueID(id string, seen map[string]int) string {
	n, ok := seen[id]
	seen[id] = n + 1
	if !ok {
		return id
	}
	return fmt.Sprintf("%s-%d", id, n)
}

// site holds every doc while links are resolved, because a link in one doc can point at a
// heading in another.
type site struct {
	root   string
	cfg    *Config
	docs   []*Doc
	bySrc  map[string]*Doc
	assets map[string]string // repository path of an image -> its path in the built site
}

// resolveLinks rewrites every link and image on a page for the site. A link to a rendered doc
// goes to the page that holds its heading; a link to any other file in the repository goes to
// that file on GitHub. Anything that resolves to nothing fails the build, so a broken link is
// found here and not by a reader.
func (s *site) resolveLinks(p *Page) error {
	var links []ast.Node
	for _, n := range p.nodes {
		_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch c.(type) {
			case *ast.Link, *ast.Image:
				links = append(links, c)
			}
			return ast.WalkContinue, nil
		})
	}
	for _, n := range links {
		switch v := n.(type) {
		case *ast.Link:
			dest, err := s.linkTarget(p, string(v.Destination))
			if err != nil {
				return err
			}
			v.Destination = []byte(dest)
		case *ast.Image:
			dest, err := s.imageTarget(p, string(v.Destination))
			if err != nil {
				return err
			}
			v.Destination = []byte(dest)
		}
	}
	return nil
}

var hasScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// linkTarget turns a link as written in a doc into the address it has on the site.
func (s *site) linkTarget(p *Page, dest string) (string, error) {
	if hasScheme.MatchString(dest) || strings.HasPrefix(dest, "//") {
		return dest, nil // somewhere else on the web: left as written, never fetched
	}
	u, err := url.Parse(dest)
	if err != nil {
		return "", fmt.Errorf("%s: link %q: %w", p.Doc.Config.Source, dest, err)
	}
	target := p.Doc
	if u.Path != "" {
		repoPath, err := s.repoPath(p.Doc.Config.Source, u.Path)
		if err != nil {
			return "", err
		}
		if alias, ok := s.cfg.Aliases[repoPath]; ok {
			if u.Fragment != "" {
				return "", fmt.Errorf("%s: link %q: %s is shown as %s, which has no #%s",
					p.Doc.Config.Source, dest, repoPath, alias, u.Fragment)
			}
			return relative(p.File, alias), nil
		}
		target = s.bySrc[repoPath]
		if target == nil {
			return s.githubLink(p, dest, repoPath, u.Fragment)
		}
	}
	if u.Fragment == "" {
		return relative(p.File, target.Pages[0].File), nil
	}
	for _, tp := range target.Pages {
		if tp.Anchors[u.Fragment] {
			return relative(p.File, tp.File) + "#" + u.Fragment, nil
		}
	}
	return "", fmt.Errorf("%s: link %q: %s has no heading #%s", p.Doc.Config.Source, dest,
		target.Config.Source, u.Fragment)
}

// repoPath resolves a relative link against the doc that holds it, and refuses one that climbs
// out of the repository.
func (s *site) repoPath(from, rel string) (string, error) {
	joined := path.Clean(path.Join(path.Dir(from), rel))
	if joined == ".." || strings.HasPrefix(joined, "../") || path.IsAbs(rel) {
		return "", fmt.Errorf("%s: link %q leaves the repository", from, rel)
	}
	return joined, nil
}

// githubLink sends a link to a file the site does not render to that file on GitHub, once the
// file is known to exist, so a renamed or deleted file is caught by the build.
func (s *site) githubLink(p *Page, dest, repoPath, fragment string) (string, error) {
	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(repoPath)))
	if err != nil {
		return "", fmt.Errorf("%s: link %q: %s is not in the repository", p.Doc.Config.Source, dest, repoPath)
	}
	kind := "blob"
	if info.IsDir() {
		kind = "tree"
	}
	out := strings.TrimSuffix(s.cfg.Repository, "/") + "/" + kind + "/" + s.cfg.Branch + "/" + repoPath
	if fragment != "" {
		out += "#" + fragment
	}
	return out, nil
}

// imageTarget copies an image from the repository into the built site, under files/ at its
// repository path, and points the page at the copy.
func (s *site) imageTarget(p *Page, dest string) (string, error) {
	if hasScheme.MatchString(dest) || strings.HasPrefix(dest, "//") {
		return "", fmt.Errorf("%s: image %q is not in the repository; the site serves only its own files",
			p.Doc.Config.Source, dest)
	}
	repoPath, err := s.repoPath(p.Doc.Config.Source, dest)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(repoPath))); err != nil {
		return "", fmt.Errorf("%s: image %q: %s is not in the repository", p.Doc.Config.Source, dest, repoPath)
	}
	file := path.Join("files", repoPath)
	s.assets[repoPath] = file
	return relative(p.File, file), nil
}

// relative is the address of one file in the built site as seen from another.
func relative(from, to string) string {
	rel, err := filepath.Rel(filepath.Dir(filepath.FromSlash(from)), filepath.FromSlash(to))
	if err != nil {
		return to
	}
	return filepath.ToSlash(rel)
}
