package main

import (
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The built site is read with patterns rather than an HTML parser because every page in it is
// written by goldmark or by html/template, and both always quote attributes with double quotes.
var (
	attrRef = regexp.MustCompile(`\s(?:href|src)="([^"]*)"`)
	attrID  = regexp.MustCompile(`\sid="([^"]*)"`)
	cssURL  = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
)

// dashes are the en dash and the em dash, written as code points so this file never holds one.
var dashes = string([]rune{0x2013, 0x2014})

// Check reads a built site and returns every problem a reader would hit: a link or an image to a
// file that is not there, a link to an anchor that is not on its page, and an em or en dash, which
// the project's writing never uses. Links to other sites are not followed: the check stays the
// same with no network.
func Check(out string) []string {
	var problems []string
	ids := map[string]map[string]bool{}
	type ref struct{ from, target string }
	var refs []ref

	err := filepath.WalkDir(out, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		ext := filepath.Ext(p)
		if ext != ".html" && ext != ".css" && ext != ".js" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		rel = filepath.ToSlash(rel)
		s := string(b)
		for i, line := range strings.Split(s, "\n") {
			if strings.ContainsAny(line, dashes) {
				problems = append(problems, fmt.Sprintf("%s:%d: an em or en dash; use a hyphen", rel, i+1))
			}
		}
		switch ext {
		case ".html":
			set := map[string]bool{}
			for _, m := range attrID.FindAllStringSubmatch(s, -1) {
				set[html.UnescapeString(m[1])] = true
			}
			ids[rel] = set
			for _, m := range attrRef.FindAllStringSubmatch(s, -1) {
				refs = append(refs, ref{rel, html.UnescapeString(m[1])})
			}
		case ".css":
			for _, m := range cssURL.FindAllStringSubmatch(s, -1) {
				refs = append(refs, ref{rel, m[1]})
			}
		}
		return nil
	})
	if err != nil {
		return append(problems, err.Error())
	}

	for _, r := range refs {
		if r.target == "" {
			problems = append(problems, fmt.Sprintf("%s: an empty link", r.from))
			continue
		}
		if hasScheme.MatchString(r.target) || strings.HasPrefix(r.target, "//") {
			continue
		}
		u, err := url.Parse(r.target)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: link %q: %v", r.from, r.target, err))
			continue
		}
		file := r.from
		if u.Path != "" {
			if strings.HasPrefix(u.Path, "/") {
				// The site is opened from disk as well as served, so a link from the root would
				// break when it is opened from disk.
				problems = append(problems, fmt.Sprintf("%s: link %q starts at the root; make it relative", r.from, r.target))
				continue
			}
			file = filepath.ToSlash(filepath.Join(filepath.Dir(r.from), u.Path))
			info, err := os.Stat(filepath.Join(out, filepath.FromSlash(file)))
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: link %q: %s is not in the site", r.from, r.target, file))
				continue
			}
			if info.IsDir() {
				problems = append(problems, fmt.Sprintf("%s: link %q is a folder; link its index.html, which opens from disk too", r.from, r.target))
				continue
			}
		}
		if u.Fragment != "" {
			if set, ok := ids[file]; !ok || !set[u.Fragment] {
				problems = append(problems, fmt.Sprintf("%s: link %q: %s has no #%s", r.from, r.target, file, u.Fragment))
			}
		}
	}
	sort.Strings(problems)
	return problems
}
