#!/usr/bin/env bash
# new-worker-cert.sh issues a client certificate for a worker (docs/10-deploy.md,
# "Сертификаты"). It runs on the owner's PC: the project CA key never leaves it.
#
# Usage:
#   new-worker-cert.sh <worker-id> [--ca-dir DIR] [--out DIR] [--days N]
#
#   worker-id   the CN and the id allowed on the core, e.g. worker-2
#   --ca-dir    directory with ca.crt and ca.key, default ~/quoker-ca
#   --out       output directory, default ~/quoker-certs/<worker-id>
#   --days      validity in days, default 365 (docs: one year)
#
# The script prints the SHA-256 fingerprint and the next commands. It does not connect
# to any server. Afterwards, move worker.key to the worker and delete the local copy.
set -euo pipefail

die() { printf 'new-worker-cert: ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
	sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
}

worker_id=""
ca_dir=$HOME/quoker-ca
out_dir=""
days=365

while (($#)); do
	case "$1" in
	--ca-dir) [[ $# -ge 2 ]] || die "--ca-dir needs a directory"; ca_dir=$2; shift 2 ;;
	--out) [[ $# -ge 2 ]] || die "--out needs a directory"; out_dir=$2; shift 2 ;;
	--days) [[ $# -ge 2 ]] || die "--days needs a number"; days=$2; shift 2 ;;
	-h | --help) usage; exit 0 ;;
	-*) die "unknown option: $1 (see --help)" ;;
	*)
		[[ -z $worker_id ]] || die "only one worker id is allowed"
		worker_id=$1
		shift
		;;
	esac
done

[[ -n $worker_id ]] || die "worker id is required, e.g. worker-2 (see --help)"
[[ $worker_id =~ ^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$ ]] || die "worker id must be 2-32 chars: a-z, 0-9, dash"
[[ $days =~ ^[1-9][0-9]{0,3}$ ]] || die "--days must be a positive integer"
command -v step >/dev/null || die "step CLI is not installed (brew install step)"

ca_crt=$ca_dir/ca.crt
ca_key=$ca_dir/ca.key
[[ -r $ca_crt ]] || die "$ca_crt not found: the project CA certificate"
[[ -r $ca_key ]] || die "$ca_key not found: the project CA key"

# The CA key must not be readable by others.
if [[ $(uname) == Darwin ]]; then
	key_mode=$(stat -f '%Lp' "$ca_key")
else
	key_mode=$(stat -c '%a' "$ca_key")
fi
case "$key_mode" in
400 | 600) ;;
*) die "$ca_key has mode $key_mode; run: chmod 600 $ca_key" ;;
esac

[[ -n $out_dir ]] || out_dir=$HOME/quoker-certs/$worker_id
if [[ -e $out_dir/worker.key || -e $out_dir/worker.crt ]]; then
	die "$out_dir already holds a certificate: move it away first, so an old key is not overwritten"
fi

umask 077
mkdir -p "$out_dir"
crt=$out_dir/worker.crt
key=$out_dir/worker.key

# Leaf certificate signed by the project CA. --no-password --insecure: the key is stored
# unencrypted on purpose, because the worker reads it without a passphrase.
step certificate create "$worker_id" "$crt" "$key" \
	--profile leaf \
	--ca "$ca_crt" --ca-key "$ca_key" \
	--not-after "$((days * 24))h" \
	--no-password --insecure

fingerprint=$(step certificate fingerprint "$crt")
expires=$(step certificate inspect "$crt" 2>/dev/null | sed -n 's/^ *Not After: *//p' | head -n 1 || true)

cat <<EOF

Certificate: $crt
Key:         $key   (mode 0600, never commit, never send to chats)
Expires:     ${expires:-see: step certificate inspect $crt}
Fingerprint: $fingerprint

Next steps (the commands are not run by this script):

1. Put the files on the worker. Replace <worker-host> with the worker's address. The key
   must be group-readable by quoker (gid 10001), which the container uses:
     ssh deploy@<worker-host> 'umask 027 && cat > /opt/app/certs/worker.crt && chmod 0640 /opt/app/certs/worker.crt' < "$crt"
     ssh deploy@<worker-host> 'umask 027 && cat > /opt/app/certs/worker.key && chmod 0640 /opt/app/certs/worker.key' < "$key"
   The CA certificate is needed on the worker once: ca.crt into /opt/app/certs/ (mode 0644).

2. Allow the fingerprint on the core, either in the bot (admin, private chat):
     /allow_worker $worker_id $fingerprint
   or on the core server:
     docker compose exec core core workers allow $worker_id $fingerprint

3. Start or restart the worker: cd /opt/app && docker compose up -d worker

4. Delete the local copy of the key once the worker is running:
     rm -f "$key"
EOF
