#!/usr/bin/env bash
# render-xray.sh renders xray/config.json from xray/config.json.tmpl and the
# REALITY values in .env (docs/10-deploy.md, "Запасной транспорт REALITY").
#
# Usage: bash render-xray.sh core|worker [DIR]
#   DIR defaults to the current directory and must contain .env and xray/config.json.tmpl.
#   Output: DIR/xray/config.json, mode 0640, group quoker (the container runs as 10001).
#
# The .env file is read as data, never executed. Secret values are not printed.
# Run it again after any REALITY value changes, then restart xray:
#   docker compose --profile fallback up -d xray
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: bash render-xray.sh core|worker [DIR]

Renders DIR/xray/config.json from DIR/xray/config.json.tmpl using REALITY_* values
from DIR/.env. Fails if a required value is missing or malformed.
EOF
}

die() { printf 'render-xray: ERROR: %s\n' "$*" >&2; exit 1; }

# env_value KEY prints the last KEY=VALUE in .env. Nothing is executed.
env_value() {
	local line
	line=$(grep -E "^$1=" "$env_file" | tail -n 1 || true)
	printf '%s' "${line#*=}"
}

# check NAME VALUE PATTERN fails unless VALUE matches the extended regex PATTERN.
check() {
	local name=$1 value=$2 pattern=$3
	[[ -n $value ]] || die "$name is empty in $env_file"
	[[ $value =~ $pattern ]] || die "$name has an unexpected format (see .env.example)"
}

case "${1:-}" in
core | worker) role=$1 ;;
-h | --help | "") usage; exit 0 ;;
*) usage >&2; die "role must be core or worker" ;;
esac
dir=${2:-.}
env_file="$dir/.env"
tmpl="$dir/xray/config.json.tmpl"
out="$dir/xray/config.json"

[[ -r $env_file ]] || die "$env_file not found: copy .env.example and fill REALITY_* values"
[[ -r $tmpl ]] || die "$tmpl not found"
command -v jq >/dev/null || die "jq is required (bootstrap.sh installs it)"

uuid_re='^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
key_re='^[A-Za-z0-9_-]{43}$'
short_re='^([0-9a-f]{2}){1,8}$'
host_re='^[A-Za-z0-9._:-]{1,253}$'

declare_vars=(REALITY_UUID REALITY_SHORT_ID REALITY_SNI)
if [[ $role == core ]]; then
	declare_vars+=(REALITY_PRIVATE_KEY REALITY_DEST)
else
	declare_vars+=(REALITY_PUBLIC_KEY REALITY_SERVER CORE_HOST)
fi

content=$(<"$tmpl")
for name in "${declare_vars[@]}"; do
	value=$(env_value "$name")
	case "$name" in
	REALITY_UUID) check "$name" "$value" "$uuid_re" ;;
	REALITY_PRIVATE_KEY | REALITY_PUBLIC_KEY) check "$name" "$value" "$key_re" ;;
	REALITY_SHORT_ID) check "$name" "$value" "$short_re" ;;
	*) check "$name" "$value" "$host_re" ;;
	esac
	# The template keeps the JSON quotes around "${NAME}"; only the bare token is replaced.
	# Values are validated above, so they need no escaping. Keep the replacement unquoted:
	# a quoted replacement adds literal quote characters (checked on bash 3.2 and 5.x).
	content="${content//\$\{$name\}/$value}"
done

placeholder_re='\$\{[A-Za-z_][A-Za-z0-9_]*\}'
if [[ $content =~ $placeholder_re ]]; then
	die "template has placeholders this script does not fill: ${BASH_REMATCH[0]}"
fi

umask 027
tmp=$(mktemp "$dir/xray/.config.XXXXXX")
trap 'rm -f "$tmp"' EXIT
printf '%s\n' "$content" >"$tmp"
jq empty "$tmp" >/dev/null || die "rendered config is not valid JSON"
chgrp 10001 "$tmp" || die "cannot set group 10001 on the config: run bootstrap.sh first (it creates group quoker)"
chmod 0640 "$tmp"
mv -f "$tmp" "$out"
trap - EXIT
printf 'render-xray: wrote %s (role %s); restart xray to apply\n' "$out" "$role"
