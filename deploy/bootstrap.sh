#!/usr/bin/env bash
# bootstrap.sh prepares a fresh Ubuntu VPS for Quoker (docs/10-deploy.md,
# section "bootstrap.sh"). Run it as root on the server:
#
#   sudo ./bootstrap.sh --role core --authorized-keys /root/keys.pub [--reality]
#   sudo ./bootstrap.sh --role worker --authorized-keys /root/keys.pub
#
# The script is idempotent: running it again changes nothing that is already
# in the desired state. It never prints secrets and never touches containers
# or application files beyond creating the /opt/app directory layout.
#
# Lockout safety: sshd is hardened (no root login, no passwords) only after
# user "deploy" has at least one valid public key. Open a second terminal and
# check `ssh deploy@<host>` before you close your root session.

set -euo pipefail

readonly APP_DIR=/opt/app
readonly APP_GROUP=quoker
readonly APP_ID=10001          # uid and gid the containers run as
readonly DEPLOY_USER=deploy
readonly SWAP_FILE=/swapfile
readonly SWAP_SIZE=1G
readonly SSHD_DROPIN=/etc/ssh/sshd_config.d/10-quoker.conf
readonly F2B_JAIL=/etc/fail2ban/jail.d/quoker.local
readonly DOCKER_KEYRING=/etc/apt/keyrings/docker.asc
readonly DOCKER_LIST=/etc/apt/sources.list.d/docker.list
readonly DAEMON_JSON=/etc/docker/daemon.json
readonly APT_AUTO_UPGRADES=/etc/apt/apt.conf.d/20auto-upgrades

ROLE=""
KEYS_FILE=""
REALITY=0

log() { printf '[bootstrap] %s\n' "$*"; }
warn() { printf '[bootstrap] WARNING: %s\n' "$*" >&2; }
die() { printf '[bootstrap] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
	cat <<'EOF'
Usage: sudo ./bootstrap.sh --role core|worker [options]

Prepares an Ubuntu server for Quoker: upgrades, unattended security updates,
user "deploy" with key-only SSH, fail2ban, Docker Engine with the compose
plugin, log rotation, 1 GB swap, /opt/app layout and the firewall.

Options:
  --role core|worker       Required. core: public API, Caddy, core, optional xray.
                           worker: Telegram/Instagram/TikTok worker.
  --authorized-keys FILE   Public keys (one per line) for user "deploy". They
                           replace the file's current content. Without this
                           option the keys from /root/.ssh/authorized_keys are
                           copied, if deploy has none yet.
  --reality                core only: open 8443/tcp for the REALITY fallback
                           (docs/10-deploy.md). Without it the port is closed.
  -h, --help               Show this help and exit.

Requirements: root, Ubuntu 22.04 or newer, outbound HTTPS to download.docker.com.
EOF
}

parse_args() {
	while (($#)); do
		case "$1" in
		--role)
			[[ $# -ge 2 ]] || die "--role needs a value (core or worker)"
			ROLE=$2
			shift 2
			;;
		--role=*)
			ROLE=${1#*=}
			shift
			;;
		--authorized-keys)
			[[ $# -ge 2 ]] || die "--authorized-keys needs a file name"
			KEYS_FILE=$2
			shift 2
			;;
		--reality)
			REALITY=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1 (see --help)"
			;;
		esac
	done

	case "$ROLE" in
	core | worker) ;;
	"") die "--role is required (core or worker); see --help" ;;
	*) die "--role must be core or worker, got: $ROLE" ;;
	esac
	if [[ $ROLE == worker && $REALITY -eq 1 ]]; then
		die "--reality applies to the core only (the worker needs no open ports)"
	fi
	if [[ -n $KEYS_FILE && ! -r $KEYS_FILE ]]; then
		die "--authorized-keys: cannot read $KEYS_FILE"
	fi
}

preflight() {
	[[ $EUID -eq 0 ]] || die "run as root (sudo)"
	[[ -r /etc/os-release ]] || die "/etc/os-release not found: unsupported system"
	# shellcheck source=/dev/null
	. /etc/os-release
	[[ ${ID:-} == ubuntu ]] || die "only Ubuntu is supported, found: ${ID:-unknown}"
	local major=${VERSION_ID%%.*}
	((major >= 22)) || die "Ubuntu 22.04 or newer is required, found $VERSION_ID"
	if dpkg -s docker.io >/dev/null 2>&1; then
		die "package docker.io (Ubuntu's Docker) is installed; remove it first, it conflicts with Docker Engine"
	fi
}

# write_file PATH MODE reads the new content from stdin. It writes the file
# only when the content differs and returns 0 when it changed, 1 otherwise.
write_file() {
	local path=$1 mode=$2 tmp
	tmp=$(mktemp)
	cat >"$tmp"
	if [[ -f $path ]] && cmp -s "$tmp" "$path"; then
		rm -f "$tmp"
		return 1
	fi
	install -D -m "$mode" "$tmp" "$path"
	rm -f "$tmp"
	return 0
}

ensure_group() {
	local existing
	existing=$(getent group "$APP_GROUP" | cut -d: -f3 || true)
	if [[ -n $existing ]]; then
		[[ $existing == "$APP_ID" ]] || die "group $APP_GROUP exists with gid $existing, expected $APP_ID"
		return 0
	fi
	if getent group "$APP_ID" >/dev/null; then
		die "gid $APP_ID is taken by another group; the containers need this gid"
	fi
	groupadd --gid "$APP_ID" "$APP_GROUP"
	log "created group $APP_GROUP (gid $APP_ID)"
}

# validate_keys FILE checks that every non-empty line is a plain public key.
validate_keys() {
	local file=$1 line count=0 tmp
	tmp=$(mktemp)
	while IFS= read -r line || [[ -n $line ]]; do
		[[ -z ${line//[[:space:]]/} ]] && continue
		[[ $line == \#* ]] && continue
		if [[ $line == *PRIVATE* ]]; then
			die "$file contains a private key; pass only public keys"
		fi
		if ! [[ $line =~ ^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)\ [A-Za-z0-9+/=]+ ]]; then
			die "$file: line is not a plain public key (options are not supported): ${line:0:40}..."
		fi
		printf '%s\n' "$line" >"$tmp"
		ssh-keygen -l -f "$tmp" >/dev/null 2>&1 || die "$file: invalid public key: ${line:0:40}..."
		count=$((count + 1))
	done <"$file"
	rm -f "$tmp"
	((count > 0)) || die "$file contains no public keys"
}

step_base() {
	log "1/7 apt: upgrade packages, enable unattended security upgrades"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	apt-get -y -qq -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold upgrade
	apt-get install -y -qq ca-certificates curl gnupg unattended-upgrades ufw fail2ban jq rsync
	if printf '%s\n' \
		'APT::Periodic::Update-Package-Lists "1";' \
		'APT::Periodic::Unattended-Upgrade "1";' \
		'APT::Periodic::AutocleanInterval "7";' |
		write_file "$APT_AUTO_UPGRADES" 0644; then
		log "unattended-upgrades: periodic security updates enabled"
	fi
	systemctl enable --now unattended-upgrades >/dev/null
}

step_deploy_user() {
	log "2/7 user $DEPLOY_USER: key-only login, sshd hardening, fail2ban"
	ensure_group
	if ! id -u "$DEPLOY_USER" >/dev/null 2>&1; then
		useradd --create-home --shell /bin/bash --user-group "$DEPLOY_USER"
		log "created user $DEPLOY_USER"
	fi
	usermod -aG "$APP_GROUP" "$DEPLOY_USER"

	local ssh_dir=/home/$DEPLOY_USER/.ssh auth
	auth=$ssh_dir/authorized_keys
	install -d -m 0700 -o "$DEPLOY_USER" -g "$DEPLOY_USER" "$ssh_dir"

	if [[ -n $KEYS_FILE ]]; then
		validate_keys "$KEYS_FILE"
		write_file "$auth" 0600 <"$KEYS_FILE" || true
		log "authorized_keys for $DEPLOY_USER set from $KEYS_FILE"
	elif [[ ! -s $auth && -s /root/.ssh/authorized_keys ]]; then
		install -m 0600 /root/.ssh/authorized_keys "$auth"
		warn "copied root's authorized_keys to $DEPLOY_USER; check it with: cat $auth"
	fi
	chown -R "$DEPLOY_USER:$DEPLOY_USER" "$ssh_dir"
	chmod 0600 "$auth"

	# The gate: never disable passwords or root login without a working key.
	if [[ ! -s $auth ]] || ! grep -qE '^(ssh-|ecdsa-|sk-)' "$auth"; then
		die "$DEPLOY_USER has no public keys in $auth: refusing to disable password and root login. Re-run with --authorized-keys FILE"
	fi

	harden_sshd
	install_fail2ban
}

harden_sshd() {
	local content changed=0
	content=$(cat <<'EOF'
# Managed by deploy/bootstrap.sh (Quoker). This file is read before the
# distribution defaults, so its values win.
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
EOF
)
	if printf '%s\n' "$content" | write_file "$SSHD_DROPIN" 0644; then changed=1; fi
	if ((changed)); then
		if ! sshd -t; then
			rm -f "$SSHD_DROPIN"
			die "sshd configuration is invalid after hardening; the drop-in was removed"
		fi
		systemctl reload ssh 2>/dev/null || systemctl reload sshd
		log "sshd hardened and reloaded (existing sessions stay open)"
	fi
	local effective
	effective=$(sshd -T)
	grep -qx 'permitrootlogin no' <<<"$effective" || die "sshd still allows root login; check $SSHD_DROPIN"
	grep -qx 'passwordauthentication no' <<<"$effective" || die "sshd still allows passwords; check $SSHD_DROPIN"
}

install_fail2ban() {
	local content changed=0
	content=$(cat <<'EOF'
# Managed by deploy/bootstrap.sh (Quoker).
[DEFAULT]
bantime = 1h
findtime = 10m
maxretry = 5
backend = systemd

[sshd]
enabled = true
EOF
)
	if printf '%s\n' "$content" | write_file "$F2B_JAIL" 0644; then changed=1; fi
	systemctl enable --now fail2ban >/dev/null
	if ((changed)); then systemctl restart fail2ban; fi
}

step_docker() {
	log "3/7 Docker Engine and compose plugin from the Docker apt repository"
	install -d -m 0755 /etc/apt/keyrings
	if [[ ! -s $DOCKER_KEYRING ]]; then
		curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o "$DOCKER_KEYRING.tmp"
		install -m 0644 "$DOCKER_KEYRING.tmp" "$DOCKER_KEYRING"
		rm -f "$DOCKER_KEYRING.tmp"
	fi
	local arch codename
	arch=$(dpkg --print-architecture)
	# shellcheck source=/dev/null
	codename=$(. /etc/os-release && echo "${UBUNTU_CODENAME:-$VERSION_CODENAME}")
	printf 'deb [arch=%s signed-by=%s] https://download.docker.com/linux/ubuntu %s stable\n' \
		"$arch" "$DOCKER_KEYRING" "$codename" | write_file "$DOCKER_LIST" 0644 || true
	apt-get update -qq
	apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
	systemctl enable --now docker >/dev/null
	usermod -aG docker "$DEPLOY_USER"
	docker compose version >/dev/null
	log "docker: $(docker --version | cut -d, -f1), $(docker compose version --short 2>/dev/null || echo compose)"
}

step_docker_config() {
	log "4/7 Docker log rotation (json-file, 10m x 3)"
	local content
	content=$(cat <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
EOF
)
	if printf '%s\n' "$content" | write_file "$DAEMON_JSON" 0644; then
		systemctl restart docker
		log "daemon.json changed, docker restarted"
	fi
}

step_swap() {
	log "5/7 swap $SWAP_SIZE"
	if [[ ! -e $SWAP_FILE ]]; then
		if [[ -n $(swapon --show --noheadings) ]]; then
			log "swap is already active on this system, $SWAP_FILE not created"
			return 0
		fi
		fallocate -l "$SWAP_SIZE" "$SWAP_FILE"
		chmod 0600 "$SWAP_FILE"
		mkswap -q "$SWAP_FILE" >/dev/null
	fi
	if ! swapon --show=NAME --noheadings | grep -qx "$SWAP_FILE"; then
		swapon "$SWAP_FILE"
	fi
	if ! grep -qE "^${SWAP_FILE}[[:space:]]" /etc/fstab; then
		printf '%s none swap sw 0 0\n' "$SWAP_FILE" >>/etc/fstab
	fi
}

# dir_setup PATH OWNER GROUP MODE creates a directory with the given owner.
dir_setup() {
	install -d -m "$4" -o "$2" -g "$3" "$1"
	# install does not change an existing directory, so enforce the mode too.
	chown "$2:$3" "$1"
	chmod "$4" "$1"
}

step_app_dirs() {
	log "6/7 $APP_DIR layout"
	dir_setup "$APP_DIR" "$DEPLOY_USER" "$DEPLOY_USER" 0755
	# Container-written directories belong to uid 10001. Group quoker (gid
	# 10001) is shared with deploy, so deploy can read and copy files in.
	# Setgid (2xxx) makes files created by deploy inherit the group.
	# Private data (data, cache, tmp) is readable by the container only.
	dir_setup "$APP_DIR/certs" "$DEPLOY_USER" "$APP_GROUP" 2750
	dir_setup "$APP_DIR/xray" "$DEPLOY_USER" "$APP_GROUP" 2750
	if [[ $ROLE == core ]]; then
		dir_setup "$APP_DIR/data" "$APP_ID" "$APP_ID" 0750
		dir_setup "$APP_DIR/cache" "$APP_ID" "$APP_ID" 0750
		dir_setup "$APP_DIR/backups" "$APP_ID" "$APP_GROUP" 2750
		dir_setup "$APP_DIR/releases" "$APP_ID" "$APP_GROUP" 2770
	else
		dir_setup "$APP_DIR/tmp" "$APP_ID" "$APP_ID" 0750
	fi
}

step_firewall() {
	log "7/7 ufw"
	ufw default deny incoming >/dev/null
	ufw default allow outgoing >/dev/null

	# Allow the SSH port sshd really listens on, before enabling ufw.
	local port
	while read -r port; do
		ufw allow "$port/tcp" comment 'ssh' >/dev/null
	done < <(sshd -T | awk '$1 == "port" {print $2}')

	if [[ $ROLE == core ]]; then
		ufw allow 80/tcp comment 'caddy http (ACME)' >/dev/null
		ufw allow 443/tcp comment 'caddy https' >/dev/null
		if ((REALITY)); then
			ufw allow 8443/tcp comment 'xray reality fallback' >/dev/null
		else
			# Remove the rule if an earlier run added it. Absent rule is fine.
			ufw delete allow 8443/tcp >/dev/null 2>&1 || true
		fi
	fi
	ufw --force enable >/dev/null
	ufw status verbose | sed 's/^/[bootstrap]   /'
	warn "Docker-published ports bypass ufw (iptables DNAT). Only ports listed in compose 'ports:' are reachable from outside."
}

print_next_steps() {
	cat <<EOF

[bootstrap] Done (role: $ROLE). Next steps, as the owner:

1. In a NEW terminal check key login: ssh $DEPLOY_USER@<server-ip>
   Only then close the root session.
EOF
	if [[ $ROLE == core ]]; then
		cat <<EOF
2. Copy the core files to $APP_DIR (from the repo, deploy/core/ and deploy/render-xray.sh):
     compose.yml, Caddyfile, xray/config.json.tmpl, render-xray.sh
     .env           <- cp .env.example .env, then fill DOMAIN, ACME_EMAIL, tags
     certs/ca.crt   <- the project CA public certificate (mode 0644, required before 'up')
3. REALITY (optional): fill REALITY_* in .env, then
     cd $APP_DIR && bash render-xray.sh core
   and run 'docker compose --profile fallback up -d xray' only when needed.
4. Log in to GHCR for a manual deploy (private images), e.g.
     echo "\$PAT" | docker login ghcr.io -u tuskar2321 --password-stdin
5. cd $APP_DIR && docker compose up -d
   Then: docker compose exec core core admin bootstrap  (see docs/admin-manual.md)
EOF
	else
		cat <<EOF
2. Copy the worker files to $APP_DIR (from the repo, deploy/worker/ and deploy/render-xray.sh):
     compose.yml, .env (cp .env.example .env, fill WORKER_ID, CORE_ADDR, tags)
     certs/worker.crt, certs/worker.key (mode 0640), certs/ca.crt
     Use: install -m 0640 ... or 'umask 027; cat > file' so the group quoker can read the key.
3. REALITY (optional): TRANSPORT=reality and REALITY_* in .env, then
     cd $APP_DIR && bash render-xray.sh worker
4. Log in to GHCR (see the core step 4), then
     cd $APP_DIR && docker compose up -d        (add --profile fallback with REALITY)
EOF
	fi
	cat <<EOF

Reboot is not automatic. After kernel updates run 'sudo reboot' in a quiet moment.
EOF
}

main() {
	parse_args "$@"
	preflight
	log "role: $ROLE, reality: $REALITY"
	step_base
	step_deploy_user
	step_docker
	step_docker_config
	step_swap
	step_app_dirs
	step_firewall
	print_next_steps
}

main "$@"
