# Contributing

Thank you for taking an interest. This project is small and opinionated about process, because
the process is what keeps it trustworthy: every behaviour is written down before it is built,
and every test can be shown to fail.

## How work is tracked

- Every piece of work starts as a GitHub issue. A feature issue names the acceptance scenarios
  it delivers by id (for example `SESS-NEW-001`).
- Acceptance criteria live in `acceptance/<area>/<area>-acceptance.md`, written to the
  [criteria-review](https://www.npmjs.com/package/criteria-review) standard. Engineering checks
  that no user would describe live beside them in `<area>-obligations.md`.
- Decisions, with the options that were weighed, are in `docs/decisions.md`. Check there before
  reopening a settled question.

## Making a change

1. Comment on the issue, or open one, before starting anything non-trivial.
2. Branch from `main`. Branch names are hyphenated slugs with no slashes, starting with the
   issue number: `12-session-list`.
3. Write the test first and watch it fail for the reason the scenario names. A test that has
   never been seen failing is not trusted.
4. Never edit an acceptance criterion to make a test pass. If the criterion looks wrong, say so
   on the issue.
5. Run `scripts/verify.sh` and make sure it exits 0.
6. Open a pull request using the template. It is merged with a merge commit, never squashed.

## Requirements

- Go (the version in `go.mod`) and `staticcheck`
  (`go install honnef.co/go/tools/cmd/staticcheck@latest`)
- `shellcheck` (`brew install shellcheck`), used by the gate to check the shell scripts
- `criteria-review` on `PATH` (`npm install -g criteria-review`), used by the gate to check the
  acceptance documents and to generate the scenario bindings
- Zellij, git and OpenSSH, which the journeys run for real
- Docker, for the throwaway engine the journeys reach over SSH (`testenv/engine`)
- `agg` and `ffmpeg`, only to render journey recordings to video

## The two commands

- `scripts/verify.sh`: the fast gate. Formatting, the generated scenario bindings, vet
  (journeys included), every journey implementing every clause, staticcheck, build, unit
  tests and the website. Run it before every pull request.
- `scripts/journeys.sh`: the journeys, against real Zellij, git and SSH. Set
  `JOURNEY_RECORD=<dir>` to record each one and render it to video.

The website is built from `docs/` and the pages in `site/` by `go run ./tools/site`, into
`site/out` (not committed). To read it while working on it, `go run ./tools/site -serve` serves it
on <http://localhost:20800/> (`-port` for another), builds it again on every save and reloads open
pages; with engineroom, `engineroom up` does the same on the checkout's own slot port
(`.engineroom/up`, `.engineroom/down`). The gate builds and checks it
too, so a doc edit that breaks a link or an anchor fails there. The
product's name, its command and its folders live only in `site/site.toml`; pages read them from
there.

After editing an acceptance document, run `go run ./tools/gen-scenarios` before the tests, or
the tests prove the previous requirement.

## Rules that apply to every change

- **No personal data.** No real machine names, addresses, user names, home paths or port
  numbers from anyone's own setup, in code, tests, fixtures, docs or commit messages. Use
  invented names such as `project-a` and `engine.example`.
- **Nothing hidden.** Every action the program takes is a list of real commands the user can
  see before, during and after it runs. A new action adds its commands to that view.
- **Comments state intent.** Say why the code does what it does, so behaviour can be checked
  against what was meant.

## Releasing

A release is a version number, `vX.Y.Z`, never a commit (docs/decisions.md D-040).

1. Move the `## [Unreleased]` entries of `CHANGELOG.md` under `## [X.Y.Z] - <date>`, and merge that
   to `main`. The release's notes are that section; a tag without one stops the release.
2. Tag `main` and push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The `release` workflow builds the program and the app for Apple Silicon and Intel
   (`scripts/release-build.sh`), signs them with the Developer ID, notarizes them
   (`scripts/release-notarize.sh`), publishes them on the GitHub release, and writes the formula and
   cask into the tap, `KyleyHarris/homebrew-engineroom` (`scripts/release-tap.sh`).

A pull request that changes how a release is built runs the same workflow as a dry run, signed ad
hoc and never published. Its secrets are the Developer ID certificate
(`DEVELOPER_ID_P12_BASE64`, `DEVELOPER_ID_P12_PASSWORD`), an App Store Connect API key
(`NOTARY_KEY_P8`, `NOTARY_KEY_ID`, `NOTARY_ISSUER_ID`) and the tap's deploy key (`TAP_DEPLOY_KEY`).
