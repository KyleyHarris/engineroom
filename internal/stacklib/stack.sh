# engineroom's stack helper, for a project's up and down scripts: start each part of a worktree's
# stack in the background on its slot's ports, and stop exactly what was started.
#
# An up, down or status script sources it first:
#
#   . "$(engineroom stack-lib)"
#
# then calls deps, start and stop, or running (below). Safe to run again by construction: a part whose port
# already answers for this worktree is not started, packages are installed only when missing or
# older than their lock file, and stopping something already stopped just says so.

set -uo pipefail
cd "${ENGINEROOM_WORKTREE:-$PWD}" || exit 1
WORKTREE="$PWD"
# The slot's values. engineroom gives them to every script it runs; run by hand, they come from the
# slot's environment file in the worktree: ENGINEROOM_SLOT_ENV when the project's settings name
# their own, else the default .engineroom-slot.env.
slot_env="${ENGINEROOM_SLOT_ENV:-.engineroom-slot.env}"
if [ -f "$slot_env" ]; then
  # shellcheck disable=SC1090
  . "./$slot_env"
fi
# The pids and logs live outside the worktree, keyed by its path, so nothing is ever committed
# and two worktrees never share them.
STATE_KEY="$(printf %s "$WORKTREE" | shasum | cut -c1-12)"
STATE="${TMPDIR:-/tmp}/engineroom-stack-$STATE_KEY"
mkdir -p "$STATE"

# listening PORT: true when something on this machine listens on the port.
listening() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }

# deps DIR: install DIR's npm packages when node_modules is missing or older than the lock file.
deps() {
  local dir="$1"
  if [ ! -d "$dir/node_modules" ] || [ "$dir/package-lock.json" -nt "$dir/node_modules" ]; then
    echo "+ (cd $dir && npm ci)"
    (cd "$dir" && npm ci --no-audit --no-fund) || return 1
    touch "$dir/node_modules"
  fi
}

# start NAME PORT URL DIR -- COMMAND...: runs COMMAND in DIR in the background, its output in
# NAME's log, and waits up to START_WAIT seconds (default 120) for URL to answer (any HTTP
# answer), or for PORT to listen when URL is "-". Starts nothing when PORT already listens.
start() {
  local name="$1" port="$2" url="$3" dir="$4"
  shift 5
  if listening "$port"; then
    # A port already in use is only "already running" when what holds it belongs to this
    # worktree. Anything else (the main clone's server on the same numbers, another worktree's)
    # is a failure: reporting success would leave this worktree served by someone else's code.
    local holder
    holder="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -1)"
    if [ -n "$holder" ] && belongs_here "$holder"; then
      echo "$name already runs here on $port (pid $holder); not started again."
      return 0
    fi
    echo "$name: port $port is held by pid ${holder:-?} ($(ps -o command= -p "${holder:-0}" 2>/dev/null | cut -c1-80)), which is not this worktree's, so $name was not started." >&2
    return 1
  fi
  echo "+ $name: (cd $dir && $*)  [port $port, log $STATE/$name.log]"
  # cd on its own, so only the command itself goes to the background: its pid is the one
  # recorded, and none of this script's output pipes stay open in it (engineroom waits for them).
  (
    cd "$dir" || exit 1
    BROWSER=none nohup "$@" >"$STATE/$name.log" 2>&1 </dev/null &
    echo $! >"$STATE/$name.pid"
  )
  # The port is kept beside the pid, so running can say whether the part still listens on it.
  echo "$port" >"$STATE/$name.port"
  local i
  for ((i = 0; i < ${START_WAIT:-120}; i++)); do
    if [ "$url" = "-" ]; then
      listening "$port" && { echo "$name is running on $port"; return 0; }
    elif curl -sk -o /dev/null --max-time 2 "$url"; then
      echo "$name is running: $url"
      return 0
    fi
    if ! kill -0 "$(cat "$STATE/$name.pid")" 2>/dev/null; then
      echo "$name stopped while starting; the end of its log:" >&2
      tail -20 "$STATE/$name.log" >&2
      return 1
    fi
    sleep 1
  done
  echo "$name did not answer within ${START_WAIT:-120}s; see $STATE/$name.log" >&2
  return 1
}

# belongs_here PID: true when the process works in this worktree (its folder is the worktree or
# inside it) or was started for it (its ENGINEROOM_WORKTREE is this worktree). A sibling
# worktree's path only shares a prefix, so the folder test compares whole path segments.
belongs_here() {
  local pid="$1" cwd env
  cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
  case "$cwd" in
    "$WORKTREE" | "$WORKTREE"/*) return 0 ;;
  esac
  env="$(ps eww -o command= -p "$pid" 2>/dev/null)"
  case " $env " in
    *" ENGINEROOM_WORKTREE=$WORKTREE "*) return 0 ;;
  esac
  return 1
}

# killtree PID: stops a process and everything it started, children first.
killtree() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null); do killtree "$child"; done
  kill -TERM "$1" 2>/dev/null
}

# stop NAME...: stops each named part that up started here, by its recorded pid. Nothing else.
stop() {
  local name pid
  for name in "$@"; do
    if [ ! -f "$STATE/$name.pid" ]; then
      echo "$name: not started by up here; nothing to stop."
      continue
    fi
    pid="$(cat "$STATE/$name.pid")"
    if kill -0 "$pid" 2>/dev/null; then
      echo "+ stop $name (pid $pid and what it started)"
      killtree "$pid"
    else
      echo "$name: already stopped."
    fi
    rm -f "$STATE/$name.pid" "$STATE/$name.port"
  done
}

# running NAME...: for a status script. True when every named part that up started here is still
# running: its recorded pid is alive and, where up recorded its port, something listens on that
# port. Says each part's state on one line, and changes nothing.
running() {
  local name pid port all=0
  for name in "$@"; do
    if [ ! -f "$STATE/$name.pid" ]; then
      echo "$name: not started by up here."
      all=1
      continue
    fi
    pid="$(cat "$STATE/$name.pid")"
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "$name: stopped (pid $pid is gone)."
      all=1
      continue
    fi
    port="$(cat "$STATE/$name.port" 2>/dev/null)"
    if [ -n "$port" ] && ! listening "$port"; then
      echo "$name: pid $pid runs, but nothing listens on $port."
      all=1
      continue
    fi
    echo "$name: running (pid $pid${port:+, on $port})."
  done
  return "$all"
}
