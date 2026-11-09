// Package tui is the terminal interface: one Bubble Tea program whose screens follow the
// machine's state (no role yet, the engine not answering, the session list, an action running).
//
// Every screen is drawn by View from the model alone, and the header is drawn on every screen,
// so anything that must be visible everywhere (the machine's role, LOCAL) is put there once.
// Every action goes through a plan of real commands, shown while it runs (see runView), and is
// recorded in the history.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KyleyHarris/engineroom/internal/action"
	"github.com/KyleyHarris/engineroom/internal/config"
	"github.com/KyleyHarris/engineroom/internal/handoff"
	"github.com/KyleyHarris/engineroom/internal/history"
	"github.com/KyleyHarris/engineroom/internal/ops"
	"github.com/KyleyHarris/engineroom/internal/paths"
	"github.com/KyleyHarris/engineroom/internal/roots"
	"github.com/KyleyHarris/engineroom/internal/signal"
	"github.com/KyleyHarris/engineroom/internal/source"
)

type screen int

const (
	screenNoRole screen = iota
	screenConnecting
	screenEngineDown
	screenList
	screenProjects
	screenConfirmStop
	screenPreview
	screenRun
	screenHistory
	screenNewWork
	screenRoots
	screenBrowse
	screenRepoForm
	// screenRejoin waits for the engine after a join's connection dropped (rejoin.go).
	screenRejoin
)

// Intent is what the program was started to do, carried through an engine-down prompt so that
// retry and working locally continue the same request.
type Intent struct {
	// Open names a project to open straight away.
	Open string
	// Switch shows the session list for moving this Zellij window to another session.
	Switch bool
	// NewProject and NewWork start a piece of work on a project.
	NewProject, NewWork string
	// Clean asks to clean up CleanWork on CleanProject (the main worktree when CleanWork is
	// empty, which is refused).
	Clean                   bool
	CleanProject, CleanWork string
	// Edit names a session to open in the editor once the session list has been read.
	Edit string
	// NoJoin starts the session for Open or NewWork and leaves it running without joining it
	// (SESS-NEW-004).
	NoJoin bool
	// RemoteControl turns Remote Control on for this one request, whatever the remembered
	// choice is.
	RemoteControl bool
	// Join names a session to join (or bring back) straight away.
	Join string
	// Stack starts ("up") or stops ("down") the app stack of StackSession (WORK-STACK-001, 002).
	Stack, StackSession string
	// Clone brings one of a root's owner's repositories in (PROJ-CLONE-001).
	Clone *RepoRequest
	// Create makes a new repository in a root and on GitHub (PROJ-CREATE-001).
	Create *RepoRequest
	// JoinHere joins Join on this machine, on a client working on the engine: a session started
	// while it was offline (CONN-HANDOFF-006).
	JoinHere bool
	// Handoff shows the handoff in one direction, handoff.ToEngine or handoff.FromEngine, and runs it
	// on enter (CONN-HANDOFF-001, 004).
	Handoff string
}

// RepoRequest names a repository to bring into a root: its root, its name, the category it is
// registered in, and, for a new one, whether it is public on GitHub.
type RepoRequest struct {
	Root, Name, Category string
	Public               bool
}

// Model is the whole interface's state.
type Model struct {
	cfg    *config.Config
	intent Intent
	screen screen
	width  int

	src   source.Source
	local bool // working locally after the engine did not answer

	sessions []ops.SessionView
	readCmd  string
	loaded   bool
	cursor   int

	projects     []ops.ProjectView // in list order, each with the heading it is listed under
	projectsNote string            // why the project list is shown (empty list, unknown name)

	message  string // the latest problem or outcome, shown under the header
	stopFor  string // the session a stop is being confirmed for
	stopHere bool   // the session being stopped is on this machine, listed among the engine's

	// handoffNext is the handoff to show once the engine has answered, after going back online.
	handoffNext string
	// afterHandoff marks the plan on screen as the handoff a switch offered: leaving it carries
	// on with what the program was doing rather than ending the program.
	afterHandoff bool

	plan       action.Plan
	steps      []action.Step
	running    bool
	runOK      bool
	stepCh     chan action.Step
	doneCh     chan bool
	entry      *history.Entry
	histNote   string
	newFor     string // the project new work is being named for
	newName    string // the name typed so far
	history    []history.Entry
	historyBad int
	// copyNote says what the last copy sent to the terminal's clipboard, on the screens that
	// show an action's commands; cmdCursor is the command y copies there.
	copyNote  string
	cmdCursor int
	// layout is where the last frame drew its clickable rows (see mouse.go).
	layout *layout
	// quitAfter ends the program once the plan is done, for a command-line request
	// (engineroom open, engineroom switch) rather than the session list.
	quitAfter bool
	// stayAfter marks the running plan as a start without joining: once it succeeds the
	// developer is taken back to the session list, where the new session now is (SESS-NEW-004).
	stayAfter bool
	exitCode  int

	// joined is the engine's session the current join is attached to, when it was joined by name
	// (engineroom join, or enter on the list): if its connection drops, that session is joined again
	// (CONN-REJOIN-001). Empty for any other command that takes over the terminal.
	joined string
	// rejoin is the wait for the engine after joined's connection dropped; nil when not waiting.
	rejoin *rejoinWait

	height int

	// Bringing repositories in from GitHub: the roots, the root being browsed and its listing,
	// the filter typed over it, and the form for a clone's category or a new repository.
	roots      []config.Root
	rootsRead  bool
	browseRoot string
	listing    roots.Listing
	listed     bool
	ghProblem  *roots.Problem
	filter     string
	form       repoForm
}

// repoForm is the short form before a repository is brought in: for a clone only its category,
// for a new repository its name, category and visibility. field is the one being typed in.
type repoForm struct {
	create   bool
	name     string
	category string
	public   bool
	field    int
}

// New builds the interface for this machine's configuration and what it was asked to do.
func New(cfg *config.Config, intent Intent) Model {
	m := Model{cfg: cfg, intent: intent, layout: &layout{}}
	switch {
	case cfg == nil || cfg.Role == "":
		// Every action needs to know which machine it runs on, so nothing else is offered
		// (CONN-SETUP-001).
		m.screen = screenNoRole
	case cfg.Role == config.RoleClient && cfg.Offline:
		// Switched offline: this machine is used as though it were the engine, and the engine is
		// never asked (CONN-OFFLINE-001, 002).
		m.src = source.Local{Cfg: cfg, Label: "this machine"}
		m.local = true
		m.screen = screenList
	case cfg.Role == config.RoleClient:
		m.src = remoteSource(cfg)
		m.screen = screenConnecting
	default:
		m.src = source.Local{Cfg: cfg, Label: "engine"}
		m.screen = screenList
	}
	if cfg != nil && cfg.Role == config.RoleClient && (intent.Handoff != "" || intent.JoinHere) {
		// A handoff asks the engine itself and says when it does not answer, and a session here
		// needs nothing of the engine, so neither waits for the engine first.
		m.screen = screenList
		if intent.JoinHere {
			m.src = source.Here(cfg)
		}
	}
	m.stayAfter = intent.NoJoin
	if !intent.JoinHere {
		m.joined = intent.Join
	}
	m.quitAfter = intent.Handoff != "" || intent.Open != "" || intent.Switch || intent.NewWork != "" || intent.Clean || intent.Edit != "" || intent.Clone != nil || intent.Create != nil || intent.Stack != "" || intent.Join != ""
	return m
}

func remoteSource(cfg *config.Config) source.Remote {
	return source.Remote{R: remoteOf(cfg), Editor: cfg.Editor.Command}
}

func remoteOf(cfg *config.Config) action.Remote {
	return action.Remote{Host: cfg.Engine.Host, SSHConfig: cfg.Engine.SSHConfig, ConnectTimeout: cfg.Engine.ConnectTimeout.Duration}
}

// ExitCode is the program's exit status once the interface ends.
func (m Model) ExitCode() int { return m.exitCode }

// ---- messages ----

type probeMsg struct{ err error }
type sessionsMsg struct {
	list []ops.SessionView
	read string
	err  error
}
type projectsMsg struct {
	views []ops.ProjectView
	err   error
}
type planMsg struct {
	plan    action.Plan
	err     error
	preview bool
}
type rootsMsg struct {
	list []config.Root
	err  error
}
type browseMsg struct {
	listing roots.Listing
	err     error
}
type handoffMsg struct {
	plan action.Plan
	err  error
}
type stepMsg action.Step
type doneMsg struct{ ok bool }

// handedBackMsg is the terminal coming back from a command it was handed to: how that command
// ended, the command as shown, and the end of what it wrote to standard error.
type handedBackMsg struct {
	err     error
	command string
	said    string
	// lasted is how long the command held the terminal: a join that could not attach ends at
	// once, one that was attached ran until its session ended or was left.
	lasted time.Duration
}

// attachedFor is how long a join must have held the terminal to count as attached. A join that
// cannot attach fails within ssh's 5 second connect timeout; this leaves room above it.
const attachedFor = 10 * time.Second

type historyMsg struct {
	entries []history.Entry
	bad     int
	err     error
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	switch m.screen {
	case screenConnecting:
		return m.probe()
	case screenList:
		return m.start()
	}
	return nil
}

// start does what the program was asked: open a project, or show the session list.
func (m Model) start() tea.Cmd {
	if d := m.intent.Handoff; d != "" {
		return m.handoff(d)
	}
	if m.intent.Clean {
		// Clean up always shows its commands and waits for enter: it removes things.
		src, project, work := m.src, m.intent.CleanProject, m.intent.CleanWork
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Clean(ctx, project, work, "") }, true)
	}
	if name := m.intent.Join; name != "" {
		src := m.src
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.OnSession(ctx, "join", name, ops.Start{}) }, false)
	}
	if kind := m.intent.Stack; kind != "" {
		src, session := m.src, m.intent.StackSession
		start := m.startFor(true)
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Stack(ctx, kind, session, start) }, false)
	}
	if r := m.intent.Clone; r != nil {
		src := m.src
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Clone(ctx, r.Root, r.Name, r.Category) }, false)
	}
	if r := m.intent.Create; r != nil {
		// Creating publishes a repository on GitHub, so its commands wait for enter.
		src := m.src
		return m.makePlan(func(ctx context.Context) (action.Plan, error) {
			return src.Create(ctx, r.Root, r.Name, r.Category, r.Public)
		}, true)
	}
	if m.intent.NewWork != "" {
		src, project, work, start := m.src, m.intent.NewProject, m.intent.NewWork, m.startFor(m.intent.NoJoin)
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.NewWork(ctx, project, work, start) }, false)
	}
	if m.intent.Open != "" {
		src, name, start := m.src, m.intent.Open, m.startFor(m.intent.NoJoin)
		return m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Open(ctx, name, start) }, false)
	}
	return m.loadSessions()
}

func (m Model) probe() tea.Cmd {
	r, ok := m.src.(source.Remote)
	if !ok {
		return nil
	}
	return func() tea.Msg { return probeMsg{err: r.Probe(context.Background())} }
}

func (m Model) loadSessions() tea.Cmd {
	src, cfg := m.src, m.cfg
	return func() tea.Msg {
		l, read, err := src.Sessions(context.Background())
		if _, remote := src.(source.Remote); remote && err == nil {
			// The engine's sessions, then this machine's own still running (CONN-HANDOFF-006).
			l = append(l, source.HereSessions(cfg)...)
			read += "; on this machine: " + source.HereRead
		}
		return sessionsMsg{list: l, read: read, err: err}
	}
}

// handoff reads both machines and builds the handoff in one direction.
func (m Model) handoff(direction string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		p, err := handoff.ForClient(context.Background(), cfg, direction)
		return handoffMsg{plan: p, err: err}
	}
}

func (m Model) loadProjects() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		v, err := src.Projects(context.Background())
		return projectsMsg{views: v, err: err}
	}
}

func (m Model) makePlan(f func(context.Context) (action.Plan, error), preview bool) tea.Cmd {
	return func() tea.Msg {
		p, err := f(context.Background())
		return planMsg{plan: p, err: err, preview: preview}
	}
}

// Init's helpers above never touch the model; Update owns every change.

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case rootsMsg:
		if msg.err != nil {
			return m.problem(msg.err)
		}
		m.roots, m.rootsRead = msg.list, true
		if m.cursor >= len(m.roots) {
			m.cursor = 0
		}
		return m, nil
	case browseMsg:
		return m.gotListing(msg)
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case probeMsg:
		if msg.err != nil {
			m.screen = screenEngineDown
			m.message = msg.err.Error()
			return m, nil
		}
		m.screen = screenList
		m.message = ""
		if d := m.handoffNext; d != "" {
			// Back online and the engine answers: what this machine holds is shown before the
			// list (CONN-HANDOFF-001).
			m.handoffNext = ""
			m.message = "Checking what this machine holds that is not on GitHub ..."
			return m, m.handoff(d)
		}
		return m, m.start()
	case sessionsMsg:
		return m.gotSessions(msg)
	case projectsMsg:
		if msg.err != nil {
			return m.problem(msg.err)
		}
		m.projects = msg.views
		if m.cursor >= len(m.projects) {
			m.cursor = 0
		}
		return m, nil
	case planMsg:
		return m.gotPlan(msg)
	case handoffMsg:
		return m.gotHandoff(msg)
	case stepMsg:
		return m.gotStep(action.Step(msg))
	case doneMsg:
		return m.gotDone(msg.ok)
	case historyMsg:
		if msg.err != nil {
			return m.problem(msg.err)
		}
		m.history, m.historyBad = msg.entries, msg.bad
		if m.cursor >= len(m.history) {
			m.cursor = 0
		}
		return m, nil
	case rejoinMsg:
		return m.gotRejoin(msg)
	case rejoinTickMsg:
		if m.screen == screenRejoin && m.rejoin != nil {
			return m, m.checkRejoin()
		}
		return m, nil
	case handedBackMsg:
		m.finishJoin(msg)
		// A join whose connection to the engine dropped waits for the engine and joins the same session
		// again, rather than ending as the session's end (CONN-REJOIN-001, 002).
		if w, ok := m.droppedJoin(msg); ok {
			m.rejoin, m.screen, m.message = w, screenRejoin, ""
			return m, m.checkRejoin()
		}
		m.joined = ""
		// Started only to join, the program ends with the session, as it does when Zellij exits
		// with no word of a problem: a session ended or killed while joined also ends Zellij with
		// a failure status, and that is the session's end, not a failed join. So does one that
		// was attached for a while and then ended with words, as a join over SSH does when its
		// session is restarted or stopped (ssh says the connection closed): it was the session's
		// end, and leaving the program open there left the app's tab on the session list, which
		// the next join then brought forward instead of the session.
		if m.quitAfter && (msg.err == nil || msg.said == "" || msg.lasted >= attachedFor) {
			return m, tea.Quit
		}
		m.screen = screenList
		m.message = ""
		if msg.err != nil {
			// A join that failed and said why is said again here, with the command's own words,
			// and the program stays open even when it was started only to join: started in a
			// window of its own (as the app does), quitting would close the window before the
			// reason could be read.
			m.message = msg.command + " ended with " + msg.err.Error()
			if msg.said != "" {
				m.message += ": " + msg.said
			}
			if m.quitAfter {
				m.exitCode = 1
			}
		}
		return m, m.loadSessions()
	}
	return m, nil
}

// finishJoin records how a join that took over the terminal ended, as its step and the entry's
// outcome: failed when Zellij ended with a failure and said why (it could not attach), done
// otherwise, including a session that ended or was stopped while joined, which also ends Zellij
// with a failure status but says nothing of one.
func (m *Model) finishJoin(msg handedBackMsg) {
	if m.entry == nil {
		return
	}
	step := action.Step{Index: len(m.plan.Commands) - 1, Status: action.StatusOK}
	status := history.Done
	if msg.err != nil && msg.said != "" {
		step.Status, step.Output, status = action.StatusFailed, msg.said, history.Failed
		var ee *exec.ExitError
		if errors.As(msg.err, &ee) {
			step.Exit = ee.ExitCode()
		}
	}
	m.entry.Update(step)
	if err := m.entry.Finish(status); err != nil && m.histNote == "" {
		m.histNote = "The history could not be written: " + err.Error()
	}
	m.entry = nil
}

func (m Model) problem(err error) (tea.Model, tea.Cmd) {
	if errors.Is(err, source.ErrEngineDown) {
		m.screen = screenEngineDown
		m.message = err.Error()
		return m, nil
	}
	m.message = err.Error()
	if m.quitAfter && m.screen != screenProjects {
		m.exitCode = 1
	}
	return m, nil
}

func (m Model) gotSessions(msg sessionsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.problem(msg.err)
	}
	m.sessions, m.readCmd, m.loaded = msg.list, msg.read, true
	if m.cursor >= len(m.sessions) {
		m.cursor = max(0, len(m.sessions)-1)
	}
	if name := m.intent.Edit; name != "" {
		// engineroom edit <session>: the list just read is what the plan is built from, so the
		// request is answered from it alone. Asked once; the list is shown afterwards.
		m.intent.Edit = ""
		for _, s := range m.sessions {
			if s.Name == name {
				return m.edit(s)
			}
		}
		m.exitCode = 1
		m.message = fmt.Sprintf("There is no session called %q on %s.", name, on(m.src.Where()))
		return m, nil
	}
	if len(m.sessions) == 0 && !m.intent.Switch {
		// An empty list leads somewhere: the projects that can be opened (SESS-LIST-004).
		m.screen = screenProjects
		m.projectsNote = "There are no sessions."
		m.cursor = 0
		return m, m.loadProjects()
	}
	return m, nil
}

func (m Model) gotPlan(msg planMsg) (tea.Model, tea.Cmd) {
	var nr *ops.NotRegisteredError
	if errors.As(msg.err, &nr) {
		// The name is answered with the names that exist (SESS-NEW-003); nothing is created.
		m.screen = screenProjects
		m.projectsNote = fmt.Sprintf("%q is not a registered project.", nr.Name)
		m.projects = make([]ops.ProjectView, len(nr.Registered))
		for i, n := range nr.Registered {
			m.projects[i] = ops.ProjectView{Name: n}
		}
		m.cursor = 0
		m.quitAfter = false
		return m, nil
	}
	if msg.err != nil {
		return m.problem(msg.err)
	}
	if err := msg.plan.Validate(); err != nil {
		return m.problem(err)
	}
	m.plan = msg.plan
	m.cmdCursor, m.copyNote = 0, ""
	m.steps = make([]action.Step, len(msg.plan.Commands))
	for i := range m.steps {
		m.steps[i] = action.Step{Index: i, Status: action.StatusPending}
	}
	if msg.preview {
		// LEARN-SHOW-001: the commands are listed and nothing runs until asked.
		m.screen = screenPreview
		return m, nil
	}
	return m.run()
}

// gotHandoff shows a handoff. Offered by a switch with nothing to run, it is said in a line over
// the list and the program carries on; with commands, they are shown and wait for enter, as any
// action that changes things on two machines should (CONN-HANDOFF-001, 004). Asked for by name
// (engineroom handoff), it is always shown.
func (m Model) gotHandoff(msg handoffMsg) (tea.Model, tea.Cmd) {
	fromSwitch := m.intent.Handoff == ""
	if msg.err != nil {
		if fromSwitch {
			m.message = "The handoff could not be read: " + msg.err.Error()
			return m, m.start()
		}
		return m.problem(msg.err)
	}
	if fromSwitch && len(msg.plan.Commands) == 0 {
		m.message = strings.Join(msg.plan.Notes, " ")
		return m, m.start()
	}
	m.afterHandoff = fromSwitch
	m.message = ""
	return m.gotPlan(planMsg{plan: msg.plan, preview: true})
}

// leavePlan goes back from an action's screen: to the program's request after a handoff a switch
// offered, out of the program for a command-line request, otherwise to the session list, read
// again when the action ran (reload) and as it was when it only previewed.
func (m Model) leavePlan(reload bool) (tea.Model, tea.Cmd) {
	if m.afterHandoff {
		m.afterHandoff = false
		m.screen = screenList
		return m, m.start()
	}
	if m.quitAfter {
		return m, tea.Quit
	}
	m.screen = screenList
	if !reload {
		return m, nil
	}
	return m, m.loadSessions()
}

// run starts the current plan, recording it in the history first.
func (m Model) run() (tea.Model, tea.Cmd) {
	if len(m.plan.Commands) == 0 {
		// A handoff with nothing to hand over has nothing to run.
		return m, nil
	}
	m.screen = screenRun
	m.running = true
	m.message = ""
	entry, err := history.Begin(m.plan)
	m.entry = entry
	m.histNote = ""
	if err != nil {
		// The history is best effort: losing it is reported once and never stops the action
		// (learning obligation O6).
		m.histNote = "The history could not be written: " + err.Error()
	}
	m.stepCh = make(chan action.Step, len(m.plan.Commands)*3+1)
	m.doneCh = make(chan bool, 1)
	plan, stepCh, doneCh := m.plan, m.stepCh, m.doneCh
	go func() {
		ok := action.Run(context.Background(), plan, func(s action.Step) { stepCh <- s })
		doneCh <- ok
	}()
	return m, m.listen()
}

func (m Model) listen() tea.Cmd {
	stepCh, doneCh := m.stepCh, m.doneCh
	return func() tea.Msg {
		select {
		case s := <-stepCh:
			return stepMsg(s)
		case ok := <-doneCh:
			// Drain steps sent just before completion so none is lost.
			select {
			case s := <-stepCh:
				doneCh <- ok
				return stepMsg(s)
			default:
			}
			return doneMsg{ok: ok}
		}
	}
}

func (m Model) gotStep(s action.Step) (tea.Model, tea.Cmd) {
	if s.Index >= 0 && s.Index < len(m.steps) {
		m.steps[s.Index] = s
	}
	if m.entry != nil {
		m.entry.Update(s)
	}
	return m, m.listen()
}

func (m Model) gotDone(ok bool) (tea.Model, tea.Cmd) {
	m.running = false
	m.runOK = ok
	status := history.Done
	if !ok {
		status = history.Failed
		for _, s := range m.steps {
			if s.Status == action.StatusInterrupted {
				status = history.Interrupted
			}
		}
	}
	last := m.plan.Commands[len(m.plan.Commands)-1]
	// A join hands the terminal to Zellij after the other commands have run; its outcome is known
	// only when Zellij hands it back, so the history entry is finished then (handedBackMsg). Before,
	// it was finished here as done, and a join that failed read as a success in the history.
	if m.entry != nil && !(ok && last.Interactive) {
		if err := m.entry.Finish(status); err != nil && m.histNote == "" {
			m.histNote = "The history could not be written: " + err.Error()
		}
	}
	if !ok {
		// A failed start without joining stays on the run screen with the failure, like any
		// other action, and the next action starts clean.
		m.stayAfter = false
		m.exitCode = 1
		return m, nil
	}
	if last.Interactive {
		cmd := exec.Command(last.Argv[0], last.Argv[1:]...)
		cmd.Dir = last.Dir
		// What the command writes to standard error still reaches the terminal, and its end is
		// kept: when it fails, the terminal is redrawn over its words, so they are said again.
		said := &tailWriter{}
		cmd.Stderr = io.MultiWriter(os.Stderr, said)
		shown := last.Display()
		began := time.Now()
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return handedBackMsg{err: err, command: shown, said: said.Said(), lasted: time.Since(began)}
		})
	}
	// A switch has moved the window away, so nothing is left to read here. Any other finished
	// action stays on screen with its outcome until the developer leaves it.
	if m.intent.Switch {
		return m, tea.Quit
	}
	if m.stayAfter {
		// Started without joining: the developer stays where they were, and the list shows the
		// session now running. The commands that ran are in the history (SESS-NEW-004).
		m.stayAfter = false
		if m.quitAfter {
			return m, tea.Quit
		}
		m.screen = screenList
		m.message = m.plan.Title + ": the session is running. Join it from this list when you want it."
		return m, m.loadSessions()
	}
	return m, nil
}

// toggleOffline switches a client offline or back online and remembers it in this machine's
// configuration (CONN-OFFLINE-001, 002). Nothing is stopped either way: sessions started on this
// machine while offline keep running, and the engine's keep running (CONN-OFFLINE-003). Going online
// reaches the engine first, so an engine that does not answer is the usual prompt.
func (m Model) toggleOffline() (tea.Model, tea.Cmd) {
	if m.cfg == nil || m.cfg.Role != config.RoleClient {
		m.message = "Only a client can go offline; this machine is the engine."
		return m, nil
	}
	m.cfg.Offline = !m.cfg.Offline
	m.message = ""
	if err := m.cfg.Save(); err != nil {
		m.message = "The switch applies now but could not be remembered: " + err.Error()
	}
	m.cursor, m.loaded, m.sessions = 0, false, nil
	if m.cfg.Offline {
		m.src = source.Local{Cfg: m.cfg, Label: "this machine"}
		m.local = true
		m.screen = screenList
		m.message = "Checking what the engine holds that is not on GitHub ..."
		// What the engine holds that this machine does not is offered first, while the engine may
		// still answer; then the program carries on with what it was asked, on this machine
		// (CONN-HANDOFF-004, 005).
		return m, m.handoff(handoff.FromEngine)
	}
	m.src = remoteSource(m.cfg)
	m.local = false
	m.screen = screenConnecting
	m.handoffNext = handoff.ToEngine
	return m, m.probe()
}

// startFor is how a session this request creates is started: Remote Control as this machine
// remembers it (or as asked for this one request), joined unless noJoin. On a client this is
// the client's own choice, sent with the request to the engine (SESS-REMOTE-003).
func (m Model) startFor(noJoin bool) ops.Start {
	remote := m.intent.RemoteControl
	if m.cfg != nil && m.cfg.Agent.RemoteControl {
		remote = true
	}
	return ops.Start{RemoteControl: remote, NoJoin: noJoin}
}

// toggleRemoteControl flips the remembered Remote Control choice and saves it in this machine's
// configuration, so it holds for later runs (SESS-REMOTE-001). A failed save is reported and
// the choice still applies for this run, so the screen never shows a state that is not used.
func (m Model) toggleRemoteControl() (tea.Model, tea.Cmd) {
	m.cfg.Agent.RemoteControl = !m.cfg.Agent.RemoteControl
	m.message = ""
	if err := m.cfg.Save(); err != nil {
		m.message = "The Remote Control choice applies now but could not be remembered: " + err.Error()
	}
	return m, nil
}

// offlineHint is the offline key's hint, on a client only.
func (m Model) offlineHint() string {
	if m.cfg == nil || m.cfg.Role != config.RoleClient {
		return ""
	}
	if m.cfg.Offline {
		return keyStyle.Render("f") + " go online   "
	}
	return keyStyle.Render("f") + " go offline   "
}

// remoteControlLine shows the remembered choice where a session is about to be started.
func (m Model) remoteControlLine() string {
	state := dimStyle.Render("off")
	if m.cfg != nil && m.cfg.Agent.RemoteControl {
		state = okStyle.Render("on")
	}
	return "Remote Control: " + state + dimStyle.Render("  for new sessions, reached in the Claude app by the session's name")
}

// ---- keys ----

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.screen {
	case screenNoRole, screenConnecting:
		// Nothing may start until a role is set, or while the engine is being reached.
		if key == "q" {
			return m, tea.Quit
		}
	case screenEngineDown:
		return m.engineDownKey(key)
	case screenRejoin:
		return m.rejoinKey(key)
	case screenList:
		return m.listKey(key)
	case screenProjects:
		return m.projectsKey(key)
	case screenConfirmStop:
		return m.confirmKey(key)
	case screenPreview:
		if mm, cmd, ok := m.copyKeys(key, true); ok {
			return mm, cmd
		}
		switch key {
		case "enter":
			return m.run()
		case "esc", "q":
			return m.leavePlan(false)
		}
	case screenHistory:
		return m.historyKey(key)
	case screenNewWork:
		return m.newWorkKey(k)
	case screenRoots:
		return m.rootsKey(key)
	case screenBrowse:
		return m.browseKey(k)
	case screenRepoForm:
		return m.formKey(k)
	case screenRun:
		if !m.running {
			if mm, cmd, ok := m.copyKeys(key, true); ok {
				return mm, cmd
			}
			switch key {
			case "q":
				return m, tea.Quit
			case "esc", "enter":
				return m.leavePlan(true)
			}
		}
	}
	return m, nil
}

func (m Model) engineDownKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "r":
		// Retry asks the engine again and carries on with the same request once it answers;
		// while it still does not answer, the same question comes back (CONN-DOWN-003/004).
		m.screen = screenConnecting
		m.message = ""
		// Retrying always means the engine, even after a look at working locally.
		if _, ok := m.src.(source.Remote); !ok {
			m.src = remoteSource(m.cfg)
		}
		return m, m.probe()
	case "l":
		return m.workLocally()
	case "f":
		return m.toggleOffline()
	case "s", "q":
		// Stop changes nothing on either machine (CONN-DOWN-002).
		m.exitCode = 1
		return m, tea.Quit
	}
	return m, nil
}

// workLocally switches to this machine's own copy of the project. It needs the project to be
// registered here with a folder that exists; creating a copy unasked would change things
// outside the program's own state (CONN-LOCAL-003).
func (m Model) workLocally() (tea.Model, tea.Cmd) {
	// Read the configuration again: the developer may have registered this machine's copy
	// while the question was on screen.
	if fresh, err := config.Load(); err == nil {
		m.cfg = fresh
	}
	if m.intent.Open != "" {
		p, ok := m.cfg.Project(m.intent.Open)
		if !ok || !dirExists(p.Path) {
			m.message = fmt.Sprintf("This machine has no copy of %s, so it cannot be worked on locally.", m.intent.Open)
			return m, nil
		}
	}
	m.local = true
	m.src = source.Local{Cfg: m.cfg, Label: "this machine"}
	m.screen = screenList
	m.message = ""
	return m, m.start()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// srcFor is where an action on a listed session is done: this machine for one listed as here,
// otherwise where this machine's actions are done (CONN-HANDOFF-006).
func (m Model) srcFor(s ops.SessionView) source.Source {
	if s.Here {
		return source.Here(m.cfg)
	}
	return m.src
}

func (m Model) selected() (ops.SessionView, bool) {
	if m.cursor < 0 || m.cursor >= len(m.sessions) {
		return ops.SessionView{}, false
	}
	return m.sessions[m.cursor], true
}

func (m Model) listKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.sessions)-1 {
			m.cursor++
		}
	case "r":
		return m, m.loadSessions()
	case "f":
		return m.toggleOffline()
	case "h":
		return m.openHistory()
	case "o":
		m.screen = screenProjects
		m.projectsNote = ""
		m.cursor = 0
		return m, m.loadProjects()
	case "enter", "p":
		s, ok := m.selected()
		if !ok {
			return m, nil
		}
		kind := "join"
		if m.intent.Switch {
			kind = "switch"
		}
		src, name := m.srcFor(s), s.Name
		// A join of one of the engine's sessions is joined again if its connection drops
		// (CONN-REJOIN-001); a session on this machine has no connection to lose.
		m.joined = ""
		if kind == "join" && !s.Here {
			m.joined = name
		}
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.OnSession(ctx, kind, name, ops.Start{}) }, key == "p")
	case "c":
		s, ok := m.selected()
		if !ok {
			return m, nil
		}
		src, name := m.srcFor(s), s.Name
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Clean(ctx, "", "", name) }, true)
	case "e":
		if s, ok := m.selected(); ok && !m.intent.Switch {
			return m.edit(s)
		}
	case "u", "d":
		// The selected session's app stack, through the repository's up or down script
		// (WORK-STACK-001, 002).
		s, ok := m.selected()
		if !ok || m.intent.Switch {
			return m, nil
		}
		kind := map[string]string{"u": "up", "d": "down"}[key]
		src, name := m.srcFor(s), s.Name
		start := m.startFor(true)
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Stack(ctx, kind, name, start) }, false)
	case "x":
		s, ok := m.selected()
		if !ok || s.Ended {
			return m, nil
		}
		// Stopping always asks first (SESS-STOP-001).
		m.stopFor, m.stopHere = s.Name, s.Here
		m.screen = screenConfirmStop
	}
	return m, nil
}

// edit opens a listed session's worktree in the editor. The plan is built on this machine from
// the listed session, so it needs no request of its own; a refusal stays on the list with its
// reason.
func (m Model) edit(s ops.SessionView) (tea.Model, tea.Cmd) {
	plan, err := m.srcFor(s).Edit(s)
	return m.gotPlan(planMsg{plan: plan, err: err})
}

func (m Model) projectsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, tea.Quit
	case "h":
		return m.openHistory()
	case "n":
		if m.cursor < len(m.projects) {
			m.newFor, m.newName = m.projects[m.cursor].Name, ""
			m.screen = screenNewWork
		}
	case "esc":
		m.screen = screenList
		m.cursor = 0
		return m, m.loadSessions()
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.projects)-1 {
			m.cursor++
		}
	case "m":
		return m.toggleRemoteControl()
	case "f":
		// The offline switch is on this screen too: with no sessions, this is the first screen.
		return m.toggleOffline()
	case "g":
		// Bring a repository in from GitHub, through the roots (PROJ-BROWSE-001).
		m.screen, m.cursor, m.rootsRead, m.message = screenRoots, 0, false, ""
		return m, m.loadRoots()
	case "enter", "p", "s":
		if m.cursor >= len(m.projects) {
			return m, nil
		}
		// s starts the project's session and stays here (SESS-NEW-004); enter opens and joins.
		src, name, start := m.src, m.projects[m.cursor].Name, m.startFor(key == "s")
		m.stayAfter = key == "s"
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.Open(ctx, name, start) }, key == "p")
	}
	return m, nil
}

func (m Model) confirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y":
		src, name := m.src, m.stopFor
		if m.stopHere {
			src = source.Here(m.cfg)
		}
		m.stopFor, m.stopHere = "", false
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.OnSession(ctx, "stop", name, ops.Start{}) }, false)
	case "n", "esc", "q":
		// Declining leaves the session exactly as it was (SESS-STOP-003).
		m.stopFor, m.stopHere = "", false
		m.screen = screenList
		m.message = "Nothing was stopped."
		return m, m.loadSessions()
	}
	return m, nil
}

// ---- views ----

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	if m.message != "" && m.screen != screenEngineDown {
		// Wrapped to the screen, so a long message (one holding a path and the command that
		// fixes it) is read whole rather than cut at the edge.
		b.WriteString(warnStyle.Width(m.cols() - 1).Render(m.message))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	switch m.screen {
	case screenNoRole:
		b.WriteString(m.noRoleView())
	case screenConnecting:
		b.WriteString(dimStyle.Render("Reaching the engine " + m.cfg.Engine.Host + " ..."))
		b.WriteString("\n")
	case screenEngineDown:
		b.WriteString(m.engineDownView())
	case screenRejoin:
		b.WriteString(m.rejoinView())
	case screenList:
		b.WriteString(m.listView())
	case screenProjects:
		b.WriteString(m.projectsView())
	case screenConfirmStop:
		b.WriteString(fmt.Sprintf("Stop the session %s? Only this session is stopped.\n\n", titleStyle.Render(m.stopFor)))
		b.WriteString(keyStyle.Render("y") + " stop it   " + keyStyle.Render("n") + " keep it running\n")
	case screenPreview:
		b.WriteString(m.previewView())
	case screenRun:
		b.WriteString(m.runView())
	case screenHistory:
		b.WriteString(m.historyView())
	case screenRoots:
		b.WriteString(m.rootsView())
	case screenBrowse:
		b.WriteString(m.browseView())
	case screenRepoForm:
		b.WriteString(m.formView())
	case screenNewWork:
		b.WriteString(fmt.Sprintf("New work on %s. Its name becomes the branch and the end of the worktree's folder name.\n\n", titleStyle.Render(m.newFor)))
		b.WriteString("Name: " + m.newName + keyStyle.Render("_") + "\n\n")
		b.WriteString(m.remoteControlLine() + dimStyle.Render("  (change it on the projects screen)") + "\n\n")
		b.WriteString(dimStyle.Render(keyStyle.Render("enter")+" start it   "+keyStyle.Render("esc")+" cancel") + "\n")
	}
	if m.layout == nil {
		return b.String()
	}
	return m.layout.strip(b.String(), m.height)
}

func (m Model) header() string {
	role := "no role"
	if m.cfg != nil && m.cfg.Role != "" {
		role = string(m.cfg.Role)
		if m.cfg.Role == config.RoleClient {
			role = "client of " + m.cfg.Engine.Host
		}
	}
	h := titleStyle.Render("engineroom") + "  " + dimStyle.Render(role)
	if m.cfg != nil && m.cfg.Role == config.RoleClient && m.cfg.Offline {
		// Offline is never silent either: marked on every screen while it is on (CONN-OFFLINE-001).
		return h + "  " + localBadge.Render("OFFLINE") + dimStyle.Render(" working on this machine as the engine, until you go online (f)")
	}
	if m.local {
		// Working locally is never silent (CONN-LOCAL-002): the marker is on every screen.
		h += "  " + localBadge.Render("LOCAL") + dimStyle.Render(" working on this machine, not the engine")
	}
	return h
}

func (m Model) noRoleView() string {
	return strings.Join([]string{
		warnStyle.Render("This machine has no role yet."),
		"",
		"Every action runs either here or on the engine, so the program needs to know which this",
		"machine is before it can do anything. Set it with one of:",
		"",
		"  " + commandStyle.Render("engineroom role set engine") + dimStyle.Render("                             this machine is the engine"),
		"  " + commandStyle.Render("engineroom role set client --engine <ssh destination>") + dimStyle.Render("  this machine works on an engine"),
		"",
		dimStyle.Render(fmt.Sprintf("The setting is kept in %s", paths.ConfigFile())),
		"",
		dimStyle.Render(keyStyle.Render("q") + " quit"),
	}, "\n") + "\n"
}

func (m Model) engineDownView() string {
	lines := []string{
		errStyle.Render("The engine " + m.cfg.Engine.Host + " did not answer."),
		dimStyle.Render(m.message),
		"",
		"Nothing has been started on either machine. What now?",
		"",
		"  " + keyStyle.Render("r") + "  retry the engine",
		"  " + keyStyle.Render("l") + "  work locally, on this machine's own copy",
		"  " + keyStyle.Render("f") + "  switch offline: work on this machine until you go online again",
		"  " + keyStyle.Render("s") + "  stop",
	}
	if m.message != "" && strings.HasPrefix(m.message, "This machine has no copy") {
		lines[1] = ""
		lines = append(lines, "", warnStyle.Render(m.message))
	}
	return strings.Join(lines, "\n") + "\n"
}

// ClaudeMark is drawn beside a session in which Claude is waiting for the developer: the mark
// Claude Code itself shows, so it reads as "Claude wants you" at a glance.
const ClaudeMark = "✻"

func (m Model) listView() string {
	var b strings.Builder
	title := "Sessions on " + on(m.src.Where())
	if m.intent.Switch {
		title = "Move this window to which session?"
	}
	b.WriteString(titleStyle.Render(title) + "\n\n")
	if !m.loaded {
		b.WriteString(dimStyle.Render("Reading sessions ...") + "\n")
		return b.String()
	}
	if len(m.sessions) == 0 {
		b.WriteString("There are no sessions.\n")
	}
	for i, s := range m.sessions {
		if s.Here && (i == 0 || !m.sessions[i-1].Here) {
			// This machine's own sessions follow the engine's under a heading of their own, so
			// which machine each is on is never in doubt (CONN-HANDOFF-006).
			b.WriteString("\n" + headingStyle.Render("Still running on this machine, started while it was offline") + "\n")
		}
		cursor := "  "
		if i == m.cursor {
			cursor = keyStyle.Render("> ")
		}
		state := okStyle.Render("running")
		// Claude's mark beside the name says Claude is waiting for the developer; the session
		// keeps its place in the list (ops.Order, D-022), so the mark is how it is found.
		claude := "  "
		switch {
		case s.Ended:
			state = dimStyle.Render("ended")
		case s.Waiting == signal.Waiting:
			state = warnStyle.Render("waiting")
			claude = warnStyle.Render(ClaudeMark) + " "
		case s.Waiting == signal.Unknown:
			state = dimStyle.Render("running, waiting unknown")
		}
		project, checkout := s.Project, s.Checkout
		if project == "" {
			project = "-"
		}
		if checkout == "" {
			checkout = "-"
		}
		b.WriteString(fmt.Sprintf("%s%s%s%-28s %-10s %-16s %s\n", mark(hitSession, i), cursor, claude, s.Name, state, project, dimStyle.Render(checkout)))
		if s.Waiting == signal.Waiting && s.Question != "" {
			b.WriteString("    " + warnStyle.Render("asks: ") + s.Question + "\n")
		}
	}
	b.WriteString("\n")
	// Two lines, so the hints fit a narrow screen (a 13-inch laptop's terminal) without being cut:
	// what acts on the selected session, then everything else.
	keys := keyStyle.Render("enter") + " join   " + keyStyle.Render("p") + " show commands   " + keyStyle.Render("e") + " VS Code   " +
		keyStyle.Render("u") + "/" + keyStyle.Render("d") + " stack up/down   " + keyStyle.Render("x") + " stop   " + keyStyle.Render("c") + " clean up\n" +
		keyStyle.Render("o") + " open a project   " + keyStyle.Render("h") + " history   " + keyStyle.Render("r") + " refresh   " + m.offlineHint() + keyStyle.Render("q") + " quit"
	if m.intent.Switch {
		keys = keyStyle.Render("enter") + " switch   " + keyStyle.Render("p") + " show commands   " + keyStyle.Render("q") + " quit"
	}
	b.WriteString(dimStyle.Render(keys) + "\n")
	if m.readCmd != "" {
		b.WriteString(dimStyle.Render("Read with: "+m.readCmd) + "\n")
	}
	return b.String()
}

func (m Model) projectsView() string {
	var b strings.Builder
	if m.projectsNote != "" {
		b.WriteString(m.projectsNote + "\n\n")
	}
	if len(m.projects) == 0 {
		b.WriteString("No projects are registered.\n")
	} else {
		b.WriteString(titleStyle.Render("Registered projects") + "\n")
		b.WriteString(m.remoteControlLine() + "\n\n")
		// Each category is a heading with its projects indented under it; the projects with no
		// category follow the last heading, set apart by a blank line (PROJ-CAT-001). The cursor
		// moves over projects only, so a heading is never selected.
		grouped := false
		for i, p := range m.projects {
			if i == 0 || p.Group != m.projects[i-1].Group {
				if p.Group != "" {
					b.WriteString(headingStyle.Render(p.Group) + "\n")
					grouped = true
				} else if grouped {
					b.WriteString("\n")
				}
			}
			cursor := "  "
			if i == m.cursor {
				cursor = keyStyle.Render("> ")
			}
			indent := ""
			if p.Group != "" {
				indent = "  "
			}
			b.WriteString(mark(hitProject, i) + cursor + indent + p.Name + "\n")
		}
	}
	// Two lines, so the hints fit a narrow terminal without being cut.
	b.WriteString("\n" + dimStyle.Render(keyStyle.Render("enter")+" open and join   "+keyStyle.Render("s")+" start, stay here   "+keyStyle.Render("p")+" show commands   "+keyStyle.Render("n")+" new work   "+keyStyle.Render("m")+" Remote Control on/off\n"+
		keyStyle.Render("g")+" from GitHub   "+keyStyle.Render("esc")+" sessions   "+keyStyle.Render("h")+" history   "+m.offlineHint()+keyStyle.Render("q")+" quit") + "\n")
	return b.String()
}

func (m Model) previewView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.plan.Title) + dimStyle.Render("  on "+on(m.plan.Machine)) + "\n\n")
	b.WriteString(m.notesView())
	if len(m.plan.Commands) == 0 {
		nothing := "Nothing to run."
		if m.plan.Blocked {
			nothing = "Nothing to run: clear what is listed just above, then try again."
		}
		b.WriteString(nothing + "\n\n" + dimStyle.Render(keyStyle.Render("esc")+" back") + "\n")
		return b.String()
	}
	b.WriteString("These commands will run, in this order. Nothing has run yet.\n\n")
	for i, c := range m.plan.Commands {
		b.WriteString(wrapped(m.cmdGutter(i)+fmt.Sprintf("%d. ", i+1), c.Display(), m.cols()))
		if c.Why != "" {
			b.WriteString("     " + dimStyle.Render(c.Why) + "\n")
		}
	}
	b.WriteString("\n" + m.copyLine())
	b.WriteString(dimStyle.Render(keyStyle.Render("enter")+" run them   "+keyStyle.Render("esc")+" back") + "\n")
	return b.String()
}

// notesView is what a plan says it does not do, and why, wrapped to the screen, before its
// commands (CONN-HANDOFF-003).
func (m Model) notesView() string {
	if len(m.plan.Notes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, n := range m.plan.Notes {
		b.WriteString(lipgloss.NewStyle().Width(m.cols()-3).Render(n) + "\n")
	}
	return b.String() + "\n"
}

func (m Model) runView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.plan.Title) + dimStyle.Render("  on "+on(m.plan.Machine)) + "\n\n")
	b.WriteString(m.notesView())
	for i, c := range m.plan.Commands {
		s := m.steps[i]
		label := "waiting"
		style := dimStyle
		switch s.Status {
		case action.StatusRunning:
			label, style = "running", warnStyle
		case action.StatusOK:
			label, style = "ok, done", okStyle
		case action.StatusFailed:
			label, style = fmt.Sprintf("FAILED (exit %d)", s.Exit), errStyle
		case action.StatusNotRun:
			label = "not run"
		case action.StatusInterrupted:
			label, style = "interrupted", errStyle
		case action.StatusHandedOver:
			label, style = "joining", okStyle
		}
		// The outcome comes first so it is never pushed off a narrow screen by a long command.
		b.WriteString(wrapped(m.cmdGutter(i)+style.Render(fmt.Sprintf("%-17s", label))+" ", c.Display(), m.cols()))
		if s.Output != "" && (s.Status == action.StatusFailed || s.Status == action.StatusRunning || (s.Status == action.StatusOK && c.Why != "" && strings.HasPrefix(c.Why, "the repository's own"))) {
			style := dimStyle
			if s.Status == action.StatusFailed {
				style = errStyle
			}
			for _, line := range lastLines(s.Output, 8) {
				b.WriteString(wrapped("                    ", style.Render(line), m.cols()))
			}
		}
		if s.Status == action.StatusFailed && c.OnFail != "" {
			b.WriteString(wrapped("                    ", warnStyle.Render(c.OnFail), m.cols()))
		}
	}
	if m.histNote != "" {
		b.WriteString("\n" + warnStyle.Render(m.histNote) + "\n")
	}
	if !m.running {
		b.WriteString("\n")
		if m.runOK {
			b.WriteString(okStyle.Render("Done.") + "\n")
		} else {
			b.WriteString(errStyle.Render("Stopped at the failed command; the commands after it were not run.") + "\n")
		}
		b.WriteString(m.copyLine())
		b.WriteString(dimStyle.Render(keyStyle.Render("enter")+" back   "+keyStyle.Render("q")+" quit") + "\n")
	}
	return b.String()
}

// on names a machine after "on": "the engine", "the engine studio", or "this machine".
func on(where string) string {
	if strings.HasPrefix(where, "this ") {
		return where
	}
	return "the " + where
}

// cols is the screen width to wrap to, with a sensible width before the terminal reports one.
func (m Model) cols() int {
	if m.width > 20 {
		return m.width
	}
	return 100
}

// wrapped writes prefix then text, breaking text across lines at the screen width and
// indenting the continuation under the text, so a long command is shown whole rather than cut.
func wrapped(prefix, text string, width int) string {
	// A click marker at the start of the prefix is drawn on every row of the command, so a click
	// on any of them is a click on the command; it takes no width on screen.
	marker := ""
	if strings.HasPrefix(prefix, string(markerStart)) {
		end := strings.IndexRune(prefix, markerEnd) + len(string(markerEnd))
		marker, prefix = prefix[:end], prefix[end:]
	}
	indent := lipgloss.Width(prefix)
	room := width - indent - 1
	if room < 20 {
		room = 20
	}
	var b strings.Builder
	runes := []rune(text)
	first := true
	for len(runes) > 0 || first {
		n := min(room, len(runes))
		b.WriteString(marker)
		if first {
			b.WriteString(prefix)
			first = false
		} else {
			b.WriteString(strings.Repeat(" ", indent))
		}
		b.WriteString(commandStyle.Render(string(runes[:n])))
		b.WriteString("\n")
		runes = runes[n:]
	}
	return b.String()
}

func lastLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// newWorkKey reads the name for new work a key at a time. The name is checked by the plan, so
// one rule (and one message) covers the interface and the command line.
func (m Model) newWorkKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.screen = screenProjects
		return m, nil
	case tea.KeyEnter:
		if m.newName == "" {
			return m, nil
		}
		src, project, work, start := m.src, m.newFor, m.newName, m.startFor(false)
		m.stayAfter = false
		return m, m.makePlan(func(ctx context.Context) (action.Plan, error) { return src.NewWork(ctx, project, work, start) }, false)
	case tea.KeyBackspace:
		if r := []rune(m.newName); len(r) > 0 {
			m.newName = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.newName += " "
	case tea.KeyRunes:
		m.newName += string(k.Runes)
	}
	return m, nil
}
