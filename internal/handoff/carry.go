package handoff

import (
	"fmt"
	"strings"
)

// Helpers a handover's plan uses to find and name checkouts and branches.

// describeChanges names a checkout's uncommitted work for a note: its changes, the first few by
// name.
func describeChanges(c Checkout) string {
	if len(c.Changes) == 0 {
		return "uncommitted changes"
	}
	const named = 6
	list := c.Changes
	more := ""
	if len(list) > named {
		more = fmt.Sprintf(" and %d more", len(list)-named)
		list = list[:named]
	}
	n := "1 change"
	if len(c.Changes) != 1 {
		n = fmt.Sprintf("%d changes", len(c.Changes))
	}
	return n + " (" + strings.Join(list, ", ") + more + ")"
}

func branchOf(r Repo, name string) Branch {
	for _, b := range r.Branches {
		if b.Name == name {
			return b
		}
	}
	return Branch{Name: name}
}

func checkoutOn(r Repo, branch string) (Checkout, bool) {
	for _, c := range r.Checkouts {
		if c.Branch == branch {
			return c, true
		}
	}
	return Checkout{}, false
}

func checkoutAt(r Repo, path string) (Checkout, bool) {
	for _, c := range r.Checkouts {
		if c.Path == path {
			return c, true
		}
	}
	return Checkout{}, false
}

func orDetached(branch string) string {
	if branch == "" {
		return "a detached commit"
	}
	return branch
}
