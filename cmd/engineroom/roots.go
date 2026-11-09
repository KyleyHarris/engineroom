package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/roots"
)

// repoFlags are the choices for a repository brought into a root.
type repoFlags struct {
	category string
	public   bool
}

// newRepoFlags reads --category and --public from anywhere in args and returns the rest.
func newRepoFlags(args []string, stderr io.Writer) (repoFlags, []string, bool) {
	var f repoFlags
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--category":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "engineroom: --category needs a value")
				return f, nil, false
			}
			f.category = args[i+1]
			i++
		case "--public":
			f.public = true
		default:
			rest = append(rest, a)
		}
	}
	return f, rest, true
}

// runRepoPlan prints the plan for cloning or creating a repository in a root, for a client to
// run over SSH, in the same reply shape as runPlan.
func runRepoPlan(args []string, stdout, stderr io.Writer) int {
	f, rest, ok := newRepoFlags(args, stderr)
	if ok && len(rest) == 2 && rest[0] == "register" {
		cfg, ok := engineConfig(stderr)
		if !ok {
			return 1
		}
		p, err := roots.Register(cfg, rest[1], f.category)
		if err != nil {
			writeJSON(stdout, struct {
				Error string `json:"error"`
			}{err.Error()})
			return 3
		}
		p.Machine = "engine"
		return writeJSON(stdout, struct {
			Plan *action.Plan `json:"plan"`
		}{&p})
	}
	if !ok || len(rest) != 3 {
		fmt.Fprintln(stderr, "usage: engineroom plan clone <root> <repository> [--category <c>] | engineroom plan create <root> <name> [--category <c>] [--public]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	type reply struct {
		Plan  *action.Plan `json:"plan,omitempty"`
		Error string       `json:"error,omitempty"`
	}
	var p action.Plan
	var err error
	if rest[0] == "clone" {
		p, err = roots.Clone(cfg, rest[1], rest[2], f.category)
	} else {
		p, err = roots.Create(cfg, rest[1], rest[2], f.category, f.public)
	}
	if err != nil {
		writeJSON(stdout, reply{Error: err.Error()})
		return 3
	}
	p.Machine = "engine"
	return writeJSON(stdout, reply{Plan: &p})
}

func runRoots(args []string, stdout, stderr io.Writer) int {
	asJSON, _ := jsonFlag(args)
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	if asJSON {
		list := cfg.Roots
		if list == nil {
			return writeJSON(stdout, []any{})
		}
		return writeJSON(stdout, list)
	}
	for _, r := range cfg.Roots {
		account := r.Account
		if account == "" {
			account = "(gh's active account)"
		}
		fmt.Fprintf(stdout, "%s\t%s\towner %s\taccount %s\n", r.Name, r.Path, r.Owner, account)
	}
	return 0
}

// runRoot registers and forgets roots. It changes only this machine's configuration.
func runRoot(args []string, stdout, stderr io.Writer) int {
	usage := "usage: engineroom root add <folder> --owner <owner> [--account <account>] [--name <name>] | engineroom root remove <name> | engineroom root hide|unhide <root> <repository> | engineroom root list"
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	switch args[0] {
	case "list":
		return runRoots(args[1:], stdout, stderr)
	case "add":
		fs := flag.NewFlagSet("root add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		owner := fs.String("owner", "", "the GitHub user or organisation whose repositories go here")
		account := fs.String("account", "", "the gh account that lists them (default: gh's active account)")
		name := fs.String("name", "", "the root's name (default: the folder's name)")
		folder, rest := "", args[1:]
		if len(rest) > 0 && len(rest[0]) > 0 && rest[0][0] != '-' {
			folder, rest = rest[0], rest[1:]
		}
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if folder == "" && fs.NArg() > 0 {
			folder = fs.Arg(0)
		}
		if folder == "" || *owner == "" {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		r, err := roots.Add(cfg, folder, *owner, *account, *name)
		if err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		with := "gh's active account"
		if r.Account != "" {
			with = "the account " + r.Account
		}
		fmt.Fprintf(stdout, "Registered the root %s: %s, for the GitHub owner %s, listed with %s.\n", r.Name, r.Path, r.Owner, with)
		return 0
	case "remove":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: engineroom root remove <name>")
			return 2
		}
		if err := roots.Remove(cfg, args[1]); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Forgot the root %s. Nothing on disk was changed, and its projects stay registered.\n", args[1])
		return 0
	case "hide", "unhide":
		// A repository on GitHub, not here, left out of the repositories list or put back.
		// Nothing changes on GitHub or on disk.
		if len(args) != 3 {
			fmt.Fprintf(stderr, "usage: engineroom root %s <root> <repository>\n", args[0])
			return 2
		}
		if _, ok := cfg.Root(args[1]); !ok {
			fmt.Fprintf(stderr, "engineroom: %q is not a root\n", args[1])
			return 1
		}
		if err := cfg.SetHidden(args[1], args[2], args[0] == "hide"); err != nil {
			fmt.Fprintf(stderr, "engineroom: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s/%s is %s.\n", args[1], args[2], map[string]string{"hide": "hidden from the list", "unhide": "listed again"}[args[0]])
		return 0
	}
	fmt.Fprintf(stderr, "engineroom root: unknown %q\n", args[0])
	return 2
}

// browseReply is what `engineroom browse --json` prints: the listing, or the problem with its fix, so a
// client shows the same reason and fix the engine found (PROJ-BROWSE-002).
type browseReply struct {
	Listing *roots.Listing `json:"listing,omitempty"`
	Error   string         `json:"error,omitempty"`
	Problem *roots.Problem `json:"problem,omitempty"`
}

func runBrowse(args []string, stdout, stderr io.Writer) int {
	asJSON, rest := jsonFlag(args)
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "usage: engineroom browse <root> [--json]")
		return 2
	}
	cfg, ok := engineConfig(stderr)
	if !ok {
		return 1
	}
	l, err := roots.Browse(cfg, rest[0])
	if asJSON {
		if err != nil {
			r := browseReply{Error: err.Error()}
			var p *roots.Problem
			if errors.As(err, &p) {
				r.Problem = p
			}
			writeJSON(stdout, r)
			return 3
		}
		return writeJSON(stdout, browseReply{Listing: &l})
	}
	fmt.Fprintf(stdout, "Read with: %s\n", l.Read)
	if err != nil {
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	for _, r := range l.Repos {
		here := ""
		switch r.Here {
		case "registered":
			here = "registered as " + r.Project
		case "folder":
			here = "folder here, not registered"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", r.Name, r.Visibility, here, r.Description)
	}
	return 0
}

// runGH runs gh as one account, with that account's token from `gh auth token --user`, so a
// root's repositories are listed and created with the right account without switching gh's
// active account for the rest of the machine. Its standard streams are gh's own.
func runGH(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 3 || args[0] != "--account" {
		fmt.Fprintln(stderr, "usage: engineroom gh --account <account> <gh arguments>")
		return 2
	}
	account, rest := args[1], args[2:]
	token, err := exec.Command("gh", "auth", "token", "--user", account).Output()
	if err != nil || len(token) == 0 {
		fmt.Fprintf(stderr, "engineroom: gh is not signed in to the account %s on this machine. To fix it, run: gh auth login --hostname github.com\n", account)
		return 1
	}
	cmd := exec.Command("gh", rest...)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+string(trimNewline(token)))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "engineroom: %v\n", err)
		return 1
	}
	return 0
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
