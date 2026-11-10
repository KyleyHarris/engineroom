// Command site builds the project's website from the committed docs and a few hand-written pages.
//
// The docs stay the one source of fact (docs/decisions.md D-027): every guide page on the site is
// rendered from docs/*.md, so a change to the docs is a change to the site and the two cannot
// drift. Only the pages that tell the story (the front page, how it works) are written by hand,
// under site/pages, and they take the product's name and command from site/site.toml so a rename
// is one edit there.
//
// Usage:
//
//	go run ./tools/site                build into site/out and check it
//	go run ./tools/site -out <dir>     build into another folder and check it
//	go run ./tools/site -check         build into a temporary folder, check it, and remove it
//	go run ./tools/site -serve         serve it on 127.0.0.1:20800, building again on every change
//	go run ./tools/site -serve -port N serve it on another port
//
// Every build is checked: a link or an anchor that goes nowhere, a missing image, and an em or
// en dash all fail it. Nothing is fetched from the network, so the check is the same offline.
//
// Beside the pages, a build writes sitemap.xml and robots.txt from the published address in
// site.toml, and copies the pictures folder it names to images/.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// defaultPort is the site's port in the main clone: the first of the block the repository
// reserves for itself in .engineroom/engineroom.toml, so the address is the same with engineroom or without it.
const defaultPort = 20800

func main() {
	out := flag.String("out", filepath.Join("site", "out"), "folder to build the site into")
	check := flag.Bool("check", false, "build into a temporary folder, check it, and remove it")
	serve := flag.Bool("serve", false, "serve the site on 127.0.0.1, building it again whenever docs/ or site/ change")
	port := flag.Int("port", defaultPort, "the port -serve listens on")
	flag.Parse()

	// The tool runs from the repository root, as scripts/verify.sh does; site/site.toml is how
	// it knows it is there, rather than guessing a root from wherever it was started.
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "site.toml")); err != nil {
		fail(fmt.Errorf("run from the repository root: %w", err))
	}

	if *serve {
		// Stopped by Ctrl-C, or by the up script's down sending SIGTERM; either way the
		// temporary builds are removed on the way out.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err := Serve(ctx, root, *port, os.Stderr, func(addr string) {
			fmt.Printf("site: serving http://%s/ (it builds again when docs/ or site/ change; Ctrl-C stops it)\n", addr)
		})
		if err != nil {
			fail(err)
		}
		return
	}

	dir := *out
	if *check {
		tmp, err := os.MkdirTemp("", "site-check-")
		if err != nil {
			fail(err)
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		dir = tmp
	}

	pages, err := Build(root, dir)
	if err != nil {
		fail(err)
	}
	if problems := Check(dir); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, p)
		}
		fail(fmt.Errorf("%d problem(s) in the built site", len(problems)))
	}
	if *check {
		fmt.Printf("site: %d pages built and checked\n", pages)
		return
	}
	fmt.Printf("site: %d pages built and checked in %s\n", pages, dir)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "site:", err)
	os.Exit(1)
}
