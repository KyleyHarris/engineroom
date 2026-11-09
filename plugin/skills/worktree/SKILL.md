---
name: worktree
description: Make, move on or close a worktree of a project registered with engineroom, through engineroom, never with git worktree directly. Asks whether it is for a branch (closed when the branch is done) or for a role (kept, holding one branch after another), shows engineroom's commands before anything runs, and never loses work when closing. Use when the developer says "make a worktree", "new worktree", "spin up a worktree for X", "a designer/tester worktree", "move the designer worktree on to X", "close this worktree", "remove the worktree", "clean up the worktree".
---

# Worktrees through engineroom

A worktree is made, moved on and closed only through engineroom, and only when the developer asks for
it in this conversation: engineroom gives each one its own slot of ports, a copy of the project's setup,
a session, and a close that checks nothing will be lost. Never run `git worktree add`, `git
worktree remove`, `git stash` or `git push` yourself for this; every step below is an engineroom action
whose commands are shown before it runs.

There are two kinds (docs/decisions.md D-024 in the engineroom repository):

- **For a branch**: one piece of work. Named `<repo>-<branch>`, on that branch; closed when the
  branch is done.
- **For a role**: kept, such as designer, tester, primary or secondary. Named `<repo>-<role>`, it
  holds one branch after another, with the same session name and ports.

## Find the project

`engineroom project here` prints the registered project this folder belongs to (exit 0), or fails when
it is in none. If it fails, say the folder is not in a project registered with engineroom, and offer the
register-project skill. `engineroom projects` lists every project if the developer names another.

## Make one

1. Ask whether it is for a branch or for a role, unless they said. For a branch, ask its branch
   name; for a role, ask the role's name and its first branch (a new one is made from the
   remote's main; an existing branch is used as it is). Names are letters, digits, `.`, `_` and
   `-`, with no slashes.
2. Show the plan, and nothing runs yet:

   ```sh
   engineroom api plan new <project> <branch> --mode remote
   engineroom api plan new-role <project> <role> <branch> --mode remote
   ```

   It prints JSON: show the developer its `title` and every line of `lines`, word for word. If it
   has an `error`, say it and stop. `--mode remote` starts the new session with Remote Control and
   opens no window on this machine, which may not be the one the developer is at; they join it from
   the menu bar panel, `engineroom`, or the Claude app.
3. Run it only when the developer says to: the same action with `run` in place of `plan`. It
   prints one JSON event per line; report each step that failed with its `error` and `onFail`, and
   the outcome (`"event":"done"`, `ok`).

## Move a role worktree on to its next branch

`engineroom api plan next-branch <project> <role> <branch>`, shown, then run on their word. It refuses
while work would be lost: see the next section for what it offers.

## Close one

1. Find its session name: `engineroom api project <project>` lists `checkouts`, each with `session`,
   `work`, `role` and `branch`. The main clone is never closed.
2. `engineroom api plan clean-session <session>`. When it plans, show the lines (the session stopped,
   the teardown script, the worktree removed, the branch deleted only if merged, a role forgotten)
   and run it only on their word.
3. When it is refused, its `error` says why (uncommitted changes, naming files, or commits never
   pushed) and `offers` lists what would make it safe, each a `label` and an `action`:
   - save the changes to the branch and push it (`save-work`),
   - put them aside as a stash of the repository, which outlives the worktree (`put-aside`),
   - push the commits (`push-work`).

   Read the error and the offers to the developer and ask which they want. Never choose for them,
   and never throw work away: there is no offer to discard, and you do not make one. Show the
   chosen offer's plan (`engineroom api plan <action...>`), run it on their word, then plan the close
   again.

