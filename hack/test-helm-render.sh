#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Renders helm/templates/agents.yaml and asserts that the optional per-agent
# rerunOnLostClaim field renders whenever it is present — including `false` —
# and not at all when unset.
#
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

# Agent PVC: rendered only for a job agent with volumeMountPath, and always
# carries helm.sh/resource-policy: keep so a render that drops it cannot prune it.

# run_pvc_case <case> <values file> <0|1 expected PVC count>
run_pvc_case() {
	local name="$1"
	local values="$2"
	local expect="$3"
	local out="$tmpdir/$name.out"

	"$helm_bin" template x "$CHART_DIR" -f "$values" --show-only templates/agents.yaml >"$out"

	local count
	count="$(grep -c '^kind: PersistentVolumeClaim$' "$out" || true)"
	if [ "$count" -ne "$expect" ]; then
		echo "FAIL: $name: expected $expect PersistentVolumeClaim, got $count"
		fail=1
		return
	fi
	if [ "$expect" -eq 1 ] && ! grep -qx '    helm.sh/resource-policy: keep' "$out"; then
		echo "FAIL: $name: PVC missing 'helm.sh/resource-policy: keep'"
		fail=1
		return
	fi

	echo "OK: $name"
}

write_values "$tmpdir/pvc-job.yaml" "volumeMountPath: /data"
write_values "$tmpdir/pvc-service.yaml" "volumeMountPath: /data"
printf '    type: service\n' >>"$tmpdir/pvc-service.yaml"

run_pvc_case pvc-job "$tmpdir/pvc-job.yaml" 1
run_pvc_case pvc-no-mount "$tmpdir/unset.yaml" 0
run_pvc_case pvc-service "$tmpdir/pvc-service.yaml" 0

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: helm render tests passed"
