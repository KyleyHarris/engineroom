package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The dev server: `go run ./tools/site -serve` builds the site, serves it on this machine only,
// and builds it again whenever docs/ or site/ change, so the site can be read and worked on
// without anything but Go. Open pages reload themselves after each build through a small script
// the server adds to every page it serves; the built site itself never carries it, so the
// published site stays free of scripts.
//
// A build that fails, or that the check finds problems in (a link to a heading that was renamed,
// a decision that does not exist), is shown in place of every page until it is fixed, with the
// problems listed, so a broken edit is seen at once instead of a stale page that looks fine.

// reloadPath is the address the injected script listens on for "the site was built again".
const reloadPath = "/_site/reload"

// reloadScript is added before </body> of every page served. It reloads the page when the server
// says a build finished, and reconnects on its own when the server restarts.
const reloadScript = `<script>new EventSource("` + reloadPath + `").onmessage = function () { location.reload(); };</script>`

// pollEvery is how often the sources are looked at for a change: often enough that a save shows
// in about half a second, cheap enough (a stat of a few hundred files) to go unnoticed.
const pollEvery = 400 * time.Millisecond

// devServer holds the latest build and the pages waiting to hear of the next one.
type devServer struct {
	root string

	mu      sync.RWMutex
	dir     string // the folder of the last good build, served
	problem string // why the latest build is not served; empty when it is

	listeners map[chan struct{}]struct{}
	lmu       sync.Mutex

	builds []string // temporary folders made by builds still on disk, removed at the end
}

// Serve builds the site from the repository at root and serves it on 127.0.0.1:port until ctx is
// done, building again whenever its sources change. ready, when not nil, receives the address
// once the server is listening.
func Serve(ctx context.Context, root string, port int, log io.Writer, ready func(addr string)) error {
	s := &devServer{root: root, listeners: map[chan struct{}]struct{}{}}
	defer s.cleanup()
	s.rebuild(log)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	go s.watch(ctx, log)
	if ready != nil {
		ready(ln.Addr().String())
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// rebuild builds the site into a new folder and serves it in place of the last one, so a page
// is never served from a half-written build. A failed build keeps nothing new and records why.
func (s *devServer) rebuild(log io.Writer) {
	tmp, err := os.MkdirTemp("", "site-serve-")
	if err != nil {
		s.fail(log, err.Error())
		return
	}
	out := filepath.Join(tmp, "out")
	n, err := Build(s.root, out)
	if err != nil {
		_ = os.RemoveAll(tmp)
		s.fail(log, err.Error())
		return
	}
	if problems := Check(out); len(problems) > 0 {
		_ = os.RemoveAll(tmp)
		s.fail(log, strings.Join(problems, "\n"))
		return
	}
	s.mu.Lock()
	old := s.dir
	s.dir, s.problem = out, ""
	s.builds = append(s.builds, tmp)
	s.mu.Unlock()
	if old != "" {
		s.drop(filepath.Dir(old))
	}
	fmt.Fprintf(log, "site: %d pages built and checked\n", n)
	s.notify()
}

func (s *devServer) fail(log io.Writer, problem string) {
	s.mu.Lock()
	s.problem = problem
	s.mu.Unlock()
	fmt.Fprintf(log, "site: the build failed, and its pages show why:\n%s\n", problem)
	s.notify()
}

// drop removes a build folder no longer served.
func (s *devServer) drop(tmp string) {
	_ = os.RemoveAll(tmp)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, b := range s.builds {
		if b == tmp {
			s.builds = append(s.builds[:i], s.builds[i+1:]...)
			break
		}
	}
}

func (s *devServer) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.builds {
		_ = os.RemoveAll(b)
	}
	s.builds = nil
}

// watch builds again when anything the site is made from changes: docs/ and site/, apart from
// the folders built into (site/out, and site/.serve where the up script keeps its server).
func (s *devServer) watch(ctx context.Context, log io.Writer) {
	last := s.fingerprint()
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if now := s.fingerprint(); now != last {
				last = now
				s.rebuild(log)
			}
		}
	}
}

// fingerprint is every watched file's path, size and time of change, so any edit, addition,
// removal or rename changes it.
func (s *devServer) fingerprint() string {
	var lines []string
	for _, top := range []string{"docs", "site"} {
		_ = filepath.WalkDir(filepath.Join(s.root, top), func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil // a file removed while walking is seen on the next look
			}
			rel, _ := filepath.Rel(s.root, p)
			rel = filepath.ToSlash(rel)
			if e.IsDir() {
				if rel == "site/out" || rel == "site/.serve" {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			lines = append(lines, fmt.Sprintf("%s %d %d", rel, info.Size(), info.ModTime().UnixNano()))
			return nil
		})
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func (s *devServer) notify() {
	s.lmu.Lock()
	defer s.lmu.Unlock()
	for ch := range s.listeners {
		select {
		case ch <- struct{}{}:
		default: // already told, and not yet sent on
		}
	}
}

// ServeHTTP serves the reload stream, a page with the reload script added, or any other file of
// the current build as it is.
func (s *devServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == reloadPath {
		s.reloads(w, r)
		return
	}
	s.mu.RLock()
	dir, problem := s.dir, s.problem
	s.mu.RUnlock()

	p := path.Clean("/" + r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/") {
		p = path.Join(p, "index.html")
	}
	isPage := strings.HasSuffix(p, ".html")
	if problem != "" && isPage {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>The site did not build</title>"+
			"<body style=\"font:16px/1.5 system-ui;margin:2rem;max-width:60rem\"><h1>The site did not build</h1>"+
			"<p>Fix what is listed and save; this page reloads when the build passes.</p>"+
			"<pre style=\"white-space:pre-wrap;background:#f4f4f4;padding:1rem\">%s</pre>%s</body>",
			html.EscapeString(problem), reloadScript)
		return
	}
	if dir == "" {
		http.Error(w, "the site has not been built yet", http.StatusServiceUnavailable)
		return
	}
	file := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if !isPage {
		http.ServeFile(w, r, file)
		return
	}
	b, err := os.ReadFile(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b = s.inject(b)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// inject adds the dev server's scripts to a page: the reload script before </body>, and, when the
// site offers more than one style, the style switch: a script in <head> that puts back the style
// last chosen before the page is drawn, and a bar of buttons, one per style, that swaps the
// style's stylesheet in place and remembers the choice in this browser. A page without </head> or
// </body> gets them at the end.
func (s *devServer) inject(b []byte) []byte {
	before := func(b []byte, tag, add string) []byte {
		if i := bytes.LastIndex(b, []byte(tag)); i >= 0 {
			return append(b[:i:i], append([]byte(add), b[i:]...)...)
		}
		return append(b, add...)
	}
	end := reloadScript
	if styles := s.styles(); len(styles) > 1 {
		js, _ := json.Marshal(styles) // escapes <, > and &, so it is safe inside a script
		b = before(b, "</head>", "<script>"+strings.ReplaceAll(styleHeadJS, "STYLES", string(js))+"</script>")
		end = "<script>" + strings.ReplaceAll(styleBarJS, "STYLES", string(js)) + "</script>" + end
	}
	return before(b, "</body>", end)
}

// styles are the looks site.toml offers, read on each page so a style added while the server runs
// is offered at once; none when the settings cannot be read (the build says why).
func (s *devServer) styles() map[string]string {
	cfg, err := loadConfig(filepath.Join(s.root, "site", "site.toml"))
	if err != nil {
		return nil
	}
	return cfg.Styles
}

// styleHeadJS puts back the style last chosen in this browser, before the page is drawn.
const styleHeadJS = `(function () {
  try {
    var styles = STYLES, name = localStorage.getItem("site-style"), link = document.getElementById("site-style");
    if (name && link && Object.prototype.hasOwnProperty.call(styles, name)) {
      link.href = link.getAttribute("href").replace(/[^\/]+\.css$/, name + ".css");
    }
  } catch (e) {}
})();`

// styleBarJS draws the style switch: a small bar in the corner with a button per style.
const styleBarJS = `(function () {
  var styles = STYLES, link = document.getElementById("site-style");
  if (!link) return;
  var bar = document.createElement("div");
  bar.setAttribute("role", "group");
  bar.setAttribute("aria-label", "Style");
  bar.setAttribute("style", "position:fixed;right:12px;bottom:12px;z-index:9999;display:flex;gap:4px;align-items:center;padding:6px;border-radius:8px;background:rgb(17 17 17 / 0.85);font:600 12px system-ui,sans-serif;color:#fff;box-shadow:0 4px 18px rgb(0 0 0 / 0.3)");
  var label = document.createElement("span");
  label.textContent = "Style";
  label.style.padding = "0 6px";
  bar.appendChild(label);
  function current() { return (link.getAttribute("href").match(/([^\/]+)\.css$/) || [])[1]; }
  function paint() {
    var on = current();
    [].forEach.call(bar.querySelectorAll("button"), function (b) {
      var chosen = b.getAttribute("data-style") === on;
      b.style.background = chosen ? "#f0b23c" : "rgb(255 255 255 / 0.14)";
      b.style.color = chosen ? "#111" : "#fff";
      b.setAttribute("aria-pressed", chosen ? "true" : "false");
    });
  }
  Object.keys(styles).forEach(function (name) {
    var b = document.createElement("button");
    b.type = "button";
    b.textContent = styles[name];
    b.setAttribute("data-style", name);
    b.setAttribute("style", "font:inherit;border:0;border-radius:5px;padding:5px 9px;cursor:pointer");
    b.onclick = function () {
      link.href = link.getAttribute("href").replace(/[^\/]+\.css$/, name + ".css");
      try { localStorage.setItem("site-style", name); } catch (e) {}
      paint();
    };
    bar.appendChild(b);
  });
  paint();
  document.body.appendChild(bar);
})();`

// reloads holds a page's connection open and sends one event after each build, so the page
// reloads itself.
func (s *devServer) reloads(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	ch := make(chan struct{}, 1)
	s.lmu.Lock()
	s.listeners[ch] = struct{}{}
	s.lmu.Unlock()
	defer func() {
		s.lmu.Lock()
		delete(s.listeners, ch)
		s.lmu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, "retry: 1000\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "data: built\n\n")
			flusher.Flush()
		}
	}
}
