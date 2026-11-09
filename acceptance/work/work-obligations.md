# New work - engineering obligations (tier 2)

The checks no user would describe, for the units of work that deliver `work-acceptance.md`.
Ids come from the criteria-review catalogue (O1 to O11).

| Obligation | What it means here | Tests owed |
|---|---|---|
| O2 No silent partial success | A worktree created whose script or session then failed is reported as created and failing, with clean up offered; an up or down script that fails is reported with what it may have left running, and a failing down stops clean up before anything is removed | Failing script (WORK-NEW-006); failing session start; failing up and down scripts |
| O3 Idempotency | Starting the same work twice opens it once; create, up and down scripts may be run again safely | WORK-NEW-004; script contract documented (D-006, the run-stack skill) |
| O4 Races | Two machines starting the same work at once get one worktree | Concurrent starts |
| O5 Interruption | Ctrl-C during creation leaves a worktree that can be cleaned up, never one the tool cannot see | Interrupt after the worktree exists |
| O7 Validation | Work names follow the branch rule; unknown projects are refused | WORK-NEW-003 |
| O10 Authorisation | Clean up never removes the main clone, never deletes main or release branches, and deletes a branch only when merged | WORK-CLEAN-004 to 006; a release branch fixture |
| O11 Deletion | Uncommitted or unpushed work is never removed without an explicit discard | WORK-CLEAN-002 and 003 |
