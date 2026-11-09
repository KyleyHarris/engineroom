# Projects - engineering obligations (tier 2)

The checks no user would describe, for the unit of work that delivers `projects-acceptance.md`.
Ids come from the criteria-review catalogue (O1 to O11).

| Obligation | What it means here | Tests owed |
|---|---|---|
| O3 Idempotency | Registering the same folder twice, by path or through a symbolic link to it, gives one entry | Register by path and through a link |
| O5 Interruption | An interrupted save never leaves a half-written configuration | Saves write a temporary file and rename it (covered by the configuration's save) |
| O7 Validation | A name outside letters, digits, '.', '_' and '-' is refused before anything is saved | One case per rule |
| O8 Malformed input | A folder path with spaces and non-ASCII characters registers and opens | Register such a folder and open it |
| O9 Boundaries | Two projects may not share a name; removing a name that is not registered is reported, not an error that changes anything | Duplicate name; unknown name |
