# Using engineroom

The full guide to the command line, the terminal interface, the settings and the menu bar app.
For what engineroom is and why it exists, see the [README](../README.md); to set a machine up, see
[setup.md](setup.md). Examples use invented names: the engine is `engine.example`, a project is
`project-a`.

## Set up

On the engine:

```sh
engineroom role set engine
```

On each client, naming the engine as an SSH destination (an alias from `~/.ssh/config` works):

```sh
engineroom role set client --engine engine.example
```

To reach the engine from any network, put both machines on a Tailscale network and name the engine by
its Tailscale name (`engineroom role set client --engine engine.your-tailnet.ts.net`); see "Recommended" in
docs/setup.md. An engine used on its own, with no clients, needs only `engineroom role set engine`.

Register each repository's main clone on the engine:

```sh
engineroom project add ~/src/project-a            # named after its folder
engineroom project add ~/src/web-app --name web   # or under a name of your choosing
engineroom project add ~/src/tool --category tools  # listed under the heading "tools"
engineroom projects                               # list them
engineroom project remove web                     # forget one; nothing on disk changes
engineroom project here                           # exit 0 and the project's name when this folder is in
                                            # a registered project's clone or worktree, else exit 1
```

A category is only a label in the configuration: the projects list shows each category as a
heading with its projects under it, and the projects with none after them. Register a project
again with `--category` to give it one or change it; nothing on disk moves.

A client can register its own copies the same way, which is what "work locally" opens when the
engine does not answer.

The configuration lives in `~/.config/engineroom/config.toml` (or under
`XDG_CONFIG_HOME`):

```toml
role = "engine"

[agent]
command = "claude"          # what runs in a session's first tab
remote_control = false      # start new sessions with Claude Code Remote Control (m in the projects list)

[editor]
command = "code"            # VS Code's command line, the default

[[project]]
name = "project-a"
path = "/Users/you/src/project-a"
```

### Showing which sessions are waiting for you

`engineroom` lists sessions in one fixed order: your pinned projects' sessions first, then the rest, then
Ask Claude sessions, each by name. A session waiting for an answer keeps its place and is marked
with Claude's mark (✻) and its question, when Claude Code reports it, through two hooks that
engineroom's Claude Code plugin brings on every machine that runs sessions (see "The Claude Code
plugin" in [setup.md](setup.md)). They always exit 0, so a problem
with them never blocks Claude Code. A report older than `waiting_signal_limit` (default 6h) is
shown as unknown rather than believed.

## Use

```sh
engineroom                             # the session list: join, open a project, stop, history
engineroom open project-a              # open a project's session (creating it if needed) and join it
engineroom open project-a --no-join    # start its session and leave it running, without joining
engineroom new project-a fix-login     # new work: a worktree on a new branch, set up, with a session
engineroom clean project-a fix-login   # finished work: stop, tear down, remove; branch only if merged
engineroom edit project-a-fix-login    # open a session's worktree in VS Code
engineroom switch                      # inside a session: move this window to another session
engineroom history                     # every action this machine ran, with its commands
```

In the session list: `enter` joins, `p` shows the commands an action would run without running
them, `o` opens a project (there, `enter` opens and joins, `s` starts its session and stays on
the list, `n` starts new work, `m` turns Remote Control on or off), `e` opens the session's
worktree in VS Code, `x` stops a session (after asking), `c` cleans up its work, `h` shows the
history, `q` quits. Leave a session with Zellij's detach (`Ctrl o`, then `d`); it keeps running
on the engine.

On a client, a joined session survives the laptop sleeping, waking on another network or moving.
When the connection to the engine drops, the session keeps running on the engine and the window says so,
asks the engine every few seconds (showing the command it asks with), and joins the same session again
as soon as the engine answers. A session stopped or restarted meanwhile is not joined again, and `q`
stops the waiting.

The mouse works too: click a row to select it, click it again to act on it, and scroll with
the wheel. Where an action's commands are shown (before, while and after it runs, and in the
history), click a command to send it to the clipboard, or `[copy all]` for every command joined
with `&&`, so a paste stops at the first failure as the program does; `y` and `c` do the same
from the keyboard. The program asks the terminal to copy (OSC 52), which works over SSH and
through Zellij; see the setup guide for terminals that need clipboard access turned on. While
the program has the mouse, most terminals still select text when you hold a modifier as you
drag (`Shift` in many, `Option` in iTerm2).

A new session has two tabs: `agent`, with Claude Code alone on it (continuing the folder's last
conversation when there is one), and `git`. Open more tabs in Zellij when you need them.

### Remote Control

With Remote Control on, a new session's Claude Code starts with `--remote-control <session
name>`, so the session is listed in the Claude app and on claude.ai/code under the same name as
in Zellij, and can be worked on from there, including pasting images. The choice is remembered
per machine (`[agent] remote_control`); a client sends its own choice with each request, so the
engine's setting never decides for it. It applies when a session is created: a running session is
joined as it is. Combine it with `s` (or `--no-join`) to start a session and carry on in the app
without joining it here.

### New work and clean up

`engineroom new` fetches the project's remote and creates a sibling worktree named
`<project>-<work>` on a new branch `<work>` from the remote's main, then opens a session in it.
`engineroom clean` refuses while the worktree has uncommitted changes or commits that were never
pushed; otherwise it runs the project's down, ends the session fully (nothing of it is left
listed as ended), stops anything still running that was started for it, removes the worktree, and deletes the branch, here and
on the remote, only when it is merged into main. The main clone, `main` and release branches are
never touched. When it refuses, the app offers what would make it safe: save the changes to the
branch and push it, put them aside as a stash of the repository (which outlives the worktree;
`git stash list` in any checkout finds it), or push the commits. Each is shown before it runs;
throwing work away is never offered.

A worktree can instead be kept for a role, such as a designer's, a tester's, or a primary and a
secondary, holding one branch after another (D-024). It is named `<project>-<role>`, so its
session name and its ports stay the same whichever branch it holds:

```sh
engineroom api run new-role project-a designer nav-rework     # the designer worktree, on nav-rework
engineroom api run next-branch project-a designer search-fix  # done with nav-rework: move on
engineroom roles project-a                                    # the project's roles
```

The role is recorded in your settings (`roles.json` in the project's settings folder), so every
machine knows it; cleaning up the worktree forgets it. Moving on refuses, with the same offers,
while work would be lost. The app's New worktree asks which kind you want.

A project that needs more than a plain worktree (a database, a storage emulator, installed
dependencies) has executable lifecycle scripts: `create` runs in a new worktree before its
session opens, and `teardown` runs before a worktree is removed. Each gets `ENGINEROOM_PROJECT`,
`ENGINEROOM_WORK`, `ENGINEROOM_MAIN` and `ENGINEROOM_WORKTREE` in its environment, and its output is shown while it
runs.

### Your settings and a repository's recommendations

Everything you set for a project lives in your own settings folder, outside every repository:
`engineroom settings where <project>` prints it (`~/.config/engineroom/settings/projects/<owner>/<repository>/`,
keyed by the project's GitHub owner and name). It holds the project's `engineroom.toml` and its scripts
(`create`, `up`, `down`, `teardown`). Nothing of yours is ever committed to a repository you
work in but do not own.

Keep the settings folder in a private git repository of your own and it is the same on every
machine (D-014):

```bash
engineroom settings init git@github.com:<you>/engineroom-settings.git   # git init, git-crypt init, commit, push
engineroom settings status                                        # where it syncs, what is not sent, are secrets readable here
engineroom settings sync                                          # commit here, take the others', send this one's
engineroom settings clone <repository> <key file>                 # on another machine: clone and unlock
```

A project's secrets (a local settings file with keys, an environment file) go in its settings
folder under `secrets/`. git-crypt encrypts every file in a `secrets/` folder when it is committed,
so the repository, GitHub and any machine without the key hold only encrypted bytes. On a machine
with the key they are ordinary files: open, read and edit them as you would any other. Save the
key where you keep secrets (`git-crypt export-key <file>` in the settings folder, store the file,
delete it); another machine needs it to read them. A sync refuses to commit a file in a secrets
folder that would not be encrypted.

Name a project's secret files in its `engineroom.toml`, before any table:

```toml
secrets = ["Server/App/local.settings.json", ".env", "~/.microsoft/usersecrets/<id>/secrets.json"]
```

`engineroom secrets save <project>` copies them from the main clone (or `--worktree <dir>`) into the
project's `secrets/`, and the next sync commits them encrypted. `engineroom secrets restore <project>`
puts them into a checkout that lacks them, and new work does that for every new worktree before
its `create` script runs. A file the checkout already has is never overwritten: one that differs
from the saved copy is named for you to compare. `engineroom secrets status <project>` shows each
file: saved, missing, or different. The app syncs when you open its panel, at most once an hour, and
has a Sync now button in Settings.

A repository may commit the same files in a `.engineroom/` folder. Those are recommendations: your
settings decide, section by section and script by script, and a repository's `.engineroom/` is used
only for what your settings leave out.

**Scripts must be safe to run again.** Run twice, `up` finds what it installed and started the
first time and leaves it alone; `down` with nothing running does nothing; `create` and `teardown`
find their own work done or undone. The program runs a script whenever it is asked to and never
checks for you: check before you install, build or start, and exit 0 when there is nothing to do.

### Running the app stack

Starting the stack is a full build and run of one worktree: `up` installs, builds and starts
everything the worktree needs (servers, emulators, a database) on that worktree's own ports and
returns once it is running; `down` stops it. `u` and `d` in the session list run them for the
selected session; `engineroom up` and `engineroom down` do the same from a shell, and inside a session they
act on that session, so the agent builds and runs the stack itself while it works (the
`run-stack` skill tells it how). Clean up runs
`down` before `teardown`.

The scripts are yours, and engineroom has a helper for them: `. "$(engineroom stack-lib)"` at the
top of an up or down script gives it `deps <dir>` (installs npm packages when the lock file is
newer), `start <name> <port> <url> <dir> -- <command...>` (runs the command in the background,
waits for the URL to answer, or the port to listen with `-` as the URL, and leaves a part alone
that already runs for this worktree, refusing a port another worktree's part holds) and
`stop <name>...` (stops exactly what `start` started, and what that started). The pids and logs
are kept outside the worktree. For example:

```bash
#!/usr/bin/env bash
# up: the web app on the slot's ui port
. "$(engineroom stack-lib)"
deps . || exit 1
start web "${ENGINEROOM_PORT_UI:-5173}" "http://localhost:${ENGINEROOM_PORT_UI:-5173}/" . -- npx vite --port "${ENGINEROOM_PORT_UI:-5173}" --strictPort
```

and `down` is `. "$(engineroom stack-lib)"` then `stop web`. The `adopt-project` skill writes
them for a repository, starting from your own recipes.

### Ports and slots

Each project's ports are reserved for it, so no two projects can be given the same port, and
each worktree runs on its own numbered **slot** of them, so several worktrees of one project run
side by side without reaching each other's servers or data.

```toml
# engineroom.toml in your settings for the project
reserve = ["20100-20199", "4100", "4200"]   # the ports this project owns

[slots]
record = ".engineroom-slot.json"                  # written into each worktree that holds a slot

[[slot]]                                    # slot 1 is the main clone: the numbers it has always used
slot = 1
ports = { ui = 4100, api = 4200 }
names = { database = "app" }                # names only, never a password

[[slot]]
slot = 2
ports = { ui = 20110, api = 20111 }
names = { database = "app_wt2" }

[derived]                                   # more values, built from a slot's own
apiOrigin = "https://localhost:{{api}}"

[[render]]                                  # a template filled in for each worktree
template = "appsettings.Worktree.json.tmpl" # beside engineroom.toml
to = "server/appsettings.Worktree.json"
```

- `engineroom ports reserve <project> --write` gives a project that names its `[ports]` a block of 100
  ports from 20000-29999 and writes ten slots: slot 1 keeps the project's own numbers, slots 2 to
  10 take ten ports each from the block.
- New work claims a free slot (`engineroom slot claim`, shown among its commands) and writes the slot's
  record, the same values as an environment file beside it, and each template. Clean up gives the
  slot back. A slot belongs to the worktree folder, never the branch. A main clone that has
  claimed nothing runs on slot 1, so a worktree is given slot 1 only when the main clone holds
  another; with no slot free, new work is refused before anything is created.
- New work runs the developer's own create script, or else the repository's `.engineroom/create` as
  the commit it starts from has it (the remote's main), not the branch the main clone has out.
- The project's scripts and tasks are run with every value of the worktree's slot set as an
  environment variable named by its kind: the program's own as `ENGINEROOM_SLOT`,
  `ENGINEROOM_WORKTREEROOT`, `ENGINEROOM_ISMAINCLONE`, `ENGINEROOM_PROJECT` and `ENGINEROOM_TMP`,
  and the project's as `ENGINEROOM_PORT_<KEY>`, `ENGINEROOM_NAME_<KEY>` and
  `ENGINEROOM_DERIVED_<KEY>` (for example `ENGINEROOM_PORT_WEB`), the key in capitals with
  anything not a letter or digit made `_`, whatever the record is called. A port, name or derived
  value whose variable would be the same as another's is refused when the settings are read
  (D-028). The record and `{{key}}` keep the plain keys. The project's own code reads
  its ports from the record (or `engineroom slot get <key>`, `engineroom slot env`), falling back to slot 1's
  numbers when there is none. A port written into a config file is used
  whatever is reserved.
- Add the record, its `.env` and every rendered file to the repository's `.gitignore`;
  `engineroom slot claim` warns about any that git does not ignore, and `engineroom slot ignore` adds them
  (asking again changes nothing). Commit the `.gitignore` it changes.
- `engineroom ports` lists every project's reservation and slots; `engineroom ports check` reports every
  clash, with both projects named, and which reserved ports something is listening on now. Fixing
  a clash is a change to a project's settings; the program never renumbers anything.
- `engineroom slot status --hook` as a Claude Code SessionStart hook tells each session which slot it is
  on, and warns in a linked worktree that holds none.

### Reaching the engine's ports from a client

On a client, a session's ports on the engine can be reached on the client's own localhost, under the
same numbers, whether or not you joined the session. Forward the worktree you are testing:
`engineroom forward add <session>` (in the app, the session's Forward switch, or simply opening one of
its links), and `engineroom forward remove <session>` to stop. `engineroom forward on` forwards every running
session instead (`engineroom forward skip <session>` leaves one out), and `engineroom forward off` closes every
forward.

One SSH connection holds every forward, and each forward added or cancelled is a command in the
history. A port the client already uses is named with what holds it and left alone. While anything
is forwarded, `engineroom forward --watch` (the app runs it) follows the engine's session list over that
same connection: the engine sends it only when a session starts or stops, so a forwarded session's
ports follow it within seconds and nothing crosses the network in between. With nothing to
forward, the connection is closed. With the connection gone (the Mac slept, or the network moved)
the watch opens nothing; the app opens it again at launch, on wake, when you open its panel and
after each action it runs. `engineroom forward` brings the forwards in line once; `engineroom forward --check`
does so only over a connection already open. Apart from the watch, the app keeps one connection to
the engine open while it runs, `engineroom api feed`, over which the engine sends the sessions only when they
change; it reads nothing on a timer (D-023).

### Repositories from GitHub

A root is a folder on the engine for one GitHub owner's repositories, with the `gh` account that
can list them. Keep one root per account or organisation:

```sh
engineroom root add ~/src/owner-a --owner owner-a --account account-a
engineroom roots                                   # list them
engineroom browse owner-a                          # the owner's repositories, marking those already here
engineroom clone owner-a widget --category tools   # clone into ~/src/owner-a/widget and register it
engineroom create owner-a gadget --category tools  # make it, create it on GitHub (private), push, register
```

In the interface, `g` on the projects screen opens the roots; `enter` browses one, typing
filters the list, `enter` adds a repository and `ctrl+n` starts a new one. Creating shows its
commands and waits for `enter`, because it publishes a repository. Projects inside a root are
listed under the root's name, then their category.

Each root's account is used through `engineroom gh --account <account>`, which runs `gh` with that
account's token, so gh's active account is never switched. Cloning uses
`git@github.com:<owner>/<repository>.git`; which SSH key reaches it is the machine's own git
and SSH setup.

### Working local

On a client, the app's "Work local" switch (or `f` in the interface, or `engineroom offline on`) takes
the work over from the engine: everything then runs on this machine as though it were the engine, using
the copies registered here, and every screen says so. It stays that way until switched back
("Hand back to the engine", `f`, or `engineroom offline off`). Switching either way stops nothing. The
last machine in use has the work, so take it over while both are on.

Each switch shows a handover first, because each machine has its own copies of the work, and
nothing runs until you press enter (or Run in the app). With the engine out of reach, a switch to
working local still happens, saying the engine could not be checked (CONN-HANDOFF-005), and
handing back with the engine out of reach pushes what this machine can. Working it out reads every registered
project on both machines against GitHub, several projects at a time; while it does, the app shows
a meter for each machine, the project it last read and the command doing the reading
(`engineroom api plan` writes that progress to its standard error, one `{"progress": ...}` line at
a time, CONN-HANDOFF-013).

A handover leaves the receiving machine in the giving machine's state, checkout by checkout: the
same branch, at the same commit, with the same uncommitted work (D-017, amended). It is all or
nothing:

- **Checked first.** Working it out finds anything that would stop the receiving machine reaching
  that state, and then the plan has no commands, only what to clear (CONN-HANDOFF-015): a
  checked-out branch that has moved on both here and on GitHub, a release branch (`release/...` or
  `release-...`) with commits GitHub lacks (a handover never pushes one, since pushing one can
  start a release), a receiving branch with commits of its own, a giving branch behind GitHub where
  the receiving machine already has GitHub's newer commits (taking it would move the receiving
  checkout back; bring the giving machine up first, CONN-HANDOFF-018), a checkout on a detached
  commit, a branch checked out in another place on the receiving machine, or two different
  repositories under one name. A switch to working local, or back, waits for the handover. When it can run, its
  first commands change nothing: each machine checks its checkouts are still as they were read
  (`engineroom handoff verify`), and each push is tried with `--dry-run`, so an edit or a commit
  made since, or a GitHub that moved on, stops it before anything changes (CONN-HANDOFF-017).
- **Pushed.** The branch each checkout of a registered project has out (its main clone and its
  worktrees) is pushed where GitHub lacks commits of it, or lacks it. Any other branch with commits
  GitHub lacks (a backup, an experiment) is named with the command to push it and left where it is.
- **Matched.** Each receiving checkout (the main clone for the main clone, and the worktree in the
  same place beside it for each worktree) sets its own uncommitted work aside in its stash list,
  labelled with its branch (`engineroom handoff aside`), then is put on the giving checkout's
  branch at its commit (`git switch -C`, safe since that branch there has nothing GitHub lacks). A
  worktree the receiving machine lacks is made, with a slot and the project's secret files
  (CONN-HANDOFF-003, 016).
- **Carried.** The giving checkout's uncommitted work, changed files and new files git does not
  ignore, travels as a stash straight between the two machines over SSH, never through GitHub
  (CONN-HANDOFF-008, 014): stashed there (`engineroom handoff pack`, kept in its stash list as a
  copy), sent under `refs/engineroom/handover/<branch>`, and applied (`engineroom handoff unpack`).
  A session still running in the giving folder is named, since its changes leave it.
- **What never travels:** ignored files, engineroom's slot files, Finder's `.DS_Store` files and
  the `.claude` folder (Claude Code's agent memory and this machine's settings for the project).
  They belong to each machine, so they are neither carried nor set aside. Anything ignored that has
  to follow you belongs in your settings as a secret file.
- A project the receiving machine does not have is cloned to the same place under its home,
  registered under the same name and category, given its secret files, and matched like any
  other; a clone of the same repository already at that place is registered instead of cloned
  over, and a folder there holding anything else blocks the handover.

The plan says what it does one line per checkout ("project-a: switched from main to
feature at 3f2a9c1 on this machine; 2 changes (...) carried from the engine"), then
folds what it leaves alone into a line each: branches in no checkout with commits GitHub lacks,
and projects it could not check (CONN-HANDOFF-019). What it blocks on comes last. What a handover
said, carried and set aside is kept with it in the history.

On the engine, Settings, "Release…" (or `engineroom handoff release`) pushes the work checked out, before
you work local on a client. It has no other machine to carry uncommitted work to, and work in
progress does not go to GitHub, so it names that work and offers to commit it as partial work (a
button in the app, which commits it on that worktree's branch, for the next release to push); the engine then shows when it
was last released, as information only. While a client can reach the engine, the app keeps it ready
when you open its panel, at most once an hour (`engineroom mirror`): it copies the engine's projects it lacks and fetches the rest, and
moves no worktree. `engineroom handoff to-engine` and `engineroom handoff from-engine` show either handover at any
time. Back from working local, sessions you started on this machine keep running, and are listed
after the engine's under a heading of their own.

### The menu bar app

`scripts/install.sh --app` builds the menu bar app into `~/Applications/engineroom.app`. It has two
parts. The panel from the menu bar holds what you use many times a day, as a list and a detail
(D-025). The list is one row per project: your pinned projects first, always there whether running
or not, then every other project with a session running, each in a fixed order by name, with one
light for all its sessions (Claude's mark when one waits for you). Point at a project, or click it,
and the right shows its GitHub line (code, issues, pull requests) and one compact card per
checkout, running or not: what is unsaved, not pushed, behind main or merged; VS Code, Finder
(when the folder is on this Mac: on the engine, or on a client working local), Ask Claude, its
branch and pull requests on GitHub and copying the branch name as icons; every action as a button
(Join, Restart and Stop, or Start; Next branch for a role worktree; Close for a worktree); the
question Claude is waiting on; and the links its stack serves, with Run app and Stop app (its up
and down scripts) and its Forward switch on a client. Opening a link first asks the machine the
stack runs on whether anything answers on the link's port (`engineroom api plan serve <session>
<port>`); when nothing does, the app is started, shown as any action is, and the link opens once
it is up (WORK-STACK-015). The project's Ask Claude sessions follow as one line each. On a client the panel's header
names the engine by its own machine name (on a Mac, its name in System Settings, Sharing), read
with the engine's version when the panel opens (`engineroom api engine-version`), not by the SSH
name this machine reaches it with (CONN-ROLE-003). When the two run different builds, the panel
says which is behind and offers to upgrade it: Upgrade starts a Claude session on that machine
(`engineroom-upgrade`, briefed by `engineroom upgrade prepare`) in the clone the running build was
made from; it pulls `main`, runs `scripts/install.sh --app`, restarts the app with
`engineroom app relaunch` and says what changed, and it asks before anything that could lose work.
Each press starts a new upgrade session, ending one left from an earlier upgrade (APP-UPGRADE-003).
Settings has the same buttons. The panel opens on the project
you last chose, and comes back to it after an action. Its gear opens the window, which holds
everything else: a sidebar of your projects (pinned first), the repositories in your roots (pinned,
registered by category, here but not registered, and on GitHub but not here), the history and the
settings. A project's page shows each checkout (the main clone and every worktree) as a card: Join
or Start and Stop for its session, Run and Stop for its app and its tasks, the links it serves with
its Forward switch, VS Code, Finder (when the folder is on this Mac) and Ask Claude, and under More info its branch, folder, slot and ports. A project
on GitHub has one line under its name linking to the repository's code, issues and pull requests,
and each checkout whose branch GitHub has links to that branch and the pull requests from it. The
page also adds a worktree, shows the project's scripts, ports and secrets from your settings, and pins,
categorises or unregisters it. Name the links a stack serves in your settings:

```toml
[links]
app = "https://localhost:{{ui}}/"
api = "http://localhost:{{api}}/health"
```

Without them each port is shown as `http://localhost:<port>`. A session is running or it is not
there (D-015): closing the terminal window leaves it running, Stop ends it fully, and starting
one that ended starts it fresh. What runs for a session lives as long as it (D-020): Stop runs
the project's down, ends the session, then stops anything still running that was started for it
(`engineroom session reap <name>`; `--dry-run` only says what); Restart keeps the stack running.
Everything engineroom runs for a checkout carries `ENGINEROOM_SESSION`, the checkout's session, so it is known
to belong to that session. Only servers are stopped this way (what listens on a port, with what
started it and what it started): an editor opened from inside a session stays open. A session
that ends without engineroom (quit from inside Zellij) is found by `engineroom sweep`, which stops what still
runs for it once it has been gone two minutes and forgets ended sessions. The menu bar app runs
the sweep (`engineroom sweep --watch`, every minute, ending with the app) on the engine from when it
starts, and on a client only while it works local; there is no job of its own to install. It
reads that machine's sessions and processes only, never the network. A server
on a project's ports that belongs to no running session (started by hand, or for a session that
ended and has not been swept yet) is listed on the project's page under "Running with no
session", with its port, pid, start and folder, and a Stop for that one process
(`engineroom api run stop-server <project> <pid> <port>`, which stops it only while it still listens
there). It starts a session three ways: in Zellij with a terminal
window, with Remote Control and no window, or both. Claude Code always runs inside Zellij, so
anything started can be joined from the other Mac. It opens VS Code on the engine directly or over
SSH from a client, gets, creates and registers repositories, and has a Work local switch on a
client. It runs nothing itself: everything is a `engineroom api` command, shown before it runs where
it creates something, and kept in the history. Settings chooses the terminal (from those
installed, Ghostty by default) and opening at login.

### Ask Claude, and a project's own tasks

The ✨ button on a project, a worktree or a running session opens a Claude session of its own on the
engine, in that folder, beside its main session (named with `-ai`). Claude is told what you opened
(the project, the worktree, its branch, ports, links, scripts, secret files, tasks and settings
folder, which is opened to it) and how engineroom wants things done, then asks what you want. Say it in
plain words: "make the up script seed the database", "the teardown should drop this worktree's
database", "build a worktree from here for fix-login". It starts the way your sessions do, so on a
client you can use it in the Claude app.

A project can name its own tasks in its `engineroom.toml`, for things done now and then:

```toml
[[task]]
name = "Copy the shared test database"
about = "a local copy of the shared test data, for trying a migration"
run = "scripts/copy-db.sh shared-test app_local"   # or ./file, kept beside engineroom.toml
```

They are under Tasks on each worktree in the app (shown before they run), `engineroom task <name>` runs
one inside a session, and Claude knows them.

### VS Code

On the engine, `e` (or `engineroom edit <session>`) runs `code <worktree>`. On a client it runs
`code --remote ssh-remote+<engine> <worktree>` on the client, so VS Code's Remote - SSH extension
opens the engine's folder with the engine's files and toolchain; nothing is copied. That needs the
extension installed, and the engine's SSH destination resolvable in the SSH configuration VS Code
uses (`~/.ssh/config` unless its `remote.SSH.configFile` setting says otherwise). If `code` is
not found, install it from VS Code's Command Palette with "Shell Command: Install 'code'
command in PATH", or set `command` under `[editor]` to another compatible command.

