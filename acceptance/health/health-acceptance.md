# Health - acceptance criteria

**Scope**: `engineroom health`, and the Health page of the app, which say whether the
{engine.lower} is ready to be worked on from away: it is set up (the {engine.lower}'s items from
`engineroom setup check`), and for each running {session.lower}, whether Remote Control is on and whether its {project.possessive} app is running.
Every item says how it was checked, and a missing one shows the command or settings change that
would fix it. Nothing is changed unless the developer asks for that one fix. Whether an app is
running is said by the {project.possessive} own `status` script, as starting and stopping it is
said by its `up` and `down` scripts (D-006); the program never guesses how to check an app. Out of
scope: which {session.lower}s should be running (the running ones are the ones checked), and
whether the {engine.lower} sleeps, which is the decision of the person running it, never checked
or changed (D-042).

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
| **Developer** | Checks the {engine.lower} before leaving it, from the {engine.lower} or from a {client.lower}, and fixes what is missing |

---

## User stories

> **As a** developer about to leave the {engine.lower}, **I want** one view that says whether it is
> ready to be worked on from away, **so that** I find out what is wrong while I can still fix it.

- Each running {session.lower} says whether Remote Control is on, however it was turned on.
- Each running {session.lower} says whether its app is running, as its {project.lower} checks it.
- Every missing item shows its fix, and nothing is changed until the developer asks.

> **As a** developer at a {client.lower}, **I want** the same view of the {engine.lower},
> **so that** I can check it when I am not at it.

---

## Gherkin scenarios

### Feature: Checking the engine

<!-- intent: one view says whether the engine is ready; "nothing is left out silently" means
     every item is listed, the present ones as well, as the setup check does (SETUP-CHECK-001) -->

```gherkin
@HEALTH-CHECK-001 @status:proposed @persona:Developer
Scenario: Every item is reported as present or missing
  Given the {engine.lower} has two running {session.lower}s
  When the developer runs the health check on the {engine.lower}
  Then every item the setup check reports for the {engine.lower} is listed
  And each running {session.lower} is listed with its Remote Control and its app
  And each item is marked present or missing
```

<!-- intent: CLAUDE.md "It may report a problem and show the fix; it applies a fix only on an
     explicit request" and "The program never hides what it does" -->

```gherkin
@HEALTH-CHECK-002 @status:proposed @persona:Developer
Scenario: Each item says how it was checked, and the check changes nothing
  Given a running {session.lower} whose app is not running
  When the developer runs the health check on the {engine.lower}
  Then each item shows the command it was checked with, or the file it was read from
  And the {session.possessive} app is marked missing, with its fix
  And nothing on the {engine.lower} has changed
```

<!-- intent: the developer checks the engine when away from it; a client asks the engine over the
     one SSH command every client action uses, and an engine not reached is said with its cause
     (D-034) -->

```gherkin
@HEALTH-CHECK-003 @status:proposed @persona:Developer
Scenario: On a {client.lower}, the health check is the {engine.possessive}
  Given this machine is a {client.lower}
  When the developer runs the health check
  Then the items are the {engine.possessive}, checked on the {engine.lower}
  And when the {engine.lower} is not reached, that is said with its cause and no item is marked present
```

### Feature: Remote Control

<!-- intent: Remote Control is turned on when engineroom starts a session (SESS-REMOTE-002) or by
     Claude inside the session (/remote-control); either way it is on, so the check reads the
     running agent, not how it was started -->

```gherkin
@HEALTH-REMOTE-001 @status:proposed @persona:Developer
Scenario: A {session.possessive} Remote Control is shown however it was turned on
  Given a {session.lower} started with Remote Control
  And a {session.lower} started without it, where Remote Control was then turned on inside it
  And a {session.lower} without Remote Control
  When the developer runs the health check
  Then the first two are shown with Remote Control on
  And the third is shown with Remote Control off
```

<!-- intent: Remote Control is wanted where the developer has turned it on; the engine's
     remembered choice (SESS-REMOTE-001) says so. A running session is never restarted to apply it
     (SESS-REMOTE-004), so the fix is the command inside the session -->

```gherkin
@HEALTH-REMOTE-002 @status:proposed @persona:Developer
Scenario: With Remote Control chosen, a {session.lower} without it is reported
  Given Remote Control is on in the {engine.possessive} choice
  And a {session.lower} is running without Remote Control
  When the developer runs the health check
  Then its Remote Control is marked missing, with /remote-control to run inside it
  And the {session.lower} is not restarted
```

### Feature: Apps

<!-- intent: D-006 - what a project needs is set up by its own scripts; whether its app is running
     is said by its status script, so the program never guesses how to check an app -->

```gherkin
@HEALTH-APP-001 @status:proposed @persona:Developer
Scenario: A running app is reported by the {project.possessive} status script
  Given a running {session.lower} whose {project.lower} has a status script
  And the script ends with success
  When the developer runs the health check
  Then the {session.possessive} app is marked present, with what the script printed
```

<!-- intent: as HEALTH-APP-001; the fix for an app that is not running is starting it, as the
     Run button does (D-006, WORK-STACK-001) -->

```gherkin
@HEALTH-APP-002 @status:proposed @persona:Developer
Scenario: An app that is not running is reported with what its script said
  Given a running {session.lower} whose {project.lower} has a status script
  And the script ends with a failure
  When the developer runs the health check
  Then the {session.possessive} app is marked missing, with what the script printed
  And its fix is starting the app with engineroom up and the {session.possessive} name
```

<!-- intent: nothing is counted as running unproved; a project that starts an app but cannot say
     whether it is running is told what the status script must do, as a missing up script is
     (WORK-STACK-004) -->

```gherkin
@HEALTH-APP-003 @status:proposed @persona:Developer
Scenario: A {project.lower} with an up script and no status script is not counted as running
  Given a running {session.lower} whose {project.lower} has an up script and no status script
  When the developer runs the health check
  Then the {session.possessive} app is marked missing, as not checked
  And its fix says what a status script must do and where it goes
```

<!-- intent: a project with no up script has no app the program starts, so there is nothing to be
     running and nothing missing -->

```gherkin
@HEALTH-APP-004 @status:proposed @persona:Developer
Scenario: A {project.lower} with no app is not reported missing
  Given a running {session.lower} whose {project.lower} has neither an up script nor a status script
  When the developer runs the health check
  Then the {session.possessive} app is marked present, as having no app to run
```

<!-- intent: one script that hangs cannot hold the whole view, as one tool cannot hold the setup
     check -->

```gherkin
@HEALTH-APP-005 @status:proposed @persona:Developer
Scenario: A status script that does not end is reported, and the rest are still checked
  Given a running {session.lower} whose status script does not end
  When the developer runs the health check
  Then that {session.possessive} app is marked missing, as not answering within its time
  And every other item is still reported
```

### Feature: Fixing

<!-- intent: CLAUDE.md "it applies a fix only on an explicit request"; a fix that is the
     program's own action runs as any action does, its commands shown before, during and after -->

```gherkin
@HEALTH-FIX-001 @status:proposed @persona:Developer
Scenario: A fix the program can run is run only when asked, as an action
  Given the health check reports a {session.possessive} app as not running
  When the developer asks for that fix
  Then the commands that start the app are shown before they run
  And the start is listed in the history with each of its commands and their outcomes
  And nothing else is changed
```

<!-- intent: a fix that needs an administrator, or has to be typed inside a session, is shown for
     the developer to apply; the program never applies it for them -->

```gherkin
@HEALTH-FIX-002 @status:proposed @persona:Developer
Scenario: A fix the program cannot run is shown, never run
  Given the health check reports a {session.possessive} Remote Control as missing
  When the developer looks at its fix
  Then the fix is shown as what to type inside the {session.lower}
  And there is no way to run it from the check
```

### Feature: Reading the report

<!-- intent: as SETUP-READ-001 and 002 - the app and scripts read the same report a person does -->

```gherkin
@HEALTH-READ-001 @status:proposed @persona:Developer
Scenario: The exit status says whether anything is missing, and the report can be read as JSON
  Given every item is present
  When the developer runs the health check
  Then it ends with success
  And when one item is missing, it ends with a failure
  And as JSON each item has its name, whether it is present, how it was checked and its fix
```

<!-- intent: D-023 and D-019 - the window holds everything other than the sessions, and the app
     reads nothing on a timer; the check runs on the engine, the app only asks -->

```gherkin
@HEALTH-PAGE-001 @status:proposed @persona:Developer
Scenario: The app's Health page checks when opened and when asked again
  Given the app is running
  When the developer opens the Health page
  Then the {engine.lower} is checked and every item is shown with its fix
  And it is checked again only when the developer asks
```
