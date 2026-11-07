#!/usr/bin/env bash
# Prints the newest yt-dlp release tag, for example 2026.10.26.
# Uses GH_TOKEN when it is set, to stay clear of the anonymous API rate limit.
set -euo pipefail

auth=()
if [[ -n ${GH_TOKEN:-} ]]; then
	auth=(-H "Authorization: Bearer $GH_TOKEN")
fi
tag=$(curl -fsSL --retry 3 "${auth[@]}" -H "Accept: application/vnd.github+json" \
	https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest | jq -r '.tag_name')
if [[ ! $tag =~ ^[0-9]{4}\.[0-9]{2}\.[0-9]{2}(\.[0-9]+)?$ ]]; then
	echo "::error::unexpected yt-dlp release tag: $tag" >&2
	exit 1
fi
printf '%s\n' "$tag"
