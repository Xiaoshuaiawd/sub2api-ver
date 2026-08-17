#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker frontend memory test failed: %s\n' "$1" >&2
  exit 1
}

grep -Fqx 'ARG NODE_MAX_OLD_SPACE_SIZE=4096' deploy/Dockerfile || \
  fail 'deploy/Dockerfile must provide a 4096 MB default Node heap'

grep -Fqx 'ENV NODE_OPTIONS=--max-old-space-size=${NODE_MAX_OLD_SPACE_SIZE}' deploy/Dockerfile || \
  fail 'deploy/Dockerfile must derive NODE_OPTIONS from NODE_MAX_OLD_SPACE_SIZE'

if grep -Fq 'ENV NODE_OPTIONS=--max-old-space-size=1536' deploy/Dockerfile; then
  fail 'deploy/Dockerfile still hard-codes the insufficient 1536 MB Node heap'
fi

printf 'docker frontend memory test passed\n'
