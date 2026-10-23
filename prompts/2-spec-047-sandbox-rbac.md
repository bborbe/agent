---
status: draft
spec: [047-agent-sandbox-namespace]
created: "2026-08-21T08:00:00Z"
branch: dark-factory/agent-sandbox-namespace
---

<summary>
- The executor's ServiceAccount gains a namespace-scoped Role inside the sandbox namespace with exactly `create`/`get`/`list`/`watch`/`delete` on Jobs, `get` on Secrets, and `get` on PersistentVolumeClaims — and nothing else
- The RoleBinding binds the executor's existing `agent-task-executor` ServiceAccount (which stays in the control namespace) to that Role across namespaces
- Grants are sandbox-scoped and enumerated — no wildcard verbs, no cluster-scope, so the executor's rights outside its environment's sandbox namespace do not widen
- The chart version is bumped and the change is recorded in the repo CHANGELOG
- The full build-time Acceptance Criteria set of the spec passes after this prompt — the sandbox namespace, the policy, and the RBAC all render correctly for dev and prod
- The Post-Deploy acceptance criteria remain a deferred operator checklist: they need real cluster creds, not a scenario harness. They do **not** wait on the sibling executor spec — the policy ACs run against a hand-created `app: agent` probe pod
</summary>

<objective>
Complete the sandbox feature by granting the executor exactly the cross-namespace rights it needs inside `<env>-agents-sandbox` (Jobs create/read/delete, Secrets read, PVCs read) and by releasing the change (chart version + CHANGELOG). After this prompt the chart is deployable end-to-end: nothing breaks when deployed before the executor spec ships, and when the executor does spawn into the sandbox it has precisely the rights it needs.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing:
- `docs/agent-network-security.md` — the background document the spec cites (its "Executor changes" section names the cross-namespace RBAC).
- `helm/templates/executor-rbac.yaml` — the file this prompt extends. Note the existing structure: top-level `{{- if .Values.executor.enabled }}`, then `{{- $ns := include "agent.namespace" . }}`, then ServiceAccount / ClusterRole / ClusterRoleBinding / Role / RoleBinding, ending with `{{- end }}`.
- `helm/templates/_helpers.tpl` — already contains the `agent.sandboxNamespace` helper from the sibling prompt (this prompt depends on it; if it is missing, STOP and report a precondition failure rather than inventing the name).
- `helm/Chart.yaml` — current `version: 0.18.0`. (Note: `0.18.0` is **not published** — `helm show chart oci://registry-1.docker.io/bborbe/agent --version 0.18.0` returns `not found`; the highest published tag is `0.17.0`, which is what both nuke worktrees pin.)
- `CHANGELOG.md` — current top release entry is `## v0.120.0`, and a `## Unreleased` section already sits above it (added when spec 047 was amended). Append to that `## Unreleased`; do not create a second one.
- `helm/README.md` — the "Generic cluster (not quant)" section is where the sandbox note belongs.

Key facts (verified against the repo):
- The executor ServiceAccount is named `agent-task-executor` and lives in the control namespace `{{ $ns }}` (see `helm/templates/executor-rbac.yaml` ServiceAccount and `helm/templates/executor-deployment.yaml` `serviceAccountName`). The sandbox RoleBinding's subject must reference `namespace: {{ $ns }}` (the control namespace), NOT the sandbox namespace — that is what makes it a cross-namespace binding.
- The spec AC `grep -A6 'kind: RoleBinding' | grep -c 'agent-task-executor-sandbox'` needs only ≥1; GNU `grep -A` emits context for every match, so placement of the sandbox RoleBinding anywhere inside `executor-rbac.yaml` passes (verified).
- The sandbox Role/RoleBinding are appended inside the existing `{{- if .Values.executor.enabled }}` block, after the existing `agent-task-executor` RoleBinding, before the final `{{- end }}`.
- Chart-version convention: every helm change bumps `helm/Chart.yaml` — a feature takes a minor bump, a fix a patch (most recent example: `v0.120.0` was `feat(helm)` and took `0.17.0 → 0.18.0`). This is a feature → `0.18.0 → 0.19.0`.
- CHANGELOG convention: one bullet per logical change, prefixed `feat(helm):`, newest section at the top. Recent prompts add a `## Unreleased` section above the latest `## vX.Y.Z` when none exists.
</context>

<requirements>

1. **Extend `helm/templates/executor-rbac.yaml` with the sandbox Role + RoleBinding**

   Inside the existing `{{- if .Values.executor.enabled }}` block, after the existing `agent-task-executor` RoleBinding and before the closing `{{- end }}`, append exactly:

   ```yaml
   ---
   # Sandbox-scoped grants: the executor, still in the control namespace, gets exactly
   # the cross-namespace rights it needs inside this environment's sandbox namespace
   # and no more. No wildcard verbs, no cluster-scope — a widened grant here would
   # widen the executor's blast radius past the sandbox boundary.
   apiVersion: rbac.authorization.k8s.io/v1
   kind: Role
   metadata:
     name: agent-task-executor-sandbox
     namespace: {{ include "agent.sandboxNamespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   rules:
     - apiGroups: [batch]
       resources: [jobs]
       verbs: [create, get, list, watch, delete]
     - apiGroups: [""]
       resources: [secrets]
       verbs: [get]
     - apiGroups: [""]
       resources: [persistentvolumeclaims]
       verbs: [get]
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: RoleBinding
   metadata:
     name: agent-task-executor-sandbox
     namespace: {{ include "agent.sandboxNamespace" . }}
     labels:
       {{- include "agent.labels" . | nindent 4 }}
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: Role
     name: agent-task-executor-sandbox
   subjects:
     - kind: ServiceAccount
       name: agent-task-executor
       namespace: {{ $ns }}
   ```

   Copy this verbatim. `$ns` is already defined at the top of the file; `include "agent.sandboxNamespace" .` comes from the sibling prompt.

2. **Do NOT grant more than DB 5.** The Role's rules are fixed: `create`/`get`/`list`/`watch`/`delete` on `jobs` (apiGroup `batch`), `get` on `secrets` (apiGroup `""`), `get` on `persistentvolumeclaims` (apiGroup `""`). No `*` verbs, no `*` resources, no cluster-scope, no additional resources or apiGroups. The spec AC asserts zero `*` characters within 15 lines of the Role name.

3. **Bump `helm/Chart.yaml` `version: 0.18.0` → `version: 0.19.0`.** Nothing else in Chart.yaml changes (appVersion stays `"0.3.1"`).

4. **Add a CHANGELOG entry in `CHANGELOG.md`.** A `## Unreleased` section already exists at the top (above `## v0.120.0`) — append your bullet to it. Create one directly under the `# Changelog` heading only if it is somehow absent. Entry (one bullet, `feat(helm):` prefix, matches the repo's helm-entry style):

   ```
   ## Unreleased

   - feat(helm): add a per-environment `<namespace>-agents-sandbox` namespace with a default-deny egress NetworkPolicy (kube-dns 53 UDP+TCP; sentry-proxy 9090; claude-code-router 8788; pushgateway 9090; vault-obsidian-personal/openclaw/trading 9090; frontend-gateway and frontend-strategy 9090; Kafka nodeports 32159 — and nothing else) and a sandbox-scoped Role/RoleBinding granting the executor ServiceAccount exactly `create/get/list/watch/delete` on jobs, `get` on secrets, and `get` on persistentvolumeclaims in that namespace. Chart 0.18.0→0.19.0. Inert until the executor ships `ConfigSpec.JobNamespace` (agent-task-executor); agent Jobs then land in an environment with no reachable service outside that list, no other private range, no metadata endpoint, and no direct internet.
   ```

   Follow the existing CHANGELOG.md style (dash-prefixed bullets, past-tense, no trailing period on the prefix).

5. **Add a short sandbox note to `helm/README.md`.** In the "Generic cluster (not quant)" section, extend the existing "**Your own namespace**" bullet list with one new bullet directly after it:

   ```
   - **Network-isolated sandbox** — the chart renders `<namespace>-agents-sandbox` with a
     default-deny egress NetworkPolicy (kube-dns, sentry-proxy, claude-code-router, pushgateway, the three
     vault-obsidian services, frontend-gateway, frontend-strategy and the Kafka nodeports — nothing else) and cross-namespace RBAC for
     the executor. Agent Jobs land there; it is inert until the executor ships
     `ConfigSpec.JobNamespace` (spawns Jobs into the sandbox).
   ```

   Do not restructure any other README content.

6. **Do NOT make any of these changes** (spec Non-goals / Constraints):
   - Do NOT touch any executor Go code, `ConfigSpec.JobNamespace`, or pod labelling — the sibling spec `bborbe/agent-task-executor:specs/config-job-namespace.md` owns those.
   - Do NOT add per-agent egress policies, pod-to-pod isolation, Kafka topic-level authorization, or runtime sandboxing — all spec Non-goals.
   - Do NOT modify the sandbox Namespace or NetworkPolicy from the sibling prompt.
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- Existing workloads in `dev`/`prod` must not be affected — the new Role/RoleBinding live only in the sandbox namespace.
- Kafka result publishing must not regress.
- No capability, securityContext, or privilege change to any pod.
- The executor's rights outside its environment's sandbox namespace must not widen — grants are namespace-scoped and enumerated, no `*`.
- The dev and prod installs must never share a sandbox Role or RoleBinding — both are in the parameterized `{{ .Values.namespace }}-agents-sandbox` namespace.
- Deploying this chart before `config-job-namespace.md` ships must be a no-op, not a breakage.
- No executor Go changes — the sibling spec owns them.
</constraints>

<verification>
Render the chart and run the FULL build-time AC set of the spec (this prompt completes the feature, so every AC must pass). Install helm from the Go proxy if missing, as in the sibling prompt:

```bash
if ! command -v helm >/dev/null 2>&1; then
  go install helm.sh/helm/v3/cmd/helm@v3.16.4
  export PATH="$PATH:$(go env GOPATH)/bin"
fi

RENDER='helm template helm/ --set namespace=dev --set executor.kafkaBrokers=kafka:9092 --set executor.existingSecret=agent-secret'
eval "$RENDER" > /tmp/rendered-dev.yaml
echo "AC1 render exit: $? (must be 0)"
grep -c 'name: dev-agents-sandbox' /tmp/rendered-dev.yaml            # AC2: >=1
grep -A3 'name: agent-task-executor-sandbox' /tmp/rendered-dev.yaml | grep -c 'namespace: dev-agents-sandbox'   # AC3: >=1
grep -A15 'name: agent-task-executor-sandbox' /tmp/rendered-dev.yaml | grep -cE 'jobs|secrets|persistentvolumeclaims'  # AC4: >=3
grep -A15 'name: agent-task-executor-sandbox' /tmp/rendered-dev.yaml | grep -c '\*'   # AC4 negative: ==0
grep -A6 'kind: RoleBinding' /tmp/rendered-dev.yaml | grep -c 'agent-task-executor-sandbox'   # AC5: >=1
grep -A5 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'app: agent'   # AC6: >=1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'policyTypes' # AC7: ==1
grep -A30 'name: agent-sandbox-egress' /tmp/rendered-dev.yaml | grep -c 'ingress:'     # AC7 negative: ==0
helm template helm/ --set namespace=prod --set executor.kafkaBrokers=kafka:9092 \
  --set executor.existingSecret=agent-secret > /tmp/rendered-prod.yaml
grep -c 'dev-agents-sandbox' /tmp/rendered-dev.yaml                  # AC8: >=1
grep -c 'prod-agents-sandbox' /tmp/rendered-prod.yaml                # AC8: >=1
grep -c 'name: agents-sandbox' /tmp/rendered-dev.yaml                # AC8 negative: ==0
grep -c 'name: agents-sandbox' /tmp/rendered-prod.yaml               # AC8 negative: ==0

# Spec AC7 — the nine destinations, asserted on the SELECTOR line, on BOTH renders.
# A bare-name grep is useless: the template comments name every destination and Helm
# preserves them, so a deleted peer still greps green.
for f in /tmp/rendered-dev.yaml /tmp/rendered-prod.yaml; do
  echo "$f"
  grep -cE '^\s+app(\.kubernetes\.io/instance)?: (sentry-proxy|claude-code-router-(dev|prod)|pushgateway|vault-obsidian-(personal|openclaw|trading)|frontend-(gateway|strategy))$' "$f"   # ==8
  grep -cE 'cidr: 192\.168\.178\.(41|42|43)/32' "$f"   # ==3
  grep -c 'cidr: 192.168.178.0/24' "$f"                # ==0
  grep -c 'port: 9092' "$f"                            # ==0
done

# Spec AC8 — sentry-proxy keeps namespaceSelector and podSelector in ONE `- to:` entry.
# A split peer renders identically under every grep above and ORs into "every service on
# 9090 in that namespace", which is what this policy exists to prevent.
awk '/- to:/{t=NR} /namespaceSelector:/{n=NR} /app: sentry-proxy/{print (n>t)?"SAME_PEER_OK":"SPLIT_PEER_FAIL"}' /tmp/rendered-dev.yaml   # SAME_PEER_OK
```

Every grep must return the annotated value or the prompt is not done. Also confirm the RBAC subject is cross-namespace (the RoleBinding's subject ServiceAccount namespace must be `dev`, the control namespace, not `dev-agents-sandbox`):

```bash
grep -A12 'name: agent-task-executor-sandbox$' /tmp/rendered-dev.yaml | grep -A6 'subjects:' | grep -E 'name:|namespace:'
# must show ServiceAccount agent-task-executor in namespace: dev
```

Bookkeeping checks:

```bash
grep -n '^version:' helm/Chart.yaml                       # must show 0.19.0
head -15 CHANGELOG.md | grep -E 'Unreleased|feat\(helm\)'  # must show both
grep -n 'agents-sandbox' helm/README.md                    # must show the new bullet
```

Do NOT run `make precommit` — this prompt changes no Go code; the render + greps above are the verification.

All Post-Deploy ACs are outside this prompt's scope — they are operator-driven Rung-2/Rung-3 checks in the spec's Verification ladder (`specs/in-progress/047-agent-sandbox-namespace.md`), not commands for this container. This prompt is done when every grep above returns its annotated value.
</verification>
