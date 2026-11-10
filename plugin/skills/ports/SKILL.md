---
name: ports
description: See which ports engineroom gives each project and worktree, check them for clashes, and on a client reach a session's ports on the engine by forwarding them. Use when the developer says "what port is X on", "which ports does this worktree use", "do any ports clash", "is something on that port", "forward X", "I can't reach the app on the engine", "stop forwarding", "what is forwarded".
---

# Ports, slots and forwards

A project names its ports in its `engineroom.toml` in the developer's settings. Each worktree
holds a slot, and a slot is one row of those ports, so two worktrees of one project never share a
number. On a client, a session's ports on the engine are reached on the client's own localhost,
under the same numbers, by forwarding them.

Never pick a port number yourself, and never renumber anything: the numbers come from the
developer's settings, and a clash is theirs to settle there.

## Which ports

```sh
engineroom slot status                      # this worktree: its slot and its ports
engineroom slot get <key>                   # one value of the slot; an unknown key lists the known ones
engineroom slot json                        # every value of the slot
engineroom slot status --worktree <folder>  # another worktree of a registered project
engineroom ports                            # every project on this machine: what it reserves, each slot's ports
```

For a running session, its `slot`, `ports` and `links` are in `engineroom api sessions`, wherever
it runs (the `sessions` skill). Give the developer a session's `links` as they are, not an address
you built from a number.

`slot status` says one of three things: the slot the worktree holds; that it is the main clone, on
slot 1, the project's own numbers; or a WARNING that a linked worktree holds no slot and would
share the main clone's ports and data. For the warning, tell the developer and show the command it
names (`engineroom slot claim --worktree <folder>`); run it when they say.

A project that says it declares no ports has no slots: say so, and that the `adopt-project` skill
is how its ports are named.

## Clashes, and what is listening

```sh
engineroom ports check
```

- `clash:` lines are two projects' settings naming the same port. Exit 1 when there are any.
- `in use:` lines are ports that something is listening on now, with what holds each. That is a
  report, not a fault: it is often the project's own app, running.
- `No clashes between the projects' ports.` when the settings are clean.

Report the lines word for word. A clash is fixed in the settings (`engineroom settings where
<project>` prints a project's folder there); show the developer both lines and let them choose
which project moves. Never stop a process because it holds a port: a session's app stops through
the `run-stack` skill, and anything else is the developer's.

A project with no reservation of its own gets one with `engineroom ports reserve <project>`, which
shows what it would write and writes nothing; with `--write` it writes it into the settings. Show
the first, and run the second only on the developer's word.

## Forwarding, on a client

These are a client's commands. On the engine they refuse, since the ports are already there:
`engineroom api status` gives `role`.

```sh
engineroom forward status            # what is forwarded now, and what the last check found
engineroom forward add <session>     # reach this session's ports here
engineroom forward remove <session>  # stop reaching them
engineroom forward on                # every running session's ports, as sessions start and stop
engineroom forward skip <session>    # with forwarding on, leave one session out (unskip puts it back)
engineroom forward off               # close every forward
engineroom forward                   # bring the forwards in line with the engine's sessions, once
```

Each one changes this machine's own configuration and opens or closes SSH forwards at once, and
each forward made or closed is a command in the history. So run one only when the developer asked
for that: `add` for the one session they are testing, by its exact name from the list; `on` or
`off` only when they said every session.

Then read what it printed, and `engineroom forward status`:

- A port this machine already uses is named with what holds it and left alone. Tell the
  developer; do not stop what holds it.
- `FAILED:` under the last check is why the forwards were left as they were, word for word.
- While the client works local nothing is forwarded, and the output says so: its own apps run on
  those numbers. Forwarding comes back when it hands back to the engine (the `work-local` skill).
- When the developer cannot reach an app that is forwarded, check that the session's app is
  running at all (the `run-stack` skill) before looking further.

Never open an SSH tunnel of your own in place of these: a forward engineroom did not make is one
it cannot show, follow or close.
