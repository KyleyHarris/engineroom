package main

import (
	"fmt"
	"io"

	"github.com/KyleyHarris/engineroom/internal/settings"
)

// runRoles is `engineroom roles <project>` and `engineroom roles add|remove <project> <role>`: the worktrees
// of a project kept for a role rather than for one branch (docs/decisions.md D-024), recorded in
// the developer's settings so every machine that syncs them knows the same roles. A role worktree
// is made by `engineroom api run new-role`, whose plan records the role with this command, and clean up
// forgets it the same way, so each is a command the developer sees.
func runRoles(args []string, stdout, stderr io.Writer) int {
	usage := "usage: engineroom roles <project> | engineroom roles add|remove <project> <role>"
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	switch {
	case len(args) == 1:
		p, found := cfg.Project(args[0])
		if !found {
			fmt.Fprintf(stderr, "engineroom: %q is not a registered project\n", args[0])
			return 1
		}
		roles, err := settings.Roles(p.Name, p.Path)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		for _, r := range roles {
			fmt.Fprintln(stdout, r)
		}
		return 0
	case len(args) == 3 && (args[0] == "add" || args[0] == "remove"):
		p, found := cfg.Project(args[1])
		if !found {
			fmt.Fprintf(stderr, "engineroom: %q is not a registered project\n", args[1])
			return 1
		}
		if err := settings.SetRole(p.Name, p.Path, args[2], args[0] == "add"); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, usage)
	return 2
}
