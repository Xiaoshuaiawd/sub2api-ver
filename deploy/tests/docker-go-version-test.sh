#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

fail() {
  printf 'docker Go version test failed: %s\n' "$1" >&2
  exit 1
}

required_go_version=$(awk '$1 == "go" { print $2; exit }' backend/go.mod)
[ -n "$required_go_version" ] || fail 'backend/go.mod does not declare a Go version'

assert_contains() {
  file=$1
  expected=$2
  grep -Fqx "$expected" "$file" || fail "$file is not aligned with Go $required_go_version"
}

assert_contains Dockerfile "ARG GOLANG_IMAGE=golang:${required_go_version}-alpine"
assert_contains deploy/Dockerfile "ARG GOLANG_IMAGE=golang:${required_go_version}-alpine"
assert_contains backend/Dockerfile "FROM golang:${required_go_version}-alpine"

printf 'docker Go version test passed\n'
