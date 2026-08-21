---
status: draft
spec: [047-agent-sandbox-namespace]
created: "2026-08-21T08:00:00Z"
branch: dark-factory/agent-sandbox-namespace
---

<summary>
- The chart renders a per-environment `<env>-agents-sandbox` Namespace, so the dev and prod installs own distinct objects (`dev-agents-sandbox` / `prod-agents-sandbox`) even when they share one cluster
- The sandbox name is derived from the existing required `namespace` value — no new chart values are introduced
- The chart renders a default-deny egress NetworkPolicy `agent-sandbox-egress` that selects pods labelled `app: agent` and permits only kube-dns (53 UDP+TCP) and strimzi Kafka (9092 TCP)
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
- The strimzi Kafka namespace selector uses `kubernetes.io/metadata.name: strimzi` (the kube-apiserver adds this label to every namespace automatically) rather than the doc's `name: strimzi`. See "REVIEWER OPEN QUESTIONS" at the bottom of this prompt.
- Do NOT add an egress-proxy rule (port 8888, `app: egress-proxy`) — public-internet egress is owned by spec 048 `agent-egress-proxy` and must stay denied here. The docs reference policy shows the future end-state; this spec (047) only ships kube-dns + Kafka.
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
   # Default-deny egress for sandbox agent pods: only kube-dns (53 UDP+TCP) and
   # strimzi Kafka (9092 TCP) are reachable. Everything else — internal services,
   # private CIDRs, 169.254.0.0/16, direct internet — is denied by omission.
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
       # strimzi Kafka — the only path a task result reaches its topic
       - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: strimzi
             podSelector:
               matchLabels:
                 strimzi.io/cluster: my-cluster
         ports:
           - { protocol: TCP, port: 9092 }
   {{- end }}
   ```

   Copy this file verbatim. Do not reformat the inline flow-mapped ports (`{ protocol: UDP, port: 53 }` style) — the rendered output is asserted against the spec's ACs.

3. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT add an egress rule for the public internet, egress-proxy, or any port beyond 53 UDP, 53 TCP, and 9092 TCP. `agent-egress-proxy.md` (spec 048) owns public-internet egress.
   - Do NOT add `metadata.labels` to the NetworkPolicy (see context — the spec AC grep depends on `app: agent` being within 5 lines of the `name:` line).
   - Do NOT add any new keys to `helm/values.yaml` — the sandbox name derives from the existing `namespace` value and the Kafka/DNS selectors are hardcoded per `docs/agent-network-security.md`.
   - Do NOT touch anything in the executor Go code, `ConfigSpec.JobNamespace`, or pod labelling — the sibling spec `bborbe/agent-task-executor:specs/config-job-namespace.md` owns those.
   - Do NOT modify `helm/templates/executor-rbac.yaml` in this prompt — the sandbox Role/RoleBinding ship in the sibling prompt (execution order: this prompt first).
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- Existing workloads in `dev`/`prod` must not be affected — the policy is scoped to the sandbox namespace and selects only `app: agent`.
- Kafka result publishing must not regress — Kafka egress (9092 TCP) must be permitted.
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
helm version --short   # must print v3.16

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
```

Every grep must return the annotated value or the prompt is not done. Do NOT run `make precommit` — this prompt changes no Go code; the render + ACs above are the verification.

Note: the first `go install`/`go run` of helm compiles for a few minutes — that is expected; do not treat the compile output as an error.

The Post-Deploy ACs (namespace Active, policy selects real pods, internal-service blocked, metadata endpoint blocked, Kafka reachable) are operator-executed against real clusters with `kubectlquant` after this prompt AND the sibling RBAC prompt AND the executor spec all land — they are NOT part of this prompt's verification.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **strimzi namespace selector form.** This prompt uses `kubernetes.io/metadata.name: strimzi` for the Kafka rule's `namespaceSelector`, where `docs/agent-network-security.md`'s reference policy uses `name: strimzi`. `kubernetes.io/metadata.name` is auto-added by the kube-apiserver to every namespace (guaranteed match); `name: strimzi` requires a manually-applied label whose existence on quant could not be verified from the repo. The spec's Kafka-reachability AC only asserts ports (`53 53 9092`), so both forms pass. If quant's strimzi namespace carries a different metadata.name than `strimzi` (the chart's `executor.kafkaUser.strimziNamespace` default), this rule must be re-pointed.
- **NetworkPolicy carries no `agent.labels`.** Required for the spec AC `grep -A5 'name: agent-sandbox-egress' | grep -c 'app: agent'` to pass (verified: with labels it returns 0). Diverges from the chart-wide convention of labelling every object. Confirm this is acceptable.
- **Helm availability in the executor container.** The verification installs helm via `go install helm.sh/helm/v3/cmd/helm@v3.16.4` because `get.helm.sh` returned 403 from the build network. If the claude-yolo container already ships helm, the `command -v helm` guard skips the install.
