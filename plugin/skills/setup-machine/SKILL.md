---
name: setup-machine
description: Set up this machine to run engineroom as the engine, as a client, or as a Mac used on its own (local), following engineroom's setup guide (docs/setup.md) - install engineroom when it is missing, check every item for the machine's role, explain each gap, and apply a fix only after showing the exact change and getting a yes. Use when someone says "set up this machine", "install engineroom here", "configure this as the engine", "make this a client", "use it on its own", "is this machine set up", or is installing the tool on a new machine.
---

# Set up this machine

engineroom's setup guide, `docs/setup.md` in its repository, is the authority on what a machine
needs. This skill walks it; it never decides on its own that something is required, and it never
adds a requirement the guide does not list. If the guide and the machine disagree in a way the
guide does not cover, say so and ask.

## The rule that governs every step

Nothing outside engineroom's own state changes without the person's yes. For each fix: show the
exact command or the exact lines being added to which file, say why, and wait. One fix per yes. A
file you change is backed up first (`<file>.bak-<date>`), and a JSON settings file is merged,
never overwritten.

## Steps

1. **Find the guide.** When `engineroom version` answers, the guide is in the clone it was built
   from: `engineroom version --clone` prints that folder (empty for a release Homebrew installed).
   When engineroom is not installed yet, ask how to install it, and say the difference:
   **install a release** with Homebrew (for using engineroom: needs only Homebrew, signed and
   notarized, updated by `brew upgrade`), or **build from the code** (for changing it or running
   unreleased work: needs Go, and Xcode for the app). Recommend the same way as the other Macs this
   one connects to. Releases are for Apple Silicon only. Then, on a yes, either

   ```sh
   brew install --cask kyleyharris/engineroom/engineroom   # a release: the app, the program and Zellij
   ```

   or ask where the clone should go (suggest `~/src/engineroom`) and

   ```sh
   git clone https://github.com/KyleyHarris/engineroom <folder>
   <folder>/scripts/install.sh            # with --app on a Mac that wants the menu bar app, --local on a Mac used on its own
   ```

   Never both on one Mac. Without a clone, read the guide from GitHub
   (`https://github.com/KyleyHarris/engineroom/blob/main/docs/setup.md`); with one, from
   `<folder>/docs/setup.md`.
2. **Read the guide in full**, then ask which role this machine has, unless
   `engineroom role show` already answers it: the engine, a client, or local (a Mac used on its
   own, with no clients; `engineroom role set local`). For a client, ask for the engine's SSH
   destination (an alias from `~/.ssh/config`, or user@host). For a local Mac, install with
   `scripts/install.sh --local`, which sets the role and does not ask for SSH.
3. **Check every item for that role.** Once engineroom is installed and the role is set, run
   `engineroom setup check --json`: it is the program's own list of what this role needs, each item
   present or missing with how it was checked and its fix, and it changes nothing. Its items are
   decided there, not here: report them as it gives them. Then check the guide's items the program
   does not check (keys, the settings repository, logged in at the screen, the menu bar
   app, Homebrew, Go, git-crypt) with the guide's Check column exactly. Report everything as one
   list: in place, missing, or could not be checked (and why). A check that needs the network and
   fails because there is no network is "could not be checked", not "missing".
4. **Walk the gaps one at a time**, in the guide's order, for each: what it is for, the exact
   fix (for an item from the setup check, its `fix`), and a yes or no. Apply it only on a yes, then
   run its check again and report the result.
   Fixes that need an administrator password (`sudo`) or a System Settings pane are given to
   the person to do, with the exact steps; do not try to get around a password prompt.
5. **Keys are copied by the person, never invented.** If a project's remote or the engine refuses
   this machine's key, say which key file the SSH entry names and where the person keeps the
   key, and wait for them to put it in place. Check a copied key's fingerprint
   (`ssh-keygen -lf <file>`) against the one they expect before using it. The same holds for the
   settings repository's git-crypt key. Never print a private key, a token, or any secret.
6. **Offer the recommended items last**, clearly marked as optional.
7. **Finish with the whole list again** (`engineroom setup check` once more, and the guide's other
   checks), so the person sees the machine's state in one place,
   and name anything still missing. Then offer the next step: register the repositories they work
   on (`register-project`) and make each one run in its worktrees (`adopt-project`).

## What this skill does not do

- Anything the guide lists under "What is not covered".
- Change another machine. An engine item seen from a client (for example the client's key in the
  engine's `authorized_keys`) is reported with what to do on the engine.
- Register projects the person has not named. Offer to register main clones it finds, and list
  them for a yes first.
