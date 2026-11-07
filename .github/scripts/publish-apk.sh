#!/usr/bin/env bash
# Publishes a signed APK on the core server: uploads it to /opt/app/releases and
# registers it with `core release add` (docs/10-deploy.md, release-android).
#
#   publish-apk.sh <apk> <version_code> <version_name> <changelog file>
#
# Env: DEPLOY_HOST, DEPLOY_SSH_KEY, DEPLOY_KNOWN_HOSTS (see lib-ssh.sh).
# Refuses versions the server already has: the core rejects duplicates, and
# Android never installs a lower versionCode over a higher one.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib-ssh.sh
. "$here/lib-ssh.sh"

apk=${1:?usage: publish-apk.sh <apk> <version_code> <version_name> <changelog>}
code=${2:?missing version_code}
name=${3:?missing version_name}
changelog=${4:?missing changelog file}
if [[ ! $code =~ ^[1-9][0-9]{0,9}$ || ! $name =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "::error::invalid version: code=$code name=$name" >&2
	exit 2
fi
[[ -s $apk && -s $changelog ]] || {
	echo "::error::APK and changelog must exist and be non-empty" >&2
	exit 2
}

ssh_setup
trap ssh_cleanup EXIT

remote_name="quoker-$name.apk"

echo "::group::check the server"
running=$(rssh "cd /opt/app && docker compose ps --status running -q core") || {
	echo "::error::cannot query the core container" >&2
	exit 1
}
[[ -n $running ]] || {
	echo "::error::core is not running; releases are registered through it" >&2
	exit 1
}
published=$(rssh "cd /opt/app && docker compose exec -T core core release list")
latest=$(printf '%s\n' "$published" | cut -f1 | sort -n | tail -n 1)
if [[ -n $latest ]] && ((code <= latest)); then
	echo "::error::version_code $code must be greater than the latest published $latest" >&2
	exit 1
fi
if printf '%s\n' "$published" | cut -f2 | grep -qx "$name"; then
	echo "::error::version_name $name is already published" >&2
	exit 1
fi
if ! rssh "test ! -e /opt/app/releases/$remote_name"; then
	echo "::error::/opt/app/releases/$remote_name already exists on the server" >&2
	exit 1
fi
echo "::endgroup::"

echo "::group::upload"
rscp "$apk" "$SSH_TARGET:/opt/app/releases/.$remote_name.part"
rssh "chmod 0644 /opt/app/releases/.$remote_name.part && mv -n /opt/app/releases/.$remote_name.part /opt/app/releases/$remote_name"
echo "::endgroup::"

echo "::group::register"
# The changelog goes through stdin ("-"), so it never appears in a command line.
rssh_stdin "cd /opt/app && docker compose exec -T core core release add $code $name /releases/$remote_name -" <"$changelog"
rssh "cd /opt/app && docker compose exec -T core core release list" || true
echo "::endgroup::"
echo "published $name ($code)"
