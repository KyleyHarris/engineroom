package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/history"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/source"
	"github.com/KyleyHarris/engineroom/internal/tui"
)

// runStack starts or stops the app stack of a session's checkout with the repository's own
// script (WORK-STACK-001, 002). The session defaults to the one this command runs in, so the
// agent inside a session runs `engineroom up` and the stack starts the standard way.
//
// In a terminal it is the interface's run screen. Without one (the agent's shell tool, a script)
// the same plan runs and prints its commands and output as plain text, and is recorded in the
// history the same way, so the agent's run is as visible as the developer's.
func runStack(kind string, args []string, stdout, stderr io.Writer) int {
	session := os.Getenv("ZELLIJ_SESSION_NAME")
	switch len(args) {
	case 0:
	case 1:
		session = args[0]
	default:
		session = ""
	}
	if session == "" {
		fmt.Fprintf(stderr, "usage: engineroom %s [<session>]   (inside a session, the session defaults to this one)\n", kind)
		return 2
	}
	if term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stdin.Fd()) {
		return runTUI(tui.Intent{Stack: kind, StackSession: session}, stderr)
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if cfg.Role == "" {
		fmt.Fprintln(stderr, "engineroom: this machine has no role yet (engineroom role set engine)")
		return 1
	}
	src := source.For(cfg)
	// A session the stack has to start is started with Remote Control as this machine remembers
	// it for the terminal, in the background (WORK-STACK-005).
	plan, err := src.Stack(context.Background(), kind, session, ops.Start{RemoteControl: cfg.Agent.RemoteControl, NoJoin: true})
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if runPlain(plan, stdout, stderr) {
		return 0
	}
	return 1
}

// runPlain runs a plan without a terminal interface: each command is printed before it runs,
// its output after, then its outcome, and a failure is followed by what it left behind. The
// action is recorded in the history like any other.
func runPlain(plan action.Plan, stdout, stderr io.Writer) bool {
	return runPlainWith(plan, stdout, stderr, nil)
}

// runPlainWith is runPlain that also tells onStep each step's outcome, for a caller that has to
// know which commands ran (the forwarder records only the forwards that were made).
func runPlainWith(plan action.Plan, stdout, stderr io.Writer, onStep func(action.Step)) bool {
	fmt.Fprintf(stdout, "%s, on %s\n", plan.Title, plan.Machine)
	entry, herr := history.Begin(plan)
	if herr != nil {
		fmt.Fprintf(stderr, "engineroom: the history could not be written: %v\n", herr)
	}
	started := map[int]bool{}
	ok := action.Run(context.Background(), plan, func(s action.Step) {
		if entry != nil {
			entry.Update(s)
		}
		if onStep != nil {
			onStep(s)
		}
		c := plan.Commands[s.Index]
		switch s.Status {
		case action.StatusRunning:
			// Running is reported again as output arrives; the command is printed once, and
			// its output in full when it finishes.
			if !started[s.Index] {
				started[s.Index] = true
				fmt.Fprintf(stdout, "$ %s\n", c.Display())
			}
		case action.StatusOK:
			writeOutput(stdout, s.Output)
			fmt.Fprintln(stdout, "ok, done")
		case action.StatusFailed:
			writeOutput(stdout, s.Output)
			fmt.Fprintf(stdout, "FAILED (exit %d)\n", s.Exit)
			if c.OnFail != "" {
				fmt.Fprintln(stdout, c.OnFail)
			}
		case action.StatusNotRun:
			fmt.Fprintf(stdout, "not run: %s\n", c.Display())
		}
	})
	if entry != nil {
		status := history.Done
		if !ok {
			status = history.Failed
		}
		if err := entry.Finish(status); err != nil && herr == nil {
			fmt.Fprintf(stderr, "engineroom: the history could not be written: %v\n", err)
		}
	}
	return ok
}

func writeOutput(w io.Writer, out string) {
	if out = strings.TrimRight(out, "\n"); out != "" {
		fmt.Fprintln(w, out)
	}
}

// runTask is `engineroom task [<session>] <name>` and `engineroom task list [<session>]`: one of a project's
// own named tasks (engineroom.toml's [[task]]), run in a session's checkout and recorded in the history;
// inside a session the session defaults to this one, so Claude runs a task the way the app does.
func runTask(args []string, stdout, stderr io.Writer) int {
	usage := "usage: engineroom task [<session>] <task name> | engineroom task list [<session>]   (inside a session, the session defaults to this one)"
	session := os.Getenv("ZELLIJ_SESSION_NAME")
	list := len(args) > 0 && args[0] == "list"
	if list {
		args = args[1:]
	}
	name := ""
	switch {
	case list && len(args) == 1:
		session = args[0]
	case list && len(args) == 0:
	case !list && len(args) == 1:
		name = args[0]
	case !list && len(args) == 2:
		session, name = args[0], args[1]
	default:
		session = ""
	}
	if session == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if list {
		tasks, err := ops.Tasks(cfg, session)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		if len(tasks) == 0 {
			fmt.Fprintln(stdout, "No tasks. Name them in the project's engineroom.toml in your settings: [[task]] name, about, run.")
		}
		for _, t := range tasks {
			fmt.Fprintf(stdout, "%s\t%s\n", t.Name, t.About)
		}
		return 0
	}
	plan, err := ops.Task(cfg, session, name)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	plan.Machine = "this machine"
	return exitOf(runPlain(plan, stdout, stderr))
}
