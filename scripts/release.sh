#!/usr/bin/env bash
# Cuts a release: works out the next version, checks the tree, builds and
# vets, tags the commit, pushes main and the tag, then asks the Go module
# proxy for the new version. A local main ahead of origin is pushed on the
# way; one behind or diverged from origin stops the release.
# It then asks the proxy for the new version so `deck update` and `go install ...@latest`
# see it at once instead of after the proxy's cache window.
#
#   scripts/release.sh            # bump the patch:  v0.3.9 → v0.3.10
#   scripts/release.sh patch      # the same
#   scripts/release.sh minor      # v0.3.9 → v0.4.0
#   scripts/release.sh major      # v0.3.9 → v1.0.0
#   scripts/release.sh v0.5.0     # exactly this version
#   scripts/release.sh minor --dry-run   # print the version it would tag, change nothing
#
# The first release of a repository with no tag starts from v0.0.0, so a
# bare run tags v0.0.1 and `minor` tags v0.1.0.
#
# There is no version to edit in the source: `deck version` reads the module
# version Go stamps into a binary installed from a tag.
set -euo pipefail
module=github.com/unixsuperhero/tuios-deck
bump=patch; tag=""; dry=0
for arg in "$@"; do
	case "$arg" in
		patch|minor|major) bump=$arg ;;
		v[0-9]*.[0-9]*.[0-9]*) tag=$arg ;;
		--dry-run|-n) dry=1 ;;
		-h|--help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) echo "usage: scripts/release.sh [patch|minor|major|vX.Y.Z] [--dry-run]" >&2; exit 2 ;;
	esac
done
cd "$(dirname "$0")/.."

# The newest tag by semantic version, not by date.
current=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' | sort -t. -k1.2,1n -k2,2n -k3,3n | tail -1)
current=${current:-v0.0.0}
if [ -z "$tag" ]; then
	IFS=. read -r major minor patch <<< "${current#v}"
	case "$bump" in
		major) tag="v$((major + 1)).0.0" ;;
		minor) tag="v$major.$((minor + 1)).0" ;;
		patch) tag="v$major.$minor.$((patch + 1))" ;;
	esac
fi
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a version: $tag" >&2; exit 2; }
echo "current: $current"
echo "next:    $tag"
if [ "$dry" = 1 ]; then
	echo "dry run: nothing tagged or pushed"
	exit 0
fi

[ -z "$(git status --porcelain)" ] || { echo "the tree is not clean; commit first" >&2; exit 1; }
[ "$(git branch --show-current)" = main ] || { echo "release from main" >&2; exit 1; }
git rev-parse -q --verify "refs/tags/$tag" >/dev/null && { echo "$tag exists" >&2; exit 1; }
# A local main ahead of origin is pushed first; one behind or diverged from
# it stops the release, since the tag would not be built on what is published.
git fetch -q origin main
if [ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]; then
	git merge-base --is-ancestor origin/main HEAD || { echo "main has diverged from origin/main (or is behind it); rebase or pull first" >&2; exit 1; }
	git push -q origin main || { echo "pushing main to origin was rejected; rebase on origin/main first" >&2; exit 1; }
	echo "pushed main to origin ($(git rev-list --count origin/main@{1}..HEAD 2>/dev/null || echo some) commits)"
fi
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
