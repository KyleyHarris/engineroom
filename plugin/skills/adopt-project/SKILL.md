---
name: adopt-project
description: Make a repository run with engineroom - read the repository, work out what its app needs (servers, ports, emulators, databases, secret files, set-up per worktree), recommend the project's engineroom settings and up and down scripts, and write them into the developer's own settings only after they agree. Learns the developer's own stacks from recipes kept in their settings. Use when the developer says "adopt this repo", "set this project up with engineroom", "make engineroom run this", "how should this run in worktrees", "integrate this repository", or registers a project that has no up script yet.
---

# Adopt a repository into engineroom

engineroom runs each worktree of a project on its own **slot** of ports, starts its app with the
project's `up` script and stops it with `down`. Those live in the developer's own settings, outside
the repository: `engineroom settings where <project>` prints the folder. This skill reads the
repository and recommends what to put there. It writes nothing until the developer has seen it and
said yes, and it never writes into the repository itself unless the developer owns it and asks for
its recommendations in `.engineroom/`.

## 1. Know where you are

- `engineroom version` answers, and `engineroom projects` lists the project. If it is not
  registered, offer the `register-project` skill first.
- `engineroom settings where <project>`: the project's settings folder. `engineroom settings status`:
  the settings root (its first line), and whether the settings sync to a private repository.
- Read what is already there: the project's `engineroom.toml` and scripts in the settings folder,
  and any `.engineroom/` folder in the repository. Recommend changes to those, not a second copy.

## 2. Read the developer's recipes first

The developer teaches this skill their own stacks in `<settings root>/adopt/`, one Markdown file
per stack or habit (for example `vite.md`, `dotnet.md`, `azurite.md`, `preferences.md`). Read every
file there before analysing the repository. A recipe wins over the general guidance below: it is how
this developer wants that stack run. When there is no `adopt/` folder, start from the guidance
below, and offer at the end to record what was learned (step 6).

## 3. Read the repository

Find, and note with the file that says it:

- **Parts that serve something**: a web front end (Vite, Astro, Next, a plain dev server), an API
  (.NET, Node, Python, Go), workers, and the port each listens on today and where that port is set
  (`vite.config.*`, `package.json` scripts, `launchSettings.json`, `appsettings*.json`, `.env`,
  `docker-compose.yml`).
- **What they need around them**: package installs (lock files), emulators (Azurite, a Cosmos or
  Firebase emulator), databases and their names, containers.
- **Secret files** the app reads locally and git ignores: `.env`, `local.settings.json`, user
  secrets, certificates.
- **One-time set-up per worktree**: a database to create, a file to generate, a certificate to trust.
- **Tests that start their own servers** on their own ports: these are not the app, and need no slot.

## 4. Recommend, in one message

Show, with a line of reasoning each:

- **`engineroom.toml`**: `secrets = [...]` for the secret files, before any table; a `[ports]` key
  per part (`ui = 5173`, `api = 5000`), the numbers the project uses today, which become slot 1;
  `[links]` naming what to open (`app = "http://localhost:{{ui}}/"`); `[derived]` values built
  from a slot's own; `[[render]]` templates where a config file must carry a slot's ports;
  `[[task]]` for the project's own jobs (copy a database, seed data). Then
  `engineroom ports reserve <project> --write` gives the project its block of ports and writes slots 1
  to 10, where per-slot names such as a database's (`names = { database = "app_wt2" }`) can be
  added. The guide's "Ports and slots" section has the whole format.
- **`up`**: sources engineroom's stack helper, installs what is missing, and starts each part on the
  slot's port, read from `ENGINEROOM_PORT_<KEY>` with slot 1's number as the fallback:

  ```bash
  #!/usr/bin/env bash
  # up for project-a: the web app on the slot's ui port, the API on its api port.
  . "$(engineroom stack-lib)"
  deps . || exit 1
  start web "${ENGINEROOM_PORT_UI:-5173}" "http://localhost:${ENGINEROOM_PORT_UI:-5173}/" . -- npx vite --port "${ENGINEROOM_PORT_UI:-5173}" --strictPort
  start api "${ENGINEROOM_PORT_API:-5000}" - server -- dotnet run --urls "http://localhost:${ENGINEROOM_PORT_API:-5000}"
  ```

  `start NAME PORT URL DIR -- COMMAND...` runs the command in the background, waits for the URL
  to answer (or the port to listen, with `-`), and leaves a part alone that already runs for this
  worktree. A part must be told its port on its command line or in its environment, never left to
  pick one, and must fail rather than move to another port when its own is taken (Vite's
  `--strictPort`).
- **`down`**: `. "$(engineroom stack-lib)"`, then `stop web api`: exactly what `up` started.
- **`status`**: `. "$(engineroom stack-lib)"`, then `running web api`: ends with success when every
  part `up` started still runs, for `engineroom health`. It must change nothing and end within 10
  seconds.
- **`create` and `teardown`**, only when a worktree needs set-up of its own (a database per slot)
  and its undoing.
- **What the developer must do**: add the slot record and anything rendered to `.gitignore`
  (`engineroom slot ignore`), save the secret files (`engineroom secrets save <project>`).

Every script must be safe to run again: check before installing, building or starting, and exit 0
when there is nothing to do. Ask: "Shall I write these into your settings for <project>?"

## 5. Write, then prove it

On a yes, write the files into the settings folder, make the scripts executable, then run, showing
each command and what it printed:

```sh
engineroom ports check
engineroom up <session>     # inside the project's session: engineroom up
engineroom down <session>
```

Open the links and say what answered. A failure is fixed in the scripts with the developer, never by
starting a server another way. Then `engineroom settings sync` when the settings sync to a
repository.

## 6. Learn

When the developer corrected a recommendation, or the repository used a stack the recipes did not
cover, offer to record it: a short recipe in `<settings root>/adopt/<stack>.md` saying how that
stack is run (its install, its start command with the port, how to tell it is up, its secret files),
written as instructions for the next adoption. Write it only on a yes, then sync the settings. The
next repository on that stack starts from it.
