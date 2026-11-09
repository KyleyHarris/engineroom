package action

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func collect(p Plan, ctx context.Context) ([]Step, bool) {
	var steps []Step
	ok := Run(ctx, p, func(s Step) { steps = append(steps, s) })
	return steps, ok
}

func final(steps []Step) map[int]Status {
	out := map[int]Status{}
	for _, s := range steps {
		out[s.Index] = s.Status
	}
	return out
}

// Learning obligation O2 / LEARN-SHOW-003: a failing command stops the plan, its output is
// kept, and every later command is reported as not run. Fails if a later command runs, or
// if the run is reported as successful.
func TestRunStopsAtTheFirstFailure(t *testing.T) {
	marker := t.TempDir() + "/ran"
	p := Plan{Title: "t", Commands: []Command{
		{Argv: []string{"true"}},
		{Argv: []string{"sh", "-c", "echo broken >&2; exit 3"}},
		{Argv: []string{"touch", marker}},
	}}
	steps, ok := collect(p, context.Background())
	if ok {
		t.Fatal("a plan with a failing command was reported as successful")
	}
	got := final(steps)
	if got[0] != StatusOK || got[1] != StatusFailed || got[2] != StatusNotRun {
		t.Fatalf("statuses %v", got)
	}
	for _, s := range steps {
		if s.Index == 1 && s.Status == StatusFailed && (s.Exit != 3 || !strings.Contains(s.Output, "broken")) {
			t.Fatalf("failure lost its exit code or output: %+v", s)
		}
	}
	if err := exec.Command("test", "-e", marker).Run(); err == nil {
		t.Fatal("a command after the failure ran")
	}
}

// Learning obligation O5: cancelling stops the running command, marks it interrupted and
// the rest not run. Fails if the plan waits for the slow command or reports success.
func TestRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := Plan{Commands: []Command{{Argv: []string{"sleep", "30"}}, {Argv: []string{"true"}}}}
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start := time.Now()
	steps, ok := collect(p, ctx)
	if ok || time.Since(start) > 10*time.Second {
		t.Fatalf("interrupt not honoured: ok=%v after %v", ok, time.Since(start))
	}
	if got := final(steps); got[0] != StatusInterrupted || got[1] != StatusNotRun {
		t.Fatalf("statuses %v", got)
	}
}

func TestInteractiveIsHandedOverNotRun(t *testing.T) {
	marker := t.TempDir() + "/ran"
	p := Plan{Commands: []Command{{Argv: []string{"true"}}, {Argv: []string{"touch", marker}, Interactive: true}}}
	steps, ok := collect(p, context.Background())
	if !ok || final(steps)[1] != StatusHandedOver {
		t.Fatalf("ok=%v steps=%v", ok, steps)
	}
	if exec.Command("test", "-e", marker).Run() == nil {
		t.Fatal("the interactive command was run by the runner instead of handed over")
	}
}

func TestValidateRefusesAnInteractiveCommandBeforeTheEnd(t *testing.T) {
	p := Plan{Title: "x", Commands: []Command{{Argv: []string{"a"}, Interactive: true}, {Argv: []string{"b"}}}}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "not last") {
		t.Fatalf("got %v", err)
	}
	if err := (Plan{Commands: []Command{{}}}).Validate(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty command accepted: %v", err)
	}
}

// Output beyond the limit is cut to its tail, so a chatty command cannot grow memory or the
// history without bound (learning obligation O9), and the cut is said, not hidden.
func TestOutputIsBounded(t *testing.T) {
	p := Plan{Commands: []Command{{Argv: []string{"sh", "-c", "yes x | head -c 100000; echo END"}}}}
	steps, _ := collect(p, context.Background())
	last := steps[len(steps)-1]
	if len(last.Output) > outputLimit+64 || !strings.HasSuffix(last.Output, "END") || !strings.HasPrefix(last.Output, "[earlier output cut]") {
		t.Fatalf("output %d bytes, prefix %q", len(last.Output), last.Output[:30])
	}
}

// Connection obligation O8: a command sent to the engine arrives as the same arguments, however
// awkward. The remote side of ssh hands the string to a shell, so this runs the exact string
// Wrap produced through sh and compares what arrives.
func TestWrapDeliversTheSameArguments(t *testing.T) {
	r := Remote{Host: "engine", ConnectTimeout: 5 * time.Second}
	argv := []string{"printf", "%s\\0", "two words", "it's", "$(id)", "a;b", "new\nline", ""}
	wrapped := r.Wrap(Command{Argv: argv, Dir: "/tmp"})
	if wrapped.Argv[0] != "ssh" || wrapped.Argv[len(wrapped.Argv)-2] != "--" {
		t.Fatalf("not an ssh command: %v", wrapped.Argv)
	}
	remote := wrapped.Argv[len(wrapped.Argv)-1]
	out, err := exec.Command("sh", "-c", remote).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !reflect.DeepEqual(got, argv[2:]) {
		t.Fatalf("arrived as %q, want %q", got, argv[2:])
	}
}

func TestWrapNeverPromptsAndBoundsTheWait(t *testing.T) {
	w := Remote{Host: "engine", SSHConfig: "/x/config", ConnectTimeout: 2 * time.Second}.Probe()
	joined := strings.Join(w.Argv, " ")
	for _, want := range []string{"-F /x/config", "BatchMode=yes", "ConnectTimeout=2", "engine -- true"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("probe %q lacks %q", joined, want)
		}
	}
	if j := (Remote{Host: "engine"}).Wrap(Command{Argv: []string{"zellij", "attach", "a"}, Interactive: true}); !strings.Contains(strings.Join(j.Argv, " "), " -t engine") {
		t.Fatalf("an interactive command is not given a terminal: %v", j.Argv)
	}
}
