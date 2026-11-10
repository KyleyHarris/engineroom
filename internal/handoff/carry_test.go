package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Carrying uncommitted work (D-017): Pack and Unpack run for real against the
// test world's clones, with the stash sent between them as the handover sends it (a fetch of its
// ref).

// The work in a checkout (a changed tracked file and a new file) is stashed on the giving machine,
// sent, and applied on the receiving one; the slot file engineroom wrote for the giving machine
// stays there, and the giving machine keeps a copy in its stash list. Fails if either change does
// not arrive, the slot file travels or leaves, the giving checkout keeps the changes, the giver's
// stash list lacks it, or the ref survives on the receiver.
func TestPackAndUnpackCarryChangesAndNewFiles(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `echo edit >> f && echo new > fresh.txt && echo '{"slot":1}' > .engineroom-slot.json`)
	leave := []string{".engineroom-slot.json"}
	ref := HandoverRef("main")
	if err := Pack(w.a, ref, "engineroom handover of main to the engine", leave); err != nil {
		t.Fatal(err)
	}
	if st := w.sh(w.a, `git status --porcelain`); st != "?? .engineroom-slot.json" {
		t.Fatalf("the giving checkout after packing: %q", st)
	}
	if list := w.sh(w.a, `git stash list`); !strings.Contains(list, "engineroom handover of main to the engine") {
		t.Fatalf("the giving machine keeps no copy: %q", list)
	}
	w.sh(w.b, `git fetch -q "$1" "+$2:$2"`, w.a, ref)
	if err := Unpack(w.b, ref, nil); err != nil {
		t.Fatal(err)
	}
	if got := w.sh(w.b, `cat f fresh.txt`); got != "one\nedit\nnew" {
		t.Fatalf("the receiving checkout has %q", got)
	}
	if _, err := os.Stat(filepath.Join(w.b, ".engineroom-slot.json")); err == nil {
		t.Fatal("the giving machine's slot file travelled")
	}
	if _, err := git(w.b, "rev-parse", "-q", "--verify", ref); err == nil {
		t.Fatal("the ref is still there after the work was applied")
	}
}

// Partial failure, and an old stash: with nothing to stash (the work was committed after the plan
// was made), Pack fails and names no ref, so an earlier stash is never sent in its place. Fails if
// Pack succeeds or the ref points anywhere.
func TestPackWithNothingToStashSendsNothing(t *testing.T) {
	w := newWorld(t)
	w.sh(w.a, `echo old >> f && git stash -q`)
	err := Pack(w.a, HandoverRef("main"), "label", nil)
	if err == nil || !strings.Contains(err.Error(), "nothing to stash") {
		t.Fatalf("err = %v", err)
	}
	if _, err := git(w.a, "rev-parse", "-q", "--verify", HandoverRef("main")); err == nil {
		t.Fatal("a ref was named for an old stash")
	}
}

// The receiving checkout is left as it is when the work cannot land as it was made: it has changes
// of its own, or it is at another commit. Each time the work stays under its ref with the command
// to apply it. Fails if Unpack applies, changes the checkout, or drops the ref.
func TestUnpackLeavesACheckoutThatIsNotAsPlanned(t *testing.T) {
	w := newWorld(t)
	ref := HandoverRef("main")
	w.sh(w.a, `echo edit >> f`)
	if err := Pack(w.a, ref, "label", nil); err != nil {
		t.Fatal(err)
	}
	w.sh(w.b, `git fetch -q "$1" "+$2:$2" && echo mine > mine.txt`, w.a, ref)
	err := Unpack(w.b, ref, nil)
	if err == nil || !strings.Contains(err.Error(), "has changes of its own (?? mine.txt)") || !strings.Contains(err.Error(), "stash apply "+ref) {
		t.Fatalf("err = %v", err)
	}
	if got := w.sh(w.b, `cat f`); got != "one" {
		t.Fatalf("the checkout was changed: %q", got)
	}
	w.sh(w.b, `rm mine.txt && echo other >> f && git commit -qam other`)
	err = Unpack(w.b, ref, nil)
	if err == nil || !strings.Contains(err.Error(), "where the work was made") {
		t.Fatalf("err = %v", err)
	}
	if _, err := git(w.b, "rev-parse", "-q", "--verify", ref); err != nil {
		t.Fatal("the ref was dropped though the work was not applied")
	}
}
