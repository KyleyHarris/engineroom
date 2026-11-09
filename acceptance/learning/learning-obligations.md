# Learning - engineering obligations (tier 2)

The checks no user would describe, for the unit of work that delivers `learning-acceptance.md`.
Ids come from the criteria-review catalogue (O1 to O11).

| Obligation | What it means here | Tests owed |
|---|---|---|
| O1 Audit before mutation | An action's entry is written to the history before its first command runs, then completed, so a crash still leaves a record | Kill the process mid-action; assert a started, unfinished entry |
| O4 Races | Two copies of the tool writing the history at once lose and interleave nothing | Concurrent writers; count and parse entries |
| O5 Interruption | Ctrl-C mid-action records the action as interrupted, with the commands that ran | Interrupt; read the entry |
| O6 Best effort | A history that cannot be written never fails the action; the failure is reported once | Read-only history location |
| O7 Shown equals run | The command shown is generated from the same arguments that run, and pasting it reproduces them exactly, including spaces, quotes, `$` and newlines | Round trip through a real shell for each shape |
| O8 Malformed input | A corrupt or truncated history is read as far as it is valid, the rest reported, never a crash | Truncated file; garbage line |
| O9 Boundaries | Very long command output is shortened on screen and kept whole in the history; the history has a stated size limit | Output at and over the screen limit; history at its limit |
| O10 Secrets | Values of secrets (tokens, passwords, keys) never appear in a shown command or in the history | Run an action carrying a secret; search screen and history |
