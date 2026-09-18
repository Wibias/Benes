#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'ci-local-macos: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || fail "run this script from a Benes checkout"
head_sha="$(git -C "$repo_root" rev-parse HEAD)"
expected_sha="${BENES_CI_EXPECTED_SHA:-$head_sha}"

if [[ "$head_sha" != "$expected_sha" ]]; then
  fail "checkout HEAD $head_sha does not match expected commit $expected_sha"
fi

if [[ "${BENES_CI_IN_WORKTREE:-0}" != "1" ]]; then
  require_command git
  worktree_root="$(mktemp -d "${TMPDIR:-/tmp}/benes-ci-macos.XXXXXX")"
  worktree="$worktree_root/repo"
  cleanup_outer() {
    git -C "$repo_root" worktree remove --force "$worktree" >/dev/null 2>&1 || true
    rm -rf -- "$worktree_root"
  }
  trap cleanup_outer EXIT INT TERM
  git -C "$repo_root" worktree add --detach "$worktree" "$expected_sha"
  BENES_CI_IN_WORKTREE=1 BENES_CI_EXPECTED_SHA="$expected_sha" BENES_CI_SCOPE="${BENES_CI_SCOPE:-}" bash "$worktree/scripts/ci-local-macos.sh"
  exit $?
fi

cd "$repo_root"
[[ "$(uname -s)" == "Darwin" ]] || fail "full macOS evidence requires a real macOS host"

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

printf '\n== Benes local CI: macOS @ %s ==\n' "$expected_sha"
node --version
go version

if ci_scope_enabled go; then
  printf '\n-- Go core --\n'
  go build -o "${TMPDIR:-/tmp}/benes-ci-macos-$$_benes" ./cmd/benes
  go test ./...
  go vet ./...
  rm -f -- "${TMPDIR:-/tmp}/benes-ci-macos-$$_benes"
fi

if ci_scope_enabled keyring; then
  printf '\n-- macOS keyring smoke --\n'
  npm install
  node --experimental-strip-types scripts/keyring-smoke.ts
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

printf '\nPASS: macOS local CI @ %s\n' "$expected_sha"
