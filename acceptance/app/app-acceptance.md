# Menu bar app - acceptance criteria

**Scope**: the menu bar app on a Mac, the engine or a {client.lower}: listing the
{session.lowerPlural}, every repository the {engine.lower} knows of, adding and registering
repositories, starting a {session.lower} three ways, opening VS Code, choosing the terminal a
{session.lower} is joined in, seeing commands before they run, the history, opening at login, and
each repository's page with its worktrees, links and organising.
Out of scope: a phone, which checks on {session.lowerPlural} through the Claude app and Remote
Control.

**Sourcing**: every scenario's intent is stated in its own comment, or in the decisions it cites.
Nothing here is inferred from software.

**How it is checked**: the app runs nothing itself; every list it shows and every action it takes
is a `engineroom api` command (docs/decisions.md D-008). The journeys drive those commands through the
real program, against real Zellij, git and SSH, exactly as the app calls them. What only the
window does (drawing, opening at login) is checked by the app's own tests.

**Status**: every scenario is `proposed`, written at planning before the software exists.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Works from the menu bar on the engine or a {client.lower} |

---

## User stories

> **As a** developer, **I want** engineroom a click away in the menu bar, **so that** I can start,
> join and open my work without keeping a terminal copy of the tool running.

- Everything is done where the terminal interface would do it: on the {engine.lower}, or on a
  {client.lower} that is offline.
- Claude Code always runs inside Zellij, so anything started can be joined from another machine.
- Anything that creates something shows its commands first; everything is in the history.

---

## Gherkin scenarios

### Feature: Sessions

<!-- intent: the panel lists the sessions with the waiting ones marked on their own line, never in
     a section of their own that lists them twice -->

```gherkin
@APP-LIST-001 @status:proposed @persona:Developer
Scenario: The app is told which {session.lower} is waiting for the developer, with its question
  Given the {engine.lower} has a {session.lower} waiting for the developer and one that is not
  When the app lists the {session.lowerPlural}
  Then the waiting {session.lower} is marked as waiting, with its question
  And the other is listed once, not marked
```

<!-- intent: a list whose order changes whenever Claude starts waiting never has anything where it
     was last seen. Pinned projects are always there and always first, and the order never changes
     (D-022). The order is laid out by the app from the list the engine gives it, so it is checked
     by the app's own tests. -->

```gherkin
@APP-LIST-002 @status:proposed @persona:Developer
Scenario: The panel keeps one order, pinned {project.lowerPlural} first, whatever is waiting
  Given the pinned {project.lower} beta with no running {session.lower}
  And running {session.lowerPlural} of alpha and gamma, one of them waiting for the developer
  When the panel lists them
  Then beta is listed first, with a way to start it
  And the {session.lowerPlural} of alpha and gamma follow by name, the waiting one marked in its place
  And they are listed in the same order when none is waiting
```

<!-- intent: a project's code, issues and pull requests on GitHub are one click from the panel, with
     no browser search in between -->

```gherkin
@APP-LIST-003 @status:proposed @persona:Developer
Scenario: A {session.lower} in the panel links to its {project.lower} and its branch on GitHub
  Given a {project.lower} whose origin is the GitHub repository acme/widget
  And a running {session.lower} in a {checkout.lower} on the branch feature-x, sent to GitHub
  When the app lists the {session.lowerPlural} and the repositories
  Then the {project.lower} links to the code, the issues and the pull requests of acme/widget on GitHub
  And the {session.lower} links to the branch feature-x and the pull requests from it on GitHub
```

### Feature: Staying up to date

<!-- intent: reading the sessions on a timer costs a client a connection per look, and its battery;
     the engine tracks its own sessions cheaply. The engine looks at its own sessions and sends the
     app a list only when it changed, over one connection the app keeps (D-023). -->

```gherkin
@APP-FEED-001 @status:proposed @persona:Developer
Scenario: A change on the {engine.lower} reaches the app without the app asking
  Given the app on a {client.lower} follows the {engine.possessive} {session.lowerPlural}
  When a {session.lower} on the {engine.lower} starts waiting for the developer
  Then within seconds the app is told it is waiting, with its question
  And it was told over the one connection the app opened
```

```gherkin
@APP-FEED-002 @status:proposed @persona:Developer
Scenario: Nothing is sent to the app while nothing changes
  Given the app follows the {session.lowerPlural}, one of them running
  When nothing changes for a while
  Then the app is sent no list after the first
```

<!-- intent: an action the app runs reads again at once, so what it changed shows; with the feed
     that is a request to look now, over the connection already open -->

```gherkin
@APP-FEED-003 @status:proposed @persona:Developer
Scenario: After an action the app is told what changed at once
  Given the app follows the {session.lowerPlural}, looking by itself only once a minute
  When a {session.lower} starts and the app asks to look now
  Then the app is told of the new {session.lower} within seconds
```

<!-- intent: nothing is left running on the engine for an app that has gone (D-020's spirit: what runs
     lives only as long as what it was started for) -->

```gherkin
@APP-FEED-004 @status:proposed @persona:Developer
Scenario: Following ends on the {engine.lower} when the app stops reading
  Given the app on a {client.lower} follows the {engine.possessive} {session.lowerPlural}
  When the app closes its end
  Then nothing is left following on the {engine.lower}
```

<!-- intent: the feed stays connected while the app runs, so the menu bar says Claude is waiting
     without the panel being opened. Drawn by the app from the list the feed gives it, so it is
     checked by the app's own tests. -->

```gherkin
@APP-FEED-005 @status:proposed @persona:Developer
Scenario: The menu bar shows Claude's mark while a {session.lower} waits, with the panel closed
  Given the app follows the {session.lowerPlural} and its panel is closed
  When a {session.lower} starts waiting for the developer
  Then the menu bar icon carries Claude's mark
  And the mark goes when no {session.lower} is waiting
```

<!-- intent: the panel's compact card shows what is unsaved, what is not pushed and how far behind
     main a checkout is, and whether its branch is done, live where it is cheap: read on the engine
     from its own disk and sent with the sessions (D-025) -->

```gherkin
@APP-FEED-006 @status:proposed @persona:Developer
Scenario: The app is told what is unsaved, unpushed and done in each {checkout.lower} of a pinned {project.lower}
  Given a pinned {project.lower} has a {checkout.lower} with a file changed and a commit that is not on the remote
  And another {checkout.lower} whose branch was merged into the remote's main
  And a third on a new branch with no commits of its own
  When the app follows the {session.lowerPlural}
  Then the first is sent with one change and one commit not pushed
  And the second is sent as merged
  And the third is not
```

<!-- intent: the panel is a short, readable list of the pinned and running projects, each with one
     light for running and for Claude waiting; pointing at a project or clicking it shows that
     project on the right (D-025). Laid out by the app from the feed, so it is checked by the app's
     tests. -->

```gherkin
@APP-LIST-004 @status:proposed @persona:Developer
Scenario: The panel lists one row per {project.lower}, each with one light for all its {session.lowerPlural}
  Given the pinned {project.lower} beta, with no running {session.lower}
  And alpha with two running {session.lowerPlural}, one waiting for the developer
  And gamma with one running {session.lower}
  When the panel lists them
  Then beta is first, its light off, then alpha with Claude's mark and gamma with a running light
  And alpha shows both of its {checkout.lowerPlural} when it is chosen
```

### Feature: Repositories

<!-- intent: the app shows every repository there is, grouped by category, including those on
     GitHub and on this machine that are not registered with engineroom yet -->

```gherkin
@APP-REPOS-001 @status:proposed @persona:Developer
Scenario: Every repository of a {root.lower} is listed with where it is
  Given a {root.lower} whose owner has one repository on GitHub that is not here
  And one cloned in the {root.lower} and registered in the {category.lower} "tools"
  And a git repository in the folder of the {root.lower} that is not registered
  When the app lists the repositories
  Then each is listed under the {root.lower}
  And they are marked on GitHub but not here, registered in "tools", and here but not registered
```

<!-- intent: as APP-REPOS-001 - "a registered menu that can register new ones" -->

```gherkin
@APP-REPOS-002 @status:proposed @persona:Developer
Scenario: A repository here that is not registered is registered from the app
  Given a git repository in the folder of a {root.lower} that is not registered
  When the developer registers it from the app in the {category.lower} "tools"
  Then it is a {project.lower} in the {category.lower} "tools"
  And the command that registered it is in the history
```

<!-- intent: hiding is for repositories that are not registered: a repository on GitHub that will
     never be worked on is kept out of the list, and nothing changes on GitHub -->

```gherkin
@APP-REPOS-003 @status:proposed @persona:Developer
Scenario: A repository on GitHub that is not here can be hidden from the list, and shown again
  Given a {root.lower} whose owner has a repository on GitHub that is not here
  When the developer hides it from the app
  Then it is marked hidden in the list
  And once they show it again it is not
```

<!-- intent: a repository is created or brought in from the app, with the place and the category it
     goes into chosen there -->

```gherkin
@APP-ADD-001 @status:proposed @persona:Developer
Scenario: A repository is got from GitHub into a chosen {root.lower} and {category.lower}
  Given a {root.lower} whose owner has a repository called "widget" that is not here
  When the developer gets "widget" from the app into that {root.lower}, in the {category.lower} "tools"
  Then it is cloned into the {root.lower} as "widget"
  And it is a {project.lower} in the {category.lower} "tools"
```

<!-- intent: as APP-ADD-001 -->

```gherkin
@APP-ADD-002 @status:proposed @persona:Developer
Scenario: A new repository is created from the app in a chosen {root.lower} and {category.lower}
  Given the owner of a {root.lower} has no repository called "gadget"
  When the developer creates "gadget" from the app as a public repository, in the {category.lower} "tools"
  Then a public repository "gadget" is created for the owner on GitHub, holding its first commit
  And it is a {project.lower} in the {category.lower} "tools"
```

### Feature: Starting a session

<!-- intent: a session can be started to join in a terminal, with Remote Control only, or both;
     Claude always runs inside Zellij, so anything started can be joined from another machine
     (D-008) -->

```gherkin
@APP-START-001 @status:proposed @persona:Developer
Scenario: Starting in Zellij runs Claude Code in a {session.lower} and opens a terminal joined to it
  Given a {project.lower} with no {session.lower}
  When the developer starts it from the app in Zellij
  Then a {session.lower} runs Claude Code inside Zellij, without Remote Control
  And a window of the chosen terminal is opened, joined to that {session.lower}
```

<!-- intent: as APP-START-001 -->

```gherkin
@APP-START-002 @status:proposed @persona:Developer
Scenario: Starting with Remote Control runs the {session.lower} and opens no window
  Given a {project.lower} with no {session.lower}
  When the developer starts it from the app with Remote Control
  Then a {session.lower} runs Claude Code inside Zellij, with Remote Control under the name of the {session.lower}
  And no terminal window is opened
```

<!-- intent: as APP-START-001 -->

```gherkin
@APP-START-003 @status:proposed @persona:Developer
Scenario: Starting with both runs the {session.lower} with Remote Control and opens a terminal
  Given a {project.lower} with no {session.lower}
  When the developer starts it from the app with both
  Then a {session.lower} runs Claude Code inside Zellij, with Remote Control under the name of the {session.lower}
  And a window of the chosen terminal is opened, joined to that {session.lower}
```

### Feature: VS Code

<!-- intent: docs/decisions.md D-007 - the app knows whether this machine is the engine or a
     client, and opens VS Code on the folder directly or over SSH to match -->

```gherkin
@APP-EDIT-001 @status:proposed @persona:Developer
Scenario: {editor} opens a {project.lower} directly on the {engine.lower}, and over SSH from a {client.lower}
  Given a {project.lower} registered on the {engine.lower}
  When the developer opens it in {editor} from the app on the {engine.lower}, and from the app on a {client.lower}
  Then on the {engine.lower}, {editor} opens the folder of the {project.lower} itself
  And on the {client.lower}, {editor} opens the folder on the {engine.lower} over SSH
```

### Feature: The terminal sessions are joined in

<!-- intent: the developer chooses the terminal from those installed on this machine, with Ghostty
     the default (D-008) -->

```gherkin
@APP-TERM-001 @status:proposed @persona:Developer
Scenario: The terminal is chosen from the ones installed, Ghostty by default, and remembered
  Given Ghostty and Terminal are the terminals installed on this Mac
  When the developer looks at which terminal {session.lowerPlural} are joined in
  Then only Ghostty and Terminal are offered, and Ghostty is chosen
  And once the developer chooses Terminal, {session.lowerPlural} are joined in Terminal from then on
```

### Feature: Seeing and finding what was done

<!-- intent: CLAUDE.md "the program never hides what it does", for the app as for the terminal -->

```gherkin
@APP-PREVIEW-001 @status:proposed @persona:Developer
Scenario: Creating a repository from the app shows its commands before anything is made
  Given the owner of a {root.lower} has no repository called "gadget"
  When the developer asks the app to create "gadget"
  Then the commands it would run are shown, in order
  And no folder is made and nothing is created on GitHub
```

<!-- intent: as APP-PREVIEW-001 - everything done from the app is in the same history -->

```gherkin
@APP-HIST-001 @status:proposed @persona:Developer
Scenario: What the app did is in the history
  Given the developer started a {session.lower} from the app
  When they open the history
  Then the start is listed with each of its commands and their outcomes
```

### Feature: A repository's page

<!-- intent: clicking a project opens its page, where everything that can be done is in sight: the
     repository, then each checkout with its own join or start, editor, stack and ports (D-016) -->

```gherkin
@APP-PAGE-001 @status:proposed @persona:Developer
Scenario: A {project.lower}'s page shows the main clone and each {checkout.lower} with its own ports and links
  Given a {project.lower} with ports, a running {session.lower} in its main clone, and a {checkout.lower} with no {session.lower}
  When the developer opens the {project.lower}'s page
  Then the main clone is listed first, as running, with slot 1's ports and the links its stack serves
  And the {checkout.lower} is listed with its branch, as not running, with its own slot's ports
  And the {project.lower}'s scripts are listed with where each comes from
```

<!-- intent: the repositories are listed in sections (pinned, registered, not registered), and a
     project no longer worked on can be unregistered from its page (D-016) -->

```gherkin
@APP-PAGE-002 @status:proposed @persona:Developer
Scenario: Pinning and categorising a {project.lower} from its page changes how it is listed
  Given a registered {project.lower} with no {category.lower}
  When the developer pins it and gives it the {category.lower} "tools" from its page
  Then the {project.lower} is listed as pinned, under "tools"
  And nothing on disk is changed
```

<!-- intent: as APP-PAGE-002 -->

```gherkin
@APP-PAGE-003 @status:proposed @persona:Developer
Scenario: Unregistering a {project.lower} from its page forgets it and keeps its folder
  Given a registered {project.lower}
  When the developer unregisters it from its page
  Then it is no longer a registered {project.lower}
  And its folder is still there
```

<!-- intent: every address a running stack serves is a link in the app, because an address cannot
     be copied out of a terminal reached over SSH -->

```gherkin
@APP-PAGE-004 @status:proposed @persona:Developer
Scenario: A running {session.lower} carries the addresses its stack serves, filled from its slot
  Given a {project.lower} whose settings name the link "app" as https on its "ui" port
  When the developer lists the {session.lowerPlural}
  Then the running {session.lower} carries the link "app" with its slot's "ui" port
```

<!-- intent: one GitHub line per project links to its code, its issues and its pull requests -->

```gherkin
@APP-PAGE-005 @status:proposed @persona:Developer
Scenario: A {project.lower} on GitHub links to its code, issues and pull requests
  Given a {project.lower} whose origin is the GitHub repository acme/widget
  When the developer opens its page
  Then the page links to the code, the issues and the pull requests of acme/widget on GitHub
```

<!-- intent: as APP-PAGE-005 - a {checkout.lower}'s own branch is where its work is, and a link
     to a branch GitHub does not have would open nothing -->

```gherkin
@APP-PAGE-006 @status:proposed @persona:Developer
Scenario: A {checkout.lower} links to its branch on GitHub only once GitHub has it
  Given a {project.lower} on GitHub with a {checkout.lower} on the branch feature-x, sent to GitHub
  And a {checkout.lower} on the branch local-only, which GitHub does not have
  When the developer opens the {project.lower}'s page
  Then the {checkout.lower} on feature-x links to that branch's code and its pull requests on GitHub
  And the {checkout.lower} on local-only has no GitHub links
```

<!-- intent: as APP-PAGE-005 - a link is only offered where GitHub holds the repository -->

```gherkin
@APP-PAGE-007 @status:proposed @persona:Developer
Scenario: A {project.lower} that is not on GitHub has no GitHub links
  Given a {project.lower} with no origin
  When the developer opens its page
  Then neither the page nor its {checkout.lower} links to GitHub
```

### Feature: Asking Claude about what is open

<!-- intent: one button opens a Claude session that already knows what was open (the project, the
     checkout, its ports and scripts) and how engineroom wants things done, so the developer says
     what they want without explaining the setup (D-018) -->

```gherkin
@APP-ASK-001 @status:proposed @persona:Developer
Scenario: Asking Claude about a {checkout.lower} opens a {session.lower} that already knows it
  Given a {project.lower} with ports, whose main clone the developer is looking at in the app
  When they ask Claude about it
  Then a {session.lower} of its own starts in that {checkout.lower}, beside its main {session.lower}
  And Claude is told the {project.lower}, the {checkout.lower}, its ports and where its settings are
  And Claude is asked to say what it sees and ask what to do
```

### Feature: Upgrading engineroom

<!-- intent: an Upgrade button starts a Claude session on this machine that pulls, reinstalls and
     reopens the app; running the upgrade through Claude means a failure can be diagnosed and
     corrected where it happens. The session is told the steps, the clone the running build was made
     from, and what it may fix by itself and what it must ask about first -->

```gherkin
@APP-UPGRADE-001 @status:proposed @persona:Developer
Scenario: Upgrading this machine opens a Claude {session.lower} that knows how
  Given this machine runs engineroom with its app
  When the developer asks to upgrade this machine
  Then an upgrade {session.lower} starts on this machine
  And Claude is told the steps, this machine's build and where its app is
  And Claude is asked to upgrade it now
```

<!-- intent: as APP-UPGRADE-001 - the version check names the machine that is behind, and a {client.lower}
     upgrades the {engine.lower} without the developer at it -->

```gherkin
@APP-UPGRADE-002 @status:proposed @persona:Developer
Scenario: A {client.lower} upgrades the {engine.lower} through a {session.lower} on the {engine.lower}
  Given this machine is a {client.lower} of an {engine.lower} that answers
  When the developer asks to upgrade the {engine.lower}
  Then an upgrade {session.lower} starts on the {engine.lower}
  And nothing is started on this machine
```

<!-- intent: pressing Upgrade must never join an upgrade {session.lower} left from an earlier press,
     where nothing would happen. Each press is a new upgrade, from the build now installed -->

```gherkin
@APP-UPGRADE-003 @status:proposed @persona:Developer
Scenario: Upgrading again starts a new upgrade {session.lower}
  Given an upgrade {session.lower} from an earlier upgrade is still on this machine
  When the developer asks to upgrade this machine
  Then the earlier upgrade {session.lower} is ended and a new one starts
  And Claude is asked to upgrade it now
```

### Feature: A project's own tasks

<!-- intent: a project names its own scripts as tasks the app can run (for example, copying a
     database for testing) -->

```gherkin
@APP-TASK-001 @status:proposed @persona:Developer
Scenario: A task named in a {project.lower}'s settings runs in a {checkout.lower} from the app
  Given a {project.lower} whose settings name a task
  When the developer runs that task on its main clone from the app
  Then the task's command runs in that {checkout.lower}, with the slot's ports
  And it is listed on the {project.lower}'s page
```

### Feature: Opening at login

<!-- intent: the app can start at login -->

```gherkin
@APP-LOGIN-001 @status:proposed @persona:Developer
Scenario: The app can open at login
  Given the app is installed
  When the developer turns on opening at login
  Then the app is registered with macOS to open at login
  And turning it off unregisters it
```
