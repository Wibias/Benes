#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ci-local-linux: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

expected_sha="${BENES_CI_EXPECTED_SHA:-}"
source_git_dir="${BENES_CI_SOURCE_GIT_DIR:-}"

# Windows Git worktrees store a .git pointer that may contain a Windows-native
# absolute path. Linux Git cannot safely use that metadata through /mnt/<drive>.
# When the Windows orchestrator provides the common Git directory, materialize
# exactly the requested commit into a WSL-native temporary repository first.
if [[ "${BENES_CI_IN_WORKTREE:-0}" != "1" && -n "$source_git_dir" ]]; then
  require_command git
  [[ -n "$expected_sha" ]] || fail "BENES_CI_EXPECTED_SHA is required with BENES_CI_SOURCE_GIT_DIR"
  [[ -d "$source_git_dir" ]] || fail "source git directory is unavailable: $source_git_dir"

  worktree_root="$(mktemp -d "${TMPDIR:-/tmp}/benes-ci-linux.XXXXXX")"
  worktree="$worktree_root/repo"
  cleanup_bootstrap() {
    rm -rf -- "$worktree_root"
  }
  trap cleanup_bootstrap EXIT INT TERM

  git init -q "$worktree"
  # Fetching a raw SHA into an empty repo can create a shallow destination.
  # Windows Git directories may also advertise shallow/partial roots over /mnt.
  # Bind the commit to a local ref and check that SHA out; do not use FETCH_HEAD
  # (a failed fetch leaves Git treating FETCH_HEAD as a path argument).
  git -C "$worktree" fetch --no-tags --update-shallow "$source_git_dir" "+${expected_sha}:refs/heads/benes-ci"
  git -C "$worktree" checkout --detach "$expected_sha"
  actual_sha="$(git -C "$worktree" rev-parse HEAD)"
  [[ "$actual_sha" == "$expected_sha" ]] || fail "WSL checkout HEAD $actual_sha does not match expected commit $expected_sha"

  (
    cd "$worktree"
    BENES_CI_IN_WORKTREE=1 \
      BENES_CI_EXPECTED_SHA="$expected_sha" \
      BENES_CI_SCOPE="${BENES_CI_SCOPE:-}" \
      bash scripts/ci-local-linux.sh
  )
  exit $?
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "run this script from a Benes checkout"
head_sha="$(git -C "$repo_root" rev-parse HEAD)"
expected_sha="${expected_sha:-$head_sha}"

if [[ "$head_sha" != "$expected_sha" ]]; then
  fail "checkout HEAD $head_sha does not match expected commit $expected_sha"
fi

if [[ "${BENES_CI_IN_WORKTREE:-0}" != "1" ]]; then
  require_command git
  worktree_root="$(mktemp -d "${TMPDIR:-/tmp}/benes-ci-linux.XXXXXX")"
  worktree="$worktree_root/repo"
  cleanup_outer() {
    git -C "$repo_root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
    rm -rf -- "$worktree_root"
  }
  trap cleanup_outer EXIT INT TERM
  git -C "$repo_root" worktree add --detach "$worktree" "$expected_sha"
  BENES_CI_IN_WORKTREE=1 BENES_CI_EXPECTED_SHA="$expected_sha" BENES_CI_SCOPE="${BENES_CI_SCOPE:-}" bash "$worktree/scripts/ci-local-linux.sh"
  exit $?
fi

cd "$repo_root"
[[ "$(uname -s)" == "Linux" ]] || fail "Linux evidence requires a Linux host (WSL2 is supported)"

for command in git node npm go; do
  require_command "$command"
done

node_major="$(node -p 'Number(process.versions.node.split(".")[0])')"
[[ "$node_major" == "24" ]] || fail "Node 24 is required to match .github/workflows/ci.yml; found $(node --version)"
expected_go="$(awk '$1 == "toolchain" { print $2; exit }' go.mod)"
actual_go="$(go env GOVERSION)"
[[ -z "$expected_go" || "$actual_go" == "$expected_go" ]] || fail "Go toolchain $expected_go is required; found $actual_go"

# shellcheck source=ci-scope.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ci-scope.sh"

printf '\n== Benes local CI: Linux @ %s ==\n' "$expected_sha"
node --version
go version

if ci_scope_enabled automation; then
  printf '\n-- Workflow contract --\n'
  node .github/scripts/run-automation-tests.cjs
fi

if ci_scope_enabled privacy; then
  printf '\n-- Privacy --\n'
  node --experimental-strip-types scripts/privacy-scan.ts
fi

if ci_scope_enabled gui; then
  printf '\n-- GUI lint/build --\n'
  (
    cd gui
    npm ci
    npm run lint
    npm run build
  )
fi

if ci_scope_enabled go; then
  printf '\n-- Go core --\n'
  go build -o "${TMPDIR:-/tmp}/benes-ci-linux-$$_benes" ./cmd/benes
  go test ./...
  go vet ./...
  go test -race ./...
  rm -f -- "${TMPDIR:-/tmp}/benes-ci-linux-$$_benes"

  printf '\n-- CLI help smoke --\n'
  go run ./cmd/benes help
fi

if ci_scope_enabled keyring; then
  printf '\n-- Linux keyring smoke --\n'
  if ! command -v dbus-run-session >/dev/null 2>&1 || ! command -v gnome-keyring-daemon >/dev/null 2>&1; then
    require_command sudo
    sudo apt-get update
    sudo apt-get install --yes --no-install-recommends dbus-x11 gnome-keyring
  fi
  npm install
  (
    keyring_home="$(mktemp -d)"
    runtime_dir="$(mktemp -d)"
    trap 'rm -rf -- "$keyring_home" "$runtime_dir"' EXIT
    chmod 700 "$keyring_home" "$runtime_dir"
    HOME="$keyring_home" XDG_RUNTIME_DIR="$runtime_dir" dbus-run-session -- bash -euo pipefail -c '
      od -An -N32 -tx1 /dev/urandom |
        tr -d "[:space:]" |
        gnome-keyring-daemon --unlock --components=secrets >/dev/null
      node --experimental-strip-types scripts/keyring-smoke.ts
    '
  )
fi

if ci_scope_enabled packaging; then
  printf '\n-- npm global packaging smoke --\n'
  git clean -fdx
  npm install --omit=dev
  npm run build:gui
  npm pack --json > pack.json
  node .github/scripts/npm-pack-json.cjs verify pack.json
  tarball="$(node .github/scripts/npm-pack-json.cjs filename pack.json)"
  [[ -n "$tarball" && -f "$tarball" ]] || fail "npm pack did not report a usable tarball"
  (
    prefix="$(mktemp -d)"
    trap 'rm -rf -- "$prefix"' EXIT
    npm install -g --prefix "$prefix" "./$tarball"
    PATH="$prefix/bin:$PATH" benes help
  )
fi

printf '\nPASS: Linux local CI @ %s\n' "$expected_sha"
