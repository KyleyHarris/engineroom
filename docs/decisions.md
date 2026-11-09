# Decisions

Append-only, numbered in order. Each entry records what was decided, the options weighed, and
why the others lost, so "was this already decided?" has one place to look.

## D-001  2026-11-04  A repository of its own, private until a deliberate decision to publish

Context:    The hub and client tooling first grew inside the architect's personal configuration
            repo. It is general machine plumbing that other people could use, with its own
            tests and releases, while the personal repo holds how one person works.
Options:    (a) A separate repository, private now, published only by a later, explicit
                decision [CHOSEN]
            (b) Keep it in the personal configuration repo - rejected: it would mix a reusable
                tool with one person's rules, and could never be published.
            (c) A separate repository, public from the start - rejected: publishing cannot be
                undone, and the tool has not yet proven itself in daily use.
Decision:   This repository, created private on 2026-11-04 at the architect's request. Nothing
            personal is ever committed (see CLAUDE.md), so publishing stays possible at any
            time without rewriting history.
Status:     accepted

## D-002  2026-11-04  Go, with a terminal UI and a scriptable command line, one binary per machine

Context:    The work already happens in terminals and in Zellij, on the hub and over SSH from a
            client, sometimes from a phone terminal.
Options:    (a) Go: a terminal UI, plain subcommands, and `--json` output from one binary
                [CHOSEN]
            (b) A native macOS app - rejected as the core: it cannot run over SSH or inside a
                Zellij pane. It may come later as a thin face over (a).
            (c) A shell script with a fuzzy finder - rejected: fine for a list, awkward for
                multi-step flows, live progress and two roles.
Decision:   Go, confirmed by the architect on 2026-11-04 ("go is good"). The same binary runs
            on the hub and on clients; a per-machine setting says which it is.
Status:     accepted

## D-003  2026-11-04  Journeys drive the real binary in a real terminal, against real dependencies

Context:    The program is a terminal UI and a command line, not a web page, so a browser test
            runner has nothing to drive. The acceptance criteria still need executable journeys
            bound to their own clauses, and recorded evidence of each run.
Options:    (a) Run the built binary in a pseudo-terminal at a fixed size, type keys as a person
                would, read the screen through a terminal emulator, and wait on visible text
                [CHOSEN]
            (b) Test the screen model inside the process only - rejected as the journey layer:
                it never runs the binary, the terminal or the commands the program issues. It
                stays available for unit-level screen tests.
            (c) Assert on the printed command list alone - rejected: it proves what the program
                meant to do, not what happened.
Decision:   - Unit tests (`go test`) cover pure logic: the project registry, port clash checks,
              branch name rules and each action's command list.
            - Journeys run the real binary in a pseudo-terminal at a fixed size. They wait on
              screen text, never on a fixed sleep. The text a person reads is the selector.
            - Clauses come from the acceptance documents, never retyped:
              `criteria-review generate --format json` feeds a generator that emits one Go
              struct per scenario with one field per clause. A reworded or invented clause fails
              to compile; a skipped clause fails the gate.
            - Dependencies are real and isolated: Zellij with its own socket directory per run,
              git in throwaway repositories, SSH to a throwaway hub in a container. An
              unreachable hub is a real address that does not answer. macOS-only behaviour is
              checked on a Mac. One smoke run goes against a real hub.
            - What a screen cannot show is checked through the system itself (`git worktree
              list`, `zellij list-sessions`).
            - The gating run records the terminal as a cast, captioned with each clause, and
              renders it to video. Recording and pass or fail are one code path.
            - Every journey is observed failing at least once, with the change that broke it
              recorded.
            - `scripts/verify.sh` is the format, lint, build, unit test and generate-check gate.
              Journeys run as a separate command.
            Approved by the architect on 2026-11-04 ("testing approved").
Status:     accepted

## D-004  2026-11-04  Work is tracked in GitHub issues, written for a public reader

Context:    Each task needs a record that names its brief and the acceptance scenario ids it
            covers. The repository may be published, and an issue tracker is public with it.
Options:    (a) GitHub issues in this repository [CHOSEN]
            (b) A document in the repository - rejected: issues are where open source projects
                take work from, and they link to pull requests.
Decision:   Task records are GitHub issues in this repository. They are written as for an
            outside contributor: clear, professional, following common open source conventions
            (issue and pull request templates, a contributing guide, a security policy, a
            licence). The no-personal-data rule in CLAUDE.md applies to issues, pull requests
            and labels as much as to files. Decided by the architect on 2026-11-04.
Status:     accepted

## D-005  2026-11-04  Each repository owns its port numbers; the tool reserves, isolates and reports clashes

Context:    Several repositories, and several worktrees of one repository, run dev servers and
            test stacks on one machine at the same time. Two stacks on one port do not fail
            loudly: the second one is reached by mistake. The numbers also have to stay the same
            from day to day, because people, bookmarks and registered sign-in callbacks use them.
Options:    (a) The repository commits its own port table; the tool reads every registered
                table, reports clashes and never renumbers [CHOSEN]
            (b) The tool assigns numbers - rejected: numbers would differ between machines and
                over time, and a repository could not be run without the tool.
            (c) Slots for every repository - rejected: most repositories run one stack and need
                nothing beyond their base ports.
Decision:   - Worktree slots are opt in. A repository that wants them declares in a committed
              file how many slots it has and every port each slot binds. A repository without
              slots declares its base ports only.
            - The tool reports a clash between repositories, or with a process already listening,
              and names both sides. Fixing it is a change to the repository's table, never an
              automatic renumbering.
            - Claiming a slot is keyed by worktree path, is safe when two claims race, and is
              released when git no longer lists the worktree.
            - Where a repository uses blob or table storage emulation, each slot can run its own
              emulator instance on its own ports with its own data directory inside the
              worktree, git ignored. The slot's full connection string is written to the
              worktree's git-ignored settings. The emulator's account key is a published
              development constant, not a secret.
            - Where a repository has a database, each slot can have its own.
            - A slot's database is handed over by NAME only. No password is ever written into a
              worktree. The full connection string is assembled where the password already
              lives: by the application from its own secret store, or by the tool in the
              environment of a process it starts, never in a file. (The architect, 2026-11-04:
              "names only is fine".)
            - The tool always writes one standard git-ignored record per claimed slot (JSON and
              an env file). A repository may also commit templates, which the tool renders
              into git-ignored files for that slot, so a repository of any language gets its
              settings in its own format without the tool knowing that format. (The architect
              left the mechanism to the implementer: "do it the way that works best".)
            Decided by the architect on 2026-11-04 ("a repo is good because we want consistency
            of numbers.. but the tool reports clashes so the repo can fix a clash").
Status:     accepted

## D-006  2026-11-04  Repository-specific setup lives in the repository, as scripts the tool runs

Context:    What a new worktree needs differs from repository to repository. One has a
            database and a storage emulator per slot, another has neither, another only needs
            its dependencies installed. A tool that knew about each kind of resource could never
            be generic, and every new stack would need a change to the tool.
Options:    (a) Each repository commits a small description of itself and lifecycle scripts;
                the tool runs them at fixed points, and the scripts call back into the tool's
                command line [CHOSEN]
            (b) The tool knows each kind of resource (emulators, database servers) - rejected:
                never generic, and the list grows with every stack.
            (c) A Claude Code skill performs the setup - rejected as the mechanism: it needs a
                model and is not guaranteed to run the same way twice. A skill can call the same
                scripts, so the two never diverge.
Decision:   - A repository that wants more than a plain worktree and session commits a `.hub/`
              folder: a manifest naming its slots, their ports, and its scripts. The manifest
              format is settled when it is built.
            - Lifecycle scripts, each optional:
              create, run once after the worktree exists and its slot is claimed, creates what
              is unique to that worktree, including databases and storage;
              up, run on request, starts what the worktree needs to run and returns once it is
              running (servers go to the background or a container);
              down, run on request and before teardown, stops what up started;
              teardown, run before the worktree is removed, deallocates what create made.
              (Amended 2026-11-05: an `open` script run on every session open was replaced by
              up and down run on request, see below.)
            - Contract: a script runs in the worktree with `HUB_*` environment variables (the
              worktree, the slot, each named port, the settings record). It calls the tool's
              command line to read slot details and to record what it created (resources, URLs,
              extra ports to forward). Recorded resources are listed to the developer before
              teardown and in the clean up, so nothing allocated is forgotten.
            - Scripts must be safe to run again. A failure is reported with the step that
              failed, and retry or clean up is offered; a half-made worktree is never left
              unannounced.
            - Script output streams into the learning view, and the history keeps the script's
              content as it was when it ran.
            - The first run of a script, and the first run after it changes, shows it and asks
              the developer to trust it, because running it runs the repository's code.
            - Amends D-005: a slot's storage emulator and database are provided by the
              repository's scripts, not by the tool. The tool still owns the slot, the settings
              record, the clash report and port forwarding.
            Decided by the architect on 2026-11-04 ("some repos don't even have a database ...
            each repository knows what to do and maybe we also have a tier down when deleting a
            work tree, run this script to tier resources down and deallocate things by calling
            the hub CLI").
Status:     accepted
Amended:    2026-11-05. The stack is started on request, not on every session open: the
            developer opens sessions far more often than they run the app, and a session is
            often opened only to talk to the agent. `open` (never built) is replaced by `up` and
            `down`, run from the interface, the command line (`hubs up`, `hubs down`) or by the
            agent inside a session, so a request to run the stack always goes through the
            repository's script. Clean up runs `down` before `teardown`. A repository with no
            `up` is told what one must do; the tool never guesses how to start a stack. Decided
            by the architect ("lets build the features", issue #32, names approved 2026-11-05).

## D-007  2026-11-04  The editor runs where the developer is; on a client it opens the hub's folder over its own SSH remote

Context:    Issue #11: open a session's worktree in VS Code from the session list, on whichever
            machine the developer is at. On a client the files live on the hub, and reading and
            debugging them should use the hub's files and toolchain.
Options:    (a) Run the editor's command line on this machine. On the hub, or working locally,
                `code <folder>`; on a client, `code --remote ssh-remote+<hub> <folder>`, the
                hub's folder through VS Code's Remote - SSH [CHOSEN]
            (b) Run the editor on the hub - rejected: the hub's screen is not where the
                developer is, and a hub reached over SSH has no display to open a window on.
            (c) Mount or sync the hub's folder to the client - rejected: copies drift, and the
                client's toolchain is not the hub's.
Decision:   - The plan has one command, run on this machine, never wrapped in ssh; nothing runs
              on the hub for it.
            - The hub reports each session's absolute folder in `hubs sessions --json` (`path`),
              so the plan is built from the list already read, with no second request.
            - The command is `[editor] command`, default `code`. Another command must accept the
              same `--remote ssh-remote+<host> <folder>` arguments from a client.
            - The remote is `hub.host` exactly as configured. `hub.ssh_config` is not passed:
              VS Code resolves the host through its own SSH configuration
              (`remote.SSH.configFile`), so the host must resolve there too.
            - Refused before anything runs: a session outside every registered project (its
              folder is not known), a hub that did not report a folder, and a command that is
              not installed, which is answered with how to install it.
            Decided while building issue #11, to the intent stated in it, and confirmed by the
            architect on 2026-11-04 ("if we are on a hub session, I want a remote connection to
            the hub source code. If I'm using a local session ... I want to open VS Code on the
            local repository. It should know which one and do the right thing").
Status:     accepted

## D-008  2026-11-05  A native menu bar app, a thin face over hubs api; amends D-002

Context:    The terminal interface has to be started in a terminal each time, so a session and
            then VS Code means running it again. The developer works from two Macs, the hub and
            a client, and wants the tool always within reach, and the right thing done on each
            (VS Code directly on the hub, over SSH from a client). A window inside a Zellij
            session cannot know which Mac is looking at it, because its panes run on the hub.
Options:    (a) A native SwiftUI menu bar app that runs nothing itself: every list and action is
                a `hubs api` command printing JSON [CHOSEN]
            (b) A window inside Zellij - rejected: it always runs on the hub, so it cannot open
                the editor or a terminal on the Mac the developer is at.
            (c) A local web page - rejected: a server to run and secure, for two Macs.
            (d) The app computing plans itself - rejected: two engines drift, and the history
                would miss what the app did.
Decision:   - `hubs api` is the app's only source: status, sessions, repos, plan and run (one
              JSON event per step, recorded in the history like any action), and set. It acts
              where this machine's actions are done: the hub over SSH for a client that is
              online, this machine otherwise (offline, CONN-OFFLINE).
            - Claude Code always starts inside Zellij, so anything started can be joined from
              any machine; the app starts it to join (Zellij), with Remote Control and no window,
              or both. Joining opens a window of the chosen terminal running `hubs join`.
            - The terminal is chosen from those installed (Ghostty by default); joins open
              through `open` or `osascript`, shown as a command like any other.
            - Built from macos/ with SwiftPM and bundled by `scripts/install.sh --app` into
              ~/Applications, signed with the Mac's Apple Development identity or ad hoc; no
              notarization for a locally built app. The phone is out of scope: it checks on
              sessions through the Claude app and Remote Control.
            - Amends D-002: the native app, rejected there as the core, is added as a face over
              the core, which stays the one Go binary.
            Decided by the architect on 2026-11-04 and 2026-11-05 ("the ui outside of a terminal
            however can know if it is the client or server and lauch vs code local or ssh";
            "All of the above options look good approved").
Status:     accepted

## D-009  2026-11-05  Switching offline or online hands work over through GitHub, shown and run on request

Context:    Offline, a client works in its own worktrees (CONN-OFFLINE). The client and the hub
            share nothing but GitHub, so work committed on one is invisible to the other until
            it is pushed there and brought in, and a session started offline drops out of sight
            once the client is back online.
Options:    (a) A handoff on each switch: read both machines' branches against GitHub, show what
                one holds that the other does not, and plan real git commands (push from the
                giver, fetch and merge --ff-only on the receiver), run only when asked [CHOSEN]
            (b) Sync automatically on the switch - rejected: it would change worktrees and GitHub
                unasked, and "the program never hides what it does".
            (c) Copy worktrees or Claude's conversations between machines directly - rejected:
                copies drift (D-007), and conversations are stored under each machine's own paths.
Decision:   - Going offline offers the hub-to-client handoff, read before the switch while the hub
              may still answer; going online offers the client-to-hub one once the hub answers.
              `hubs handoff to-hub|from-hub` shows either at any time. Each is a plan in the
              history like any other action.
            - Reading changes nothing: remote URL, ls-remote, for-each-ref, rev-list, worktree
              list and status, never fetch. The hub's reading is `hubs handoff state --json`.
            - Only branches checked out in a worktree are pushed: that is the work in hand, where
              sessions run. A branch with unpushed commits in no worktree (a backup, an
              experiment, a release branch) may be kept off GitHub on purpose, so it is named with
              the command to push it by hand.
            - Every push runs before any worktree moves, so a refused push leaves both machines'
              worktrees as they were. A worktree moves only by merge --ff-only. Left alone with a
              note: a worktree with uncommitted changes to tracked files, a branch with commits
              of its own, a branch GitHub has also moved on, a repository whose origin is not
              the same owner/name. Untracked files never hold a worktree back.
            - Projects are matched by name and then by origin owner/name, since each machine may
              reach GitHub by its own address.
            - Back online, the client's own sessions still running are listed after the hub's,
              marked as on this machine, and acted on there (`--here`).
            - Not carried across: uncommitted changes (said, never committed for the developer),
              Claude's conversations, and the two machines' histories.
            Decided by the architect on 2026-11-05 ("Going back online needs a handoff. Do 1 2 3").
Status:     accepted

## D-010  2026-11-05  Lists of repositories and projects are in name order, ignoring case

Context:    The repositories of a root came in GitHub's own order, by recent activity, which
            changes from one look to the next, and projects under a heading were in the order they
            were registered. Neither lets a name be found where it is expected.
Options:    (a) Alphabetical by name, ignoring case, everywhere a list of repositories or projects
                is shown: browsing a root, the app's repositories, the project list under each
                heading, and the projects a handoff goes through [CHOSEN]
            (b) Registration order inside a heading - rejected: it is an order only the developer's
                history explains.
            (c) Alphabetical minding case - rejected: capitalised names would be listed first,
                away from their neighbours.
Decision:   - One order, `roots.NameLess`: lower-cased names compared, and names that differ only
              in case kept in a fixed order so a list never shuffles between two reads.
            - A client sorts what the hub sends as well, and the app sorts inside each category,
              so a hub not yet updated is listed the same way.
            - Headings keep their own rule (alphabetical, ignoring case, none last).
            Decided by the architect on 2026-11-05 ("the repository lists from Git should be in
            alphabetical order. Case insensitive").
Status:     accepted

## D-011  2026-11-05  The app runs hubs with the PATH the developer's login shell has

Context:    macOS starts an app with a bare PATH (/usr/bin:/bin:/usr/sbin:/sbin), or with the PATH
            of whatever launched it. hubs, run by the app, looks commands up on that PATH, so the
            editor's command (`code`, linked into /usr/local/bin) was not found from the app though
            it is found in a terminal, and the same would hold for zellij, gh and ssh helpers
            installed by Homebrew.
Options:    (a) Read the login shell's PATH once at start (login and interactive, as a terminal
                does, with a time limit), and add the usual install folders it lacks [CHOSEN]
            (b) A fixed list of folders only - rejected: misses whatever the developer's own
                profile adds, and differs from what the terminal finds.
            (c) A PATH setting in the program's configuration - rejected: one more thing to keep in
                step with the shell by hand.
Decision:   - The app runs its login shell once (`$SHELL -ilc`), reads PATH between markers so
              anything the profile prints is ignored, and gives up after 3 seconds; then adds
              ~/.local/bin, /opt/homebrew/bin, /usr/local/bin and /etc/paths where missing. With
              no answer from the shell, its own PATH and those folders are used.
            - Every hubs the app runs gets that environment.
            - Separately, a run whose plan cannot be made is answered as a run: a done event that
              carries the reason, so the app says why nothing ran.
            Proposed on 2026-11-05 after opening VS Code from the app failed on the hub.
Status:     proposed

## D-012  2026-11-05  Ghostty holds every joined session as a tab of one window; the panel is solid and gets out of the way

Context:    Each join from the app started another copy of Ghostty (`open -na ... -e`), so four or
            five sessions meant four or five Ghostty applications. The panel stayed open over the
            joined window, and drew as a see-through, dimmed, inactive window.
Options:    (a) Ghostty's own scripting: one window holds the sessions, a tab each [CHOSEN]
            (b) A window per session in the running Ghostty - rejected by the architect: one
                window with every session is easier to use.
            (c) Keep a copy of Ghostty per join - rejected: one application per session.
Decision:   - Joining in Ghostty runs an AppleScript through osascript, shown with the plan like
              any command. Each tab hubs opens is titled "hubs: <session>"; the window holding
              such a tab is the hubs window. Joining a session whose tab is open selects that tab;
              otherwise a new tab goes into the hubs window, or a new window when there is none.
              The tab runs the shell-quoted `hubs join ...` line, which Ghostty runs through its
              login shell, with this program's PATH passed in: a tab starts with the running
              Ghostty's environment, whose bare PATH lacks zellij and ssh's folders.
            - The first join asks macOS whether hubs may control Ghostty; a refusal is said with
              where to allow it. Ghostty's macos-applescript setting must stay on.
            - The other terminals are unchanged: a new window each.
            - The panel has a solid window background, is brought to the front when it opens, and
              closes when an action that opens another application's window starts (Join, VS
              Code, a start that joins). A success returns it to the list; a failure stays on
              the action page for the next time it opens.
            Decided by the architect on 2026-11-05 ("I prefer one ghosty terminal that has all
            sessions in it"; "when I click join yes, the panel should close"; "I prefer it was a
            solid colour, like a real application").
Status:     accepted

## D-013  2026-11-06  The developer's settings decide; ports are reserved per project and lent to worktrees as slots; a client forwards running sessions' ports

Context:    D-005 and D-006 put a project's ports and scripts in a `.hub/` folder committed to its
            repository. That cannot work for a repository the developer works in but does not
            own, and nothing stopped two projects being given the same port: a list of ports kept
            by hand cannot check itself. Several worktrees
            of one project each need their own ports and data, and a client needs the ports of
            the hub's sessions whether the developer joined a session or works from the Claude
            app.
Options:    (a) The developer's own settings folder decides, a repository's `.hub/` recommends;
                the program reserves port ranges per project and lends each worktree a slot of
                them; a client forwards the ports of every running session [CHOSEN]
            (b) Only a repository's committed `.hub/` - rejected by the architect: "We can't
                have a hub file committed to a repo I don't own".
            (c) Forward ports only while a session is joined - rejected by the architect: a
                session is often used only from the Claude app; "if the ports exist while there
                is a session then that is good".
            (d) Check the hub's sessions for forwarding every few seconds - rejected by the
                architect as "serious overkill".
Decision:   - Settings: one folder outside every repository (`hubs settings where <project>`),
              keyed by the project's GitHub owner and name, holds the project's hub.toml and its
              scripts. The developer may keep it as a private git repository so it is the same on
              every machine. A repository's `.hub/` holds the same files as recommendations: the
              developer's settings win section by section and script by script.
            - Reservations: each project reserves port ranges. A clash between projects, or a slot
              port outside its own reservation, is reported with both sides named
              (`hubs ports check`). New blocks come from 20000-29999, 100 ports per project, ten
              slots of ten (`hubs ports reserve`). Existing numbers are kept and reserved as they
              are; nothing is ever renumbered by the program.
            - Slots: explicit rows, slot 1 the main
              clone with the numbers it always had, a slot belonging to a worktree folder and
              never a branch; claims in the clone's shared .git folder as
              {"assignments": {path: slot}}, safe under two claims at once, dropped when git no
              longer lists the worktree. New work claims a slot as a shown command, clean up
              gives it back. A claimed slot's record (JSON), the same values as an environment
              file, and each template the settings name are written into the worktree. Names
              only, never a password (D-005 stands).
            - Scripts must be safe to run again. That is the script's job, not the program's,
              and the guide (docs/usage.md), the run-stack skill and every message about a
              missing script say so (the architect: "that will be up to the script itself. not our app. but the
              instructions and readmes should indicate this well").
            - Starting the stack is a full build and run of one worktree on its slot's ports; the
              agent may run it itself (`hubs up`).
            - Forwarding: on a client, `hubs forward` keeps one SSH connection to the hub and adds
              or cancels a forward on it for each port of each running session, the same number
              on both sides. It checks when asked and, with `--watch`, once a minute. Each change
              is a command in the history. A port the client already uses is named with what
              holds it and left alone. It is off until the developer turns it on.
            - Amends D-005 (where the table lives, and that the program reserves ranges) and
              D-006 (where scripts live).
            Decided by the architect on 2026-11-05 and 2026-11-06 ("i think the hubs project should
            reserve ranges per repo that can then be delegated to worktrees"; "10 slots is
            plenty. port suggestion is fine"; "a hubs file in the repo can set recommendations,
            but our custom one in a private repo is the controller").
Status:     accepted. The forwarder's cadence was settled on 2026-11-07: while forwarding is on,
            the app checks the hub's sessions once a minute over the connection the forwards are
            already on (`hubs forward --check`), and does nothing when that connection is gone; it
            opens the connection again at launch, on wake, when its panel opens and after an action
            it runs. Nothing else is read while the panel is closed (amended 2026-11-09 by D-023:
            the app keeps one feed of the sessions open, which carries nothing until they change).
            The architect: "[do not do]
            anything unless I open the panel ... Especially on the client"; "Forwarding should be
            either an on off switch or session based. When a session is running on forwarding".
            Amended the same day: forwarding is chosen per session (`hubs forward add|remove`; the
            app's Forward switch on each session, and opening one of its links), with forwarding
            every session still available; the forwarder follows the hub's session list, which the
            hub sends only on a change, over the forwards' own connection, rather than checking
            each minute; with nothing to forward the connection is closed. The architect: "I
            manually enable forwarding on a worktree when I need it. Or by clicking the hyperlink
            you enable on demand"; "a switch per worktree session on or off"; "Minimal. Useful.
            Battery mindful ... one click".
            Amended again the same day: only a running session's ports are forwarded, so a running
            stack belongs to a running session. Starting a checkout's stack (Run) when its session
            is not running starts the session first, in the background, as the app starts sessions;
            stopping a stack never starts one (WORK-STACK-005). A Forward switch turned on for a
            checkout with no session takes effect when its session starts. The architect: "the
            session must exist ... the run button should start a session if needed".

## D-014  2026-11-06  Your settings are a private repository; secrets in it are encrypted by git-crypt
Context:    D-013 made the developer's settings folder the controller, to be kept in a private
            repository so every machine has the same settings. Some of what a project needs to run
            is secret (a local settings file with keys, an environment file), and a hosted git
            repository is not a place for those in the clear, even a private one.
Decision:   - `hubs settings init [<repository>]` makes the settings folder a git repository, sets
              up git-crypt in it, writes a .gitattributes that encrypts every file in any
              `secrets/` folder, commits, and pushes to the given private repository. Run again,
              it does only what is missing.
            - A project's secrets go in its settings folder under `secrets/`. On a machine holding
              the key they are plain files, read and edited like any other; in the repository, on
              GitHub and on a machine without the key they are encrypted bytes.
            - The key is kept by the developer where they keep secrets (`git-crypt export-key`);
              `hubs settings clone <repository> <key file>` brings the settings to another machine
              and unlocks them.
            - `hubs settings sync` commits this machine's changes, takes the other machines' and
              sends this one's, as a plan of git commands in the history. It refuses, before
              anything runs, when a file in a secrets folder would be committed unencrypted.
            - The app syncs on Sync now, and when its panel is opened at most once an hour; never
              on every change, and never while the panel is closed (the architect, 2026-11-07:
              "[do not do] anything unless I open the panel").
            - `hubs settings status` says where the settings are, where they sync to, what is not
              yet sent, and whether the secrets are readable here.
            Decided by the architect on 2026-11-05 and 2026-11-06 ("a git crypt is a better organic
            option for an app anyone can use. Careful not to build around my specific"; "a manual
            sync button or hourly").
            - A project names its secret files in hub.toml, `secrets = [...]`: paths in the
              worktree, or `~/...` for one in the home folder every checkout shares. `hubs secrets
              save <project>` copies them from a checkout into the settings' secrets/ (worktree
              files under secrets/worktree/, home ones under secrets/home/); `hubs secrets restore`
              puts them into a checkout that lacks them and never overwrites one it has, naming a
              file that differs instead; `hubs secrets status` compares the two. New work restores
              them after claiming its slot and before its create script.
Status:     accepted

## D-015  2026-11-07  A session is running or it is not there: stop ends it fully, an ended one starts fresh
Context:    Zellij keeps a snapshot of a session that is quit or killed, listed as EXITED, and
            "attach" rebuilds it from that snapshot: the old tabs, panes and layout. The program
            offered that as Bring back, so a session quit after the layout changed came back with
            its old four panes. Claude Code keeps its own conversation, so the snapshot holds
            nothing the work needs.
Decision:   - Stop ends a session fully: `hubs session end`, which runs `zellij kill-session` and
              then `zellij delete-session` as the session's state needs, and returns once Zellij no
              longer lists it (Zellij's own `delete-session --force` reports a failure when the stop
              leaves no snapshot behind).
            - Joining or starting a session that ended anyway (quit from inside Zellij, or the hub
              restarted) deletes the snapshot and starts it fresh from the current standard layout
              in its checkout; the agent runs `claude --continue`, so the conversation carries on.
            - Restart (app and API) is stop then start in one, for a running session on an old
              layout.
            - The app lists running sessions only; the terminal interface still marks an ended one
              as ended, and Enter on it starts it fresh. Bring back as it was is not offered.
            - Closing the terminal window leaves a session running; Join returns to it.
            Decided by the architect on 2026-11-07 ("why would we bring it back"; "i should never
            have to do this from a terminal myself"; "restart is good").
Status:     accepted. Supersedes the restore-the-layout half of SESS-BACK-001.

## D-016  2026-11-07  Each repository has a page; links come from settings; a client records why a port is not forwarded
Context:    The app listed repositories with a Start button each, and nothing showed a project's
            worktrees, ports, scripts or secrets, or the addresses a running stack serves; a
            terminal reached over SSH cannot copy them out. Forwarding ran in the background with
            nowhere to say why a port was not forwarded.
Decision:   - `hubs api project <name>` is one project's page, read where its checkouts are (the
              hub, for a client): every checkout with its branch, session state, uncommitted
              changes, slot, ports and links, and the project's scripts with their source, its
              reservation and its secret files. Every action on it is an existing action, named by
              the session a checkout has or would have.
            - The app lists repositories as Pinned, Registered by category, Here and not
              registered, and On GitHub and not here; a registered project opens its page.
            - A project's settings name the links its stack serves, `[links]` templates over the
              slot's values; without them each port is http://localhost:<port>. Sessions carry them.
            - A project can be pinned and given a category (`hubs project set`), and unregistered,
              from its page, as plans run where the project is registered.
            - The forwarder records each pass (`hubs forward status`, `--json`): what the hub had
              running, what was forwarded, and why a port was not.
            Decided by the architect on 2026-11-06 and 2026-11-07 ("when i click it ... all actions
            and things we can do are clear and obvious"; "a pinned section. a registered section.
            un registered section"; "hyperlinks in the hub menu to click on any api or urls it
            exposes").
Status:     accepted

## D-017  2026-11-07  Working local takes all of the work over: every branch, partial work, a mirror of what is missing
Context:    D-009 handed work over by pushing only the branches checked out in a worktree, said
            uncommitted changes and carried nothing else, and a client offline could not work on a
            project it had never cloned. The architect asked for clearer roles: the client "works
            local", the hub releases, and the last machine in use has the work.
Decision:   - A handover pushes every branch the giving machine has committed that GitHub does
              not, in a worktree or not (from the main clone when in none). Amends D-009.
            - Uncommitted changes are named, and committing them as partial work is offered as a
              separate action (`commit-partial`, run on the machine that has them); nothing in
              the handover itself commits.
            - A project the receiving machine lacks is mirrored: cloned to the same place under its
              home, registered under the same name and category, with the giver's worktrees whose
              branches are on GitHub, a slot each when the project has ports, and its secret files
              from the developer's settings (D-014). A project outside the home folder is named,
              not copied.
            - On the hub, Release pushes every branch and names uncommitted work; it records when,
              shown as information only.
            - A client that can reach the hub keeps itself ready (`hubs mirror`) when the app's
              panel is opened, at most once an hour:
              mirrors what it lacks and fetches the rest; it pushes nothing and moves no worktree.
            - The app calls the client's switch "Work local" and its way back "Hand back to the
              hub".
            Decided by the architect on 2026-11-05 and 2026-11-06 ("on the server i say offline,
            meaning make all the local code available in git ... it should push all branches, and
            any uncommitted work should be warned. i should open a session and finnish the work or
            commit it as partial work"; "on the client it should say work local"; "Last one in
            use is the controller"; "like my project folder run a mirror model"; "it shuold be a
            regular potentially background operation").
Status:     accepted. Amended 2026-11-09: the first bullet no longer holds. Pushing every branch
            would have published backups, experiments, another session's branches and a release
            branch whose push starts a deployment. A handover (and Release) pushes only the
            branch each checkout of a registered project has out, its main clone and its
            worktrees, where it has commits GitHub lacks; any other such branch is named with
            the command to push it. A release branch (release/... or release-...) is never
            pushed by a handover, even checked out. Options weighed: (a) only checked-out work
            [CHOSEN, the architect: "it will only push things registered ... prob registered
            jobs is safer"]; (b) checked-out work plus branches GitHub already has - rejected:
            it still pushes branches no one is working on; (c) the developer picks projects for
            each switch - rejected: riskier in a pinch, when something would be forgotten.
            Amended again 2026-11-09: the second bullet no longer holds. Uncommitted work (changed
            files and new files git does not ignore, without engineroom's slot files) travels
            with the handover as a stash, sent straight between the two machines' repositories
            over SSH under refs/engineroom/handover/<branch> and applied in the receiving
            checkout of the same branch (or the one at the same place, switched to it first),
            only where it lands as it was made: same branch, same commit, no changes of its own
            there; otherwise it stays, said. The giving machine keeps a copy in its stash list.
            Ignored files do not travel. A release, which has no other machine, still names the
            work and offers partial work. Options weighed: (a) a stash between the machines
            [CHOSEN, the architect: "any work should be stashed so the other system can pull,
            branch to correct branch and unstash"]; (b) a stash pushed through GitHub - rejected:
            it publishes work in progress, visible to anyone who can read a public repository;
            (c) committing partial work on the branch - rejected: it rewrites the branch's
            history with commits nobody meant to make; (d) copying modified ignored files over
            SSH as well - rejected by the architect: large and generated files have no place in
            a handover, and "anything that needs to travel should be in the secrets repo".
            Amended again 2026-11-09: a handover puts the receiving machine in the giving
            machine's state, and is all or nothing. The architect: "the handover should do a
            pre-run first that verifies everything can be done, and only do the real handover if
            it can"; "local work could be stashed using the branch name and a stash, and then it
            should switch to the same branch as the other machine and unstash. we want identical
            state. that is what handover means". So: (1) planning finds everything that would
            stop it (a branch moved on both sides, a release branch with commits GitHub lacks, a
            receiving branch with commits of its own, a detached commit, a branch checked out
            elsewhere there, two repositories under one name, no place for a missing project) and
            then plans no commands, and no switch; (2) its first commands check each machine's
            checkouts against a fingerprint of their state as read and dry-run every push; (3)
            each receiving checkout sets its own uncommitted work aside in its stash list,
            labelled with its branch, and is put on the giving branch at the giving commit with
            git switch -C (safe: that branch there has nothing GitHub lacks); a missing worktree
            is made; (4) the giving checkout's uncommitted work is carried as before, applied only
            on that commit in a checkout with nothing of its own. Finder's .DS_Store files and
            the .claude folder are the machine's own, never carried or set aside: carried, they
            moved a running session's agent memory off its machine. A machine out of reach still
            lets the switch happen with what can be done (CONN-HANDOFF-005), since working local
            without the engine is the reason the switch exists. Options weighed: (a) all or
            nothing, identical state [CHOSEN, the architect]; (b) carry what can land and say
            the rest - rejected: it left the two machines in different states without the
            developer noticing ("i dont know if a stash occurred"); (c) refuse a receiving
            checkout with changes of its own - rejected: setting them aside, labelled, loses
            nothing and reaches the state the handover means.
            Amended again 2026-11-09: a giving branch behind GitHub, where the receiving
            checkout already has GitHub's newer commits (counted from what each machine last
            fetched, or the receiving branch at GitHub's head when the giving machine never
            fetched them), blocks the handover rather than moving the receiving checkout back:
            the newer work is the work to hand over (the architect agreed: "yes. do those").

## D-018  2026-11-07  Ask Claude opens a session that knows what was open; projects name their own tasks
Context:    The app's buttons cover the common actions, but changing a project's scripts, settings or
            secrets, or doing something particular to one project, still meant opening a terminal,
            finding the settings folder and explaining ports and paths to Claude.
Decision:   - Ask Claude (`hubs api run assistant <project> <folder>`, the ✨ button) starts a Claude
              session of its own beside the checkout's main one (<session>-ai), on the hub, in the
              checkout, with the settings folder added. Claude's system prompt is appended with how
              hubs wants things done (its commands; scripts safe to run twice, in the settings
              folder, reading ports from the slot; secrets never printed; destructive steps only when
              asked; the settings repository and git-crypt) and what was opened (project, checkout,
              branch, slot, ports, links, scripts, secret files, tasks). It is asked to say what it
              sees and ask what to do. The guide is part of the program, so no skill has to be
              installed into the developer's Claude configuration.
            - A project names its own tasks in hub.toml ([[task]] name, about, run); the app lists
              them on each checkout, `hubs task` runs one in a session's checkout with the slot's
              variables, and the assistant is told them.
            Decided by the architect on 2026-11-07 ("an AI button which opens a Claude session with
            context about what we opened"; "add custom scripts to run from hub ... could be a
            registered script and skill"; "the Claude setting ai helper must understand hubs AI and
            secrets and setting repo").
Status:     accepted by the architect on 2026-11-08, with the ui-redesign branch merged into main

## D-019  2026-11-07  The menu bar panel holds the sessions; a window holds everything else
Context:    The panel tried to hold everything: the sessions, the repositories, each project's page,
            the history and the settings, behind tabs and back links in a 500 by 740 popover that
            closes whenever focus leaves it. What is done many times a day (join a session, open its
            app, forward it) shared that space with what is done now and then (set up a worktree,
            edit scripts, read the history), and the occasional work was cramped and lost its place
            when the panel closed.
Decision:   - The panel from the menu bar holds the sessions only: waiting ones first, then the
              pinned projects, then the rest, each on a card with its links, its Forward switch on a
              client, Ask Claude and Join. An action started from it is shown in it.
              (Amended 2026-11-08 by D-022: one fixed order, pinned projects first; a waiting
              session is marked where it is, and has no section of its own.)
              (Amended 2026-11-09 by D-025: a list of projects and a detail of compact cards.)
            - Its gear opens a window: a sidebar of the registered projects (pinned first), the
              repositories, the history and the settings, and on the right whichever is chosen. A
              project's worktree cards sit side by side when the window is wide enough. A project's
              name on a panel card opens its page in the window. An action started from the window
              is shown in the window.
            - Both are read only while they are open (D-013's amendment), every 15 seconds while
              open, and both draw from the one store, so they always agree.
              (Amended 2026-11-09 by D-023: the hub sends the sessions when they change, over one
              connection the app keeps; nothing is read on a timer.)
            - Every screen is drawn in one visual language (macos/Sources/HubsMenu/Style.swift), and
              `hubs-menu --snapshot <dir> [dark]` draws them from invented sample data, so how the
              app looks can be checked after every change.
            Decided by the architect on 2026-11-07 ("it might be nicer if the dropdown was for the
            sessions and everything else could be managed by a fuller form that can be opened by a
            gear"; "designed. Minimal. Useful. Battery mindful. Clean. Easy. Efficient. When I want
            to do a thing. I should be able to do it with one click").
Status:     accepted by the architect on 2026-11-08, with the ui-redesign branch merged into main

## D-020  2026-11-07  What runs for a session lives only as long as the session
Context:    A checkout's stack is started on request through its up script (D-006, D-013), and a
            stack needs its session running to be reached from a client (WORK-STACK-005). Ending a
            session left its stack running: on the hub, a server and a web dev server started from
            inside a session ran for three days after it had ended, and two stacks started by up
            ran with no session at all. Nothing tied a process to the session it was started for,
            so nothing stopped it when the session went, and the app showed the project as not
            running while its ports were taken.
Decision:   - What is started for a checkout's session belongs to that session and lives only as
              long as it does: ending the session fully ends it too. Leaving a session ends
              nothing: closing the terminal tab or window, detaching, or a client sleeping or
              losing its connection leaves the session and everything it runs as they were.
            - Started for a session means what the project's up script started, which its down
              stops, and any process still running whose environment names the session:
              HUB_SESSION, which hubs sets on everything it runs for a checkout (up, down, tasks);
              where that is absent, the session of the checkout named by HUB_WORKTREE, for what
              hubs ran before HUB_SESSION existed; else ZELLIJ_SESSION_NAME, which Zellij sets on
              everything started inside a session. HUB_SESSION wins where they differ, so a stack
              started for one checkout from inside another session belongs to its own checkout's
              session. Of those, only servers are stopped: each process that listens on a TCP
              port, with the processes of the same session that started it and those it started.
              An editor or app opened from inside a session carries its name too, listens on
              nothing, and is the developer's to close (found when VS Code, opened from a session,
              named it). A process is never matched by its name.
            - Stop and clean up run the project's down while the session is there, end the
              session, then stop whatever still runs in its name (`hubs session reap <name>`,
              which says what it stopped, and refuses while the session runs). Handing work over
              to a client (D-017) does not end hub sessions, so it does not stop their stacks
              either.
            - Restart keeps the stack: the same session comes back on the current layout.
            - A session that ends without hubs (quit from inside Zellij, killed by hand, or crashed)
              is found by the sweep, which the menu bar app runs on the machine whose sessions
              they are, and only there and then: on the hub from when the app starts, and on a
              client only while it works local. It is never a job of its own: the app starts one
              `hubs sweep --watch`, which sweeps every minute in its own loop and ends with the app.
              (Amended 2026-11-08, replacing a launchd job that ran every minute on every machine:
              the architect, "this should be enabled in the server hub when it starts. and on local
              only enabled when needed when running local. and not a seperate thing that runs all
              the time".) It reads that machine's Zellij sessions and processes and nothing else,
              never the network. A checkout whose session has been gone for two minutes has what
              was started for it stopped, as Stop would; what Zellij keeps of an ended session is
              deleted (D-015). Each is an entry in the history. `hubs sweep --dry-run` shows what
              it would do; `hubs sweep --uninstall` removes the launchd job an earlier build
              installed.
            - A process on a project's ports that belongs to no session (started by hand outside a
              session, or before this decision) is never stopped for the developer: the project's
              page lists it with its port, pid, start time and folder, and offers to stop that one.
            - Claude in a session runs the app through `hubs up` and `hubs down` (the run-stack
              skill), so what it starts belongs to the session. A build handed to the developer to
              try is `hubs down` then `hubs up`, so the app running is the build handed over.
            - The repository's rule on stopping reads: it stops a session or a process only when
              the developer has chosen that exact one, or ended the session it was started for.
            Decided by the architect on 2026-11-07 ("anything run for the session like starting
            the app stack, should be tied to the session. shutting down the session totally vs
            disconecting should shut down everything related"; "stopping the exact one meant the
            button closes the software.. but the point was only within the lifetime of the
            session"; "claude itself from in the session should run up and down between builds
            when delivering testing"; the sweep "run by hubs on the server ... no internet every
            minute").
Status:     accepted (WORK-STACK-006 to 013; merged into main with ui-redesign on 2026-11-08)

## D-021  2026-11-08  A join whose connection drops joins the same session again
Context:    A client's join is an SSH connection to the hub, and it ends whenever the client
            sleeps, wakes on another network or moves, while its session runs on, untouched, on
            the hub (D-020). The window then ended as though the session had, so the developer had
            to join it again by hand after every lid close. The architect, 2026-11-08: "the laptop
            maybe opened and closed frequently saving power moving etc with the point to retain
            easy access".
Decision:   - A join by name of a hub session (`hubs join`, as the app opens it, or enter on the
              session list) that had been attached and then ended with ssh's own failure status
              (255) is a dropped connection. The window says the connection was lost and that the
              session keeps running, asks the hub for its sessions every 3 seconds, showing the
              command it asks with and why the hub did not answer, and joins the session again as
              soon as the hub answers. Each join again is an action in the history like any join.
            - Only the same session is joined again: one still running and older than the join
              that dropped. A session stopped, or restarted (a new session under the same name),
              while the connection was down ends the join as before (SESS-BACK-004), and nothing is
              started.
            - The developer stops the waiting with q, which joins nothing.
Rejected:   - Keeping the connection alive through sleep (long keep-alive limits, or a mobile shell
              such as mosh): a long limit keeps a dead connection open on a real network fault and
              still loses it on a long sleep or a network change, and a mobile shell is another
              program on both machines and another port through the private network, for what
              Zellij already gives: joining again redraws the whole session.
            - Rejoining from the app on wake: the window already knows what it was joined to and
              can wait for the hub itself, without the app.
Status:     accepted (CONN-REJOIN-001 to 004)

## D-022  2026-11-08  The panel keeps one order, pinned first; waiting is a mark, not a place
Context:    The panel listed sessions waiting for the developer first, under a heading of their own,
            then the pinned projects, then the rest (D-019). A session moved between sections each
            time Claude started or stopped waiting, so nothing stayed where the developer last saw
            it, and a waiting session of a pinned project left its pinned place. The architect,
            2026-11-08: "i dont like the order of information changing based on claude is waiting.
            i think pinned ones are always there. always at the top, and always maintain a
            consistent order so its not random"; "dont have a waiting for you section that
            duplicates everything. just make that a hint on the line".
Decision:   - The panel lists, in this order and never another: the pinned projects by name, each
              always there with its running sessions (main clone and worktrees together, by
              session name) or a row to start it; then the hub's other sessions by name; then Ask
              Claude sessions by name; then this Mac's own sessions started while it was offline.
              The app lays this out itself (PanelList), whatever order the hub lists them in.
            - A waiting session is marked on its own row (the orange dot, "waiting for you" and
              Claude's question), and is not moved or listed twice.
            - Each card carries a GitHub line for its project (code, issues, pull requests) and,
              for a worktree whose branch GitHub has, that branch and the pull requests from it
              (APP-LIST-003). The repository's links come with the repositories list, read hourly
              or when asked; a session's branch links come with the sessions, from two git commands
              per project with a session, read from the clone with no request to GitHub.
            - The terminal's session list (and `hubs sessions`) has the same order, laid out by the
              hub (ops.Order): pinned projects' sessions, then the rest, then Ask Claude, each by
              name; an ended session keeps its place too, marked ended. A waiting session is marked
              with Claude's mark (✻) beside its name and its question below it, on the panel in
              place of the state dot (the architect, 2026-11-08: "i want my list in fixed order. if
              something is waiting on claude i dont need it in a different order. that can just be
              an icon or something like a claude icon showing").
            - Only the app asks for the sessions' GitHub links (`hubs sessions --json --github`);
              the forwarder, which asks every 3 seconds, and the terminal list read none.
Status:     accepted (APP-LIST-001 to 003, SESS-LIST-002, SESS-LIST-005)

## D-023  2026-11-09  The hub sends the app its sessions when they change; the app reads nothing on a timer
Context:    The app read the sessions every 15 seconds while its panel or window was open (D-019),
            each read a new SSH connection to the hub on a client, and the window kept reading for
            as long as it was open, even behind other windows. With the panel closed it knew
            nothing, so the menu bar could not say Claude was waiting. The architect, 2026-11-09:
            "a server tracking everything low cost can easily send batches of data"; "I'm not sure
            if the panel needs to check local things every 15 seconds. Not much will change other
            than by an action direct"; on battery, "if the battery consumption is negligible then
            don't overact on my concern".
Options:    (a) Keep reading on a timer while the panel is open - rejected: a new connection to the
                hub for every look, the panel shows what was true up to 15 seconds ago, and nothing
                is known while it is closed.
            (b) The hub sends a list only when it changed, over one connection the app keeps for as
                long as it runs [CHOSEN]: the hub, which is plugged in, does the looking; an idle
                connection costs the client next to nothing.
            (c) The same, but connected only while the panel is open - rejected: it keeps the
                panel's freshness but not the menu bar's mark, for a saving too small to notice.
Decision:   - `hubs api feed` sends the sessions as one JSON line, the reply `hubs api sessions`
              gives, now and again each time anything in it changes (a session's age, which Zellij
              gives relative to now, does not count), and nothing in between. Where the sessions
              are on the machine it runs on (the hub, or a client working local) it looks every
              3 seconds; each project's GitHub links are read again at most every 30 seconds. On a
              client that is online it runs the hub's feed over one SSH connection and adds this
              machine's own sessions to each list.
            - Any line written to it asks it to look now: the app does that after an action, so
              what the action changed shows at once. Closing its input ends it, and the hub's with
              it, so nothing is left looking on the hub for an app that has gone. After a quiet
              minute it sends an empty line, so a connection that died without closing is found.
            - The app keeps the feed open while it runs. When it ends, the app starts it again two
              seconds later, or, when it ended at once (the hub not answering, or too old to have a
              feed), after 5, 15, 45 seconds and so on up to five minutes; opening the panel, a
              wake and the refresh button try at once. While there is no feed the sessions are read
              as before, when the panel opens and after an action.
            - The app reads nothing on a timer. Its own state (forwarding, history, settings) is
              read when the panel or window opens, after an action, on wake and on refresh. The
              open project's page is read again when the feed's sessions change.
            - The menu bar icon carries Claude's mark (✻) while Claude is waiting in any session,
              panel open or not.
            - Anything that could lose work checks at the moment it is asked for, never from what
              the feed last said.
Status:     accepted (APP-FEED-001 to 005). Amends D-013's "nothing else is read while the panel is
            closed" and D-019's "every 15 seconds while open".

## D-024  2026-11-09  A worktree is for a branch or for a role; closing or moving on never loses work
Context:    Every worktree was named for its branch (<repo>-<branch>) and closed when the branch was
            done. Work is often batched instead: standing worktrees such as a designer's, a
            tester's and a developer's, each holding one branch after another. Clean up refused
            work that would be lost but offered no way through. The architect, 2026-11-09: "When I
            add a worktree I would prefer to be pick role or branch specific. Meaning I will delete
            after it's branch is done"; "Either branch or role. Based and recorded so the rule can
            value both"; "Close worktree should have some kind of verify script that no code would
            get lost. Ie rollback or commit to a branch".
Options:    (a) Worktrees for branches only - rejected: batching work in standing worktrees is how
                the work is done.
            (b) A role list declared ahead in the project's hub.toml - rejected: the kind is chosen
                when a worktree is made, and hubs would rewrite a hand-written file to record it.
            (c) The kind chosen when the worktree is made; a role recorded by hubs in a file of its
                own in the developer's settings [CHOSEN].
Decision:   - New work is either branch work, as before (`new <project> <branch>`: folder
              <repo>-<branch>, closed when done), or a role (`new-role <project> <role> <branch>`:
              folder <repo>-<role>, on the branch given: a new one from the remote's main, or the
              branch as it is here or on the remote). The plan records the role with
              `hubs roles add`, in roles.json in the project's settings folder, so every machine
              that syncs the settings knows it; `hubs roles <project>` lists them.
            - A name that is already branch work is refused as a role. The project's page marks a
              role worktree with its role, and the branch it holds now.
            - `next-branch <project> <role> <branch>` moves a role worktree on to its next branch
              in the same folder, keeping its session name and its slot of ports.
            - Clean up and moving on refuse, before anything runs, when work would be lost
              (uncommitted changes, or commits on no remote), and now offer what makes it safe:
              save the changes to the branch and push (`save-work`), put them aside as a stash of
              the repository, which outlives the worktree (`put-aside`), or push the commits
              (`push-work`). Each offer is a plan of its own, shown before it runs. Throwing work
              away is never offered.
            - Cleaning up a role worktree forgets the role (`hubs roles remove`).
Status:     accepted (WORK-CLEAN-008 to 011, WORK-ROLE-001 to 005). The app's New worktree choice,
            the role card and Next branch, and a skill so Claude in any session makes and removes
            worktrees through hubs, follow.

## D-025  2026-11-09  The panel is a list of projects and a detail of compact cards, fed with each checkout's state
Context:    The panel listed a full card per session, one under another, so with several projects
            running it had to be scrolled, and a stopped worktree was not there at all. The
            architect, 2026-11-08: "my panel is no longer quick and directive because I've got to
            scroll to get to things. I think there's too much information in a vertical layout";
            "a very small clean list of my pinned or running items ... with a little bit bigger text
            ... running lights ... if you click on it or mouse over it, you would see the panel based
            information on the right hand side ... the card view is small, tight and compact";
            2026-11-09: "each worktree card needs join or restart available", and to know what
            would be lost before closing.
Options:    (a) Keep the list of cards and trim them - rejected: it still grows with every
                session and still scrolls.
            (b) A list of projects and a detail for the one pointed at [CHOSEN]: the list stays
                short; the detail holds everything about one project at once.
            (c) A row per session in the list - rejected: a project with worktrees fills the list,
                and the project is what the developer thinks in.
Decision:   - The panel is 680 by 460. On the left, one row per project in the order of D-022
              (pinned first, each always there; then every project with a running session; each
              by name), larger text, one light for all its sessions: Claude's mark when any waits,
              green when one runs, grey when none does, and a count when more than one runs. This
              Mac's own sessions started while offline follow, with Join.
            - Pointing at a row, or clicking it, shows that project on the right and keeps it while
              the pointer moves across. The right is its name (opening its page), its state, its
              GitHub line, a compact card per checkout (main clone first, then worktrees by name,
              running or not), and its Ask Claude sessions as one line each.
            - A card: its light; the main clone by branch, a role worktree by role with the branch
              it holds, any other by name; chips for unsaved files, commits not pushed, commits
              behind main and a merged branch ("merged: close it", or "move on" for a role); VS
              Code, Ask Claude and the branch on GitHub as icons; Join with the three restarts in
              its menu, or Start; Claude's question; links and Forward; a ... menu with the page,
              Stop, copy the branch name, and Close (and Next branch on the page) for a worktree.
            - The feed (D-023) carries each checkout of the pinned projects and of those with a
              running session: unsaved, unpushed, behind and merged, read from git on the hub's own
              disk at most every 30 seconds and after an action. Nothing is fetched to find them, so
              behind and merged are as of the last fetch; Close always checks again when pressed.
              "Merged" means the remote's branch is in main and is not simply one of main's own
              commits, so a new branch with nothing of its own is not called done.
Status:     accepted (APP-LIST-004, APP-FEED-006). Amends D-019's panel of session cards.
            Amended the same day (the architect: "when i click the panel icon. remember the last
            one i interacted with"; "start/stop etc.. should all be visible. no... menu"): the
            panel is 840 by 480; the project last pointed at or clicked is remembered across
            opening the panel and restarting the app, and stays listed ("Last used") after its
            last session stops, so an action returns to it; every action is a button in sight
            (Join, Restart as this Mac starts sessions, Stop, or Start; Next branch for a role;
            Close for a worktree; the branch and its pull requests on GitHub and copying the
            branch name as icons), with no menus; each card shows its links and Forward whether
            or not its session runs.

## D-026  2026-11-09  The program is engineroom: one name for the repository, the app and the command
Context:    The program was zellij-claude-hub, run as hubs. Both names say less than it is: Zellij is
            one way to keep sessions (a multiplexer with its own server is planned beside it), and
            the tool is a powerful machine that does the work with light machines that reach it.
            The architect, 2026-11-09: "engineroom is nice"; "engineroom all the way for the name";
            most people will use the app more than the command line.
Options:    (a) Keep hubs as the command under a new product name - rejected: two names for one
                thing, met by everyone who goes from the app to the command line.
            (b) A short command (er) - rejected: the app is what most people use, and a developer
                who wants a short command can alias it.
            (c) engineroom everywhere [CHOSEN].
Decision:   - The repository is KyleyHarris/engineroom, the Go module github.com/KyleyHarris/engineroom,
              the command engineroom, the app engineroom.app with bundle id local.engineroom.menu,
              and the Swift modules EngineRoomKit and EngineRoomMenu.
            - The program's folders are ~/.config/engineroom and ~/.local/state/engineroom (XDG
              roots as before); the first command a new build runs moves the old
              zellij-claude-hub folders there once (paths.Migrate), never over a folder that
              already exists. Its environment variables are ENGINEROOM_*.
            - "hub" stays the word for the machine that does the work: the hub role, a repository's
              .hub folder and hub.toml, the HUB_* variables its scripts get, and the slot files
              (.hubs-slot.json, hubs-slots.json) keep their names, since they live in other
              repositories and the developer's settings.
            - The old launchd sweep job keeps its old label (local.zellij-claude-hub.sweep): it is
              only ever removed.
            - install.sh keeps hubs as a link to engineroom until every machine runs engineroom,
              so an old build, a hook or a habit that calls hubs keeps working meanwhile.
Status:     accepted. Amended the same day: every machine runs engineroom, so install.sh no
            longer makes the hubs link, and the link was removed from each machine.
            Amended the same day (the architect: "Engine is better than hub. The application
            also gets this wrong"): the third bullet no longer holds. A name says what it names:
            the machine that does the work is the engine, and the program's own things are
            engineroom. The role is engine (role = "engine", [engine], --engine, "engine" in
            what `engineroom api` answers, engineDown when it does not answer, handoff to-engine
            and from-engine); a repository's folder is .engineroom with engineroom.toml, as is
            the project's file in the settings; the slot files are .engineroom-slot.json and
            .engineroom-slot.env, and the registry .git/engineroom-slots.json; the variables a
            project's scripts get are ENGINEROOM_*, named by kind (D-028). The app and the
            command line read only these names.

## D-027  2026-11-09  The website is built from the docs; only the story pages are written by hand
Context:    A website in the repository is to explain the program, show how it works and how to
            use it, "like a dynamic readme and documentation tool", and to look good while doing
            it. Pages written by hand beside the docs hold every fact twice, and the two copies
            drift. The product is also to be renamed, and the tools it joins together (the
            terminal, the Git host, the agent) may change.
Options:    (a) Pages generated from docs/*.md by a small Go program in this repository, with the
                pages that tell the story written by hand [CHOSEN]
            (b) Every page written by hand - rejected: two copies of every fact, which drift.
            (c) A site generator from the JavaScript ecosystem - rejected: a toolchain and a
                build step this repository does not otherwise need, for what a small Go program
                does.
Decision:   - `tools/site` renders docs/setup.md, docs/usage.md and docs/decisions.md into
              pages, one per heading down to a level set for each doc, with the heading ids
              GitHub gives, so a link written for GitHub lands in the same place on the site.
              A link to any other file in the repository goes to that file on GitHub, and a
              decision named in running text links to its page. The one new dependency is
              goldmark, to read Markdown.
            - New facts go into the docs first and the site shows them. The pages in site/pages
              carry only the story: the front page and how it works.
            - The product's name, its command and the stack of tools it joins together are data
              in site/site.toml, never spelled in a template, a class, a file name or an
              address, so the rename and a swapped tool are edits there.
            - Plain HTML and CSS, no JavaScript build, fonts served by the site itself: it sends
              no request elsewhere and opens from disk.
            - The gate builds the site into a temporary folder and fails on a link, an anchor or
              an image that goes nowhere, and on an em or en dash. The built site is never
              committed.
            - Publishing it waits for the decision to make the repository public (D-001).
            Decided by the architect on 2026-11-09 ("i like gerating. but i do want this to be
            a nice looking site with good content"; "journey recordings sound good").
Status:     accepted. The front page and how it works, the journey recordings with their
            captions, and the glossary follow. Amended the same day: How it works has one
            hand-written script, the mechanics map, with no build and nothing fetched; the
            looks are styles over one shared stylesheet, and the dev server offers a switch
            between them so one can be chosen on the running site (site.toml names the one
            the site is built with); `go run ./tools/site -serve` and the repository's own
            .hub stack run it.
            Amended the same day: the architect chose the machine room look ("of the 3 styles.
            the engineroom is prob the best ... this is kind of like the mash unit keeping it
            all together") and the other two looks were removed with the fonts only they used.
            The switch stays in the dev server and shows itself again only when a second style
            is listed.

## D-028  2026-11-09  A slot's keys are named by kind, and a key that would overwrite another is refused
Context:    Every key of a slot (the program's own slot, worktreeRoot, isMainClone, project and
            tmp, and a project's ports, names and derived values) is one value in the slot's
            record and in a template's {{key}}, and is given to a project's scripts as an
            environment variable. With one prefix for every variable, a project's key could take
            a name the program uses for itself, and a port named project silently replaced the
            project's name in the record. The architect, 2026-11-09: "some other sort of prefix
            to prevent any overlap, i.e. adding the word port"; "make this happen".
Options:    (a) A prefix for each kind, and a project file refused when one of its keys would
                overwrite another [CHOSEN]
            (b) A prefix for each kind alone - rejected: the record and {{key}} still hold one
                value per key, so a derived value named as a port, or a port named project,
                would still overwrite it there.
            (c) Refuse only a key spelled exactly as another - rejected: "db-name" and "db_name"
                are one variable name, so one would still overwrite the other.
Decision:   - The program's own keys are ENGINEROOM_SLOT, ENGINEROOM_WORKTREEROOT,
              ENGINEROOM_ISMAINCLONE, ENGINEROOM_PROJECT and ENGINEROOM_TMP. A project's keys are
              ENGINEROOM_PORT_<KEY>, ENGINEROOM_NAME_<KEY> and ENGINEROOM_DERIVED_<KEY>: the key in
              capitals with anything not a letter or digit made an underscore
              (engineroomfile.Fold). The program never names a variable of its own with PORT_,
              NAME_ or DERIVED_ after its prefix. The record and {{key}} keep the plain keys.
            - When a project's settings are read (settings.Load), a port, name or derived value
              is refused if it folds to the same name as a built-in, or as another key of a
              different kind or spelling, after the developer's file and the repository's are
              merged, so a clash between the two is caught too. The same port in [ports] and in
              every [[slot]] row is one key. The error names the file and both keys.
Status:     accepted

## D-029  2026-11-10  Rex as an alternative to Zellij and Ghostty
Context:    Every session lives in Zellij on the engine and is joined through a terminal app
            (Ghostty by default), locally or with `ssh -t`. Rex is a terminal app and session
            server in one: its server keeps sessions running whether or not a window shows them
            (a session holds windows, a window holds blocks), its Mac app is the terminal and
            reaches other Macs' Rex servers natively (Tailscale, or an SSH-forwarded socket),
            and its `rex` CLI creates, lists, reads, types into and kills sessions, with stable
            JSON output and exit codes. It also shows each program's own status (idle, working,
            waiting on the user) from the Program Status Protocol (OSC 7501). It is in public
            testing: the Mac app, and Linux and FreeBSD servers. The architect, 2026-11-10: "start
            preparing a branch that integrates this as an option over ghostty and zellij".
Options:    (a) Rex as a session backend beside Zellij, chosen per machine, with the Rex app as
                its terminal [RECOMMENDED]: sessions created, listed and ended through `rex`,
                joined by asking the Rex app to show the session, on the engine's server.
            (b) Rex only as a terminal app, joining Zellij sessions like Ghostty does: small, but
                Rex's own sessions, its native remote connection and its status display go unused.
            (c) Replace Zellij outright: rejected for now, as Rex is in testing and not
                available to everyone.
Decision:   - Agreed by the architect, 2026-11-10: Rex's own Tailscale support is the way a
              client reaches the engine; Rex is installed on the engine and on each client, but
              only the engine's Rex server holds sessions; a session has two windows, Claude and a
              plain terminal.
            - The architect, 2026-11-10: build what can be built and keep it on its branch until
              Rex is available. The design, written from Rex's CLI and server API documentation
              before Rex has been run here, is docs/rex.md: each operation as Rex commands, how a
              client reaches the engine's Rex, errors, naming, and what is still to confirm,
              which scripts/rex-probe.sh records on a machine with Rex.
            - Built on that branch: one session backend (internal/mux) with Zellij and Rex behind
              it, chosen by `multiplexer`; ENGINEROOM_SESSION set on every session, Zellij's too,
              and read first wherever a session's name is needed; a client runs the one step that
              shows a session on its own machine (a viewer command, `engineroom rex show`) and
              every other step on the engine. Sessions are addressed by label, not by a recorded
              ID: engineroom never creates a second session under a name in use, and Rex refuses
              a label two sessions share, so recording IDs would add state for no open case.
Status:     built on the rex-option branch, kept aside until Rex is available
