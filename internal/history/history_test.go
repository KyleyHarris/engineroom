package history

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
)

func useState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func plan(title string) action.Plan {
	return action.Plan{Title: title, Commands: []action.Command{{Argv: []string{"echo", "a b"}}, {Argv: []string{"true"}}}}
}

// Learning obligation O1: an action is in the history before its first command runs, so a
// crash still leaves a record. Fails if Begin writes nothing.
func TestBeginRecordsBeforeRunning(t *testing.T) {
	useState(t)
	e, err := Begin(plan("Open project-a"))
	if err != nil {
		t.Fatal(err)
	}
	all, _, _ := Read()
	if len(all) != 1 || all[0].ID != e.ID || all[0].Status != Running || all[0].Commands[0].Display != "echo 'a b'" {
		t.Fatalf("history after Begin: %+v", all)
	}
	e.Update(action.Step{Index: 0, Status: action.StatusOK})
	e.Update(action.Step{Index: 1, Status: action.StatusFailed, Exit: 2, Output: "no"})
	if err := e.Finish(Failed); err != nil {
		t.Fatal(err)
	}
	all, _, _ = Read()
	if len(all) != 1 || all[0].Status != Failed || all[0].Commands[1].Exit != 2 {
		t.Fatalf("the finished entry did not replace the started one: %+v", all)
	}
}

// Learning obligation O4: a write waits for any other writer's lock, so two copies of the
// program can never interleave a line. The test holds the lock itself and checks that a write
// does not land until it is released; it fails if writes ignore the lock.
func TestWritesWaitForTheLock(t *testing.T) {
	useState(t)
	first, err := Begin(plan("first"))
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	f, err := os.OpenFile(File(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = Begin(plan("second"))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("a write landed while another writer held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the write never landed after the lock was released")
	}
	all, bad, _ := Read()
	if len(all) != 2 || bad != 0 {
		t.Fatalf("entries=%d unreadable=%d", len(all), bad)
	}
}

// Many writers at once lose nothing.
func TestConcurrentWritersLoseNothing(t *testing.T) {
	useState(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := Begin(plan(strings.Repeat("x", 2000)))
			if err == nil {
				_ = e.Finish(Done)
			}
		}()
	}
	wg.Wait()
	all, bad, err := Read()
	if err != nil || bad != 0 || len(all) != 40 {
		t.Fatalf("entries=%d unreadable=%d err=%v", len(all), bad, err)
	}
}

// Learning obligation O8: a damaged history is read as far as it is valid.
func TestDamagedLinesAreSkippedAndCounted(t *testing.T) {
	useState(t)
	e, _ := Begin(plan("good"))
	_ = e.Finish(Done)
	f, _ := os.OpenFile(File(), os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("{not json\n\n{\"id\":\"\"}\n")
	_ = f.Close()
	all, bad, err := Read()
	if err != nil || len(all) != 1 || bad != 3 {
		t.Fatalf("entries=%d unreadable=%d err=%v", len(all), bad, err)
	}
}

// Learning obligation O6: a history that cannot be written is an error for the caller to
// report, never a panic or a hang.
func TestUnwritableHistoryIsAnError(t *testing.T) {
	dir := useState(t)
	locked := filepath.Join(dir, "engineroom")
	if err := os.MkdirAll(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := Begin(plan("x")); err == nil {
		t.Fatal("writing into a read-only directory reported success")
	}
}

func TestFindByPrefix(t *testing.T) {
	useState(t)
	e, _ := Begin(plan("x"))
	got, ok, _ := Find(e.ID[:len(e.ID)-4])
	if !ok || got.ID != e.ID {
		t.Fatalf("prefix lookup failed: %v %v", ok, got.ID)
	}
	if _, ok, _ := Find("nope"); ok {
		t.Fatal("found an id that does not exist")
	}
}
