// Command render-casts turns every journey recording in a directory into an MP4 beside it,
// with the clause captions stacked above the terminal.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/termtest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: render-casts <dir>")
		os.Exit(2)
	}
	casts, _ := filepath.Glob(filepath.Join(os.Args[1], "*.cast"))
	failed := false
	rendered := 0
	for _, c := range casts {
		if strings.HasSuffix(c, ".captions.cast") {
			continue
		}
		out := strings.TrimSuffix(c, ".cast") + ".mp4"
		if err := termtest.Render(c, out); err != nil {
			fmt.Fprintf(os.Stderr, "render %s: %v\n", c, err)
			failed = true
			continue
		}
		rendered++
		fmt.Println("rendered", out)
	}
	if rendered == 0 && !failed {
		fmt.Fprintln(os.Stderr, "render-casts: no recordings found in", os.Args[1])
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}
