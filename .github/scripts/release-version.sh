#!/usr/bin/env bash
# Validates a release tag and derives its version. The tagged commit must be on master.
#
#   release-version.sh <core|worker|app> <tag>
#
# core, worker: tag core-vX.Y.Z[-pre] / worker-vX.Y.Z[-pre]; version = X.Y.Z[-pre].
# app:          tag app-vX.Y.Z; version_name = X.Y.Z;
#               version_code = X*10000 + Y*100 + Z, with Y and Z below 100.
#               The annotated tag message is written to the changelog file
#               ($CHANGELOG_OUT, default $RUNNER_TEMP/changelog.txt).
#
# Prints key=value lines and appends them to $GITHUB_OUTPUT when it is set.
set -euo pipefail

kind=${1:-}
tag=${2:-}
emit() {
	printf '%s=%s\n' "$1" "$2"
	if [[ -n ${GITHUB_OUTPUT:-} ]]; then
		printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"
	fi
}
fail() {
	echo "::error::$1" >&2
	exit 1
}

case $kind in
core | worker)
	re="^$kind-v([0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?)\$"
	[[ $tag =~ $re ]] || fail "tag '$tag' does not match $kind-vX.Y.Z[-pre]"
	emit version "${BASH_REMATCH[1]}"
	;;
app)
	re='^app-v([0-9]+)\.([0-9]+)\.([0-9]+)$'
	[[ $tag =~ $re ]] || fail "tag '$tag' does not match app-vX.Y.Z"
	major=$((10#${BASH_REMATCH[1]}))
	minor=$((10#${BASH_REMATCH[2]}))
	patch=$((10#${BASH_REMATCH[3]}))
	((minor < 100 && patch < 100)) || fail "minor and patch must be below 100 for the versionCode scheme"
	emit version_name "$major.$minor.$patch"
	emit version_code "$((major * 10000 + minor * 100 + patch))"
	;;
*)
	fail "usage: release-version.sh <core|worker|app> <tag>"
	;;
esac

git rev-parse --verify -q origin/master >/dev/null ||
	fail "origin/master is missing: check out with fetch-depth: 0"
if ! git merge-base --is-ancestor HEAD origin/master; then
	fail "tag $tag is not on master: releases are cut from master only"
fi

if [[ $kind == app ]]; then
	if [[ $(git cat-file -t "refs/tags/$tag") != tag ]]; then
		fail "tag $tag must be annotated: its message is the changelog"
	fi
	out=${CHANGELOG_OUT:-${RUNNER_TEMP:-/tmp}/changelog.txt}
	git for-each-ref --format='%(contents)' "refs/tags/$tag" >"$out"
	if [[ ! -s $out ]] || [[ -z $(tr -d '[:space:]' <"$out") ]]; then
		fail "tag $tag has an empty message: write the changelog in the tag message"
	fi
	emit changelog_file "$out"
fi
