//go:build journeys

// Package journeys holds the end-to-end journeys: the built program in a real terminal,
// against real Zellij, git and SSH, each bound to an acceptance scenario's own clauses.
// Run them with scripts/journeys.sh, not go test ./..., because they need Docker for the engine.
package journeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyleyHarris/engineroom/internal/scenarios"
	"github.com/KyleyHarris/engineroom/internal/termtest"
)

// term0 captions the recording of whichever terminal the journey starts. The terminal does
// not exist until a When clause starts the program, so captions before that point are kept
// and replayed onto it.
func term0(term **termtest.Terminal) scenarios.Captioner {
	return &lateCaptioner{term: term}
}

type lateCaptioner struct {
	term    **termtest.Terminal
	pending []string
}

func (c *lateCaptioner) Caption(text string) {
	if *c.term == nil {
		c.pending = append(c.pending, text)
		return
	}
	for _, p := range c.pending {
		(*c.term).Caption(p)
	}
	c.pending = nil
	(*c.term).Caption(text)
}

// journeyOptions is the terminal a journey uses when it starts something other than the
// program itself (for example attaching to a session directly).
func journeyOptions(t *testing.T) termtest.Options {
	opts := termtest.Options{Cols: 120, Rows: 34, Timeout: 20 * time.Second, Title: t.Name()}
	if dir := os.Getenv("JOURNEY_RECORD"); dir != "" {
		opts.Record = filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "_")+".cast")
	}
	return opts
}
