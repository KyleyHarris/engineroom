package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is site/site.toml: the names that change with the product, the docs the site renders,
// and the stack of tools the product joins together. Templates read names from here and never
// spell them, so renaming the product is an edit to this file and to the docs, not to pages.
type Config struct {
	Product    string `toml:"product"`    // the product's name as people read it
	Command    string `toml:"command"`    // the command people type
	Folder     string `toml:"folder"`     // the program's own folder name under ~/.config and ~/.local/state
	Tagline    string `toml:"tagline"`    // one sentence: what it is for
	Platform   string `toml:"platform"`   // what it is built and tested on, stated plainly
	Repository string `toml:"repository"` // the repository's web address, for links to files
	Branch     string `toml:"branch"`     // the branch those links point at

	// Style is the look the site is built with: assets/styles/<style>.css over assets/base.css.
	// Styles names every look there is, by file name, with the label the dev server's style switch
	// shows, so looks can be compared on the running site before one is chosen.
	Style  string            `toml:"style"`
	Styles map[string]string `toml:"styles"`

	// Aliases send a link to a repository file that the site does not render (the README) to a
	// page of the site instead of to the file on GitHub.
	Aliases map[string]string `toml:"aliases"`

	Docs  []DocConfig `toml:"doc"`
	Stack []Layer     `toml:"stack"`
}

// DocConfig says how one doc becomes pages. Every heading at a level from 2 to Split starts a
// page of its own; what comes before the first of them is the doc's own index page.
type DocConfig struct {
	Source string `toml:"source"` // repository path of the Markdown file
	Slug   string `toml:"slug"`   // the folder its pages are written to
	Nav    string `toml:"nav"`    // its name in the site's navigation
	About  string `toml:"about"`  // one sentence on what it holds, for the front page
	Split  int    `toml:"split"`  // deepest heading level that starts a page
}

// Layer is one tool in the stack the product sits on: the job it does and the tool that does it
// today. Kept as data so a tool that is swapped for another (a terminal, a Git host, an agent)
// is an edit here and not a redesign of a page.
type Layer struct {
	Job  string `toml:"job"`
	Tool string `toml:"tool"`
	URL  string `toml:"url"`
	Note string `toml:"note"`
}

func loadConfig(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, err
	}
	// Every name a page reads must be set, or a page would render an empty space where the
	// product's name belongs and nothing would say so.
	for _, f := range []struct{ name, value string }{
		{"product", c.Product}, {"command", c.Command}, {"folder", c.Folder}, {"tagline", c.Tagline},
		{"platform", c.Platform}, {"repository", c.Repository}, {"branch", c.Branch},
	} {
		if strings.TrimSpace(f.value) == "" {
			return nil, fmt.Errorf("%s: %q is not set", path, f.name)
		}
	}
	if _, ok := c.Styles[c.Style]; !ok {
		return nil, fmt.Errorf("%s: style %q is not one of [styles]", path, c.Style)
	}
	for name := range c.Styles {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "assets", "styles", name+".css")); err != nil {
			return nil, fmt.Errorf("%s: style %q has no assets/styles/%s.css", path, name, name)
		}
	}
	slugs := map[string]bool{}
	for i := range c.Docs {
		d := &c.Docs[i]
		if d.Source == "" || d.Slug == "" || d.Nav == "" || d.About == "" {
			return nil, fmt.Errorf("%s: doc %d needs source, slug, nav and about", path, i+1)
		}
		if d.Split < 2 || d.Split > 6 {
			return nil, fmt.Errorf("%s: doc %s: split must be a heading level from 2 to 6", path, d.Source)
		}
		if slugs[d.Slug] {
			return nil, fmt.Errorf("%s: two docs use the folder %q", path, d.Slug)
		}
		slugs[d.Slug] = true
	}
	return &c, nil
}
