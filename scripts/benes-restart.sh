#!/usr/bin/env bash
#
# Recycle the benes loopback listener so the replacement process outlives this
# shell.
#
# The run moves through explicit lifecycle phases in this order:
#
#   stop      - ask the previous listener to exit; this is best effort
#   detach    - spawn the replacement outside this shell's session
#   ready     - poll the runtime state until the listener really answers
#   diagnose  - bounded log context when readiness never arrives
#
# "A process exists" is never accepted as "the listener is up": readiness needs
# the runtime-port file to decode to a port that answers GET /v1/models.
#
# Usage: scripts/benes-restart.sh
#
# Environment:
#   BENES_RESTART_LOG       startup log destination (default /tmp/benes-restart.log)
#   BENES_RESTART_TIMEOUT   readiness budget in seconds (default 30)
#   BENES_RESTART_INTERVAL  delay between readiness probes in seconds (default 1)
#
# Runtime state stays where the CLI owns it: $HOME/.benes/runtime-port.json and
# $HOME/.benes/benes.pid.
set -euo pipefail

# Fall back to the given default unless the value is a plain non-negative
# integer, so a malformed override cannot make the deadline arithmetic explode.
positive() {
  case "${1:-}" in
    '' | *[!0-9]*) printf '%s' "$2" ;;
    *) printf '%s' "$1" ;;
  esac
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
RESTART_LOG="${BENES_RESTART_LOG:-/tmp/benes-restart.log}"
# The runtime state directory belongs to the CLI; naming the directory and the
# two files it holds once keeps this helper from drifting away from it.
RUNTIME_STATE_DIR="$HOME/.benes"
RUNTIME_PORT_NAME="runtime-port.json"
LISTENER_PID_NAME="benes.pid"
PORT_FILE="$RUNTIME_STATE_DIR/$RUNTIME_PORT_NAME"
PID_FILE="$RUNTIME_STATE_DIR/$LISTENER_PID_NAME"
# The exact argv for each lifecycle action is written down once, so no phase has
# to assemble a shell string and the production Go CLI entry point stays visible
# in this helper rather than being implied.
LISTENER_START=(go run ./cmd/benes start)
LISTENER_STOP=(go run ./cmd/benes stop)
STOP_SETTLE_SECONDS=2
DIAGNOSTIC_LINES=15
READY_BUDGET="$(positive "${BENES_RESTART_TIMEOUT:-30}" 30)"
PROBE_GAP="$(positive "${BENES_RESTART_INTERVAL:-1}" 1)"

status() { printf '[benes-restart] %s\n' "$*"; }
warn() { printf '[benes-restart] %s\n' "$*" >&2; }

phase_stop() {
  status "stop"
  "${LISTENER_STOP[@]}" >/dev/null 2>&1 || true
  sleep "$STOP_SETTLE_SECONDS"
  rm -f "$PID_FILE"
}

phase_detach() {
  status "detach start; log $RESTART_LOG"
  setsid nohup "${LISTENER_START[@]}" >"$RESTART_LOG" 2>&1 </dev/null &
  disown || true
}

# The published port, or nothing at all while the state file is missing or does
# not yet carry a usable port. The payload is read and parsed explicitly instead
# of being handed to require(), so a malformed file can never be evaluated.
published_port() {
  [ -f "$PORT_FILE" ] || return 0
  node -e 'const raw = require("node:fs").readFileSync(process.argv[1], "utf8");
    process.stdout.write(String(JSON.parse(raw).port ?? ""));' "$PORT_FILE" 2>/dev/null || true
}

answers_health() {
  local budget="$2"
  timeout "${budget}s" curl -sf "http://127.0.0.1:$1/v1/models" >/dev/null 2>&1
}

phase_ready() {
  local deadline=$((SECONDS + READY_BUDGET))
  local port remaining
  while :; do
    port="$(published_port)"
    if [ -n "$port" ]; then
      remaining=$((deadline - SECONDS))
      [ "$remaining" -gt 0 ] || return 1
      if answers_health "$port" "$remaining"; then
        printf '%s' "$port"
        return 0
      fi
    fi
    [ "$SECONDS" -lt "$deadline" ] || return 1
    sleep "$PROBE_GAP"
  done
}

phase_diagnose() {
  warn "still down after ${READY_BUDGET}s"
  tail -n "$DIAGNOSTIC_LINES" "$RESTART_LOG" >&2 || true
}

main() {
  cd "$REPO_ROOT"
  phase_stop
  phase_detach
  local port
  if port="$(phase_ready)"; then
    status "ready port=$port pid=$(cat "$PID_FILE" 2>/dev/null || printf 'unknown')"
    return 0
  fi
  phase_diagnose
  return 1
}

main "$@"