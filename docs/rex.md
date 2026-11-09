# Rex as a session backend

This is the design for running engineroom's sessions in Rex instead of Zellij, with the Rex app as
the terminal instead of Ghostty (docs/decisions.md D-029). It is written from Rex's published CLI
and server API documentation alone, before Rex has been run here: every command below is as the
documentation gives it, and everything the documentation does not settle is listed under "To
confirm on a machine with Rex", with the command that confirms it (`scripts/rex-probe.sh`).

The backend is built (see "What is built"), tested against a stand-in `rex` that answers as the
documentation says. It is off unless a machine's configuration says `multiplexer = "rex"`.

## How it would work

engineroom keeps its own model: a project's checkout has one session, named as today
(`project-a`, `project-a-fix-login`). In Rex it has two windows: Claude Code, and a plain
terminal in the checkout. Rex is installed on the engine and on every client, but only the
engine's Rex server holds sessions; a client's Rex app reaches it over Tailscale. What changes is
who keeps the session and what shows it.

| Today (Zellij, Ghostty) | With Rex |
|---|---|
| The engine's Zellij server keeps every session. | The engine's Rex server keeps every session (the Rex app starts it on a Mac; `rex server start` elsewhere). |
| A session is joined by a terminal app running `zellij attach`, or `ssh -t engine zellij attach` from a client. | A session is shown by the Rex app, which is the terminal. On the engine the app switches to the session; on a client, the client's Rex app opens the engine's Rex server as a host and switches to the session there. No `ssh -t` is involved. |
| engineroom's actions on the engine run over SSH. | Unchanged: engineroom still plans and runs every action on the engine over SSH, where it calls the engine's own `rex`. Only showing a session uses Rex's own connection. |
| Waiting sessions are marked by engineroom's hook, keyed by `ZELLIJ_SESSION_NAME`. | The hook keys on `ENGINEROOM_SESSION`, which engineroom sets in every session it starts, whatever keeps it. Rex's own status display may show the same thing natively (below). |

A machine chooses its backend in its configuration (`multiplexer = "zellij"` or `"rex"`); Zellij
stays the default. The engine's choice decides where its sessions live; a client is set the same
way, with `rex.host` naming the engine's Rex server among the client's Rex hosts:

```toml
multiplexer = "rex"

[rex]
path = "/path/to/rex"   # only when rex is not on PATH
host = "engine"         # on a client: the label given to `rex hosts add`
```

## Each operation, as Rex commands

engineroom runs `rex` like any other tool: as a plan of commands shown before they run, with
`--json` wherever it reads an answer.

| engineroom | Rex | Notes |
|---|---|---|
| Is a new session ready (`Ready`) | `rex session inspect <name> --json` | Exit 0 once Rex knows the session. |
| List sessions (`List`) | `rex ls --json` | Documented shape: `{"sessions": [{"session_id": "session:...", "label": "..."}]}`. engineroom's session name is the label. Exit 4: the server could not be reached. |
| Create a checkout's session (`Create`) | `rex session new <name> --cwd <dir> --window claude --shell <login shell> --interactive --json -- env ... ENGINEROOM_SESSION=<name> sh <agent script>` then `rex window new -s <name> terminal --focus=false --cwd <dir> --shell <login shell> --interactive -- env ... ENGINEROOM_SESSION=<name> <login shell>` | The agent script is the standard layout's agent command, written beside the layout (`standard.sh` beside `standard.kdl`). The login shell's environment and startup files give the agent the PATH a terminal would. An assistant or upgrade session is the first window alone. |
| End a session (`Stop`) | `rex session kill <name>` | Rex keeps nothing of a killed session, so there is nothing to forget afterwards. |
| Join (`Join`) | `engineroom rex show <name>`, run on the machine the developer is at | On the engine: `rex do session.select session=<name>`. On a client: `rex do client.host.switch host=<rex.host>`, then `rex -S <rex.host> do session.select session=<name>`. The app must be running with its Remote Control setting on; with one app connected, Rex sends the action to it. |
| Send text, read a pane, wait for a command | `rex send`, `rex send-key`, `rex capture --trim`, `rex wait`, `rex run --wait` | Not needed by today's features; available to the assistant and tasks. |
| Follow sessions for the app's feed | `rex events --json` | Session created, removed and state changes, one JSON line each, flushed per event: a replacement for polling `zellij list-sessions`. |

The same calls exist on the server API (`rex api call <method>`): `session.list`,
`session.create` (with `label` and `initial_windows`, created all at once or not at all),
`session.new_window`, `session.destroy`, `client.list`, `client.perform`. engineroom uses the CLI
commands above; the API is the fallback where a CLI command cannot carry what is needed in one call
(creating the session with both windows atomically is `session.create` with two
`initial_windows`).

### Naming, and never acting on the wrong session

A Rex label can change and two sessions can share one; an ID never changes and is never reused.
engineroom addresses a session by its label, which is its session name, as it does with Zellij:

- before creating a session, it checks no session already has the name, as it does today, and an
  answer from `rex ls` it cannot read is an error, never an empty list;
- Rex itself refuses a label two sessions share (exit status 3) rather than picking one, and
  engineroom says so and changes nothing.

Recording each session's ID would add state to keep in step with Rex's for no case the two rules
above leave open; it is worth adding only if a real Rex shows labels being changed from inside
the app.

### Errors, as engineroom says them

| Rex exit | Meaning | engineroom says |
|---|---|---|
| 0 | done | the step ran |
| 1 | failed | the step failed, with Rex's message |
| 2 | bad command line | a defect in engineroom: reported as such |
| 3 | target not found or ambiguous | the session is gone, or the name is shared; nothing was changed |
| 4 | server unreachable | the session server is not running here, or, for a remote host, the engine did not answer |

### Finding `rex`

Rex puts `rex` on `PATH` only inside its own terminals (`REX_BIN_DIR` first). engineroom also runs
from its menu bar app and over SSH, so it looks, in order: `rex.path` in its configuration,
`REX_BIN_DIR` when set, then `rex` on `PATH`. Where the Rex app keeps its copy is not documented;
once `scripts/rex-probe.sh` has found it, it is a fourth place to look. A machine set to Rex
without a `rex` it can run says so (set `rex.path`), never a quiet fall back to Zellij.

### Running where the developer is

engineroom plans every action on the engine and a client runs the plan's commands over SSH. Showing
a session is the one step that must run on the client instead, since it drives the Rex app on the
developer's screen. That step is marked as a viewer command, and a client runs a viewer command on
its own machine only in the one shape that shows a session (`engineroom rex show <name>`); any
other command is run on the engine as before.

### Reaching the engine's Rex from a client

Rex's server listens on a Unix socket by default. A client's Rex app reaches the engine's over
**Tailscale** (agreed): the engine's server serves its API on the tailnet (`rex server tailscale
on`, or started with `--tailscale`), and each client adds it once with
`rex hosts add engine <tailnet endpoint>`. Only the engine's Rex server holds engineroom's
sessions; a client's own Rex server holds none of them. Forwarding the engine's socket over
engineroom's SSH remains possible, but is not built.

### Processes belonging to a session

engineroom decides which servers belong to a session by the `ENGINEROOM_SESSION` variable on the
process, falling back to `ZELLIJ_SESSION_NAME` (internal/procs). Every session engineroom starts
carries `ENGINEROOM_SESSION` set to its name in every window's environment, in Rex and in Zellij
alike, so stopping a session still stops exactly what it started, and the agent's Remote Control
name, the waiting mark, and `engineroom up` and `down` inside a session read the same variable
under either. Rex's own `REX_SESSION` holds an ID, not a name, so engineroom does not rely on it.

### The waiting mark

engineroom's Claude Code hook records a waiting session by name; under Rex it reads
`ENGINEROOM_SESSION`. Rex also shows a program's status (working, waiting on the user) when the
program reports it with the Program Status Protocol (OSC 7501), in its session picker and tabs.
Whether Claude Code reports it is not documented; if it does not, engineroom's hook could write the
sequence to the session's terminal so Rex shows the same mark. That is an option to weigh once Rex
is running, not part of the first build.

## What is built

1. **The session backend** (`internal/mux`): listing, creating, joining, switching, stopping and
   forgetting sessions go through one interface, with Zellij and Rex behind it, chosen by
   `multiplexer` when engineroom starts.
2. **`ENGINEROOM_SESSION` everywhere**: set on every session engineroom starts, Zellij's too, and
   read first by the waiting hook, `engineroom up`, `down` and `task` inside a session, and the
   agent's Remote Control name.
3. **The Rex backend** (`internal/mux/rex.go`) and `engineroom rex show`, tested against a stand-in
   `rex` that records what it is asked and answers in the documented shapes: the plans, their
   order, the client-side show, the error statuses.
4. **Configuration**: `multiplexer`, `rex.path`, `rex.host`; and `scripts/rex-probe.sh`.

## What is left for a machine with Rex

- Run `scripts/rex-probe.sh --session` on the engine and a client, and correct what the probe shows
  the documentation left open (below); `ShowArgv` holds every guessed argument name.
- Open the Rex app when it is not running (its bundle identifier is not documented).
- The app's settings screen: a choice of Zellij or Rex, and the engine's Rex host on a client.
- The setup guide's Rex items, and the machine restore's, once the steps are confirmed for real.
- Journeys against a real Rex, beside the Zellij ones, once a test machine can run it.

## To confirm on a machine with Rex

`scripts/rex-probe.sh` runs each read-only command below and saves the answers, so the backend can
be checked against the real thing the day Rex is installed:

- the full JSON of `rex ls`, `rex session new` and `rex session inspect` (fields beyond
  `session_id` and `label`);
- the arguments `session.select` and `client.host.switch` take (`rex client inspect --json`);
- where `rex` lives inside the Rex app, and the app's bundle identifier, to open it when it is not
  running;
- the local server's socket path (`rex server status --json`, `rex whoami --json`), for the SSH
  forward;
- whether a session created with `rex session new` survives the app quitting (the server keeps
  sessions; the app starts the server on a Mac);
- whether Claude Code reports OSC 7501, by watching `rex events --json -s <session>` while Claude
  waits.
