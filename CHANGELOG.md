# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Installing a release starts with `brew trust kyleyharris/engineroom`**, once per Mac: Homebrew
  loads nothing from a tap outside its own until it is trusted. The README, the setup guide and the
  setup skill say so.
- **Works with Zellij 0.46**, which Homebrew installs with the release. Zellij 0.46 names a session's
  socket and saved layout by an id rather than the session's name; engineroom only ever reaches a
  session through Zellij's own commands, so nothing changes for it, and its tests now read the id.
  The setup guide says how to dismiss the setup screen Zellij 0.46 opens over the first session.

## [0.2.1] - 2026-11-14

### Removed

- **The health check no longer asks whether the engine stays awake.** Whether a Mac sleeps is the
  decision of the person running it, kept awake when they need it with a setting or `caffeinate`;
  engineroom never checks or changes it (docs/decisions.md D-042).

## [0.2.0] - 2026-11-14

### Added

- **Releases, installed with Homebrew**: `brew install --cask kyleyharris/engineroom/engineroom`
  installs the menu bar app, the program and Zellij, for Apple Silicon, signed with the Developer ID
  and notarized, with no Go or Xcode needed. The README and the setup guide ask whether to install a
  release or build from the code; `brew upgrade` and the app's Upgrade move to the next release. A release is
  a version number, `vX.Y.Z`. The setup check reports the app installed twice, from a clone and by
  Homebrew (docs/decisions.md D-040).

- **A Mac used on its own has its own role, `local`** (`engineroom role set local`,
  `scripts/install.sh --local`, or the app's Settings, Role, This Mac only). It works as the engine
  does, and its setup check and install ask nothing about being reached over SSH. The app's
  Settings switch between the engine and local; changing the role loses nothing
  (docs/decisions.md D-039).

- **`engineroom health`, and a Health page in the app's window, say whether the engine is ready to
  be worked on from away**: its setup items, staying awake on power and with the lid closed, and
  for each running session whether Remote Control is on and whether its app is running, as the
  project's new optional `status` script says. Every item says how it was checked and, when
  missing, its fix; nothing is changed unless you ask for a fix the program can run, such as
  starting a session's app, whose commands are shown first. On a client it is the engine's report
  (docs/decisions.md D-037).

- **Three more skills in the Claude Code plugin**, so Claude in any session works engineroom the
  way the menu bar app does, with the same commands: `sessions` (what is running and what waits
  for you; start, join, restart or stop a session; run a project's task; read the history),
  `ports` (a worktree's slot and ports, clashes between projects, and forwarding a session's
  ports to a client) and `work-local` (show a handover, a release or a mirror, say what blocks
  it, and run it). Each shows an action's commands first and runs it only when you say; none
  stops a session you did not name. Update the plugin with `/plugin update engineroom@engineroom`.

- **The website shows the app and how to install it, and can be found and shared.** The front
  page has the app's two pictures and the install steps. Every page has its own description, its
  address and link preview tags; the site has an icon, a sitemap and a page for an address that is
  not there. `site/site.toml` gains `url`, `images` and `share`.

### Changed

- **The menu bar app keeps the permissions macOS granted it across installs**: `scripts/install.sh
  --app` signs it with the Mac's Apple Development identity or installs nothing, saying why and
  how to let `codesign` use the key, where it used to sign ad hoc and so make macOS ask for every
  permission again. A failed install leaves the installed app as it was (docs/decisions.md D-038).

- **A session on the engine runs inside your desktop login, however it was started.** A session
  started from a client, over SSH, ran in that SSH login, without your login keychain. While you
  are logged in at the engine's screen, its start is now handed to launchd as a one-time job in
  your desktop login, shown in the plan, so Claude Code and everything else in the session has the
  keychain and permissions you have at the screen (docs/decisions.md D-031). With no one logged in
  there, a session starts as before.

- **The design decisions are rewritten and no longer part of the website.** `docs/decisions.md`
  now states each decision as the program is today: the problem, what was decided, and what else
  was considered. The website keeps to setting engineroom up and using it.

### Removed

- `engineroom sweep --uninstall`, and the one-time move of the program's folders at start. The
  sweep runs with the menu bar app and installs nothing.

### Fixed

- **An application the program starts is started as from the Dock.** A terminal opened to join
  a session, and the menu bar app reopened after an upgrade, now start with the developer's own
  session environment, whatever ran the program: a shell, a Claude Code session, an SSH login.
  Before, a Ghostty started by the program kept the starter's variables, and every tab it opened
  afterwards inherited them (docs/decisions.md D-030). A Ghostty already running keeps what it
  was started with until it is quit and opened again.

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
