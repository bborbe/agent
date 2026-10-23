---
status: completed
summary: Parameterized the sandbox namespace, Kafka peer, router instance, optional vault-API peer and agent storage copies via new helm values, with default renders unchanged and render tests pinning each branch
execution_id: agent-sandbox-param-exec-251-sandbox-cluster-parameterization
dark-factory-version: v0.198.7
created: "2026-10-23T18:40:00Z"
queued: "2026-10-23T15:19:35Z"
started: "2026-10-23T15:20:13Z"
completed: "2026-10-23T15:27:11Z"
---

# Parameterize the sandbox namespace, Kafka peer and agent storage

<summary>
- The sandbox namespace becomes an explicit chart value instead of a derived name
- Existing installs that set nothing render exactly as they do today — no namespace is renamed
- Sandbox egress to Kafka is driven by values, so a cluster whose Kafka is an in-cluster Service selects it by namespace and port rather than by hardcoded LAN addresses
- The router peer is driven by a value, so a cluster whose router release carries no environment suffix still matches
- An optional vault-API peer can be added for clusters whose vault service uses a different app label
- Job pods spawned into the sandbox can mount their Secret and cache volume there, which today they cannot
- New render tests pin all of the above so a later template edit cannot silently undo them
</summary>

<objective>
Let one chart serve two cluster shapes: the nuke clusters, where dev and prod are namespaces on one cluster with an external Kafka nodeport listener, and sun, a single cluster with in-cluster Kafka and differently named router and vault workloads. Today the chart hardcodes nuke's addresses and derives a sandbox name that produces a doubled suffix on a namespace already ending in `-agents`.

Job pods that land in the sandbox also need their Secret and cache volume there; today the chart renders both into the control namespace only, which is the unbound-PVC / missing-Secret failure mode spec 047 records.
</objective>

<context>
Read `CLAUDE.md` for project conventions, and `docs/dod.md` for the repo's definition of done — it requires a `## Unreleased` CHANGELOG entry and a README update whenever configuration changes.

Read these before writing:
- `helm/values.yaml` — the values contract, especially the `executor` and `egressProxy` blocks, for the commenting and defaulting style
- `helm/README.md` — the `## Values reference` section (`### Global` at ~85) and the `## Generic cluster (not quant)` section (~273), both of which this change must extend and partially correct
- `helm/templates/_helpers.tpl` — `agent.namespace` and `agent.sandboxNamespace`
- `helm/templates/sandbox.yaml` — the sandbox `Namespace`, `agent-sandbox-egress` and `egress-proxy-egress`
- `helm/templates/agents.yaml` — the per-agent Secret, PVC and env rendering
- `hack/test-helm-render.sh` — the render-test harness; `run_sandbox_case` (~151), `run_pvc_case` (~115) and the nested `doc_has` (~163)
- `docs/agent-network-security.md` — the threat model and the "a policy selecting zero pods looks exactly like a working policy" trap

This is a standalone follow-up, not a prompt generated from spec `047-agent-sandbox-namespace`. That spec is already at `status: verifying`; its Desired Behavior 1 states the chart renders `{{ .Values.namespace }}-agents-sandbox`, which stays true as the default, but the value now overrides it. Do not edit the spec. Spec 047's Failure Modes row 3 — "a Kafka broker address changes in `values-*` but not in the policy … better, derive them from that value" — stays open for nuke by design: requirement 3 explains why deriving would change nuke's rendered policy.
</context>

<requirements>
1. **`helm/templates/_helpers.tpl` — sandbox namespace becomes a value.**
   `agent.sandboxNamespace` must resolve to `.Values.sandbox.namespace` when that is non-empty, and otherwise to the current `printf "%s-agents-sandbox" (include "agent.namespace" .)`.
   The fallback is load-bearing: every existing install renders unchanged, so no deployed namespace is renamed and no `helm.sh/resource-policy: keep` orphan is left behind. Do **not** change the fallback to `%s-sandbox`.
   **Blast radius, stated so you do not try to pin it:** this helper is also used by `egress-proxy.yaml` (5 sites — Deployment, Service, the `egress-proxy-ingress` metadata and its `from` namespaceSelector, PodDisruptionBudget), `egress-proxy-allowlist.yaml` (1 site) and `executor-rbac.yaml` (2 sites). Overriding `sandbox.namespace` relocates the proxy, its allowlist and the sandbox RBAC along with the `Namespace` and the policy. That is the intended behaviour — leave those templates alone.
   Update the helper's comment to say which value wins.

2. **`helm/values.yaml` — add a `sandbox` block** with exactly these keys and defaults, documented in the surrounding style:
   ```yaml
   sandbox:
     # Explicit sandbox namespace name. Empty falls back to
     # "<namespace>-agents-sandbox", which is what every current install renders.
     namespace: ""
     kafka:
       # In-cluster Kafka: set this (and servicePort) to select brokers by
       # namespace instead of by the built-in nodeport addresses. Takes
       # precedence over them.
       namespace: ""
       # Optional pod selector narrowing the peer inside that namespace.
       # Empty opens EVERY pod in that namespace on servicePort — set it
       # whenever the namespace holds anything but the brokers.
       podLabels: {}
       servicePort: 0
     # app.kubernetes.io/instance value the router peer selects.
     # Empty falls back to "claude-code-router-<namespace>".
     routerInstance: ""
     # When set, render an extra egress peer selecting this `app` label on
     # port 9090. Set on sun to "vault-api-openclaw", whose vault service
     # carries that label rather than a vault-obsidian-* one.
     vaultApiApp: ""
     # When true, also render each agent's Secret and PVC into the sandbox
     # namespace, gated on executor.enabled. Only chart-rendered objects are
     # copied: an agent referencing an externally managed Secret via
     # `secretName` renders no Secret, so the operator must still place that
     # Secret in the sandbox namespace by hand. A copied PVC is a NEW, EMPTY
     # volume, not a view of the control-namespace claim — data written to the
     # original is not visible in the sandbox copy.
     renderAgentStorage: false
   ```
   The three nodeport addresses are **not** values — see requirement 3.

3. **`helm/templates/sandbox.yaml` — Kafka peer branches on values.**
   - When `sandbox.kafka.namespace` is non-empty: render **one** peer whose `- to:` entry holds a `namespaceSelector` matching `kubernetes.io/metadata.name: <sandbox.kafka.namespace>`, a `podSelector` built from `sandbox.kafka.podLabels` when that map is non-empty (omit the `podSelector` key entirely when it is empty), and ports `TCP/<sandbox.kafka.servicePort>`.
   - Otherwise: render today's **single peer holding three `ipBlock` entries** on `TCP/32159`, byte-identical to the current output. Those addresses stay hardcoded in the template — no cluster sets them, `bborbe/nuke` renders the defaults and this change must not touch that repo, and `executor.kafkaBrokers` is a comma-separated `host:port` string rather than CIDRs, so deriving from it would change nuke's rendered policy.
   - `namespaceSelector` and `podSelector` must sit in the **same** `- to:` entry. As separate peers they OR and open every service on that port in that namespace. The sentry peer's comment (~57-62) is the one that already explains this — keep its reasoning, and extend the wording to the Kafka rule.
   - **Fail the render when `sandbox.kafka.namespace` is non-empty and `sandbox.kafka.servicePort` is falsy.** A silent `port: 0` produces a policy that matches nothing and looks healthy — the exact trap `<context>` cites. Do **not** copy `agent.namespace`'s `required`: Helm's `required` fails only on `nil` and `""`, so `servicePort: 0` renders `port: 0` and passes. Guard explicitly at the top of the Kafka branch, e.g. `{{- if and .Values.sandbox.kafka.namespace (not .Values.sandbox.kafka.servicePort) }}{{ fail "sandbox.kafka.servicePort is required when sandbox.kafka.namespace is set" }}{{- end }}` — `not 0` is true in Go templates.

4. **`helm/templates/sandbox.yaml` — router peer takes a value.**
   The router peer's `app.kubernetes.io/instance` becomes `sandbox.routerInstance` when non-empty, else `claude-code-router-<namespace>` (today's value).

5. **`helm/templates/sandbox.yaml` — optional vault-API peer.**
   When `sandbox.vaultApiApp` is non-empty, render an additional egress peer selecting that `app` label in `agent.namespace` on `TCP/9090`, in the same one-peer-per-destination shape as the existing vault peers.

6. **`helm/templates/sandbox.yaml` — update the policy's header comment.** It currently enumerates "the three Kafka nodeports (32159)" and a fixed vault list. Reword it so it describes the peer set the values actually render, and says the Kafka branch is either the built-in nodeport addresses *or* an in-cluster namespace selector.

7. **`helm/templates/sandbox.yaml` — correct the Namespace annotation comment.** The comment above `helm.sh/resource-policy: keep` says "the chart deliberately creates neither" (PVCs and Secrets). That becomes conditional under requirement 8. Keep `resource-policy: keep` and say the copies are opt-in via `sandbox.renderAgentStorage` and default to operator-managed.

8. **`helm/templates/agents.yaml` — optional sandbox copies of the Secret and PVC.**
   When `sandbox.renderAgentStorage` is true **and** `executor.enabled` is true (gate it like every other sandbox object, or the copies land in a namespace the chart does not create), render each chart-rendered agent Secret and PVC **twice**: once in `agent.namespace` exactly as today, and once in `agent.sandboxNamespace`. The sandbox copies keep the same names, keys and `helm.sh/resource-policy: keep` on the PVC. Do not move the control-namespace copies — the executor reads the Secret from there.
   A copied PVC is a **new, empty volume**, not a view of the original — say so in the values comment (requirement 2 already carries the wording), so nobody expects the control-namespace claim's data to appear in the sandbox.

9. **`helm/README.md` — document the new values.** Add a `### sandbox` table to the `## Values reference` covering every key from requirement 2 with its default, and correct the statements this change falsifies:
   - in `## Generic cluster (not quant)`, the **Network-isolated sandbox** bullet ("the chart renders `<namespace>-agents-sandbox`") and the **Egress proxy** bullet below it ("port 8888) in `<namespace>-agents-sandbox`") — both state the namespace unconditionally;
   - the ⚠️ note in that section saying the destination set is "nuke-shaped and hardcoded … until those destinations become values" — this change is that;
   - the `### agents` sentence claiming each enabled entry emits a `Config` CR + Secret + PVC + PriorityClass + ResourceQuota — the Secret renders only with `secretEnv`, the PVC only with `volumeMountPath`, and neither for `type: service`.

10. **`docs/agent-network-security.md` — update the statements this change supersedes.** The paragraph saying the chart must prefix the sandbox namespace "because it must work on quant" (~81), the `## Target topology` block (~83), and the stale note (~140) that says the real rule is three `/32` `ipBlock`s on `32159` — after this change that is only the nuke default. Describe the value-driven name and the Kafka branch; do not restate the whole policy.

11. **`helm/Chart.yaml` — bump `version` to `0.22.0`.** New values and new rendered objects are a minor bump on a 0.x chart.

12. **`CHANGELOG.md` — add an `## Unreleased` heading** (the file has none today; its top section is `## v0.123.1`) and one or two bullets describing the five changes and the chart bump (`Chart 0.21.2→0.22.0`). Match the existing bullet style.

13. **`hack/test-helm-render.sh` — add render cases.** Refactor first, without changing current behaviour or the existing `OK: sandbox-<env>` output:
    - move `doc_has` (currently nested inside `run_sandbox_case`, ~163) to file scope and give it the rendered file as its first argument: `doc_has <file> <kind> <name> <namespace>`;
    - add `run_policy_case <case> <values file> [extra --set args…]` that renders the whole chart into a caller-visible path (a `POLICY_OUT` global the caller then greps), so a case can add its own assertions;
    - `run_sandbox_case` keeps its `<env>` signature and current assertions by delegating to `run_policy_case`, so the default case stays byte-identical;
    - extend `run_pvc_case` (~115) to `<case> <values file> <expected PVCs> <expected Secrets> <expected namespace>`, using `doc_has` for the per-namespace assertion rather than totals alone — "one of each in `<env>-agents-sandbox` and one of each in `<env>`" cannot be expressed as a count;
    - keep the `fail=1` accumulation.

    **The shared `write_values` agent (`rr-agent`) sets neither `secretEnv` nor `volumeMountPath`, so it renders zero Secrets and zero PVCs.** Do NOT add those fields to `write_values`'s heredoc unconditionally — `run_pvc_case pvc-no-mount` uses `unset.yaml` and expects 0 PVCs. Add a third `write_values` argument, defaulted as `${3:-}` under `set -u`, that appends extra agent fields on one line each (flow-style, e.g. `secretEnv: {SENTRY_DSN: dummy}`, so a multi-line field cannot break the indentation), and put the storage case's top-level `sandbox.renderAgentStorage: true` in its own values file, since a 4-space-indented argument cannot emit a top-level key.

    Then add these cases:
    - **Default render** — `run_sandbox_case` passes unchanged (`<env>-agents-sandbox`, `sel == 8`). This is the regression lock for "no install is renamed".
    - **Explicit sandbox name** — with `sandbox.namespace: <env>-sandbox`, `doc_has` finds the `Namespace` there, no document carries `namespace: <env>-agents-sandbox`, and the NetworkPolicy and sandbox RoleBinding moved with it — that covers requirement 1's blast radius.
    - **In-cluster Kafka** — with `sandbox.kafka.namespace: kafka`, `sandbox.kafka.podLabels` `{strimzi.io/cluster: sun}`, `sandbox.kafka.servicePort: 9092`: the Kafka peer's document holds a `namespaceSelector`, a `podSelector` whose `matchLabels` carries `strimzi.io/cluster: sun`, and `port: 9092`; the rendered policy contains no `32159`. With `podLabels` omitted, the peer carries no `podSelector` key at all. Put `podLabels` in a values file, or escape the dotted key (`--set 'sandbox.kafka.podLabels.strimzi\.io/cluster=sun'`) — an unescaped dotted `--set` builds nested `strimzi: {io: {cluster: sun}}`.
    - **Missing servicePort fails** — with `sandbox.kafka.namespace: kafka` and `servicePort` left at `0`, the render exits non-zero and its combined output contains `sandbox.kafka.servicePort is required`. Capture it as `if err="$(helm template … 2>&1)"; then …FAIL…; fi` — the script is `set -euo pipefail`, so a bare failing `helm template` aborts the whole run, and Helm writes the message to stderr.
    - **Default Kafka addresses** — inside the `agent-sandbox-egress` document exactly three entries match `cidr: 192\.168\.178\.(41|42|43)/32`, plus `port: 32159`. Count the `cidr` lines, not `ipBlock` lines — `egress-proxy-egress` in the same file contributes a fourth `ipBlock`. This duplicates `run_sandbox_case`'s existing `cidr32 == 3` check on purpose: it is the explicit regression lock for the default branch, stated where the branch is introduced.
    - **Router instance override** — with `sandbox.routerInstance: claude-code-router`, the peer selects that literal and not `claude-code-router-<env>`.
    - **Vault-API peer** — with `sandbox.vaultApiApp: vault-api-openclaw`, a peer selecting `vault-api-openclaw` on 9090 exists; with the default, it does not.
    - **Sandbox storage** — with `sandbox.renderAgentStorage: true` and a test agent carrying `secretEnv` + `volumeMountPath`, `agents.yaml` renders two Secrets and two PVCs, one of each in `<env>-agents-sandbox`; with the default it renders one of each, both in `<env>`; with `renderAgentStorage: true` **and** `executor.enabled: false` it renders one of each, both in `<env>`.

    Assert absence with `! grep -q`, or with the file's own `count="$(grep -c … || true)"` then a numeric compare — bare `grep -c` exits 1 on a zero count and would fail the case it was written to pass.

14. **Self-check before finishing.** Re-run `<verification>` and confirm it passes; then walk requirements 1-13 against the rendered output and confirm each one is observable in a render.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass, including every case already in `hack/test-helm-render.sh` — `run_sandbox_case` in particular, unchanged.
- The default render must be unchanged for any install that sets no `sandbox` values. This is the constraint that keeps nuke dev and nuke prod untouched by this release; a renamed sandbox namespace there would orphan a `resource-policy: keep` namespace and strand its PVCs.
- `namespaceSelector` and `podSelector` stay in the same `- to:` entry in every peer.
- Do not add a `strimzi`-specific assumption to the chart: `podLabels` is a plain map the consumer fills in.
- Do not touch `helm/templates/egress-proxy.yaml` or `egress-proxy-allowlist.yaml`.
- An empty `sandbox.kafka.podLabels` with `kafka.namespace` set is allowed but exposes every pod in that namespace on `servicePort`. That is documented in the values comment and is an accepted trade-off for a dedicated broker namespace; do not turn it into a hard failure.
- Repo-relative paths only.
</constraints>

<verification>
Run `make test` — must pass. It includes `test-helm`, which runs `bash hack/test-helm-render.sh`; every new case must report `OK:` and the script must end with `OK: helm render tests passed`.

Do NOT run `make precommit` for this change: it additionally pulls `trivy`, `osv-scanner` and `golangci-lint` over an unchanged Go tree, which adds no signal for a chart-only edit and can fail on tooling availability rather than on the change.
</verification>
