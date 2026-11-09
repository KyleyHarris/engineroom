package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startServe runs the dev server on a free port for the fixture repository and returns its base
// address and a function that stops it and waits for it to finish.
func startServe(t *testing.T, root string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, root, 0, io.Discard, func(a string) { addr <- a })
	}()
	var base string
	select {
	case a := <-addr:
		base = "http://" + a
	case err := <-done:
		t.Fatalf("the server stopped before listening: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not start listening")
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("the server stopped with an error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	}
	t.Cleanup(stop)
	return base, stop
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// eventually asks again until the answer satisfies ok, so a test waits for a rebuild without a
// fixed sleep that is either too short on a busy machine or wasted on a quiet one.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Guards what a reader of the dev server gets: pages with the reload script, other files as they
// are, and nothing written into the repository; and the temporary builds removed when it stops.
func TestServeServesPagesWithReload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	root := fixture(t, nil)
	base, stop := startServe(t, root)

	code, home := get(t, base+"/")
	if code != http.StatusOK || !strings.Contains(home, `<h1 id="top">tool-a</h1>`) {
		t.Fatalf("GET / = %d:\n%s", code, home)
	}
	if !strings.Contains(home, reloadScript+"</body>") {
		t.Errorf("the page has no reload script before </body>:\n%s", home)
	}
	code, page := get(t, base+"/guide/set-up.html")
	if code != http.StatusOK || !strings.Contains(page, `<h1 id="set-up">Set up</h1>`) {
		t.Errorf("GET a guide page = %d:\n%s", code, page)
	}
	code, css := get(t, base+"/assets/site.css")
	if code != http.StatusOK || css != `body { color: black; }` {
		t.Errorf("GET the stylesheet = %d %q, want it exactly as written", code, css)
	}
	if code, _ := get(t, base+"/guide/no-such-page.html"); code != http.StatusNotFound {
		t.Errorf("GET a missing page = %d, want 404", code)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "out")); err == nil {
		t.Error("serving wrote site/out into the repository")
	}

	stop()
	left, _ := filepath.Glob(filepath.Join(tmp, "site-serve-*"))
	if len(left) > 0 {
		t.Errorf("the server left its builds behind: %v", left)
	}
}

// Guards the edit loop: a saved change is served, an open page is told to reload, a broken build
// is shown in place of the page with its reason, and fixing it brings the page back.
func TestServeRebuildsOnChange(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	root := fixture(t, nil)
	base, _ := startServe(t, root)

	// An open page's reload stream.
	resp, err := http.Get(base + reloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	events := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") {
				events <- sc.Text()
			}
		}
	}()

	guide := filepath.Join(root, "docs", "guide.md")
	orig, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(orig), "Install it.", "Install it with care.", 1)
	if err := os.WriteFile(guide, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the edit to be served", func() bool {
		_, b := get(t, base+"/guide/tool-a-itself.html")
		return strings.Contains(b, "Install it with care.")
	})
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Error("the open page was not told to reload after the build")
	}

	broken := edited + "\nSee [the missing part](#nowhere).\n"
	if err := os.WriteFile(guide, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the broken build to be shown", func() bool {
		code, b := get(t, base+"/guide/set-up.html")
		return code == http.StatusInternalServerError && strings.Contains(b, "has no heading #nowhere")
	})

	if err := os.WriteFile(guide, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the page to come back after the fix", func() bool {
		code, b := get(t, base+"/guide/set-up.html")
		return code == http.StatusOK && strings.Contains(b, `<h1 id="set-up">`)
	})
}

// Guards the style switch: with more than one style the dev server adds it to every page it
// serves, naming each style, and the built site never carries it; with one style it is not added.
func TestServeOffersStyleSwitch(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	one := fixture(t, nil)
	base, _ := startServe(t, one)
	if _, home := get(t, base+"/"); strings.Contains(home, "site-style\")") || strings.Contains(home, `aria-label", "Style"`) {
		t.Errorf("a site with one style was given the style switch:\n%s", home)
	}

	toml, err := os.ReadFile(filepath.Join(one, "site", "site.toml"))
	if err != nil {
		t.Fatal(err)
	}
	two := fixture(t, map[string]string{
		"site/site.toml":              strings.Replace(string(toml), `plain = "Plain"`, "plain = \"Plain\"\nbold = \"Bold look\"", 1),
		"site/assets/styles/bold.css": `body { font-weight: bold; }`,
	})
	base, _ = startServe(t, two)
	_, home := get(t, base+"/")
	head, body, found := strings.Cut(home, "</head>")
	if !found {
		t.Fatalf("no </head> in the page:\n%s", home)
	}
	if !strings.Contains(head, `localStorage.getItem("site-style")`) {
		t.Errorf("the head does not put back the chosen style:\n%s", head)
	}
	if !strings.Contains(body, `"Bold look"`) || !strings.Contains(body, `"Plain"`) || !strings.Contains(body, `aria-label", "Style"`) {
		t.Errorf("the switch does not offer both styles:\n%s", body)
	}

	out := filepath.Join(t.TempDir(), "out")
	if _, err := Build(two, out); err != nil {
		t.Fatal(err)
	}
	if built := read(t, out, "index.html"); strings.Contains(built, "<script") {
		t.Errorf("the built page carries a script:\n%s", built)
	}
}
