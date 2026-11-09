// Package clip moves an image on the clipboard from a client to the engine.
//
// Why pasting an image works at the engine and not over SSH: a terminal sends only text. Claude
// Code takes an image from the clipboard of the machine it runs on, the engine, when asked to paste
// one, so an image copied on a client never reaches it. SendImage puts the client's
// clipboard image on the engine's clipboard, as a plan of real commands, so pasting in the session
// then finds it there.
package clip

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KyleyHarris/engineroom/internal/action"
)

// EngineFile is where the image is put on the engine, under its home folder.
const EngineFile = ".cache/engineroom/clipboard.png"

// saveScript writes the clipboard's image, as PNG, to the file named by its argument; it fails
// with a plain reason when the clipboard holds no image.
const saveScript = `on run argv
try
set png to the clipboard as «class PNGf»
on error
error "The clipboard holds no image. Copy an image first (a screenshot to the clipboard is Control-Shift-Command-4)."
end try
set f to open for access (POSIX file (item 1 of argv)) with write permission
set eof f to 0
write png to f
close access f
end run`

// putScript puts the PNG file named by its argument on the clipboard.
const putScript = `on run argv
set the clipboard to (read (POSIX file (item 1 of argv)) as «class PNGf»)
end run`

// Save writes this machine's clipboard image to path (engineroom clipboard save).
func Save(path string) error { return osa(saveScript, path) }

// Put puts the PNG at path on this machine's clipboard (engineroom clipboard put).
func Put(path string) error { return osa(putScript, path) }

func osa(script, arg string) error {
	out, err := exec.Command("osascript", "-e", script, arg).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.Index(msg, "The clipboard holds no image"); i >= 0 {
			msg = strings.TrimSuffix(msg[i:], " (-2700)")
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// SendImage is the plan that puts this client's clipboard image on the engine's clipboard: save it
// here, copy it to the engine, and put it on the clipboard there. Pasting in a session on the engine then
// pastes it.
func SendImage(r action.Remote, local string) action.Plan {
	scp := action.Command{Argv: append(r.CopyArgs(), local, r.Host+":"+EngineFile), Why: "copy it to the engine"}
	return action.Plan{
		Title:   "Send the clipboard's image to the engine",
		Machine: "this machine and the engine",
		Commands: []action.Command{
			{Argv: []string{"engineroom", "clipboard", "save", local}, Why: "save the image on this Mac's clipboard"},
			r.Wrap(action.Command{Argv: []string{"mkdir", "-p", ".cache/engineroom"}, Why: "make the folder it goes in on the engine"}),
			scp,
			r.Wrap(action.Command{Argv: []string{"engineroom", "clipboard", "put", EngineFile}, Why: "put it on the engine's clipboard; paste in the session now"}),
		},
		Notes: []string{"Then paste in the session (Control-V in Claude Code): it takes the image from the engine's clipboard."},
	}
}

// LocalFile is where the image is saved on this machine before it is sent.
func LocalFile(stateDir string) string { return stateDir + string(os.PathSeparator) + "clipboard.png" }
