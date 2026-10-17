#!/usr/bin/env bash
# Cuts a release: checks the tree, runs the build and vet, tags the commit,
# pushes main and the tag, then asks the Go module proxy for the new version
# so `deck update` and `go install ...@latest` see it at once instead of
# after the proxy's cache window.
#
#   scripts/release.sh v0.2.0
#
# There is no version to edit in the source: `deck version` reads the module
# version Go stamps into a binary installed from a tag.
set -euo pipefail
tag=${1:-}
module=github.com/unixsuperhero/tuios-deck
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: scripts/release.sh vX.Y.Z" >&2; exit 2; }
cd "$(dirname "$0")/.."
[ -z "$(git status --porcelain)" ] || { echo "the tree is not clean; commit first" >&2; exit 1; }
[ "$(git branch --show-current)" = main ] || { echo "release from main" >&2; exit 1; }
git rev-parse -q --verify "refs/tags/$tag" >/dev/null && { echo "$tag exists" >&2; exit 1; }
git fetch -q origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || { echo "main is not in sync with origin/main; push or pull first" >&2; exit 1; }
GOWORK=off go build ./... && GOWORK=off go vet ./...
git tag -a "$tag" -m "deck $tag"
git push -q origin main "$tag"
echo "tagged and pushed $tag"
# Prime the proxy. The query runs outside any module so it is about the
# published one; a failure here only means the proxy is slow, not that the
# release failed.
( cd "${TMPDIR:-/tmp}" && GOWORK=off GOFLAGS= GOPROXY=https://proxy.golang.org go list -m "$module@$tag" ) \
  && echo "proxy.golang.org knows $tag" || echo "proxy not primed yet; it will fetch $tag on the first request"
echo "users update with: deck update   (or go install $module/cmd/deck@$tag)"
