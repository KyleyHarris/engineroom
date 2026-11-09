#!/usr/bin/env bash
#
# Records what Rex answers on this machine, for checking engineroom's Rex backend against the real
# thing (docs/rex.md, "To confirm on a machine with Rex"). Every command is printed before it runs,
# and its answer is saved, one file per command, in the folder named (default: ./rex-probe-<time>).
#
#   scripts/rex-probe.sh [folder]            read only: changes nothing
#   scripts/rex-probe.sh --session [folder]  also creates a session named engineroom-probe, reads it,
#                                            and kills it again (the one change it makes)

set -uo pipefail

session=0
if [ "${1:-}" = "--session" ]; then
  session=1
  shift
fi
out="${1:-rex-probe-$(date +%Y%m%d-%H%M%S)}"
mkdir -p "${out}" || exit 1

# Where rex is: inside a Rex terminal REX_BIN_DIR holds it; elsewhere PATH, then the app bundle.
rex="$(command -v rex || true)"
[ -n "${REX_BIN_DIR:-}" ] && [ -x "${REX_BIN_DIR}/rex" ] && rex="${REX_BIN_DIR}/rex"
if [ -z "${rex}" ]; then
  for app in /Applications/Rex.app "${HOME}/Applications/Rex.app"; do
    found="$(find "${app}" -type f -name rex -perm -u+x 2>/dev/null | head -1)"
    [ -n "${found}" ] && rex="${found}" && break
  done
fi
{
  echo "REX_BIN_DIR=${REX_BIN_DIR:-}"
  echo "rex=${rex:-not found}"
  for app in /Applications/Rex.app "${HOME}/Applications/Rex.app"; do
    [ -d "${app}" ] && echo "app=${app} bundle=$(mdls -raw -name kMDItemCFBundleIdentifier "${app}" 2>/dev/null)"
  done
} | tee "${out}/where.txt"
if [ -z "${rex}" ]; then
  echo "rex was not found; nothing else to probe." >&2
  exit 1
fi

# probe NAME ARGS...: runs rex ARGS, saving its output and exit status as NAME.
probe() {
  local name="$1"
  shift
  echo "+ rex $*"
  "${rex}" "$@" >"${out}/${name}.txt" 2>&1
  echo "exit $?" >>"${out}/${name}.txt"
}

probe version version
probe server-status server status --json
probe whoami whoami --json
probe hosts hosts --json
probe sessions ls --json
probe clients client ls --all --json
probe app-client client inspect --json
probe api-list api list
for m in session.list session.create session.new_window session.view session.destroy client.list client.perform; do
  probe "api-${m}" api describe "${m}"
done
probe block-types block types

if [ "${session}" -eq 1 ]; then
  probe session-new session new engineroom-probe --cwd "${HOME}" --window agent --json -- sh -c 'echo probe; sleep 600'
  probe window-new window new -s engineroom-probe git --focus=false -- sh -c 'echo git; sleep 600'
  probe session-inspect session inspect engineroom-probe --json
  probe session-view api call -s engineroom-probe session.view
  probe session-kill session kill engineroom-probe
fi

echo "Saved in ${out}/"
