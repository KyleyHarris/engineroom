package handoff

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
)

// Reading several projects at once keeps them in the configuration's order and reports each one
// exactly once, counting up to the total after a first report of the total alone. Fails if the
// order follows finishing order, a project is reported twice or missed, or the count skips.
func TestAReadingKeepsItsOrderAndCountsEachProject(t *testing.T) {
	cfg := &config.Config{}
	root := t.TempDir()
	for i := 0; i < 2*readAtOnce+1; i++ {
		// Folders that do not exist read at once, each as a problem, which is all the order and
		// the counting need.
		cfg.Projects = append(cfg.Projects, config.Project{Name: "project-" + strconv.Itoa(i), Path: filepath.Join(root, "none-"+strconv.Itoa(i))})
	}
	var mu sync.Mutex
	var counts []int
	seen := map[string]int{}
	st := ReadWith(cfg, func(done, total int, project string) {
		mu.Lock()
		defer mu.Unlock()
		if total != len(cfg.Projects) {
			t.Errorf("total %d, want %d", total, len(cfg.Projects))
		}
		counts = append(counts, done)
		if project != "" {
			seen[project]++
		}
	})
	for i, r := range st.Repos {
		if r.Project != cfg.Projects[i].Name || r.Problem == "" {
			t.Fatalf("repo %d is %q (%q), want %q read as a problem", i, r.Project, r.Problem, cfg.Projects[i].Name)
		}
	}
	for i, n := range counts {
		if n != i {
			t.Fatalf("counts %v, want 0 to %d in order", counts, len(cfg.Projects))
		}
	}
	for _, p := range cfg.Projects {
		if seen[p.Name] != 1 {
			t.Errorf("%s reported %d times", p.Name, seen[p.Name])
		}
	}
}

// The engine's progress lines (its standard error) reach the context's Progress as the engine's,
// with the command shown, and its reading (standard output) is still read whole; an engine that
// does not answer is said so. The engine is reached through a stand-in ssh on PATH, which stands
// for the network only. Fails if a step is lost or misnamed, the reading is spoiled by the
// progress lines, or an unreachable engine is not said as one.
func TestTheEnginesProgressIsPassedOn(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ssh := func(script string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ssh(`echo '{"progress":{"side":"","done":0,"total":2}}' >&2
echo '{"progress":{"side":"","done":1,"total":2,"project":"project-a"}}' >&2
echo '{"progress":{"side":"","done":2,"total":2,"project":"project-b"}}' >&2
echo '{"home":"/home/dev","repos":[{"project":"project-a","path":"/p","branches":[],"checkouts":[]}],"sessions":[]}'
`)
	var steps []Step
	ctx := WithProgress(context.Background(), func(s Step) { steps = append(steps, s) })
	st, unread := readEngine(ctx, action.Remote{Host: "engine.example"})
	if st == nil || len(st.Repos) != 1 || st.Repos[0].Project != "project-a" {
		t.Fatalf("reading %+v, unread %q", st, unread)
	}
	if len(steps) != 4 {
		t.Fatalf("steps %+v, want the start and three from the engine", steps)
	}
	for _, s := range steps {
		if s.Side != "the engine" || !strings.Contains(s.Command, "engineroom handoff state --json") {
			t.Fatalf("step %+v is not the engine's, with its command", s)
		}
	}
	if last := steps[3]; last.Done != 2 || last.Total != 2 || last.Project != "project-b" {
		t.Fatalf("last step %+v", last)
	}

	ssh("echo 'ssh: connect to host engine.example: Operation timed out' >&2; exit 255\n")
	if st, unread := readEngine(context.Background(), action.Remote{Host: "engine.example"}); st != nil || unread != "did not answer" {
		t.Fatalf("an unreachable engine: %+v, %q", st, unread)
	}
}
