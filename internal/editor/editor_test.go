package editor

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/ops"
)

func found(string) (string, error)   { return "/bin/x", nil }
func missing(string) (string, error) { return "", errors.New("not found") }

var session = ops.SessionView{Name: "project-a-fix", Project: "project-a", Checkout: "~/src/project-a-fix", Path: "/srv/src/project-a-fix"}

// On this machine the folder is passed as it is; from a client the engine is the editor's remote.
// Catches: the remote form used locally, or the screen's ~/ path used instead of the real one.
func TestPlanLocalAndRemote(t *testing.T) {
	p, err := Plan("code", session, "", found)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"code", "/srv/src/project-a-fix"}; !reflect.DeepEqual(p.Commands[0].Argv, want) {
		t.Fatalf("local argv %q, want %q", p.Commands[0].Argv, want)
	}
	p, err = Plan("code", session, "engine", found)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"code", "--remote", "ssh-remote+engine", "/srv/src/project-a-fix"}; !reflect.DeepEqual(p.Commands[0].Argv, want) {
		t.Fatalf("remote argv %q, want %q", p.Commands[0].Argv, want)
	}
	if p.Commands[0].Interactive {
		t.Fatal("the editor does not take over the terminal")
	}
}

// Editor obligation O8: awkward folders and hosts stay single arguments, and the shown command
// quotes them so it pastes back as the same command.
func TestPlanAwkwardPathAndHost(t *testing.T) {
	s := session
	s.Path = "/srv/My Project; rm -rf x/$(id)"
	p, err := Plan("code", s, "dev@engine.example", found)
	if err != nil {
		t.Fatal(err)
	}
	argv := p.Commands[0].Argv
	if len(argv) != 4 || argv[3] != s.Path || argv[2] != "ssh-remote+dev@engine.example" {
		t.Fatalf("argv %q", argv)
	}
	if shown := p.Commands[0].Display(); !strings.Contains(shown, "'/srv/My Project; rm -rf x/$(id)'") {
		t.Fatalf("shown unquoted: %s", shown)
	}
}

// Editor obligation O7: each refusal names the reason, and nothing is planned.
func TestPlanRefusals(t *testing.T) {
	unregistered := ops.SessionView{Name: "scratch"}
	if _, err := Plan("code", unregistered, "", found); !errors.Is(err, ErrNoFolder) || !strings.Contains(err.Error(), "not in a registered project") {
		t.Fatalf("unregistered: %v", err)
	}
	unreported := session
	unreported.Path = ""
	if _, err := Plan("code", unreported, "engine", found); !errors.Is(err, ErrNoFolder) || !strings.Contains(err.Error(), "update engineroom on the engine") {
		t.Fatalf("unreported: %v", err)
	}
	var nf *NotFoundError
	if _, err := Plan("code", session, "", missing); !errors.As(err, &nf) || nf.Command != "code" || !strings.Contains(err.Error(), "Shell Command: Install") {
		t.Fatalf("missing: %v", err)
	}
}

// An empty command is the default, so a configuration written before the setting existed
// still opens VS Code.
func TestPlanDefaultCommand(t *testing.T) {
	p, err := Plan("", session, "", found)
	if err != nil || p.Commands[0].Argv[0] != "code" {
		t.Fatalf("%v %+v", err, p.Commands)
	}
}
