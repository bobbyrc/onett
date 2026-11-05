#!/usr/bin/env bash
# Updates the long-lived dogfood instance to origin/main (or, when ONETT_UPDATE_REF is set, to
# that commit: the daemon's self_update loop sets it to the release that was healthy on staging).
# It builds the commit in a temporary worktree, swaps the binary ($DOGFOOD/onett; the old one is
# kept as onett.prev), restarts the daemon and checks that it answers with the new version. A
# restart that fails health is undone with the previous binary. It does not pause the factory or
# wait for runs: agent runs reattach to their shims (or resume their session) in the new daemon.
# The daemon's self_update loop does not start this script while a release is in flight.
#
# Needs DOGFOOD (see README.md) and go/node on PATH (`mise exec -- examples/dogfood/update.sh`).
# The config is ONETT_CONFIG, else $DOGFOOD/onett.yaml, else the example in the clone. When
# the launchd service is installed (`onett service install`), launchd restarts the daemon;
# otherwise it is started with $DOGFOOD/run-daemon.sh when that file exists (a machine's own
# launcher), or with `onett serve`. Never run this from an agent that onett runs: restarting
# the daemon ends every run it supervises. The self_update loop is not an agent run: it starts
# this script detached, so the script outlives the daemon it restarts.
#
# ONETT_UPDATE_DRY_RUN=1 builds the commit and reports what it would do, then stops before it swaps
# the binary or restarts anything. That is the one mode allowed inside an onett agent run, so an
# agent can test the script without touching the running daemon.
#
# It leaves $ONETT_DATA_DIR/self-update.result (default $DOGFOOD/data) for the new daemon to
# report: old, new, ref, status ok or rolled_back and, for a rollback with a known cause, reason.
set -euo pipefail

DOGFOOD=${DOGFOOD:?set DOGFOOD to the dogfood directory}
export DOGFOOD
dry_run=${ONETT_UPDATE_DRY_RUN:-}
if [ -n "${ONETT_OUTBOX:-}" ] && [ -z "$dry_run" ]; then
  echo "update.sh: refusing to run inside an onett agent run (ONETT_UPDATE_DRY_RUN=1 is allowed)" >&2
  exit 1
fi
if [ -z "${ONETT_CONFIG:-}" ]; then
  ONETT_CONFIG=$DOGFOOD/onett.yaml
  [ -f "$ONETT_CONFIG" ] || ONETT_CONFIG=$DOGFOOD/clone/examples/dogfood/onett.yaml
fi
export ONETT_CONFIG

bin=$DOGFOOD/onett
repo=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
addr=$(sed -n 's/^  addr: *//p' "$ONETT_CONFIG" | head -n 1)
port=${addr##*:}
[ -n "$port" ] || { echo "update.sh: no server.addr in $ONETT_CONFIG" >&2; exit 1; }

tmp=$(mktemp -d)
cleanup() {
  git -C "$repo" worktree remove --force "$tmp/src" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

# Build first, so a failed build leaves the running instance untouched.
ref=${ONETT_UPDATE_REF:-}
if [ -n "$ref" ]; then
  git -C "$repo" fetch -q origin dogfood main
  git -C "$repo" worktree add -q --detach "$tmp/src" "$ref"
else
  git -C "$repo" fetch -q origin main
  git -C "$repo" worktree add -q --detach "$tmp/src" origin/main
  ref=$(git -C "$tmp/src" rev-parse HEAD)
fi
OUT=$tmp/onett "$tmp/src/scripts/build.sh"
new=$("$tmp/onett" version | awk '{print $2}')
running=$(curl -sf "http://$addr/api/v1/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p' || true)
if [ "$new" = "$running" ]; then
  echo "update.sh: already running $new"
  exit 0
fi

if [ -n "$dry_run" ]; then
  echo "update.sh: dry run: built $new from ${ref:0:7} (running: ${running:-unreachable}); nothing swapped or restarted"
  exit 0
fi

# Under launchd (`onett service install`) the service restarts the binary and config its plist
# names, so they must be this script's. A service that is installed but not running cannot be
# restarted with launchctl kickstart, so stop rather than guess.
service=no
log=$DOGFOOD/serve.log
svc=$("$bin" service status 2>/dev/null || true)
case "$(head -n 1 <<<"$svc")" in
  running*)
    service=yes
    log=$(sed -n 's/^log: //p' <<<"$svc")
    plist=$HOME/Library/LaunchAgents/io.onett.serve.plist
    if ! grep -qF "<string>$bin</string>" "$plist" || ! grep -qF "<string>$ONETT_CONFIG</string>" "$plist"; then
      echo "update.sh: the launchd service does not run $bin with $ONETT_CONFIG (see $plist);" >&2
      echo "  reinstall it: $bin service uninstall && $bin service install --config $ONETT_CONFIG" >&2
      exit 1
    fi
    ;;
  installed*)
    echo "update.sh: the launchd service is installed but not running; start it or reinstall it first" >&2
    exit 1
    ;;
esac

stop_daemon() {
  local pid
  pid=$(lsof -tiTCP:"$port" -sTCP:LISTEN || true)
  [ -n "$pid" ] || return 0
  case "$(ps -o command= -p "$pid")" in
    *" serve"*) ;;
    *) echo "update.sh: port $port is held by pid $pid, which is not onett serve" >&2; exit 1 ;;
  esac
  kill "$pid"
  for _ in $(seq 1 60); do kill -0 "$pid" 2>/dev/null || return 0; sleep 0.5; done
  echo "update.sh: pid $pid did not exit" >&2
  exit 1
}

# restart_daemon restarts the daemon on the binary now at $bin and waits until it answers with
# version $1. Under launchd, killing the daemon would only make launchd start it again, so the
# service is restarted through launchd (`onett service install` on an installed service).
restart_daemon() {
  if [ "$service" = yes ]; then
    "$bin" service install >/dev/null || return 1
  else
    stop_daemon
    if [ -x "$DOGFOOD/run-daemon.sh" ]; then
      nohup "$DOGFOOD/run-daemon.sh" >>"$log" 2>&1 </dev/null &
    else
      nohup "$bin" serve --config "$ONETT_CONFIG" >>"$log" 2>&1 </dev/null &
    fi
    disown
  fi
  for _ in $(seq 1 60); do
    [ "$(curl -sf "http://$addr/api/v1/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')" = "$1" ] && return 0
    sleep 0.5
  done
  return 1
}

# write_result records the outcome for the daemon that starts next, which reports it once. It is
# written before the restart because the new daemon reads it as it starts.
result=${ONETT_DATA_DIR:-$DOGFOOD/data}/self-update.result
write_result() {
  mkdir -p "$(dirname "$result")"
  printf 'old=%s\nnew=%s\nref=%s\nstatus=%s\n' "$old" "$new" "$ref" "$1" >"$result"
  [ -z "${2:-}" ] || printf 'reason=%s\n' "$2" >>"$result"
}

# preflight_state prints whether the running daemon's last tool-directory check passed: pass,
# fail, or none when it has not run one yet. `onett doctor` reports the daemon's own result,
# which can differ from what a shell can read (a launchd service may lack Full Disk Access).
preflight_state() {
  local out
  out=$("$bin" doctor 2>&1 || true)
  if grep -qE '^FAIL +daemon preflight' <<<"$out"; then
    echo fail
  elif grep -qE '^ok +daemon preflight' <<<"$out"; then
    echo pass
  else
    echo none
  fi
}

# rollback puts the previous binary back and restarts the daemon on it. $1 is the reason for the
# result file, empty for a daemon that did not come up.
rollback() {
  write_result rolled_back "${1:-}"
  cp "$bin.prev" "$bin.new" && mv "$bin.new" "$bin"
  restart_daemon "$old" || echo "update.sh: $old did not come up either" >&2
}

old=$("$bin" version | awk '{print $2}')
# A new build that loses macOS privacy access (an ad-hoc signed binary is a new app to macOS) would
# leave the factory holding every run, so remember whether the running daemon can read its tools.
preflight_before=$(preflight_state)
cp "$bin" "$bin.prev"
cp "$tmp/onett" "$bin.new" && mv "$bin.new" "$bin"
write_result ok
if ! restart_daemon "$new"; then
  echo "update.sh: $new did not come up healthy; restoring $old (see $log)" >&2
  rollback
  exit 1
fi
# The new daemon runs its first preflight when the scheduler next passes; give it 30 seconds.
if [ "$preflight_before" = pass ]; then
  state=none
  for _ in $(seq 1 15); do
    state=$(preflight_state)
    [ "$state" = none ] || break
    sleep 2
  done
  if [ "$state" = fail ]; then
    echo "update.sh: $new lost macOS privacy access; restoring $old" >&2
    rollback "new build lost macOS privacy access; set ONETT_CODESIGN_IDENTITY, see README"
    exit 1
  fi
fi
echo "update.sh: running $new (was $old); previous binary kept as $bin.prev"
