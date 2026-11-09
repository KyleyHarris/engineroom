# Editor - engineering obligations (tier 2)

The checks no user would describe, for the units of work that deliver `editor-acceptance.md`.
Ids come from the criteria-review catalogue (O1 to O11).

| Obligation | What it means here | Tests owed |
|---|---|---|
| O6 Dependency down | On a {client.lower} the session list comes from the {engine.lower}; an {engine.lower} that does not answer for `engineroom edit` gets the same retry, work locally or stop question as every other action | `engineroom edit` with the {engine.lower} unreachable |
| O7 Validation | An unknown {session.lower} name is refused naming it; a {session.lower} with no registered folder is refused (EDIT-OPEN-004); an {engine.lower} that does not report a folder is refused rather than guessed | Unit: plan for each refusal |
| O8 Injection | A folder with spaces or shell syntax reaches {editor} as one argument; the {engine.lower}'s host is one argument | Unit: argument lists for awkward paths and hosts |
| O9 Cost | Opening from the session list uses the list already read; it makes no second request to the {engine.lower} | Unit: the plan is built from the listed {session.lower} alone |
