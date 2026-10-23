---
status: draft
spec: [047-agent-sandbox-namespace]
created: "2026-08-21T08:00:00Z"
branch: dark-factory/agent-sandbox-namespace
---

<summary>
- The chart renders a per-environment `<env>-agents-sandbox` Namespace, so the dev and prod installs own distinct objects (`dev-agents-sandbox` / `prod-agents-sandbox`) even when they share one cluster
- The sandbox name is derived from the existing required `namespace` value — no new chart values are introduced
- The chart renders a default-deny egress NetworkPolicy `agent-sandbox-egress` that selects pods labelled `app: agent` and permits only kube-dns (53 UDP+TCP), `sentry-proxy` (9090), `claude-code-router-dev` (8788), `pushgateway` (9090), the three `vault-obsidian-*` (9090), `frontend-gateway` (9090), `frontend-strategy` (9090) and the three Kafka nodeports (32159)
- Everything else — internal services, private CIDRs, the cloud metadata endpoint `169.254.169.254`, direct internet — is denied by omission
- Ingress is not declared at all, matching the executor→Job architecture (the agent publishes its result directly to Kafka)
- The policy lives only in the sandbox namespace, so existing workloads in `dev`/`prod` are untouched
- Deploying this chart stays a no-op until the sibling executor spec ships — nothing spawns into the namespace yet
- All build-time Acceptance Criteria for the namespace and policy pass against both a dev and a prod render
</summary>

<objective>
Make the chart render a per-environment sandbox namespace plus a default-deny egress NetworkPolicy, so that when the sibling executor spec (`ConfigSpec.JobNamespace`) ships, agent Jobs land in an environment they cannot reach anything else from. After this prompt the chart's dev and prod renders each contain the sandbox Namespace and the `agent-sandbox-egress` policy, and nothing else changes.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing (they establish the existing template patterns this prompt must follow):
- `docs/agent-network-security.md` — the background document the spec cites; contains the reference NetworkPolicy this change is modeled on.
- `helm/templates/_helpers.tpl` — defines the `agent.namespace` / `agent.labels` helpers this prompt extends.
- `helm/templates/executor-rbac.yaml` — shows the `{{- if .Values.executor.enabled }}` gate + `{{- $ns := include "agent.namespace" . }}` pattern and how `agent.labels` are applied to every object.
- `helm/templates/executor-deployment.yaml` — confirms the executor runs in the control namespace as ServiceAccount `agent-task-executor`.
- `helm/values.yaml` — confirms there are NO defaults for `namespace` (empty string, required), and that `executor.enabled` defaults to `true`.

Key facts (verified against the repo):
- The chart requires `namespace`, `executor.kafkaBrokers`, and `executor.existingSecret` — a bare `helm template helm/` exits 1. Every render/grep in this prompt MUST use the spec's RENDER variable values.
- The spec's build-time ACs all run against this exact render:
  `helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret`
- The `agent.namespace` helper uses `required`, so `.Values.namespace` is guaranteed non-empty once a render succeeds.
- The NetworkPolicy MUST carry NO `metadata.labels`. The spec AC `grep -A5 'name: agent-sandbox-egress' | grep -c 'app: agent'` requires `app: agent` within 5 lines after the `name:` line; `agent.labels` expands to three lines and would push the podSelector out of that window (verified: with labels the AC returns 0). This is a deliberate divergence from the chart-wide "every object gets labels" convention.
- Kafka egress is three `/32` `ipBlock`s on TCP 32159 — **not** a namespace/pod selector. Kafka on nuke is the shared cluster's external nodeport listener, so there is no in-cluster `kafka` Service to select. Every other peer is namespace-scoped, and each one selects exactly ONE service by `podSelector` — never a bare namespace+port, which would open every service on that port in that namespace.
- Do NOT add an egress-proxy rule (port 8888, `app: egress-proxy`) — public-internet egress is owned by spec 048 `agent-egress-proxy` and must stay denied here. `docs/agent-network-security.md` is the quant-era reference shape and its own stale-note records eight divergences from it; this spec (047) ships the nine destinations in requirement 2 — kube-dns, `sentry-proxy`, `claude-code-router-<env>`, `pushgateway`, the three `vault-obsidian-*`, `frontend-gateway`, `frontend-strategy` and the Kafka nodeports.
</context>

<requirements>

1. **Add a `agent.sandboxNamespace` helper to `helm/templates/_helpers.tpl`**

   Append at the end of the file (after the existing `agent.kafkaCertVolumes` define):

   ```
   {{/* Per-environment sandbox namespace hosting agent Job pods. Parameterized so
        the dev + prod installs own distinct objects (dev-agents-sandbox /
        prod-agents-sandbox) instead of colliding on a shared name (quant: dev/prod
        are namespaces on ONE cluster). */}}
   {{- define "agent.sandboxNamespace" -}}
   {{- printf "%s-agents-sandbox" (include "agent.namespace" .) -}}
   {{- end -}}
   ```

2. **Create `helm/templates/sandbox.yaml`** with exactly two documents — a Namespace and a NetworkPolicy — both gated on `{{- if .Values.executor.enabled }}`. The full file:

   ```yaml
   {{- if .Values.executor.enabled }}
   # Per-environment sandbox namespace hosting agent Job pods. Hosted Jobs get a
   # default-deny egress policy (agent-sandbox-egress below); the executor, still
   # in the control namespace, reaches them via the cross-namespace RBAC in
   # executor-rbac.yaml (sibling prompt).
   apiVersion: v1
   kind: Namespace
   metadata:
     name: {{ include "agent.sandboxNamespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   ---
   # Default-deny egress for sandbox agent pods. Permitted: kube-dns (53 UDP+TCP),
   # sentry-proxy (9090), claude-code-router (8788), pushgateway (9090),
   # vault-obsidian-{personal,openclaw,trading} (9090),
   # frontend-gateway and frontend-strategy (9090), and the three Kafka
   # nodeports (32159). Everything else — other internal services, other private
   # CIDRs, 169.254.0.0/16, direct internet — is denied by omission.
   # Ingress is not declared at all: no executor→Job ingress path exists (the agent
   # publishes its result directly to Kafka).
   #
   # NOTE: this object intentionally carries NO metadata.labels. The approved spec
   # greps the first five lines after the name line for the podSelector label; a
   # labels block would push the selector out of that window.
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: agent-sandbox-egress
     namespace: {{ include "agent.sandboxNamespace" . }}
   spec:
     podSelector:
       matchLabels:
         app: agent
     policyTypes:
       - Egress
     egress:
       # kube-dns (cluster DNS for the sandbox)
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: kube-system
             podSelector:
               matchLabels:
                 k8s-app: kube-dns
         ports:
           - { protocol: UDP, port: 53 }
           - { protocol: TCP, port: 53 }
       # sentry-proxy — every agent Job gets SENTRY_PROXY pointed at
       # http://sentry-proxy.<env>.svc.cluster.local:9090 by the chart's
       # fleet-wide `sentry.proxy` value. namespaceSelector and podSelector sit
       # in the SAME peer: as two separate peers they OR, which opens every
       # service on 9090 in that namespace. The namespace is templated, never a
       # literal `dev`, so the prod render selects prod.
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: sentry-proxy
         ports:
           - { protocol: TCP, port: 9090 }
       # claude-code-router-<env> — every agent's model endpoint. 13 of the 17 job
       # Configs set it; without this rule they cannot call a model at all. The
       # instance label is per-environment (…-dev / …-prod), hence the template.
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app.kubernetes.io/instance: claude-code-router-{{ .Values.namespace }}
         ports:
           - { protocol: TCP, port: 8788 }
       # pushgateway — job metrics. Lives in the monitoring namespace, not the
       # release namespace, so this peer's namespaceSelector differs.
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: monitoring
             podSelector:
               matchLabels:
                 app: pushgateway
         ports:
           - { protocol: TCP, port: 9090 }
       # The three vaults and the two trading frontends. One peer EACH, not one
       # peer on port 9090 — a namespace+port rule would open every service on
       # 9090 in the release namespace, which is the trap this policy exists to
       # avoid. These are the destinations the egress matrix attributes to job
       # pods (sentry-collector, the github-* agents, trading-analysis,
       # trading-backtest); the union is shared because per-agent policies are a
       # Non-goal for this spec.
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: vault-obsidian-personal
         ports:
           - { protocol: TCP, port: 9090 }
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: vault-obsidian-openclaw
         ports:
           - { protocol: TCP, port: 9090 }
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: vault-obsidian-trading
         ports:
           - { protocol: TCP, port: 9090 }
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: frontend-gateway
         ports:
           - { protocol: TCP, port: 9090 }
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: {{ include "agent.namespace" . }}
             podSelector:
               matchLabels:
                 app: frontend-strategy
         ports:
           - { protocol: TCP, port: 9090 }
       # Kafka — the only path a task result reaches its topic. On nuke this is
       # the shared cluster's EXTERNAL nodeport listener, not an in-cluster
       # service, so these are LAN IPs. Three /32s, never a /24: a /24 would
       # allow every host on that subnet on this port.
       - to:
           - ipBlock:
               cidr: 192.168.178.41/32
           - ipBlock:
               cidr: 192.168.178.42/32
           - ipBlock:
               cidr: 192.168.178.43/32
         ports:
           - { protocol: TCP, port: 32159 }
   {{- end }}
   ```

   Copy this file verbatim. Do not reformat the inline flow-mapped ports (`{ protocol: UDP, port: 53 }` style) — the rendered output is asserted against the spec's ACs.

3. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT add an egress rule for the public internet or the `egress-proxy` (port 8888, `app: egress-proxy`) — `agent-egress-proxy.md` (spec 048) owns public-internet egress. The only permitted destinations are the nine in the policy above — kube-dns 53, `sentry-proxy` 9090, `claude-code-router-dev` 8788, `pushgateway` 9090, the three `vault-obsidian-*` on 9090, `frontend-gateway` 9090, `frontend-strategy` 9090, and Kafka 32159. Do NOT drop any of them because "the agent does not obviously need it": each is attributed to a job pod in the parent goal's egress matrix, and omitting one breaks that agent.
   - Do NOT add `metadata.labels` to the NetworkPolicy (see context — the spec AC grep depends on `app: agent` being within 5 lines of the `name:` line).
   - Do NOT add any new keys to `helm/values.yaml` — the sandbox name derives from the existing `namespace` value, and the DNS/Kafka rules are written literally in the template. Note `docs/agent-network-security.md` is the background document, but its reference policy is **stale on Kafka** (quant-era strimzi `namespaceSelector` on 9092); the ipBlock form above is authoritative for nuke.
   - Do NOT touch anything in the executor Go code, `ConfigSpec.JobNamespace`, or pod labelling — the sibling spec `bborbe/agent-task-executor:specs/config-job-namespace.md` owns those.
   - Do NOT modify `helm/templates/executor-rbac.yaml` in this prompt — the sandbox Role/RoleBinding ship in the sibling prompt (execution order: this prompt first).
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- Existing workloads in `dev`/`prod` must not be affected — the policy is scoped to the sandbox namespace and selects only `app: agent`.
- Kafka result publishing must not regress — Kafka egress (TCP 32159 to the three nodeport IPs) must be permitted.
- No capability, securityContext, or privilege change to any pod.
- The dev and prod installs must never share a sandbox namespace or NetworkPolicy — the name is parameterized per env via `{{ .Values.namespace }}-agents-sandbox`.
- Deploying this chart before `config-job-namespace.md` ships must be a no-op, not a breakage — nothing spawns into the namespace yet.
- No executor Go changes — the sibling spec owns them.
</constraints>

<verification>
Render the chart and run the spec's build-time ACs. The executor container may not ship a `helm` binary and `get.helm.sh` is unreachable from the build network (403) — install helm from the Go proxy (verified working) before rendering:

```bash
# install helm once (Go proxy is reachable; get.helm.sh is not)
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi
helm version --short   # record what it prints; any v3.x or v4.x works for template/lint

RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered-dev.yaml
echo "AC1 render exit: $? (must be 0)"
grep -c 'name: dev-agents-sandbox' /tmp/rendered-dev.yaml            # AC2: >=1
grep -A5 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'app: agent'   # AC6: >=1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'policyTypes' # AC7: ==1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'ingress:'     # AC7 negative: ==0
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-prod.yaml
grep -c 'dev-agents-sandbox' /tmp/rendered-dev.yaml                  # AC8: >=1
grep -c 'prod-agents-sandbox' /tmp/rendered-prod.yaml                # AC8: >=1
grep -c 'name: agents-sandbox' /tmp/rendered-dev.yaml                # AC8 negative: ==0
grep -c 'name: agents-sandbox' /tmp/rendered-prod.yaml               # AC8 negative: ==0

# AC9: the egress DESTINATIONS — a wrong policy renders identically under AC2/AC6/AC7/AC8,
# so these are the assertions that actually pin the rule set.
grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32' /tmp/rendered-dev.yaml # ==3
grep -c 'cidr: 192.168.178.0/24' /tmp/rendered-dev.yaml              # ==0 (never widen to the subnet)
grep -c 'port: 32159' /tmp/rendered-dev.yaml                         # ==1
grep -c 'port: 8788' /tmp/rendered-dev.yaml                          # ==1 (claude-code-router-dev)
grep -c 'port: 9092' /tmp/rendered-dev.yaml                          # ==0 (quant-era, must be gone)
# every in-cluster destination present, asserted on the SELECTOR line, on BOTH renders.
# A bare-name grep is useless here: the template comments name every destination and Helm
# preserves them, so a deleted peer still greps green.
for f in /tmp/rendered-dev.yaml /tmp/rendered-prod.yaml; do
  echo "$f"
  grep -cE '^\s+app(\.kubernetes\.io/instance)?: (sentry-proxy|claude-code-router-(dev|prod)|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy))$' "$f"   # ==8
done

# AC10: the sentry peer must keep namespaceSelector AND podSelector in ONE `- to:` entry.
# A split peer renders the same greps above and passes them all, so assert the structure:
# the nearest `- to:` before `app: sentry-proxy` must come BEFORE its namespaceSelector.
awk '
  /- to:/            { last_to = NR }
  /namespaceSelector:/ { ns = NR }
  /app: sentry-proxy/  { print (ns > last_to) ? "SAME_PEER_OK" : "SPLIT_PEER_FAIL" }
' /tmp/rendered-dev.yaml                                             # must print SAME_PEER_OK
```

Every grep must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + ACs above are the verification.

Note: the first `go install`/`go run` of helm compiles for a few minutes — that is expected; do not treat the compile output as an error.

All Post-Deploy ACs are outside this prompt's scope — they are operator-driven Rung-2/Rung-3 checks in the spec's Verification ladder (`specs/in-progress/047-agent-sandbox-namespace.md`), not commands for this container. This prompt is done when every grep above returns its annotated value.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **Kafka is an `ipBlock`, not a strimzi selector.** On nuke, Kafka is the shared cluster's external nodeport listener (`kafkaBrokers: 192.168.178.41:32159,192.168.178.42:32159,192.168.178.43:32159` in `values-dev.yaml` and `values-prod.yaml`), not an in-cluster Service — `kubectlnukedev -n dev get svc | grep -i kafka` returns nothing. So the rule is three `/32` `ipBlock`s on TCP 32159. Never widen to `192.168.178.0/24`: that allows every host on the subnet on that port. `docs/agent-network-security.md`'s reference policy still shows the quant-era strimzi `namespaceSelector` form and is out of date on this point.
- **`sentry-proxy` peer shape.** `namespaceSelector` and `podSelector` must sit in the SAME `to:` entry. Split into two entries they OR, and the rule then permits every service on 9090 in the environment's namespace — which the spec's `vault-obsidian-openclaw` block AC catches.
- **NetworkPolicy carries no `agent.labels`.** Required for the spec AC `grep -A5 'name: agent-sandbox-egress' | grep -c 'app: agent'` to pass (verified: with labels it returns 0). Diverges from the chart-wide convention of labelling every object. Confirm this is acceptable.
- **Helm availability in the executor container.** The verification installs helm via `go install helm.sh/helm/v3/cmd/helm@v3.16.4` because `get.helm.sh` returned 403 from the build network. If the claude-yolo container already ships helm, the `command -v helm` guard skips the install.
