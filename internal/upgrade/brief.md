# You are upgrading engineroom on this machine

The developer pressed Upgrade in the engineroom app. Bring engineroom on this machine up to the
latest `main` on GitHub: the command line and, when it is installed here, the menu bar app. Show
every command before you run it, and say in one line what each step found.

## The steps

1. Look at the clone (named below): its branch, uncommitted changes, and commits GitHub does not
   have (`git -C <clone> status --short --branch`, `git -C <clone> fetch origin`,
   `git -C <clone> log --oneline origin/main..HEAD`).
2. Bring it up to GitHub's main: `git -C <clone> pull --ff-only origin main`.
3. Build and install: `<clone>/scripts/install.sh --app` (without `--app` when the app is not
   installed here, as said below).
4. Check the new build: `engineroom version` names the commit now at the clone's HEAD.
5. When the app is installed here, restart it so the new app runs: `engineroom app relaunch`. It
   quits the app and opens it again, without this session's markers, a moment after it returns.
   This session keeps running.
6. Say what changed: the old and the new version, and `git -C <clone> log --oneline <old>..<new>`
   in a few lines, with what the `## [Unreleased]` part of CHANGELOG.md gained.

## When something is in the way or fails

- Diagnose it: read the full output, the script that failed, and what it needs.
- Fix what is plainly this machine's tooling (a missing tool, `brew install` it; a stale build
  cache; a PATH that misses `~/.local/bin`), showing each command, then carry on.
- Stop and ask before anything that could lose work or changes the code: the clone on a branch
  other than main, uncommitted changes, commits GitHub does not have, a merge, a reset, a stash,
  deleting anything. Say what you found and what you recommend, then wait.
- Never commit, push or open a pull request in the engineroom repository from here.
- If the build fails in the code itself, say where and why, and stop: that is a fix for the
  repository, not for this machine.

When it is done, say so in one line with the new version, and that this session can be closed.
