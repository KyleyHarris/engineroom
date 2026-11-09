# New work - acceptance criteria

**Scope**: starting a piece of work on a {project.lower} in its own {checkout.lower}, with a
session open in it, starting and stopping its app stack with the repository's own scripts, and
cleaning that {checkout.lower} up when the work is finished. Out of
scope: claiming ports for a {checkout.lower}, which is the ports area.

**Sourcing**: every scenario's intent was stated by the architect in conversation on 2026-11-04,
or in the decisions it cites. Nothing here is inferred from software.

**Status**: every scenario is `proposed`, written at planning before the software exists.
`proposed` is a proposal to be confirmed, `derived` is written up from delivered software,
`verified` means a person watched it happen, and `accepted` means it is confirmed as what the
software should do.

---

## Roles

| Role | What they do here |
|------|-------------------|
| **Developer** | Starts new work on a {project.lower} and cleans it up when finished, from the {engine.lower} or a {client.lower} |

---

## User stories

> **As a** developer, **I want** to start a piece of work on a {project.lower} in one action,
> **so that** a fresh {checkout.lower} on its own branch, set up the way that repository needs,
> has a session waiting for me without my remembering the steps.

- The work starts from the remote's current main.
- What a repository needs (databases, storage, dependencies) is done by its own create script.
- A step that fails is reported as such; nothing half-made is left unannounced.

> **As a** developer finishing a piece of work, **I want** to clean up its {checkout.lower} in
> one action, **so that** nothing it used is left behind, and nothing unsaved is lost.

---

## Gherkin scenarios

### Feature: Starting new work

<!-- intent: the architect, 2026-11-04 - "build a new worktree for an issue on a repo and then open
     a session there"; worktrees are siblings named <repo>-<branch> -->

```gherkin
@WORK-NEW-001 @status:proposed @persona:Developer
Scenario: New work gets its own {checkout.lower} beside the main one, with a {session.lower} open in it
  Given a {project.lower} is registered
  When the developer starts new work on it called "fix-login"
  Then a {checkout.lower} named after the {project.lower} and "fix-login" exists beside the main one, on a new branch called "fix-login"
  And a {session.lower} is open in that {checkout.lower}
```

<!-- intent: issue #7 - "Fetch the project's remote, so the worktree starts from the current main" -->

```gherkin
@WORK-NEW-002 @status:proposed @persona:Developer
Scenario: New work starts from the remote's current main
  Given the remote's main has a commit this machine has not fetched
  When the developer starts new work on the {project.lower}
  Then the new branch starts from that commit
```

<!-- intent: the architect's rule that branch names carry no slashes, so a branch, its folder and
     its session share one name -->

```gherkin
@WORK-NEW-003 @status:proposed @persona:Developer
Scenario: A name that cannot be a plain branch name is refused
  Given a {project.lower} is registered
  When the developer starts new work on it called "feature/login page"
  Then they are told the name may only hold letters, digits, '.', '_' and '-'
  And no {checkout.lower}, branch or {session.lower} is created
```

<!-- intent: as WORK-NEW-001 - asking twice must not make a second copy of the same work -->

```gherkin
@WORK-NEW-004 @status:proposed @persona:Developer
Scenario: Starting work that already exists opens it instead
  Given new work called "fix-login" was started on the {project.lower}
  When the developer starts new work called "fix-login" again
  Then the {session.lower} in the existing {checkout.lower} is opened
  And no second {checkout.lower} or branch is created
```

<!-- intent: docs/decisions.md D-006 - the repository's create script runs once after the
     worktree exists, with the tool's details in ENGINEROOM_* variables -->

```gherkin
@WORK-NEW-005 @status:proposed @persona:Developer
Scenario: The repository's create script prepares the new {checkout.lower}
  Given the {project.lower} declares a create script
  When the developer starts new work on it
  Then the create script runs in the new {checkout.lower} before the {session.lower} opens
  And what the script prints is shown while it runs
```

<!-- intent: docs/decisions.md D-006 - "a half-made worktree is never left unannounced" -->

```gherkin
@WORK-NEW-006 @status:proposed @persona:Developer
Scenario: A failing create script is reported and no {session.lower} opens
  Given the {project.lower} declares a create script that fails
  When the developer starts new work on it
  Then they are told the {checkout.lower} was created but its setup failed, with the script's error
  And no {session.lower} is opened
```

<!-- intent: docs/decisions.md D-006 - "a repository's script is read from the checkout, so a
     branch that changes it runs its own version"; new work starts from the remote's main, so the
     script it runs is main's, not whatever branch the main clone has checked out (found
     2026-11-07: a create script on an unmerged branch made every new worktree fail) -->

```gherkin
@WORK-NEW-007 @status:proposed @persona:Developer
Scenario: New work runs the create script of the commit it starts from, not the main {checkout.lower}'s branch
  Given the main {checkout.lower} is on a branch that adds a create script the remote's main does not have
  When the developer starts new work on the {project.lower}
  Then no create script is run
  And the {session.lower} in the new {checkout.lower} opens
```

### Feature: Cleaning up finished work

<!-- intent: the architect, 2026-11-04 - "clean up work tree option ... which can execute the
     commands and scripts to dispose of that"; issue #8 -->

```gherkin
@WORK-CLEAN-001 @status:proposed @persona:Developer
Scenario: Cleaning up finished work removes its {checkout.lower} after asking
  Given new work whose changes are all committed and pushed has a running {session.lower}
  When the developer cleans it up and confirms
  Then its {session.lower} is stopped
  And its {checkout.lower} is removed
```

<!-- intent: issue #8 - "It refuses, and says why, when the worktree has uncommitted changes" -->

```gherkin
@WORK-CLEAN-002 @status:proposed @persona:Developer
Scenario: Work with uncommitted changes is not cleaned up
  Given new work has a file changed but not committed
  When the developer cleans it up
  Then they are told it has uncommitted changes, naming the file
  And nothing is stopped or removed
```

<!-- intent: issue #8 - "or commits that were never pushed" -->

```gherkin
@WORK-CLEAN-003 @status:proposed @persona:Developer
Scenario: Work with commits that were never pushed is not cleaned up
  Given new work has a commit that is not on the remote
  When the developer cleans it up
  Then they are told it has commits that were never pushed
  And nothing is stopped or removed
```

<!-- intent: issue #8 - delete the branch "only when the branch has been merged into main" -->

```gherkin
@WORK-CLEAN-004 @status:proposed @persona:Developer
Scenario: A merged branch is deleted with its {checkout.lower}
  Given new work whose branch has been merged into main
  When the developer cleans it up and confirms
  Then its branch no longer exists on this machine or on the remote
```

<!-- intent: as WORK-CLEAN-004 - an unmerged branch may still be needed -->

```gherkin
@WORK-CLEAN-005 @status:proposed @persona:Developer
Scenario: An unmerged branch is kept when its {checkout.lower} is removed
  Given new work whose branch is pushed but not merged into main
  When the developer cleans it up and confirms
  Then its {checkout.lower} is removed
  And its branch still exists on this machine and on the remote
```

<!-- intent: issue #8 - "It never touches the main clone" -->

```gherkin
@WORK-CLEAN-006 @status:proposed @persona:Developer
Scenario: The main {checkout.lower} of a {project.lower} cannot be cleaned up
  Given a {project.lower} is registered
  When the developer asks to clean up its main {checkout.lower}
  Then they are told the main {checkout.lower} is never cleaned up
  And nothing is stopped or removed
```

<!-- intent: docs/decisions.md D-006 - the teardown script deallocates what create made, before the
     worktree is removed -->

```gherkin
@WORK-CLEAN-007 @status:proposed @persona:Developer
Scenario: The repository's teardown script runs before the {checkout.lower} is removed
  Given the {project.lower} declares a teardown script
  When the developer cleans up new work on it and confirms
  Then the teardown script runs in the {checkout.lower} before it is removed
```

<!-- intent: the architect, 2026-11-09 - "Close worktree should have some kind of verify script that
     no code would get lost. Ie rollback or commit to a branch"; a put-aside change is a stash of
     the repository, which outlives the worktree (D-024) -->

```gherkin
@WORK-CLEAN-008 @status:proposed @persona:Developer
Scenario: Cleaning up work with uncommitted changes offers to save them or put them aside
  Given new work has a file changed but not committed
  When the developer cleans it up
  Then they are told it has uncommitted changes, naming the file
  And they are offered to save the changes to the branch, or to put them aside
```

```gherkin
@WORK-CLEAN-009 @status:proposed @persona:Developer
Scenario: Work saved to its branch can be cleaned up
  Given new work has a file changed but not committed
  When the developer saves its changes to the branch
  Then the change is committed on the branch, on this machine and on the remote
  And cleaning the work up is no longer refused
```

```gherkin
@WORK-CLEAN-010 @status:proposed @persona:Developer
Scenario: Changes put aside are kept after their {checkout.lower} is removed
  Given new work has a file changed but not committed
  When the developer puts the changes aside and cleans the work up
  Then its {checkout.lower} is removed
  And the change can be brought back from the repository's stash
```

```gherkin
@WORK-CLEAN-011 @status:proposed @persona:Developer
Scenario: Work with commits that were never pushed offers to push them
  Given new work has a commit that is not on the remote
  When the developer cleans it up
  Then they are offered to push the commits
  And once they are pushed, cleaning the work up is no longer refused
```

### Feature: Worktrees kept for a role

<!-- intent: the architect, 2026-11-09 - "When I add a worktree I would prefer to be pick role or
     branch specific. Meaning I will delete after it's branch is done"; "Role. Worktrees. Either
     branch or role. Based and recorded so the rule can value both"; a role worktree keeps its
     folder, session and ports while branches come and go in it (D-024) -->

```gherkin
@WORK-ROLE-001 @status:proposed @persona:Developer
Scenario: A {checkout.lower} made for a role is named for the role and holds the branch given
  Given a {project.lower} is registered
  When the developer makes a {checkout.lower} for the role "designer" on the branch "nav-rework"
  Then a {checkout.lower} named after the {project.lower} and "designer" exists beside the main one, on a new branch "nav-rework" from the remote's main
  And "designer" is recorded as one of the {project.lower}'s roles in the developer's settings
  And the {project.lower}'s page shows the {checkout.lower} as the designer's, on "nav-rework"
```

```gherkin
@WORK-ROLE-002 @status:proposed @persona:Developer
Scenario: A role {checkout.lower} moves on to its next branch in the same folder
  Given the "designer" {checkout.lower}'s branch is pushed and nothing in it is uncommitted
  When the developer moves it on to the branch "search-fix"
  Then the same {checkout.lower} is on a new branch "search-fix" from the remote's main
  And the earlier branch is still on the remote
```

```gherkin
@WORK-ROLE-003 @status:proposed @persona:Developer
Scenario: Moving a role {checkout.lower} on is refused while work would be lost
  Given the "designer" {checkout.lower} has a file changed but not committed
  When the developer moves it on to another branch
  Then they are told it has uncommitted changes, naming the file, and offered to save or put them aside
  And the {checkout.lower} is still on its branch, with the change
```

```gherkin
@WORK-ROLE-004 @status:proposed @persona:Developer
Scenario: A name already used by branch work is refused for a role
  Given new work called "designer" exists on the {project.lower}
  When the developer makes a {checkout.lower} for the role "designer"
  Then they are told the name is taken by branch work
  And no role is recorded
```

```gherkin
@WORK-ROLE-005 @status:proposed @persona:Developer
Scenario: Cleaning up a role {checkout.lower} forgets the role
  Given the "designer" {checkout.lower} has nothing uncommitted or unpushed
  When the developer cleans it up and confirms
  Then its {checkout.lower} is removed
  And "designer" is no longer one of the {project.lower}'s roles
```

### Feature: Starting and stopping the app stack

<!-- intent: the architect, 2026-11-04 - "proper startup and tear down scripts per repo to run or
     shut down that work in each worktree. im relying on claude knowing how rather than a
     standard"; docs/decisions.md D-006 as amended: the stack starts on request, through the
     repository's up script -->

```gherkin
@WORK-STACK-001 @status:proposed @persona:Developer
Scenario: Starting the stack runs the repository's up script in the {checkout.lower}
  Given the {project.lower} declares an up script and has a {session.lower} in a {checkout.lower}
  When the developer starts the stack of that {session.lower}
  Then the up script runs in that {checkout.lower}, told which {project.lower}, work and {checkout.lower} it is
  And its command and output are shown, and kept in the history
```

<!-- intent: as WORK-STACK-001 - what up started is stopped the same standard way -->

```gherkin
@WORK-STACK-002 @status:proposed @persona:Developer
Scenario: Stopping the stack runs the repository's down script in the {checkout.lower}
  Given the {project.lower} declares a down script and has a {session.lower} in a {checkout.lower}
  When the developer stops the stack of that {session.lower}
  Then the down script runs in that {checkout.lower}
```

<!-- intent: issue #32 - "Clean up runs the stop script before teardown", so nothing the stack
     started is left running against a worktree that is gone -->

```gherkin
@WORK-STACK-003 @status:proposed @persona:Developer
Scenario: Cleaning up stops the stack before tearing the {checkout.lower} down
  Given the {project.lower} declares a down script and a teardown script
  When the developer cleans up new work on it and confirms
  Then the down script runs before the teardown script
  And both run before the {checkout.lower} is removed
```

<!-- intent: as WORK-STACK-001 - the standard is the script; without one the developer is told how
     to add it rather than the tool guessing -->

```gherkin
@WORK-STACK-004 @status:proposed @persona:Developer
Scenario: A {project.lower} without an up script is told how to add one
  Given the {project.lower} declares no up script and has a {session.lower} in a {checkout.lower}
  When the developer starts the stack of that {session.lower}
  Then they are told the repository declares no up script, and what one must do
  And nothing is run
```

<!-- intent: the architect, 2026-11-07 - a stack started with no session ran on the engine but could
     not be reached from the client, which forwards running sessions' ports only: "the session must
     exist ... the run button should start a session if needed" -->

```gherkin
@WORK-STACK-005 @status:proposed @persona:Developer
Scenario: Starting the stack of a {checkout.lower} with no {session.lower} starts its {session.lower} first
  Given the {project.lower} declares an up script and its {checkout.lower} has no running {session.lower}
  When the developer starts the stack of that {checkout.lower} from the app
  Then its {session.lower} is started in the background, as the app starts {session.lower}s, before the up script runs
  And no terminal window is opened
```

### Feature: What runs for a session lives as long as the session

<!-- intent: the architect, 2026-11-07 - "anything run for the session like starting the app
     stack, should be tied to the session. shutting down the session totally vs disconnecting
     should shut down everything related"; "the point was only within the lifetime of the
     session"; docs/decisions.md D-020 -->

```gherkin
@WORK-STACK-006 @status:proposed @persona:Developer
Scenario: Stopping a {session.lower} stops what was started for it, and nothing else
  Given a {checkout.lower}'s {session.lower} is running with its stack started
  And a server was started from inside that {session.lower}
  And another server on the {project.lower}'s ports was started outside any {session.lower}
  When the developer stops that {session.lower} and confirms
  Then the down script runs before the {session.lower} ends
  And the server started inside the {session.lower} is stopped
  And the server started outside any {session.lower} is still running
```

<!-- intent: as WORK-STACK-006 - "shutting down the session totally vs disconnecting" -->

```gherkin
@WORK-STACK-007 @status:proposed @persona:Developer
Scenario: Leaving a {session.lower} without stopping it leaves its stack running
  Given a {session.lower} is running with its stack started, and the developer has joined it
  When the developer closes the terminal window
  Then the {session.lower} is still running
  And so is its stack
```

<!-- intent: as WORK-STACK-006 - a session can end without the program (quit from inside Zellij,
     or killed by hand), and what it started must not outlive it -->

```gherkin
@WORK-STACK-008 @status:proposed @persona:Developer
Scenario: The stack of a {session.lower} that ended outside the program is stopped when the {engine.lower} next checks
  Given a {session.lower}'s stack was started
  And the {session.lower} was ended from inside Zellij more than two minutes ago
  When the {engine.lower} checks its {session.lowerPlural}
  Then the down script has run in that {checkout.lower}
  And the history says the stack was stopped because its {session.lower} had ended
```

<!-- intent: as WORK-STACK-008 - a session quit by mistake and joined again straight away keeps
     its stack -->

```gherkin
@WORK-STACK-009 @status:proposed @persona:Developer
Scenario: A {session.lower} that ended only a moment ago keeps its stack
  Given a {session.lower}'s stack was started
  And the {session.lower} was ended from inside Zellij less than two minutes ago
  When the {engine.lower} checks its {session.lowerPlural}
  Then nothing is stopped
```

<!-- intent: the architect, 2026-11-07 - restart brings the same session back on the current
     layout, so it is not an end of the session's life -->

```gherkin
@WORK-STACK-010 @status:proposed @persona:Developer
Scenario: Restarting a {session.lower} keeps its stack running
  Given a {session.lower} is running with its stack started
  When the developer restarts it and confirms
  Then its stack is still running
```

<!-- intent: as WORK-STACK-006 - what belongs to no session is the developer's to stop: it is shown
     and never stopped for them -->

```gherkin
@WORK-STACK-011 @status:proposed @persona:Developer
Scenario: A server on a {project.lower}'s ports that belongs to no {session.lower} is shown, not stopped
  Given a server on a {project.lower}'s ports was started outside any {session.lower}
  When the {engine.lower} checks its {session.lowerPlural}
  Then the server is still running
  And the {project.lower}'s page lists it with its port, its process, when it started and its folder, and offers to stop that one
```

<!-- intent: as WORK-STACK-006 - a stack belongs to the session of the checkout it was started for,
     wherever the request came from -->

```gherkin
@WORK-STACK-012 @status:proposed @persona:Developer
Scenario: A stack started from inside another {session.lower} belongs to its own {checkout.lower}'s {session.lower}
  Given two {checkout.lowerPlural} each have a running {session.lower}
  And from inside the first, the developer started the stack of the second
  When the developer stops the first {session.lower} and confirms
  Then the second {checkout.lower}'s stack is still running
```

<!-- intent: as WORK-STACK-006 - found 2026-11-07: an editor opened from inside a session carries
     the session's name too; it is the developer's to close, and only what serves on a port
     (with what started it and what it started) is stopped for a session -->

```gherkin
@WORK-STACK-013 @status:proposed @persona:Developer
Scenario: An app opened from inside a {session.lower} stays open when the {session.lower} is stopped
  Given a {checkout.lower}'s {session.lower} is running
  And an app that serves nothing was opened from inside that {session.lower}
  When the developer stops that {session.lower} and confirms
  Then the app is still running
```

<!-- intent: the architect, 2026-11-08: "this should be enabled in the server hub when it starts.
     and on local only enabled when needed when running local. and not a seperate thing that runs
     all the time" - the sweep lives with the app that starts it, never as a job of its own -->

```gherkin
@WORK-STACK-014 @status:proposed @persona:Developer
Scenario: The sweep runs with what started it and ends with it
  Given a {session.lower} of a {checkout.lower} has ended, leaving what Zellij keeps of it
  When the sweep's watch is started
  Then the ended {session.lower} is forgotten at once
  And when what started the watch ends, the watch ends too
```

<!-- intent: the architect, 2026-11-09 - clicking one of a {checkout.lower}'s links "did not check
     and run the up.. get to dead browser". Opening a link first asks the machine its stack runs
     on whether anything answers on the link's port, and starts the app when nothing does -->

```gherkin
@WORK-STACK-015 @status:proposed @persona:Developer
Scenario: Opening a link whose app is not running starts the app first
  Given a {checkout.lower} of a {project.lower} with an up script has a {session.lower}, and nothing answers on its port
  When the developer opens one of its links
  Then its up script runs in that {checkout.lower} before the link is opened
  And opening a link when something answers on its port starts nothing
```
