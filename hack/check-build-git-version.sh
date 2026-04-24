#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Verifies every Dockerfile and the shared Makefile.docker wires BUILD_GIT_VERSION.
# Run from repo root.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fail=0

if ! grep -q 'BUILD_GIT_VERSION=\$\$(git describe --tags --always --dirty)' Makefile.docker; then
	echo "FAIL: Makefile.docker missing BUILD_GIT_VERSION build-arg"
	fail=1
fi

while IFS= read -r -d '' dockerfile; do
	if ! grep -q '^ARG BUILD_GIT_VERSION=dev' "$dockerfile"; then
		echo "FAIL: $dockerfile missing 'ARG BUILD_GIT_VERSION=dev'"
		fail=1
	fi
	if ! grep -q '^ENV BUILD_GIT_VERSION=\${BUILD_GIT_VERSION}' "$dockerfile"; then
		echo "FAIL: $dockerfile missing 'ENV BUILD_GIT_VERSION=\${BUILD_GIT_VERSION}'"
		fail=1
	fi
	if ! grep -q 'LABEL org.opencontainers.image.version="\${BUILD_GIT_VERSION}"' "$dockerfile"; then
		echo "FAIL: $dockerfile missing OCI image.version LABEL"
		fail=1
	fi
done < <(find . -name Dockerfile -not -path './vendor/*' -not -path './prompts/*' -print0)

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: BUILD_GIT_VERSION wired in Makefile.docker and every Dockerfile"
