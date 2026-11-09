# Learning - acceptance criteria

**Scope**: seeing the commands an action will run, watching them run, and finding them again
afterwards. Applies to every action the tool takes in every area. Out of scope: explaining what a
command does beyond showing it and its result.

**Sourcing**: every scenario's intent was stated by the architect in conversation on 2026-11-04.
Nothing here is inferred from software; none exists yet.

**Status**: every scenario is `proposed`, written at planning before the software exists.
`proposed` is a proposal to be confirmed, `derived` is written up from delivered software,
`verified` means a person watched it happen, and `accepted` means it is confirmed as what the
software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Runs actions, reads the commands behind them, and reuses those commands by hand |

---

## User stories

> **As a** developer who does not want to memorise the tooling, **I want** every action to show
> me the real commands it runs, **so that** I learn the tools over time instead of depending on a
> black box.

- The commands are visible before an action runs, while it runs, and afterwards.
- A command that fails is named, with its error, and the rest are not run silently.
- Every past action can be found again, and its commands reused by hand.

---

## Gherkin scenarios

### Feature: Seeing what an action does

<!-- intent: the architect, 2026-11-04 - "when it does them. show me what was done so i learn
     over time" -->

```gherkin
@LEARN-SHOW-001 @status:proposed @persona:Developer
Scenario: The commands of an action can be seen before it runs
  Given the developer has chosen an action
  When they ask to see what it will do
  Then the commands it will run are listed in the order they will run
  And nothing has run yet
```

<!-- intent: as LEARN-SHOW-001 -->

```gherkin
@LEARN-SHOW-002 @status:proposed @persona:Developer
Scenario: Each command is shown while the action runs
  Given an action is running
  When each of its commands starts and then finishes
  Then the command is shown as it starts
  And its outcome is shown when it finishes
```

<!-- intent: as LEARN-SHOW-001 - showing what was done includes what failed and what never ran -->

```gherkin
@LEARN-SHOW-003 @status:proposed @persona:Developer
Scenario: A failed command stops the action and is named
  Given an action is running
  When one of its commands fails
  Then the action stops at that command
  And that command and its error are shown
  And the commands after it are shown as not run
```

### Feature: Finding it again

<!-- intent: as LEARN-SHOW-001 - "so i learn over time" needs a record that outlasts the screen -->

```gherkin
@LEARN-HIST-001 @status:proposed @persona:Developer
Scenario: Every action is kept in a history
  Given the developer has run actions on this machine
  When they open the history
  Then each action is listed with when it ran, which machine it ran on, its commands and their outcomes
```

<!-- intent: the history is only worth reading if it is true: a join that could not attach was kept
     as done (found 2026-11-07) -->

```gherkin
@LEARN-HIST-003 @status:proposed @persona:Developer
Scenario: A join that fails is kept in the history as failed
  Given the developer joins a {session.lower} and Zellij cannot attach to it
  When they open the history
  Then the join is listed as failed, with what Zellij said
```

<!-- intent: as LEARN-SHOW-001 - learning the tools means being able to run them without the tool -->

```gherkin
@LEARN-HIST-002 @status:proposed @persona:Developer
Scenario: The commands of a past action can be copied and run by hand
  Given an action is in the history
  When the developer copies its commands
  Then they get commands that run as they are when pasted into a terminal
```

### Feature: Copying commands to learn them

<!-- intent: the architect, 2026-11-04 - "commands i want to learn can have a copy line, or copy
     batch button or click": one command or a whole action goes to the clipboard of the terminal
     the developer is looking at, which works over SSH and through Zellij -->

```gherkin
@LEARN-COPY-001 @status:proposed @persona:Developer
Scenario: One command of an action can be copied
  Given the commands of an action are shown
  When the developer copies the second command
  Then exactly that command is sent to the terminal's clipboard
  And they are told it was sent to the terminal's clipboard
```

<!-- intent: as LEARN-COPY-001, and the architect, 2026-11-05, approving "copy all joins the
     commands with &&": a pasted action stops at its first failure, as the program does -->

```gherkin
@LEARN-COPY-002 @status:proposed @persona:Developer
Scenario: All of an action's commands can be copied as one that stops at the first failure
  Given the commands of an action are shown
  When the developer copies all of them
  Then the commands are sent to the terminal's clipboard in order, each joined to the next with "&&"
  And pasted into a shell, they stop at the first command that fails
```

