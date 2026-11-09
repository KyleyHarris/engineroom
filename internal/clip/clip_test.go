package clip

import (
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// The plan saves the image here, makes the folder on the engine, copies the file there with scp under
// the engine's own ssh configuration, then puts it on the engine's clipboard, in that order. It fails if
// the copy does not use the engine's ssh configuration (a client whose engine needs it would fail), the
// file goes anywhere but EngineFile, or a step runs on the wrong machine.
func TestSendingAnImageIsSavedCopiedThenPut(t *testing.T) {
	r := action.Remote{Host: "engine", SSHConfig: "/cfg", ConnectTimeout: 5 * time.Second}
	p := SendImage(r, "/state/clipboard.png")
	lines := p.Lines()
	want := []string{
		"engineroom clipboard save /state/clipboard.png",
		"ssh -F /cfg -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 engine -- 'mkdir -p .cache/engineroom'",
		"scp -F /cfg -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 /state/clipboard.png engine:" + EngineFile,
		"ssh -F /cfg -o BatchMode=yes -o ConnectTimeout=5 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 engine -- 'engineroom clipboard put " + EngineFile + "'",
	}
	if len(lines) != len(want) {
		t.Fatalf("plan:\n%s", strings.Join(lines, "\n"))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("step %d is %q, want %q", i+1, lines[i], want[i])
		}
	}
}
