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

# run_sandbox_case <env>
# The spec 047 build-time ACs, in CI. The prompts' greps live in markdown and never
# run here, so without this a regression back to a three-destination policy — the
# exact shape this chart was amended to replace — would merge green.
run_sandbox_case() {
	local env="$1"
	local out="$tmpdir/sandbox-$env.out"

	"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
		--set "namespace=$env" --set executor.existingSecret=agent-secret >"$out"

	# Assert each object by kind + name + namespace. Counting `kind:` lines does
	# NOT work here and spec 047 AC3 says so: executor-rbac.yaml already renders a
	# Role and RoleBinding, so a dropped sandbox Namespace or Role still reaches a
	# count of 4. Assert the identity, not the tally.
	local doc_ok=1
	doc_has() { # kind name namespace("" for cluster-scoped)
		awk -v K="$1" -v N="$2" -v NS="$3" '
			BEGIN{RS="---"}
			index($0,"kind: " K "\n") && index($0,"  name: " N "\n") &&
			(NS=="" || index($0,"  namespace: " NS "\n")) {found=1}
			END{exit(found?0:1)}' "$out"
	}
	doc_has Namespace "${env}-agents-sandbox" "" || {
		echo "FAIL: sandbox-$env: missing Namespace ${env}-agents-sandbox"; doc_ok=0; }
	doc_has NetworkPolicy "agent-sandbox-egress" "${env}-agents-sandbox" || {
		echo "FAIL: sandbox-$env: missing NetworkPolicy agent-sandbox-egress in ${env}-agents-sandbox"; doc_ok=0; }
	doc_has Role "agent-task-executor-sandbox" "${env}-agents-sandbox" || {
		echo "FAIL: sandbox-$env: missing Role agent-task-executor-sandbox in ${env}-agents-sandbox"; doc_ok=0; }
	doc_has RoleBinding "agent-task-executor-sandbox" "${env}-agents-sandbox" || {
		echo "FAIL: sandbox-$env: missing RoleBinding agent-task-executor-sandbox in ${env}-agents-sandbox"; doc_ok=0; }
	# The binding must cross namespaces: the subject SA lives in the CONTROL
	# namespace, not the sandbox. Checked inside the sandbox RoleBinding document
	# only — the chart renders a second RoleBinding (agent-task-executor) whose
	# subject is also in the control namespace and would mask a regression here.
	if ! awk -v NS="${env}" '
		BEGIN{RS="---"}
		index($0,"kind: RoleBinding") && index($0,"  name: agent-task-executor-sandbox\n") &&
		index($0,"    namespace: " NS "\n") {found=1}
		END{exit(found?0:1)}' "$out"; then
		echo "FAIL: sandbox-$env: RoleBinding subject is not in the control namespace ${env}"; doc_ok=0
	fi
	if [ "$doc_ok" -ne 1 ]; then
		fail=1
		return
	fi

	local sel
	# The router instance label must be THIS render's env. An alternation over
	# (dev|prod) would let a hardcoded `-dev` literal in the prod template count 8
	# and pass — which is the exact regression the prod render exists to catch.
	sel="$(grep -cE "^[[:space:]]+app(\.kubernetes\.io/instance)?: (sentry-proxy|claude-code-router-${env}|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy))\$" "$out" || true)"
	if [ "$sel" -ne 8 ]; then
		echo "FAIL: sandbox-$env: expected 8 in-cluster egress peer selectors, got $sel"
		fail=1
		return
	fi

	local cidr32 cidr24 port9092
	cidr32="$(grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32' "$out" || true)"
	cidr24="$(grep -c 'cidr: 192.168.178.0/24' "$out" || true)"
	port9092="$(grep -c 'port: 9092' "$out" || true)"
	if [ "$cidr32" -ne 3 ] || [ "$cidr24" -ne 0 ] || [ "$port9092" -ne 0 ]; then
		echo "FAIL: sandbox-$env: cidr/32=$cidr32 (want 3) cidr/24=$cidr24 (want 0) port9092=$port9092 (want 0)"
		fail=1
		return
	fi

	# pushgateway must select the `monitoring` namespace — a typo there matches
	# nothing and the peer goes inert without any other check noticing.
	if ! awk -v NS="monitoring" '
		BEGIN{RS="---"}
		index($0,"app: pushgateway") && index($0,"metadata.name: " NS "\n") {found=1}
		END{exit(found?0:1)}' "$out"; then
		echo "FAIL: sandbox-$env: pushgateway peer does not select namespace monitoring"; fail=1; return
	fi

	# Egress-only: declaring nothing for ingress keeps it unrestricted, which is
	# intended — but the policy must not silently grow an ingress rule.
	if ! grep -A40 'name: agent-sandbox-egress' "$out" | grep -q 'policyTypes'; then
		echo "FAIL: sandbox-$env: policy has no policyTypes"; fail=1; return
	fi
	if grep -A40 'name: agent-sandbox-egress' "$out" | grep -q 'ingress:'; then
		echo "FAIL: sandbox-$env: policy declares ingress, expected egress-only"; fail=1; return
	fi
	if ! grep -q 'helm.sh/resource-policy: keep' "$out"; then
		echo "FAIL: sandbox-$env: Namespace missing helm.sh/resource-policy: keep"; fail=1; return
	fi

	local peer
	peer="$(awk '/- to:/{t=NR} /namespaceSelector:/{n=NR} /app: sentry-proxy/{print (n>t)?"SAME_PEER_OK":"SPLIT_PEER_FAIL"}' "$out")"
	if [ "$peer" != "SAME_PEER_OK" ]; then
		echo "FAIL: sandbox-$env: sentry peer does not keep both selectors in one to: entry ($peer)"
		fail=1
		return
	fi

	echo "OK: sandbox-$env"
}

run_sandbox_case dev
run_sandbox_case prod

# executor.enabled=false must suppress every sandbox object (spec AC3).
disabled_out="$tmpdir/sandbox-disabled.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set namespace=dev --set executor.enabled=false >"$disabled_out"
if grep -q 'agents-sandbox' "$disabled_out"; then
	echo "FAIL: sandbox-disabled: sandbox objects render with executor.enabled=false"
	fail=1
else
	echo "OK: sandbox-disabled"
fi

# spec 048: agent.proxyEnv derives the NO_PROXY broker hosts from
# executor.kafkaBrokers. A naive split on ':' yields the literal "tls" for the
# documented tls:// form, which silently routes Kafka through the proxy.
check_proxy_env() {
	local case_name="$1" brokers="$2" want="$3"
	local out="$tmpdir/proxy-env-$case_name.out"
	"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
		--set "executor.kafkaBrokers=$brokers" >"$out"
	if grep -q "$want" "$out"; then
		echo "OK: proxy-env $case_name"
	else
		echo "FAIL: proxy-env $case_name: missing $want"
		fail=1
	fi
}

check_proxy_env plain 'kafka:9092' \
	'NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,kafka$'
check_proxy_env tls 'tls://kafka.example:9093' \
	'NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,kafka\.example$'
check_proxy_env ipv6 '[::1]:9092' \
	'NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,::1$'

# The env must point at this install's proxy.
proxy_out="$tmpdir/proxy-env.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" >"$proxy_out"
if grep -q 'HTTPS_PROXY: http://egress-proxy\.test-agents-sandbox\.svc\.cluster\.local:8888' "$proxy_out"; then
	echo "OK: proxy-env url"
else
	echo "FAIL: proxy-env url: HTTPS_PROXY does not point at the test sandbox proxy"
	fail=1
fi

# executor.enabled=false must suppress every proxy object, not just the string.
proxy_off_out="$tmpdir/proxy-disabled.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set executor.enabled=false >"$proxy_off_out"
for obj in 'name: egress-proxy$' 'name: egress-proxy-allowlist' 'name: egress-proxy-ingress' 'kind: PodDisruptionBudget'; do
	if grep -q "$obj" "$proxy_off_out"; then
		echo "FAIL: proxy-disabled: rendered $obj"
		fail=1
	else
		echo "OK: proxy-disabled ($obj)"
	fi
done

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: helm render tests passed"
