# Rex as a session backend (design, proposed)

This is the design for running engineroom's sessions in Rex instead of Zellij, with the Rex app as
the terminal instead of Ghostty (docs/decisions.md D-029, proposed). It is written from Rex's
published CLI and server API documentation alone, before Rex has been run here: every command
below is as the documentation gives it, and everything the documentation does not settle is listed
under "To confirm on a machine with Rex", with the command that confirms it
(`scripts/rex-probe.sh`).

## How it would work

engineroom keeps its own model: a project's checkout has one session, named as today
(`project-a`, `project-a-fix-login`), holding Claude Code in one window and git in another. What
changes is who keeps the session and what shows it.

| Today (Zellij, Ghostty) | With Rex |
|---|---|
| The engine's Zellij server keeps every session. | The engine's Rex server keeps every session (the Rex app starts it on a Mac; `rex server start` elsewhere). |
| A session is joined by a terminal app running `zellij attach`, or `ssh -t engine zellij attach` from a client. | A session is shown by the Rex app, which is the terminal. On the engine the app switches to the session; on a client, the client's Rex app opens the engine's Rex server as a host and switches to the session there. No `ssh -t` is involved. |
| engineroom's actions on the engine run over SSH. | Unchanged: engineroom still plans and runs every action on the engine over SSH, where it calls the engine's own `rex`. Only showing a session uses Rex's own connection. |
| Waiting sessions are marked by engineroom's hook, keyed by `ZELLIJ_SESSION_NAME`. | The hook keys on `ENGINEROOM_SESSION`, which engineroom sets in every session it starts, whatever keeps it. Rex's own status display may show the same thing natively (below). |

A machine chooses its backend in its configuration (`multiplexer = "zellij"` or `"rex"`); Zellij
stays the default. The engine's choice decides where its sessions live, so a client follows the
engine's backend when it joins.

## Each operation, as Rex commands

engineroom runs `rex` like any other tool: as a plan of commands shown before they run, with
`--json` wherever it reads an answer.

| engineroom | Rex | Notes |
|---|---|---|
| Is the session server up (`zj.Answers`) | `rex server status --json` | Exit 4: the server could not be reached. |
| List sessions (`zj.List`) | `rex ls --json` | Documented shape: `{"sessions": [{"session_id": "session:...", "label": "..."}]}`. engineroom's session name is the label. |
| Create a session with the standard layout (`zj.Create`) | `rex session new <name> --cwd <dir> --window agent --json -- <agent command>` then `rex window new -s <id> git --focus=false --cwd <dir> -- lazygit` | The agent command is run through the login shell (`--shell`, `--interactive` to read `~/.zshrc`). engineroom records the `session_id` the first call returns and addresses the session by it from then on. |
| End a session (`zj.End`) | `rex session kill <session_id>` | Closing a terminal hangs up its program, as closing a window does. |
| Join on this machine (`zj.Attach` in a terminal app) | `rex do -C <app client> session.select <args>` | Needs the app running and its Remote Control setting on. The app client is found with `rex client ls --json` (kind `app`). |
| Join from a client | `rex do -C <local app client> client.host.switch <args>`, then `session.select` | The engine's server is one of the client's Rex hosts (`rex hosts add engine <endpoint>`). |
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
engineroom therefore:

- records the `session_id` of each session it creates (beside its other state), and acts on that
  ID;
- before creating a session, checks no session already has the name, as it does today;
- treats exit status 3 (a target not found, or ambiguous) as a refusal to say, never as a reason to
  guess.

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
from its menu bar app and over SSH, so it looks, in order: `REX_BIN_DIR` when set, a path in
engineroom's configuration, then the copy inside the installed Rex app. A machine set to Rex
without a `rex` it can run is a setup item, said plainly, never a quiet fall back to Zellij.

### Reaching the engine's Rex from a client

Rex's server listens on a Unix socket by default. A client's Rex app reaches it one of two ways,
chosen when the client is set up:

- **Tailscale**, which Rex recommends: the engine's server joins the tailnet
  (`rex server start --tailscale`, or the address of a tailnet the engine is already on), and the
  client adds it with `rex hosts add engine <tailnet endpoint>`.
- **engineroom's SSH**: engineroom already keeps an SSH connection to the engine for forwarding;
  the engine's Rex socket is forwarded over it to a socket on the client, and the client's host
  endpoint is `unix:///<that socket>`.

### Processes belonging to a session

engineroom decides which servers belong to a session by the `ENGINEROOM_SESSION` variable on the
process, falling back to `ZELLIJ_SESSION_NAME` (internal/procs). Every session engineroom starts
in Rex carries `ENGINEROOM_SESSION` in its agent's and git window's environment, so stopping a
session still stops exactly what it started. Rex's own `REX_SESSION` holds an ID, not a name,
so engineroom does not rely on it.

### The waiting mark

engineroom's Claude Code hook records a waiting session by name; under Rex it reads
`ENGINEROOM_SESSION`. Rex also shows a program's status (working, waiting on the user) when the
program reports it with the Program Status Protocol (OSC 7501), in its session picker and tabs.
Whether Claude Code reports it is not documented; if it does not, engineroom's hook could write the
sequence to the session's terminal so Rex shows the same mark. That is an option to weigh once Rex
is running, not part of the first build.

## What can be built now, without Rex

1. **A session backend seam**: the calls in `internal/zj` behind one interface (list, create with
   the standard layout, end, join, is the server up, the name variable), with Zellij as the only
   backend. No change in behaviour; every journey stays green.
2. **`ENGINEROOM_SESSION` everywhere**: the waiting hook, `engineroom up` and `down` inside a
   session, and the assistant's Remote Control name read engineroom's own variable first, so
   nothing outside `internal/zj` depends on Zellij's.
3. **The Rex backend**, against a stand-in `rex` in the journeys that implements the documented
   commands and JSON shapes above, so the plans and their parsing are tested. Each thing still to
   confirm is one function, so a correction is a one-place change.
4. **Configuration and setup**: `multiplexer = "rex"`, the setup guide's Rex items, and
   `scripts/rex-probe.sh`.

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
