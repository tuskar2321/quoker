#!/bin/sh
# Seeds the local stand after `docker compose up` (run by `make dev`):
#   1. the bot token, which points the bot at the fake Telegram API;
#   2. the worker, allowed by its certificate fingerprint (in dev: its WORKER_ID);
#   3. the first admin invite, printed once, while no admin exists.
# Every step is safe to repeat.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
compose="docker compose -f $here/compose.yml"

: "${DEV_BOT_TOKEN:?run make dev}"
: "${DEV_WORKER_ID:?run make dev}"

printf '%s' "$DEV_BOT_TOKEN" | $compose exec -T core core settings set bot_token -

$compose exec -T core core workers allow "$DEV_WORKER_ID" "$DEV_WORKER_ID"

if out=$($compose exec -T core core admin bootstrap 2>&1); then
  echo "$out"
elif echo "$out" | grep -q "already exists"; then
  echo "admin already exists: no new invite (create one in the app or with the bot)"
else
  echo "$out" >&2
  exit 1
fi

cat <<'HINT'

Fake Telegram admin API: http://127.0.0.1:8099 (see internal/devtools/faketelegram/admin.go)
  inject a message:  curl -s -X POST 127.0.0.1:8099/_fake/messages -d '{"chat_id":42,"user_id":42,"first_name":"Max","text":"/start"}'
  list sent:         curl -s 127.0.0.1:8099/_fake/sent
  state:             curl -s 127.0.0.1:8099/_fake/state
HINT
