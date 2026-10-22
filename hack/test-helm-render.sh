#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Renders helm/templates/agents.yaml and asserts that the optional per-agent
# rerunOnLostClaim field renders whenever it is present — including `false` —
# and not at all when unset.
#
# Also renders helm/templates/cluster-assigner-cronjob.yaml and asserts that
# clusterAssigner.excludeAssignees renders EXCLUDE_ASSIGNEES comma-joined for a
# non-empty list and no env entry at all for the empty default.
# Run from repo root.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

CHART_DIR="${CHART_DIR:-helm}"
HELM_VERSION=v3.16.4

if command -v helm >/dev/null 2>&1; then
	helm_bin="$(command -v helm)"
else
	go install "helm.sh/helm/v3/cmd/helm@${HELM_VERSION}"
	helm_bin="$(go env GOPATH)/bin/helm"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fail=0

# write_values <file> <rerunOnLostClaim line or empty>
write_values() {
	local file="$1"
	local rr_line="$2"
	{
		cat <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
agents:
  - name: rr-agent
    enabled: true
    assignee: rr
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF
		if [ -n "$rr_line" ]; then
			printf '    %s\n' "$rr_line"
		fi
	} >"$file"
}

# run_case <case> <values file> <false|true|unset>
run_case() {
	local name="$1"
	local values="$2"
	local expect="$3"
	local out="$tmpdir/$name.out"

	"$helm_bin" template x "$CHART_DIR" -f "$values" --show-only templates/agents.yaml >"$out"

	local count
	count="$(grep -c '^kind: Config$' "$out" || true)"
	if [ "$count" -ne 1 ]; then
		echo "FAIL: $name: expected exactly 1 Config document, got $count"
		fail=1
		return
	fi

	case "$expect" in
	false)
		if ! grep -qx '  rerunOnLostClaim: false' "$out"; then
			echo "FAIL: $name: missing 'rerunOnLostClaim: false'"
			fail=1
			return
		fi
		;;
	true)
		if ! grep -qx '  rerunOnLostClaim: true' "$out"; then
			echo "FAIL: $name: missing 'rerunOnLostClaim: true'"
			fail=1
			return
		fi
		;;
	unset)
		if grep -q 'rerunOnLostClaim' "$out"; then
			echo "FAIL: $name: unexpected rerunOnLostClaim line"
			fail=1
			return
		fi
		;;
	esac

	echo "OK: $name"
}

write_values "$tmpdir/false.yaml" "rerunOnLostClaim: false"
write_values "$tmpdir/true.yaml" "rerunOnLostClaim: true"
write_values "$tmpdir/unset.yaml" ""

run_case false "$tmpdir/false.yaml" false
run_case true "$tmpdir/true.yaml" true
run_case unset "$tmpdir/unset.yaml" unset

# clusterAssigner.excludeAssignees → EXCLUDE_ASSIGNEES

# write_ca_values <file> <excludeAssignees YAML list literal or empty to omit the key>
write_ca_values() {
	local file="$1"
	local exclude="$2"
	{
		cat <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
clusterAssigner:
  enabled: true
  kafkaBrokers: kafka:9092
  gitRestURL: http://git-rest
EOF
		if [ -n "$exclude" ]; then
			printf '  excludeAssignees: %s\n' "$exclude"
		fi
	} >"$file"
}

# run_ca_case <case> <values file> <empty|"joined value">
run_ca_case() {
	local name="$1"
	local values="$2"
	local expect="$3"
	local out="$tmpdir/$name.out"

	"$helm_bin" template x "$CHART_DIR" -f "$values" \
		--show-only templates/cluster-assigner-cronjob.yaml >"$out"

	# Pin the chart default introduced alongside this feature: on defaults the
	# assigner must run a tag that honours EXCLUDE_ASSIGNEES, otherwise the
	# value is a silent no-op.
	if ! grep -q 'agent-cluster-assigner:v0\.2\.0' "$out"; then
		echo "FAIL: $name: default image tag is not v0.2.0"
		fail=1
		return
	fi

	if [ "$expect" = "empty" ]; then
		if grep -q 'EXCLUDE_ASSIGNEES' "$out"; then
			echo "FAIL: $name: unexpected EXCLUDE_ASSIGNEES entry for an empty list"
			fail=1
			return
		fi
		echo "OK: $name"
		return
	fi

	if ! grep -qx '                - name: EXCLUDE_ASSIGNEES' "$out"; then
		echo "FAIL: $name: missing 'EXCLUDE_ASSIGNEES' env entry"
		fail=1
		return
	fi
	if ! grep -qxF "                  value: \"$expect\"" "$out"; then
		echo "FAIL: $name: EXCLUDE_ASSIGNEES value is not \"$expect\""
		fail=1
		return
	fi

	echo "OK: $name"
}

write_ca_values "$tmpdir/ca-empty.yaml" "[]"
write_ca_values "$tmpdir/ca-one.yaml" "[releaser]"
write_ca_values "$tmpdir/ca-two.yaml" "[releaser, claude]"
write_ca_values "$tmpdir/ca-absent.yaml" ""

run_ca_case ca-empty "$tmpdir/ca-empty.yaml" empty
run_ca_case ca-absent "$tmpdir/ca-absent.yaml" empty
run_ca_case ca-one "$tmpdir/ca-one.yaml" "releaser"
run_ca_case ca-two "$tmpdir/ca-two.yaml" "releaser,claude"

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: helm render tests passed"
