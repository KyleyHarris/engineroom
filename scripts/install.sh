#!/usr/bin/env bash
#
# Builds engineroom from this clone and installs it for every shell on this machine, including the
# non-login shell an SSH command runs in, which is how a client reaches the engine. Run it on the
# engine and on every client, from the same commit, so they speak the same version.
#
#   scripts/install.sh            install into ~/.local/bin
#   scripts/install.sh --app      also build the menu bar app into ~/Applications/engineroom.app
#   scripts/install.sh --local    set this Mac's role to local: used on its own, never reached over
#                                 SSH (docs/decisions.md D-039); combines with --app
#   ENGINEROOM_BIN=/some/dir ENGINEROOM_APPS=/some/dir scripts/install.sh --app
#
# It changes nothing but the installed binary (and, with --local, the role in the configuration).
# Every command it runs is printed first. It
# never edits a shell's startup files: when the folder is not on the PATH an SSH command sees,
# it says which line to add and where, and stops with an error.

set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

bin="${ENGINEROOM_BIN:-$HOME/.local/bin}"
apps="${ENGINEROOM_APPS:-$HOME/Applications}"
with_app=0
local_role=0
for arg in "$@"; do
  case "${arg}" in
    --app) with_app=1 ;;
    --local) local_role=1 ;;
    *) echo "usage: scripts/install.sh [--app] [--local]" >&2; exit 2 ;;
  esac
done
run() { printf '+ %s\n' "$*"; "$@"; }

if ! command -v go >/dev/null 2>&1; then
  echo "install: Go is needed to build engineroom (brew install go)" >&2
  exit 1
fi

# The version names the commit, and says so when the clone has changes not committed, so
# "engineroom version" on two machines tells whether they run the same code.
version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

run mkdir -p "${bin}" || exit 1
# The clone it was built from is recorded too, so a client can tell from git's ancestry whether
# the engine's build or its own is the older one.
run env GOBIN="${bin}" go install -ldflags "-X main.version=${version} -X main.built=$(pwd)" ./cmd/engineroom || exit 1
run "${bin}/engineroom" version || exit 1

if [ "${local_role}" -eq 1 ]; then
  run "${bin}/engineroom" role set local || exit 1
fi

# An SSH command runs zsh with neither a login nor an interactive shell, so only ~/.zshenv
# sets its PATH. Checked with exactly that, from a bare environment. A Mac used on its own is never
# reached by an SSH command (D-039), so it is not checked there: by --local, or by its role already
# being local.
role="$("${bin}/engineroom" role show 2>/dev/null)"
found="$(env -i HOME="$HOME" PATH=/usr/bin:/bin:/usr/sbin:/sbin /bin/zsh -c 'command -v engineroom' 2>/dev/null)"
if [ "${role}" != "local" ] && [ "${found}" != "${bin}/engineroom" ]; then
  echo
  echo "install: an SSH command on this machine would not find ${bin}/engineroom (it finds: ${found:-nothing})." >&2
  echo "Add this line to ~/.zshenv, then run this script again:" >&2
  echo "  export PATH=\"${bin}:\$PATH\"" >&2
  exit 1
fi

if [ "${with_app}" -eq 1 ]; then
  # The menu bar app (docs/decisions.md D-008): built from macos/, then put in place signed by
  # scripts/install-app.sh, with this Mac's Apple Development identity when it has one and never
  # ad hoc in its place (D-038). A locally built app needs no notarization.
  if ! command -v swift >/dev/null 2>&1; then
    echo "install: Swift is needed for the app (install Xcode, or: xcode-select --install)" >&2
    exit 1
  fi
  run swift build -c release --package-path macos || exit 1
  app="${apps}/engineroom.app"
  scripts/install-app.sh macos/.build/release/EngineRoomMenu macos/bundle/Info.plist "${version}" "${apps}" || exit 1
  echo
  # An empty environment, so the app starts as when opened from the Dock, not with this shell's
  # variables (docs/decisions.md D-030).
  echo "Installed the app at ${app}. Open it with: env -i open \"${app}\""
  echo "If an older copy is running, quit it from its Settings first."
fi

echo
if [ "${role}" = "local" ]; then
  echo "Installed engineroom ${version} at ${bin}/engineroom, for this Mac used on its own (role local)."
else
  echo "Installed engineroom ${version} at ${bin}/engineroom; an SSH command finds it too."
fi
echo "engineroom's Claude Code plugin (its skills and hooks) installs from inside Claude Code:"
echo "  /plugin install engineroom --marketplace KyleyHarris/engineroom"
