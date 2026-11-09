# engineroom

A Go CLI and terminal UI that runs Claude Code sessions inside Zellij on one machine, the
engine, and lets any client machine find, join, create and work on them. One binary; each machine is configured
as the engine or a client. Decisions live in `docs/decisions.md`; acceptance criteria in
`acceptance/<area>/`, to the criteria-review standard.

## Rules specific to this repo

- **No personal data, ever.** This repo may become public, and git history cannot be taken
  back. No machine names, Tailscale or IP addresses, user names, home paths, account names,
  project names or port numbers belonging to a real setup: not in code, tests, fixtures,
  examples, docs or commit messages. A real user's setup lives in their own config file outside
  this repo. Tests and examples use invented names (`project-a`, `engine.example`).
- **The program never hides what it does.** Every action it takes is a list of real commands
  that the user can see before it runs, sees while it runs, and finds afterwards in the
  history. A change that adds an action adds its commands to that view.
- **Nothing outside the program's own state is changed unasked.** It may report a problem and
  show the fix; it applies a fix only on an explicit request. It stops a session or a process
  only when the developer has chosen that exact one, or ended the session it was started for
  (docs/decisions.md D-020), and never by matching a name pattern.

<!-- rhythm: propagated from global CLAUDE.md -->
## Working rhythm (resumable sessions)

- Work proceeds in **units**, not atoms: a unit is a coherent change sharing one mental model,
  run to verified completion (build + tests pass) in one context.
- The durable handoff between sessions is the per-branch plan file at
  `.claude/local/plans/<branch-slug>.md`: what is DONE, the exact NEXT steps, and the resume
  pointer. Durable design decisions are promoted to committed docs (`CLAUDE.md`,
  `docs/decisions.md`), never left only in the plan file.
- **Handback:** do not commit at unit end. Leave the change uncommitted and present the draft
  commit message as the review brief. Commits happen only on an explicit ask.
- At the end of a verified unit, emit the `UNIT COMPLETE` block (plan updated, recommend
  `/clear`, give the resume line). Mid-unit under heavy context, emit `CONTEXT HEAVY` and
  suggest `/compact`; never recommend `/clear` mid-unit.
- On any new session, first read the current branch's plan, restate the current unit in a
  line or two, and continue from the resume pointer instead of re-exploring.
