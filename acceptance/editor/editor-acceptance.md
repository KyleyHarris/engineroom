# Editor - acceptance criteria

**Scope**: opening a {session.lower}'s {checkout.lower} in {editor} from the {session.lower} list or the
command line, on whichever machine the developer is at. Out of scope: installing {editor} or
its remote extension, and forwarding a {checkout.lower}'s ports, which is the ports area.

**Sourcing**: every scenario's intent was stated by the architect in conversation on 2026-11-04
(an open-in-{editor} action per {session.lower}), in issue #11, or in the decisions it cites.
Nothing here is inferred from software.

**Status**: every scenario is `proposed`, written at planning before the software exists.
`proposed` is a proposal to be confirmed, `derived` is written up from delivered software,
`verified` means a person watched it happen, and `accepted` means it is confirmed as what the
software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Opens a {session.lower}'s files in {editor}, from the {engine.lower} or a {client.lower} |

---

## User stories

> **As a** developer, **I want** to open the files of the {session.lower} I am looking at in
> {editor} in one action, **so that** I read and debug the same {checkout.lower} Claude Code is
> working in, without finding its folder myself.

- On the {engine.lower}, {editor} opens the {checkout.lower}'s folder directly.
- On a {client.lower}, {editor} opens the {engine.lower}'s folder over SSH, so the {engine.lower}'s
  files and toolchain are used and nothing is copied.
- The command that opens {editor} is shown, like every other action.

---

## Gherkin scenarios

### Feature: Opening a session's worktree in the editor

<!-- intent: issue #11 - "On the hub, VS Code opens the worktree directly. The command it runs is
     shown, like every other action." -->

```gherkin
@EDIT-OPEN-001 @status:proposed @persona:Developer
Scenario: On the {engine.lower}, {editor} opens the {session.lower}'s {checkout.lower}
  Given a {session.lower} is running in a {checkout.lower} on the {engine.lower}
  When the developer opens it in {editor} from the {session.lower} list
  Then {editor} is started on that {checkout.lower}'s folder
  And the command that started it is shown
```

<!-- intent: issue #11 - "On a client, it opens the same worktree on the hub over Remote-SSH, so I
     read and debug the hub's files with the hub's toolchain." -->

```gherkin
@EDIT-OPEN-002 @status:proposed @persona:Developer
Scenario: On a {client.lower}, {editor} opens the {engine.lower}'s {checkout.lower} over SSH
  Given the developer is on a {client.lower}
  And a {session.lower} is running in a {checkout.lower} on the {engine.lower}
  When the developer opens it in {editor}
  Then {editor} on the {client.lower} is started on the {engine.lower}'s folder for that {checkout.lower}, reached over SSH
  And nothing is started on the {engine.lower}
```

<!-- intent: as EDIT-OPEN-001 - the files outlive the session, and reading them must not restart
     the agent -->

```gherkin
@EDIT-OPEN-003 @status:proposed @persona:Developer
Scenario: An ended {session.lower}'s {checkout.lower} still opens, and the {session.lower} stays ended
  Given a {session.lower} in a {checkout.lower} on the {engine.lower} has ended
  When the developer opens it in {editor} from the {session.lower} list
  Then {editor} is started on that {checkout.lower}'s folder
  And the {session.lower} is still ended
```

<!-- intent: the rule that the program never guesses: a session started outside the tool has no
     registered folder, so there is nothing to open -->

```gherkin
@EDIT-OPEN-004 @status:proposed @persona:Developer
Scenario: A {session.lower} outside every registered {project.lower} is not opened
  Given a {session.lower} is running that does not belong to a registered {project.lower}
  When the developer opens it in {editor} from the {session.lower} list
  Then they are told its folder is not known because it is not in a registered {project.lower}
  And {editor} is not started
```

<!-- intent: CLAUDE.md "It may report a problem and show the fix" - a missing command line is the
     usual first-time setup gap, and the fix is one step in the editor -->

```gherkin
@EDIT-OPEN-005 @status:proposed @persona:Developer
Scenario: {editor}'s command missing on this machine is reported with how to install it
  Given {editor}'s command is not installed on this machine
  When the developer opens a {session.lower} in {editor}
  Then they are told the command was not found on this machine, and how to install it
  And nothing is run
```

<!-- intent: CONN-LOCAL-001 and CONN-LOCAL-002 - working locally acts on this machine's own copy,
     never silently on the engine -->

```gherkin
@EDIT-OPEN-006 @status:proposed @persona:Developer
Scenario: Working locally, {editor} opens this machine's own copy
  Given the developer is working locally on a {client.lower} because the {engine.lower} did not answer
  And a {session.lower} is running in this machine's own copy of a {project.lower}
  When the developer opens it in {editor} from the {session.lower} list
  Then {editor} is started on this machine's folder, not the {engine.lower}'s
```
