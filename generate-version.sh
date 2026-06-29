#!/usr/bin/env bash

set -e

# Default computed version
COMPUTED_VERSION=$(git describe --tags --match 'v*.*.*' 2>/dev/null || true)

# Compute the version if the previous command has failed
if [ -z "${COMPUTED_VERSION}" ]; then
    COMMIT_COUNT=$(git rev-list --all --count 2>/dev/null || echo "0")
    SHORT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    COMPUTED_VERSION="0.0.0-${COMMIT_COUNT}-${SHORT_COMMIT}"
fi

echo ${COMPUTED_VERSION}
