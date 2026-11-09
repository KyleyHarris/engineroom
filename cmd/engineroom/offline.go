package main

import (
	"context"
	"fmt"
	"io"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/handoff"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// runOffline switches a client offline or back online, or says which it is (CONN-OFFLINE-001,
// 002). It changes only this machine's configuration: nothing is stopped either way
// (CONN-OFFLINE-003). Each switch is followed by the handoff in its direction, shown and not run
// (CONN-HANDOFF-001, 004): going offline reads the engine before switching, while it may still
// answer, and going online reads it after.
func runOffline(args []string, stdout, stderr io.Writer) int {
	// "set on|off" only switches, as the last step of a handover the app showed and ran: the switch
	// then happens only if the developer ran the handover.
	if len(args) == 2 && args[0] == "set" && (args[1] == "on" || args[1] == "off") {
		cfg, ok := engineConfig(stderr)
		if !ok {
			return 1
		}
		if cfg.Role != config.RoleClient {
			fmt.Fprintln(stderr, "engineroom: only a client can work local; this machine is the engine")
			return 1
		}
		cfg.Offline = args[1] == "on"
		if cfg.Offline {
			closeForwardsForLocal(cfg)
		}
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, map[bool]string{true: "Working local on this machine.", false: "Working on the engine again."}[cfg.Offline])
		return 0
	}
	if len(args) != 1 || (args[0] != "on" && args[0] != "off" && args[0] != "status") {
		fmt.Fprintln(stderr, "usage: engineroom offline on|off|status | engineroom offline set on|off")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if cfg.Role != config.RoleClient {
		fmt.Fprintln(stderr, "engineroom: only a client can go offline; this machine is the engine")
		return 1
	}
	var handover action.Plan
	direction := handoff.ToEngine
	switch args[0] {
	case "status":
		if cfg.Offline {
			fmt.Fprintln(stdout, "offline: working on this machine as though it were the engine")
		} else {
			fmt.Fprintf(stdout, "online: working on the engine %s\n", cfg.Engine.Host)
		}
		return 0
	case "on":
		direction = handoff.FromEngine
		handover, _ = handoff.ForClient(context.Background(), cfg, direction)
		cfg.Offline = true
		closeForwardsForLocal(cfg)
	case "off":
		cfg.Offline = false
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintf(stderr, "engineroom: saving %s: %v\n", paths.ConfigFile(), err)
		return 1
	}
	if cfg.Offline {
		fmt.Fprintln(stdout, "Offline: everything now runs on this machine as though it were the engine, until: engineroom offline off")
	} else {
		fmt.Fprintf(stdout, "Online: everything runs on the engine %s again. Sessions started here keep running.\n", cfg.Engine.Host)
		handover, _ = handoff.ForClient(context.Background(), cfg, direction)
	}
	printHandoff(stdout, handover, direction)
	return 0
}
