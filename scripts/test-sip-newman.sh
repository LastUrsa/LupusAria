#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

work="$(mktemp -d)"
pid=""
cleanup() {
  if [[ -n "$pid" ]]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

go build -o "$work/LupusAria" .
: > "$work/.env"
env -u TWITCH_CHANNEL -u TWITCH_BOT_USERNAME -u TWITCH_OAUTH_TOKEN -u TWITCH_REFRESH_TOKEN \
  -u RAFFLE_ENABLED XDG_CONFIG_HOME="$work" LUPUSARIA_ENV_PATH="$work/.env" "$work/LupusAria" --service >"$work/service.log" 2>&1 &
pid=$!
base_url=http://127.0.0.1:47050
for _ in {1..75}; do
  if ! kill -0 "$pid" 2>/dev/null; then
    cat "$work/service.log" >&2
    exit 1
  fi
  if curl --silent --fail --max-time 1 "$base_url/api/v1/app" >/dev/null; then
    npm exec --yes --package=newman@6.2.1 -- newman run docs/postman/LupusAria-SIP-v1.postman_collection.json \
      --env-var "baseUrl=$base_url" --timeout-request 35000 --bail
    exit
  fi
  sleep 0.2
done
echo "LupusAria SIP did not become ready." >&2
cat "$work/service.log" >&2
exit 1
