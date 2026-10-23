#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Renders the chart and asserts its conditional per-agent fields and the
# sandbox/egress-proxy branches:
#   - rerunOnLostClaim renders whenever present — including `false` — and not at all when unset
#   - jobNamespace renders quoted when set, and not at all when unset or empty, including
#     the per-agent scoping case (two agents, only one sets it)
#   - the sandbox namespace, Kafka peer, PVC and egress-proxy cases each pin their branch
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

# write_values <file> <rerunOnLostClaim line or empty> [extra agent field(s)]
# Extra fields are appended one per line, flow-style, so a multi-line field can
# never break the 4-space indentation of the agents[] entry.
write_values() {
	local file="$1"
	local rr_line="$2"
	local extra="${3:-}"
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
		if [ -n "$extra" ]; then
			while IFS= read -r line; do
				if [ -n "$line" ]; then
					printf '    %s\n' "$line"
				fi
			done <<<"$extra"
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

# agents[].jobNamespace: rendered (quoted) when set, absent when the key is empty
# or unset — an install that sets nothing must render exactly as before.
write_values "$tmpdir/jobns.yaml" "" "jobNamespace: dev-agents-sandbox"
write_values "$tmpdir/jobns-empty.yaml" "" 'jobNamespace: ""'

jobns_out="$tmpdir/jobns-set.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns.yaml" --show-only templates/agents.yaml >"$jobns_out"
if ! grep -qx '  jobNamespace: "dev-agents-sandbox"' "$jobns_out"; then
	echo 'FAIL: job-namespace: missing the quoted jobNamespace line'
	fail=1
else
	echo "OK: job-namespace"
fi

jobns_empty_out="$tmpdir/jobns-empty.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns-empty.yaml" --show-only templates/agents.yaml >"$jobns_empty_out"
jobns_empty_count="$(grep -c '^kind: Config$' "$jobns_empty_out" || true)"
if [ "$jobns_empty_count" -ne 1 ]; then
	echo "FAIL: job-namespace-empty: expected exactly 1 Config document, got $jobns_empty_count"
	fail=1
elif grep -q 'jobNamespace' "$jobns_empty_out"; then
	echo "FAIL: job-namespace-empty: an empty jobNamespace still rendered a line"
	fail=1
else
	echo "OK: job-namespace-empty"
fi

jobns_unset_out="$tmpdir/jobns-unset.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" --show-only templates/agents.yaml >"$jobns_unset_out"
jobns_unset_count="$(grep -c '^kind: Config$' "$jobns_unset_out" || true)"
if [ "$jobns_unset_count" -ne 1 ]; then
	echo "FAIL: job-namespace-unset: expected exactly 1 Config document, got $jobns_unset_count"
	fail=1
elif grep -q 'jobNamespace' "$jobns_unset_out"; then
	echo "FAIL: job-namespace-unset: unexpected jobNamespace line"
	fail=1
else
	echo "OK: job-namespace-unset"
fi

# Two agents, only ONE setting jobNamespace: exactly one line, on that agent's
# Config. This is the case that catches a template reading .Values instead of
# $agent — such a read renders the value onto both Configs (or onto neither).
cat >"$tmpdir/jobns-mixed.yaml" <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
agents:
  - name: ns-agent
    enabled: true
    assignee: ns
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: dev-agents-sandbox
  - name: plain-agent
    enabled: true
    assignee: plain
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF
jobns_mixed_out="$tmpdir/jobns-mixed.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/jobns-mixed.yaml" --show-only templates/agents.yaml >"$jobns_mixed_out"
jobns_mixed_docs="$(grep -c '^kind: Config$' "$jobns_mixed_out" || true)"
jobns_mixed_lines="$(grep -c 'jobNamespace:' "$jobns_mixed_out" || true)"
if [ "$jobns_mixed_docs" -ne 2 ]; then
	echo "FAIL: job-namespace-mixed: expected 2 Config documents, got $jobns_mixed_docs"
	fail=1
elif [ "$jobns_mixed_lines" -ne 1 ]; then
	echo "FAIL: job-namespace-mixed: expected exactly 1 jobNamespace line, got $jobns_mixed_lines"
	fail=1
elif ! awk 'BEGIN{RS="---"} index($0,"kind: Config") && index($0,"  name: ns-agent\n") && index($0,"  jobNamespace: \"dev-agents-sandbox\"\n"){found=1} END{exit(found?0:1)}' "$jobns_mixed_out"; then
	echo "FAIL: job-namespace-mixed: the jobNamespace line is not on ns-agent's Config"
	fail=1
elif awk 'BEGIN{RS="---"} index($0,"kind: Config") && index($0,"  name: plain-agent\n") && index($0,"jobNamespace"){found=1} END{exit(found?0:1)}' "$jobns_mixed_out"; then
	echo "FAIL: job-namespace-mixed: plain-agent's Config carries a jobNamespace line"
	fail=1
else
	echo "OK: job-namespace-mixed"
fi

# doc_has <file> <kind> <name> <namespace("" for cluster-scoped)>
# Assert the identity of a rendered object, not a tally: a dropped object can
# still reach the same `kind:` count as another object of that kind.
doc_has() {
	local file="$1"
	local kind="$2"
	local name="$3"
	local ns="$4"
	awk -v K="$kind" -v N="$name" -v NS="$ns" '
		BEGIN{RS="---"}
		index($0,"kind: " K "\n") && index($0,"  name: " N "\n") &&
		(NS=="" || index($0,"  namespace: " NS "\n")) {found=1}
		END{exit(found?0:1)}' "$file"
}

POLICY_OUT=""
# run_policy_case <case> <values file> [extra helm args...]
# Renders the WHOLE chart into $POLICY_OUT. The caller adds its own assertions
# and prints the OK line, so a failed assertion cannot follow a premature OK.
run_policy_case() {
	local name="$1"
	local values="$2"
	shift 2
	POLICY_OUT="$tmpdir/policy-$name.out"
	"$helm_bin" template x "$CHART_DIR" -f "$values" "$@" >"$POLICY_OUT"
}

# policy_doc <file> <policy name> — print that NetworkPolicy document.
policy_doc() {
	awk -v N="$2" 'BEGIN{RS="---"} index($0,"kind: NetworkPolicy") && index($0,"  name: " N "\n"){printf "%s", $0}' "$1"
}

# Agent PVC: rendered only for a job agent with volumeMountPath, and always
# carries helm.sh/resource-policy: keep so a render that drops it cannot prune it.

# run_pvc_case <case> <values file> <expected PVCs> <expected Secrets> <namespace(s)>
# The namespace may be a comma-separated list when the objects are rendered into
# more than one namespace (sandbox storage copies). Totals alone cannot express
# "one of each here and one of each there".
run_pvc_case() {
	local name="$1"
	local values="$2"
	local expect_pvc="$3"
	local expect_secret="$4"
	local ns="$5"
	local out="$tmpdir/$name.out"

	"$helm_bin" template x "$CHART_DIR" -f "$values" --show-only templates/agents.yaml >"$out"

	local pvc_count secret_count
	pvc_count="$(grep -c '^kind: PersistentVolumeClaim$' "$out" || true)"
	secret_count="$(grep -c '^kind: Secret$' "$out" || true)"
	if [ "$pvc_count" -ne "$expect_pvc" ] || [ "$secret_count" -ne "$expect_secret" ]; then
		echo "FAIL: $name: expected $expect_pvc PVC / $expect_secret Secret, got $pvc_count / $secret_count"
		fail=1
		return
	fi
	if [ "$expect_pvc" -gt 0 ] && ! grep -qx '    helm.sh/resource-policy: keep' "$out"; then
		echo "FAIL: $name: PVC missing 'helm.sh/resource-policy: keep'"
		fail=1
		return
	fi

	local one
	for one in ${ns//,/ }; do
		if [ "$expect_pvc" -gt 0 ] && ! doc_has "$out" PersistentVolumeClaim rr-agent "$one"; then
			echo "FAIL: $name: missing PersistentVolumeClaim rr-agent in $one"
			fail=1
			return
		fi
		if [ "$expect_secret" -gt 0 ] && ! doc_has "$out" Secret rr-agent "$one"; then
			echo "FAIL: $name: missing Secret rr-agent in $one"
			fail=1
			return
		fi
	done

	echo "OK: $name"
}

write_values "$tmpdir/pvc-job.yaml" "volumeMountPath: /data"
write_values "$tmpdir/pvc-service.yaml" "volumeMountPath: /data"
printf '    type: service\n' >>"$tmpdir/pvc-service.yaml"

run_pvc_case pvc-job "$tmpdir/pvc-job.yaml" 1 0 test
run_pvc_case pvc-no-mount "$tmpdir/unset.yaml" 0 0 test
run_pvc_case pvc-service "$tmpdir/pvc-service.yaml" 0 0 test

# sandbox.renderAgentStorage: the chart-rendered Secret and PVC are rendered
# twice — once in the control namespace (the executor reads the Secret there)
# and once in the sandbox namespace.
storage_fields=$'secretEnv: {SENTRY_DSN: dummy}\nvolumeMountPath: /data'
write_values "$tmpdir/storage-default.yaml" "" "$storage_fields"
write_values "$tmpdir/storage-enabled.yaml" "" "$storage_fields"
printf 'sandbox:\n  renderAgentStorage: true\n' >>"$tmpdir/storage-enabled.yaml"
# executor.enabled=false must suppress the sandbox copies, like every other
# sandbox object — the copies would land in a namespace the chart does not create.
sed 's/^executor:$/executor:\n  enabled: false/' "$tmpdir/storage-enabled.yaml" >"$tmpdir/storage-disabled.yaml"

run_pvc_case storage-default "$tmpdir/storage-default.yaml" 1 1 test
run_pvc_case storage-enabled "$tmpdir/storage-enabled.yaml" 2 2 "test,test-agents-sandbox"
run_pvc_case storage-disabled "$tmpdir/storage-disabled.yaml" 1 1 test

# run_sandbox_case <env>
# The spec 047 build-time ACs, in CI. The prompts' greps live in markdown and never
# run here, so without this a regression back to a three-destination policy — the
# exact shape this chart was amended to replace — would merge green.
run_sandbox_case() {
	local env="$1"

	run_policy_case "sandbox-$env" "$tmpdir/unset.yaml" \
		--set "namespace=$env" --set executor.existingSecret=agent-secret
	local out="$POLICY_OUT"

	# Assert each object by kind + name + namespace. Counting `kind:` lines does
	# NOT work here and spec 047 AC3 says so: executor-rbac.yaml already renders a
	# Role and RoleBinding, so a dropped sandbox Namespace or Role still reaches a
	# count of 4. Assert the identity, not the tally.
	local doc_ok=1
	doc_has "$out" Namespace "${env}-agents-sandbox" "" || {
		echo "FAIL: sandbox-$env: missing Namespace ${env}-agents-sandbox"; doc_ok=0; }
	doc_has "$out" NetworkPolicy "agent-sandbox-egress" "${env}-agents-sandbox" || {
		echo "FAIL: sandbox-$env: missing NetworkPolicy agent-sandbox-egress in ${env}-agents-sandbox"; doc_ok=0; }
	doc_has "$out" Role "agent-task-executor-sandbox" "${env}-agents-sandbox" || {
		echo "FAIL: sandbox-$env: missing Role agent-task-executor-sandbox in ${env}-agents-sandbox"; doc_ok=0; }
	doc_has "$out" RoleBinding "agent-task-executor-sandbox" "${env}-agents-sandbox" || {
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

# --- sandbox values -------------------------------------------------------
# Every case below sets at least one `sandbox.*` value; the default render above
# is the regression lock that says no install is renamed.

# Explicit sandbox name. The Namespace, NetworkPolicy and sandbox RoleBinding
# move with it and no document keeps the derived name — this covers the blast
# radius of agent.sandboxNamespace (the egress proxy, its allowlist and the
# sandbox RBAC resolve through the same helper).
run_policy_case sandbox-explicit "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.namespace=dev-sandbox --set executor.existingSecret=agent-secret
out="$POLICY_OUT"
if ! doc_has "$out" Namespace dev-sandbox ""; then
	echo "FAIL: sandbox-explicit: missing Namespace dev-sandbox"
	fail=1
elif ! doc_has "$out" NetworkPolicy agent-sandbox-egress dev-sandbox; then
	echo "FAIL: sandbox-explicit: NetworkPolicy did not move to dev-sandbox"
	fail=1
elif ! doc_has "$out" RoleBinding agent-task-executor-sandbox dev-sandbox; then
	echo "FAIL: sandbox-explicit: sandbox RoleBinding did not move to dev-sandbox"
	fail=1
elif grep -q 'namespace: dev-agents-sandbox' "$out"; then
	echo "FAIL: sandbox-explicit: a document still carries the derived sandbox namespace"
	fail=1
else
	echo "OK: sandbox-explicit"
fi

# In-cluster Kafka: namespace + pod selector + port, and no nodeport address.
cat >"$tmpdir/kafka-incluster.yaml" <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  existingSecret: agent-secret
sandbox:
  kafka:
    namespace: kafka
    podLabels:
      strimzi.io/cluster: sun
    servicePort: 9092
EOF
run_policy_case kafka-incluster "$tmpdir/kafka-incluster.yaml"
out="$POLICY_OUT"
kafka_doc="$(policy_doc "$out" agent-sandbox-egress)"
# RS splits on the peer marker, so this is the single Kafka peer, not the doc.
kafka_peer="$(awk 'BEGIN{RS="    - to:"} /metadata.name: kafka/{print; exit}' <<<"$kafka_doc")"
if ! grep -q 'namespaceSelector:' <<<"$kafka_peer" ||
	! grep -q 'podSelector:' <<<"$kafka_peer" ||
	! grep -q 'strimzi.io/cluster: sun' <<<"$kafka_peer" ||
	! grep -q 'port: 9092' <<<"$kafka_peer"; then
	echo "FAIL: kafka-incluster: Kafka peer is missing its namespace/pod selector or port"
	fail=1
elif grep -q '32159' <<<"$kafka_doc"; then
	echo "FAIL: kafka-incluster: policy still contains the nodeport address"
	fail=1
else
	echo "OK: kafka-incluster"
fi

# Without podLabels the peer must carry no podSelector at all (an empty one
# would render as `podSelector: {}` and select every pod).
cat >"$tmpdir/kafka-incluster-nolabels.yaml" <<'EOF'
namespace: dev
executor:
  kafkaBrokers: kafka:9092
  existingSecret: agent-secret
sandbox:
  kafka:
    namespace: kafka
    servicePort: 9092
EOF
run_policy_case kafka-incluster-nolabels "$tmpdir/kafka-incluster-nolabels.yaml"
kafka_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
kafka_peer="$(awk 'BEGIN{RS="    - to:"} /metadata.name: kafka/{print; exit}' <<<"$kafka_doc")"
if [ -z "$kafka_peer" ]; then
	echo "FAIL: kafka-incluster-nolabels: no kafka peer extracted — the assertion would pass vacuously"
	fail=1
elif grep -q 'podSelector:' <<<"$kafka_peer"; then
	echo "FAIL: kafka-incluster-nolabels: peer carries a podSelector without podLabels"
	fail=1
else
	echo "OK: kafka-incluster-nolabels"
fi

# A namespace without a servicePort must fail the render. A silent `port: 0`
# produces a policy that matches nothing and looks healthy.
if err="$("$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.kafka.namespace=kafka 2>&1)"; then
	echo "FAIL: kafka-missing-port: render succeeded with sandbox.kafka.servicePort unset"
	fail=1
elif grep -q 'sandbox.kafka.servicePort is required' <<<"$err"; then
	echo "OK: kafka-missing-port"
else
	echo "FAIL: kafka-missing-port: render failed without the expected message: $err"
	fail=1
fi

# A non-integer servicePort must fail too. `abc` is truthy, so a falsy-only
# guard lets it through and renders `port: abc` — a named port that matches
# nothing, the same silent no-op as `port: 0`.
if err="$("$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.kafka.namespace=kafka \
	--set sandbox.kafka.servicePort=abc 2>&1)"; then
	echo "FAIL: kafka-nonint-port: render succeeded with a non-integer servicePort"
	fail=1
elif grep -q 'sandbox.kafka.servicePort is required' <<<"$err"; then
	echo "OK: kafka-nonint-port"
else
	echo "FAIL: kafka-nonint-port: render failed without the expected message: $err"
	fail=1
fi

# sandbox.namespace equal to the control namespace must fail: the chart would
# render a second Namespace object for a namespace that already exists, and
# helm install then aborts with "already exists".
if err="$("$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.namespace=dev 2>&1)"; then
	echo "FAIL: sandbox-ns-collision: render succeeded with sandbox.namespace equal to namespace"
	fail=1
elif grep -q 'sandbox.namespace must differ from namespace' <<<"$err"; then
	echo "OK: sandbox-ns-collision"
else
	echo "FAIL: sandbox-ns-collision: render failed without the expected message: $err"
	fail=1
fi

# The default branch stays the nuke nodeports: exactly three /32 cidrs on 32159
# in agent-sandbox-egress (count cidrs — egress-proxy-egress adds a fourth
# ipBlock to the same render).
run_policy_case kafka-default "$tmpdir/unset.yaml" \
	--set namespace=dev --set executor.existingSecret=agent-secret
kafka_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
cidr32="$(grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32' <<<"$kafka_doc" || true)"
if [ "$cidr32" -ne 3 ] || ! grep -q 'port: 32159' <<<"$kafka_doc"; then
	echo "FAIL: kafka-default: expected 3 nodeport /32 cidrs and port 32159, got cidr32=$cidr32"
	fail=1
else
	echo "OK: kafka-default"
fi

# Router instance override: the peer selects the literal, not the derived name.
run_policy_case router-override "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.routerInstance=claude-code-router \
	--set executor.existingSecret=agent-secret
router_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
if ! grep -q 'app.kubernetes.io/instance: claude-code-router$' <<<"$router_doc"; then
	echo "FAIL: router-override: router peer does not select the overridden instance"
	fail=1
elif grep -q 'claude-code-router-dev' <<<"$router_doc"; then
	echo "FAIL: router-override: derived instance name still present"
	fail=1
else
	echo "OK: router-override"
fi

# Optional vault-API peer: present with the value, absent without it.
run_policy_case vault-api "$tmpdir/unset.yaml" \
	--set namespace=dev --set sandbox.vaultApiApp=vault-api-openclaw \
	--set executor.existingSecret=agent-secret
vault_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
vault_peer="$(awk 'BEGIN{RS="    - to:"} /app: vault-api-openclaw/{print; exit}' <<<"$vault_doc")"
if ! grep -q 'app: vault-api-openclaw' <<<"$vault_peer" ||
	! grep -q 'port: 9090' <<<"$vault_peer"; then
	echo "FAIL: vault-api: missing vault-api-openclaw peer on 9090"
	fail=1
elif grep -q 'app: vault-api-openclaw' <<<"$(policy_doc "$tmpdir/policy-kafka-default.out" agent-sandbox-egress)"; then
	echo "FAIL: vault-api: peer rendered without sandbox.vaultApiApp"
	fail=1
else
	echo "OK: vault-api"
fi

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
		--set egressProxy.injectAgentEnv=true \
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
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set egressProxy.injectAgentEnv=true >"$proxy_out"
if grep -q 'HTTPS_PROXY: http://egress-proxy\.test-agents-sandbox\.svc\.cluster\.local:8888' "$proxy_out"; then
	echo "OK: proxy-env url"
else
	echo "FAIL: proxy-env url: HTTPS_PROXY does not point at the test sandbox proxy"
	fail=1
fi

# Injection is OFF by default: until spec 002 moves Jobs into the sandbox, every
# agent runs in the control namespace and the ingress policy would deny the proxy.
proxy_default_out="$tmpdir/proxy-default.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" >"$proxy_default_out"
if grep -q 'HTTPS_PROXY' "$proxy_default_out"; then
	echo "FAIL: proxy-default: proxy env injected without egressProxy.injectAgentEnv"
	fail=1
else
	echo "OK: proxy-default (no env injected)"
fi

# ...and the proxy objects themselves must still render with injection off.
for obj in 'name: egress-proxy$' 'name: egress-proxy-ingress' 'name: egress-proxy-egress'; do
	if grep -q "$obj" "$proxy_default_out"; then
		echo "OK: proxy-default renders $obj"
	else
		echo "FAIL: proxy-default: missing $obj (proxy objects must render with injection off)"
		fail=1
	fi
done

# A quoted "false" must not enable injection — the fail-safe direction.
quoted_out="$tmpdir/proxy-quoted.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set-string egressProxy.injectAgentEnv=false >"$quoted_out"
if grep -q 'HTTPS_PROXY' "$quoted_out"; then
	echo "FAIL: proxy-quoted: injectAgentEnv=\"false\" enabled injection"
	fail=1
else
	echo "OK: proxy-quoted (quoted false stays off)"
fi

# A type: service agent never gets proxy env, even when injection is on.
printf 'namespace: test\nexecutor:\n  kafkaBrokers: kafka:9092\n  existingSecret: agent-secret\nagents:\n  - name: svc-only\n    type: service\n    enabled: true\n    assignee: b\n    image: bborbe/agent-claude\n    heartbeat: 5m\n    taskTypes: [llm]\n    triggerPhases: [planning]\n    triggerStatuses: [in_progress]\n' >"$tmpdir/svc-only.yaml"
svc_out="$tmpdir/proxy-svc.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/svc-only.yaml" \
	--set egressProxy.injectAgentEnv=true >"$svc_out"
if grep -q 'HTTPS_PROXY' "$svc_out"; then
	echo "FAIL: proxy-svc: service agent received proxy env"
	fail=1
else
	echo "OK: proxy-svc (no env for type: service)"
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
