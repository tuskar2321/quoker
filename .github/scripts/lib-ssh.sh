# shellcheck shell=bash
# Sourced by the deploy scripts. Not executable on its own.
#
# ssh_setup reads DEPLOY_HOST, DEPLOY_SSH_KEY and DEPLOY_KNOWN_HOSTS from the
# environment, writes the key and the pinned host keys to a private temp dir, and
# prepares the ssh options. Host keys are pinned: a changed server key refuses the
# connection (StrictHostKeyChecking=yes, no global known_hosts). The user is "deploy"
# unless DEPLOY_USER is set.
#
#   rssh <cmd...>        run a remote command with stdin closed
#   rssh_stdin <cmd...>  run a remote command with stdin passed through
#   rscp <src> <dst>     copy files (dst may be host:path)
#   ssh_cleanup          remove the key material; call it from an EXIT trap

ssh_setup() {
	: "${DEPLOY_HOST:?DEPLOY_HOST is not set}"
	: "${DEPLOY_SSH_KEY:?DEPLOY_SSH_KEY is not set}"
	: "${DEPLOY_KNOWN_HOSTS:?DEPLOY_KNOWN_HOSTS is not set}"
	if [[ ! $DEPLOY_HOST =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
		echo "::error::DEPLOY_HOST must be a host name or an IP address" >&2
		return 1
	fi
	SSH_WORKDIR=$(mktemp -d)
	(
		umask 077
		printf '%s\n' "$DEPLOY_SSH_KEY" >"$SSH_WORKDIR/key"
		printf '%s\n' "$DEPLOY_KNOWN_HOSTS" >"$SSH_WORKDIR/known_hosts"
	)
	SSH_TARGET="${DEPLOY_USER:-deploy}@$DEPLOY_HOST"
	SSH_OPTS=(
		-i "$SSH_WORKDIR/key"
		-o IdentitiesOnly=yes
		-o BatchMode=yes
		-o ConnectTimeout=20
		-o ServerAliveInterval=15
		-o StrictHostKeyChecking=yes
		-o UserKnownHostsFile="$SSH_WORKDIR/known_hosts"
		-o GlobalKnownHostsFile=/dev/null
	)
}

rssh() {
	# shellcheck disable=SC2029 # the remote command is a fixed string built by the caller
	ssh "${SSH_OPTS[@]}" "$SSH_TARGET" "$@" </dev/null
}

rssh_stdin() {
	# shellcheck disable=SC2029 # the remote command is a fixed string built by the caller
	ssh "${SSH_OPTS[@]}" "$SSH_TARGET" "$@"
}

rscp() {
	scp -q "${SSH_OPTS[@]}" "$@"
}

ssh_cleanup() {
	if [[ -n ${SSH_WORKDIR:-} && -d $SSH_WORKDIR ]]; then
		rm -rf "$SSH_WORKDIR"
	fi
}
