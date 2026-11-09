#!/usr/bin/env bash
# Build a Vikunja binary (API + embedded frontend) from the current checkout
# of a fork release branch (e.g. release/v2.7.1-fork = upstream tag + cherry-picks).
#
# Usage: scripts/build-fork.sh [--allow-dirty]
#   OUT_DIR  where the artifact goes (default: dist/fork)
#
# Output: $OUT_DIR/vikunja-<git describe>-<short sha>-linux-amd64 and a .sha256 next to it.
# The version embedded in the binary is `git describe --tags` (set by magefile.go), so a
# branch without extra commits reports exactly the upstream tag.
set -euo pipefail

ALLOW_DIRTY=0
[[ "${1:-}" == "--allow-dirty" ]] && ALLOW_DIRTY=1

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
OUT_DIR="${OUT_DIR:-$ROOT/dist/fork}"

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != release/* ]]; then
	echo "warning: building from '$branch', not a release/* branch" >&2
fi

if [[ $ALLOW_DIRTY -eq 0 && -n "$(git status --porcelain --untracked-files=no)" ]]; then
	echo "error: tracked files have local changes; commit them or pass --allow-dirty" >&2
	exit 1
fi

describe="$(git describe --tags --always --abbrev=10)"
sha="$(git rev-parse --short=10 HEAD)"
arch="$(go env GOOS 2>/dev/null || echo linux)-$(go env GOARCH 2>/dev/null || echo amd64)"
artifact="vikunja-${describe}-${sha}-${arch}"
[[ $ALLOW_DIRTY -eq 1 ]] && artifact="${artifact}-dirty"

# Toolchain versions are pinned in mise.toml at the tag (node, pnpm, go).
if command -v mise >/dev/null 2>&1; then
	mise install
	eval "$(mise env -s bash)"
fi
echo "toolchain: go $(go env GOVERSION), node $(node --version), pnpm $(pnpm --version)"

echo "==> frontend"
(
	cd frontend
	pnpm install --frozen-lockfile
	pnpm build
)

echo "==> api ($describe @ $sha)"
rm -f vikunja
mage build:build

mkdir -p "$OUT_DIR"
cp vikunja "$OUT_DIR/$artifact"
(cd "$OUT_DIR" && sha256sum "$artifact" >"$artifact.sha256")

echo "==> done"
"$OUT_DIR/$artifact" version
ls -l "$OUT_DIR/$artifact"
echo "$OUT_DIR/$artifact"
