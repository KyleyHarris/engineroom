---
name: run-stack
description: Build and run, or stop, the app stack of the current worktree through its project's up and down scripts, with engineroom. Use when the developer says "run the app", "start the stack", "bring the stack up", "stop the stack", "shut it down", or asks to run what this worktree needs, in a repository registered with engineroom.
---

# Start and stop the app stack with engineroom

A project registered with `engineroom` says how its app runs in a worktree as two scripts: `up`
installs, builds and starts everything the worktree needs, on the worktree's own ports, and
returns once it is running; `down` stops it. The developer's own scripts live in their settings
(`engineroom settings where <project>`); a repository may also recommend its own in `.engineroom/`, used when
the developer has none. Running the stack always goes through them, so it is done the same way
every time, by the developer or by you. Run it whenever you need the app running while you work.

1. Inside an engineroom session, run:

   ```sh
   engineroom up     # start the stack of this session's worktree
   engineroom down   # stop it
   ```

   Outside a session, name it: `engineroom up <session>`. `engineroom sessions` lists the sessions.

2. Report what it printed: each command, its output, and `ok, done` or the failure with what
   it left behind. The run is in `engineroom history` too.

3. **Handing a build to the developer to try:** run `engineroom down`, then `engineroom up`. `up` leaves a
   part that is already running alone, so after a rebuild `up` on its own would keep serving the
   old build. Then give the developer the app's links (`engineroom slot get`, or the session's links in
   the app) and the commit that is now running (`git log --oneline -1`).

4. **Never start the app's servers another way** (`dotnet run`, `npm run dev`, `vite`) in a
   repository registered with engineroom. What `engineroom up` starts belongs to the session and is stopped
   when the session ends (docs/decisions.md D-020); a server started by hand outlives it and
   holds the worktree's ports. A test suite that starts its own servers on its test ports is not
   the app and is unaffected.

5. If it says the project has no up (or down) script, do not start the stack another way. Tell
   the developer, and offer to write the script in their settings folder: an executable `up`
   that installs, builds and starts what the worktree needs (servers in the background or a
   container) and returns once it is running; `down` stops what it started. Both run in the
   worktree with `ENGINEROOM_PROJECT`, `ENGINEROOM_WORK`, `ENGINEROOM_MAIN` and `ENGINEROOM_WORKTREE` set, and read the
   worktree's ports from its slot (`ENGINEROOM_PORT_<KEY>`, `engineroom slot get <name>`, or the
   slot record). Build them on engineroom's stack helper: `. "$(engineroom stack-lib)"`, then
   `deps <dir>`, `start <name> <port> <url> <dir> -- <command...>` and `stop <name>...`; the
   `adopt-project` skill writes them that way. Offer a `status` script with them too
   (`running <name>...` with the helper), which `engineroom health` runs to say whether the app
   is up; it must change nothing and end within 10 seconds.

6. **Scripts must be safe to run again.** Run twice, `up` finds what it installed and started the
   first time and leaves it alone, and `down` with nothing running does nothing. Check before
   installing, building or starting, and exit 0 when there is nothing to do. The program does not
   check for the script.
