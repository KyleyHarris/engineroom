package action

import (
	"context"
	"testing"
	"time"
)

// A command that leaves a background process holding its output open ends its step once the
// command has exited and the grace has passed, with what it said and its own exit code, instead
// of waiting for the background process. Fails if the step waits for the background process.
func TestABackgroundProcessDoesNotHoldAStep(t *testing.T) {
	saved := outputGrace
	outputGrace = 300 * time.Millisecond
	defer func() { outputGrace = saved }()
	start := time.Now()
	code, out, err := runOne(context.Background(), Command{Argv: []string{"sh", "-c", "sleep 20 & echo started"}}, nil)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the step waited %s for the background process", took)
	}
	if err != nil || code != 0 || out != "started" {
		t.Fatalf("code %d, output %q, error %v", code, out, err)
	}
}
