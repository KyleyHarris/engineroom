# Connection - engineering obligations (tier 2)

The checks no user would describe, for the unit of work that delivers
`connection-acceptance.md`. Ids come from the criteria-review catalogue (O1 to O11). Each line is a
list of tests owed, proved by tests against real ssh and a real unreachable address, never by a
journey.

| Obligation | What it means here | Tests owed |
|---|---|---|
| O2 No silent partial success | An action that ran part of its commands on the {engine.lower} before the connection dropped reports which ran and which did not | Drop the connection between two remote commands; assert the report names both sides |
| O5 Interruption | Ctrl-C while the {engine.lower} is being checked, or at the retry prompt, starts nothing and leaves no background connection behind | Interrupt at each point; assert no new process on either machine |
| O7 Validation at the boundary | A machine configuration with no role, an unknown role, or no {engine.lower} address is refused with the file and the field named, before anything runs | One test per missing or invalid field |
| O8 Malformed input | A {project.lower} name containing spaces, quotes, `;`, `$(...)` or a newline reaches the {engine.lower} as one argument, unchanged | Round trip each name through a real remote command and compare |
| O9 Boundaries | An {engine.lower} that accepts the connection but never answers is treated as not answering within a fixed limit, never a hang | Point at a listener that never speaks; assert the prompt appears within the limit |
| O10 Authorisation | Connections go only to the configured {engine.lower}, never prompt for a password, and a changed host key is reported as a changed host key, not offered as a retry | Changed host key fixture; password-only host fixture |
| Dependency down | ssh not installed, name resolution failing, and the private network being down are each reported as themselves | One test per cause |

## The handoff (CONN-HANDOFF)

Proved by unit tests in `internal/handoff/handoff_test.go`, against real git with a bare
repository standing in for GitHub and two clones for the two machines.

| Obligation | What it means here | Test |
|---|---|---|
| O2 No silent partial success | A push GitHub refuses stops the handoff before any worktree is moved, and says how to go on | `TestARefusedPushChangesNoWorktree` |
| O3 Idempotency | A handoff run twice hands nothing over the second time | `TestASecondHandoffHasNothingToDo` |
| O8 Malformed input | A registered folder that is gone, is not a repository, or has no origin is named and never handed over | `TestUnusableProjectsAreNamed` |
| O9 Boundaries | Untracked files never hold a worktree back; a new branch is pushed only when it holds commits | `TestUntrackedFilesDoNotHoldAWorktreeBack`, `TestNewBranches` |
| Work kept local stays local | A branch with unpushed commits in no worktree is named, never pushed; a branch in a linked worktree is pushed from that worktree | `TestABranchInNoWorktreeIsNotPushed`, `TestALinkedWorktreesBranchIsPushedFromIt` |
| Identity | Two repositories under one name are never mixed; one repository reached by different addresses is one | `TestDifferentRepositoriesAreNotMixed`, `TestSameRemote` |
| Work is never overwritten | A receiving worktree with its own commits, or a giving branch GitHub has also moved on, gets a note and no command | `TestAReceivingWorktreeWithItsOwnCommitsIsLeftAlone`, `TestADivergedBranchIsNotPushed` |
| O6 Dependency down | GitHub unreadable: branches are compared with what was last fetched, and said; the giving machine unreadable: nothing is planned, and said | `TestGitHubUnreadable`, `TestAnUnreadGivingMachineIsSaid` |
