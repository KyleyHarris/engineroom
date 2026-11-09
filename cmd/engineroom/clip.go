package main

import (
	"fmt"
	"io"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/clip"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

const clipUsage = `usage:
  engineroom clipboard send          on a client: put the image on this Mac's clipboard on the engine's,
                               so pasting in a session on the engine pastes it
  engineroom clipboard save <file>   write this Mac's clipboard image to a PNG file
  engineroom clipboard put <file>    put a PNG file on this Mac's clipboard`

// runClipboard is `engineroom clipboard ...` (internal/clip says why it is needed).
func runClipboard(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 2 && args[0] == "save":
		if err := clip.Save(args[1]); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	case len(args) == 2 && args[0] == "put":
		if err := clip.Put(args[1]); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	case len(args) == 1 && args[0] == "send":
		cfg, ok := engineConfig(stderr)
		if !ok {
			return 1
		}
		p, err := sendImagePlan(cfg)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		ok = runPlain(p, stdout, stderr)
		if ok {
			fmt.Fprintln(stdout, p.Notes[0])
		}
		return exitOf(ok)
	}
	fmt.Fprintln(stderr, clipUsage)
	return 2
}

// sendImagePlan is clip.SendImage for this client's engine.
func sendImagePlan(cfg *config.Config) (action.Plan, error) {
	if cfg.Role != config.RoleClient {
		return action.Plan{}, fmt.Errorf("only a client sends its clipboard image to the engine; on the engine, paste it directly")
	}
	r := action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
	return clip.SendImage(r, clip.LocalFile(paths.StateDir())), nil
}
