#!/usr/bin/env bash
#
# Writes the Homebrew formula and cask for a release into a checkout of the tap,
# KyleyHarris/homebrew-engineroom (docs/decisions.md D-040), from the downloads in the out folder.
#
#   scripts/release-tap.sh <vX.Y.Z> <out folder> <tap checkout>
#
# The formula installs the program and brings Zellij, which every session runs in; the cask
# installs the app and brings the formula. Both are for Apple Silicon only (D-041), and say so, so
# Homebrew refuses them on an Intel Mac rather than installing a program that cannot run. Each names its download on the GitHub release and its
# SHA-256, so Homebrew refuses a download that is not the one built. It writes the two files and
# nothing else; committing and pushing them is the caller's.

set -uo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: scripts/release-tap.sh <vX.Y.Z> <out folder> <tap checkout>" >&2
  exit 2
fi
if ! [[ "$1" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "release-tap: $1 is not a release version (vX.Y.Z)" >&2
  exit 2
fi
tag="$1"
version="${BASH_REMATCH[1]}"
out="$2"
tap="$3"
cli="${out}/engineroom-${version}-macos.tar.gz"
app="${out}/engineroom-app-${version}.zip"
for f in "${cli}" "${app}"; do
  if [ ! -f "${f}" ]; then
    echo "release-tap: ${f} is missing; build the release first" >&2
    exit 1
  fi
done
cli_sha="$(shasum -a 256 "${cli}" | awk '{print $1}')"
app_sha="$(shasum -a 256 "${app}" | awk '{print $1}')"
releases="https://github.com/KyleyHarris/engineroom/releases/download"

mkdir -p "${tap}/Formula" "${tap}/Casks" || exit 1
printf '+ write %s\n' "${tap}/Formula/engineroom.rb"
cat > "${tap}/Formula/engineroom.rb" <<RUBY || exit 1
# Written by engineroom's release (scripts/release-tap.sh); edit that, not this file.
class Engineroom < Formula
  desc "Run Claude Code sessions on one Mac and work on them from any other"
  homepage "https://github.com/KyleyHarris/engineroom"
  url "${releases}/${tag}/engineroom-${version}-macos.tar.gz"
  version "${version}"
  sha256 "${cli_sha}"
  license "MIT"

  depends_on arch: :arm64
  depends_on :macos
  depends_on "zellij"

  def install
    bin.install "engineroom"
  end

  test do
    assert_match "engineroom ${tag}", shell_output("#{bin}/engineroom version")
  end
end
RUBY
printf '+ write %s\n' "${tap}/Casks/engineroom.rb"
cat > "${tap}/Casks/engineroom.rb" <<RUBY || exit 1
# Written by engineroom's release (scripts/release-tap.sh); edit that, not this file.
cask "engineroom" do
  version "${version}"
  sha256 "${app_sha}"

  url "${releases}/v#{version}/engineroom-app-#{version}.zip"
  name "engineroom"
  desc "Menu bar app for engineroom's Claude Code sessions"
  homepage "https://github.com/KyleyHarris/engineroom"

  depends_on arch: :arm64
  depends_on formula: "kyleyharris/engineroom/engineroom"
  depends_on macos: :ventura

  app "engineroom.app"
end
RUBY
