package handoff

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/paths"
)

// The two directions a client hands work over in.
const (
	// ToEngine is going back online: what this machine holds goes to the engine (CONN-HANDOFF-001).
	ToEngine = "to-engine"
	// FromEngine is going offline: what the engine holds comes to this machine (CONN-HANDOFF-004).
	FromEngine = "from-engine"
)

// ErrNotClient is a handoff asked for on the engine, which has no other machine to hand to.
var ErrNotClient = errors.New("only a client hands work over; this machine is the engine")

// ForClient reads this machine and the engine and builds the handoff in the given direction. It
// works whether the client is offline or online at the time: it always asks the engine itself, over
// SSH, because a handoff is between the two machines. An engine that does not answer is not an
// error: the plan says what could not be checked, and still pushes what this machine holds when
// this machine is the one giving (CONN-HANDOFF-005).
func ForClient(ctx context.Context, cfg *config.Config, direction string) (action.Plan, error) {
	if cfg.Role != config.RoleClient {
		return action.Plan{}, ErrNotClient
	}
	if direction != ToEngine && direction != FromEngine {
		return action.Plan{}, fmt.Errorf("a handoff is %q or %q, not %q", ToEngine, FromEngine, direction)
	}
	remote := action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
	engine := Side{Name: "the engine", Wrap: remote.Wrap, Git: &remote}
	engine.State, engine.Unread = readEngine(ctx, remote)
	here := Side{Name: "this machine", State: &State{}, Here: true, Wrap: func(c action.Command) action.Command { return c }, Folder: LocalFolder}
	if direction == ToEngine || engine.State != nil {
		// This machine is read only when something can come of it: when it gives, or when the
		// engine could be read to give. Reading it asks GitHub, which takes time a switch to
		// offline with the engine out of reach should not wait for.
		mine := readHere(ctx, cfg)
		here.State = &mine
	}
	if direction == ToEngine {
		return Build(here, engine), nil
	}
	return Build(engine, here), nil
}

// readEngine asks the engine for its reading with `engineroom handoff state --json`, returning why it could
// not when it could not.
//
// The engine writes how far its reading has got to its standard error, one {"progress": step}
// line per project (`handoff state`); those lines are passed on to the context's Progress as they
// arrive, named as the engine's, with the command shown. An engine too old to write them is read
// all the same, with no progress said.
func readEngine(ctx context.Context, r action.Remote) (*State, string) {
	report := progressOf(ctx)
	c := r.Engineroom("handoff", "state", "--json")
	report(Step{Side: "the engine", Command: c.Display()})
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	var stdout, rest bytes.Buffer
	cmd.Stdout = &stdout
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, "could not be read (" + err.Error() + ")"
	}
	if err := cmd.Start(); err != nil {
		return nil, "could not be read (" + err.Error() + ")"
	}
	lines := bufio.NewScanner(errPipe)
	for lines.Scan() {
		var p struct {
			Progress *Step `json:"progress"`
		}
		if json.Unmarshal(lines.Bytes(), &p) == nil && p.Progress != nil {
			s := *p.Progress
			s.Side, s.Command = "the engine", c.Display()
			report(s)
			continue
		}
		rest.Write(lines.Bytes())
		rest.WriteByte(' ')
	}
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 255 {
		return nil, "did not answer"
	}
	if err != nil {
		return nil, "could not be read (on the engine: " + strings.TrimSpace(rest.String()+" "+stdout.String()) + "; an engine whose engineroom is older than handoff needs it updated)"
	}
	out := stdout.Bytes()
	var st State
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, "answered with something that is not a reading (" + err.Error() + ")"
	}
	return &st, ""
}

// PartialCommit is the plan that commits a worktree's uncommitted work as partial work, so the
// next release can push it, for work the developer would rather not finish first. Offered by a
// release, which has no other machine to carry
// the work to (a handover carries it as a stash), run only when chosen. machine names
// where it runs, for the commit message.
func PartialCommit(path, machine string) action.Plan {
	return action.Plan{
		Title:   "Commit the work in " + path + " as partial work",
		Machine: machine,
		Commands: []action.Command{
			{Argv: []string{"git", "-C", path, "add", "-A"}, Why: "take every change, new files included"},
			{Argv: []string{"git", "-C", path, "commit", "-q", "-m", "Partial work, committed to hand over from " + machine},
				Why: "commit it on the worktree's branch; the next handover pushes it"},
		},
	}
}

// Release is the engine's half of a handover, run on the engine (D-017): the branch each checkout
// has out, with commits GitHub lacks, is pushed, and each worktree with uncommitted changes is
// said, with the offer to commit it as partial work. It moves nothing on any machine; a client then works local
// and brings the work in. Running it records when, which the engine shows; the record decides
// nothing.
func Release(cfg *config.Config) action.Plan {
	st := Read(cfg)
	plan := Build(Side{Name: "the engine", State: &st, Wrap: func(c action.Command) action.Command { return c }},
		Side{Name: "GitHub", State: &State{}, PushOnly: true})
	plan.Title = "Release the engine's work to GitHub"
	plan.Machine = "the engine"
	return plan
}

// Mirror keeps this client ready to work local, while it can reach the engine: every project the
// engine has that this machine does not is copied here as on the engine (mirror), and every project
// this machine has is fetched, so its branches are known here. It pushes nothing and moves no
// worktree; a handover does that. The app runs it with the settings sync, when its panel is
// opened, at most once an hour.
func Mirror(ctx context.Context, cfg *config.Config) (action.Plan, error) {
	if cfg.Role != config.RoleClient {
		return action.Plan{}, ErrNotClient
	}
	remote := action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
	engineState, unread := readEngine(ctx, remote)
	if engineState == nil {
		return action.Plan{}, fmt.Errorf("the engine %s, so there is nothing to mirror now", unread)
	}
	mine := readHere(ctx, cfg)
	plan := action.Plan{Title: "Keep this machine ready to work local", Machine: "this machine"}
	have := map[string]bool{}
	for _, r := range mine.Repos {
		have[r.Project] = true
		if r.Problem != "" {
			continue
		}
		plan.Commands = append(plan.Commands, action.Command{Argv: []string{"git", "-C", r.Path, "fetch", "-q", "origin"}, Why: "know GitHub's branches of " + r.Project + " here; no worktree moves"})
	}
	here := Side{Name: "this machine", State: &mine, Wrap: func(c action.Command) action.Command { return c }}
	engine := Side{Name: "the engine", State: engineState}
	for _, r := range engineState.Repos {
		if have[r.Project] || r.Problem != "" {
			continue
		}
		mirrorAll(&plan, &plan.Commands, r, engine, here, nil)
	}
	return plan, nil
}

func releasedPath() string { return filepath.Join(paths.StateDir(), "released.json") }

// SaveReleased records when the engine was last released.
func SaveReleased(at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(releasedPath()), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]time.Time{"at": at})
	return os.WriteFile(releasedPath(), b, 0o644)
}

// Released is when the engine was last released, if it was.
func Released() (time.Time, bool) {
	var v map[string]time.Time
	b, err := os.ReadFile(releasedPath())
	if err != nil || json.Unmarshal(b, &v) != nil {
		return time.Time{}, false
	}
	at, ok := v["at"]
	return at, ok
}

// readHere reads this machine, telling the context's Progress each project as it is read.
func readHere(ctx context.Context, cfg *config.Config) State {
	report := progressOf(ctx)
	// The same reads the engine makes, said as one line since they run in every project.
	const reads = "git ls-remote --heads origin, git for-each-ref, git worktree list and git status, in each project"
	return ReadWith(cfg, func(done, total int, project string) {
		report(Step{Side: "this machine", Done: done, Total: total, Project: project, Command: reads})
	})
}
