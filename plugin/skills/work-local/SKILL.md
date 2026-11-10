---
name: work-local
description: Move the work between the engine and a client with an engineroom handover - take it over to work local on the client, hand it back to the engine, release the engine's work to GitHub, or get a client ready to work local. Shows what the handover would do and blocks on, and runs it only on the developer's word. Use when the developer says "work local", "take the work over", "I'm going offline", "hand back to the engine", "what would a handover do", "is this machine ready to work local", "release the engine", "why is the handover blocked".
---

# Working local, and handing back

Each machine has its own copies of every project. A handover leaves the receiving machine in the
giving machine's state, checkout by checkout: the same branch, at the same commit, with the same
uncommitted work. It is all or nothing, and it is the one thing here that moves the developer's
work between machines, so it is shown in full and run only when they say.

## Where you are

```sh
engineroom api status
```

`role` is `engine` or `client`; `offline: true` is a client working local; `released`, on the
engine, is when it was last released. Taking the work over and handing it back are done **on the
client**. On the engine there is only the release, below. If the developer asks for a handover and
you are on the engine, say so: it is run from the client.

## Show a handover

```sh
engineroom api plan handoff from-engine --switch   # take the work over, then work local here
engineroom api plan handoff to-engine --switch     # hand it back, then work on the engine again
```

This runs nothing. It reads every registered project on both machines against GitHub, so it can
take a while; lines of `{"progress": ...}` on standard error say how far it has got. Without
`--switch` the plan is the handover alone, for "what would a handover do".

From the reply, show the developer, in this order:

1. `plan.title`.
2. Every entry of `plan.notes`, word for word. They say what happens to each checkout, what is
   left alone, and what stands in the way. Do not summarise them: a branch it will not push, or a
   session whose changes leave its folder, is in there.
3. Every line of `lines`, word for word: the commands it would run.

Then read it for the developer:

- `plan.blocked: true` is a handover that cannot run as things stand. It has no commands, and the
  switch waits with it. The notes say what to clear (a branch that moved both here and on GitHub,
  a release branch with commits GitHub lacks, a receiving branch with commits of its own, and the
  like). Say what blocks it and stop. Do not clear it yourself: each is a decision about the
  developer's work.
- `plan.offers` are other actions the plan suggests, each with a `label` and the `action` that
  does it. Name them. Never choose one for the developer; when they choose one, show its plan
  (`engineroom api plan <its action words>`) and run it on their word.
- A note that nothing needs handing over means running it only switches.
- A note that the engine could not be read or checked: the engine did not answer, and the plan
  is made without it. Taking the work over then moves nothing from the engine, and whatever is
  committed there and not pushed stays there. Read that note out before anything else.
- An `error` instead of a plan is a refusal: say it word for word. On the engine it is "only a
  client hands work over".

## Run it

Only when the developer has seen the plan and says to:

```sh
engineroom api run handoff from-engine --switch
```

with the same words as the plan they saw. It prints one JSON event per line: progress, the plan,
a `step` each time a command changes, then `done` with `ok`. Its first commands change nothing:
each machine checks its checkouts are still as they were read, and each push is tried with
`--dry-run`, so an edit or a commit made since the plan stops it before anything moves.

Report the outcome. For a step that failed, give its `output` and its `onFail`, which says what
state the handover left and how to go on. Follow that; never improvise a recovery with `git` on
the developer's checkouts. The switch is the last step, so it happens only when the handover ran.

Do not use `engineroom offline on` or `engineroom offline off` for this: they switch at once and
only show the handover, so the work can be left on the other machine. And do not run `engineroom
handoff to-engine` or `from-engine` without `--json`: they take over the terminal.

If you are running in a session whose folder is handed over, its changes leave with the handover
and the plan names the session. Say that before the developer decides.

## Is this client ready

```sh
engineroom api plan mirror
```

The mirror copies to the client the engine's projects it lacks and fetches the rest, and moves no
worktree. Show its lines, and run it with `engineroom api run mirror` when the developer says. The
menu bar app does the same while it can reach the engine.

## Release, on the engine

```sh
engineroom api plan handoff release
```

On the engine, before working local on a client that cannot ask it: this pushes the work that is
checked out to GitHub. It has no other machine to carry uncommitted work to, and work in progress
does not go to GitHub, so the notes name that work and `plan.offers` offer to commit each
worktree's as partial work on its own branch. Show the notes, the lines and the offers; run the
release with `engineroom api run handoff release`, and an offer only when the developer chose that
one.

## What a handover never moves

Ignored files, engineroom's slot files and the `.claude` folder belong to each machine and do not
travel. Anything ignored that has to follow the developer belongs in their settings as a secret
file (the `adopt-project` skill). Sessions are not moved either: a session started on the client
keeps running after it hands back, and is listed with `here` (the `sessions` skill).
