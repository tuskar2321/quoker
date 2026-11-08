#!/usr/bin/env bash
# Deploys one role to its server over SSH (docs/10-deploy.md, "CI/CD").
#
#   deploy-server.sh <core|worker> <image tag>
#
# Env: DEPLOY_HOST, DEPLOY_SSH_KEY, DEPLOY_KNOWN_HOSTS (see lib-ssh.sh),
#      GHCR_USER and GHCR_TOKEN (the job's GITHUB_TOKEN; sent to the server on stdin only).
#
# Steps: copy compose.yml (and Caddyfile for core) to /opt/app; log in to ghcr.io on
# the server; core only: `core backup` before anything changes; set <ROLE>_TAG in
# /opt/app/.env; pull; `up -d --wait`; `healthcheck`. If the new image is not healthy,
# the previous tag is restored, its logs are printed, and the job fails. The server is
# logged out of ghcr.io on every exit.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
# shellcheck source=lib-ssh.sh
. "$here/lib-ssh.sh"

role=${1:-}
tag=${2:-}
case $role in
core | worker) ;;
*)
	echo "usage: deploy-server.sh <core|worker> <image tag>" >&2
	exit 2
	;;
esac
if [[ ! $tag =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]]; then
	echo "::error::invalid image tag: $tag" >&2
	exit 2
fi
: "${GHCR_USER:?GHCR_USER is not set}"
: "${GHCR_TOKEN:?GHCR_TOKEN is not set}"
if [[ ! $GHCR_USER =~ ^[A-Za-z0-9][A-Za-z0-9-]*$ ]]; then
	echo "::error::invalid GHCR_USER" >&2
	exit 2
fi

case $role in
core) key=CORE_TAG ;;
worker) key=WORKER_TAG ;;
esac

logged_in=0
# shellcheck disable=SC2317,SC2329 # invoked by the EXIT trap below (SC2317 on shellcheck < 0.10)
cleanup() {
	if [[ $logged_in == 1 ]]; then
		rssh "docker logout ghcr.io >/dev/null 2>&1; exit 0" || true
	fi
	ssh_cleanup
}
trap cleanup EXIT

ssh_setup

echo "::group::preflight ($role)"
if ! rssh "test -f /opt/app/.env && test -d /opt/app"; then
	echo "::error::/opt/app/.env is missing on the $role server: run bootstrap.sh and fill .env first (docs/10-deploy.md)"
	exit 1
fi
echo "::endgroup::"

echo "::group::copy deploy files"
rscp "$repo/deploy/$role/compose.yml" "$SSH_TARGET:/opt/app/compose.yml.new"
rssh "mv -f /opt/app/compose.yml.new /opt/app/compose.yml"
if [[ $role == core ]]; then
	rscp "$repo/deploy/core/Caddyfile" "$SSH_TARGET:/opt/app/Caddyfile.new"
	rssh "mv -f /opt/app/Caddyfile.new /opt/app/Caddyfile"
fi
echo "::endgroup::"

echo "::group::registry login on the server"
printf '%s' "$GHCR_TOKEN" | rssh_stdin "docker login ghcr.io -u '$GHCR_USER' --password-stdin >/dev/null"
logged_in=1
echo "::endgroup::"

prev=$(rssh "grep -E '^${key}=' /opt/app/.env | tail -n 1 | cut -d= -f2-; exit 0")

if [[ $role == core ]]; then
	running=$(rssh "cd /opt/app && docker compose ps --status running -q core") || {
		echo "::error::cannot query the core container" >&2
		exit 1
	}
	if [[ -n $running ]]; then
		echo "::group::backup before the switch"
		rssh "cd /opt/app && docker compose exec -T core core backup" || {
			echo "::error::backup failed: nothing was changed, the deploy is aborted" >&2
			exit 1
		}
		echo "::endgroup::"
	else
		echo "core is not running: no backup before the first deploy"
	fi
fi

set_tag() {
	rssh "cd /opt/app && touch .env && if grep -q '^${key}=' .env; then sed -i 's/^${key}=.*/${key}=$1/' .env; else printf '%s=%s\n' '${key}' '$1' >> .env; fi"
}

# Pull, start and wait for healthy. Core also refreshes Caddy, so a changed
# Caddyfile is reloaded (or Caddy starts on the first deploy).
deploy_role() {
	local cmd="cd /opt/app && docker compose pull $role && docker compose up -d --no-deps --wait --wait-timeout 180 $role"
	if [[ $role == core ]]; then
		cmd+=" && { if docker compose ps --status running -q caddy | grep -q .; then docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile; else docker compose up -d --wait caddy; fi; }"
	fi
	cmd+=" && docker compose exec -T $role /usr/local/bin/$role healthcheck"
	rssh "$cmd"
}

echo "::group::switch $key to $tag and deploy"
set_tag "$tag"
if deploy_role; then
	echo "::endgroup::"
	rssh "cd /opt/app && docker compose ps $role" || true
	echo "$role deployed: $tag"
	exit 0
fi
echo "::endgroup::"

echo "::error::$role did not become healthy on $tag"
rssh "cd /opt/app && docker compose ps -a $role; docker compose logs --tail=200 $role" || true
if [[ -n $prev && $prev != "$tag" ]]; then
	echo "::warning::rolling $role back to $prev"
	set_tag "$prev"
	if ! deploy_role; then
		echo "::error::rollback to $prev also failed: investigate on the server" >&2
	fi
fi
echo "::error::deploy of $role $tag failed; the database is not restored automatically (see docs/10-deploy.md, backups)" >&2
exit 1
