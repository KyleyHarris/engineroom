package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/assistant"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// runAssistant is `engineroom assistant prepare <project> <checkout> [--remote-control]`: the first step
// of opening an assistant, run on the engine. It writes what the assistant is told and its layout,
// from the project's page as it is now, and prints where.
func runAssistant(args []string, stdout, stderr io.Writer) int {
	remote := false
	var rest []string
	for _, a := range args {
		if a == "--remote-control" {
			remote = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) != 3 || rest[0] != "prepare" {
		fmt.Fprintln(stderr, "usage: engineroom assistant prepare <project> <checkout folder> [--remote-control]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	contextFile, layoutFile, err := prepareAssistant(cfg, rest[1], rest[2], remote)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s\nwrote %s\n", contextFile, layoutFile)
	return 0
}

// assistantDir is where assistants' context and layouts are written.
func assistantDir() string { return filepath.Join(paths.StateDir(), "assistant") }

func prepareAssistant(cfg *config.Config, project, checkout string, remote bool) (string, string, error) {
	page, err := ops.Page(cfg, project)
	if err != nil {
		return "", "", err
	}
	c, ok := checkoutOf(page, checkout)
	if !ok {
		return "", "", fmt.Errorf("%s is not a checkout of %s", checkout, project)
	}
	p, _ := cfg.Project(project)
	name := assistant.SessionName(c.Path)
	ctx := assistant.Context(page, c, settings.Dir(p.Name, p.Path))
	contextFile := filepath.Join(assistantDir(), name+".md")
	layout := assistant.Layout(cfg.Agent.Command, contextFile, settings.Root(), assistant.Opening(page, c), remote)
	return assistant.Write(assistantDir(), name, ctx, layout)
}

// checkoutOf finds the checkout at path, comparing folders as the file system resolves them: git
// names them through any symbolic link (/private/var for /var on macOS), the app as it was given.
func checkoutOf(page ops.ProjectPage, path string) (ops.CheckoutView, bool) {
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	want := real(path)
	for _, c := range page.Checkouts {
		if real(c.Path) == want {
			return c, true
		}
	}
	return ops.CheckoutView{}, false
}

// assistantPlan opens a checkout's assistant (internal/assistant): joins it when it is running,
// otherwise prepares its context and layout on the engine, starts it there in the background, and
// waits for it. A client's app then joins it in a terminal window unless it started with Remote
// Control alone.
func assistantPlan(cfg *config.Config, project, checkout string, remote bool) (action.Plan, error) {
	page, err := ops.Page(cfg, project)
	if err != nil {
		return action.Plan{}, err
	}
	c, ok := checkoutOf(page, checkout)
	if !ok {
		return action.Plan{}, errors.New(checkout + " is not a checkout of " + project)
	}
	name := assistant.SessionName(c.Path)
	plan := action.Plan{Title: "Ask Claude about " + project, Machine: "engine"}
	if list, err := zj.List(); err == nil {
		for _, s := range list {
			if s.Name == name && !s.Ended {
				return plan, nil
			}
			if s.Name == name {
				plan.Commands = append(plan.Commands, zj.End(name))
			}
		}
	}
	prepare := []string{"engineroom", "assistant", "prepare", project, c.Path}
	if remote {
		prepare = append(prepare, "--remote-control")
	}
	plan.Commands = append(plan.Commands,
		action.Command{Argv: prepare, Why: "write what the assistant is told about " + project + ", and its layout"},
		zj.Create(name, filepath.Join(assistantDir(), name+".kdl"), c.Path),
		action.Command{Argv: []string{"engineroom", "session", "wait", name}, Why: "wait until the assistant is ready"},
	)
	return plan, nil
}
