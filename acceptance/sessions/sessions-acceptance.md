# Sessions - acceptance criteria

**Scope**: seeing, joining, opening, switching, stopping and bringing back {session.lowerPlural}
on the {engine.lower}, and starting them with Claude Code Remote Control. Out of scope: creating new {checkout.lowerPlural} for new work, ports, and
opening an editor, which have their own areas.

**Sourcing**: every scenario's intent is stated in its own comment, or in the decisions it cites.
Nothing here is inferred from software.

**Status**: every scenario is `proposed`, written at planning before the software exists.
`proposed` is a proposal to be confirmed, `derived` is written up from delivered software,
`verified` means a person watched it happen, and `accepted` means it is confirmed as what the
software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Finds, joins, opens, switches between, stops and brings back {session.lowerPlural}, from the {engine.lower} or a {client.lower} |

---

## User stories

> **As a** developer who runs several pieces of work at once, **I want** one place that shows
> every {session.lower} on the {engine.lower} and what each needs from me, **so that** none sits
> waiting for an answer I did not know it was asking.

- Every {session.lower} is shown with what it is working in and whether it is running.
- A {session.lower} that is waiting for the developer is impossible to miss.

> **As a** developer moving between machines, **I want** to join the same {session.lower} from
> anywhere and leave it running, **so that** the work carries on whichever machine I am at.

- Joining shows the {session.lower} as it is; leaving never stops it.
- Opening a {project.lower} never makes a second {session.lower} for the same
  {checkout.lower}.

> **As a** developer who starts work and walks away from it, **I want** a {session.lower} to be
> reachable from the Claude app under its own name, and to start one without joining it, **so
> that** I can carry on from the app, where pasting images and text works, wherever I am.

- Whether a new {session.lower} uses Remote Control is one remembered choice, made before opening.
- The name in the Claude app is the {session.possessive} own name.

> **As a** developer, **I want** to stop a {session.lower} on purpose and bring back one that
> ended without my asking, **so that** a restart of the {engine.lower} costs me nothing and a stop
> is never an accident.

---

## Gherkin scenarios

### Feature: Seeing sessions

<!-- intent: the tool knows every session and where it belongs: the list shows each one with its
     project, worktree and state -->

```gherkin
@SESS-LIST-001 @status:proposed @persona:Developer
Scenario: Every {session.lower} on the {engine.lower} is listed
  Given the {engine.lower} has {session.lowerPlural} for two {project.lowerPlural}
  When the developer opens the list of {session.lowerPlural}
  Then each {session.lower} is listed with its {project.lower}, its {checkout.lower} and whether it is running
```

<!-- intent: a session waiting for an answer is shown with its question, as a mark on its own line;
     the list keeps its fixed order and a waiting session is never moved (D-022) -->

```gherkin
@SESS-LIST-002 @status:proposed @persona:Developer
Scenario: A {session.lower} waiting for the developer keeps its place, marked with Claude's mark
  Given one {session.lower} has stopped to ask the developer a question
  When the developer opens the list of {session.lowerPlural}
  Then that {session.lower} is listed where it would be if it were not waiting
  And it is marked with Claude's mark as waiting
  And the question it asked is shown beside it
```

<!-- intent: as SESS-LIST-001 - whether a session is running is part of what is listed, so an
     ended one must never read as running -->

```gherkin
@SESS-LIST-003 @status:proposed @persona:Developer
Scenario: An ended {session.lower} is shown as ended
  Given a {session.lower} has ended
  When the developer opens the list of {session.lowerPlural}
  Then it is listed as ended
  And it is not shown as running
```

<!-- intent: an empty list has to lead somewhere -->

```gherkin
@SESS-LIST-004 @status:proposed @persona:Developer
Scenario: With no {session.lowerPlural}, the list offers to open a {project.lower}
  Given the {engine.lower} has no {session.lowerPlural}
  When the developer opens the list of {session.lowerPlural}
  Then they are told there are no {session.lowerPlural}
  And they are offered the registered {project.lowerPlural} to open
```

<!-- intent: pinned projects are always there and always first, and the order is fixed, so nothing
     moves between two looks (D-022) -->

```gherkin
@SESS-LIST-005 @status:proposed @persona:Developer
Scenario: The pinned {project.lowerPlural}' {session.lowerPlural} are listed first, then the rest by name
  Given running {session.lowerPlural} for alpha, beta and gamma, with gamma pinned
  When the developer opens the list of {session.lowerPlural}
  Then gamma's {session.lower} is listed first
  And alpha's and beta's follow it, by name
```

### Feature: Joining a session

<!-- intent: Claude always runs inside Zellij, so the same session can be reached from the engine or
     from a client -->

```gherkin
@SESS-JOIN-001 @status:proposed @persona:Developer
Scenario: Joining from a {client.lower} shows the {engine.possessive} {session.lower}
  Given a {session.lower} is running on the {engine.lower}
  When the developer joins it from a {client.lower}
  Then they see that {session.lower} as it is on the {engine.lower}
```

<!-- intent: as SESS-JOIN-001 - the same session is reached from either machine, so leaving it
     from one must not end it -->

```gherkin
@SESS-JOIN-002 @status:proposed @persona:Developer
Scenario: Leaving a {session.lower} keeps it running
  Given the developer has joined a {session.lower} from a {client.lower}
  When they leave it
  Then the {session.lower} is still running on the {engine.lower}
  And the work it was doing carries on
```

### Feature: Opening a project

<!-- intent: Claude Code is always started inside Zellij, never directly in a terminal. On a
     small screen it gets the whole tab, with no extra terminal panes, since Zellij opens a tab
     whenever one is needed and the repository's own scripts start its stack -->

```gherkin
@SESS-NEW-001 @status:proposed @persona:Developer
Scenario: A {project.lower} with no {session.lower} gets one with the standard layout
  Given a {project.lower} has no {session.lower} on the {engine.lower}
  When the developer opens it
  Then a {session.lower} is created in its {checkout.lower}
  And it holds Claude Code on its own tab, and a place for git
```

<!-- intent: as SESS-JOIN-001 - one session per checkout, reached from anywhere -->

```gherkin
@SESS-NEW-002 @status:proposed @persona:Developer
Scenario: Opening a {project.lower} that has a {session.lower} joins it
  Given a {project.lower} already has a running {session.lower}
  When the developer opens it
  Then they join that {session.lower}
  And no second {session.lower} is created
```

<!-- intent: names need not be remembered; a name the tool does not know is answered with the names
     it does -->

```gherkin
@SESS-NEW-003 @status:proposed @persona:Developer
Scenario: Opening a {project.lower} that is not registered names the ones that are
  Given no {project.lower} called "acme-site" is registered
  When the developer opens "acme-site"
  Then they are told it is not a registered {project.lower}
  And they are shown the registered {project.lowerPlural}
  And no {session.lower} is created
```

<!-- intent: a session can be started without a Zellij window open at all, because Remote Control
     is used once the session runs; joining it in a terminal is optional -->

```gherkin
@SESS-NEW-004 @status:proposed @persona:Developer
Scenario: A {project.lower} can be started without joining its {session.lower}
  Given a {project.lower} has no {session.lower} on the {engine.lower}
  When the developer starts it without joining
  Then a {session.lower} is created in its {checkout.lower} and is running
  And the developer stays where they were, shown the list of {session.lowerPlural}
```

<!-- intent: a {session.lower} started by engineroom from inside a Claude Code session (Claude
     running engineroom, or the app opened from one) must not be given that session's markers, with
     which its agent would save no transcript. A {session.lower}'s agent is the developer's own,
     whatever started it -->

```gherkin
@SESS-NEW-005 @status:proposed @persona:Developer
Scenario: A {session.lower} started by a running Claude Code agent has an agent of its own
  Given the program is run by a Claude Code agent, with that agent's markers in its environment
  When it starts a {project.lower}'s {session.lower}
  Then the agent in that {session.lower} is given none of those markers
```

<!-- intent: a {session.lower} has the rights it would have if the developer had started it at the
     machine itself: one started from another machine, over SSH, would stay in that SSH login, where
     the login keychain is locked (D-031). The developer leaves the {engine.lower} logged in at its
     screen; with no one logged in there, a {session.lower} starts as before -->

```gherkin
@SESS-NEW-006 @status:proposed @persona:Developer
Scenario: A {session.lower} started from outside the {engine.possessive} desktop login runs inside it
  Given the developer is logged in at the {engine.possessive} screen
  And the program runs on the {engine.lower} outside that login, as it does over SSH
  When it starts a {project.lower}'s {session.lower}
  Then what runs in the {session.lower} runs inside the developer's desktop login
  And nothing of how it was started there is left running or loaded
```

### Feature: Remote Control

<!-- intent: Remote Control is a switch set before a session is opened, and remembered; it is where
     pasting images works, so the choice is made once and kept -->

```gherkin
@SESS-REMOTE-001 @status:proposed @persona:Developer
Scenario: The Remote Control choice is shown before opening and remembered
  Given the developer is choosing a {project.lower} to open
  When they turn Remote Control on
  Then the choice is shown as on
  And it is still on the next time the program is started
```

<!-- intent: engineroom starts the Claude session inside Zellij under the same name, with Remote
     Control -->

```gherkin
@SESS-REMOTE-002 @status:proposed @persona:Developer
Scenario: A {session.lower} created with Remote Control on is reachable under its own name
  Given Remote Control is on
  And a {project.lower} has no {session.lower} on the {engine.lower}
  When the developer opens it
  Then Claude Code in the new {session.lower} starts with Remote Control
  And the name it is reached by is the {session.possessive} name
```

<!-- intent: as SESS-REMOTE-001 - the choice is made on the machine the developer is at, so the
     engine's own setting never decides for a client -->

```gherkin
@SESS-REMOTE-003 @status:proposed @persona:Developer
Scenario: On a {client.lower}, the {client.possessive} own choice decides
  Given Remote Control is on for the {client.lower} and off on the {engine.lower}
  When the developer opens a {project.lower} with no {session.lower} from the {client.lower}
  Then Claude Code in the new {session.lower} starts with Remote Control
```

<!-- intent: as SESS-REMOTE-001 - the choice is about starting a session; a running session is
     joined as it is, and is never restarted to apply it -->

```gherkin
@SESS-REMOTE-004 @status:proposed @persona:Developer
Scenario: The Remote Control choice never changes a running {session.lower}
  Given a {session.lower} is running without Remote Control
  And Remote Control is on
  When the developer opens its {project.lower}
  Then they join that {session.lower} as it is
  And it is not restarted
```

### Feature: Switching sessions

<!-- intent: a session can be left for another from inside it, without going back to a shell -->

```gherkin
@SESS-SWITCH-001 @status:proposed @persona:Developer
Scenario: From inside a {session.lower}, the developer moves to another
  Given the developer is in one {session.lower}
  When they choose another {session.lower} from inside it
  Then this window shows the other {session.lower}
  And the one they left is still running
```

### Feature: Stopping a session

<!-- intent: any running session can be stopped from the tool, and nothing is stopped without a
     clear ask -->

```gherkin
@SESS-STOP-001 @status:proposed @persona:Developer
Scenario: Stopping a {session.lower} asks first
  Given a {session.lower} is running
  When the developer chooses to stop it
  Then they are asked to confirm before anything is stopped
```

<!-- intent: as SESS-STOP-001 -->

```gherkin
@SESS-STOP-002 @status:proposed @persona:Developer
Scenario: A confirmed stop ends only that {session.lower}, fully
  Given two {session.lowerPlural} are running
  When the developer stops one of them and confirms
  Then that {session.lower} is gone, with nothing of it kept to bring back
  And the other is still running
```

<!-- intent: as SESS-STOP-001 -->

```gherkin
@SESS-STOP-003 @status:proposed @persona:Developer
Scenario: Declining the confirmation leaves the {session.lower} running
  Given the developer has been asked to confirm stopping a {session.lower}
  When they decline
  Then the {session.lower} is still running
```

### Feature: Starting a session again

<!-- intent: a session is running or it is not there, so one that ended (the engine restarted, or it
     was quit from inside Zellij) starts fresh from the current layout, never as Zellij's old
     snapshot (D-015) -->

```gherkin
@SESS-BACK-001 @status:proposed @persona:Developer
Scenario: Starting a {session.lower} that ended starts it fresh from the current layout
  Given a {session.lower} with a layout of its own ended because the {engine.lower} restarted
  When the developer starts it again
  Then it opens with the current standard layout
  And not with the tabs and panes it had
```

<!-- intent: the work is the conversation, so starting a session again without it would lose what
     mattered -->

```gherkin
@SESS-BACK-002 @status:proposed @persona:Developer
Scenario: Starting a {session.lower} again continues the Claude Code conversation
  Given a {session.lower} ended because the {engine.lower} restarted
  When the developer starts it again
  Then Claude Code continues the conversation it was having
  And a new, empty conversation is not started in its place
```

<!-- intent: a running session on an old layout is put on the current one from the app, with no
     terminal command (D-015) -->

```gherkin
@SESS-BACK-003 @status:proposed @persona:Developer
Scenario: Restarting a running {session.lower} puts it on the current layout
  Given a {session.lower} with a layout of its own is running
  When the developer restarts it from the app
  Then it runs with the current standard layout
  And Claude Code continues the conversation it was having
```

<!-- intent: a restarted session must stay within reach: a window joined to a session from a
     client is left on the session list when the session is restarted, and the next join brings
     that window forward -->

```gherkin
@SESS-BACK-004 @status:proposed @persona:Developer
Scenario: Restarting a {session.lower} joined from a {client.lower} ends the join in its window
  Given the developer has joined a {session.lower} from a {client.lower}, in a window opened only to join it
  When they restart it from the app
  Then the program in that window ends, rather than staying open on the {session.lower} list
  And the {session.lower} is running again on the {engine.lower}
```

### Feature: Using the mouse

<!-- intent: the terminal interface answers the mouse: a row is chosen by clicking it, and
     acted on by clicking it again, as enter does -->

```gherkin
@SESS-MOUSE-001 @status:proposed @persona:Developer
Scenario: Clicking a row selects it, and clicking the selected row acts on it
  Given two {session.lowerPlural} are running
  When the developer clicks the second {session.lower} in the list
  Then the second {session.lower} is selected
  And clicking it again joins it
```

<!-- intent: as SESS-MOUSE-001 - the wheel moves through a list as the arrow keys do -->

```gherkin
@SESS-MOUSE-002 @status:proposed @persona:Developer
Scenario: The scroll wheel moves the selection
  Given three {session.lowerPlural} are running and the first is selected
  When the developer scrolls down twice over the list
  Then the third {session.lower} is selected
  And scrolling up once selects the second
```

