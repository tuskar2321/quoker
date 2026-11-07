#!/usr/bin/env bash
# backup-pull.sh copies new core database snapshots from the core server to this PC
# (docs/10-deploy.md, "Бэкапы"). Snapshots are created by the core itself, daily at
# 04:00 Moscow time and before each release. This script only downloads them.
#
# Usage:
#   backup-pull.sh [--verify] [--keep N] [--dest DIR] [--host USER@HOST] [--remote-dir DIR]
#
#   --verify      run PRAGMA integrity_check on the newest local snapshot (needs sqlite3)
#   --keep N      keep the N newest local snapshots, default 30
#   --dest DIR    local directory, put it on an encrypted disk
#   --host        SSH target, default deploy@<core>, see the config file
#   --remote-dir  remote snapshot directory, default /opt/app/backups
#
# Configuration file (optional, outside the repo): ~/.config/quoker/backup-pull.env
#   QUOKER_BACKUP_HOST=deploy@<core-ip-or-host>
#   QUOKER_BACKUP_DEST=/Volumes/Encrypted/quoker-backups
#   QUOKER_BACKUP_KEY=~/.ssh/quoker_backup_ed25519
#   QUOKER_BACKUP_KEEP=30
#
# Snapshots contain the bot token and Instagram cookies: treat them as secrets.
# Compatible with the macOS /bin/bash 3.2 and with Linux bash.
set -euo pipefail

CONFIG_FILE=${QUOKER_PC_CONFIG:-$HOME/.config/quoker/backup-pull.env}
if [[ -r $CONFIG_FILE ]]; then
	# shellcheck source=/dev/null
	. "$CONFIG_FILE"
fi

host=${QUOKER_BACKUP_HOST:-}
dest=${QUOKER_BACKUP_DEST:-}
key=${QUOKER_BACKUP_KEY:-}
keep=${QUOKER_BACKUP_KEEP:-30}
remote_dir=/opt/app/backups
verify=0

die() { printf 'backup-pull: ERROR: %s\n' "$*" >&2; exit 1; }
log() { printf '%s backup-pull: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# list_snapshots prints local snapshot names, newest first.
list_snapshots() {
	local f
	for f in "$dest"/quoker-*.db; do
		[[ -e $f ]] && basename "$f"
	done | sort -r
}

usage() {
	sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
}

while (($#)); do
	case "$1" in
	--verify) verify=1; shift ;;
	--keep) [[ $# -ge 2 ]] || die "--keep needs a number"; keep=$2; shift 2 ;;
	--dest) [[ $# -ge 2 ]] || die "--dest needs a directory"; dest=$2; shift 2 ;;
	--host) [[ $# -ge 2 ]] || die "--host needs USER@HOST"; host=$2; shift 2 ;;
	--remote-dir) [[ $# -ge 2 ]] || die "--remote-dir needs a path"; remote_dir=$2; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	*) die "unknown argument: $1 (see --help)" ;;
	esac
done

[[ -n $host ]] || die "set QUOKER_BACKUP_HOST in $CONFIG_FILE or pass --host"
[[ -n $dest ]] || die "set QUOKER_BACKUP_DEST in $CONFIG_FILE or pass --dest"
[[ $keep =~ ^[1-9][0-9]*$ ]] || die "--keep must be a positive integer, got: $keep"
command -v rsync >/dev/null || die "rsync is not installed"

if [[ ! -d $dest ]]; then
	die "destination $dest does not exist: mount the encrypted disk first"
fi
[[ -w $dest ]] || die "destination $dest is not writable"

ssh_opts="-o BatchMode=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=20"
if [[ -n $key ]]; then
	ssh_opts="$ssh_opts -i $key -o IdentitiesOnly=yes"
fi

log "pulling new snapshots from $host:$remote_dir"
# Only finished snapshots: VACUUM INTO writes complete files, WAL files are never copied.
rsync -a --partial --timeout=120 \
	--include='quoker-*.db' --exclude='*' \
	-e "ssh $ssh_opts" \
	"$host:$remote_dir/" "$dest/"

# Keep the newest $keep snapshots. Names are quoker-YYYYMMDD-HHMMSS.db, so a name sort is a time sort.
count=0
while IFS= read -r name; do
	count=$((count + 1))
	if ((count > keep)); then
		rm -f -- "$dest/$name"
		log "pruned $name"
	fi
done < <(list_snapshots)

newest=$(list_snapshots | head -n 1 || true)
if [[ -z $newest ]]; then
	die "no snapshots in $dest after the pull: check the core backup timer (docs/10-deploy.md)"
fi
log "newest local snapshot: $newest (snapshots found before pruning: $count)"

if ((verify)); then
	command -v sqlite3 >/dev/null || die "--verify needs sqlite3"
	result=$(sqlite3 "$dest/$newest" 'PRAGMA integrity_check;')
	if [[ $result == ok ]]; then
		log "integrity_check: ok"
	else
		die "integrity_check failed for $newest: $result"
	fi
fi
log "done"
