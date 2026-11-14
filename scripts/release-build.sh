#!/usr/bin/env bash
#
# Builds a release of engineroom: the command line program and the menu bar app, for Apple Silicon
# only (docs/decisions.md D-041), signed, and packed as the two downloads a release carries
# (D-040). The release workflow runs it; it runs the same on a Mac by hand.
#
#   scripts/release-build.sh <vX.Y.Z> <out folder>
#
# A release is named by its version, never a commit: the program says vX.Y.Z and the app X.Y.Z.
# With SIGN_IDENTITY set (a Developer ID Application identity in a keychain codesign can use), both
# are signed with it, with the hardened runtime and a secure timestamp, as notarization requires.
# Without it they are signed ad hoc: a dry run that proves the build, never published.
#
# It writes only into the out folder: engineroom (the program), engineroom.app,
# engineroom-<X.Y.Z>-macos.tar.gz and engineroom-app-<X.Y.Z>.zip. Notarizing them is
# scripts/release-notarize.sh, which needs Apple's credentials.

set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

if [ "$#" -ne 2 ]; then
  echo "usage: scripts/release-build.sh <vX.Y.Z> <out folder>" >&2
  exit 2
fi
tag="$1"
out="$2"
# The version is the tag's three numbers; anything else is not a release.
if ! [[ "${tag}" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "release-build: ${tag} is not a release version (vX.Y.Z)" >&2
  exit 2
fi
version="${BASH_REMATCH[1]}"
run() { printf '+ %s\n' "$*"; "$@"; }

run rm -rf "${out}" || exit 1
run mkdir -p "${out}" || exit 1

# The program, for Apple Silicon. main.built stays empty: a release is not built from a clone, so
# the program never takes one for its own (D-040).
run env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-X main.version=${tag}" -o "${out}/engineroom" ./cmd/engineroom || exit 1

# The app, for Apple Silicon, in a bundle whose version is the release's.
swift_args=(-c release --arch arm64 --package-path macos)
run swift build "${swift_args[@]}" || exit 1
# Where SwiftPM puts a build for a named architecture differs between its versions; it says.
products="$(swift build "${swift_args[@]}" --show-bin-path)" || exit 1
app="${out}/engineroom.app"
run mkdir -p "${app}/Contents/MacOS" || exit 1
run cp "${products}/EngineRoomMenu" "${app}/Contents/MacOS/engineroom-menu" || exit 1
printf '+ write %s\n' "${app}/Contents/Info.plist"
sed "s/VERSION/${version}/g" macos/bundle/Info.plist > "${app}/Contents/Info.plist" || exit 1

if [ -n "${SIGN_IDENTITY:-}" ]; then
  # Notarization requires the hardened runtime and a secure timestamp. The app's entitlements let
  # it send Apple Events (it drives the terminal it joins sessions in, and quits itself to restart
  # after an upgrade), which the hardened runtime otherwise refuses.
  run codesign --force --options runtime --timestamp --sign "${SIGN_IDENTITY}" "${out}/engineroom" || exit 1
  run codesign --force --options runtime --timestamp --entitlements macos/bundle/engineroom.entitlements \
    --sign "${SIGN_IDENTITY}" "${app}" || exit 1
else
  echo "release-build: SIGN_IDENTITY is not set, so this is a dry run signed ad hoc; it is never published." >&2
  run codesign --force --sign - "${out}/engineroom" || exit 1
  run codesign --force --options runtime --entitlements macos/bundle/engineroom.entitlements --sign - "${app}" || exit 1
fi
run codesign --verify --strict "${out}/engineroom" || exit 1
run codesign --verify --strict --deep "${app}" || exit 1

# The two downloads. ditto keeps the app's signature and extended attributes, as zip does not.
run tar -czf "${out}/engineroom-${version}-macos.tar.gz" -C "${out}" engineroom || exit 1
run ditto -c -k --keepParent "${app}" "${out}/engineroom-app-${version}.zip" || exit 1

"${out}/engineroom" version
