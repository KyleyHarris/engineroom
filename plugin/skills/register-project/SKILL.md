---
name: register-project
description: Register the current git repository (or a named folder) with engineroom, so it can be opened, given new worktrees and listed by name. Use when the developer says "register this repo", "add this project to engineroom", "make engineroom know about this repository".
---

# Register a repository with engineroom

`engineroom` knows repositories by name once they are registered. Registering changes only the
program's own configuration; it never touches the repository.

1. Find the repository's root: `git rev-parse --show-toplevel`. If that fails, say the folder
   is not a git repository and stop.
2. Show the developer the command you will run, then run it:

   ```sh
   engineroom project add <root>            # named after the folder
   engineroom project add <root> --name <n> # or a name they choose
   engineroom project add <root> --category <c>  # listed under that category's heading
   ```

   Ask which category it belongs in when the developer has not said; a category is only a
   label in engineroom, so leaving it out is fine and it can be set later by registering again with
   `--category`.

3. Report what it printed, word for word: the name it was registered as, and which setup
   scripts (`.engineroom/create`, `.engineroom/up`, `.engineroom/status`, `.engineroom/down`, `.engineroom/teardown`) the repository declares.
4. If it says the repository is already registered, say under which name; do not register it
   again under another.

To see what is registered: `engineroom projects`. To forget one: `engineroom project remove <name>`
(nothing on disk changes).

To make the project run in its worktrees (its ports, its up and down scripts, its secrets), use the
`adopt-project` skill next.
