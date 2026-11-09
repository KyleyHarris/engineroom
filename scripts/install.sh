#!/usr/bin/env bash
#
# Builds engineroom from this clone and installs it for every shell on this machine, including the
# non-login shell an SSH command runs in, which is how a client reaches the engine. Run it on the
# engine and on every client, from the same commit, so they speak the same version.
#
#   scripts/install.sh            install into ~/.local/bin
#   scripts/install.sh --app      also build the menu bar app into ~/Applications/engineroom.app
#   ENGINEROOM_BIN=/some/dir ENGINEROOM_APPS=/some/dir scripts/install.sh --app
#
# It changes nothing but the installed binary. Every command it runs is printed first. It
# never edits a shell's startup files: when the folder is not on the PATH an SSH command sees,
# it says which line to add and where, and stops with an error.

set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

bin="${ENGINEROOM_BIN:-$HOME/.local/bin}"
apps="${ENGINEROOM_APPS:-$HOME/Applications}"
with_app=0
for arg in "$@"; do
  case "${arg}" in
    --app) with_app=1 ;;
    *) echo "usage: scripts/install.sh [--app]" >&2; exit 2 ;;
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

# An SSH command runs zsh with neither a login nor an interactive shell, so only ~/.zshenv
# sets its PATH. Checked with exactly that, from a bare environment.
found="$(env -i HOME="$HOME" PATH=/usr/bin:/bin:/usr/sbin:/sbin /bin/zsh -c 'command -v engineroom' 2>/dev/null)"
if [ "${found}" != "${bin}/engineroom" ]; then
  echo
  echo "install: an SSH command on this machine would not find ${bin}/engineroom (it finds: ${found:-nothing})." >&2
  echo "Add this line to ~/.zshenv, then run this script again:" >&2
  echo "  export PATH=\"${bin}:\$PATH\"" >&2
  exit 1
fi

if [ "${with_app}" -eq 1 ]; then
  # The menu bar app (docs/decisions.md D-008): built from macos/, put in an app bundle, and
  # signed with this Mac's Apple Development identity when it has one (otherwise ad hoc, which
  # runs on this Mac only). A locally built app needs no notarization.
  if ! command -v swift >/dev/null 2>&1; then
    echo "install: Swift is needed for the app (install Xcode, or: xcode-select --install)" >&2
    exit 1
  fi
  run swift build -c release --package-path macos || exit 1
  app="${apps}/engineroom.app"
  run mkdir -p "${app}/Contents/MacOS" || exit 1
  run cp macos/.build/release/EngineRoomMenu "${app}/Contents/MacOS/engineroom-menu" || exit 1
  printf '+ write %s\n' "${app}/Contents/Info.plist"
  sed "s/VERSION/${version}/g" macos/bundle/Info.plist > "${app}/Contents/Info.plist" || exit 1
  identity="$(security find-identity -v -p codesigning 2>/dev/null | awk '/Apple Development/ {print $2; exit}')"
  # The identity's key is in the login keychain, which is locked to a session started over SSH;
  # signing then fails, and an ad hoc signature, enough to run on this Mac, is used instead.
  if [ -z "${identity}" ] || ! run codesign --force --sign "${identity}" "${app}"; then
    echo "install: signing with an Apple Development identity was not possible here; signing ad hoc (runs on this Mac)." >&2
    run codesign --force --sign - "${app}" || exit 1
  fi
  echo
  echo "Installed the app at ${app}. Open it with: open \"${app}\""
  echo "If an older copy is running, quit it from its Settings first."
fi

echo
echo "Installed engineroom ${version} at ${bin}/engineroom; an SSH command finds it too."
echo "engineroom's Claude Code plugin (its skills and hooks) installs from inside Claude Code:"
echo "  /plugin install engineroom --marketplace KyleyHarris/engineroom"
