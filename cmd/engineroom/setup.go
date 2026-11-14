package main

import (
	"context"
	"fmt"
	"io"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/setup"
)

// runSetup is `engineroom setup check [--json]`: every item this machine needs for its role,
// present or missing, with how each was checked and how to fix it (acceptance/setup). It changes
// nothing. It ends with a failure when anything is missing, so a script or the setup-machine skill
// reads the answer from the exit status, not from words (SETUP-READ-001); --json gives the same
// items for a program to read (SETUP-READ-002).
func runSetup(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	if len(rest) != 1 || rest[0] != "check" {
		fmt.Fprintln(stderr, "usage: engineroom setup check [--json]")
		return 2
	}
	r := setup.Check(context.Background())
	if asJSON {
		if code := writeJSON(stdout, r); code != 0 {
			return code
		}
	} else {
		writeSetup(stdout, r)
	}
	if r.Missing > 0 {
		return 1
	}
	return 0
}

// writeSetup is the report for a person: the role, then each item with what was found, how it was
// checked, and, when missing, its fix.
func writeSetup(w io.Writer, r setup.Report) {
	switch {
	case len(r.Items) > 0 && !r.Items[0].Present:
		// The first item is the configuration: the role in it could not be read.
		fmt.Fprintf(w, "This machine's configuration is not valid (%s).\n\n", r.Config)
	case r.Role == "":
		fmt.Fprintf(w, "This machine has no role (configuration: %s).\n\n", r.Config)
	default:
		fmt.Fprintf(w, "This machine is %s (configuration: %s).\n\n", (&config.Config{Role: config.Role(r.Role)}).Describe(), r.Config)
	}
	for _, it := range r.Items {
		state := "present"
		if !it.Present {
			state = "missing"
		}
		fmt.Fprintf(w, "%s  %s (%s): %s\n", state, it.Name, it.For, it.Found)
		fmt.Fprintf(w, "         checked with: %s\n", it.Checked)
		if !it.Present {
			fmt.Fprintf(w, "         fix: %s\n", it.Fix)
		}
	}
	fmt.Fprintln(w)
	switch r.Missing {
	case 0:
		fmt.Fprintf(w, "Every item is present (%d checked). Nothing was changed.\n", len(r.Items))
	case 1:
		fmt.Fprintf(w, "1 of %d items is missing. Nothing was changed.\n", len(r.Items))
	default:
		fmt.Fprintf(w, "%d of %d items are missing. Nothing was changed.\n", r.Missing, len(r.Items))
	}
}
