package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/health"
)

// runHealth is `engineroom health [--json]`: whether the engine is ready to be worked on from away
// (acceptance/health, docs/decisions.md D-037). On the engine it checks this machine; on a client
// it asks the engine for its report over SSH (HEALTH-CHECK-003). It changes nothing, and ends with
// a failure when anything is missing or the engine was not reached, so a script reads the answer
// from the exit status (HEALTH-READ-001).
func runHealth(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	if len(rest) != 0 {
		fmt.Fprintln(stderr, "usage: engineroom health [--json]")
		return 2
	}
	ctx := context.Background()
	var r health.Report
	cfg, err := config.Load()
	if err == nil && cfg.Role == config.RoleClient {
		r = health.OfEngine(ctx, cfg)
	} else {
		// The engine, a machine with no role yet, or a configuration that cannot be read: this
		// machine is checked, and its setup items say what is wrong with the configuration.
		if err != nil && !errors.Is(err, config.ErrNoConfig) {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
		}
		r = health.Check(ctx)
	}
	if asJSON {
		if code := writeJSON(stdout, r); code != 0 {
			return code
		}
	} else {
		// A Mac used on its own is checked as itself, not as an engine worked on from away (D-039).
		title := "the engine"
		if err == nil && cfg.Role == config.RoleLocal {
			title = "this Mac"
		}
		writeHealth(stdout, r, title)
	}
	if !r.Ready() {
		return 1
	}
	return 0
}

// writeHealth is the report for a person: under each heading, each item with what was found, how
// it was checked and, when missing, its fix, and whether the program can run that fix. title is
// whose health it is: "the engine", or "this Mac" on a Mac used on its own.
func writeHealth(w io.Writer, r health.Report, title string) {
	if r.Unreached != "" {
		fmt.Fprintf(w, "The engine was not reached, so nothing was checked: %s\n", r.Unreached)
		return
	}
	fmt.Fprintf(w, "Health of %s, checked on %s.\n", title, r.Machine)
	headings := map[health.Group]string{
		health.Setup:   "Setup",
		health.Session: "Running sessions",
	}
	var last health.Group
	sessions := 0
	for _, it := range r.Items {
		if it.Group != last {
			fmt.Fprintf(w, "\n%s\n", headings[it.Group])
			last = it.Group
		}
		if it.Group == health.Session {
			sessions++
		}
		state := "present"
		if !it.Present {
			state = "missing"
		}
		fmt.Fprintf(w, "%s  %s: %s\n", state, it.Name, it.Found)
		fmt.Fprintf(w, "         checked with: %s\n", it.Checked)
		if !it.Present {
			fmt.Fprintf(w, "         fix: %s\n", it.Fix)
		}
	}
	if sessions == 0 {
		fmt.Fprintln(w, "\nRunning sessions\nnone running")
	}
	fmt.Fprintln(w)
	switch r.Missing {
	case 0:
		fmt.Fprintf(w, "Ready: every item is present (%d checked). Nothing was changed.\n", len(r.Items))
	case 1:
		fmt.Fprintf(w, "Not ready: 1 of %d items is missing. Nothing was changed.\n", len(r.Items))
	default:
		fmt.Fprintf(w, "Not ready: %d of %d items are missing. Nothing was changed.\n", r.Missing, len(r.Items))
	}
}
