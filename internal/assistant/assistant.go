// Package assistant opens a Claude session that already knows what the developer was looking at in
// the app: a project or one of its worktrees, its slot, ports, links, scripts and settings, and
// how engineroom wants them changed, so asking it to change a start-up script needs no explaining.
// It runs on the engine, in the checkout, with the developer's settings folder open to it,
// in a session of its own beside the checkout's main one.
package assistant

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// guide is how engineroom wants things done, given to every assistant: the commands, the rules for
// scripts, settings, ports and secrets.
//
//go:embed guide.md
var guide string

// SessionName is the assistant session for a checkout: the checkout's own session name with -ai,
// kept within Zellij's limit.
func SessionName(checkout string) string {
	base := zj.SessionName(checkout)
	if len(base) > zj.MaxNameLength-3 {
		base = base[:zj.MaxNameLength-3]
	}
	return base + "-ai"
}

// Context is what the assistant is told about what was opened: the guide, then the project and
// the checkout as the project page reads them.
func Context(page ops.ProjectPage, c ops.CheckoutView, settingsDir string) string {
	var b strings.Builder
	b.WriteString(guide)
	b.WriteString("\n## What was opened\n\n")
	fmt.Fprintf(&b, "- Project: %s", page.Name)
	if page.Identity != "" {
		fmt.Fprintf(&b, " (GitHub %s)", page.Identity)
	}
	b.WriteString("\n")
	if c.Main {
		fmt.Fprintf(&b, "- Checkout: the project's main clone, %s, on branch %s\n", c.Path, orNone(c.Branch))
	} else {
		fmt.Fprintf(&b, "- Checkout: the worktree %s, on branch %s (work name %q)\n", c.Path, orNone(c.Branch), c.Work)
	}
	fmt.Fprintf(&b, "- Its session: %s (%s)\n", c.Session, map[bool]string{true: "running", false: "not running"}[c.Running])
	if c.Slot > 0 {
		var ports []string
		for _, p := range c.Ports {
			ports = append(ports, fmt.Sprintf("%s %d", p.Name, p.Number))
		}
		fmt.Fprintf(&b, "- Slot %d: %s\n", c.Slot, strings.Join(ports, ", "))
	} else {
		b.WriteString("- No ports: the project's settings name none yet\n")
	}
	for _, l := range c.Links {
		fmt.Fprintf(&b, "- Link %s: %s\n", l.Name, l.URL)
	}
	fmt.Fprintf(&b, "- Settings folder for this project: %s\n", settingsDir)
	for _, s := range page.Scripts {
		if s.From == "" {
			fmt.Fprintf(&b, "- Script %s: none yet\n", s.Name)
		} else {
			fmt.Fprintf(&b, "- Script %s: %s (from %s)\n", s.Name, s.Path, s.From)
		}
	}
	for _, t := range page.Tasks {
		fmt.Fprintf(&b, "- Task %q: %s (run it with: engineroom task %s %q)\n", t.Name, t.About, c.Session, t.Name)
	}
	for _, s := range page.Secrets {
		fmt.Fprintf(&b, "- Secret file %s: %s\n", s.Name, s.State)
	}
	if len(page.Reserve) > 0 {
		fmt.Fprintf(&b, "- Reserved ports: %s; %d slot(s)\n", strings.Join(page.Reserve, ", "), page.Slots)
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// Layout is the assistant session's layout: one tab running the agent with the context appended to
// its system prompt, the settings folder open to it, and an opening question; with remoteControl,
// under Remote Control by the session's own name. It never continues the checkout's own
// conversation: the assistant is a session of its own.
//
// --add-dir comes first: Claude Code's --add-dir takes any number of folders, so whatever follows
// it up to the next option is taken as one more folder. Last before the opening, it would take the
// opening as a folder whenever Remote Control was off, and the session would sit at an empty
// prompt. The options after it each take one value, so the opening is the command's prompt.
func Layout(agentCommand, contextFile, settingsRoot, opening string, remoteControl bool) string {
	cmd := agentCommand + " --add-dir " + shQuote(settingsRoot) + " --append-system-prompt-file " + shQuote(contextFile)
	if remoteControl {
		// Named as well, so it shows under the session's name (as zj.Layout).
		cmd += ` --name "$ZELLIJ_SESSION_NAME" --remote-control "$ZELLIJ_SESSION_NAME"`
	}
	cmd += " " + shQuote(opening)
	return fmt.Sprintf(`layout {
    default_tab_template {
        pane size=1 borderless=true {
            plugin location="zellij:tab-bar"
        }
        children
        pane size=2 borderless=true {
            plugin location="zellij:status-bar"
        }
    }
    tab name="assistant" focus=true {
        pane name="assistant" focus=true command="sh" {
            args "-c" %s
        }
    }
}
`, kdl(cmd))
}

// Opening is the first thing the assistant is asked, so it says what it is looking at and asks
// what to do.
func Opening(page ops.ProjectPage, c ops.CheckoutView) string {
	where := "its main clone"
	if !c.Main {
		where = "the worktree on " + orNone(c.Branch)
	}
	return fmt.Sprintf("I opened %s, %s, from the engineroom app. Say in one line what you can see, then ask me what I want to do.", page.Name, where)
}

// Write puts the context and the layout for a checkout's assistant in dir, returning their paths.
func Write(dir, name, context, layout string) (contextFile, layoutFile string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	contextFile = filepath.Join(dir, name+".md")
	layoutFile = filepath.Join(dir, name+".kdl")
	if err := os.WriteFile(contextFile, []byte(context), 0o644); err != nil {
		return "", "", err
	}
	return contextFile, layoutFile, os.WriteFile(layoutFile, []byte(layout), 0o644)
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func kdl(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
