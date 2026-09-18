#!/usr/bin/env bash
#
# benes installer for POSIX shells.
#
# The run is a fixed pipeline of stages, executed in order by main(). A stage
# returns only once its own precondition holds; a stage that cannot finish ends
# the whole run with a non-zero status. Nothing is reported as installed on the
# strength of an earlier stage alone.
#
# Stage order:
#   announce  - state what is about to happen
#   preflight - every external binary this installer drives must exist
#   version   - the Node.js floor is a real version test, not just presence
#   package   - write the published package into the npm global prefix
#   locate    - the launcher must be reachable through PATH
#   health    - the launcher must answer its own help entry point
#   handoff   - tell the operator the one command that finishes setup
#
# The requirement table below is the single source of truth for the toolchain;
# the preflight loop and its remedies are derived from it so they cannot drift.
set -euo pipefail

readonly PACKAGE="@wibias/benes"
readonly CLI="benes"
readonly NODE_FLOOR=18
readonly NODE_ORIGIN="https://nodejs.org/"
readonly GO_ORIGIN="https://go.dev/dl/"

# "<binary>|<remediation shown when it is absent>"
readonly -a TOOLCHAIN=(
  "node|Node.js ${NODE_FLOOR}+ is required. Install it from ${NODE_ORIGIN} and rerun."
  "npm|npm is required to install the published ${PACKAGE} package."
  "go|Go 1.27.0 is required. Install it from ${GO_ORIGIN} and rerun."
)

announce() { printf '%s\n' "$*"; }
report() { printf '%s\n' "$*" >&2; }
abort() { report "$1"; exit "${2:-1}"; }
located() { command -v "$1" >/dev/null 2>&1; }

stage_preflight() {
  local row binary remedy
  for row in "${TOOLCHAIN[@]}"; do
    binary="${row%%|*}"
    remedy="${row#*|}"
    located "$binary" || abort "$remedy"
  done
}

stage_version() {
  local observed
  observed="$(node -p 'Number(process.versions.node.split(".")[0])')"
  if (( observed < NODE_FLOOR )); then
    abort "Node.js ${NODE_FLOOR}+ is required. Current version: $(node --version)"
  fi
}

stage_package() {
  announce "Using Node $(node --version)"
  announce "Using $(go version)"
  npm install -g "$PACKAGE"
}

stage_locate() {
  located "$CLI" || abort \
    "${PACKAGE} is installed but ${CLI} is not on PATH. Add the npm global bin directory, then open a new shell: $(npm prefix -g)/bin"
}

stage_health() {
  "$CLI" help >/dev/null || abort \
    "${CLI} is on PATH but '${CLI} help' failed. Check the global npm install."
}

stage_handoff() {
  announce ""
  announce "${PACKAGE} is installed. Next: ${CLI} init"
}

main() {
  announce "Installing ${PACKAGE}..."
  stage_preflight
  stage_version
  stage_package
  stage_locate
  stage_health
  stage_handoff
}

main "$@"
