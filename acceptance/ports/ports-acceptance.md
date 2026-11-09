# Ports - acceptance criteria

**Scope**: each {project.lower}'s ports and names, reserved so no two {project.lowerPlural}
clash; each {checkout.lower} running on its own numbered slot of them, so several
{checkout.lowerPlural} of one {project.lower} run side by side; the developer's own settings
deciding over what a repository recommends; and a {client.lower} reaching the ports of the
{engine.possessive} running {session.lowerPlural} on its own machine. Out of scope: the app's
{project.lower} page, and syncing the settings folder between machines.

**Sourcing**: every scenario's intent was stated by the architect in conversation on 2026-11-05
and 2026-11-06, quoted in each intent comment.

**Status**: every scenario is `proposed`. `proposed` is a proposal to be confirmed, `derived` is
written up from delivered software, `verified` means a person watched it happen, and `accepted`
means it is confirmed as what the software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Runs several {checkout.lowerPlural} of several {project.lowerPlural} at once, on the {engine.lower} and from a {client.lower} |

---

## User stories

> **As a** developer running several pieces of work on one {project.lower}, **I want** each
> {checkout.lower} to have its own ports and names, **so that** a second stack never reaches the
> first one's server or data.

- A new {checkout.lower} gets a slot no other {checkout.lower} holds, and gives it back when it
  is cleaned up.

> **As a** developer with many {project.lowerPlural}, **I want** the program to reserve ports
> per {project.lower}, **so that** no two {project.lowerPlural} can ever be given the same port.

> **As a** developer who works in repositories they do not own, **I want** my own settings to
> decide, **so that** nothing of mine has to be committed to someone else's repository.

> **As a** developer away from the {engine.lower}, **I want** the ports of the {engine.possessive}
> running {session.lowerPlural} on my own machine, **so that** I can open the app in my browser
> whether I joined the {session.lower} or work from the Claude app.

---

## Gherkin scenarios

### Feature: Slots

<!-- intent: the architect, 2026-11-06 - "the ports are per worktree"; "10 slots is plenty" -->

```gherkin
@PORT-SLOT-001 @status:proposed @persona:Developer
Scenario: New work gets its own slot
  Given a {project.lower} whose settings give it three slots
  When the developer starts new work on it
  Then the new {checkout.lower} holds a slot that is not the main clone's
  And its record holds that slot's ports
```

<!-- intent: as PORT-SLOT-001 - a slot is lent to a worktree for as long as it exists -->

```gherkin
@PORT-SLOT-002 @status:proposed @persona:Developer
Scenario: Cleaning up new work gives its slot back
  Given new work holds a slot
  When the developer cleans it up and confirms
  Then the slot is free
  And the next new work is given it
```

<!-- intent: the architect, 2026-11-07 - "the worktree setup needs to clone db and allocate storage
     as it does now but via hubs". A worktree's scripts and tasks must run on the slot it holds,
     whatever file name the project gives its record; a script that silently gets slot 1's values
     reports success while starting nothing of its own. -->

```gherkin
@PORT-SLOT-003 @status:proposed @persona:Developer
Scenario: A {checkout.lower}'s scripts and tasks are given the slot it holds
  Given a {project.lower} whose settings name their own slot record
  And new work on it holds slot 2
  When the developer starts the stack of that new work and runs one of its tasks there
  Then the up script is given slot 2's ports
  And the task is given them too
```

<!-- intent: the architect, 2026-11-07 (agreeing the recommendation for a project kept on one
     slot): a main clone that has claimed no slot runs on slot 1, so a worktree given slot 1 would
     share every port with it, and its up script would find the main clone's servers and report
     success. A worktree gets slot 1 only when the main clone holds another; otherwise new work is
     refused with the reason, before anything is created. -->

<!-- intent: found 2026-11-07 across five repositories: every claim warned that the slot's files
     were not ignored, and nothing offered the fix, so none was made. The fix changes a file of
     the repository, so it is done when the developer asks, and asking again changes nothing -->

```gherkin
@PORT-SLOT-005 @status:proposed @persona:Developer
Scenario: The developer has the slot's files ignored with one command
  Given new work holds a slot, and the repository's .gitignore names none of the slot's files
  When the developer asks for the slot's files to be ignored, twice
  Then git ignores the record and its environment file
  And .gitignore names each of them once
```

```gherkin
@PORT-SLOT-004 @status:proposed @persona:Developer
Scenario: New work is never given the slot the main clone runs on
  Given a {project.lower} whose settings give it one slot, which its main clone runs on
  When the developer starts new work on it
  Then they are told every slot is held, naming the main clone
  And no {checkout.lower} is created
```

### Feature: Your settings decide

<!-- intent: the architect, 2026-11-06 - "a hubs file in the repo can set recommendations, but
     our custom one in a private repo is the controller"; 2026-11-05 - "We can't have a hub file
     committed to a repo I don't own" -->

```gherkin
@PORT-SET-001 @status:proposed @persona:Developer
Scenario: The developer's settings win over the repository's recommendation
  Given the repository recommends one set of ports and the developer's settings give another
  When the developer starts new work on the {project.lower}
  Then its record holds the ports from the developer's settings
```

<!-- intent: the architect, 2026-11-09 - "some other sort of prefix to prevent any overlap, i.e.
     adding the word port"; "make this happen" (D-028). Every key of a slot is one value in its
     record and one variable for the scripts, so a port named project silently replaced the
     project's name. A key that would overwrite another is refused when the settings are read,
     naming the file and both keys, before anything is created. -->

```gherkin
@PORT-SET-002 @status:proposed @persona:Developer
Scenario: A key that would overwrite another is refused, naming it
  Given the developer's settings for a {project.lower} name a port "slot", the program's own key
  When the developer starts new work on the {project.lower}
  Then they are told the port "slot" is the program's own key, naming their settings file
  And no {checkout.lower} is created
```

### Feature: Reservations

<!-- intent: the architect, 2026-11-06 - "the hubs project should reserve ranges per repo that can
     then be delegated to worktrees. this protects all projects on conflict" -->

```gherkin
@PORT-RES-001 @status:proposed @persona:Developer
Scenario: Two {project.lowerPlural} on one port are reported with both named
  Given two {project.lowerPlural} whose settings both use one port
  When the developer checks the ports
  Then the clash is reported naming both {project.lowerPlural} and the port
  And neither {project.lower}'s settings are changed
```

<!-- intent: the architect, 2026-11-06 - "10 slots is plenty. port suggestion is fine" (a block of
     100 from 20000-29999); the main clone keeps its numbers -->

```gherkin
@PORT-RES-002 @status:proposed @persona:Developer
Scenario: Reserving gives a {project.lower} a free block and keeps its own numbers
  Given another {project.lower} already reserves the first block
  When the developer reserves ports for a {project.lower} that names its ports
  Then the {project.lower} reserves the next free block of 100
  And its slot 1 keeps the ports it had
  And slots 2 to 10 take ports from the block
```

### Feature: Reaching the engine's ports from a client

<!-- intent: the architect, 2026-11-05 - "if the ports exist while there is a session then that is
     good"; "sometimes I'm just using remote control" - forwarding follows running sessions, not
     joining -->

```gherkin
@PORT-FWD-001 @status:proposed @persona:Developer
Scenario: A running {session.lower}'s ports are reached on the {client.lower}
  Given a {session.lower} is running on the {engine.lower} with a server on its slot's port
  When the developer turns forwarding on, on a {client.lower}
  Then the {client.lower}'s own port of that number reaches the {engine.possessive} server
  And the forward is kept in the history
```

<!-- intent: as PORT-FWD-001 - the ports are there while the session is -->

```gherkin
@PORT-FWD-002 @status:proposed @persona:Developer
Scenario: The ports of a {session.lower} that stopped are no longer forwarded
  Given the {client.lower} forwards a running {session.lower}'s port
  When the {session.lower} stops and forwarding checks again
  Then the {client.lower}'s port no longer reaches the {engine.lower}
```

<!-- intent: the architect, 2026-11-05 - a port the client already uses is named, never failed
     silently -->

```gherkin
@PORT-FWD-003 @status:proposed @persona:Developer
Scenario: A port already in use on the {client.lower} is named and left alone
  Given a program on the {client.lower} already listens on a running {session.lower}'s port
  When the developer turns forwarding on
  Then they are told the port is in use and what holds it
  And that program keeps the port
```

<!-- intent: the independent review, 2026-11-07, confirmed in code: working local, this machine runs
     its own stacks on the same port numbers, so a forward left open would answer for them with the
     engine's servers -->

```gherkin
@PORT-FWD-004 @status:proposed @persona:Developer
Scenario: Working local closes the {engine.possessive} forwards
  Given the {client.lower} forwards a running {session.lower}'s port
  When the developer switches the {client.lower} to work local
  Then the {client.lower}'s port no longer reaches the {engine.lower}
  And forwarding checks again do not forward it while the {client.lower} works local
```

<!-- intent: the architect, 2026-11-07 - on a client, a running session is forwarded; not by
     checking every minute, and nothing costly while nobody looks - the
     forwarder follows the engine's sessions over the connection the forwards are on, and opens no
     connection of its own -->

```gherkin
@PORT-FWD-005 @status:proposed @persona:Developer
Scenario: A {session.lower} started on the {engine.lower} is forwarded while the forwarder watches
  Given the {client.lower} forwards a running {session.lower}'s port and its forwarder is watching
  And a new connection to the {engine.lower} could not be made
  When another {session.lower} starts on the {engine.lower} with a server on its port
  Then within seconds the {client.lower}'s port of that number reaches the {engine.possessive} server
```

```gherkin
@PORT-FWD-006 @status:proposed @persona:Developer
Scenario: The forwarder opens no connection while there is none
  Given forwarding is on and a {session.lower} is running on the {engine.lower}
  And the {client.lower} has no connection to the {engine.lower}
  When the forwarder watches for a while
  Then no connection to the {engine.lower} is opened
  And nothing is added to the history
```

<!-- intent: the architect, 2026-11-07 - "I manually enable forwarding on a worktree when I need it.
     Or by clicking the hyperlink you enable on demand"; "a switch per worktree session on or off";
     minimal and battery mindful - one session's switch forwards only that session, and with
     nothing chosen the connection is closed -->

```gherkin
@PORT-FWD-007 @status:proposed @persona:Developer
Scenario: One {session.lower}'s switch forwards only that {session.lower}
  Given forwarding every {session.lower} is off, and two {session.lower}s run on the {engine.lower}, each with a server on its port
  When the developer switches one {session.lower}'s forwarding on
  Then that {session.lower}'s port reaches the {engine.possessive} server
  And the other {session.lower}'s port is not forwarded
  And once they switch it off again its port no longer reaches the {engine.lower} and the connection to the {engine.lower} is closed
```
