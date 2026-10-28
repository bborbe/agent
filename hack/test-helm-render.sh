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

# A non-positive nodePorts element must fail the render: `port: 0` (or a
# negative) matches nothing and looks healthy, the same silent no-op as a
# missing servicePort.
if err="$("$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set namespace=dev --set executor.existingSecret=agent-secret \
	--set 'sandbox.kafka.nodePorts={32159,0}' 2>&1)"; then
	echo "FAIL: kafka-nonpositive-nodeport: render succeeded with a non-positive nodePorts element"
	fail=1
elif grep -q 'sandbox.kafka.nodePorts must contain only positive integers' <<<"$err"; then
	echo "OK: kafka-nonpositive-nodeport"
else
	echo "FAIL: kafka-nonpositive-nodeport: render failed without the expected message: $err"
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

# sandbox.kafka.nodePorts: every listed nodeport gets its own ports entry in
# the LEGACY inline peer (sandbox.yaml). A Kafka client bootstraps on one
# nodeport and follows advertised metadata to another, so admitting only the
# bootstrap port rejects the connection the client actually uses.
run_policy_case kafka-nodeports-legacy "$tmpdir/unset.yaml" \
	--set namespace=dev --set executor.existingSecret=agent-secret \
	--set 'sandbox.kafka.nodePorts={32159,32706}'
kafka_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
kafka_peer="$(awk 'BEGIN{RS="    - to:"} /cidr: 192\.168\.178\.41\/32/{print; exit}' <<<"$kafka_doc")"
if [ -z "$kafka_peer" ]; then
	echo "FAIL: kafka-nodeports-legacy: no nodeport Kafka peer extracted — the assertion would pass vacuously"
	fail=1
elif ! grep -q 'port: 32159' <<<"$kafka_peer" || ! grep -q 'port: 32706' <<<"$kafka_peer"; then
	echo "FAIL: kafka-nodeports-legacy: Kafka peer does not carry both nodeports"
	fail=1
else
	echo "OK: kafka-nodeports-legacy"
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

# sandbox.peerNamespace: the non-agent peers follow it, the router does NOT.
# This is the case that catches a template which moved every peer — the router
# is a control-plane component, so a peer set that follows peerNamespace for the
# router too would deny sandboxed Jobs their model endpoint.
run_policy_case peer-namespace "$tmpdir/unset.yaml" \
	--set namespace=agent --set sandbox.namespace=dev-agents-sandbox \
	--set sandbox.peerNamespace=dev --set executor.existingSecret=agent-secret
peer_ns_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
peer_ns_sentry="$(awk 'BEGIN{RS="    - to:"} /app: sentry-proxy/{print; exit}' <<<"$peer_ns_doc")"
peer_ns_router="$(awk 'BEGIN{RS="    - to:"} /app.kubernetes.io\/instance: claude-code-router-agent/{print; exit}' <<<"$peer_ns_doc")"
peer_ns_vault="$(awk 'BEGIN{RS="    - to:"} /app: vault-obsidian-personal/{print; exit}' <<<"$peer_ns_doc")"
peer_ns_frontend="$(awk 'BEGIN{RS="    - to:"} /app: frontend-gateway/{print; exit}' <<<"$peer_ns_doc")"
peer_ns_control="$(grep -c 'metadata.name: agent$' <<<"$peer_ns_doc" || true)"
if [ -z "$peer_ns_sentry" ] || [ -z "$peer_ns_router" ] || [ -z "$peer_ns_vault" ] || [ -z "$peer_ns_frontend" ]; then
	echo "FAIL: peer-namespace: a peer could not be extracted — the assertion would pass vacuously"
	fail=1
elif ! grep -q 'metadata.name: dev$' <<<"$peer_ns_sentry" ||
	! grep -q 'metadata.name: dev$' <<<"$peer_ns_vault" ||
	! grep -q 'metadata.name: dev$' <<<"$peer_ns_frontend"; then
	echo "FAIL: peer-namespace: a non-agent peer does not select sandbox.peerNamespace (dev)"
	fail=1
elif ! grep -q 'metadata.name: agent$' <<<"$peer_ns_router"; then
	echo "FAIL: peer-namespace: the router peer does not select the control namespace (agent)"
	fail=1
elif [ "$peer_ns_control" -ne 1 ]; then
	echo "FAIL: peer-namespace: expected exactly 1 control-namespace peer (the router), got $peer_ns_control"
	fail=1
else
	echo "OK: peer-namespace"
fi

# Unset peerNamespace keeps the old render: every in-cluster peer, router
# included, selects the control namespace.
run_policy_case peer-namespace-default "$tmpdir/unset.yaml" \
	--set namespace=agent --set sandbox.namespace=dev-agents-sandbox \
	--set executor.existingSecret=agent-secret
peer_ns_default_doc="$(policy_doc "$POLICY_OUT" agent-sandbox-egress)"
peer_ns_default_control="$(grep -c 'metadata.name: agent$' <<<"$peer_ns_default_doc" || true)"
if [ "$peer_ns_default_control" -ne 7 ]; then
	echo "FAIL: peer-namespace-default: expected 7 control-namespace peers, got $peer_ns_default_control"
	fail=1
else
	echo "OK: peer-namespace-default"
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

# The catalog's Service peers, namespace-qualified, sit between the fixed
# prefix and the broker hosts (namespace "test" from the fixture).
peer_hosts='claude-code-router-test\.test,pushgateway\.monitoring,sentry-proxy\.test,vault-obsidian-personal\.test,vault-obsidian-openclaw\.test,vault-obsidian-trading\.test,frontend-gateway\.test,frontend-strategy\.test'
check_proxy_env plain 'kafka:9092' \
	"NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,$peer_hosts,kafka\$"
check_proxy_env tls 'tls://kafka.example:9093' \
	"NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,$peer_hosts,kafka\.example\$"
check_proxy_env ipv6 '[::1]:9092' \
	"NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,$peer_hosts,::1\$"
# Repeated brokers (singular executor + executors[] falling back to it) render once.
check_proxy_env dedup 'kafka:9092\,kafka:9093' \
	"NO_PROXY: 127\.0\.0\.1,localhost,\.svc\.cluster\.local,$peer_hosts,kafka\$"

# NO_PROXY short names: agents call peers as `<service>.<namespace>`
# (`pushgateway.monitoring`, `sentry-proxy.<peerNamespace>`), which the
# `.svc.cluster.local` suffix does not match — the proxy then refuses them with
# `403 Filtered`. The peer namespace must follow sandbox.peerNamespace, and no
# entry may be a bare suffix other than `.svc.cluster.local`: `.prod` would let
# every public host ending in it bypass the proxy.
short_out="$tmpdir/proxy-short-names.out"
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" \
	--set egressProxy.injectAgentEnv=true \
	--set sandbox.peerNamespace=prod \
	--set sandbox.vaultApiApp=vault-api-openclaw >"$short_out"
short_value=$(grep -m1 -E '^ +NO_PROXY: ' "$short_out" | sed -E 's/^ +NO_PROXY: //' || true)
if [ -z "$short_value" ]; then
	echo "FAIL: proxy-short-names: no NO_PROXY rendered with injectAgentEnv=true"
	fail=1
fi
for want in pushgateway.monitoring sentry-proxy.prod vault-obsidian-personal.prod \
	vault-obsidian-openclaw.prod vault-obsidian-trading.prod frontend-gateway.prod \
	frontend-strategy.prod vault-api-openclaw.prod claude-code-router-test.test; do
	if printf ',%s,' "$short_value" | grep -qF ",$want,"; then
		echo "OK: proxy-short-names has $want"
	else
		echo "FAIL: proxy-short-names: NO_PROXY missing $want (got: $short_value)"
		fail=1
	fi
done
bare=$(printf '%s\n' "$short_value" | tr ',' '\n' | grep '^\.' | grep -vx '\.svc\.cluster\.local' || true)
if [ -n "$bare" ]; then
	echo "FAIL: proxy-short-names: bare suffix entries in NO_PROXY: $bare"
	fail=1
else
	echo "OK: proxy-short-names (no bare suffix besides .svc.cluster.local)"
fi

# Drift guard: every Service peer the default egress policy admits by `app:`
# label must also bypass the proxy, so a catalog peer added to the policy
# without a NO_PROXY host fails here rather than as a 403 in production.
drift_apps=$(awk '/^---/{p=0} /^  name: agent-sandbox-egress$/{p=1} p && /^          [ ]*app: /{print $2}' "$short_out" | grep -vx 'egress-proxy' | sort -u)
if [ -z "$drift_apps" ]; then
	echo "FAIL: proxy-drift: no app peers found in agent-sandbox-egress (selector moved?)"
	fail=1
fi
for app in $drift_apps; do
	if printf ',%s,' "$short_value" | grep -qF ",$app."; then
		echo "OK: proxy-drift $app"
	else
		echo "FAIL: proxy-drift: egress peer app=$app has no NO_PROXY host"
		fail=1
	fi
done

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

# --- sandbox domains ---
# One block per behaviour the domains feature must keep. Every assertion names
# an object by kind+name+namespace, and every selector assertion greps a
# rendered selector line, never a bare catalog word: a policy comment
# containing the word would make a negative assertion pass vacuously.

cat >"$tmpdir/domains.yaml" <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
egressProxy:
  injectAgentEnv: true
sandbox:
  proxyNamespace: agent-sandbox
  renderAgentStorage: true
  kafka:
    namespace: kafka
    servicePort: 9092
    podLabels:
      strimzi.io/cluster: sun
  domains:
    agent-sandbox: {}
    development-sandbox:
      peers: [router, pushgateway, sentry-proxy, kafka]
    sentry-sandbox:
      peers: [router, pushgateway, kafka, vault-personal]
    trading-sandbox:
      peers: [router, pushgateway, frontend-gateway, frontend-strategy]
    research-sandbox:
      peers: [router, vault-trading]
agents:
  - name: mapped-agent
    enabled: true
    assignee: mapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: trading-sandbox
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
  - name: unmapped-agent
    enabled: true
    assignee: unmapped
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
EOF

run_policy_case domains "$tmpdir/domains.yaml"
out="$POLICY_OUT"

# Exactly one Namespace per declared domain, each carrying
# resource-policy: keep. The keep check runs per Namespace document: a bare
# `grep -c` over the whole render also counts every PVC.
ns_names="$(awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  name: /){sub(/^  name: /,"",a[i]); print a[i]; break}}}' "$out" | sort | paste -sd, -)"
want_ns="agent-sandbox,development-sandbox,research-sandbox,sentry-sandbox,trading-sandbox"
if [ "$ns_names" != "$want_ns" ]; then
	echo "FAIL: domains-namespaces: Namespace set is '$ns_names', want '$want_ns'"
	fail=1
elif ! awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){c++; if (index($0,"helm.sh/resource-policy: keep")==0) bad=1} END{exit(bad||c!=5?1:0)}' "$out"; then
	echo "FAIL: domains-namespaces: a Namespace lacks helm.sh/resource-policy: keep (or the count is not 5)"
	fail=1
else
	echo "OK: domains-namespaces"
fi

# The pod quota must exist in the namespace the Job runs in, not only in the
# control namespace. The executor resolves the quota there — `podQuotaFor` is
# handed the job namespace — so a missing copy makes the read succeed against
# nothing and the limiter falls back to maxConcurrentJobs, silently, with no
# error anywhere. Assert the identity per agent per domain: the control
# namespace renders the same kind, so a bare count would pass on the wrong
# objects.
domains_quotas_ok=1
for d in agent-sandbox development-sandbox research-sandbox sentry-sandbox trading-sandbox; do
	for a in mapped-agent unmapped-agent; do
		doc_has "$out" ResourceQuota "$a" "$d" || {
			echo "FAIL: domains-quotas: missing ResourceQuota $a in $d"; domains_quotas_ok=0; }
	done
done
# The control-namespace copy must survive: an agent whose jobNamespace is unset
# spawns into the control namespace and reads its quota there.
for a in mapped-agent unmapped-agent; do
	doc_has "$out" ResourceQuota "$a" test || {
		echo "FAIL: domains-quotas: missing control-namespace ResourceQuota $a"; domains_quotas_ok=0; }
done
# Both fixture agents set neither `concurrency` nor `maxConcurrentJobs`, so every
# quota must carry the documented fallback of 1.
quota_hard="$(awk 'BEGIN{RS="---"} index($0,"kind: ResourceQuota\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^    pods: /){sub(/^    pods: /,"",a[i]); gsub(/"/,"",a[i]); print a[i]; break}}}' "$out" | sort -u | paste -sd, -)"
if [ "$quota_hard" != "1" ]; then
	echo "FAIL: domains-quotas: hard pod values are '$quota_hard', want '1' for agents that set no concurrency"
	domains_quotas_ok=0
fi
if [ "$domains_quotas_ok" = "1" ]; then echo "OK: domains-quotas"; else fail=1; fi

# executor.enabled=false must suppress the sandbox quotas like every other
# sandbox object: the chart creates no sandbox namespace to hold them, and a
# quota applied to a namespace that does not exist fails the whole apply.
run_policy_case quotas-disabled "$tmpdir/domains.yaml" --set executor.enabled=false
disabled_out="$POLICY_OUT"
disabled_quotas="$(awk 'BEGIN{RS="---"} index($0,"kind: ResourceQuota\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  namespace: /){sub(/^  namespace: /,"",a[i]); print a[i]; break}}}' "$disabled_out" | sort -u | paste -sd, -)"
if [ "$disabled_quotas" != "test" ]; then
	echo "FAIL: quotas-disabled: ResourceQuota namespaces are '$disabled_quotas', want only 'test'"
	fail=1
else
	echo "OK: quotas-disabled"
fi

# The `concurrency -> maxConcurrentJobs -> 1` fallback chain, and the LEGACY
# (no `sandbox.domains`) render path, in one fixture. Without declared domains
# the sandbox quota must land in the single `<ns>-agents-sandbox` namespace —
# a regression dropping the copy there would otherwise merge green, because the
# domains fixture above never exercises that branch.
cat >"$tmpdir/quota-precedence.yaml" <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
agents:
  - name: conc-only
    enabled: true
    assignee: conc
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    concurrency: 2
    maxConcurrentJobs: 9
  - name: max-only
    enabled: true
    assignee: max
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    maxConcurrentJobs: 4
  - name: neither
    enabled: true
    assignee: neither
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
EOF

run_policy_case quota-precedence "$tmpdir/quota-precedence.yaml"
prec_out="$POLICY_OUT"

# Extract "<name> <pods>" per ResourceQuota document. Matching the bare word
# `pods` would also hit the comment text in the template, so this reads the
# rendered field under `hard:` only.
quota_pods() {
	awk 'BEGIN{RS="---"} index($0,"kind: ResourceQuota\n"){
		n=split($0,a,"\n"); name=""; pods="";
		for(i=1;i<=n;i++){
			if(a[i] ~ /^  name: /){name=a[i]; sub(/^  name: /,"",name)}
			if(a[i] ~ /^    pods: /){pods=a[i]; sub(/^    pods: /,"",pods); gsub(/"/,"",pods)}
		}
		if(name!="" && pods!="") print name" "pods
	}' "$1"
}

prec_ok=1
# Legacy path: no domains declared, so the sandbox namespace is <ns>-agents-sandbox.
for a in conc-only max-only neither; do
	doc_has "$prec_out" ResourceQuota "$a" test-agents-sandbox || {
		echo "FAIL: quota-precedence: missing legacy-path ResourceQuota $a in test-agents-sandbox"; prec_ok=0; }
done
# concurrency wins, then maxConcurrentJobs, then 1 — the exact chain this PR adds.
for pair in "conc-only 2" "max-only 4" "neither 1"; do
	# IFS-read, not `set -- $pair`: zsh does not word-split an unquoted
	# expansion, so `$1` would take the whole string and `$2` would stay empty.
	IFS=' ' read -r qname qwant <<<"$pair"
	if quota_pods "$prec_out" | grep -qx "$qname $qwant"; then
		echo "OK: quota-precedence-$qname"
	else
		echo "FAIL: quota-precedence-$qname: want pods $qwant, got '$(quota_pods "$prec_out" | grep "^$qname " | head -1)'"
		prec_ok=0
	fi
done
if [ "$prec_ok" != "1" ]; then fail=1; fi

# A quota that is read but rendered into the wrong place is the exact failure
# this chart change exists to fix, so pin the RBAC half too: the sandbox-scoped
# Role must carry resourcequotas, and the control-namespace Role must keep it.
rbac_ok=1
for d in agent-sandbox development-sandbox research-sandbox sentry-sandbox trading-sandbox; do
	if ! awk -v NS="$d" '
		BEGIN{RS="---"}
		index($0,"kind: Role") && index($0,"  name: agent-task-executor-sandbox\n") &&
		index($0,"  namespace: " NS "\n") && index($0,"resources: [resourcequotas]") {found=1}
		END{exit(found?0:1)}' "$out"; then
		echo "FAIL: domains-rbac: Role agent-task-executor-sandbox in $d lacks resourcequotas"
		rbac_ok=0
	fi
done
if ! awk '
	BEGIN{RS="---"}
	index($0,"kind: Role") && index($0,"  name: agent-task-executor\n") &&
	index($0,"  namespace: test\n") && index($0,"resources: [resourcequotas]") {found=1}
	END{exit(found?0:1)}' "$out"; then
	echo "FAIL: domains-rbac: control-namespace Role agent-task-executor lost resourcequotas"
	rbac_ok=0
fi
if [ "$rbac_ok" = "1" ]; then echo "OK: domains-rbac"; else fail=1; fi

# One `<domain>-egress` policy per domain, narrowed to its own peers.
domains_policies_ok=1
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
	doc_has "$out" NetworkPolicy "$d-egress" "$d" || {
		echo "FAIL: domains-policies: missing NetworkPolicy $d-egress in $d"; domains_policies_ok=0; }
done
trading_policy="$(policy_doc "$out" trading-sandbox-egress)"
dev_policy="$(policy_doc "$out" development-sandbox-egress)"
if [ -z "$trading_policy" ] || [ -z "$dev_policy" ]; then
	echo "FAIL: domains-policies: a policy could not be extracted — the assertions would pass vacuously"
	fail=1
elif [ "$(grep -c 'app: sentry-proxy' <<<"$trading_policy" || true)" -ne 0 ]; then
	echo "FAIL: domains-policies: trading-sandbox-egress carries the sentry-proxy peer"
	fail=1
elif [ "$(grep -c 'app: vault-obsidian-' <<<"$trading_policy" || true)" -ne 0 ]; then
	echo "FAIL: domains-policies: trading-sandbox-egress carries a vault peer"
	fail=1
elif ! grep -q 'app: agent' <<<"$trading_policy"; then
	echo "FAIL: domains-policies: trading-sandbox-egress lacks the app: agent pod selector"
	fail=1
elif grep -q 'ingress:' <<<"$trading_policy"; then
	echo "FAIL: domains-policies: a domain policy declares ingress, expected egress-only"
	fail=1
elif ! grep -q 'policyTypes:' <<<"$trading_policy" || grep -q -- '- Ingress' <<<"$trading_policy"; then
	echo "FAIL: domains-policies: a domain policy does not declare policyTypes: [Egress] only"
	fail=1
elif ! grep -q 'app: sentry-proxy' <<<"$dev_policy" || ! grep -q 'metadata.name: kafka' <<<"$dev_policy"; then
	echo "FAIL: domains-policies: development-sandbox-egress lacks sentry-proxy or the kafka peer"
	fail=1
elif [ "$domains_policies_ok" -ne 1 ]; then
	fail=1
else
	echo "OK: domains-policies"
fi

# Egress side: every domain reaches the proxy across the namespace
# boundary: namespaceSelector AND podSelector in the same `to:` entry.
domains_proxy_peer_ok=1
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
	doc="$(policy_doc "$out" "$d-egress")"
	peer="$(awk 'BEGIN{RS="    - to:"} /app: egress-proxy/{print; exit}' <<<"$doc")"
	if [ -z "$peer" ] || ! grep -q 'namespaceSelector:' <<<"$peer" ||
		! grep -q 'metadata.name: agent-sandbox' <<<"$peer" ||
		! grep -q 'podSelector:' <<<"$peer" || ! grep -q 'port: 8888' <<<"$peer"; then
		echo "FAIL: domains-proxy-peer: $d-egress proxy peer lacks the namespace+pod selectors in one to: entry"
		domains_proxy_peer_ok=0
	fi
done
if [ "$domains_proxy_peer_ok" -eq 1 ]; then echo "OK: domains-proxy-peer"; else fail=1; fi

# Proxy objects: exactly one of each, in proxyNamespace.
domains_proxy_objects_ok=1
for spec in "Deployment egress-proxy" "Service egress-proxy" "PodDisruptionBudget egress-proxy" "ConfigMap egress-proxy-allowlist" "NetworkPolicy egress-proxy-ingress" "NetworkPolicy egress-proxy-egress"; do
	obj_kind="${spec%% *}"; obj_name="${spec#* }"
	obj_count="$(awk -v K="$obj_kind" -v N="$obj_name" 'BEGIN{RS="---"} index($0,"kind: " K "\n") && index($0,"  name: " N "\n"){c++} END{print c+0}' "$out")"
	if [ "$obj_count" -ne 1 ]; then
		echo "FAIL: domains-proxy-objects: $obj_kind/$obj_name renders $obj_count times, want 1"
		domains_proxy_objects_ok=0
	elif ! doc_has "$out" "$obj_kind" "$obj_name" agent-sandbox; then
		echo "FAIL: domains-proxy-objects: $obj_kind/$obj_name is not in agent-sandbox"
		domains_proxy_objects_ok=0
	fi
done
ingress_policy="$(policy_doc "$out" egress-proxy-ingress)"
ingress_bad=0
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
	grep -q "kubernetes.io/metadata.name: $d" <<<"$ingress_policy" || {
		echo "FAIL: domains-proxy-objects: egress-proxy-ingress does not admit $d"; ingress_bad=1; }
done
# A namespace-wide from: peer (namespaceSelector without podSelector) turns the proxy
# into an open proxy for every pod in that namespace — spec Security invariant.
if ! awk 'BEGIN{RS="    - from:"} /namespaceSelector:/{if (index($0,"podSelector:")==0) bad=1} END{exit(bad?1:0)}' <<<"$ingress_policy"; then
	echo "FAIL: domains-proxy-objects: egress-proxy-ingress carries a namespace-wide peer"
	ingress_bad=1
fi
if [ "$domains_proxy_objects_ok" -ne 1 ] || [ "$ingress_bad" -ne 0 ] || [ "$(grep -c 'app: agent' <<<"$ingress_policy" || true)" -ne 5 ]; then
	echo "FAIL: domains-proxy-objects: ingress policy does not admit exactly the five domains"
	fail=1
else
	echo "OK: domains-proxy-objects"
fi

# Egress-proxy security posture. The capability list and the log volume are the two
# halves of one change: the emptyDir is what lets the root entrypoint write its log
# without a file-permission bypass on the container that enforces egress. Nothing
# pinned either before, so re-adding DAC_OVERRIDE or dropping the mount would merge
# green. Scope the capability assertion to the `add:` line — the template's own
# comment names DAC_OVERRIDE, and a bare grep for the word matches that comment.
proxy_dep="$(awk 'BEGIN{RS="---"} index($0,"kind: Deployment\n") && index($0,"  name: egress-proxy\n"){print; exit}' "$out")"
proxy_posture_ok=1
if ! grep -qE '^[[:space:]]+add: \[SETUID, SETGID\][[:space:]]*$' <<<"$proxy_dep"; then
	echo "FAIL: egress-proxy-posture: capability list is not exactly [SETUID, SETGID]"
	proxy_posture_ok=0
fi
if grep -qE '^[[:space:]]+add: \[.*DAC_OVERRIDE' <<<"$proxy_dep"; then
	echo "FAIL: egress-proxy-posture: DAC_OVERRIDE is granted"
	proxy_posture_ok=0
fi
if ! grep -q 'name: proxy-log' <<<"$proxy_dep"; then
	echo "FAIL: egress-proxy-posture: the proxy-log volume is missing"
	proxy_posture_ok=0
fi
if ! grep -q 'mountPath: /var/log/tinyproxy' <<<"$proxy_dep"; then
	echo "FAIL: egress-proxy-posture: /var/log/tinyproxy is not mounted"
	proxy_posture_ok=0
fi
if [ "$proxy_posture_ok" -ne 1 ]; then fail=1; else echo "OK: egress-proxy-posture"; fi

# Executor RBAC per domain, subject in the control namespace.
domains_rbac_ok=1
for d in agent-sandbox development-sandbox sentry-sandbox trading-sandbox research-sandbox; do
	doc_has "$out" Role agent-task-executor-sandbox "$d" || {
		echo "FAIL: domains-rbac: missing Role agent-task-executor-sandbox in $d"; domains_rbac_ok=0; }
	doc_has "$out" RoleBinding agent-task-executor-sandbox "$d" || {
		echo "FAIL: domains-rbac: missing RoleBinding agent-task-executor-sandbox in $d"; domains_rbac_ok=0; }
	awk -v NS="$d" 'BEGIN{RS="---"} index($0,"kind: RoleBinding\n") && index($0,"  name: agent-task-executor-sandbox\n") && index($0,"  namespace: " NS "\n") && index($0,"    namespace: test\n"){f=1} END{exit(f?0:1)}' "$out" || {
		echo "FAIL: domains-rbac: the $d binding subject is not in the control namespace test"; domains_rbac_ok=0; }
done
if [ "$domains_rbac_ok" -eq 1 ]; then echo "OK: domains-rbac"; else fail=1; fi

# Storage copies follow jobNamespace, and the proxy env points at
# proxyNamespace. Render agents.yaml only.
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/domains.yaml" --show-only templates/agents.yaml >"$tmpdir/domains-agents.out"
agents_out="$tmpdir/domains-agents.out"
domains_storage_ok=1
for obj_kind in Secret PersistentVolumeClaim; do
	doc_has "$agents_out" "$obj_kind" mapped-agent trading-sandbox || {
		echo "FAIL: domains-storage: missing $obj_kind mapped-agent in trading-sandbox"; domains_storage_ok=0; }
	for d in agent-sandbox development-sandbox sentry-sandbox research-sandbox; do
		if doc_has "$agents_out" "$obj_kind" mapped-agent "$d"; then
			echo "FAIL: domains-storage: mapped-agent $obj_kind copied into $d"; domains_storage_ok=0
		fi
		if doc_has "$agents_out" "$obj_kind" unmapped-agent "$d" || doc_has "$agents_out" "$obj_kind" unmapped-agent trading-sandbox; then
			echo "FAIL: domains-storage: unmapped-agent got a sandbox $obj_kind copy"; domains_storage_ok=0
		fi
	done
done
proxy_env_count="$(grep -c 'egress-proxy\.agent-sandbox\.svc\.cluster\.local:8888' "$agents_out" || true)"
foreign_proxy_count="$(grep -cE 'egress-proxy\.(development|sentry|trading|research)-sandbox' "$agents_out" || true)"
if [ "$domains_storage_ok" -ne 1 ] || [ "$proxy_env_count" -ne 8 ] || [ "$foreign_proxy_count" -ne 0 ]; then
	echo "FAIL: domains-storage/proxy-env: storage_ok=$domains_storage_ok proxy_env=$proxy_env_count foreign=$foreign_proxy_count (want 8/0)"
	fail=1
else
	echo "OK: domains-storage"
fi

# Third branch: a jobNamespace naming an undeclared namespace gets no copy.
cat >"$tmpdir/domains-stray.yaml" <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
sandbox:
  proxyNamespace: agent-sandbox
  renderAgentStorage: true
  domains:
    agent-sandbox: {}
    trading-sandbox:
      peers: [router]
agents:
  - name: stray-agent
    enabled: true
    assignee: stray
    image: bborbe/agent-claude
    heartbeat: 5m
    taskTypes: [llm]
    triggerPhases: [planning]
    triggerStatuses: [in_progress]
    jobNamespace: undeclared-sandbox
    secretEnv: {SENTRY_DSN: dummy}
    volumeMountPath: /data
EOF
"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/domains-stray.yaml" --show-only templates/agents.yaml >"$tmpdir/domains-stray.out"
stray_out="$tmpdir/domains-stray.out"
stray_bad=0
for obj_kind in Secret PersistentVolumeClaim; do
	stray_count="$(awk -v K="$obj_kind" 'BEGIN{RS="---"} index($0,"kind: " K "\n"){c++} END{print c+0}' "$stray_out")"
	if [ "$stray_count" -ne 1 ]; then
		echo "FAIL: domains-storage-stray: $obj_kind renders $stray_count times, want 1 (control namespace only)"; stray_bad=1
	fi
	if doc_has "$stray_out" "$obj_kind" stray-agent undeclared-sandbox; then
		echo "FAIL: domains-storage-stray: $obj_kind stray-agent copied into the undeclared namespace"; stray_bad=1
	fi
done
if [ "$stray_bad" -ne 0 ]; then fail=1; else echo "OK: domains-storage-stray"; fi

# The peer set comes from values, including a domain nothing else names.
cat >"$tmpdir/domains-peers.yaml" <<'EOF'
namespace: test
executor:
  kafkaBrokers: kafka:9092
  sentry:
    dsn: dummy
sandbox:
  proxyNamespace: agent-sandbox
  domains:
    agent-sandbox: {}
    research-sandbox:
      peers: [router, vault-trading]
    union-sandbox: {}
EOF
run_policy_case domains-peers "$tmpdir/domains-peers.yaml"
peers_out="$POLICY_OUT"
research_policy="$(policy_doc "$peers_out" research-sandbox-egress)"
union_policy="$(policy_doc "$peers_out" union-sandbox-egress)"
peers_bad=0
if [ -z "$research_policy" ] || [ -z "$union_policy" ]; then
	echo "FAIL: domains-peers: a policy could not be extracted — the assertions would pass vacuously"
	peers_bad=1
fi
grep -q 'app: vault-obsidian-trading' <<<"$research_policy" || {
	echo "FAIL: domains-peers: research-sandbox-egress lacks the rendered vault-trading selector"; peers_bad=1; }
grep -q 'app.kubernetes.io/instance: claude-code-router-test' <<<"$research_policy" || {
	echo "FAIL: domains-peers: research-sandbox-egress lacks the router instance"; peers_bad=1; }
grep -q 'k8s-app: kube-dns' <<<"$research_policy" || {
	echo "FAIL: domains-peers: research-sandbox-egress lacks DNS"; peers_bad=1; }
for absent in 'app: sentry-proxy' 'app: pushgateway' 'app: frontend-' 'metadata.name: kafka' 'vault-trading'; do
	if grep -q -- "$absent" <<<"$research_policy"; then
		echo "FAIL: domains-peers: research-sandbox-egress carries '$absent'"; peers_bad=1
	fi
done
union_sel="$(grep -cE "^[[:space:]]+app(\.kubernetes\.io/instance)?: (sentry-proxy|claude-code-router-test|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy))\$" <<<"$union_policy" || true)"
union_cidr="$(grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32' <<<"$union_policy" || true)"
if [ "$union_sel" -ne 8 ]; then
	echo "FAIL: domains-peers: union-sandbox-egress has $union_sel in-cluster selectors, want 8"; peers_bad=1
fi
if [ "$union_cidr" -ne 3 ] || ! grep -q 'port: 32159' <<<"$union_policy"; then
	echo "FAIL: domains-peers: union-sandbox-egress lacks the three nodeport /32 cidrs on 32159"; peers_bad=1
fi
if [ "$peers_bad" -ne 0 ]; then fail=1; else echo "OK: domains-peers"; fi

# The same nodePorts list must reach the helper-rendered peer in a domain
# policy, not just the legacy inline one — both call sites share
# agent.sandboxKafkaNodePorts, and this is the case that catches one of them
# falling back to a hardcoded port. union-sandbox omits `peers`, so it renders
# the full union including kafka on the nodeport branch.
run_policy_case kafka-nodeports-domains "$tmpdir/domains-peers.yaml" \
	--set namespace=dev --set executor.existingSecret=agent-secret \
	--set 'sandbox.kafka.nodePorts={32159,32706}'
kafka_doc="$(policy_doc "$POLICY_OUT" union-sandbox-egress)"
kafka_peer="$(awk 'BEGIN{RS="    - to:"} /cidr: 192\.168\.178\.41\/32/{print; exit}' <<<"$kafka_doc")"
if [ -z "$kafka_peer" ]; then
	echo "FAIL: kafka-nodeports-domains: no nodeport Kafka peer extracted — the assertion would pass vacuously"
	fail=1
elif ! grep -q 'port: 32159' <<<"$kafka_peer" || ! grep -q 'port: 32706' <<<"$kafka_peer"; then
	echo "FAIL: kafka-nodeports-domains: union-sandbox-egress Kafka peer does not carry both nodeports"
	fail=1
else
	echo "OK: kafka-nodeports-domains"
fi

# A single domain defaults the proxy namespace to that domain.
run_policy_case domains-single "$tmpdir/unset.yaml" --set 'sandbox.domains.only-sandbox.peers={router}'
single_ns="$(awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  name: /){sub(/^  name: /,"",a[i]); print a[i]; break}}}' "$POLICY_OUT" | sort | paste -sd, -)"
if ! doc_has "$POLICY_OUT" Deployment egress-proxy only-sandbox; then
	echo "FAIL: domains-single: egress-proxy Deployment is not in the only domain"
	fail=1
elif ! doc_has "$POLICY_OUT" NetworkPolicy egress-proxy-ingress only-sandbox; then
	echo "FAIL: domains-single: egress-proxy-ingress is not in the only domain"
	fail=1
elif grep -q 'test-agents-sandbox' <<<"$single_ns"; then
	echo "FAIL: domains-single: the legacy derived namespace still renders ($single_ns)"
	fail=1
else
	echo "OK: domains-single"
fi

# Domains mode with the executor disabled renders no domain at all.
run_policy_case domains-disabled "$tmpdir/unset.yaml" --set 'sandbox.domains.dev-sandbox.peers={router}' --set executor.enabled=false
if grep -q 'dev-sandbox' "$POLICY_OUT"; then
	echo "FAIL: domains-disabled: a dev-sandbox object renders with executor.enabled=false"
	fail=1
elif grep -q -- '-egress' "$POLICY_OUT"; then
	echo "FAIL: domains-disabled: an egress NetworkPolicy renders with executor.enabled=false"
	fail=1
else
	echo "OK: domains-disabled"
fi

# The fail-fast cases: each bad values file must fail the render by name.
domains_failfast() {
	local name="$1" fragment="$2"
	shift 2
	local err
	if err="$("$helm_bin" template x "$CHART_DIR" -f "$tmpdir/unset.yaml" "$@" 2>&1)"; then
		echo "FAIL: $name: render succeeded, want a failure naming $fragment"
		fail=1
	elif grep -q -- "$fragment" <<<"$err"; then
		echo "OK: $name"
	else
		echo "FAIL: $name: render failed without the expected message: $err"
		fail=1
	fi
}

domains_failfast domains-ns-collision 'must not name the control namespace' \
	--set 'sandbox.domains.test.peers={router}'
domains_failfast domains-both-keys 'mutually exclusive' \
	--set sandbox.namespace=dev-sandbox --set 'sandbox.domains.dev-sandbox.peers={router}'
domains_failfast domains-unknown-peer 'unknown peer' \
	--set 'sandbox.domains.dev-sandbox.peers={not-a-peer}'
domains_failfast domains-proxy-required 'sandbox.proxyNamespace is required' \
	--set 'sandbox.domains.a-sandbox.peers={router}' --set 'sandbox.domains.b-sandbox.peers={router}'
domains_failfast domains-vault-api-value 'vaultApiApp is empty' \
	--set 'sandbox.domains.dev-sandbox.peers={vault-api}'
domains_failfast domains-proxy-not-domain 'is not one of the declared' \
	--set sandbox.proxyNamespace=not-a-domain --set 'sandbox.domains.dev-sandbox.peers={router}'
domains_failfast domains-proxy-without-domains 'sandbox.domains is empty' \
	--set sandbox.proxyNamespace=agent-sandbox

# No install changes its render unless it opts in: with no `sandbox.domains`
# key the object identities — (kind, metadata.name, metadata.namespace) — are
# the ones this chart rendered before domains existed. Identities carry no
# labels, so a chart-version bump cannot perturb the comparison; that is why
# the check is on triples and not on bytes. A linked worktree's object
# database lives outside the container mount, so `git archive "$BASE"` cannot
# run in-container; this is the container-executable half, and it pins the
# objects a domain install would be most likely to rename or move. The full
# 23-object triple set was diffed against the branch point by hand when the
# change landed and was identical.
run_policy_case domains-legacy-identity "$tmpdir/unset.yaml"
legacy_ns="$(awk 'BEGIN{RS="---"} index($0,"kind: Namespace\n"){n=split($0,a,"\n"); for(i=1;i<=n;i++){if(a[i] ~ /^  name: /){sub(/^  name: /,"",a[i]); print a[i]; break}}}' "$POLICY_OUT" | sort | paste -sd, -)"
if [ "$legacy_ns" != "test-agents-sandbox" ]; then
	echo "FAIL: domains-legacy-identity: Namespace set is '$legacy_ns', want 'test-agents-sandbox'"
	fail=1
elif ! doc_has "$POLICY_OUT" NetworkPolicy agent-sandbox-egress test-agents-sandbox; then
	echo "FAIL: domains-legacy-identity: missing NetworkPolicy agent-sandbox-egress"
	fail=1
elif ! doc_has "$POLICY_OUT" NetworkPolicy egress-proxy-egress test-agents-sandbox; then
	echo "FAIL: domains-legacy-identity: missing NetworkPolicy egress-proxy-egress"
	fail=1
else
	echo "OK: domains-legacy-identity"
fi

# --- sandbox.renderPolicies ---
# `false` drops the egress layer (every NetworkPolicy, the whole egress proxy,
# the proxy env) and nothing else: Namespaces, the executor's sandbox RBAC and
# the per-agent Secret/PVC copies stay in the same namespaces. Unset and `true`
# must render byte-identically to each other, which is the regression lock that
# says no install changes unless it opts out.

# count_kind <file> <kind> — number of rendered documents of that kind.
count_kind() {
	grep -c "^kind: $2\$" "$1" || true
}

# assert_no_egress_layer <case> <file>
assert_no_egress_layer() {
	local name="$1" out="$2" n
	n="$(count_kind "$out" NetworkPolicy)"
	if [ "$n" -ne 0 ]; then
		echo "FAIL: $name: $n NetworkPolicy objects render with sandbox.renderPolicies=false, want 0"
		return 1
	fi
	n="$(count_kind "$out" PodDisruptionBudget)"
	if [ "$n" -ne 0 ]; then
		echo "FAIL: $name: $n PodDisruptionBudget objects render with sandbox.renderPolicies=false, want 0"
		return 1
	fi
	if doc_has "$out" Deployment egress-proxy "" || doc_has "$out" Service egress-proxy "" ||
		doc_has "$out" ConfigMap egress-proxy-allowlist ""; then
		echo "FAIL: $name: an egress-proxy object renders with sandbox.renderPolicies=false"
		return 1
	fi
	if grep -qE '^    (HTTPS?_PROXY|https?_proxy|NO_PROXY|no_proxy): ' "$out"; then
		echo "FAIL: $name: proxy env is injected into a Config with sandbox.renderPolicies=false"
		return 1
	fi
	return 0
}

# Domains mode: every domain Namespace, the per-domain executor RBAC and the
# mapped agent's storage copy in its own domain survive; the egress layer does not.
run_policy_case render-policies-false-domains "$tmpdir/domains.yaml" --set sandbox.renderPolicies=false
out="$POLICY_OUT"
rp_ok=1
assert_no_egress_layer render-policies-false-domains "$out" || rp_ok=0
for ns in agent-sandbox development-sandbox research-sandbox sentry-sandbox trading-sandbox; do
	if ! doc_has "$out" Namespace "$ns" ""; then
		echo "FAIL: render-policies-false-domains: missing Namespace $ns"; rp_ok=0
	fi
	if ! doc_has "$out" Role agent-task-executor-sandbox "$ns"; then
		echo "FAIL: render-policies-false-domains: missing Role agent-task-executor-sandbox in $ns"; rp_ok=0
	fi
done
if ! doc_has "$out" Secret mapped-agent trading-sandbox || ! doc_has "$out" PersistentVolumeClaim mapped-agent trading-sandbox; then
	echo "FAIL: render-policies-false-domains: mapped-agent storage copy is not in trading-sandbox"; rp_ok=0
fi
if grep -q 'test-agents-sandbox' "$out"; then
	echo "FAIL: render-policies-false-domains: storage fell back to the single test-agents-sandbox namespace"; rp_ok=0
fi
# Storage is untouched by the flag: same Secret and PVC count as the policies-on render.
run_policy_case render-policies-true-domains "$tmpdir/domains.yaml"
for kind in Secret PersistentVolumeClaim Namespace; do
	if [ "$(count_kind "$out" "$kind")" -ne "$(count_kind "$POLICY_OUT" "$kind")" ]; then
		echo "FAIL: render-policies-false-domains: $kind count $(count_kind "$out" "$kind") differs from the policies-on render $(count_kind "$POLICY_OUT" "$kind")"; rp_ok=0
	fi
done
if [ "$rp_ok" -eq 1 ]; then
	echo "OK: render-policies-false-domains"
else
	fail=1
fi

# Legacy single-sandbox mode: the derived namespace and its storage copy survive.
run_policy_case render-policies-false-legacy "$tmpdir/storage-enabled.yaml" --set sandbox.renderPolicies=false
out="$POLICY_OUT"
rp_ok=1
assert_no_egress_layer render-policies-false-legacy "$out" || rp_ok=0
if ! doc_has "$out" Namespace test-agents-sandbox ""; then
	echo "FAIL: render-policies-false-legacy: missing Namespace test-agents-sandbox"; rp_ok=0
fi
if ! doc_has "$out" Secret rr-agent test-agents-sandbox || ! doc_has "$out" PersistentVolumeClaim rr-agent test-agents-sandbox; then
	echo "FAIL: render-policies-false-legacy: rr-agent storage copy is not in test-agents-sandbox"; rp_ok=0
fi
if [ "$rp_ok" -eq 1 ]; then
	echo "OK: render-policies-false-legacy"
else
	fail=1
fi

# Unset and an explicit `true` are the same render, byte for byte, in both modes.
for fixture in domains storage-enabled; do
	run_policy_case "render-policies-unset-$fixture" "$tmpdir/$fixture.yaml"
	unset_out="$POLICY_OUT"
	run_policy_case "render-policies-explicit-true-$fixture" "$tmpdir/$fixture.yaml" --set sandbox.renderPolicies=true
	if ! cmp -s "$unset_out" "$POLICY_OUT"; then
		echo "FAIL: render-policies-identity-$fixture: unset and renderPolicies=true render differently"
		fail=1
	elif [ "$(count_kind "$unset_out" NetworkPolicy)" -eq 0 ]; then
		echo "FAIL: render-policies-identity-$fixture: the default render has no NetworkPolicy"
		fail=1
	else
		echo "OK: render-policies-identity-$fixture"
	fi
done

domains_failfast render-policies-invalid 'sandbox.renderPolicies must be true or false' \
	--set sandbox.renderPolicies=maybe

# defaultForTaskTypes renders only for an agent that declares it. The control
# matters as much as the positive: the resolver reads absence as "no default",
# so a template that always emitted the key would change every Config's spec.
for variant in set unset; do
	{
		printf 'namespace: test\nexecutor:\n  kafkaBrokers: kafka:9092\n  existingSecret: agent-secret\n  sentry:\n    dsn: dummy\n'
		printf 'agents:\n  - name: df-%s\n    enabled: true\n    assignee: df-%s\n' "$variant" "$variant"
		printf '    image: bborbe/agent-claude\n    heartbeat: 5m\n    taskTypes: [healthcheck]\n'
		if [ "$variant" = set ]; then
			printf '    defaultForTaskTypes: [healthcheck]\n'
		fi
	} >"$tmpdir/default-for-task-types-$variant.yaml"

	"$helm_bin" template x "$CHART_DIR" -f "$tmpdir/default-for-task-types-$variant.yaml" \
		--show-only templates/agents.yaml >"$tmpdir/df-$variant.out"
done

if grep -q '^  defaultForTaskTypes:$' "$tmpdir/df-set.out" &&
	grep -q '^    - healthcheck$' "$tmpdir/df-set.out"; then
	echo "OK: default-for-task-types-set renders the declared default"
else
	echo "FAIL: default-for-task-types-set: the declared default is not rendered"
	fail=1
fi

if grep -q 'defaultForTaskTypes' "$tmpdir/df-unset.out"; then
	echo "FAIL: default-for-task-types-unset: the key renders for an agent that does not declare it"
	fail=1
else
	echo "OK: default-for-task-types-unset omits the key"
fi

if [ "$fail" -ne 0 ]; then
	exit 1
fi
echo "OK: helm render tests passed"
