#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker compose OpenAI WS bridge test failed: %s\n' "$1" >&2
  exit 1
}

assert_line() {
  file=$1
  line=$2
  grep -Fqx "$line" "$file" || fail "$file is missing: $line"
}

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  assert_line "$compose_file" '      - GATEWAY_OPENAI_WS_ENABLED=${GATEWAY_OPENAI_WS_ENABLED:-true}'
  assert_line "$compose_file" '      - GATEWAY_OPENAI_WS_APIKEY_ENABLED=${GATEWAY_OPENAI_WS_APIKEY_ENABLED:-true}'
  assert_line "$compose_file" '      - GATEWAY_OPENAI_WS_OAUTH_ENABLED=${GATEWAY_OPENAI_WS_OAUTH_ENABLED:-true}'
  assert_line "$compose_file" '      - GATEWAY_OPENAI_WS_RESPONSES_WEBSOCKETS_V2=${GATEWAY_OPENAI_WS_RESPONSES_WEBSOCKETS_V2:-true}'
  assert_line "$compose_file" '      - GATEWAY_OPENAI_WS_HTTP_INGRESS_BRIDGE_ENABLED=${GATEWAY_OPENAI_WS_HTTP_INGRESS_BRIDGE_ENABLED:-false}'
done

printf 'docker compose OpenAI WS bridge test passed\n'
