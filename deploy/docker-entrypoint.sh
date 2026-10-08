#!/bin/sh
set -e

# Compatibility: if the first arg looks like a flag (e.g. --help),
# prepend the default binary so it behaves the same as the old
# ENTRYPOINT ["/app/sub2api"] style.
if [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi

# Fix data directory permissions when running as root, then drop to sub2api.
# Docker named volumes / host bind-mounts may be owned by root, preventing the
# non-root sub2api user from writing files.
#
# The drop uses the resolved argv instead of re-executing this script: a second
# pass would need the unprivileged user to *read* the file, which fails with
# "can't open '/app/docker-entrypoint.sh': Permission denied" whenever the image
# was built from a context whose script mode grants execute but not read.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    # Use || true to avoid failure on read-only mounted files (e.g. config.yaml:ro)
    chown -R sub2api:sub2api /app/data 2>/dev/null || true
    exec su-exec sub2api "$@"
fi

exec "$@"
