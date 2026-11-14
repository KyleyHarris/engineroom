#!/usr/bin/env bash
#
# Notarizes a release built by scripts/release-build.sh with Apple, and staples the ticket to the
# app (docs/decisions.md D-040), so macOS opens both downloads on any Mac without a warning.
#
#   NOTARY_KEY_FILE=<.p8> NOTARY_KEY_ID=<id> NOTARY_ISSUER_ID=<uuid> scripts/release-notarize.sh <vX.Y.Z> <out folder>
#
# The credentials are an App Store Connect API key. The program and the app are each submitted and
# waited for; anything but Accepted stops the release, with Apple's log printed. A bare program
# cannot carry a stapled ticket, so macOS finds its notarization online; the app carries its own,
# and its download is packed again once stapled.

set -uo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: scripts/release-notarize.sh <vX.Y.Z> <out folder>" >&2
  exit 2
fi
if ! [[ "$1" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "release-notarize: $1 is not a release version (vX.Y.Z)" >&2
  exit 2
fi
version="${BASH_REMATCH[1]}"
out="$2"
for v in NOTARY_KEY_FILE NOTARY_KEY_ID NOTARY_ISSUER_ID; do
  if [ -z "${!v:-}" ]; then
    echo "release-notarize: ${v} is not set" >&2
    exit 2
  fi
done
run() { printf '+ %s\n' "$*"; "$@"; }
auth=(--key "${NOTARY_KEY_FILE}" --key-id "${NOTARY_KEY_ID}" --issuer "${NOTARY_ISSUER_ID}")

# submit sends one zip to Apple and waits; it fails unless Apple answers Accepted.
submit() {
  local zip="$1" result id status
  printf '+ xcrun notarytool submit %s --wait\n' "${zip}"
  result="$(xcrun notarytool submit "${zip}" "${auth[@]}" --wait --output-format json)" || {
    echo "release-notarize: submitting ${zip} failed: ${result}" >&2
    return 1
  }
  id="$(printf '%s' "${result}" | plutil -extract id raw -o - - 2>/dev/null)"
  status="$(printf '%s' "${result}" | plutil -extract status raw -o - - 2>/dev/null)"
  echo "notarization ${id}: ${status}"
  if [ "${status}" != "Accepted" ]; then
    xcrun notarytool log "${id}" "${auth[@]}" >&2
    return 1
  fi
}

# The program: zipped only to be sent, since Apple takes a zip, not a bare file.
run ditto -c -k "${out}/engineroom" "${out}/notarize-engineroom.zip" || exit 1
submit "${out}/notarize-engineroom.zip" || exit 1
run rm -f "${out}/notarize-engineroom.zip" || exit 1

# The app: sent as its download, then stapled, checked as Gatekeeper checks it, and packed again.
submit "${out}/engineroom-app-${version}.zip" || exit 1
run xcrun stapler staple "${out}/engineroom.app" || exit 1
run xcrun stapler validate "${out}/engineroom.app" || exit 1
run spctl --assess --type execute --verbose=2 "${out}/engineroom.app" || exit 1
run rm -f "${out}/engineroom-app-${version}.zip" || exit 1
run ditto -c -k --keepParent "${out}/engineroom.app" "${out}/engineroom-app-${version}.zip" || exit 1
