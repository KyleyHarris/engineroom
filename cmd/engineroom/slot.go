package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/engineroomfile"
	"github.com/KyleyHarris/engineroom/internal/netport"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/settings"
	"github.com/KyleyHarris/engineroom/internal/slots"
)

// slotTarget is the worktree a slot command acts on and its project's settings: --worktree, or
// the folder the command runs in, which must be a registered project's checkout.
func slotTarget(fs *flag.FlagSet, args []string, stderr io.Writer) (settings.Project, string, bool) {
	dir := fs.String("worktree", "", "the worktree (default: the one this command runs in)")
	if err := fs.Parse(args); err != nil {
		return settings.Project{}, "", false
	}
	if *dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return settings.Project{}, "", false
		}
		*dir = wd
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return settings.Project{}, "", false
	}
	top, err := slots.Top(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %s is not in a git worktree: %v\n", *dir, err)
		return settings.Project{}, "", false
	}
	p, checkout, ok := ops.ProjectOf(cfg, top)
	if !ok {
		fmt.Fprintf(stderr, "engineroom: %s is not a checkout of a registered project (engineroom project add)\n", top)
		return settings.Project{}, "", false
	}
	sp, err := settings.Load(p.Name, p.Path, checkout)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return settings.Project{}, "", false
	}
	return sp, checkout, true
}

// runSlot is `engineroom slot ...`: a worktree's own slot of its project's ports and names (D-013).
// claim and release are what new work and clean up run; status, json, get and env are how a
// repository's scripts and code read the slot without knowing how it was chosen.
func runSlot(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: engineroom slot claim|release|status|json|env|ignore [--worktree <dir>] | engineroom slot get <key> [--worktree <dir>] | engineroom slot status --hook")
		return 2
	}
	cmd, rest := args[0], args[1:]
	fs := flag.NewFlagSet("slot "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	hook := false
	var key string
	switch cmd {
	case "status":
		fs.BoolVar(&hook, "hook", false, "print the status for a Claude Code SessionStart hook")
	case "get":
		if len(rest) == 0 {
			fmt.Fprintln(stderr, "usage: engineroom slot get <key> [--worktree <dir>]")
			return 2
		}
		key, rest = rest[0], rest[1:]
	case "claim", "release", "json", "env", "ignore":
	default:
		fmt.Fprintf(stderr, "engineroom: unknown slot command %q\n", cmd)
		return 2
	}
	if hook {
		// A hook must never stand between the developer and a session: anything unexpected is
		// silence (the hook rule every hook here follows).
		fs.SetOutput(io.Discard)
		stderr = io.Discard
	}
	p, checkout, ok := slotTarget(fs, rest, stderr)
	if !ok {
		if hook {
			return 0
		}
		return 1
	}
	if !p.HasSlots() {
		if hook {
			return 0
		}
		fmt.Fprintf(stderr, "engineroom: %s declares no ports, so its worktrees have no slots. Add [ports] to its engineroom.toml in your settings (%s).\n", p.Name, p.Dir)
		return 1
	}

	switch cmd {
	case "ignore":
		// The developer's request to add the slot's files to the repository's .gitignore, the
		// fix every claim's warning names; they commit the change.
		added, err := slots.Ignore(p, checkout)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		if len(added) == 0 {
			fmt.Fprintln(stdout, "git already ignores every file a slot writes here; .gitignore is unchanged.")
			return 0
		}
		for _, rel := range added {
			fmt.Fprintf(stdout, "added /%s to .gitignore\n", rel)
		}
		fmt.Fprintln(stdout, "Commit .gitignore so every clone ignores them.")
		return 0
	case "claim":
		row, warning, err := slots.Claim(p, checkout)
		if warning != "" {
			fmt.Fprintln(stderr, "engineroom: "+warning)
		}
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		written, warnings, err := slots.Write(p, row, checkout)
		for _, w := range written {
			fmt.Fprintf(stdout, "wrote %s\n", w)
		}
		for _, w := range warnings {
			fmt.Fprintln(stderr, "engineroom: "+w)
		}
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: the slot was claimed but its files were not all written: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s holds slot %d of %s: %s\n", checkout, row.Slot, p.Name, portList(row.Ports))
		return 0
	case "release":
		s, held, err := slots.Release(p, checkout)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		for _, r := range slots.Remove(p, checkout) {
			fmt.Fprintf(stdout, "removed %s\n", r)
		}
		if !held {
			fmt.Fprintf(stdout, "%s holds no slot; nothing to give back.\n", checkout)
			return 0
		}
		fmt.Fprintf(stdout, "%s gave back slot %d of %s.\n", checkout, s, p.Name)
		return 0
	case "status":
		return slotStatus(p, checkout, hook, stdout, stderr)
	}

	row, _, err := slots.Effective(p, checkout)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if row.Slot == 0 {
		fmt.Fprintf(stderr, "engineroom: %s holds no slot. Run: engineroom slot claim --worktree %s\n", checkout, checkout)
		return 1
	}
	values, err := slots.Values(p, row, checkout)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	switch cmd {
	case "json":
		b, _ := json.MarshalIndent(values, "", "  ")
		fmt.Fprintln(stdout, string(b))
	case "get":
		v, ok := values[key]
		if !ok {
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(stderr, "engineroom: no %q in the slot. Known: %s\n", key, strings.Join(keys, ", "))
			return 1
		}
		fmt.Fprintln(stdout, v)
	case "env":
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(stdout, "export %s=%s\n", slots.EnvName(p, row, k), shellQuote(values[k]))
		}
	}
	return 0
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func portList(ports []engineroomfile.Port) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf("%s %d", p.Name, p.Number)
	}
	return strings.Join(parts, ", ")
}

// slotStatus says which slot a worktree runs on. As a hook it says it to the session at its
// start: a linked worktree with no slot is a warning, because it would share the main clone's
// ports and data, and every sign of that is indirect.
func slotStatus(p settings.Project, checkout string, hook bool, stdout, stderr io.Writer) int {
	row, held, err := slots.Effective(p, checkout)
	if err != nil {
		if hook {
			return 0
		}
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	var text string
	switch {
	case held:
		text = fmt.Sprintf("Worktree slot %d of %s: %s. Its record is %s.", row.Slot, p.Name, portList(row.Ports), p.Slots.Record)
	case row.Slot == 1:
		text = fmt.Sprintf("Worktree slot: none claimed (main clone of %s), so it runs on slot 1, the project's own numbers: %s.", p.Name, portList(row.Ports))
	default:
		text = fmt.Sprintf("WARNING: this linked worktree of %s holds no slot, so its stack would share the main clone's ports (%s) and data. Run: engineroom slot claim --worktree %s", p.Name, portList(p.Rows[0].Ports), checkout)
	}
	if !hook {
		fmt.Fprintln(stdout, text)
		return 0
	}
	b, _ := json.Marshal(map[string]any{
		"systemMessage":      text,
		"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": text},
	})
	fmt.Fprintln(stdout, string(b))
	return 0
}

// allProjects loads every registered project's settings, for the checks that span projects. A
// project whose settings cannot be read is reported and left out, never read as having none.
func allProjects(cfg *config.Config, stderr io.Writer) ([]settings.Project, bool) {
	var out []settings.Project
	ok := true
	for _, p := range cfg.Projects {
		sp, err := settings.Load(p.Name, p.Path, p.Path)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			ok = false
			continue
		}
		out = append(out, sp)
	}
	return out, ok
}

// runPorts is `engineroom ports ...`: every project's reserved ports and slots, the clashes across
// them, and a reservation for a project that has none (D-013).
func runPorts(args []string, stdout, stderr io.Writer) int {
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	projects, readable := allProjects(cfg, stderr)
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		for _, p := range projects {
			if len(p.Reserve) == 0 && !p.HasSlots() {
				continue
			}
			res := "none"
			if len(p.Reserve) > 0 {
				parts := make([]string, len(p.Reserve))
				for i, r := range p.Reserve {
					parts[i] = r.String()
				}
				res = strings.Join(parts, ", ") + " (" + string(p.ReserveFrom) + ")"
			}
			fmt.Fprintf(stdout, "%s  reserved %s; %d slot(s) (%s)\n", p.Name, res, len(p.Rows), p.RowsFrom)
			for _, r := range p.Rows {
				fmt.Fprintf(stdout, "  slot %-2d %s\n", r.Slot, portList(r.Ports))
			}
		}
		return 0
	case "check":
		problems := slots.Check(projects)
		for _, pr := range problems {
			fmt.Fprintln(stdout, "clash: "+pr.String())
		}
		// What is listening now is reported, never concluded from: a port answering is not
		// proof it is that project's own server.
		for _, p := range projects {
			for _, r := range p.Rows {
				for _, port := range r.Ports {
					if why := netport.Taken(port.Number); why != "" {
						fmt.Fprintf(stdout, "in use: %s slot %d %s %d is %s\n", p.Name, r.Slot, port.Name, port.Number, why)
					}
				}
			}
		}
		if len(problems) == 0 {
			fmt.Fprintln(stdout, "No clashes between the projects' ports.")
		}
		if len(problems) > 0 || !readable {
			return 1
		}
		return 0
	case "reserve":
		return portsReserve(cfg, projects, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, "usage: engineroom ports | engineroom ports check | engineroom ports reserve <project> [--write]")
	return 2
}

// portsReserve gives a project a block of ports from the band and a slot table carved from it.
// Without --write it shows what it would write; with it, it writes it into the project's engineroom.toml
// in the developer's settings. Slot 1 keeps the project's own numbers, which are reserved too.
func portsReserve(cfg *config.Config, projects []settings.Project, args []string, stdout, stderr io.Writer) int {
	write := false
	var name string
	for _, a := range args {
		if a == "--write" {
			write = true
		} else if name == "" {
			name = a
		}
	}
	if name == "" {
		fmt.Fprintln(stderr, "usage: engineroom ports reserve <project> [--write]")
		return 2
	}
	var target *settings.Project
	for i := range projects {
		if projects[i].Name == name {
			target = &projects[i]
		}
	}
	if target == nil {
		fmt.Fprintf(stderr, "engineroom: %q is not a registered project\n", name)
		return 1
	}
	if len(target.Reserve) > 0 {
		fmt.Fprintf(stderr, "engineroom: %s already reserves ports (%s); change them in %s\n", name, string(target.ReserveFrom), filepath.Join(target.Dir, engineroomfile.Name))
		return 1
	}
	block, err := slots.NextBlock(projects)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	rows, err := slots.Plan(*target, block)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	var keep []engineroomfile.Range
	for _, p := range target.Ports {
		if !block.Contains(p.Number) {
			keep = append(keep, engineroomfile.Range{Lo: p.Number, Hi: p.Number})
		}
	}
	path := filepath.Join(target.Dir, engineroomfile.Name)
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	text := string(existing)
	if !target.Own.Has["ports"] {
		// The ports came from the repository's recommendation: they are written into the
		// developer's own file too, so the reservation and the ports it keeps are in one place.
		var b strings.Builder
		b.WriteString("[ports]\n")
		for _, p := range target.Ports {
			fmt.Fprintf(&b, "%s = %d\n", p.Name, p.Number)
		}
		text = strings.TrimRight(text, "\n") + "\n" + b.String()
	}
	out, err := slots.Add(text, block, keep, rows, target.Own)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if !write {
		fmt.Fprintf(stdout, "%s would reserve %s and keep its own ports %s. This would be written to %s:\n\n%s\nRun again with --write to write it.\n", name, block, portList(target.Ports), path, out)
		return 0
	}
	if err := os.MkdirAll(target.Dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s reserves %s. Wrote %s.\n", name, block, path)
	return 0
}

// runSettings is `engineroom settings where <project>`: the folder of the developer's own settings for a
// project, where its engineroom.toml and scripts go, whether or not it exists yet.
func runSettings(args []string, stdout, stderr io.Writer) int {
	usage := "usage: engineroom settings where <project> | status | init [<repository>] | sync | clone <repository> [<key file>]"
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	root := settings.Root()
	switch {
	case args[0] == "status" && len(args) == 1:
		return settingsStatus(settings.State(root), stdout)
	case args[0] == "init" && len(args) <= 2:
		remote := ""
		if len(args) == 2 {
			remote = args[1]
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return exitOf(runPlain(settings.InitPlan(settings.State(root), remote), stdout, stderr))
	case args[0] == "sync" && len(args) == 1:
		host, _ := os.Hostname()
		plan, err := settings.SyncPlan(settings.State(root), strings.SplitN(host, ".", 2)[0])
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		return exitOf(runPlain(plan, stdout, stderr))
	case args[0] == "clone" && (len(args) == 2 || len(args) == 3):
		if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
			fmt.Fprintf(stderr, "engineroom: %s already holds settings; move it aside first, so nothing of it is lost\n", root)
			return 1
		}
		key := ""
		if len(args) == 3 {
			key = args[2]
		}
		return exitOf(runPlain(settings.ClonePlan(args[1], key, root), stdout, stderr))
	case args[0] != "where" || len(args) != 2:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	p, found := cfg.Project(args[1])
	if !found {
		fmt.Fprintf(stderr, "engineroom: %q is not a registered project\n", args[1])
		return 1
	}
	fmt.Fprintln(stdout, settings.Dir(p.Name, p.Path))
	return 0
}

// exitOf is a plan's outcome as an exit status.
func exitOf(ok bool) int {
	if ok {
		return 0
	}
	return 1
}

// settingsStatus says what the settings folder is as a repository: where it is, where it syncs
// to, what is not yet sent, and whether its secrets can be read here. The secrets are plain files
// on a machine holding the key, so they are read and edited like any other file; on one without
// it, and on GitHub, they are encrypted bytes.
func settingsStatus(st settings.RepoState, stdout io.Writer) int {
	fmt.Fprintf(stdout, "settings: %s\n", st.Root)
	if !st.IsRepo {
		fmt.Fprintln(stdout, "not a repository yet; keep it in a private one with: engineroom settings init <repository>")
		return 0
	}
	remote := st.Remote
	if remote == "" {
		remote = "none (engineroom settings init <repository> adds one)"
	}
	fmt.Fprintf(stdout, "repository: %s\n", remote)
	switch {
	case st.Crypt && st.Attributes:
		fmt.Fprintln(stdout, "secrets: readable here; every file in a secrets folder is encrypted when committed (key: "+settings.KeyFile(st.Root)+")")
	case st.Attributes:
		fmt.Fprintln(stdout, "secrets: ENCRYPTED here; unlock them with the key saved when the repository was set up: git-crypt unlock <key file>")
	default:
		fmt.Fprintln(stdout, "secrets: not set up; engineroom settings init sets up git-crypt")
	}
	for _, f := range st.Unprotected {
		fmt.Fprintf(stdout, "UNPROTECTED: %s would be committed without encryption\n", f)
	}
	fmt.Fprintf(stdout, "not committed: %d change(s); not sent: %d commit(s); not taken: %d commit(s)\n", len(st.Changed), st.Ahead, st.Behind)
	return 0
}

// runSecrets is `engineroom secrets status|save|restore <project> [--worktree <dir>]`: a project's
// secret files compared with, copied into, or put back from the developer's settings (D-014).
// The checkout is the main clone unless --worktree names another.
func runSecrets(args []string, stdout, stderr io.Writer) int {
	usage := "usage: engineroom secrets status|save|restore <project> [--worktree <dir>]"
	if len(args) != 2 && !(len(args) == 4 && args[2] == "--worktree") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	verb := args[0]
	if verb != "status" && verb != "save" && verb != "restore" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	p, found := cfg.Project(args[1])
	if !found {
		fmt.Fprintf(stderr, "engineroom: %q is not a registered project\n", args[1])
		return 1
	}
	checkout := p.Path
	if len(args) == 4 {
		checkout = args[3]
	}
	sp, err := settings.Load(p.Name, p.Path, checkout)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	if len(sp.Secrets) == 0 {
		fmt.Fprintf(stdout, "%s names no secret files. List them in your settings' engineroom.toml (engineroom settings where %s), before any table:\nsecrets = [\"path/in/the/worktree\", \"~/path/in/your/home\"]\n", p.Name, p.Name)
		return 0
	}
	switch verb {
	case "status":
		for _, s := range settings.Secrets(sp, checkout) {
			state := "saved, and the same here"
			switch {
			case !s.IsSaved && !s.InCheckout:
				state = "MISSING: neither here nor saved"
			case !s.IsSaved:
				state = "here, NOT saved (engineroom secrets save " + p.Name + ")"
			case !s.InCheckout:
				state = "saved, not here (engineroom secrets restore " + p.Name + ")"
			case !s.Same:
				state = "DIFFERS: here " + s.Checkout + ", saved " + s.Saved
			}
			fmt.Fprintf(stdout, "%-50s %s\n", s.Name, state)
		}
		return 0
	case "save":
		plan := settings.SavePlan(sp, checkout)
		for _, n := range plan.Notes {
			fmt.Fprintln(stdout, n)
		}
		if len(plan.Commands) == 0 {
			return 0
		}
		return exitOf(runPlain(plan, stdout, stderr))
	default:
		plan := settings.RestorePlan(sp, checkout)
		for _, n := range plan.Notes {
			fmt.Fprintln(stdout, n)
		}
		if len(plan.Commands) == 0 {
			return 0
		}
		return exitOf(runPlain(plan, stdout, stderr))
	}
}
