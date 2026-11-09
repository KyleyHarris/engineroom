package mux

import (
	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/zj"
)

// Zellij keeps sessions in Zellij, joined through a terminal app (the zj package's commands).
type Zellij struct{}

// Kind implements Backend.
func (Zellij) Kind() string { return "zellij" }

// List implements Backend.
func (Zellij) List() ([]Session, error) { return zj.List() }

// Ready implements Backend: the session's server answers a client (zj.Answers).
func (Zellij) Ready(name string) bool { return zj.Answers(name) }

// Create implements Backend: Zellij starts the session from its layout file.
func (Zellij) Create(name, dir string, l Layout) []action.Command {
	return []action.Command{zj.Create(name, l.File, dir)}
}

// Join implements Backend.
func (Zellij) Join(name string) action.Command { return zj.Attach(name) }

// InTerminal implements Backend: a Zellij session is joined in a terminal app.
func (Zellij) InTerminal() bool { return true }

// Switch implements Backend.
func (Zellij) Switch(name string) action.Command { return zj.Switch(name) }

// Stop implements Backend.
func (Zellij) Stop(name string) []string { return []string{"zellij", "kill-session", name} }

// Forget implements Backend: Zellij keeps an ended session to bring back, which is deleted.
func (Zellij) Forget(name string) []string { return []string{"zellij", "delete-session", name} }
