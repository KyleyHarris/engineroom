# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Rex as a session backend, for testing.** With `multiplexer = "rex"`, a machine keeps its
  sessions in Rex and shows them in the Rex app instead of Zellij and a terminal app; a client
  reaches the engine's Rex over Tailscale (`rex.host`). Built from Rex's documentation and not yet
  run against a real Rex: see docs/rex.md for what is still to confirm.

### Changed

- Every session engineroom starts carries `ENGINEROOM_SESSION` set to its name, and that is the
  name read first inside a session, whichever multiplexer keeps it.

## [0.1.0] - 2026-11-10

The first release.

### Added

- **One engine, any number of clients.** One Mac runs every Claude Code session inside Zellij;
  any other Mac lists, joins, starts and stops them over SSH, and the same `engineroom` binary
  runs on both, set as the engine or a client.
- **The menu bar app**: a panel with every project and its checkouts (pinned projects first, in
  a fixed order, with Claude's mark on a session waiting for you), and a window with each
  project's page, the repositories, the history and the settings. Every action is shown as the
  real commands it runs, before, while and after it runs.
- **New work in its own worktree**, on its own branch and slot of ports, with its session; role
  worktrees that move on from branch to branch; clean up that refuses while work would be lost
  and offers ways to keep it.
- **Ports and slots**: each project's ports reserved for it, each worktree on its own slot, and
  forwarding of a session's ports to a client's `localhost` under the same numbers, on demand
  when one of its links is opened. A link whose app is not running starts it first.
- **The app's stack**: a project's up and down scripts in the developer's settings (or a
  repository's `.engineroom/` recommendations), run with the slot's values, and the stack
  helper they source (`engineroom stack-lib`).
- **Settings and secrets** in a private repository of the developer's own, with secret files
  encrypted by git-crypt, synced across machines.
- **Work local**: a client takes the work over from the engine, and hands it back, leaving the
  receiving machine in the giving machine's state (branch, commit and uncommitted work), all or
  nothing, with work in progress carried straight over SSH.
- **Ask Claude**: a helper session that knows the project, its ports, scripts and settings.
- **Upgrade** from the app, through a Claude session on the machine being upgraded.
- **The Claude Code plugin**, with its marketplace in this repository: the skills setup-machine,
  register-project, adopt-project, worktree and run-stack, and the hooks for the waiting mark
  and each session's slot.
- **The website**, built from these docs, with every design decision and its reasons.
