#!/usr/bin/env bash
#
# Puts a built menu bar app in place as <apps>/engineroom.app, signed (docs/decisions.md D-038).
# scripts/install.sh --app runs it after building the app; it is its own script so the signing
# rules can be tested without a build.
#
#   scripts/install-app.sh <built binary> <Info.plist template> <version> <apps folder>
#
# macOS keeps the permissions granted to an app (Automation, Screen Recording and the rest) for as
# long as its signature names the same signer. A Mac's Apple Development identity names the same
# signer on every build; an ad hoc signature is new on every build, and macOS then asks for every
# permission again. So where this Mac has an identity the app is signed with it or not installed
# at all, and ad hoc signing is only for a Mac with no identity.
#
# The new app is assembled and signed in a copy beside the installed one and put in place only
# once signing has succeeded, so a failed run leaves the installed app exactly as it was.

set -uo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: scripts/install-app.sh <built binary> <Info.plist template> <version> <apps folder>" >&2
  exit 2
fi
binary="$1"
plist="$2"
version="$3"
apps="$4"
run() { printf '+ %s\n' "$*"; "$@"; }

app="${apps}/engineroom.app"
# Beside the installed app, so moving it into place is a rename on the same volume. Named so
# Finder and Launch Services do not take it for an app.
staging="${apps}/.engineroom.app.new"
previous="${apps}/.engineroom.app.old"

run mkdir -p "${apps}" || exit 1
# Left by a run that was interrupted: never the installed app, so removed without asking.
run rm -rf "${staging}" "${previous}" || exit 1
run mkdir -p "${staging}/Contents/MacOS" || exit 1
run cp "${binary}" "${staging}/Contents/MacOS/engineroom-menu" || exit 1
printf '+ write %s\n' "${staging}/Contents/Info.plist"
sed "s/VERSION/${version}/g" "${plist}" > "${staging}/Contents/Info.plist" || exit 1

identity="$(security find-identity -v -p codesigning 2>/dev/null | awk '/Apple Development/ {print $2; exit}')"
if [ -n "${identity}" ]; then
  if ! run codesign --force --sign "${identity}" "${staging}"; then
    # The usual cause: the key's keychain is locked to this login (one started over SSH, or a
    # session started from one), or codesign has not been allowed to use the key without a
    # dialog. Signing ad hoc instead would make macOS ask for every permission again.
    rm -rf "${staging}"
    {
      echo
      echo "install: the app was not installed: this Mac has an Apple Development identity, and signing with it failed (above)."
      echo "The app already installed is unchanged. An app signed ad hoc instead would make macOS ask again for every permission it was granted."
      echo "Let codesign use the key from any session by running this once in Terminal on this Mac, with your login password:"
      echo "  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k '<login password>' ~/Library/Keychains/login.keychain-db"
      echo "then run the install again."
    } >&2
    exit 1
  fi
else
  # No identity: an ad hoc signature runs on this Mac, and is the only signature available.
  echo "install: this Mac has no Apple Development identity, so the app is signed ad hoc; macOS asks again for its permissions after each install." >&2
  if ! run codesign --force --sign - "${staging}"; then
    rm -rf "${staging}"
    echo "install: the app was not installed: signing it ad hoc failed (above). The app already installed is unchanged." >&2
    exit 1
  fi
fi

# Put the signed copy in place: the installed app is moved aside, not deleted, until the new one
# is in, so a failure here can be undone by moving it back.
if [ -e "${app}" ]; then
  run mv "${app}" "${previous}" || exit 1
fi
if ! run mv "${staging}" "${app}"; then
  [ -e "${previous}" ] && mv "${previous}" "${app}"
  echo "install: the app was not installed: moving it into place failed (above). The app installed before is back in place." >&2
  exit 1
fi
run rm -rf "${previous}" || exit 1
