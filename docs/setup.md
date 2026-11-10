# Setting up a machine

Installing `engineroom` is one step. Each machine also needs some software and settings around it,
and this guide lists them: what each one is for, how to check it, and how to fix it. It is
written for macOS, the system the tool is built and tested on.

There are two roles. The **engine** is the always-on machine where sessions run. A **client** is
any other machine you work from: it runs every action on the engine, and when the engine does not
answer it can work on its own copies instead. Working locally is meant to be as capable as
working on the engine, so a client needs everything an engine needs to run sessions, plus what it needs
to reach the engine.

To be walked through it, add engineroom's Claude Code plugin (below, "The Claude Code plugin") and
ask Claude to set up this machine; a clone of this repository has the same skill. The
`setup-machine` skill installs engineroom when it is missing, follows this guide, checks each item,
and changes nothing without showing you the change and asking first.

Examples use invented names: the engine is reached as `engine.example`, and a project is `project-a`.

## Every machine

### Software

| What | Why | Check | Fix |
|---|---|---|---|
| Homebrew | installs the rest | `brew --version` | see brew.sh |
| git | projects, worktrees | `git --version` | `xcode-select --install` |
| Go | builds `engineroom` | `go version` | `brew install go` |
| Zellij 0.45 or later | sessions run inside it | `zellij --version` | `brew install zellij` |
| Claude Code | the agent each session runs | `claude --version` | see Claude Code's install page |
| git-crypt | keeps the secret files in your settings repository encrypted | `git-crypt --version` | `brew install git-crypt` |

### `engineroom` itself

Install it into a folder that every shell finds, including the shell an SSH command runs in.
A client runs `engineroom` on the engine as `ssh engine.example engineroom ...`, and that shell is neither
interactive nor a login shell: zsh reads `~/.zshenv` for it, but not `~/.zprofile` or
`~/.zshrc`. A `PATH` set only in those two files is the most common first-install failure.

```sh
git clone <this repository> && cd engineroom
scripts/install.sh
```

The script builds `engineroom` into `~/.local/bin` (or `ENGINEROOM_BIN`), stamps it with the commit it was
built from, and checks that an SSH command on this machine finds it. It prints every command it
runs and changes nothing else; it never edits your shell's startup files.

If `~/.local/bin` is not already on your `PATH` for every shell, add this line to `~/.zshenv`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Check, with a shell that reads only what an SSH command would:

```sh
env -i HOME="$HOME" PATH=/usr/bin:/bin:/usr/sbin:/sbin /bin/zsh -c 'command -v engineroom'
```

For the menu bar app as well, on any Mac you work from: `scripts/install.sh --app`. It needs
Xcode (or its command line tools) for Swift, builds `~/Applications/engineroom.app`, and signs it with
your Apple Development identity when the keychain is open (run it from a terminal on the Mac
itself, not over SSH), otherwise ad hoc, which runs on that Mac. The app finds `engineroom` in
`~/.local/bin`, `/opt/homebrew/bin` or `/usr/local/bin`, or wherever `ENGINEROOM_PATH` says.

To update later, pull the clone and run `scripts/install.sh` again, on every machine. A client
and its engine should run the same version (`engineroom version`). The app does it for you:
Settings, "Upgrade this Mac…" (and on a client "Upgrade the engine…", also offered when the two
run different builds) starts a Claude session on that machine that pulls, installs, restarts the
app with `engineroom app relaunch` and says what changed, fixing what gets in the way and asking
before anything that could lose work (APP-UPGRADE-001, APP-UPGRADE-002).

### Keys for your projects

Sessions push and pull without anyone at the keyboard, so every machine holds its own SSH key
files for the Git hosts its projects use, and logs in with them without asking anything. A
password manager is a good place to keep the keys and copy them from onto each machine, but a
key that is only usable while a password manager's agent is unlocked stops unattended work the
moment it locks, so point SSH at the key files themselves:

```
Host github.com
  IdentityFile ~/.ssh/<your key file>
  IdentitiesOnly yes
  IdentityAgent none
```

Where different folders belong to different accounts, git's `includeIf` can set
`core.sshCommand` per folder, and a repository can override its folder with its own
`core.sshCommand`. Check each project, from its main clone, with no prompt allowed:

```sh
GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND='ssh -o BatchMode=yes' git ls-remote origin HEAD
```

A remote with an `https://` address authenticates through a stored credential instead of a
key; switching it to its SSH address (`git remote set-url origin git@github.com:<owner>/<repo>.git`)
puts it under the same key rules.

### The Claude Code plugin

engineroom's plugin gives Claude Code its skills and hooks. In Claude Code:

```text
/plugin install engineroom --marketplace KyleyHarris/engineroom
```

It brings:

- **Skills**: `setup-machine` (this guide, item by item), `register-project`, `adopt-project`
  (how a repository runs in worktrees, written to your settings when you agree), `worktree` (make,
  move on or close one) and `run-stack` (start and stop a worktree's app).
- **Hooks**: each session is told, as it starts, which slot of ports its worktree runs on
  (`engineroom slot status --hook`); and the session list marks a session waiting for your
  answer with Claude's mark (✻) and its question (`engineroom signal waiting --from-hook` when Claude
  Code notifies, `engineroom signal clear` when you answer). Each hook does nothing where
  `engineroom` is not installed, and always exits 0, so it never blocks Claude Code.

Check: `claude plugin list` lists `engineroom@engineroom` as enabled. Update it with
`/plugin update engineroom@engineroom` when you update engineroom.

### Your settings on every machine

Your settings for each project (its ports, scripts and secret files) live in a folder of your
own, kept in a private git repository so every machine has the same; git-crypt encrypts the secret
files in it, so GitHub holds only encrypted bytes. On the first machine:

```sh
engineroom settings init git@github.com:<you>/engineroom-settings.git   # git init, git-crypt init, commit, push
(cd ~/.config/engineroom/settings && git-crypt export-key <file>)   # save <file> where you keep secrets, then delete it
```

On every other machine, with the key file fetched from where you keep it:

```sh
engineroom settings clone git@github.com:<you>/engineroom-settings.git <key file>   # then delete the key file
```

Check: `engineroom settings status` says where the settings sync to and that the secrets are
readable here. The guide's "Your settings and a repository's recommendations" has the rest.

### Projects

Register the main clone of each repository you work on:

```sh
engineroom project add ~/src/project-a
engineroom projects
```

On a client, register its own copies too: they are what working locally opens.

## The engine

```sh
engineroom role set engine
```

| What | Why | Check | Fix |
|---|---|---|---|
| Remote Login on | clients reach the engine over SSH | from a client: `ssh -o BatchMode=yes engine.example true` | System Settings, General, Sharing, Remote Login |
| Logged in at the screen | a session started from a client runs in your desktop login, with your login keychain and permissions, as one started at the screen does (D-031); with no one logged in there it runs in the SSH login, where the keychain is locked | over SSH: `launchctl print gui/$(id -u) >/dev/null && echo logged in` | log in at the engine's screen after each restart (locking the screen keeps the login), or turn on System Settings, Users & Groups, Automatically log in as (not offered while FileVault is on) |
| Does not sleep on power | an asleep engine does not answer | `pmset -g custom` shows `sleep 0` under AC Power | System Settings, Battery (or Energy), Options: prevent automatic sleeping when the display is off; or `sudo pmset -c sleep 0` |
| Client keys allowed | each client logs in with its own key file | the client's public key is in `~/.ssh/authorized_keys` | append the client's `.pub` file to it |
| The menu bar app runs | it runs the sweep, so a session that ends without engineroom (quit from inside Zellij) has its servers stopped and ended sessions are forgotten (D-020); there is no job to install | `pgrep -f "engineroom sweep --watch"` shows one while the app runs | open the app, and in its Settings let it open at login; remove a sweep job an earlier build installed with `engineroom sweep --uninstall` |

## A client

```sh
engineroom role set client --engine engine.example
```

| What | Why | Check | Fix |
|---|---|---|---|
| The engine's SSH entry | every action reaches the engine through it | `ssh -o BatchMode=yes engine.example engineroom version` prints the same version as `engineroom version` here | an entry in `~/.ssh/config` naming the engine's key file, as under "Keys" |
| VS Code's `code` command | opening a session in VS Code | `code --version` | VS Code's Command Palette: "Shell Command: Install 'code' command in PATH" |
| VS Code's Remote - SSH extension | VS Code opens the engine's folder over SSH | `code --list-extensions` lists `ms-vscode-remote.remote-ssh` | `code --install-extension ms-vscode-remote.remote-ssh` |
| The engine's name known to VS Code | VS Code resolves the engine through its own SSH configuration | the engine's entry is in the file named by VS Code's `remote.SSH.configFile` setting, `~/.ssh/config` when unset | add the entry there, or point the setting at the file that has it |

Opening a session in VS Code does the right thing for where the session is: a session on the
engine opens the engine's folder over SSH, and a session you are running locally opens this
machine's own folder (docs/decisions.md D-007).

## Recommended, not required

- **Tailscale on the engine and every client**, to reach the engine from anywhere. A client needs only
  an SSH route to the engine; on a [Tailscale](https://tailscale.com) network each machine has a
  stable name (with MagicDNS, `engine.your-tailnet.ts.net`) that works on any network, with no port
  opened on a router. Use that name as the engine's SSH destination and in
  `engineroom role set client --engine`. Check: `tailscale status` on the client lists the engine, and
  `ssh -o BatchMode=yes engine.your-tailnet.ts.net true` succeeds. `engineroom` has no Tailscale-specific
  code; any network that gives the client SSH to the engine works the same way.

- **VS Code's platform for the engine.** Without it, VS Code asks whether the engine runs macOS, Linux
  or Windows the first time it connects. In VS Code's user settings:

  ```json
  "remote.SSH.remotePlatform": { "engine.example": "macOS" }
  ```

- **An engine that starts its services after a power cut.** System Settings, Energy: start up
  automatically after a power failure.

### Copying commands to the clipboard

`engineroom` copies a command by asking the terminal to put it on the clipboard (OSC 52), so it
works over SSH and inside Zellij: the terminal in front of you does the copying. The terminal
never confirms it, so the program says the command was sent, not that it was copied. Some
terminals need it allowed once:

- iTerm2: Settings, General, Selection, "Applications in terminal may access clipboard".
- Terminal.app: does not support OSC 52; the commands stay on screen to select by hand.
- Ghostty, kitty, WezTerm and Alacritty: allowed by default.
- Zellij passes the request on to the terminal, as long as the engine's Zellij configuration
  (`~/.config/zellij/config.kdl`) sets no `copy_command`. With `copy_command "pbcopy"`, every
  selection made with the mouse lands on the engine's clipboard, even while you work from a client;
  without it, Zellij sends the selection to the terminal you are looking through.
- The agent's own copies: each session's agent runs with a `pbcopy` of the program's first on
  its PATH, which sends the text to the terminal you are looking through (OSC 52) as well as to
  the engine's clipboard, so a copy made from a client lands on the client.

### Pasting an image from a client

A terminal sends only text over SSH, and Claude Code takes a pasted image from the clipboard of the
machine it runs on: the engine. So an image copied on a client never reaches it by pasting. On the
client, `engineroom clipboard send` (the photo button in the app's header) saves the image on its
clipboard, copies it to the engine and puts it on the engine's clipboard; then paste in the session as
usual. It needs the engine to be a Mac, where a session's clipboard is the logged-in user's.

## What is not covered

Anything that is not part of running and reaching sessions: mail, chat and meeting apps, and
the rest of a machine's own setup belong to however you set up the machine itself.
