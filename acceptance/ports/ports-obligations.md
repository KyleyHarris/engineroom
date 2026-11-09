# Ports - engineering obligations (tier 2)

The checks no user would describe, for the unit of work that delivers `ports-acceptance.md`.
Proved by unit tests against real git repositories and worktrees, real files and a real
listener, never by a mock.

| Obligation | What it means here | Test |
|---|---|---|
| O3 Idempotency | A second claim returns the slot already held; a second release reports nothing held; writing a slot's files again writes the same files | `TestClaims` (internal/slots) |
| O4 Races | Two worktrees claiming at the same moment get different slots: the claims file is changed only under a lock | `TestClaimsAtOnceNeverShare` (internal/slots), seen red with the lock removed |
| O6 Dependency down | A claims file that cannot be read is started again empty with a warning, never leaving every worktree unable to claim; an engine that does not answer leaves the forwards as they are | `TestAnUnreadableClaimsFileIsStartedAgain`; `syncForwards` returns before changing anything when the session list cannot be read |
| O8 Malformed input | Every way an engineroom.toml can be wrong is refused with the file and the entry named, never read as "nothing declared", and new work on a project whose settings are refused is refused with them before anything is made; a key that would overwrite another (a built-in, or one of another kind or spelling that folds to the same variable) is refused naming both; a template with a value missing is refused with its name | `TestAWrongFileIsRefusedWithWhatIsWrong`, `TestFill` (internal/engineroomfile), `TestKeysThatWouldOverwriteOneAnotherAreRefused` (internal/settings), `TestNewWorkIsRefusedWhenTheSettingsAre` (internal/ops) |
| O9 Boundaries | Ports 0, 1, 65535 and 65536; a slot taken last by a worktree (slot 1) and first by the main clone; a removed worktree's slot given out again; every slot held | `TestAWrongFileIsRefusedWithWhatIsWrong`, `TestClaims` |
| Clash detection | Two projects on one port, overlapping reservations, and a slot port outside its reservation are each reported with both sides named | `TestCheck` (internal/slots) |
| Settings precedence | The developer's settings win section by section and script by script; a repository's file fills only what they leave out; a script that is not executable is not used | `TestYourSettingsDecideAndTheRepositoryRecommends` (internal/settings) |
| Forwarding diff | Only changed forwards are made or cancelled; ended and skipped sessions are never forwarded; a port two sessions name, or one the client already uses, is said | `TestDesired`, `TestChanges`, `TestCommands` (internal/forward) |
| Socket path limit | The forwarding connection's control socket stays within the operating system's length limit whatever the home folder | `TestCommands` |
