package termtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recorder writes two asciicast v2 files that share one clock: the program's own output, and a
// two-row caption bar holding the scenario and the clause being proved. They are rendered
// separately and stacked, so a caption can never be overwritten by the program's drawing.
type recorder struct {
	mu       sync.Mutex
	start    time.Time
	main     *castFile
	captions *castFile
	cols     int
	last     string
}

type castFile struct {
	f *os.File
	w *bufio.Writer
}

type castHeader struct {
	Version   int               `json:"version"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Timestamp int64             `json:"timestamp"`
	Title     string            `json:"title,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// captionsPath is where the caption track of a recording is written.
func captionsPath(path string) string {
	return strings.TrimSuffix(path, ".cast") + ".captions.cast"
}

func newRecorder(path, title string, cols, rows int) (*recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	now := time.Now()
	main, err := newCast(path, castHeader{Version: 2, Width: cols, Height: rows, Timestamp: now.Unix(), Title: title,
		Env: map[string]string{"TERM": "xterm-256color"}})
	if err != nil {
		return nil, err
	}
	captions, err := newCast(captionsPath(path), castHeader{Version: 2, Width: cols, Height: 2, Timestamp: now.Unix(), Title: title})
	if err != nil {
		_ = main.close()
		return nil, err
	}
	return &recorder{start: now, main: main, captions: captions, cols: cols}, nil
}

func newCast(path string, h castHeader) (*castFile, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriter(f)
	head, _ := json.Marshal(h)
	_, _ = w.Write(head)
	_ = w.WriteByte('\n')
	return &castFile{f: f, w: w}, nil
}

func (c *castFile) event(t float64, kind, data string) {
	line, _ := json.Marshal([]any{t, kind, data})
	_, _ = c.w.Write(line)
	_ = c.w.WriteByte('\n')
}

func (c *castFile) close() error {
	if err := c.w.Flush(); err != nil {
		_ = c.f.Close()
		return err
	}
	return c.f.Close()
}

func (r *recorder) since() float64 {
	return time.Since(r.start).Seconds()
}

func (r *recorder) output(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.main.event(r.since(), "o", string(b))
}

// caption replaces the caption bar's text and leaves a marker in the main recording, so a
// player that understands markers can jump to each clause.
func (r *recorder) caption(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.since()
	r.last = text
	r.main.event(t, "m", text)
	// Clear the bar, then write the caption on its first row in reverse video so it reads as a
	// label rather than program output.
	line := text
	if len([]rune(line)) > r.cols {
		line = string([]rune(line)[:r.cols-1]) + "…"
	}
	r.captions.event(t, "o", "\x1b[2J\x1b[H\x1b[7m "+line+" \x1b[0m")
}

func (r *recorder) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.since()
	// Both tracks end at the same instant so the stacked video has one length.
	r.main.event(t, "o", "")
	r.captions.event(t, "o", "")
	err1 := r.main.close()
	err2 := r.captions.close()
	if err1 != nil {
		return err1
	}
	return err2
}

// Render turns a recording (and its caption track) into an MP4 at out, using agg to draw each
// track and ffmpeg to stack and encode them. Both tools are required; a missing one is an
// error, never a silently missing video.
func Render(castPath, out string) error {
	for _, tool := range []string{"agg", "ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("render needs %s on PATH", tool)
		}
	}
	dir, err := os.MkdirTemp("", "render-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	mainGIF := filepath.Join(dir, "main.gif")
	capGIF := filepath.Join(dir, "captions.gif")
	// A large idle limit keeps real time on both tracks; agg's default compresses pauses, and
	// it would compress the two tracks differently and pull the captions out of step.
	aggArgs := []string{"--idle-time-limit", "86400", "--last-frame-duration", "2", "--font-size", "18"}
	if out, err := exec.Command("agg", append(aggArgs, castPath, mainGIF)...).CombinedOutput(); err != nil {
		return fmt.Errorf("agg (main): %v\n%s", err, out)
	}
	if out, err := exec.Command("agg", append(aggArgs, captionsPath(castPath), capGIF)...).CombinedOutput(); err != nil {
		return fmt.Errorf("agg (captions): %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	filter := "[1:v]fps=15[c];[0:v]fps=15[m];[c][m]vstack=inputs=2,pad=ceil(iw/2)*2:ceil(ih/2)*2,format=yuv420p[v]"
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-i", mainGIF, "-i", capGIF,
		"-filter_complex", filter, "-map", "[v]", "-c:v", "libx264", "-movflags", "+faststart", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %v\n%s", err, b)
	}
	return nil
}
