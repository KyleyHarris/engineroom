# Sessions - engineering obligations (tier 2)

The checks no user would describe, for the unit of work that delivers `sessions-acceptance.md`.
Ids come from the criteria-review catalogue (O1 to O11). Proved by tests against a real terminal
multiplexer in an isolated socket directory, never by a journey.

| Obligation | What it means here | Tests owed |
|---|---|---|
| O2 No silent partial success | A {session.lower} created whose layout or join then failed is reported as created and failing, never left running unannounced | Fail the layout; fail the join; assert the report and the state |
| O3 Idempotency | Opening the same {project.lower} twice in quick succession gives one {session.lower} | Two opens back to back; count sessions |
| O4 Races | Two machines opening the same {project.lower} at the same moment get one {session.lower}, and both join it | Concurrent opens from two processes |
| O5 Interruption | Ctrl-C while a {session.lower} is being created leaves either a complete {session.lower} or none | Interrupt at each step of creation |
| O6 Best effort | The "waiting for the developer" signal failing or arriving late never blocks Claude Code and never fails the list; a signal older than a fixed limit is shown as unknown, because such a signal can be missed | Break the signal; delay it past the limit |
| O8 Malformed input | {checkout.lower} folder names with spaces, dots and non-ASCII characters map to valid, distinct {session.lower} names; a malformed {project.lower} registry is reported with the file and line | One fixture per shape |
| O9 Boundaries | The longest {session.lower} name the multiplexer accepts on this platform, one under and one over; over is shortened deterministically and stays distinct | Measure the real limit first, then test at it |
| O10 Authorisation | Any running {session.lower} can be stopped, but only the one chosen: never by name pattern, and never a process outside that {session.lower} | Two sessions with overlapping names; stop one |
| Dependency down | The multiplexer missing or the wrong version is reported with what is needed | Missing binary; old version |

