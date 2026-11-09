package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/handoff"
	"github.com/KyleyHarris/engineroom/internal/tui"
)

const handoffUsage = `usage:
  engineroom handoff to-engine [--json]   on a client: hand what this machine holds over to the engine
  engineroom handoff from-engine [--json] on a client: hand what the engine holds over to this machine
  engineroom handoff state --json         this machine's projects against GitHub (read by a client)
  engineroom handoff release              on the engine: push the work checked out to GitHub, and say what is uncommitted
  engineroom handoff released             record that the engine was released (the last step of a release)
  engineroom handoff pack <folder> <ref> [--label text] [--leave file]...
                                    stash a checkout's uncommitted work, new files included, as <ref> (a handover's step)
  engineroom handoff unpack <folder> <ref> [--leave file]...
                                    apply the work carried as <ref>, when the checkout is as it was made (a handover's step)
  engineroom handoff aside <folder> [--label text] [--leave file]...
                                    set a checkout's own uncommitted work aside in its stash list (a handover's step)
  engineroom handoff verify [--leave file]... <folder> <fingerprint>...
                                    check each checkout is as the handover read it, changing nothing (a handover's first step)
  engineroom mirror                       on a client: copy here the engine's projects this machine lacks,
                                    and fetch the rest, so it is ready to work local`

// runHandoff shows the handoff between a client and its engine (CONN-HANDOFF-001, 004) and runs it
// when asked, in the terminal interface like any other action. "state" is the reading a client
// asks the engine for over SSH; it reads and changes nothing.
func runHandoff(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "pack" || args[0] == "unpack" || args[0] == "aside" || args[0] == "verify") {
		return runCarry(args, stdout, stderr)
	}
	asJSON, rest := jsonFlag(args)
	if len(rest) != 1 {
		fmt.Fprintln(stderr, handoffUsage)
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	switch rest[0] {
	case "state":
		// How far the reading has got goes to standard error, one {"progress": step} line per
		// project, which the client reading this over SSH passes on to the waiting screen; the
		// reading itself is standard output, as before.
		report := handoff.ProgressLines(stderr)
		return writeJSON(stdout, handoff.ReadWith(cfg, func(done, total int, project string) {
			report(handoff.Step{Done: done, Total: total, Project: project})
		}))
	case "release":
		p := releasePlan(cfg)
		if asJSON {
			return writeJSON(stdout, map[string]any{"plan": p, "lines": p.Lines()})
		}
		printHandoff(stdout, p, "release")
		for _, o := range p.Offers {
			fmt.Fprintf(stdout, "  Offered: %s (engineroom api run %s)\n", o.Label, strings.Join(o.Action, " "))
		}
		return exitOf(runPlain(p, stdout, stderr))
	case "released":
		if err := handoff.SaveReleased(time.Now()); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	case handoff.ToEngine, handoff.FromEngine:
	default:
		fmt.Fprintln(stderr, handoffUsage)
		return 2
	}
	if !asJSON {
		return runTUI(tui.Intent{Handoff: rest[0]}, stderr)
	}
	p, err := handoff.ForClient(context.Background(), cfg, rest[0])
	if err != nil {
		return apiError(stdout, err)
	}
	return writeJSON(stdout, map[string]any{"plan": p, "lines": p.Lines()})
}

// releasePlan is the engine's release with its last step: recording when, which the engine then shows.
// The record is information only; it decides nothing.
func releasePlan(cfg *config.Config) action.Plan {
	p := handoff.Release(cfg)
	p.Commands = append(p.Commands, action.Command{Argv: []string{"engineroom", "handoff", "released"}, Why: "record when the engine was released, to show it"})
	return p
}

// runMirror is `engineroom mirror` on a client: handoff.Mirror, run.
func runMirror(stdout, stderr io.Writer) int {
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	p, err := handoff.Mirror(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	return exitOf(runPlain(p, stdout, stderr))
}

// printHandoff writes a handoff as text: what it hands over and leaves alone, then its commands,
// and how to run them. It runs nothing.
func printHandoff(w io.Writer, p action.Plan, direction string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, p.Title+":")
	for _, n := range p.Notes {
		fmt.Fprintln(w, "  "+n)
	}
	if len(p.Commands) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "These commands hand it over. Nothing has run yet.")
	for i, c := range p.Commands {
		fmt.Fprintf(w, "  %d. %s\n", i+1, c.Display())
		if c.Why != "" {
			fmt.Fprintf(w, "     %s\n", c.Why)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run them with: engineroom handoff "+direction)
}

// runCarry is a handover's steps on one machine's checkouts: `verify` (each is as the handover
// read it, handoff.Verify), `aside` (a receiving checkout's own work set aside in its stash list),
// and `pack` and `unpack`, the two ends of carrying the giving checkout's uncommitted work
// (handoff.Pack, handoff.Unpack). Each runs on the machine that holds the checkouts; the plan
// shows it, with what it does.
func runCarry(args []string, stdout, stderr io.Writer) int {
	var pos, leave []string
	label := "engineroom handover"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--leave", "--label":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, handoffUsage)
				return 2
			}
			if args[i] == "--leave" {
				leave = append(leave, args[i+1])
			} else {
				label = args[i+1]
			}
			i++
		default:
			pos = append(pos, args[i])
		}
	}
	want := map[string]bool{"pack": len(pos) == 2, "unpack": len(pos) == 2 && label == "engineroom handover", "aside": len(pos) == 1,
		"verify": len(pos) > 0 && len(pos)%2 == 0 && label == "engineroom handover"}
	if !want[args[0]] {
		fmt.Fprintln(stderr, handoffUsage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	switch args[0] {
	case "pack":
		if err := handoff.Pack(pos[0], pos[1], label, leave); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "stashed the work in %s as %s\n", pos[0], pos[1])
	case "unpack":
		if err := handoff.Unpack(pos[0], pos[1], leave); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "applied the work carried as %s in %s\n", pos[1], pos[0])
	case "aside":
		if err := handoff.Pack(pos[0], "", label, leave); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "set aside any uncommitted work in %s, as %q\n", pos[0], label)
	case "verify":
		for i := 0; i < len(pos); i += 2 {
			if err := handoff.Verify(pos[i], pos[i+1], leave); err != nil {
				return fail(err)
			}
		}
		fmt.Fprintf(stdout, "%d checkout(s) as they were read\n", len(pos)/2)
	}
	return 0
}
