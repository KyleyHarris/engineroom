package settings

// A project's secret files (D-014): the files a checkout needs that are never committed, named in
// engineroom.toml's secrets list and kept in the project's settings folder under secrets/, where the
// settings repository encrypts them. Save copies them from a checkout into the settings; restore
// copies them back into a checkout that lacks them, so a new worktree, or another machine, has
// them without anyone copying files by hand. Both are plans of real commands.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// SecretState is one secret file: where it lives in the checkout, where its saved copy lives,
// and how the two compare.
type SecretState struct {
	Name     string
	Checkout string
	Saved    string
	// InCheckout and IsSaved say which copies exist; Same is true when both exist with the same
	// content.
	InCheckout, IsSaved, Same bool
}

// secretPath is where a secret named name lives for checkout: inside it, or in the home folder
// for a "~/" name.
func secretPath(name, checkout string) string {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, filepath.FromSlash(rest))
	}
	return filepath.Join(checkout, filepath.FromSlash(name))
}

// savedPath is where a secret's copy is kept in the settings: under secrets/, a home-folder one
// under secrets/home/ so it can never meet a worktree path of the same name.
func savedPath(p Project, name string) string {
	if rest, ok := strings.CutPrefix(name, "~/"); ok {
		return filepath.Join(p.Dir, "secrets", "home", filepath.FromSlash(rest))
	}
	return filepath.Join(p.Dir, "secrets", "worktree", filepath.FromSlash(name))
}

// Secrets compares each of a project's secret files in checkout with its saved copy.
func Secrets(p Project, checkout string) []SecretState {
	var out []SecretState
	for _, name := range p.Secrets {
		st := SecretState{Name: name, Checkout: secretPath(name, checkout), Saved: savedPath(p, name)}
		a, errA := os.ReadFile(st.Checkout)
		b, errB := os.ReadFile(st.Saved)
		st.InCheckout, st.IsSaved = errA == nil, errB == nil
		st.Same = st.InCheckout && st.IsSaved && bytes.Equal(a, b)
		out = append(out, st)
	}
	return out
}

// copyCommand copies one file, making its folder first, and keeps it readable by its owner
// alone, as a secret should be.
func copyCommand(from, to, why string) action.Command {
	return action.Command{
		Argv: []string{"sh", "-c", `mkdir -p "$(dirname "$2")" && cp "$1" "$2" && chmod 600 "$2"`, "sh", from, to},
		Why:  why,
	}
}

// SavePlan copies each secret file the checkout has into the settings, where the next sync
// commits it encrypted. A file already saved with the same content is left alone; one the
// checkout lacks is said and skipped.
func SavePlan(p Project, checkout string) action.Plan {
	plan := action.Plan{Title: "Save " + p.Name + "'s secret files to your settings", Machine: "this machine"}
	for _, s := range Secrets(p, checkout) {
		switch {
		case !s.InCheckout:
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s is not in %s, so it is not saved.", s.Name, checkout))
		case s.Same:
			plan.Notes = append(plan.Notes, s.Name+" is already saved as it is.")
		default:
			plan.Commands = append(plan.Commands, copyCommand(s.Checkout, s.Saved, "save "+s.Name+"; the next sync commits it encrypted"))
		}
	}
	return plan
}

// RestorePlan copies each saved secret into a checkout that lacks it. A file the checkout
// already has is never overwritten: one that differs from the saved copy is said, with both
// paths, for the developer to compare.
func RestorePlan(p Project, checkout string) action.Plan {
	plan := action.Plan{Title: "Put " + p.Name + "'s secret files in place", Machine: "this machine"}
	for _, s := range Secrets(p, checkout) {
		switch {
		case !s.IsSaved:
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s has not been saved to your settings yet (engineroom secrets save %s).", s.Name, p.Name))
		case s.Same:
		case s.InCheckout:
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s differs from your saved copy and was left as it is; compare %s with %s.", s.Name, s.Checkout, s.Saved))
		default:
			plan.Commands = append(plan.Commands, copyCommand(s.Saved, s.Checkout, "put "+s.Name+" in place from your settings"))
		}
	}
	return plan
}
