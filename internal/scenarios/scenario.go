// Package scenarios binds journeys to the acceptance criteria in acceptance/.
//
// The types in scenarios_gen.go are generated from the acceptance documents by
// tools/gen-scenarios, never written by hand. Each scenario becomes one struct with one
// function field per clause, named from the clause's own words. That is what ties a journey to
// the requirement it proves:
//
//   - a reworded or invented clause changes or removes a field, so the journey stops compiling;
//   - a clause the journey leaves out is a nil field, caught by tools/clausecheck in the gate
//     and again by Run before any step executes.
//
// Field names come from the raw clause text, with glossary markers reduced to the term's key,
// so renaming a glossary term changes what is displayed but never breaks a journey.
package scenarios

import (
	"fmt"
	"testing"
)

// Step is one clause of a scenario in document order. Text is the clause as written, glossary
// markers included; Display is the same clause as a reader sees it, used for captions.
type Step struct {
	Text    string
	Display string
	Fn      func()
}

// Scenario is the generated description of one acceptance scenario together with the clause
// functions a journey supplied for it.
type Scenario struct {
	ID      string
	Title   string
	Feature string
	Status  string
	Persona string
	Intent  string
	Source  string
	Steps   []Step
}

// Bound is implemented by every generated scenario type.
type Bound interface {
	Scenario() Scenario
}

// Captioner receives each clause as it starts, so a recording can show which part of the
// requirement the screen is proving at that moment. A nil Captioner is allowed.
type Captioner interface {
	Caption(text string)
}

// Run executes every clause of the scenario in document order.
//
// All clauses are checked for a function before the first one runs: a journey that skips a
// clause must fail as a whole, never pass on the clauses it did implement. Each clause runs
// in the calling test's goroutine so t.Fatal inside a clause stops the journey at that clause.
func Run(t testing.TB, b Bound, c Captioner) {
	t.Helper()
	s := b.Scenario()
	for _, step := range s.Steps {
		if step.Fn == nil {
			t.Fatalf("%s: the journey does not implement the clause %q", s.ID, step.Display)
		}
	}
	if c != nil {
		c.Caption(fmt.Sprintf("%s  %s", s.ID, s.Title))
	}
	for _, step := range s.Steps {
		if c != nil {
			c.Caption(step.Display)
		}
		t.Logf("%s | %s", s.ID, step.Display)
		step.Fn()
		if t.Failed() {
			t.Fatalf("%s: failed at %q", s.ID, step.Display)
		}
	}
}
