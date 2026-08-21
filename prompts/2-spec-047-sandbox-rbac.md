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
- The Post-Deploy acceptance criteria remain a deferred operator checklist: they need real clusters and the sibling executor spec, not a scenario harness
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
- `helm/Chart.yaml` — current `version: 0.5.2`.
- `CHANGELOG.md` — current top entry is `## v0.81.3`; no `## Unreleased` section exists as of this writing.
- `helm/README.md` — the "Generic cluster (not quant)" section is where the sandbox note belongs.

Key facts (verified against the repo):
- The executor ServiceAccount is named `agent-task-executor` and lives in the control namespace `{{ $ns }}` (see `helm/templates/executor-rbac.yaml` ServiceAccount and `helm/templates/executor-deployment.yaml` `serviceAccountName`). The sandbox RoleBinding's subject must reference `namespace: {{ $ns }}` (the control namespace), NOT the sandbox namespace — that is what makes it a cross-namespace binding.
- The spec AC `grep -A6 'kind: RoleBinding' | grep -c 'agent-task-executor-sandbox'` needs only ≥1; GNU `grep -A` emits context for every match, so placement of the sandbox RoleBinding anywhere inside `executor-rbac.yaml` passes (verified).
- The sandbox Role/RoleBinding are appended inside the existing `{{- if .Values.executor.enabled }}` block, after the existing `agent-task-executor` RoleBinding, before the final `{{- end }}`.
- Chart-version convention: every helm change bumps `helm/Chart.yaml` (features → minor bump, e.g. `0.5.0 → 0.5.1` was `feat(helm)`, `0.5.1 → 0.5.2` was `fix(helm)`). This is a feature → `0.5.2 → 0.6.0`.
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

3. **Bump `helm/Chart.yaml` `version: 0.5.2` → `version: 0.6.0`.** Nothing else in Chart.yaml changes (appVersion stays `"0.3.1"`).

4. **Add a CHANGELOG entry in `CHANGELOG.md`.** If a `## Unreleased` section already exists at the top (above `## v0.81.3`), append to it; otherwise create it directly under the `# Changelog` heading. Entry (one bullet, `feat(helm):` prefix, matches the repo's helm-entry style):

   ```
   ## Unreleased

   - feat(helm): add a per-environment `<namespace>-agents-sandbox` namespace with a default-deny egress NetworkPolicy (kube-dns 53 UDP+TCP + strimzi Kafka 9092 TCP only) and a sandbox-scoped Role/RoleBinding granting the executor ServiceAccount exactly `create/get/list/watch/delete` on jobs, `get` on secrets, and `get` on persistentvolumeclaims in that namespace. Chart 0.5.2→0.6.0. Inert until the executor ships `ConfigSpec.JobNamespace` (agent-task-executor); agent Jobs then land in an environment with no reachable internal service, private range, metadata endpoint, or direct internet.
   ```

   Follow the existing CHANGELOG.md style (dash-prefixed bullets, past-tense, no trailing period on the prefix).

5. **Add a short sandbox note to `helm/README.md`.** In the "Generic cluster (not quant)" section, extend the existing "**Your own namespace**" bullet list with one new bullet directly after it:

   ```
   - **Network-isolated sandbox** — the chart renders `<namespace>-agents-sandbox` with a
     default-deny egress NetworkPolicy (kube-dns + Kafka only) and cross-namespace RBAC for
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
```

Every grep must return the annotated value or the prompt is not done. Also confirm the RBAC subject is cross-namespace (the RoleBinding's subject ServiceAccount namespace must be `dev`, the control namespace, not `dev-agents-sandbox`):

```bash
grep -A12 'name: agent-task-executor-sandbox$' /tmp/rendered-dev.yaml | grep -A6 'subjects:' | grep -E 'name:|namespace:'
# must show ServiceAccount agent-task-executor in namespace: dev
```

Bookkeeping checks:

```bash
grep -n '^version:' helm/Chart.yaml                       # must show 0.6.0
head -10 CHANGELOG.md | grep -E 'Unreleased|feat\(helm\)'  # must show both
grep -n 'agents-sandbox' helm/README.md                    # must show the new bullet
```

Do NOT run `make precommit` — this prompt changes no Go code; the render + greps above are the verification.

**Deferred operator checklist (DO NOT RUN here — document as pending).** The Post-Deploy ACs require real cluster creds AND the sibling executor spec to have shipped; this container has neither. They are reproduced below for the completion report / spec Verification ladder only. Record them as pending in your completion report; never attempt to execute them here.

```text
# Rung-2 (dev), after deploy + sibling executor spec live:
kubectlquant -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.metadata.name}'          # expect agent-sandbox-egress
kubectlquant get ns dev-agents-sandbox -o jsonpath='{.status.phase}'                                               # expect Active
kubectlquant -n dev-agents-sandbox get pods -l app=agent -o name | wc -l                                          # expect >=1 while a Job runs
kubectlquant -n dev-agents-sandbox get networkpolicy agent-sandbox-egress -o jsonpath='{.spec.egress[*].ports[*].port}'  # expect 53 53 9092
kubectlquant -n dev-agents-sandbox get role agent-task-executor-sandbox -o jsonpath='{.rules[*].resources}'        # expect jobs secrets persistentvolumeclaims (Failure-Modes row 4 recovery)
# from inside a sandbox agent pod: wget -qO- --timeout=3 http://vault-obsidian-openclaw.dev.svc.cluster.local:9090  # expect non-zero, no body
# from inside a sandbox agent pod: wget -qO- --timeout=3 http://169.254.169.254/                                    # expect non-zero
kubectlquant -n dev logs deploy/agent-task-executor --since=15m | grep -c 'consume 1 messages'                     # expect >=1
# Rung-3 (prod): same block/allow pair in prod-agents-sandbox
```
</verification>
