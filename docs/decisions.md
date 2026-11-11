# Decisions

The design decisions behind engineroom. Each one says the problem it solves, what was decided,
and what else was considered and why it lost, so "was this already decided?" has one place to
look.

Every entry describes the program as it is now. When a decision changes, its entry is rewritten
to say what holds today. An id never changes and is never reused, because code, tests and the
acceptance criteria point at decisions by id (D-029 is not used). A new decision takes the next
number.

Ids such as `WORK-STACK-005` name the acceptance scenarios, in `acceptance/`, that prove a
decision.

## D-001 A repository of its own, with nothing personal in it

**Problem.** A tool that runs one person's machines collects that person's machine names,
addresses, accounts and project names unless something keeps them out, and a git history cannot
be taken back.

**Decision.**

- engineroom is a repository of its own, open source, separate from any one person's
  configuration.
- Nothing from a real setup is ever committed: no machine name, address, user name, home path,
  account, project name or port number. A real setup lives in its owner's configuration, outside
  this repository (D-013, D-014).
- Tests, examples and docs use invented names (`project-a`, `engine.example`).
- The rule covers issues, pull requests, labels and commit messages as much as files.

**Considered.** Keeping the tool inside a personal configuration repository: it mixes a reusable
tool with one person's rules, and could never be shared.

## D-002 Go: one binary with a terminal interface and a scriptable command line

**Problem.** The work happens in terminals and in Zellij, on the engine and over SSH from a
client. The tool has to run in all of those places and be easy to drive from scripts and from
other programs.

**Decision.**

- One Go binary: a terminal interface (`engineroom`), plain subcommands, and `--json` output.
- The same binary runs on every machine. A setting on each machine says whether it is the engine
  or a client (`engineroom role set engine`, `engineroom role set client --engine <destination>`).
- The menu bar app (D-008) is a face over this binary, never a second implementation.

**Considered.**

- A native macOS app as the core: it cannot run over SSH or inside a Zellij pane.
- A shell script with a fuzzy finder: fine for a list, awkward for multi-step flows, live
  progress and two roles.

## D-003 Journeys drive the real binary in a real terminal, against real dependencies

**Problem.** The program is a terminal interface and a command line, so a browser test runner
has nothing to drive. The acceptance criteria still need executable journeys bound to their own
clauses.

**Decision.**

- Unit tests (`go test`) cover pure logic: the project registry, port clash checks, branch name
  rules and each action's command list.
- Journeys run the built binary in a pseudo-terminal at a fixed size, type keys as a person
  would, and read the screen through a terminal emulator. They wait on visible text, never on a
  fixed sleep. The text a person reads is the selector.
- Clauses come from the acceptance documents and are never retyped: a generator emits one Go
  struct per scenario with one field per clause. A reworded or invented clause fails to compile;
  a skipped clause fails the gate.
- Dependencies are real and isolated: Zellij with its own socket directory per run, git in
  throwaway repositories, SSH to a throwaway engine in a container. An unreachable engine is a
  real address that does not answer. What only macOS does is checked on a Mac.
- What a screen cannot show is checked through the system itself (`git worktree list`,
  `zellij list-sessions`).
- A journey can be recorded as a terminal cast, captioned with each clause, and rendered to
  video. Recording and pass or fail are one code path.
- Every journey is seen failing at least once.
- `scripts/verify.sh` is the gate: format, lint, build, unit tests, generated scenario bindings
  and the website. Journeys run as a separate command, `scripts/journeys.sh`.

**Considered.**

- Testing the screen model inside the process only: it never runs the binary, the terminal or
  the commands the program issues. It stays available for unit-level screen tests.
- Asserting on the printed command list alone: it proves what the program meant to do, not what
  happened.

## D-004 Work is tracked in GitHub issues, written for any reader

**Problem.** Each piece of work needs a record that says what is wanted and which acceptance
scenarios cover it, somewhere a contributor would look.

**Decision.** Work is tracked as GitHub issues in this repository, written as for an outside
contributor: clear, self-contained, following common open source conventions (issue and pull
request templates, a contributing guide, a security policy, a licence).

**Considered.** A document in the repository: issues are where open source projects take work
from, and they link to pull requests.

## D-005 Port numbers stay the same; a clash is reported, never renumbered

**Problem.** Several projects, and several worktrees of one project, run dev servers and test
stacks on one machine at the same time. Two stacks on one port do not fail loudly: the second
one is reached by mistake. The numbers also have to stay the same from day to day, because
people, bookmarks and registered sign-in callbacks use them.

**Decision.**

- A project's ports are written down in its settings (D-013) and are the same on every machine
  and every day.
- The program reports a clash between projects, or with a process already listening, and names
  both sides (`engineroom ports check`). Fixing it is an edit to the settings. The program never
  renumbers anything.
- Slots are opt in. A project that wants several worktrees running at once declares its slots
  and every port each slot binds. A project without slots declares its base ports only.
- A slot belongs to a worktree folder, never to a branch. Claiming one is safe when two claims
  race, and the slot is given back when git no longer lists the worktree.
- A slot's database is handed over by name only. No password is ever written into a worktree:
  the full connection string is assembled where the password already lives.
- Each claimed slot gets one standard git-ignored record (JSON, and the same values as an
  environment file). A project can also name templates, which are rendered into git-ignored
  files for that slot, so a project in any language gets its settings in its own format.

**Considered.**

- The program assigning numbers as it goes: they would differ between machines and over time.
- Slots for every project: most projects run one stack and need nothing beyond their base ports.

## D-006 What a project needs is set up by its own scripts, which the program runs

**Problem.** What a new worktree needs differs from project to project: one has a database and a
storage emulator per slot, another only needs its dependencies installed. A tool that knew each
kind of resource could never be general, and every new stack would need a change to the tool.

**Decision.**

- A project that wants more than a plain worktree and session has five optional scripts:
  - `create` runs once, after the worktree exists and its slot is claimed, and makes what is
    unique to that worktree, such as a database or storage;
  - `up` runs on request and starts everything the worktree needs, returning once it is running;
  - `status` runs when the health view asks, and says whether what `up` started is running
    (D-037);
  - `down` runs on request, and before teardown, and stops what `up` started;
  - `teardown` runs before the worktree is removed and gives back what `create` made.
- The stack is started on request (`engineroom up`, the app's Run button, or the agent inside a
  session), never on every session open: a session is often opened only to talk to the agent.
  A project with no `up` is told what one must do; the program never guesses how to start a
  stack.
- A script runs in the worktree with `ENGINEROOM_*` variables for the worktree, the slot and
  each named port (D-028), and can call the command line to read slot details.
- Scripts must be safe to run again. That is the script's job, and the guide, the run-stack
  skill and every message about a missing script say so.
- A failure is reported with the step that failed. A half-made worktree is never left
  unannounced.
- A repository's script is read from the checkout, so a branch that changes it runs its own
  version. New work starts from the remote's main, so the create script it runs is main's.
- A script's output streams into the view of the action, and the history keeps the script's
  content as it was when it ran.
- The first run of a script, and the first run after it changes, shows it and asks the developer
  to trust it, because running it runs the project's code.
- Where the scripts live is D-013.

**Considered.**

- The program knowing each kind of resource (emulators, database servers): never general, and
  the list grows with every stack.
- A Claude Code skill doing the setup: it needs a model and is not guaranteed to run the same
  way twice. A skill can call the same scripts, so the two never diverge.

## D-007 The editor runs where the developer is

**Problem.** A session's worktree should open in VS Code from whichever machine the developer is
at. On a client the files live on the engine, and reading and debugging them should use the
engine's files and toolchain.

**Decision.**

- The editor's command runs on this machine and is never wrapped in ssh. On the engine, or
  working local, it is `code <folder>`. On a client it is
  `code --remote ssh-remote+<host> <folder>`: the engine's folder through VS Code's Remote - SSH.
- The engine reports each session's folder in `engineroom sessions --json`, so the command is
  built from the list already read, with no second request.
- The command is `[editor] command`, `code` by default. Another editor must accept the same
  `--remote ssh-remote+<host> <folder>` arguments from a client.
- The remote is `engine.host` exactly as configured. `engine.ssh_config` is not passed: VS Code
  resolves the host through its own SSH configuration, so the host must resolve there too.
- Refused before anything runs: a session outside every registered project, an engine that did
  not report a folder, and an editor that is not installed, which is answered with how to
  install it.

**Considered.**

- Running the editor on the engine: its screen is not where the developer is.
- Mounting or syncing the engine's folder to the client: copies drift, and the client's
  toolchain is not the engine's.

## D-008 A native menu bar app, as a face over `engineroom api`

**Problem.** A terminal interface has to be started in a terminal each time. The developer works
from two Macs and wants the tool always within reach, doing the right thing on each. A window
inside a Zellij session cannot know which Mac is looking at it, because its panes run on the
engine.

**Decision.**

- A SwiftUI menu bar app that computes nothing itself: every list and every action is an
  `engineroom api` command that prints JSON. An action is planned, shown and run by the same
  code the command line uses, and lands in the same history.
- The app acts where this machine's actions are done: on the engine over SSH for a client that
  is online, on this machine otherwise.
- Claude Code always starts inside Zellij, so anything started can be joined from any machine.
  The app can start a session to join it in a terminal, with Remote Control and no window, or
  both.
- The terminal is chosen from those installed (Ghostty by default).
- It is built from `macos/` with SwiftPM and installed by `scripts/install.sh --app` into
  `~/Applications`, signed with the Mac's own development identity or ad hoc.
- A phone is out of scope: it reaches sessions through the Claude app and Remote Control.

**Considered.**

- A window inside Zellij: it always runs on the engine, so it cannot open the editor or a
  terminal on the Mac the developer is at.
- A local web page: a server to run and secure.
- The app computing plans itself: two implementations drift, and the history would miss what the
  app did.

## D-009 Work moves between machines through GitHub, shown first and run on request

**Problem.** A client that works local works in its own worktrees. Committed work on one machine
is invisible to the other until it is pushed and brought in.

**Decision.**

- Switching a client to work local offers the handover from the engine, and switching back
  offers the handover to it. `engineroom handoff to-engine|from-engine` shows either at any
  time. Each is a plan in the history like any other action.
- Reading changes nothing: remote URLs, `ls-remote`, `for-each-ref`, `rev-list`, the worktree
  list and status, never a fetch. The engine's reading is `engineroom handoff state --json`.
- Every push runs before any worktree moves, so a refused push leaves both machines as they
  were.
- Projects are matched by name and then by their GitHub owner and name, since each machine may
  reach GitHub by its own address.
- Back online, the client's own sessions that still run are listed after the engine's, marked as
  on this machine, and acted on there (`--here`).
- Claude's conversations and the two machines' histories are not carried across.
- What a handover carries, and when it refuses, is D-017.

**Considered.**

- Syncing by itself on the switch: it would change worktrees and GitHub unasked.
- Copying worktrees or conversations between machines directly: copies drift, and conversations
  are stored under each machine's own paths.

## D-010 Lists of repositories and projects are in name order, ignoring case

**Problem.** GitHub lists repositories by recent activity, which changes from one look to the
next, and projects were listed in the order they were registered. Neither lets a name be found
where it is expected.

**Decision.**

- One order everywhere a list of repositories or projects is shown (`roots.NameLess`): names
  compared in lower case, with names that differ only in case kept in a fixed order, so a list
  never shuffles between two reads.
- A client sorts what the engine sends as well, and the app sorts inside each category.
- Category headings follow the same rule, with "no category" last.

**Considered.**

- Registration order: an order only the developer's own history explains.
- Minding case: capitalised names would be listed first, away from their neighbours.

## D-011 The app runs engineroom with the PATH of the developer's login shell

**Problem.** macOS starts an app with a bare PATH. Commands the developer has in a terminal (the
editor's `code`, `zellij`, `gh`) are not found by a program the app runs.

**Decision.**

- At start the app runs the login shell once (`$SHELL -ilc`), reads PATH between markers so
  anything a profile prints is ignored, and gives up after 3 seconds. It then adds
  `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin` and the folders in `/etc/paths` where
  they are missing. With no answer from the shell, its own PATH and those folders are used.
- Every `engineroom` the app runs gets that environment.
- A run whose plan cannot be made is still answered as a run that ended, carrying the reason, so
  the app says why nothing ran.

**Considered.**

- A fixed list of folders only: it misses whatever the developer's profile adds.
- A PATH setting in the program's configuration: one more thing to keep in step with the shell
  by hand.

## D-012 Ghostty holds every joined session as a tab of one window

**Problem.** Starting a copy of the terminal for every join means one application per session,
and a panel that stays open over the joined window is in the way.

**Decision.**

- Joining in Ghostty runs an AppleScript through `osascript`, shown with the plan like any
  command. Each tab is titled `engineroom: <session>`, which is how the window and the tab are
  found again. Joining a session whose tab is open selects that tab; otherwise a new tab goes
  into that window, or a new window when there is none.
- The tab runs the `engineroom join ...` line with the program's PATH passed in, because a tab
  starts with the running Ghostty's environment.
- The first join asks macOS whether engineroom may control Ghostty; a refusal is answered with
  where to allow it. Ghostty's `macos-applescript` setting must stay on.
- Other terminals get a new window for each join.
- The panel has a solid background, comes to the front when it opens, and closes when an action
  opens another application's window (Join, VS Code, a start that joins). A failure stays on the
  action's page for the next time the panel opens.

**Considered.**

- A window per session: one window with every session is easier to use.
- A copy of Ghostty per join: one application per session.

## D-013 The developer's settings decide; ports are reserved per project and lent to worktrees as slots

**Problem.** Ports and scripts kept only in a project's own repository cannot work for a
repository the developer works in but does not own, and a list of ports kept by hand cannot
check itself. Several worktrees of one project each need their own ports and data, and a client
needs the ports of the engine's sessions whether or not a terminal is joined.

**Decision.**

- **Settings.** One folder outside every repository (`engineroom settings where <project>`),
  keyed by the project's GitHub owner and name, holds the project's `engineroom.toml` and its
  scripts. A repository's own `.engineroom/` folder holds the same files as recommendations: the
  developer's settings win, section by section and script by script.
- **Reservations.** Each project reserves a range of ports. A clash between projects, or a slot
  port outside its own reservation, is reported with both sides named. New blocks come from
  20000-29999: 100 ports per project, ten slots of ten (`engineroom ports reserve`). Numbers a
  project already uses are kept and reserved as they are.
- **Slots.** Slot 1 is the main clone, with the numbers it always had. Claims are kept in the
  clone's shared `.git` folder (`engineroom-slots.json`). New work claims a slot as a shown
  command; clean up gives it back. The slot's record (`.engineroom-slot.json`), the same values
  as an environment file (`.engineroom-slot.env`) and each template the settings name are
  written into the worktree. Names only, never a password (D-005).
- **Running.** Starting the stack is a full build and run of one worktree on its slot's ports.
  Only a running session's ports are forwarded, so Run on a checkout whose session is not
  running starts the session first, in the background. Stopping a stack never starts a session
  (WORK-STACK-005).
- **Forwarding.** On a client, `engineroom forward` keeps one SSH connection to the engine and
  adds or cancels a forward on it for each port of each chosen running session, with the same
  number on both sides. It is chosen per session (`engineroom forward add|remove`, the app's
  Forward switch, or opening one of a session's links), with forwarding every session still
  available. The forwarder follows the engine's session list, which the engine sends only on a
  change, over that same connection. With nothing to forward the connection is closed. A port
  the client already uses is named with what holds it and left alone. Each change is a command
  in the history.

**Considered.**

- Only a file committed to the repository: it cannot be added to a repository the developer does
  not own.
- Forwarding only while a terminal is joined: a session is often used only from the Claude app.
- Checking the engine's sessions for forwarding every few seconds: work on a timer that nobody
  is watching.

## D-014 Settings are a private repository; secrets in it are encrypted by git-crypt

**Problem.** Settings should be the same on every machine. Some of what a project needs to run
is secret (a local settings file with keys, an environment file), and a hosted repository is not
a place for those in the clear, even a private one.

**Decision.**

- `engineroom settings init [<repository>]` makes the settings folder a git repository, sets up
  git-crypt in it, writes a `.gitattributes` that encrypts every file in any `secrets/` folder,
  commits, and pushes to the given private repository. Run again, it does only what is missing.
- On a machine that holds the key, secrets are plain files, read and edited like any other. In
  the repository, on GitHub and on a machine without the key they are encrypted bytes.
- The developer keeps the key where they keep secrets (`git-crypt export-key`).
  `engineroom settings clone <repository> <key file>` brings the settings to another machine and
  unlocks them.
- `engineroom settings sync` commits this machine's changes, takes the other machines' and sends
  this one's, as a plan of git commands in the history. It refuses, before anything runs, when a
  file in a secrets folder would be committed unencrypted.
- The app syncs on Sync now, and at most once an hour when its panel is opened. It never syncs
  on every change or while the panel is closed.
- `engineroom settings status` says where the settings are, where they sync to, what is not yet
  sent, and whether the secrets are readable here.
- A project names its secret files in `engineroom.toml` (`secrets = [...]`): paths in the
  worktree, or `~/...` for one in the home folder that every checkout shares.
  `engineroom secrets save <project>` copies them from a checkout into the settings;
  `engineroom secrets restore` puts them into a checkout that lacks them and never overwrites
  one it has, naming a file that differs instead; `engineroom secrets status` compares the two.
  New work restores them after claiming its slot and before its create script.

**Considered.** A secrets service or a password manager's command line: a dependency on one
vendor, where git-crypt works for anyone with git.

## D-015 A session is running or it is not there

**Problem.** Zellij keeps a snapshot of a session that was quit or killed and can rebuild it:
the old tabs, panes and layout. Bringing that back restores a layout that may since have
changed, and Claude Code keeps its own conversation, so the snapshot holds nothing the work
needs.

**Decision.**

- Stop ends a session fully (`engineroom session end`): it kills the session, deletes what
  Zellij keeps of it, and returns once Zellij no longer lists it.
- Joining or starting a session that ended some other way (quit from inside Zellij, or the
  engine restarted) deletes the snapshot and starts it fresh from the current standard layout in
  its checkout. The agent runs `claude --continue`, so the conversation carries on.
- Restart is stop then start in one, for a running session on an old layout.
- The app lists running sessions only. The terminal interface marks an ended one as ended, and
  Enter on it starts it fresh.
- Closing the terminal window leaves a session running; Join returns to it.

**Considered.** Bringing an ended session back as it was: it restores a stale layout and nothing
of value.

## D-016 Each project has a page; links come from settings; a client says why a port is not forwarded

**Problem.** A list of repositories with a Start button shows nothing of a project's worktrees,
ports, scripts or secrets, or of the addresses its running stack serves, and an address cannot
be copied out of a terminal reached over SSH. Forwarding in the background has nowhere to say
why a port was not forwarded.

**Decision.**

- `engineroom api project <name>` is one project's page, read where its checkouts are: every
  checkout with its branch, session state, uncommitted changes, slot, ports and links, and the
  project's scripts with their source, its reservation and its secret files. Every action on the
  page is an existing action.
- The app lists repositories as Pinned, Registered by category, Here and not registered, and On
  GitHub and not here. A registered project opens its page.
- A project's settings name the links its stack serves: `[links]` templates over the slot's
  values. Without them each port is shown as `http://localhost:<port>`.
- A project can be pinned and given a category (`engineroom project set`), and unregistered,
  from its page.
- The forwarder records each pass (`engineroom forward status`, `--json`): what the engine had
  running, what was forwarded, and why a port was not.

**Considered.** Leaving links to the terminal's output: they cannot be clicked or copied from a
session reached over SSH.

## D-017 A handover leaves the receiving machine in the giving machine's state, all or nothing

**Problem.** The last machine in use has the work. Moving to the other machine should carry all
of it, including work not yet committed and projects the other machine has never cloned, without
publishing anything unintended and without leaving the two machines in different states
unnoticed.

**Decision.**

- **What is pushed.** Only the branch each checkout of a registered project has out (its main
  clone and its worktrees), where it has commits GitHub lacks. Any other branch with unpushed
  commits is named, with the command to push it by hand. A release branch (`release/...` or
  `release-...`) is never pushed by a handover, even when checked out.
- **Uncommitted work travels too.** Changed files, and new files git does not ignore, go as a
  stash sent straight between the two machines' repositories over SSH, under
  `refs/engineroom/handover/<branch>`, and are applied in the receiving checkout of the same
  branch. The giving machine keeps a copy in its stash list. Ignored files do not travel:
  anything that must be on both machines belongs in the settings' secrets (D-014).
- **All or nothing.** Planning first finds everything that would stop the handover: a branch
  moved on both sides, a release branch with commits GitHub lacks, a receiving branch with
  commits of its own, a detached commit, a branch checked out elsewhere on the receiving
  machine, two repositories under one name, or no place for a missing project. With any of
  those it plans no commands and no switch. Otherwise its first commands check each machine's
  checkouts against a fingerprint of their state as read, and dry-run every push.
- **The receiving side loses nothing.** Each receiving checkout sets its own uncommitted work
  aside in its stash list, labelled with its branch, and is then put on the giving branch at the
  giving commit. A missing worktree is made.
- **The machine's own files stay.** Finder's `.DS_Store` files and the `.claude` folder are
  never carried or set aside.
- **Newer work is never moved back.** A giving branch that is behind GitHub, where the receiving
  checkout already has the newer commits, blocks the handover.
- **Missing projects are mirrored.** A project the receiving machine lacks is cloned to the same
  place under its home folder and registered under the same name and category, with the giver's
  worktrees, a slot each when the project has ports, and its secret files from the settings. A
  project outside the home folder is named, not copied. A client that can reach the engine keeps
  itself ready (`engineroom mirror`) when the app's panel is opened, at most once an hour: it
  mirrors what it lacks and fetches the rest, pushes nothing and moves no worktree.
- **Release.** On the engine, `engineroom handoff release` pushes the checked-out work and names
  what is uncommitted, before a client works local. Committing that as partial work is offered
  as its own action (`commit-partial`).
- A machine out of reach still lets the switch happen with what can be done (CONN-HANDOFF-005),
  since working local without the engine is the reason the switch exists.
- The app calls the client's switch "Work local" and its way back "Hand back to the engine".

**Considered.**

- Pushing every branch the machine has: it publishes backups and experiments, and a pushed
  release branch can start a deployment.
- Letting the developer pick projects at each switch: riskier in a hurry, when something is
  forgotten.
- A stash pushed through GitHub: it publishes work in progress.
- Committing partial work onto the branch during a handover: it fills the branch's history with
  commits nobody meant to make.
- Copying ignored files as well: large and generated files have no place in a handover.
- Carrying what can land and reporting the rest: the two machines end up in different states
  without the developer noticing.
- Refusing a receiving checkout that has changes of its own: setting them aside, labelled, loses
  nothing and reaches the state a handover means.

## D-018 Ask Claude opens a session that knows what was open; projects name their own tasks

**Problem.** The app's buttons cover the common actions. Changing a project's scripts, settings
or secrets, or doing something particular to one project, would otherwise mean opening a
terminal, finding the settings folder and explaining ports and paths to Claude.

**Decision.**

- Ask Claude (`engineroom api run assistant <project> <folder>`) starts a Claude session of its
  own beside the checkout's main one (`<session>-ai`), on the engine, in the checkout, with the
  settings folder added. Its system prompt is extended with how engineroom wants things done
  (its commands; scripts safe to run twice, kept in the settings folder, reading ports from the
  slot; secrets never printed; destructive steps only when asked) and with what was open: the
  project, checkout, branch, slot, ports, links, scripts, secret files and tasks.
- That guide is part of the program, so nothing has to be installed into the developer's Claude
  configuration for it.
- A project names its own tasks in `engineroom.toml` (`[[task]]` with `name`, `about` and
  `run`). The app lists them on each checkout, `engineroom task` runs one in a session's
  checkout with the slot's variables, and the assistant is told them.

**Considered.** A button per kind of change: the list never ends, where one session that already
knows the context covers them all.

## D-019 The menu bar panel holds the sessions; a window holds everything else

**Problem.** One small panel holding sessions, repositories, project pages, history and settings
makes the things done many times a day share space with the things done now and then, and the
occasional work loses its place whenever the panel closes.

**Decision.**

- The panel from the menu bar holds what is used many times a day: the projects and their
  sessions (laid out as D-022 and D-025 say). An action started from it is shown in it.
- Its gear opens a window: a sidebar of the registered projects (pinned first), the
  repositories, the history and the settings, with the chosen one on the right. An action
  started from the window is shown in the window.
- Both draw from one store, so they always agree. How that store is kept current is D-023.
- Every screen is drawn in one visual language (D-032), and `EngineRoomMenu --snapshot <dir>
  [dark]` draws them from invented sample data, so how the app looks can be checked after every
  change.

**Considered.** One panel with tabs and back links for everything: cramped, and closed by any
click outside it.

## D-020 What runs for a session lives only as long as the session

**Problem.** A stack started for a session keeps running after the session ends unless something
ties the two together. The ports stay taken, and nothing shows why.

**Decision.**

- What is started for a checkout's session belongs to that session. Ending the session fully
  ends it too. Leaving a session ends nothing: closing the tab or window, detaching, or a client
  sleeping or losing its connection leaves the session and everything it runs as they were.
- "Started for a session" means what the project's `up` started, which its `down` stops, and any
  process still running whose environment names the session: `ENGINEROOM_SESSION`, which
  engineroom sets on everything it runs for a checkout; else the session of the checkout named
  by `ENGINEROOM_WORKTREE`; else `ZELLIJ_SESSION_NAME`, which Zellij sets on everything started
  inside a session. `ENGINEROOM_SESSION` wins where they differ, so a stack started for one
  checkout from inside another session belongs to its own checkout's session.
- Of those, only servers are stopped: each process that listens on a TCP port, with the
  processes of the same session that started it and those it started. An editor or app opened
  from inside a session carries the session's name too, listens on nothing, and is the
  developer's to close. A process is never matched by its name.
- Stop and clean up run the project's `down` while the session is there, end the session, then
  stop whatever still runs in its name (`engineroom session reap <name>`, which says what it
  stopped and refuses while the session runs).
- Restart keeps the stack: the same session comes back on the current layout.
- A session that ends without engineroom (quit from inside Zellij, killed, or crashed) is found
  by the sweep. The menu bar app starts one `engineroom sweep --watch` on the machine whose
  sessions they are: on the engine from when the app starts, and on a client only while it works
  local. It sweeps every minute and ends with the app; it is never a job of its own. It reads
  that machine's Zellij sessions and processes and never the network. A checkout whose session
  has been gone for two minutes has what was started for it stopped, as Stop would, and what
  Zellij keeps of the ended session is deleted (D-015). Each is an entry in the history.
  `engineroom sweep --dry-run` shows what it would do.
- A process on a project's ports that belongs to no session is never stopped for the developer.
  The project's page lists it with its port, process id, start time and folder, and offers to
  stop that one.
- Claude in a session runs the app through `engineroom up` and `engineroom down` (the run-stack
  skill), so what it starts belongs to the session. A build handed over to try is `down` then
  `up`, so the app that is running is the build that was handed over.
- Handing work over to a client (D-017) does not end the engine's sessions, so it does not stop
  their stacks.

**Considered.**

- Matching processes by name: it reaches processes that belong to something else.
- A scheduled job on every machine: it runs all the time, including where there is nothing to
  sweep.

Proven by WORK-STACK-006 to 013.

## D-021 A join whose connection drops joins the same session again

**Problem.** A client's join is an SSH connection, which ends whenever the client sleeps or
changes network, while the session runs on untouched on the engine. A window that then ends as
though the session had ended means joining again by hand after every lid close.

**Decision.**

- A join that had been attached and then ended with ssh's own failure status (255) is a dropped
  connection. The window says the connection was lost and that the session keeps running, asks
  the engine for its sessions every 3 seconds (showing the command it asks with and why the
  engine did not answer), and joins the session again as soon as the engine answers. Each join
  again is an action in the history.
- Only the same session is joined again: one still running and older than the join that dropped.
  A session stopped or restarted while the connection was down ends the join, and nothing is
  started.
- `q` stops the waiting and joins nothing.

**Considered.**

- Keeping the connection alive through sleep (long keep-alive limits, or a mobile shell): a long
  limit keeps a dead connection open on a real fault and still loses it on a long sleep, and a
  mobile shell is another program on both machines and another port, for what Zellij already
  gives: joining again redraws the whole session.
- Rejoining from the app on wake: the window already knows what it was joined to and can wait
  for the engine itself.

Proven by CONN-REJOIN-001 to 004.

## D-022 The panel keeps one order, pinned first; waiting is a mark, not a place

**Problem.** A list that moves a session to the top whenever Claude starts waiting never has
anything where it was last seen.

**Decision.**

- One fixed order: the pinned projects by name, each always there whether running or not; then
  the other projects with a running session, by name; then this Mac's own sessions started while
  it worked local. The app lays this out itself (`PanelList`), whatever order it is sent.
- A waiting session is marked where it is, with Claude's mark (✻) and Claude's question. It is
  never moved and never listed twice.
- Each project carries a GitHub line (code, issues, pull requests), and a worktree whose branch
  is on GitHub carries that branch and the pull requests from it. The branch links are read from
  the clone, with no request to GitHub.
- The terminal's session list and `engineroom sessions` have the same order (`ops.Order`):
  pinned projects' sessions, then the rest, then Ask Claude sessions, each by name. An ended
  session keeps its place, marked ended.
- Only the app asks for the sessions' GitHub links (`engineroom sessions --json --github`).

**Considered.** A "waiting for you" section at the top: it reorders the list on every change and
lists a session twice.

Proven by APP-LIST-001 to 003, SESS-LIST-002 and SESS-LIST-005.

## D-023 The engine sends the app its sessions when they change; the app reads nothing on a timer

**Problem.** Reading the sessions on a timer costs a new SSH connection for every look on a
client, shows what was true some seconds ago, and knows nothing while the panel is closed, so
the menu bar cannot say that Claude is waiting.

**Decision.**

- `engineroom api feed` sends the sessions as one JSON line now, and again each time anything in
  it changes, and nothing in between. Where the sessions are on the machine it runs on, it looks
  every 3 seconds; each project's GitHub links are read at most every 30 seconds. On a client
  that is online it runs the engine's feed over one SSH connection and adds this machine's own
  sessions to each list.
- Any line written to the feed asks it to look now. The app does that after an action, so what
  the action changed shows at once. Closing its input ends it, and the engine's with it, so
  nothing is left looking on the engine for an app that has gone. After a quiet minute it sends
  an empty line, so a connection that died without closing is found.
- The app keeps the feed open while it runs. When the feed ends, the app starts it again two
  seconds later; when it ended at once (the engine not answering), after 5, 15, 45 seconds and
  so on, up to five minutes. Opening the panel, a wake and the refresh button try at once.
  While there is no feed, the sessions are read when the panel opens and after an action.
- The app reads nothing on a timer. Its own state (forwarding, history, settings) is read when
  the panel or window opens, after an action, on wake and on refresh.
- The menu bar icon carries Claude's mark (✻) while Claude is waiting in any session, panel open
  or not.
- Anything that could lose work checks at the moment it is asked for, never from what the feed
  last said.

**Considered.**

- Reading on a timer while the panel is open: a connection per look, stale by up to the
  interval, and blind while closed.
- A feed only while the panel is open: it keeps the panel fresh but loses the menu bar's mark,
  for a saving too small to notice.

Proven by APP-FEED-001 to 005.

## D-024 A worktree is for a branch or for a role; closing or moving on never loses work

**Problem.** A worktree named for its branch and closed when the branch is done does not fit
work that is batched in standing worktrees (a designer's, a tester's), each holding one branch
after another. And a clean up that only refuses when work would be lost offers no way through.

**Decision.**

- New work is either branch work (`new <project> <branch>`: folder `<repo>-<branch>`, closed
  when done) or a role (`new-role <project> <role> <branch>`: folder `<repo>-<role>`, on a new
  branch from the remote's main or on an existing branch).
- A role is recorded by `engineroom roles add` in `roles.json` in the project's settings folder,
  so every machine that syncs the settings knows it. `engineroom roles <project>` lists them.
  A name that is already branch work is refused as a role.
- `next-branch <project> <role> <branch>` moves a role worktree on to its next branch in the
  same folder, keeping its session name and its slot of ports.
- Clean up and moving on refuse, before anything runs, when work would be lost (uncommitted
  changes, or commits on no remote), and offer what makes it safe: save the changes to the
  branch and push (`save-work`), put them aside as a stash of the repository, which outlives the
  worktree (`put-aside`), or push the commits (`push-work`). Each is a plan of its own, shown
  before it runs. Throwing work away is never offered.
- Cleaning up a role worktree forgets the role (`engineroom roles remove`).

**Considered.**

- Worktrees for branches only: it rules out standing worktrees.
- A list of roles declared ahead in `engineroom.toml`: the kind is chosen when a worktree is
  made, and the program would have to rewrite a hand-written file to record it.

Proven by WORK-CLEAN-008 to 011 and WORK-ROLE-001 to 005.

## D-025 The panel is a list of projects and a detail of compact cards

**Problem.** A full card per session, one under another, has to be scrolled once several
projects run, and a stopped worktree is not shown at all.

**Decision.**

- The panel is 840 by 480. On the left is one row per project in the order of D-022, with one
  light for all its sessions: Claude's mark when any waits, green when one runs, grey when none
  does, and a count when more than one runs.
- Pointing at a row, or clicking it, shows that project on the right: its name (which opens its
  page), its state, its GitHub line, a compact card per checkout (main clone first, then
  worktrees by name, running or not), and its Ask Claude sessions as one line each.
- The project last chosen is remembered across opening the panel and restarting the app, and
  stays listed ("Last used") after its last session stops, so an action returns to it.
- A card shows its light; the main clone by branch, a role worktree by its role with the branch
  it holds, any other by name; and chips for unsaved files, commits not pushed, commits behind
  main and a merged branch.
- Every action is a button in sight, with no menus: Join, Restart, Stop or Start; Next branch
  for a role; Close for a worktree; VS Code, Ask Claude, the branch and its pull requests on
  GitHub, and copying the branch name as icons. Each card shows its links and its Forward switch
  whether or not its session runs.
- The feed (D-023) carries each checkout's state for the pinned projects and those with a
  running session: unsaved, unpushed, behind and merged, read from git on the engine's own disk
  at most every 30 seconds and after an action. Nothing is fetched to find them, so behind and
  merged are as of the last fetch; Close always checks again when pressed.
- "Merged" means the remote's branch is in main and is not simply one of main's own commits, so
  a new branch with nothing of its own is not called done.

**Considered.**

- A list of trimmed cards: it still grows with every session and still scrolls.
- A row per session: a project with worktrees fills the list, and the project is what the
  developer thinks in.

Proven by APP-LIST-004 and APP-FEED-006.

## D-026 One name: engineroom is the program, the engine is the machine that does the work

**Problem.** A name should say what it names, and one thing should have one name wherever it is
met: in the app, on the command line, in a file name and in an environment variable.

**Decision.**

- The program is engineroom: the repository, the Go module
  (`github.com/KyleyHarris/engineroom`), the command (`engineroom`), the app (`engineroom.app`,
  bundle id `local.engineroom.menu`) and the Swift modules (`EngineRoomKit`, `EngineRoomMenu`).
- The machine that does the work is the engine, and a machine that reaches it is a client:
  `role = "engine"`, the `[engine]` section, `--engine`, `handoff to-engine` and `from-engine`.
- The program's folders are `~/.config/engineroom` and `~/.local/state/engineroom`, under the
  XDG roots.
- A project's folder is `.engineroom/` with `engineroom.toml`, in a repository and in the
  developer's settings alike. The slot files are `.engineroom-slot.json` and
  `.engineroom-slot.env`, and the registry is `.git/engineroom-slots.json`.
- Environment variables are `ENGINEROOM_*`, named by kind (D-028).

**Considered.**

- A shorter command beside the product's name: two names for one thing, met by everyone who goes
  from the app to the command line. A developer who wants a short command can alias it.
- Naming the program after one of the tools it uses: the tools can change, and the name should
  not.

## D-027 The website is built from the docs; only the story pages are written by hand

**Problem.** A website written beside the docs holds every fact twice, and the two copies drift.

**Decision.**

- `tools/site`, a small Go program in this repository, renders `docs/setup.md` and
  `docs/usage.md` into pages, one per heading down to a level set for each doc, with the heading
  ids GitHub gives, so a link written for GitHub lands in the same place on the site. A link to
  any other file in the repository goes to that file on GitHub.
- New facts go into the docs first and the site shows them. The pages in `site/pages` carry only
  the story: the front page and how it works.
- This file is not rendered: the site is for using engineroom, and the reasons behind it are for
  people changing it, who read them here.
- The product's name, its command and the stack of tools it joins together are data in
  `site/site.toml`, never spelled in a template, a class, a file name or an address.
- Plain HTML and CSS with no JavaScript build, and fonts served by the site itself: it sends no
  request elsewhere and opens from disk. How it works has one small hand-written script.
- The look is a style over one shared stylesheet, named in `site.toml`.
  `go run ./tools/site -serve` serves the site, builds it again on every change, and offers a
  switch between styles when more than one is listed.
- The gate builds the site into a temporary folder and fails on a link, an anchor or an image
  that goes nowhere, and on an em or en dash. The built site is never committed; a workflow
  publishes it.

**Considered.**

- Every page written by hand: two copies of every fact.
- A site generator from the JavaScript ecosystem: a toolchain and a build step this repository
  does not otherwise need.

## D-028 A slot's keys are named by kind, and a key that would overwrite another is refused

**Problem.** Every key of a slot (the program's own, and a project's ports, names and derived
values) is one value in the slot's record and in a template's `{{key}}`, and is given to scripts
as an environment variable. With one prefix for everything, a project's key could take a name
the program uses for itself, and silently replace it.

**Decision.**

- The program's own variables are `ENGINEROOM_SLOT`, `ENGINEROOM_WORKTREEROOT`,
  `ENGINEROOM_ISMAINCLONE`, `ENGINEROOM_PROJECT` and `ENGINEROOM_TMP`.
- A project's are `ENGINEROOM_PORT_<KEY>`, `ENGINEROOM_NAME_<KEY>` and
  `ENGINEROOM_DERIVED_<KEY>`: the key in capitals, with anything that is not a letter or a digit
  made an underscore (`engineroomfile.Fold`). The program never names a variable of its own with
  `PORT_`, `NAME_` or `DERIVED_` after its prefix.
- The record and `{{key}}` keep the plain keys.
- When a project's settings are read (`settings.Load`), a port, name or derived value is refused
  if it folds to the same name as a built-in, or as another key of a different kind or spelling.
  That is checked after the developer's file and the repository's are merged, so a clash between
  the two is caught too. The error names the file and both keys.

**Considered.**

- A prefix per kind alone: the record and `{{key}}` still hold one value per key, so a port
  named `project` would still overwrite the project's name there.
- Refusing only a key spelled exactly as another: `db-name` and `db_name` are one variable name.

## D-030 An application the program starts is started as the developer would start it from the Dock

**Problem.** macOS hands an application the whole environment of whatever asks for it, and the
application keeps it for as long as it runs, as does everything it opens. A terminal started
from a shell, an agent's session, an SSH login or a test would keep that caller's home,
configuration folders and session variables, so every tab it opened afterwards would read the
wrong configuration or take itself for part of another session.

**Decision.**

- Opening a terminal, and reopening the menu bar app after an upgrade, run
  `/usr/bin/env -i <command>`, shown with the plan like any command. An application started that
  way has the environment macOS gives one started from the Dock, and none of the caller's.
- The command is looked up on the program's own PATH before the environment is emptied.
- What runs inside a window the program opens is given the program's PATH (D-011): to Ghostty as
  the tab's own variable, to other terminals as `/usr/bin/env PATH=... engineroom join ...`.
- The app opens VS Code with an empty environment too.
- A Zellij session is not an application. It keeps the environment of what started it, without
  the agent's own session markers (`zj.AgentSessionVars`).
- An application that is already running keeps the environment it was started with; quitting and
  opening it again is the remedy.
- The journeys put stand-ins for `open` and `osascript` on every machine they make, so no
  journey reaches the real ones, and they check that a join asks for its window with an empty
  environment.

**Considered.**

- Removing a list of known variables: any variable not on the list still reaches the
  application, and the list only grows.
- Having launchd start it (`launchctl submit`): the same environment, but a submitted job is
  kept alive and started again when it ends, and the command is deprecated.
- Asking Finder to open it: a further permission, for the same result.

## D-031 A session on a Mac is started inside the developer's desktop login

**Problem.** A Zellij session belongs to the login that started its server, and so does
everything that runs in it. A session started from a client is started over SSH, and in an SSH
login the login keychain is locked and macOS treats what runs as a background process.

**Decision.**

- On a Mac, when the program runs outside the desktop login (`launchctl managername` is not
  `Aqua`) while one exists, a session is started by two commands, which the plan's one start
  command (`engineroom session start`, D-035) runs and prints:
  `engineroom session job <name> <layout> <dir>` writes a launchd job and prints it, then
  `launchctl bootstrap gui/<uid> <job file>` runs it. Run inside the desktop login, the start is
  direct. With no one logged in at the screen it is direct too, and the session lacks what only
  the desktop login has.
- The job runs the same `zellij attach --create-background ...` as a direct start, in the
  checkout's folder, then removes itself. The start then waits for the session, and prints the
  job's log when the session does not start.
- The job gives the session the program's environment without what belongs to the login it ran
  in: the SSH connection's variables and agent socket, its temporary folder, the shell's
  bookkeeping and the agent's session markers. launchd adds the desktop login's own.
- The job file is the program's state (`~/.local/state/engineroom/jobs/`), readable only by the
  developer and replaced by the next start of the same session. Its label carries a hash of the
  state folder, so a test's jobs and the developer's never share one.
- Joining is unchanged.
- `ENGINEROOM_DESKTOP_SESSIONS=off` never uses the route, and `=on` uses it whenever a desktop
  login exists. The journeys keep it off, except SESS-NEW-006, which proves the route.
- Not covered: whether macOS's privacy permissions (Documents, Desktop, Full Disk Access) treat
  a session started this way as they treat one started from a terminal at the screen.

**Considered.**

- `launchctl asuser`: it needs root.
- `launchctl submit`: deprecated, takes no environment or folder, and keeps the job alive.
- A process of the program's own kept running in the desktop login to start sessions on request:
  a second way in to keep running and answering, where a job made for each start needs nothing
  running.
- Unlocking the keychain in the SSH login: it needs the password on every start, and fixes only
  the keychain.

## D-032 The app has one meaning per colour, one kind of button per job, and its sizes named once

**Problem.** Screens drawn alike only by convention drift: many text sizes and corner radii, the
same piece drawn twice in two files, and a colour that means six different things means nothing.

**Decision.**

- A colour says one thing wherever it is used. Orange: something needs the developer (Claude is
  waiting, work that would be lost, a step that cannot run). Green: as it should be (a session
  running, an action done, something copied). Red: it failed or cannot be reached. The accent
  colour: under way and needing nothing, and anything that is clicked like a link. Everything
  else is quiet.
- A project's light is the same on every screen: Claude's mark when any of its running sessions
  waits, a green dot when one runs.
- A button in words changes something (Join, Stop, Close). A button that is only an icon opens
  something elsewhere or copies (VS Code, Finder, GitHub, the branch name), and has a tooltip.
  One button of a card is filled: the one most often pressed.
- On a project's page a checkout that is not running shows the three ways to start it as three
  buttons: the terminal with Remote Control, the terminal only, Remote Control only. The way
  chosen in Settings is the filled one, so the usual start is still one click
  (APP-START-001 to 003).
- Text sizes, corner radii and page gutters are named by what they are for, in
  `macos/Sources/EngineRoomMenu/Style.swift`, and no screen writes a number of its own. The
  card, the state light, the chip, the banner, the empty screen and the page title are each
  drawn once there.
- Working local is turned on and off by a button in words, in the header and in Settings, and
  the header's badge says when it is on. Quit stands apart from refresh and the gear.
- Every page of the window starts with its title line and shows what it has as cards. A page
  reached from another has the link back above its title.
- What a checkout would lose is a chip under its name. Close, which only a worktree has, sits at
  the far end of its row, apart from the buttons that join or start.
- An action's commands have one way back, the link at the top, in sight before, during and after
  a run. The bar at the foot holds what goes on: Copy, then Run or Done. A failed command is
  said in one word, "failed", wherever the app shows it.

**Considered.**

- Correcting each inconsistency where it is noticed: nothing stops the next screen from
  drifting.
- The system's stock lists and forms throughout: the cards with a label column are what make a
  checkout's state and its actions readable at once, and the panel has no stock equivalent.

## D-033 The panel's list is worked from the keyboard, and what the app shows by an icon or a colour it also says in words

**Problem.** A panel opened many times a day and worked only by pointing costs a reach for the
pointer each time. A button that is only an icon, and a state shown only as a colour, say
nothing to VoiceOver.

**Decision.**

- In the panel's list, Down and Up choose the next and the previous project, in the order the
  list is drawn, and stop at each end. The choice is the one pointing makes: it is shown on the
  right and remembered (D-025).
- Return joins or starts the chosen project: its session that waits for the developer, else its
  first running session, else it starts its first checkout. That card's Join or Start carries a
  return mark, and the key sends exactly what the button sends.
- Escape closes the panel from its list. On a page reached from another, it is the link back.
- The keys are left alone when they belong to something else: held with Command, Option, Control
  or Shift; while a dialog is up; while a field is typed into; and Escape while an input method
  is part way through a character. A held arrow walks the list; a held Return asks once.
- Pointing and clicking are unchanged. After an arrow key the pointer chooses again as soon as
  it moves.
- Every button that is only an icon has a name in words, read by VoiceOver. A state light is
  read as its state, a row of the list as one thing (the project, its state, and how many
  sessions run), and the menu bar icon as the app, with "Claude is waiting for you" while it
  carries the mark.
- An action's page arrives with a short fade, and nothing fades while Reduce Motion is on.
- Which key does what, and what Return acts on, are decided in `EngineRoomKit` (`PanelKey`,
  `PanelProject.step`, `PanelProject.keyAction`) and checked by its tests.
- Not covered: keys for the window's own lists and forms, which keep the system's.

**Considered.**

- A stock list that moves its own selection: it restyles the rows, and answers keys only once it
  has the focus, which in a panel opened from the menu bar is after a click.
- A letter per action: a letter is not in sight, and a wrong letter would stop a session. Return
  carries the one action that can always be left again.
- Return always joining the first card: the panel is opened most often because Claude is
  waiting, and that session is often a worktree's.

## D-034 An engine not reached is said with its cause, and a retry is offered only where it can help

**Problem.** Every failure of ssh to the engine was said as "the engine did not answer" with a
retry offered. A changed host key was then offered as something to retry, which it never is, and
ssh missing from the client was said as the engine not answering, with no reason at all.

**Decision.**

- When ssh to the engine fails with its own failure (status 255, or ssh not run at all), the
  program reads the cause from the sentence ssh writes for it: no answer, a name that does not
  resolve, ssh not installed, a host key other than the one this machine knows, or a host key
  this machine has never accepted (`source.Unreached`).
- An engine that does not answer is not told apart from a network that is down: from the client
  they look the same, and the message says so rather than guessing.
- Retry is offered for no answer and for a name that does not resolve, where the engine may be
  found by asking again. For a changed or unknown host key and for no ssh it is not offered:
  the message says what to do (check the new key on the engine and remove the old one from the
  file ssh names; connect once by hand to accept a new engine's key; install ssh). Working
  locally, switching offline and stopping are offered for every cause.
- The program never removes or accepts a host key itself: the file it would change is ssh's,
  outside the program's own state.
- Every cause still counts as the engine not reached for the program's own reads, so an answer
  is never read from a connection that failed. The menu bar app is told `engineDown` only where
  a retry can help, and `hostKeyChanged` for a changed key; for the other causes it shows the
  message as a problem, with no retry.
- A command sent to the engine that ends with ssh's own failure says that the connection was
  lost or never made, so it may have run in part there; the commands before it ran and those
  after it did not.

**Considered.**

- One message for every failure with ssh's words below it: the headline and the retry would
  still say the wrong thing for a host key.
- Removing the old host key on request: it changes ssh's file, and trusting a new key is a check
  only the developer can make on the engine itself.

## D-035 A session is started once, under a lock for its name

**Problem.** Two starts of one session at the same moment (two machines opening one project, or
two windows) each found no session and each ran Zellij's background create. Run twice at once for
one name, that create can leave no session listed while a server keeps running with its socket
gone, which Zellij can then neither list nor end, and both starts still report success.

**Decision.**

- A plan starts a session with one command, `engineroom session start <name> <layout> <dir>`.
  It takes a file lock for the session's name in the program's state (`locks/`) and holds it
  until the session answers, so a second start waits, then finds the session running, says so,
  and goes on to join it.
- Under the lock, a running session is not started again, and an ended one is deleted first
  (D-015). Plans no longer end an ended session themselves before starting it, so a second
  machine never ends the session the first has just started. A restart still ends the running
  session first.
- Otherwise it runs the start commands (a direct start, or the desktop login's job, D-031),
  printing each as it runs it, then waits until the session answers. The plan shows these
  commands in the start's description, and its output shows each one as it ran.
- The lock is a file lock: a start that is killed lets go of it with its process, so the next
  start is never held up. A start interrupted at any point leaves the session whole or absent.
- A start waits up to 30 seconds for another start of the same session, then fails and says
  which lock file is held.

**Considered.**

- Leaving the create and the wait as separate commands in the plan: no lock can be held across
  two commands, and the race is between the create and the session answering.
- Detecting the half-made session afterwards and repairing it: the server with no socket can only
  be found by its process, and stopping processes found that way is what the program never does.

## D-036 The program checks a machine's setup itself, and a client is checked as a mirror of the engine

**Problem.** What a machine needs was listed only in `docs/setup.md`, and the `setup-machine`
skill decided in each conversation how to check it. A machine missing something found out when an
action failed, and someone without Claude Code had no check at all.

**Decision.**

- `engineroom setup check [--json]` reports every item for this machine's role, present or
  missing, with the command or file it was checked with and the fix for a missing one
  (acceptance/setup). It changes nothing, and ends with a failure when anything is missing. The
  checks live in one package (`internal/setup`), which the skill and the health view read too.
- A client mirrors the engine: it uses the engine, and works on its own when the engine does not
  answer. So every item for running sessions (Zellij 0.45 or later, git, OpenSSH, Claude Code with
  engineroom's plugin, a registered project) is checked on every machine. Only being reached
  (`engineroom` found by an SSH command, the SSH server on) is the engine's, and only reaching
  the engine and opening it in VS Code is a client's.
- The program checks only what it can check without asking anything. The guide's other items
  (keys, the settings repository, sleep, the login at the screen, the menu bar app) stay with the
  guide and the skill.
- The engine's SSH server is checked by connecting to port 22 on this machine and reading the
  greeting an SSH server sends first. It proves an SSH server answers here, which is what a client
  needs.
- VS Code's knowledge of the engine is checked by reading the SSH configuration VS Code uses
  (`remote.SSH.configFile`, or `~/.ssh/config`) with `ssh -G`, and comparing where it leads the
  engine's name with where engineroom's own SSH configuration leads it.
- An engine not reached is said with its cause, as every action says it (D-034).

**Considered.**

- `systemsetup -getremotelogin` for Remote Login: it needs an administrator, and the check runs
  as the developer.
- Checking that VS Code's file has a `Host` entry naming the engine: a name that resolves
  without one (a full DNS name) is fine for VS Code, and an entry that leads elsewhere is not.
- Every item in the guide: the rest need a person (a key copied in, a login at the screen) or
  read another machine.

## D-037 The health view checks the engine on request; a project says whether its app runs

**Problem.** Before leaving the engine to work from somewhere else, the developer had no one place
that said whether it was ready: set up, awake on power with the lid closed, and each running
session reachable by Remote Control with its app running. Whether an app runs differs from
project to project, as starting it does (D-006).

**Decision.**

- `engineroom health [--json]`, and the app's Health page, report the engine's setup items (D-036)
  and, beside them, staying awake on power, staying awake with the lid closed, and for each running
  session its Remote Control and its app (acceptance/health). Every item says how it was checked
  and, when missing, its fix. It ends with a failure when anything is missing, and changes nothing.
- The sessions checked are the running ones. Nothing records which sessions should be running.
- On a client it is the engine's report: the client runs `engineroom health --json` on the engine
  over SSH, as every client action does, and an engine not reached is said with its cause (D-034).
- Staying awake on power is `sleep 0` under AC Power in `pmset -g custom`. Staying awake with the
  lid closed is `AppleClamshellCausesSleep` read with `ioreg`: whether closing the lid would sleep
  the machine now. A Mac with no lid has no such entry and is ready.
- Remote Control is read from the running agent, not from how the session was started, because it
  is turned on either when engineroom starts the session or by Claude inside it (`/remote-control`).
  An agent is a process in the session (its `ZELLIJ_SESSION_NAME`) that Claude Code keeps a file
  for in its `sessions` folder, `<pid>.json`; the file carries `bridgeSessionId` while Remote
  Control is on. That file is Claude Code's own and not a published contract, so the check names
  it in how it was checked, and a change to it shows as Remote Control off rather than a guess.
- Remote Control is counted missing only where the developer has chosen it on this machine:
  engineroom's choice (`agent.remote_control`, or the app starting sessions with Remote Control),
  or Claude Code's `remoteControlAtStartup`. Otherwise it is shown, on or off, and never missing.
  The fix is `/remote-control` inside the session: a running session is never restarted to apply
  it (SESS-REMOTE-004).
- Whether a session's app runs is said by its project's optional `status` script, beside
  `create`, `up`, `down` and `teardown`, found the same way (D-013) and run the same way, in the
  checkout with the slot's values. It ends with success when everything the app needs is running,
  and what it prints is shown as the reason. It must change nothing and end within 10 seconds.
  The stack helper gives it `running NAME...`, true when each part `up` started is still running.
  A project with an `up` script and no `status` script is reported as not checked, never as
  running; a project with neither has no app to run.
- A fix the program can run (starting an app, `engineroom up`) runs only when the developer asks,
  as any action does. A fix that needs an administrator, or is typed inside a session, is shown and
  never run.
- The check runs when asked: the command, or the page opening and its Check again button. Nothing
  checks on a timer (D-023).

**Considered.**

- A list of sessions that should be running: the running ones are the ones the developer works on,
  and a list would be one more thing to keep up to date.
- Judging Remote Control by the agent's command line (`--remote-control`): it misses Remote
  Control turned on inside a session.
- Judging an app by its slot's ports answering: a port held by something else, or a part that
  listens but is broken, would read as running. The project knows what running means.
