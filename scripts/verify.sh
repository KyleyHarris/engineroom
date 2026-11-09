#!/usr/bin/env bash
#
# The one fast gate for this repository. It CHECKS and never rewrites: a formatter that fixes
# files here would let unformatted code pass in CI and change on a developer's machine.
#
# Order matters only for speed: the cheapest checks run first so a broken document or an
# unformatted file fails in a second rather than after a build.
#
# Journeys are not run here. They drive real terminals, real SSH and real sessions, and run as
# their own command (see CONTRIBUTING.md and docs/decisions.md D-003).

set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

failed=0
step() { printf '\n== %s\n' "$1"; }
fail() { printf 'FAIL: %s\n' "$1"; failed=1; }

# criteria-review is resolved from PATH only. A gate must never download its own checker:
# a missing tool would then read as a pass, or put a network fetch inside the check.
step "acceptance documents"
if command -v criteria-review >/dev/null 2>&1; then
  criteria-review terms check --strict || fail "acceptance documents: a marker does not resolve, or a term is spelled instead of named"
else
  fail "criteria-review is not on PATH (npm install -g criteria-review)"
fi

# The shell scripts: install, the gate itself, the journeys, the repository's own stack scripts in
# .engineroom, and the stack helper engineroom hands to up and down scripts. shellcheck is resolved
# from PATH, never downloaded, like the tools above.
step "shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck scripts/*.sh .engineroom/up .engineroom/down || fail "shellcheck: a shell script has a finding above"
  shellcheck -s bash internal/stacklib/stack.sh || fail "shellcheck: the stack helper has a finding above"
else
  fail "shellcheck is not on PATH (brew install shellcheck)"
fi

# The Claude Code plugin and the marketplace that lists it, checked by Claude Code's own validator
# when Claude Code is installed here; a machine without it (CI) says so and goes on.
step "plugin"
if command -v claude >/dev/null 2>&1; then
  claude plugin validate ./plugin >/dev/null || { claude plugin validate ./plugin; fail "the plugin does not validate"; }
  claude plugin validate . >/dev/null || { claude plugin validate .; fail "the marketplace does not validate"; }
else
  echo "claude is not on PATH: the plugin is not validated here"
fi

if [ -f go.mod ]; then
  step "gofmt"
  unformatted="$(gofmt -l . 2>&1)"
  if [ -n "${unformatted}" ]; then
    printf '%s\n' "${unformatted}"
    fail "gofmt: the files above are not formatted (run gofmt -w on them)"
  fi

  step "generated scenario bindings"
  go run ./tools/gen-scenarios -check || fail "internal/scenarios is out of date with acceptance/ (go run ./tools/gen-scenarios)"

  step "go vet (including the journeys, so a reworded clause fails here, not only when journeys run)"
  go vet ./... || fail "go vet"
  go vet -tags journeys ./journeys/ || fail "go vet (journeys)"

  step "every journey implements every clause"
  go run ./tools/clausecheck journeys || fail "a journey leaves a clause unimplemented"

  # staticcheck is resolved, never downloaded, for the same reason as criteria-review above.
  step "staticcheck"
  sc="$(command -v staticcheck || true)"
  [ -z "${sc}" ] && [ -x "$(go env GOPATH)/bin/staticcheck" ] && sc="$(go env GOPATH)/bin/staticcheck"
  if [ -n "${sc}" ]; then
    "${sc}" ./... || fail "staticcheck"
    "${sc}" -tags journeys ./journeys/ || fail "staticcheck (journeys)"
  else
    fail "staticcheck is not installed (go install honnef.co/go/tools/cmd/staticcheck@latest)"
  fi

  step "go build"
  go build ./... || fail "go build"

  step "go test"
  go test ./... || fail "go test"

  # The website is built from the docs, so a doc change can break it: a link to a heading that
  # was renamed, a decision named that does not exist. Built into a temporary folder and checked
  # there; nothing is fetched, and nothing in site/out is touched.
  step "website (built from docs/ and site/, then checked)"
  go run ./tools/site -check || fail "website: a link, an anchor or an image goes nowhere, or a page has a dash"
fi

# The menu bar app: built with warnings as errors, and its tests run. Skipped, and said so, on a
# machine without Swift (the app is macOS only); never skipped silently.
if [ -f macos/Package.swift ]; then
  step "swift (macos/)"
  if command -v swift >/dev/null 2>&1; then
    swift build --package-path macos -Xswiftc -warnings-as-errors >/dev/null || fail "swift build (macos/)"
    # Run once; the count is shown, and on a failure the end of the output, so the failing test
    # can be read where it ran (a CI log included).
    swift_out="$(swift test --package-path macos 2>&1)"
    swift_status=$?
    printf '%s\n' "${swift_out}" | grep -E "Executed [0-9]+ tests" | tail -1
    if [ "${swift_status}" -ne 0 ]; then
      printf '%s\n' "${swift_out}" | grep -E "error|failed|XCTAssert" | head -40
      fail "swift test (macos/)"
    fi
  else
    echo "SKIPPED: swift is not installed, so the menu bar app was not built or tested"
  fi
fi

if [ "${failed}" -ne 0 ]; then
  printf '\nverify: FAILED\n'
  exit 1
fi
printf '\nverify: passed\n'
