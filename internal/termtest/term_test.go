package termtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The driver must see what a person would see: text the program draws, input it reads, and
// cursor-addressed redraws. If the emulator were bypassed, a program that clears and redraws
// would still "contain" its old text in the raw stream, and this test would catch that.
func TestScreenShowsRedrawnTextAndTypedInput(t *testing.T) {
	script := `printf 'first frame'; read line; printf '\033[2J\033[Hsecond frame: %s\n' "$line"; read done`
	term := Start(t, exec.Command("sh", "-c", script), Options{Cols: 60, Rows: 10, Timeout: 5 * time.Second})

	term.WaitFor("first frame")
	term.Type("hello")
	term.Press(Enter)
	term.WaitFor("second frame: hello")
	if strings.Contains(term.Screen(), "first frame") {
		t.Fatalf("the cleared frame is still on the emulated screen:\n%s", term.Screen())
	}
	term.Press(Enter)
	if code := term.WaitExit(); code != 0 {
		t.Fatalf("exit code %d", code)
	}
}

// A wait must fail, with the screen, when the text never appears. A driver whose wait could
// not fail would make every journey pass.
func TestWaitForFailsWhenTextNeverAppears(t *testing.T) {
	ft := &fakeT{TB: t}
	term := Start(ft, exec.Command("sh", "-c", "printf 'only this'; sleep 5"), Options{Cols: 40, Rows: 5, Timeout: 300 * time.Millisecond})
	func() {
		defer func() { _ = recover() }()
		term.WaitFor("never shown")
	}()
	if !ft.fatal {
		t.Fatal("WaitFor returned without failing for text that never appeared")
	}
	if !strings.Contains(ft.msg, "only this") {
		t.Fatalf("the failure did not include the screen; got %q", ft.msg)
	}
}

// A recording is written, with its caption track, on the same clock.
func TestRecordingWritesBothTracks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec.cast")
	term := Start(t, exec.Command("sh", "-c", "printf 'recorded'; read x"), Options{Cols: 40, Rows: 5, Record: path, Title: "test"})
	term.Caption("SAMPLE-001 a caption")
	term.WaitFor("recorded")
	term.Press(Enter)
	term.WaitExit()
	term.stop()

	main, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := os.ReadFile(captionsPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "recorded") || !strings.Contains(string(main), `"m","SAMPLE-001 a caption"`) {
		t.Fatalf("main track is missing output or the marker:\n%s", main)
	}
	if !strings.Contains(string(caps), "SAMPLE-001 a caption") {
		t.Fatalf("caption track is missing the caption:\n%s", caps)
	}
}

// fakeT records a Fatalf instead of ending the real test, so a failure can be asserted.
type fakeT struct {
	testing.TB
	fatal bool
	msg   string
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatal = true
	f.msg = strings.TrimSpace(fmt.Sprintf(format, args...))
	panic("fatal")
}

func (f *fakeT) Helper() {}

// A phrase is found whether the terminal broke it mid-word or the program wrapped it at a
// space, and a phrase that is not there is still not found. Would catch a wait that again
// depends on where a line happens to break.
func TestOnScreenAcrossRows(t *testing.T) {
	for _, screen := range []string{"was created, but its setup failed", "was created, but its se\ntup failed", "was created, but its\nsetup failed"} {
		if !onScreen(screen, "its setup failed") {
			t.Errorf("not found in %q", screen)
		}
	}
	if onScreen("its setup\nworked", "its setup failed") {
		t.Error("found a phrase that is not there")
	}
}
