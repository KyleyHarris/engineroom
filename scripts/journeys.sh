#!/usr/bin/env bash
#
# Runs the end-to-end journeys: the built program in a real terminal against real Zellij, git
# and SSH (docs/decisions.md D-003). Kept out of scripts/verify.sh because it needs Docker for
# the throwaway engine and takes longer than a gate should.
#
#   scripts/journeys.sh                 run every journey
#   scripts/journeys.sh -run ConnSetup  pass go test flags through
#   JOURNEY_RECORD=dir scripts/journeys.sh   also record each journey and render it to video
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

if ! docker version >/dev/null 2>&1; then
  echo "journeys: Docker is not running; the engine journeys need it." >&2
  exit 1
fi
# Rebuilding is cheap when nothing changed, and guarantees the engine matches testenv/engine.
docker build -q -t engineroom-testengine:dev testenv/engine >/dev/null || exit 1

go test -tags journeys -count=1 "$@" ./journeys/
status=$?

if [ -n "${JOURNEY_RECORD:-}" ] && [ -d "${JOURNEY_RECORD}" ]; then
  go run ./tools/render-casts "${JOURNEY_RECORD}" || status=1
fi
exit $status
