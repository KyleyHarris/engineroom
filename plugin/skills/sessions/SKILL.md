---
name: sessions
description: See and work engineroom's sessions - which are running, which wait for the developer, start a project's session, join, restart or stop one, open its folder in VS Code, run one of a project's own tasks, and read what engineroom ran. Shows engineroom's commands before anything runs. Use when the developer says "what is running", "which sessions are waiting", "start a session for X", "open X", "join X", "restart X", "stop X", "run the task Y", "what did engineroom just do", "show the history".
---

# Sessions through engineroom

Everything here goes through `engineroom api`, the same commands the menu bar app runs, so what
you do is shown, run and kept in the history exactly as when the developer does it. Reading
changes nothing and needs no asking. An action is always two steps: show its plan, then run it on
the developer's word. Never reach around it with `zellij`, `ssh` or `kill`.

## Know where you are

```sh
engineroom api status
```

It prints JSON: `role` (engine or client), `where` (the machine actions are done on), `offline`
(a client working local), `start` (how this machine starts sessions) and `terminal`. On a client
that is not working local, every command below acts on the engine over SSH; you run the same
command either way.

Inside a session, its own name is in `$ZELLIJ_SESSION_NAME`.

A reply that could not be made is `{"error": "...", "engineDown": true|false}` with exit 3. Say
the error word for word. With `engineDown`, the engine did not answer: say so and stop; do not
try another route to it.

## See what is there

```sh
engineroom api sessions              # every session where actions are done
engineroom api project <project>     # one project: its checkouts, each with its session
engineroom projects                  # the registered projects, by name
```

- `sessions` is a list under `sessions`. Each has `name`, `project`, `checkout` (its folder),
  `created`, `ended`, `waiting` and, when its project names them, `slot`, `ports` and `links`.
- `waiting` is `"waiting"` when Claude in that session waits for the developer, with its
  `question` when Claude Code reported one; `"unknown"` when the last report is too old to
  believe; empty otherwise.
- `ended: true` is a session that is no longer running. It is not there to join: starting it
  makes it afresh.
- `here: true`, on a client, is a session on this machine listed after the engine's. Its
  actions take `--here`.
- A project's `checkouts` each have `session`, `branch`, `main` (the main clone), `running`,
  `changes` (uncommitted files) and `waiting`.

Answer "what is running" and "what is waiting" from these, naming sessions exactly as listed.

## Do something: plan, show, run

```sh
engineroom api plan <action...>      # the commands it would run; runs nothing
engineroom api run <action...>       # the same action, run
```

1. Run `plan`. Show the developer `plan.title`, the machine (`plan.machine`), every line of
   `lines` word for word, and every entry of `plan.notes`. If the reply has an `error`, say it
   and stop.
2. Run it only when the developer says to, with `run` and the same words. It prints one JSON
   event per line: the plan, a `step` each time a command changes (`index`, `status`, `exit`,
   `output`, and `onFail` when it failed), then `done` with `ok`.
3. Report the outcome. For a step that failed, give its output and its `onFail`, which says
   what the action left behind and how to go on. `done.history` is the action's id in the
   history.

The actions:

| The developer wants | Action |
|---|---|
| A project's session started | `start <project> --mode remote` |
| A session joined at this machine | `join <session>` |
| A session started again | `restart <session> --mode remote` |
| A session ended | `stop <session>` |
| Its folder in VS Code | `edit-session <session>`, or `edit-project <project>` for the main clone |
| One of the project's tasks run | `task <session> <task name>` |

New work in a worktree, and closing one, are the `worktree` skill. Running a worktree's app is
the `run-stack` skill. Its ports and forwards are the `ports` skill, and moving the work between
machines is the `work-local` skill.

### Starting, and windows

`--mode remote` starts the session with Remote Control and opens no window, so the developer
picks it up in the Claude app, the menu bar panel or `engineroom` wherever they are. Use it
unless they say otherwise: this machine may not be the one they are sitting at.

`--mode zellij` and `--mode both` also open a terminal window on this machine joined to the
session, as `join` does. Use them, `join`, and the two edit actions only when the developer has
said they are at this machine, since a window opened on a machine nobody is at helps no one.

A session that is already running is not started twice. Asked with `--mode remote`, the plan is
refused with a reason that says so, because Remote Control is set when a session starts: tell
the developer it is running already and how they can reach it.

Never run `engineroom join`, `engineroom open` without `--no-join`, `engineroom switch` or bare
`engineroom` yourself: each takes over the terminal it runs in, and yours is not the developer's.

### Stopping

Stop only the exact session the developer named, by the name in the list. Never choose one for
them, never stop several from a pattern, and never stop one to clear the way for something else
without asking. Stopping ends Claude in that session and what was started for it; the plan's
lines say what. Stopping the session you are running in ends this conversation: say so before
they decide.

## A project's own tasks

```sh
engineroom task list <session>       # inside a session: engineroom task list
```

Each line is a task's name and what it is for. Run one as the `task <session> <task name>`
action, planned and shown first. When there are none, say that tasks are named in the project's
`engineroom.toml` in the developer's settings (`engineroom settings where <project>`).

## What engineroom ran

```sh
engineroom history --limit 20        # the newest actions: id, time, machine, outcome, title, each command
engineroom history commands <id>     # one action's commands, ready to paste into a shell
engineroom history --json --limit 20 # the same list as JSON
```

The history is this machine's: an action run from a client is in the client's history. Use it to
answer "what did that do" and "what failed" from what ran, never from memory of what you meant
to run.
