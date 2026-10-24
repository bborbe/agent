---
status: draft
spec: [062-sandbox-domains]
created: "2026-10-24T20:38:48Z"
branch: dark-factory/sandbox-domains
---

# test(helm)+docs: pin the sandbox-domains render and document it

<summary>
- The chart's own render suite proves every acceptance criterion of the domains feature: one Namespace per domain, one policy per domain built from its peer list, the proxy rendered once and reachable from every domain, per-domain executor RBAC, and storage copies that follow `jobNamespace`
- Bad values files fail the render, and the suite proves each failure names the offending key
- The suite also proves the negative direction: a narrowed domain cannot reach another domain's peers, and an install that declares no domains renders the legacy names unchanged
- A single-domain install gets its proxy in that domain without naming it twice
- The chart README documents the new values and the peer catalog
- The values file's stale `jobNamespace` comment and the network-security doc now describe what the chart actually renders
- The chart version and the changelog record the change
</summary>

<objective>
Encode spec `062-sandbox-domains` AC1–AC12 in `hack/test-helm-render.sh`, then update `helm/README.md`, `helm/values.yaml`'s comments, `docs/agent-network-security.md`, `helm/Chart.yaml` and `CHANGELOG.md`. This prompt depends on prompts 2, 3, 4 and 5 having landed.
</objective>

<context>
This is prompt 6 of 6 for spec `062-sandbox-domains`. Read fully before editing:

- `hack/test-helm-render.sh` — the render harness run by `make test` via the `test-helm` target. It already defines `write_values`, `run_case`, `doc_has <file> <kind> <name> <namespace>`, `run_policy_case <case> <values file> [helm args...]` (renders the whole chart into `$POLICY_OUT`), `policy_doc <file> <name>` (prints one NetworkPolicy document by name), and `run_pvc_case`. It accumulates failures in `$fail` and ends with `if [ "$fail" -ne 0 ]; then exit 1; fi`. Your block goes **immediately before that final `if`**, so every helper above is in scope.
- `helm/templates/sandbox.yaml`, `executor-rbac.yaml`, `egress-proxy.yaml`, `egress-proxy-allowlist.yaml`, `agents.yaml` — the rendered output you are asserting. Read the post-prompt-2/3/4/5 versions, not the pre-change ones.
- `helm/templates/_helpers.tpl` — the catalog names and the fail-fast messages, which the error cases grep verbatim.
- `helm/README.md` — `### sandbox` values table and the `### agents (leaf agents — values-driven)` prose, including the sentence "`renderAgentStorage` never follows `jobNamespace` to a different name" near the `jobNamespace` paragraph.
- `helm/values.yaml` — the `sandbox:` block, the commented `agents:` example, and the `jobNamespace` comment lines that end with "`renderAgentStorage` never follows jobNamespace to a different name."
- `docs/agent-network-security.md` — section `## Target topology` (a single data-plane namespace block) and section `## NetworkPolicy reference` (an example policy whose proxy peer has a bare `podSelector`, plus a "Stale" warning below it).
- `helm/Chart.yaml` — `version: 0.22.3`.
- `CHANGELOG.md` — top section is `## v0.127.0`; there is no `## Unreleased`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — entry format and prefixes.

The values file used by most cases (write it exactly as given; the two agents exist so AC5 and AC9 have something to observe — without an `agents:` list the render emits zero Config documents):

```yaml
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
```
</context>

<requirements>
1. **Add a domains test block to `hack/test-helm-render.sh`.** Insert it immediately before the final `if [ "$fail" -ne 0 ]; then` block, with the comment header `# --- sandbox domains (spec 062) ---`. Use only the existing helpers (`run_policy_case`, `policy_doc`, `doc_has`, `$helm_bin`, `$CHART_DIR`, `$tmpdir`, `$fail`) and the `if … then fail=1 else echo OK` shape used by the rest of the file. Every case prints exactly one `OK: <case>` line on success.

   Cases h, i and j below each print `OK: domains-peers`, `OK: domains-single` and `OK: domains-disabled` respectively on success, in the same `if … then fail=1 else echo OK` shape.

   a. Write the values file from `<context>` to `"$tmpdir/domains.yaml"` with a quoted heredoc, then render once:

   ```bash
   run_policy_case domains "$tmpdir/domains.yaml"
   out="$POLICY_OUT"
   ```

   b. **AC1 — namespaces.** Set equality on the five declared names, and `helm.sh/resource-policy: keep` checked per Namespace document (not by a bare `grep -c` over the whole render, which also counts every PVC):

   ```bash
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
   ```

   c. **AC2 — one policy per domain, narrowed by its own peers.** Assert each `<domain>-egress` in its domain by identity, then the positive and negative selector sets. `grep -c` returns exit 1 on zero matches, so capture with `|| true`:

   ```bash
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
   ```

   d. **AC4 (egress side) — every domain reaches the proxy across the namespace boundary.** The `proxy` peer must carry `namespaceSelector` for `agent-sandbox` **and** `podSelector: app: egress-proxy` in the same `to:` entry:

   ```bash
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
   ```

   e. **AC4 (proxy objects) — exactly one of each, in `proxyNamespace`.** Count by kind+name and assert the namespace with `doc_has`:

   ```bash
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
   ```

   f. **AC3 — executor RBAC per domain.** Role and RoleBinding `agent-task-executor-sandbox` in every domain, subject in the control namespace:

   ```bash
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
   ```

   g. **AC5 + AC9 — storage copies and the proxy env.** Render `agents.yaml` once:

   ```bash
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
   ```

   The proxy-env count is 8 because each of the two Configs carries `HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy` and `https_proxy`.

   g2. **AC5 (third branch) — a `jobNamespace` naming an undeclared namespace gets no copy.** A separate values file, so the counts in 1.g stay fixed at 8:

   ```bash
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
   ```

   Then add `domains-storage-stray` to the expected `OK` list in `<verification>`.

   h. **AC10 — the peer set comes from values, including a domain the spec never names.** A second values file:

   ```bash
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
   ```

   Assertions on `research_policy`: contains the **rendered selector** `app: vault-obsidian-trading` and the router instance `app.kubernetes.io/instance: claude-code-router-test`; contains `k8s-app: kube-dns`; contains zero `app: sentry-proxy`, zero `app: pushgateway`, zero `app: frontend-`, zero `metadata.name: kafka`; and contains **zero** occurrences of the bare catalog word `vault-trading` (the selector is the assertion; a catalog word in the document would mean the implementation planted it rather than resolved it). Assertions on `union_policy`: the full union — the same eight in-cluster selector regex the `run_sandbox_case` helper uses (`sentry-proxy|claude-code-router-test|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy)`) counts 8, and the three nodeport `/32` cidrs on 32159 are present (this values file leaves `sandbox.kafka.namespace` unset).

   i. **AC12 — single domain defaults the proxy namespace.** `run_policy_case domains-single "$tmpdir/unset.yaml" --set 'sandbox.domains.only-sandbox.peers={router}'`, then `doc_has "$POLICY_OUT" Deployment egress-proxy only-sandbox` and `doc_has … NetworkPolicy egress-proxy-ingress only-sandbox`, plus: no Namespace named `test-agents-sandbox` (grep the extracted Namespace set).

   j. **AC12 — domains mode with the executor disabled.** `run_policy_case domains-disabled "$tmpdir/unset.yaml" --set 'sandbox.domains.dev-sandbox.peers={router}' --set executor.enabled=false`, then assert `grep -c 'dev-sandbox'` is 0 and no `-egress` NetworkPolicy renders. Do not reuse the existing `sandbox-disabled` case: it greps for `agents-sandbox`, which a domain named `agent-sandbox` does not match.

   k. **AC7 and AC8 and AC11 (a)–(e) — seven fail-fast cases.** Use the existing `if err="$(... 2>&1)"; then … elif grep -q '<expected fragment>' <<<"$err"; then … else … fi` shape, base values `"$tmpdir/unset.yaml"` (namespace `test`):

   | Case | `--set` | expected stderr fragment |
   |---|---|---|
   | `domains-ns-collision` | `sandbox.domains.test.peers={router}` | `must not name the control namespace` |
   | `domains-both-keys` | `sandbox.namespace=dev-sandbox` + `sandbox.domains.dev-sandbox.peers={router}` | `mutually exclusive` |
   | `domains-unknown-peer` | `sandbox.domains.dev-sandbox.peers={not-a-peer}` | `unknown peer` |
   | `domains-proxy-required` | `sandbox.domains.a-sandbox.peers={router}` + `sandbox.domains.b-sandbox.peers={router}` | `sandbox.proxyNamespace is required` |
   | `domains-vault-api-value` | `sandbox.domains.dev-sandbox.peers={vault-api}` | `vaultApiApp is empty` |
   | `domains-proxy-not-domain` | `sandbox.proxyNamespace=not-a-domain` + `sandbox.domains.dev-sandbox.peers={router}` | `is not one of the declared` |
   | `domains-proxy-without-domains` | `sandbox.proxyNamespace=agent-sandbox` | `sandbox.domains is empty` |

   Quote every `--set` value containing braces: `--set 'sandbox.domains.dev-sandbox.peers={router}'`.

   l. **AC6 — no install changes its render unless it opts in.** Add a `domains-legacy-identity` case: render the whole chart from `"$tmpdir/unset.yaml"` and assert the Namespace set is exactly `test-agents-sandbox`, `NetworkPolicy agent-sandbox-egress` exists in it, `NetworkPolicy egress-proxy-egress` exists in it, and no Namespace whose name ends in `-sandbox` other than `test-agents-sandbox` renders. Add a comment above the case: the spec's byte-level evidence is a `(kind, name, namespace)` diff against the fork commit. A linked worktree's object database lives outside the container mount, so `git archive "$BASE"` cannot run in-container; the identity check here is the container-executable half, and the base-commit diff is **not executed by any rung** — record it in the completion report as an open AC6 item (this prompt must not edit `specs/`).

2. **`helm/README.md` — values reference.** In `### sandbox`, add these rows directly after the `sandbox.namespace` row (keep the table's existing column style):

   ```markdown
   | `sandbox.domains` | `{}` | Map of sandbox namespace name to its egress peer set. Non-empty renders one `Namespace` and one `<name>-egress` default-deny policy per entry, and the egress proxy once in `proxyNamespace`. Empty renders today's single sandbox unchanged. Mutually exclusive with `sandbox.namespace` — declaring both fails the render. Each entry takes an optional `peers` list; see **Multiple sandbox domains** below. |
   | `sandbox.domains.<name>.peers` | unset | Destinations this domain may reach, from the fixed catalog `dns`, `router`, `pushgateway`, `sentry-proxy`, `vault-personal`, `vault-openclaw`, `vault-trading`, `frontend-gateway`, `frontend-strategy`, `vault-api`, `kafka`, `proxy`. An unknown name fails the render. `dns` and `proxy` are added unconditionally. **Omitting `peers` renders the full union** — deliberately, so the first apply of a domains install narrows nothing; `peers: []` renders only DNS and the proxy. |
   | `sandbox.proxyNamespace` | `""` | Namespace hosting the single egress proxy (Deployment, Service, PDB, allowlist ConfigMap and both its policies). Must be one of the declared domains; required when more than one domain is declared, defaults to the only domain when exactly one is. Setting it with no `domains` fails the render. |
   ```

   Correct the `sandbox.renderAgentStorage` row: add that with `domains` declared the copy follows each agent's `jobNamespace` and renders only when that names a declared domain; without domains it targets the single sandbox namespace regardless of `jobNamespace`.

   Add a new subsection `### Multiple sandbox domains` directly after the `### sandbox` table and before `### executor`. It must state:
   - the map shape, with a short YAML example using `agent-sandbox` (hosting the proxy) and a `trading-sandbox` with `peers: [router, pushgateway, frontend-gateway, frontend-strategy]`;
   - one `Namespace` (carrying `helm.sh/resource-policy: keep`) and one `<name>-egress` policy per entry, `podSelector app: agent`, egress-only;
   - `dns` and `proxy` are unconditional because DNS is how a pod resolves anything and the proxy is the only route to the public internet — making either opt-in would be an off-switch for egress that fails silently;
   - `egress-proxy-ingress` admits `app: agent` pods from every rendered domain, so a newly declared domain reaches the proxy without a second edit;
   - `proxyNamespace` rules (required for >1, must be declared, defaults for exactly one);
   - storage copies follow `jobNamespace` in domains mode only;
   - `sandbox.namespace` and `domains` are mutually exclusive; the legacy policy name `agent-sandbox-egress` is kept for installs that declare no domains.

3. **`helm/values.yaml` — correct the stale comments.**
   - The commented `jobNamespace` entry in the `agents:` example (the lines ending "`renderAgentStorage` never follows jobNamespace to a different name."): replace that sentence with: in domains mode (`sandbox.domains` declared) `renderAgentStorage` copies the Secret and PVC into the sandbox that this `jobNamespace` names, and only when that name is a declared domain; with no domains the copy still targets the single `sandbox.namespace`.
   - The `renderAgentStorage` comment in the `sandbox:` block: add the same domains-mode sentence.
   - Keep both new `domains` / `proxyNamespace` blocks from prompt 1 unchanged.

4. **`docs/agent-network-security.md` — update the two sections.**
   - `## Target topology`: replace the single data-plane block with a domains shape: the control plane namespace (executor, Config CRDs, Kafka SASL config), N domain namespaces (each with Job pods, a `<domain>-egress` policy and any storage copies), and one proxy namespace (a declared domain) holding the egress-proxy Deployment/Service/allowlist ConfigMap and its two policies. Keep the note that `sandbox.namespace` still names a single sandbox when `sandbox.domains` is empty, and that the derived fallback is `<namespace>-agents-sandbox`.
   - `## NetworkPolicy reference`: the example policy's name becomes `<domain>-egress` (with a note that an install without domains renders `agent-sandbox-egress`), and its **proxy peer must carry both selectors in one `to:` entry**:
     ```yaml
     - to:
         - namespaceSelector:
             matchLabels: { kubernetes.io/metadata.name: agent-sandbox }
           podSelector:
             matchLabels: { app: egress-proxy }
       ports:
         - { protocol: TCP, port: 8888 }
     ```
     Add a sentence explaining why: a bare `podSelector` matches only same-namespace pods, so in every other domain it matches nothing and all internet egress is lost with no render failure. Refresh the "Stale" warning so it no longer claims the reference lacks the router/pushgateway/vault/frontend rules — the reference must match what the chart renders now (dns, the router on 8788, sentry-proxy, pushgateway, three vaults, two frontends, optional vault-api, Kafka in either branch, and the proxy), while keeping the note about the two Kafka branches and the `/24` prohibition. State that `dns` and `proxy` are unconditional in every domain.

5. **`helm/Chart.yaml`.** `version: 0.22.3` → `version: 0.23.0`. Minor bump: a new backwards-compatible values feature. Leave `appVersion` unchanged.

6. **`CHANGELOG.md`.** Add a `## Unreleased` section at the top, above `## v0.127.0`, separated by a blank line, with three bullets in the existing style (read `changelog-guide.md` for the prefix rules; the file's own recent entries are the best style reference):
   - `feat(helm):` a new `sandbox.domains` map renders one Namespace and one `<domain>-egress` default-deny policy per declared sandbox namespace, built from a closed peer catalog; `dns` and `proxy` are unconditional and every domain reaches the single egress proxy across the namespace boundary (the proxy peer carries a namespace selector and a pod selector in the same `to:` entry — without it only the proxy's own domain keeps internet egress); `sandbox.proxyNamespace` names the proxy's namespace and is required when more than one domain is declared; omitting a domain's `peers` renders today's full union, so the first apply narrows nothing; `sandbox.renderAgentStorage` copies each agent's Secret/PVC into the sandbox its `jobNamespace` names when domains are declared; declaring `domains` and `sandbox.namespace` together fails the render. Chart 0.22.3→0.23.0.
   - `test(helm):` render cases for the domains feature — namespaces and their `resource-policy: keep`, per-domain policies and their narrowed peer sets, the proxy rendered once and admitted from every domain, per-domain executor RBAC, storage copies following `jobNamespace`, the proxy env pointing at `proxyNamespace`, a domain name the spec never hardcodes, the full-union default, the single-domain proxy default, the domains-mode `executor.enabled: false` case, and seven fail-fast cases.
   - `docs(helm):` the `sandbox.domains` / `proxyNamespace` values reference, the multiple-domains section, the corrected `jobNamespace`/`renderAgentStorage` comments, and the multi-namespace target topology plus the proxy peer's namespace selector in `docs/agent-network-security.md`.

   Do not add a second `## Unreleased` heading and do not touch the released sections.
</requirements>

<constraints>
- The existing render cases must stay green — including `storage-enabled` (an agent with no `jobNamespace` still gets a copy in the single sandbox) and the four values-driven peer cases (`peer-namespace`, `peer-namespace-default`, `router-override`, `vault-api`).
- Assert by object identity (`kind` + `metadata.name` + `metadata.namespace`), not by per-kind tallies — a dropped object can reach the same `kind:` count as another object of that kind.
- Grep selector lines (`app: …`, `k8s-app: …`, `metadata.name: …`), never bare catalog words: a policy header comment containing the word would make a negative assertion pass vacuously.
- Do NOT commit — dark-factory handles git.
- `docs/dod.md` (the daemon's `validationPrompt`) asks for a `CHANGELOG.md` entry and a `README.md` update. Both are owned by prompt 6 of this spec: report them as deferred, do not edit either file, and do not report them as a blocker for this prompt.
- Do not edit `specs/` in this prompt. The templates and `helm/templates/_helpers.tpl` are owned by prompts 1–5: if a render case reveals a template bug, fix it minimally inside the template that owns the behaviour (never by weakening the assertion), and name the file and the bug in the completion report.
</constraints>

<verification>
```bash
# AC12: the chart's own suite, plus every Go test in the module.
make test
```

`make test` must exit 0. It runs `test-helm` (`bash hack/test-helm-render.sh`) and the Go tests. Then confirm the new cases are actually present and passing:

```bash
bash hack/test-helm-render.sh 2>&1 | grep -E '^(OK|FAIL): (domains|sandbox-domains)' | sort
```

Expected `OK` lines, at minimum: `domains-namespaces`, `domains-policies`, `domains-proxy-peer`, `domains-proxy-objects`, `domains-rbac`, `domains-storage`, `domains-storage-stray`, `domains-peers`, `domains-single`, `domains-disabled`, `domains-legacy-identity`, plus one `OK` per fail-fast case (`domains-ns-collision`, `domains-both-keys`, `domains-unknown-peer`, `domains-proxy-required`, `domains-vault-api-value`, `domains-proxy-not-domain`, `domains-proxy-without-domains`). No `FAIL:` line may appear.

```bash
# The docs must no longer claim the old single-sandbox storage behaviour.
# Absence is a failure here, so each check sets `bad` — `grep … || echo OK` would
# print OK on absence and exit 0 either way.
bad=0
grep -q 'never follows' helm/values.yaml && { echo "FAIL: values.yaml still says renderAgentStorage never follows jobNamespace"; bad=1; }
grep -q 'never follows' helm/README.md && { echo "FAIL: helm/README.md still says renderAgentStorage never follows jobNamespace"; bad=1; }
grep -q 'sandbox.domains' helm/README.md || { echo "FAIL: helm/README.md has no sandbox.domains row"; bad=1; }
grep -q 'sandbox.domains' docs/agent-network-security.md || { echo "FAIL: docs/agent-network-security.md does not mention sandbox.domains"; bad=1; }
grep -qx 'version: 0.23.0' helm/Chart.yaml || { echo "FAIL: helm/Chart.yaml is not at version 0.23.0"; bad=1; }
grep -qx '## Unreleased' CHANGELOG.md || { echo "FAIL: CHANGELOG.md has no top-level ## Unreleased"; bad=1; }
[ "$bad" -eq 0 ] || exit 1
```
</verification>
