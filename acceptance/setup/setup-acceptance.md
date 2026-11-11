# Setup - acceptance criteria

**Scope**: `engineroom setup check`, which reports what this machine needs for its role and lacks,
with how to fix each item, and changes nothing. The items are the ones `docs/setup.md` lists for
every machine, for the {engine.lower} and for a {client.lower}, as far as a program can check them
without asking anything. A {client.lower} mirrors the {engine.lower}: it uses the {engine.lower},
and works on its own when the {engine.lower} does not answer, so every item for running sessions
is an item every machine needs (Zellij 0.45 or later, git, OpenSSH, Claude Code with
engineroom's hooks, at least one project registered, a role, a valid configuration). Only the
{engine.lower} needs to be reached from other machines (the program found by an SSH command, the
SSH server on); only a {client.lower} needs to reach the {engine.lower} and open it in {editor}
(the {engine.lower} answering without a prompt, the `code` command, the Remote - SSH extension,
the {engine.lower}'s name in {editor}'s SSH configuration). Out of scope: applying a fix (the `setup-machine` skill does that, one
fix at a time, after showing it), installing the program itself, and items the guide marks as
recommended rather than required.

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
| **Developer** | Sets up a machine as the {engine.lower} or a {client.lower}, and checks it again after a change |

---

## User stories

> **As a** developer setting up a machine, **I want** one command that says what this machine
> still lacks for its role and how to fix each thing, **so that** I do not have to work through
> the setup guide by hand, and a machine that is missing something says so before an action fails.

- Every item for the machine's role is reported, present or missing, and nothing is left out
  silently.
- A missing item comes with the exact command or settings change that fixes it.
- The check changes nothing, so it can be run at any time, as often as wanted.
- It runs without Claude Code, so anyone who installs the program can use it.

> **As a** developer whose machine is set up by Claude, **I want** Claude to follow what the program
> reports, **so that** what counts as set up is decided in one place, not by each conversation.

- The `setup-machine` skill runs the check and works from its report.
- The report can be read by a program as well as a person.

---

## Gherkin scenarios

### Feature: Checking a machine

<!-- intent: one report says what this machine lacks for its role; "nothing is left out silently"
     means every item is listed, the present ones as well -->

```gherkin
@SETUP-CHECK-001 @status:proposed @persona:Developer
Scenario: Every item for this machine's role is reported as present or missing
  Given this machine is the {engine.lower}
  When the developer runs the setup check
  Then every item every machine needs, and every item the {engine.lower} needs, is listed
  And each is marked present or missing
  And no item a {client.lower} needs is listed
```

<!-- intent: as SETUP-CHECK-001, for the other role -->

```gherkin
@SETUP-CHECK-002 @status:proposed @persona:Developer
Scenario: A {client.lower} is checked for what a {client.lower} needs
  Given this machine is a {client.lower}
  When the developer runs the setup check
  Then every item every machine needs, and every item a {client.lower} needs, is listed
  And no item only the {engine.lower} needs is listed
```

<!-- intent: CLAUDE.md "It may report a problem and show the fix; it applies a fix only on an
     explicit request" - the check is the report, never the fix -->

```gherkin
@SETUP-CHECK-003 @status:proposed @persona:Developer
Scenario: A missing item is shown with its fix, and nothing is changed
  Given Zellij is not installed on this machine
  When the developer runs the setup check
  Then Zellij is marked missing, with the version needed and the command that installs it
  And nothing on the machine has changed
```

<!-- intent: CLAUDE.md "The program never hides what it does" - the check runs commands too, and
     each item says how it was checked, so the developer can check it again by hand -->

```gherkin
@SETUP-CHECK-004 @status:proposed @persona:Developer
Scenario: Each item says how it was checked
  Given this machine is the {engine.lower}
  When the developer runs the setup check
  Then each item shows the command it was checked with, or the file it was read from
```

<!-- intent: CONN-SETUP-001 - a machine with no role is told how to set one, and what can be checked
     without a role is still checked -->

```gherkin
@SETUP-CHECK-005 @status:proposed @persona:Developer
Scenario: A machine with no role is told how to set one
  Given this machine has no role set
  When the developer runs the setup check
  Then the role is marked missing, with the command that sets each role
  And every item every machine needs is still listed, present or missing
```

<!-- intent: connection obligation O7 - a configuration that cannot be read is named with its file
     and field, never guessed around -->

```gherkin
@SETUP-CHECK-006 @status:proposed @persona:Developer
Scenario: A configuration that is not valid is reported with its file and field
  Given this machine's configuration names an unknown role
  When the developer runs the setup check
  Then the configuration is marked missing, naming the file and the field that is wrong
```

<!-- intent: docs/setup.md "the most common first-install failure" - a client runs the program on
     the engine through a shell that reads neither ~/.zprofile nor ~/.zshrc -->

```gherkin
@SETUP-CHECK-007 @status:proposed @persona:Developer
Scenario: On the {engine.lower}, the program not found by an SSH command is reported with the line that fixes it
  Given this machine is the {engine.lower}
  And the program's folder is on the PATH only for interactive shells
  When the developer runs the setup check
  Then the program is marked missing for SSH commands, with the line to add to the file every shell reads
```

<!-- intent: D-034 - the engine not reached is said with its cause, and the check says the same as
     every action would -->

```gherkin
@SETUP-CHECK-008 @status:proposed @persona:Developer
Scenario: On a {client.lower}, an {engine.lower} that is not reached is reported with its cause
  Given this machine is a {client.lower}
  And the {engine.lower}'s host key has changed
  When the developer runs the setup check
  Then the {engine.lower} is marked missing, said as a changed host key, with what to do
```

<!-- intent: docs/setup.md "The engine's name known to VS Code" - the editor resolves the engine
     through its own SSH configuration, which may not be the one the program uses -->

```gherkin
@SETUP-CHECK-009 @status:proposed @persona:Developer
Scenario: On a {client.lower}, an {engine.lower} unknown to {editor}'s SSH configuration is reported
  Given this machine is a {client.lower}
  And the SSH configuration {editor} uses has no entry for the {engine.lower}
  When the developer runs the setup check
  Then the {engine.lower}'s name is marked missing for {editor}, naming the file {editor} reads
```

### Feature: Reading the report

<!-- intent: the check is for scripts and the skill as well as people: whether anything is missing
     is the exit status, not words to parse -->

```gherkin
@SETUP-READ-001 @status:proposed @persona:Developer
Scenario: The exit status says whether anything is missing
  Given every item for this machine's role is present
  When the developer runs the setup check
  Then it ends with success
  And when one item is missing, it ends with a failure
```

<!-- intent: the skill and the health view (#8) read the same report, so what counts as set up is
     decided once -->

```gherkin
@SETUP-READ-002 @status:proposed @persona:Developer
Scenario: The report can be read as JSON with the same items
  Given this machine is the {engine.lower}
  When the developer runs the setup check as JSON
  Then each item has its name, whether it is present, how it was checked and its fix
  And the items are the same as the report on screen
```
