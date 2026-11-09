package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/roots"
	"github.com/KyleyHarris/engineroom/internal/shell"
)

// The screens that bring repositories in from GitHub: the roots, one root's repositories, and
// the short form before a clone or a new repository. Reads (the roots, the listing) are shown
// with the command that made them; clones and creations are plans like every other action.

func (m Model) loadRoots() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		l, err := src.Roots(context.Background())
		return rootsMsg{list: l, err: err}
	}
}

func (m Model) loadListing(root string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		l, err := src.Browse(context.Background(), root)
		return browseMsg{listing: l, err: err}
	}
}

// gotListing shows a root's repositories, or why they could not be read and the command that
// fixes it (PROJ-BROWSE-002). The engine not answering is the usual engine-down prompt.
func (m Model) gotListing(msg browseMsg) (tea.Model, tea.Cmd) {
	m.listed = true
	m.ghProblem = nil
	if msg.err != nil {
		var p *roots.Problem
		if errors.As(msg.err, &p) {
			m.ghProblem = p
			return m, nil
		}
		return m.problem(msg.err)
	}
	m.listing = msg.listing
	m.cursor = 0
	return m, nil
}

func (m Model) rootsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		m.screen, m.cursor = screenProjects, 0
		return m, m.loadProjects()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.roots)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor < len(m.roots) {
			return m.browse(m.roots[m.cursor].Name)
		}
	}
	return m, nil
}

// browse opens one root's listing, read fresh each time so what is marked as here is current.
func (m Model) browse(root string) (tea.Model, tea.Cmd) {
	m.screen, m.browseRoot, m.filter, m.cursor = screenBrowse, root, "", 0
	m.listed, m.ghProblem, m.listing, m.message = false, nil, roots.Listing{}, ""
	return m, m.loadListing(root)
}

// shown is the listing's repositories that match the filter, by name or description, ignoring
// case.
func (m Model) shown() []roots.Repo {
	if m.filter == "" {
		return m.listing.Repos
	}
	f := strings.ToLower(m.filter)
	var out []roots.Repo
	for _, r := range m.listing.Repos {
		if strings.Contains(strings.ToLower(r.Name), f) || strings.Contains(strings.ToLower(r.Description), f) {
			out = append(out, r)
		}
	}
	return out
}

// browseKey moves over the repositories and filters them as letters are typed; the actions are
// on keys a name cannot hold, so typing never starts one.
func (m Model) browseKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	shown := m.shown()
	switch k.Type {
	case tea.KeyEsc:
		if m.filter != "" {
			m.filter, m.cursor = "", 0
			return m, nil
		}
		m.screen, m.cursor = screenRoots, 0
		return m, m.loadRoots()
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(shown)-1 {
			m.cursor++
		}
	case tea.KeyBackspace:
		if r := []rune(m.filter); len(r) > 0 {
			m.filter, m.cursor = string(r[:len(r)-1]), 0
		}
	case tea.KeyRunes:
		m.filter += string(k.Runes)
		m.cursor = 0
	case tea.KeyCtrlR:
		return m.browse(m.browseRoot)
	case tea.KeyCtrlN:
		// A new repository for this root's owner (PROJ-CREATE-001).
		m.form = repoForm{create: true}
		m.screen = screenRepoForm
	case tea.KeyEnter:
		if m.cursor >= len(shown) {
			return m, nil
		}
		r := shown[m.cursor]
		if r.Here != "" {
			// Already on this machine: cloning it again could only fail or clone over it.
			m.message = fmt.Sprintf("%s is already in this root; nothing to add.", r.Name)
			if r.Here == "folder" {
				// Never cloned over: the folder is the developer's, whatever it holds
				// (PROJ-CLONE-002). The command to register it is given whole.
				m.message = fmt.Sprintf("%s is already there and is not registered, so nothing was cloned. If it is the repository, register it with: %s",
					filepath.Join(m.listing.Root.Path, r.Name), shell.Join([]string{"engineroom", "project", "add", filepath.Join(m.listing.Root.Path, r.Name)}))
			}
			return m, nil
		}
		m.message = ""
		m.form = repoForm{name: r.Name}
		m.screen = screenRepoForm
	}
	return m, nil
}

// formKey reads the form a key at a time. tab moves between the fields; space on the
// visibility field turns public on and off. The values are checked by the plan, so one rule and
// one message cover the interface and the command line.
func (m Model) formKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	fields := 1
	if m.form.create {
		fields = 3
	}
	// The name is the first field of a new repository; a clone's name is the listed one.
	field := m.form.field
	if !m.form.create {
		field = 1
	}
	switch k.Type {
	case tea.KeyEsc:
		m.screen = screenBrowse
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		m.form.field = (m.form.field + 1) % fields
	case tea.KeyShiftTab, tea.KeyUp:
		m.form.field = (m.form.field + fields - 1) % fields
	case tea.KeyEnter:
		if m.form.name == "" {
			return m, nil
		}
		src, root, f := m.src, m.browseRoot, m.form
		if f.create {
			return m, m.makePlan(func(ctx context.Context) (action.Plan, error) {
				return src.Create(ctx, root, f.name, f.category, f.public)
			}, true)
		}
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Clone(ctx, root, f.name, f.category) }, false)
	case tea.KeyBackspace:
		switch field {
		case 0:
			m.form.name = dropLast(m.form.name)
		case 1:
			m.form.category = dropLast(m.form.category)
		}
	case tea.KeySpace:
		switch field {
		case 1:
			m.form.category += " "
		case 2:
			m.form.public = !m.form.public
		}
	case tea.KeyRunes:
		switch field {
		case 0:
			m.form.name += string(k.Runes)
		case 1:
			m.form.category += string(k.Runes)
		}
	}
	return m, nil
}

func dropLast(s string) string {
	if r := []rune(s); len(r) > 0 {
		return string(r[:len(r)-1])
	}
	return s
}

func (m Model) rootsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Roots on "+on(m.src.Where())) + "\n\n")
	if !m.rootsRead {
		b.WriteString(dimStyle.Render("Reading roots ...") + "\n")
		return b.String()
	}
	if len(m.roots) == 0 {
		b.WriteString("No roots are registered. A root is a folder for one GitHub owner's repositories.\nRegister one with:\n\n")
		b.WriteString("  " + commandStyle.Render("engineroom root add <folder> --owner <owner> --account <gh account>") + "\n")
	}
	for i, r := range m.roots {
		cursor := "  "
		if i == m.cursor {
			cursor = keyStyle.Render("> ")
		}
		account := r.Account
		if account == "" {
			account = "gh's active account"
		}
		b.WriteString(fmt.Sprintf("%s%s%-20s %s\n", mark(hitRoot, i), cursor, r.Name, dimStyle.Render(r.Owner+" on GitHub, as "+account+", in "+r.Path)))
	}
	b.WriteString("\n" + dimStyle.Render(keyStyle.Render("enter")+" browse its repositories   "+keyStyle.Render("esc")+" projects   "+keyStyle.Render("q")+" quit") + "\n")
	return b.String()
}

// rows is how many list rows fit on the screen under the header and above the key hints.
func (m Model) rows() int {
	if m.height > 14 {
		return m.height - 12
	}
	return 20
}

func (m Model) browseView() string {
	var b strings.Builder
	r, _ := m.rootByName(m.browseRoot)
	b.WriteString(titleStyle.Render("Repositories of "+r.Owner+" on GitHub") + dimStyle.Render("  root "+r.Name+", cloned into "+r.Path) + "\n\n")
	switch {
	case m.ghProblem != nil:
		b.WriteString(errStyle.Render(m.ghProblem.Reason) + "\n")
		if m.ghProblem.Fix != "" {
			b.WriteString("\nTo fix it, run this on " + on(m.src.Where()) + ", then press ctrl+r:\n\n  " + commandStyle.Render(m.ghProblem.Fix) + "\n")
		}
		b.WriteString("\n" + dimStyle.Render(keyStyle.Render("ctrl+r")+" read again   "+keyStyle.Render("esc")+" roots") + "\n")
		return b.String()
	case !m.listed:
		b.WriteString(dimStyle.Render("Reading GitHub ...") + "\n")
		return b.String()
	}
	b.WriteString("Filter: " + m.filter + keyStyle.Render("_") + dimStyle.Render("  type to filter") + "\n\n")
	shown := m.shown()
	if len(shown) == 0 {
		b.WriteString("No repositories match.\n")
	}
	// A window of the list that keeps the cursor on screen.
	first := 0
	if n := m.rows(); m.cursor >= n {
		first = m.cursor - n + 1
	}
	for i := first; i < len(shown) && i < first+m.rows(); i++ {
		repo := shown[i]
		cursor := "  "
		if i == m.cursor {
			cursor = keyStyle.Render("> ")
		}
		here := ""
		switch repo.Here {
		case "registered":
			here = okStyle.Render("registered as " + repo.Project)
		case "folder":
			here = warnStyle.Render("folder here, not registered")
		}
		extra := repo.Visibility
		if repo.Archived {
			extra += ", archived"
		}
		b.WriteString(fmt.Sprintf("%s%s%-30s %-9s %s %s\n", mark(hitRepo, i), cursor, repo.Name, dimStyle.Render(extra), here, dimStyle.Render(clip(repo.Description, 40))))
	}
	b.WriteString("\n" + dimStyle.Render(keyStyle.Render("enter")+" add it here   "+keyStyle.Render("ctrl+n")+" new repository   "+keyStyle.Render("ctrl+r")+" read again   "+keyStyle.Render("esc")+" back") + "\n")
	b.WriteString(dimStyle.Render("Read with: "+m.listing.Read) + "\n")
	return b.String()
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (m Model) rootByName(name string) (config.Root, bool) {
	if m.listing.Root.Name == name {
		return m.listing.Root, true
	}
	for _, r := range m.roots {
		if r.Name == name {
			return r, true
		}
	}
	return config.Root{Name: name}, false
}

func (m Model) formView() string {
	var b strings.Builder
	r, _ := m.rootByName(m.browseRoot)
	mark := func(i int) string {
		if m.form.create && m.form.field == i || !m.form.create && i == 1 {
			return keyStyle.Render("_")
		}
		return ""
	}
	if m.form.create {
		b.WriteString(titleStyle.Render("New repository for "+r.Owner) + dimStyle.Render("  made in "+r.Path+", created on GitHub, registered") + "\n\n")
		b.WriteString("Name:       " + m.form.name + mark(0) + "\n")
	} else {
		b.WriteString(titleStyle.Render("Add "+r.Owner+"/"+m.form.name) + dimStyle.Render("  cloned into "+r.Path+" and registered") + "\n\n")
	}
	b.WriteString("Category:   " + m.form.category + mark(1) + dimStyle.Render("  optional; a label in engineroom only") + "\n")
	if m.form.create {
		vis := "private"
		if m.form.public {
			vis = "public"
		}
		b.WriteString("Visibility: " + vis + mark(2) + dimStyle.Render("  space changes it") + "\n")
		b.WriteString("\n" + dimStyle.Render(keyStyle.Render("tab")+" next field   "+keyStyle.Render("enter")+" show the commands   "+keyStyle.Render("esc")+" back") + "\n")
	} else {
		b.WriteString("\n" + dimStyle.Render(keyStyle.Render("enter")+" clone and register   "+keyStyle.Render("esc")+" back") + "\n")
	}
	return b.String()
}
