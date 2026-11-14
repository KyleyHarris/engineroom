package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func engineroom(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// Setting a role is the way out of CONN-SETUP-001's screen, so it must work from nothing, keep
// what is already configured, and refuse a client with no engine to reach.
func TestRoleSetAndShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("ENGINEROOM_CONFIG", path)

	if code, out, _ := engineroom(t, "role", "show"); code != 0 || strings.TrimSpace(out) != "no role" {
		t.Fatalf("fresh machine: %d %q", code, out)
	}
	if code, _, errOut := engineroom(t, "role", "set", "client"); code != 2 || !strings.Contains(errOut, "--engine") {
		t.Fatalf("client without --engine was accepted: %d %q", code, errOut)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a refused role still wrote the configuration")
	}
	if code, _, errOut := engineroom(t, "role", "set", "client", "--engine", "engine.example"); code != 0 {
		t.Fatalf("set client: %d %s", code, errOut)
	}
	if _, out, _ := engineroom(t, "role", "show"); strings.TrimSpace(out) != "client of engine.example" {
		t.Fatalf("show after set: %q", out)
	}
	if code, _, _ := engineroom(t, "role", "set", "server"); code != 2 {
		t.Fatal("an unknown role was accepted")
	}
	if code, _, _ := engineroom(t, "role", "set", "engine"); code != 0 {
		t.Fatal("set engine failed")
	}
	if _, out, _ := engineroom(t, "role", "show"); strings.TrimSpace(out) != "engine" {
		t.Fatalf("show after set engine: %q", out)
	}
	// D-039: local is set and shown, keeps the engine's address, and a client can be set again
	// from it without --engine.
	if code, out, errOut := engineroom(t, "role", "set", "local"); code != 0 || !strings.Contains(out, "used on its own (local)") {
		t.Fatalf("set local: %d %q %q", code, out, errOut)
	}
	if _, out, _ := engineroom(t, "role", "show"); strings.TrimSpace(out) != "local" {
		t.Fatalf("show after set local: %q", out)
	}
	if code, _, errOut := engineroom(t, "role", "set", "client"); code != 0 {
		t.Fatalf("client again from local, with the engine kept: %d %q", code, errOut)
	}
	if _, out, _ := engineroom(t, "role", "show"); strings.TrimSpace(out) != "client of engine.example" {
		t.Fatalf("show after client again: %q", out)
	}
}

// SETUP-LOCAL-003 at the api the app calls: the app's Settings choose between the engine and
// local, a client is refused with the command that sets it, and a client cannot leave its role by
// a click. Fails if a choice is not saved, a client is set without an engine, an unknown role is
// accepted, or a client's role changes.
func TestTheAppChoosesBetweenEngineAndLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("ENGINEROOM_CONFIG", path)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	roleNow := func() string { _, out, _ := engineroom(t, "role", "show"); return strings.TrimSpace(out) }
	set := func(v string) (int, map[string]any) {
		code, out, _ := engineroom(t, "api", "set", "role", v)
		var reply map[string]any
		_ = json.Unmarshal([]byte(out), &reply)
		return code, reply
	}
	if code, _, errOut := engineroom(t, "role", "set", "local"); code != 0 {
		t.Fatal(errOut)
	}
	if code, reply := set("engine"); code != 0 || reply["role"] != "engine" || roleNow() != "engine" {
		t.Fatalf("choosing the engine: %d %v, role %q", code, reply, roleNow())
	}
	if code, reply := set("local"); code != 0 || reply["role"] != "local" || roleNow() != "local" {
		t.Fatalf("choosing local: %d %v, role %q", code, reply, roleNow())
	}
	if code, reply := set("client"); code != 3 || !strings.Contains(reply["error"].(string), "engineroom role set client --engine") || roleNow() != "local" {
		t.Fatalf("choosing a client: %d %v, role %q", code, reply, roleNow())
	}
	if code, _ := set("server"); code != 3 || roleNow() != "local" {
		t.Fatalf("an unknown role: %d, role %q", code, roleNow())
	}
	if code, _, errOut := engineroom(t, "role", "set", "client", "--engine", "engine.example"); code != 0 {
		t.Fatal(errOut)
	}
	if code, reply := set("local"); code != 3 || !strings.Contains(reply["error"].(string), "this Mac is a client") || roleNow() != "client of engine.example" {
		t.Fatalf("a client clicking local: %d %v, role %q", code, reply, roleNow())
	}
}

func TestUnknownCommandIsRefused(t *testing.T) {
	if code, _, errOut := engineroom(t, "frobnicate"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("%d %q", code, errOut)
	}
}

// A run whose plan cannot be made, such as opening the editor on a machine without its command,
// is answered as a run: one done event, not ok, carrying the reason. The app reads a run as
// events, so a bare error reply was skipped and the app said only that the run stopped. Fails if
// the refusal is not a done event or loses its reason.
func TestARefusedRunIsADoneEventWithItsReason(t *testing.T) {
	t.Setenv("ENGINEROOM_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "project-a")
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if code, _, errOut := engineroom(t, "role", "set", "engine"); code != 0 {
		t.Fatalf("set engine: %s", errOut)
	}
	if code, _, errOut := engineroom(t, "project", "add", project); code != 0 {
		t.Fatalf("project add: %s", errOut)
	}
	// No folder on PATH holds the editor's command, as for an app started with a bare PATH.
	t.Setenv("PATH", t.TempDir())
	code, out, _ := engineroom(t, "api", "run", "edit-project", "project-a")
	var done struct {
		Event string `json:"event"`
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &done); err != nil {
		t.Fatalf("not one JSON event: %v\n%s", err, out)
	}
	if code != 3 || done.Event != "done" || done.OK == nil || *done.OK || !strings.Contains(done.Error, `"code" was not found`) {
		t.Fatalf("exit %d, reply %s", code, out)
	}
}
