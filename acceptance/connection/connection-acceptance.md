# Connection - acceptance criteria

**Scope**: which machine an action runs on, what happens when the {engine.lower} does not answer, and
working locally. Out of scope: setting up SSH or the private network between machines, and more
than one {engine.lower}.

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
| **Developer** | Works on {project.lowerPlural} from the {engine.lower} or from a {client.lower}, and decides what happens when the {engine.lower} cannot be reached |

---

## User stories

> **As a** developer working from a {client.lower}, **I want** every action to run on the
> {engine.lower} without my saying so, **so that** my work, its {session.lowerPlural} and its
> running services all live on one always-on machine that I can reach from anywhere.

- The role of a machine is set once, and every action follows it.
- A machine that has no role yet says so rather than guessing one.

> **As a** developer whose {engine.lower} has stopped answering, **I want** to be asked what to do,
> **so that** I never end up working on the wrong machine without knowing it, and can fix the
> connection instead when it is fixable.

- Nothing is started on either machine until the developer has chosen.
- Working locally is always visible as such.

---

## Gherkin scenarios

### Feature: Which machine does the work

<!-- intent: one tool configured on each machine as the engine or a client, where a session can be
     reached from either machine (docs/decisions.md D-002) -->

```gherkin
@CONN-ROLE-001 @status:proposed @persona:Developer
Scenario: On a {client.lower}, opening a {project.lower} opens it on the {engine.lower}
  Given this machine is set up as a {client.lower}
  And the {engine.lower} answers
  When the developer opens a {project.lower}
  Then its {session.lower} is opened on the {engine.lower}
  And nothing is started on this machine
```

<!-- intent: as CONN-ROLE-001, docs/decisions.md D-002 -->

```gherkin
@CONN-ROLE-002 @status:proposed @persona:Developer
Scenario: On the {engine.lower}, opening a {project.lower} opens it directly
  Given this machine is set up as the {engine.lower}
  When the developer opens a {project.lower}
  Then its {session.lower} is opened on this machine
  And none of the commands shown connect to another machine
```

<!-- intent: the app names the {engine.lower} by its machine name, not by its SSH name: a
     {client.lower} reaches the {engine.lower} by whatever SSH name its configuration gives (an
     alias such as "engine"), which says nothing about which machine answers; the {engine.lower}
     names itself, read in the version check the app already makes -->

```gherkin
@CONN-ROLE-003 @status:proposed @persona:Developer
Scenario: A {client.lower} names its {engine.lower} by the machine's own name
  Given this machine is a {client.lower} that reaches the {engine.lower} by an SSH name of its own
  And the {engine.lower} answers
  When the {client.lower} compares its version with the {engine.possessive}
  Then it is told the {engine.possessive} own machine name, not the SSH name
```

<!-- intent: the tool is there so the tooling need not be remembered; a machine with no role cannot
     know where to act, and guessing would act on the wrong machine -->

```gherkin
@CONN-SETUP-001 @status:proposed @persona:Developer
Scenario: A machine with no role says what is missing
  Given this machine has not been set up as the {engine.lower} or as a {client.lower}
  When the developer starts the tool
  Then they are told this machine has no role yet
  And they are shown how to set one
  And no action is offered until a role is set
```

### Feature: When the engine does not answer

<!-- intent: when the engine cannot be reached the developer is asked before anything falls back,
     because the connection may be fixable and a silent switch hides which machine is in use -->

```gherkin
@CONN-DOWN-001 @status:proposed @persona:Developer
Scenario: An unreachable {engine.lower} is a question, not a fallback
  Given this machine is a {client.lower}
  And the {engine.lower} does not answer
  When the developer opens a {project.lower}
  Then they are asked to retry, work locally, or stop
  And nothing has been started on either machine
```

<!-- intent: as CONN-DOWN-001 -->

```gherkin
@CONN-DOWN-002 @status:proposed @persona:Developer
Scenario: Choosing stop changes nothing
  Given the developer was asked what to do because the {engine.lower} did not answer
  When they choose stop
  Then nothing has been started or changed on either machine
```

<!-- intent: as CONN-DOWN-001 - retry exists because the connection may be fixable -->

```gherkin
@CONN-DOWN-003 @status:proposed @persona:Developer
Scenario: Retry carries on once the {engine.lower} answers
  Given the developer was asked what to do because the {engine.lower} did not answer
  And the {engine.lower} now answers
  When they choose retry
  Then the {project.lower} they asked for is opened on the {engine.lower}
```

<!-- intent: as CONN-DOWN-001 - a retry that silently fell back would be the silent switch that
     scenario rules out -->

```gherkin
@CONN-DOWN-004 @status:proposed @persona:Developer
Scenario: Retry asks again while the {engine.lower} is still down
  Given the developer was asked what to do because the {engine.lower} did not answer
  And the {engine.lower} still does not answer
  When they choose retry
  Then they are asked again to retry, work locally, or stop
  And nothing has been started on either machine
```

### Feature: Working locally

<!-- intent: as CONN-DOWN-001 - working locally is the developer's choice, on this machine's own
     copy of the project -->

```gherkin
@CONN-LOCAL-001 @status:proposed @persona:Developer
Scenario: Working locally opens the {project.lower} on this machine
  Given the developer was asked what to do because the {engine.lower} did not answer
  And this machine has its own {checkout.lower} of the {project.lower}
  When they choose to work locally
  Then a {session.lower} is opened on this machine in that {checkout.lower}
```

<!-- intent: as CONN-DOWN-001 - "a silent switch hides which machine is in use", so the switch is
     never silent afterwards either -->

```gherkin
@CONN-LOCAL-002 @status:proposed @persona:Developer
Scenario: Working locally is marked on every screen
  Given the developer chose to work locally
  When any screen of the tool is shown
  Then it is marked LOCAL
```

<!-- intent: as CONN-DOWN-001 - working locally needs the project on this machine; creating a
     copy unasked would change things outside the tool's own state (CLAUDE.md) -->

```gherkin
@CONN-LOCAL-003 @status:proposed @persona:Developer
Scenario: A {project.lower} with no copy here cannot be worked on locally
  Given the developer was asked what to do because the {engine.lower} did not answer
  And this machine has no {checkout.lower} of the {project.lower}
  When they choose to work locally
  Then they are told this machine has no copy of the {project.lower}
  And nothing is started on this machine
```

### Feature: Working offline by choice

<!-- intent: the whole program can be told to work offline, by itself when the {engine.lower} does
     not answer or by a button, and everything then works on the {client.lower} as though it were
     the {engine.lower} -->

```gherkin
@CONN-OFFLINE-001 @status:proposed @persona:Developer
Scenario: A {client.lower} switched offline works on itself as though it were the {engine.lower}
  Given a {client.lower} whose {engine.lower} answers
  And the {client.lower} has its own {checkout.lower} of a {project.lower}
  When the developer switches the {client.lower} offline and opens that {project.lower}
  Then a {session.lower} is opened on the {client.lower} in its {checkout.lower}
  And nothing is asked of the {engine.lower}
  And every screen is marked OFFLINE
```

<!-- intent: as CONN-OFFLINE-001 - offline is a switch the developer sets, so it holds until they
     switch it back -->

```gherkin
@CONN-OFFLINE-002 @status:proposed @persona:Developer
Scenario: Offline is remembered until it is switched off
  Given the developer switched the {client.lower} offline
  When they start the tool again
  Then it is still offline and marked OFFLINE
  And once they switch it back online, the {session.lowerPlural} it lists are those on the {engine.lower}
```

<!-- intent: as CONN-OFFLINE-001, and the standing rule that a session is stopped only when the
     developer chose that exact one -->

```gherkin
@CONN-OFFLINE-003 @status:proposed @persona:Developer
Scenario: Going back online leaves the {client.lower}'s own {session.lowerPlural} running
  Given a {session.lower} was started on the {client.lower} while it was offline
  When the developer switches the {client.lower} back online
  Then that {session.lower} is still running on the {client.lower}
```

<!-- intent: a switch made before the handover has been seen would leave the {client.lower}
     working local with nothing handed over when the handover is left without running it -->

```gherkin
@CONN-OFFLINE-004 @status:proposed @persona:Developer
Scenario: The switch to work local happens only when its handover is run
  Given a {client.lower} working on the {engine.lower}
  When the developer asks the app to work local and is shown the handover
  Then the {client.lower} is still working on the {engine.lower}
  And once they run the handover, it works local
```

### Feature: Handing work over between the {client.lower} and the {engine.lower}

<!-- intent: going back online needs a handover. Work done offline lives only in the
     {client.lower}'s own {checkout.lowerPlural}; the two machines share nothing but GitHub. So
     switching shows what one machine holds that the other does not, and the commands that carry it
     across through GitHub, and runs them only when asked: the program never hides what it does, and
     changes nothing unasked. -->

```gherkin
@CONN-HANDOFF-001 @status:proposed @persona:Developer
Scenario: Going back online shows the work the {client.lower} holds that the {engine.lower} does not
  Given the {client.lower} is offline
  And a commit was made in the {client.lower}'s {checkout.lower} of a {project.lower} and not pushed
  When the developer switches the {client.lower} back online
  Then they are shown that the {project.lower} has a commit on the {client.lower} that is not on GitHub
  And they are shown the commands that push it from the {client.lower} and bring the {engine.lower}'s {checkout.lower} up to it
  And nothing has run yet
```

<!-- intent: as CONN-HANDOFF-001 - the handoff is real git through GitHub, so after it the work
     is safe off the {client.lower} and the {engine.lower} works on it -->

```gherkin
@CONN-HANDOFF-002 @status:proposed @persona:Developer
Scenario: Running the handoff puts the {client.lower}'s work in the {engine.lower}'s {checkout.lower}
  Given going back online showed a commit on the {client.lower} to hand over
  When the developer runs the handoff
  Then the commit is on GitHub
  And the {engine.lower}'s {checkout.lower} of the {project.lower} is at that commit
```

<!-- intent: a handover means identical state: the receiving {checkout.lower} ends on the giving
     machine's branch and commit (D-017). The receiving {checkout.lower}'s own uncommitted work is
     never lost: it is set aside in its stash list, labelled with its branch, before the
     {checkout.lower} takes the giving machine's state -->

```gherkin
@CONN-HANDOFF-003 @status:proposed @persona:Developer
Scenario: An {engine.lower} {checkout.lower} with uncommitted changes sets them aside and takes the {client.lower}'s state
  Given the {client.lower} has a commit to hand over
  And the {engine.possessive} {checkout.lower} of that {project.lower} has uncommitted changes
  When the developer hands the work back to the {engine.lower} and runs it
  Then the {engine.possessive} {checkout.lower} is at the {client.lower}'s commit, with no changes of its own
  And the {engine.possessive} changes are in its stash list, labelled with its branch
```

<!-- intent: the reverse of CONN-HANDOFF-001, offered while the {engine.lower} still answers, so
     work there can be taken along before the {client.lower} leaves -->

```gherkin
@CONN-HANDOFF-004 @status:proposed @persona:Developer
Scenario: Going offline shows the work the {engine.lower} holds that the {client.lower} does not
  Given a commit was made in the {engine.lower}'s {checkout.lower} of a {project.lower} and not pushed
  And the {engine.lower} answers
  When the developer switches the {client.lower} offline
  Then they are shown that the {project.lower} has a commit on the {engine.lower} that is not on GitHub
  And they are shown the commands that push it from the {engine.lower} and bring the {client.lower}'s {checkout.lower} up to it
```

<!-- intent: as CONN-HANDOFF-004 - going offline is most needed when the {engine.lower} cannot be
     reached, so it never waits on the {engine.lower}, and never implies a check it could not make -->

```gherkin
@CONN-HANDOFF-005 @status:proposed @persona:Developer
Scenario: Going offline while the {engine.lower} does not answer says what could not be checked
  Given the {engine.lower} does not answer
  When the developer switches the {client.lower} offline
  Then the {client.lower} is offline
  And they are told the {engine.lower} could not be checked for work that is not on GitHub
```

<!-- intent: a {session.lower} started offline keeps running (CONN-OFFLINE-003), so once back online
     it is listed rather than out of sight, marked as on this machine, and acted on where it is -->

```gherkin
@CONN-HANDOFF-006 @status:proposed @persona:Developer
Scenario: Back online, {session.lowerPlural} still running on the {client.lower} are listed as on this machine
  Given a {session.lower} started on the {client.lower} while it was offline is still running
  When the developer switches the {client.lower} back online
  Then the {session.lower} list shows the {engine.lower}'s {session.lowerPlural}
  And it shows that {session.lower} marked as on this machine
  And joining it joins it on the {client.lower}
```

<!-- intent: pushing every branch would put backups and experiments on GitHub, and a release branch
     whose push starts a deployment. The work in hand is what the registered projects have checked
     out (D-017); any other branch with commits GitHub lacks is named with the command to push it,
     never pushed by a handover -->

```gherkin
@CONN-HANDOFF-007 @status:proposed @persona:Developer
Scenario: Working local pushes the work checked out, and only names other branches
  Given a branch in no {checkout.lower} on the {engine.lower} has a commit that GitHub does not
  And the {engine.possessive} main clone has a commit on its own branch that GitHub does not
  When the developer hands the work over from the {engine.lower} to the {client.lower} and runs it
  Then the main clone's branch is on GitHub with its commit
  And the branch in no {checkout.lower} is not on GitHub
  And they are told the command that would push it
```

<!-- intent: pushing a release branch is how many repositories start a release (a pipeline triggered
     by release/<version>), so a handover, which pushes to keep work safe, never pushes one, even
     checked out -->

```gherkin
@CONN-HANDOFF-012 @status:proposed @persona:Developer
Scenario: A release branch is never pushed by a handover
  Given the {engine.possessive} main clone has a release branch checked out with a commit that GitHub does not
  When the developer hands the work over from the {engine.lower} to the {client.lower} and runs it
  Then the release branch is not on GitHub
  And they are told a handover never pushes a release branch
```

<!-- intent: a screen that shows nothing while a handover is planned reads as stuck. A handover
     reads every project on both machines against GitHub before it can say anything, so each machine
     says how far its reading has got, project by project, with the command doing it, and the
     projects are read several at a time so the wait is short -->

```gherkin
@CONN-HANDOFF-013 @status:proposed @persona:Developer
Scenario: Working out a handover says how far each machine's reading has got
  Given the {engine.lower} has two {project.lowerPlural} registered
  When the developer asks for the handover from the {engine.lower} to the {client.lower}
  Then they are told the {engine.possessive} reading {project.lower} by {project.lower} up to two of two, with the command doing it
  And they are told this machine's reading too
  And the handover's commands follow
```

<!-- intent: work that is not committed must reach the other machine too. Uncommitted work and new
     files travel with the handover as a stash, straight between the two machines, never through
     GitHub; ignored files do not, since anything that must be on both machines belongs in the
     settings' secrets (D-017) -->

```gherkin
@CONN-HANDOFF-008 @status:proposed @persona:Developer
Scenario: Uncommitted work on the {engine.lower} moves to the {client.lower} with the handover
  Given the {engine.possessive} {checkout.lower} of a {project.lower} has a changed file and a new file, neither committed
  When the developer hands the work over from the {engine.lower} to the {client.lower} and runs it
  Then the {client.lower}'s {checkout.lower} has the changed file and the new file, uncommitted
  And the {engine.possessive} {checkout.lower} no longer has them, and keeps them in its stash list
  And GitHub has nothing of them
```

<!-- intent: as CONN-HANDOFF-008, handing back: work done while working local goes back to the
     {engine.lower} with the handover, a new file included -->

```gherkin
@CONN-HANDOFF-014 @status:proposed @persona:Developer
Scenario: Uncommitted work on the {client.lower} moves back to the {engine.lower} with the handover
  Given the {client.lower}'s {checkout.lower} of a {project.lower} has a new file, not committed
  When the developer hands the work back to the {engine.lower} and runs it
  Then the {engine.possessive} {checkout.lower} has the new file, uncommitted
  And the {client.lower}'s {checkout.lower} no longer has it
```

<!-- intent: a handover first checks that everything can be done, and runs only if it can (D-017).
     One {checkout.lower} that cannot be put in the giving machine's state blocks the whole
     handover, and the switch with it -->

```gherkin
@CONN-HANDOFF-015 @status:proposed @persona:Developer
Scenario: A handover that cannot be done in full changes nothing
  Given the {engine.possessive} {checkout.lower} of a {project.lower} has uncommitted changes
  And the {client.lower}'s branch of that {project.lower} has a commit of its own that GitHub does not
  When the developer switches the {client.lower} to work local
  Then they are told the handover cannot run, because of the {client.lower}'s commit
  And nothing has changed on either machine
  And the {client.lower} is not working local
```

<!-- intent: as CONN-HANDOFF-003 - after a handover the receiving {checkout.lower} is on the giving
     {checkout.lower}'s branch, at its commit, with its uncommitted work, whatever it held before -->

```gherkin
@CONN-HANDOFF-016 @status:proposed @persona:Developer
Scenario: The receiving {checkout.lower} takes the giving {checkout.possessive} branch, commit and work
  Given the {engine.possessive} {checkout.lower} of a {project.lower} is on another branch than main, with an uncommitted change
  And the {client.lower}'s {checkout.lower} of that {project.lower} is on main, with a new file of its own
  When the developer hands the work over from the {engine.lower} to the {client.lower} and runs it
  Then the {client.lower}'s {checkout.lower} is on the {engine.possessive} branch, at its commit, with its change
  And the {client.lower}'s new file is in its stash list, labelled main
```

<!-- intent: as CONN-HANDOFF-015 - the checks run first, at run time too: something that changed
     after the handover was shown stops it before it changes anything -->

```gherkin
@CONN-HANDOFF-017 @status:proposed @persona:Developer
Scenario: A change made after the handover was shown stops it before anything changes
  Given the developer is shown a handover from the {engine.lower} to the {client.lower} with a commit to push
  When the {engine.possessive} {checkout.lower} is edited before the developer runs it
  Then the handover stops at its first step
  And GitHub and both {checkout.lowerPlural} are as they were
```

<!-- intent: as CONN-HANDOFF-016 - a handover hands over the newer work, never older: a giving
     machine behind GitHub, where the receiving machine has GitHub's newer commits, would move the
     receiving checkout back, so it blocks until the giving machine is brought up -->

```gherkin
@CONN-HANDOFF-018 @status:proposed @persona:Developer
Scenario: A handover that would move the receiving machine back is blocked
  Given a commit was made in the {engine.possessive} {checkout.lower} of a {project.lower} and pushed to GitHub
  And the {client.lower}'s {checkout.lower} of that {project.lower} has not taken it
  When the developer hands the work back to the {engine.lower}
  Then they are told the handover cannot run, because it would move the {engine.lower} back
  And the {engine.possessive} {checkout.lower} is still at its commit
```

<!-- intent: a handover's plan has to be readable at a glance. What a handover does is said one line
     per {checkout.lower}, and what it leaves alone is folded into a line -->

```gherkin
@CONN-HANDOFF-019 @status:proposed @persona:Developer
Scenario: A handover says what it does in one line per {checkout.lower}
  Given the {engine.possessive} {checkout.lower} of a {project.lower} has a commit GitHub does not and an uncommitted change
  And a branch in no {checkout.lower} there has a commit GitHub does not
  When the developer asks for the handover from the {engine.lower} to the {client.lower}
  Then one line for the {project.lower} says the commit is sent to GitHub and the change carried
  And the branch in no {checkout.lower} is named in one line of what is left as it is
```

<!-- intent: a project the receiving machine lacks is put in the same place under its home folder,
     so both machines' project folders mirror each other (D-017) -->

```gherkin
@CONN-HANDOFF-009 @status:proposed @persona:Developer
Scenario: A {project.lower} the {client.lower} lacks is mirrored to the same place under its home
  Given the {engine.lower} has a {project.lower} the {client.lower} does not
  When the developer hands the work over from the {engine.lower} to the {client.lower} and runs it
  Then the {client.lower} has that {project.lower} at the same place under its home as on the {engine.lower}
  And it is registered on the {client.lower} under the same name
```

<!-- intent: on the {engine.lower}, release pushes the work so a {client.lower} can take it, and
     says what is not committed; only the work checked out is pushed, as by every handover
     (CONN-HANDOFF-007) -->

```gherkin
@CONN-HANDOFF-010 @status:proposed @persona:Developer
Scenario: Releasing the {engine.lower} pushes the work checked out and is shown as information
  Given the {engine.possessive} main clone has a commit on its own branch that GitHub does not
  When the developer releases the {engine.lower} from the app on the {engine.lower}
  Then that branch is on GitHub with its commit
  And the {engine.lower} shows when it was released
```

<!-- intent: a {client.lower} keeps itself ready to work local as a regular background step, so
     going offline never starts with a long catch-up (D-017) -->

```gherkin
@CONN-HANDOFF-011 @status:proposed @persona:Developer
Scenario: Keeping the {client.lower} ready copies what it lacks and moves nothing
  Given the {engine.lower} has a {project.lower} the {client.lower} does not
  And GitHub has a commit the {client.lower}'s {checkout.lower} of another {project.lower} does not
  When the {client.lower} keeps itself ready to work local
  Then the missing {project.lower} is copied to the {client.lower} and registered
  And the {client.lower}'s {checkout.lower} is not moved
```

### Feature: Keeping a joined {session.lower} through sleep and moving

<!-- intent: a laptop is opened and closed many times a day, and its sessions must stay within easy
     reach. A {client.lower}'s connection to the {engine.lower} ends when it sleeps or changes
     network, while its {session.lower} keeps running on the {engine.lower}; the window joined to it
     comes back to the {session.lower} on its own (D-021) -->

```gherkin
@CONN-REJOIN-001 @status:proposed @persona:Developer
Scenario: A join whose connection drops joins the same {session.lower} again
  Given the developer has joined a {session.lower} from a {client.lower}
  When the connection to the {engine.lower} drops while the {session.lower} keeps running
  Then the same window joins that {session.lower} again
  And the commands it ran to do so are in the history
```

<!-- intent: as CONN-REJOIN-001 - a {client.lower} waking from sleep or between networks cannot
     reach the {engine.lower} at once, and the window waits for it rather than ending -->

```gherkin
@CONN-REJOIN-002 @status:proposed @persona:Developer
Scenario: While the {engine.lower} does not answer, the window waits and says so
  Given the developer has joined a {session.lower} from a {client.lower}
  When the connection to the {engine.lower} drops and the {engine.lower} does not answer
  Then the window says the connection was lost and that it joins again when the {engine.lower} answers
  And once the {engine.lower} answers it joins the {session.lower} again
```

<!-- intent: as CONN-REJOIN-001 - joining again is only for the same {session.lower}: one that was
     stopped or restarted ends the join as before (SESS-BACK-004) -->

```gherkin
@CONN-REJOIN-003 @status:proposed @persona:Developer
Scenario: A {session.lower} that ended while the connection was down is not joined again
  Given the developer has joined a {session.lower} from a {client.lower}
  When the connection to the {engine.lower} drops and the {session.lower} is stopped before it comes back
  Then the window does not join a {session.lower} again
  And no {session.lower} is started on either machine
```

<!-- intent: as CONN-REJOIN-002 - waiting is the developer's to end -->

```gherkin
@CONN-REJOIN-004 @status:proposed @persona:Developer
Scenario: The developer can stop the waiting
  Given the window is waiting for the {engine.lower} to answer after the connection dropped
  When the developer presses q
  Then the window stops waiting and joins nothing
```
