---
status: approved
spec: [007-agent-config-crd]
created: "2026-04-16T17:30:00Z"
queued: "2026-04-16T17:27:43Z"
---

<summary>
- Adds three example AgentConfig resources matching today's hardcoded executor configuration (claude, backtest-agent, trade-analysis-agent)
- Grants the executor ServiceAccount the minimum RBAC needed to self-install the CRD and watch AgentConfig resources
- Adds a CHANGELOG entry summarising the shift from compiled-in agents to declarative AgentConfig CRs
- Deployment artifacts are ready for `kubectl apply` in dev — no more code changes needed to onboard a new agent
- No Go code changes in this prompt — only YAML manifests and the root changelog
</summary>

<objective>
Ship the deployment artifacts that make the CRD-based executor operable: three example AgentConfig CRs under `task/executor/k8s/`, RBAC grants for self-install + watch, and a CHANGELOG entry. After this prompt a fresh dev deploy self-installs the CRD, picks up the three applied CRs via the informer, and spawns jobs identically to the previous hardcoded behavior.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**Authoritative references:**
- `~/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — changelog format rules.
- `specs/in-progress/007-agent-config-crd.md` — Acceptance Criteria (three CRs matching today's configuration with exact field values).
- `docs/agent-crd-specification.md` — field list for the CR schema.

**Outputs of prompts 1 and 2 (preconditions):**
- CRD `agentconfigs.agents.bborbe.dev` is self-installed by the executor on startup.
- The executor watches AgentConfig in its own namespace and resolves tasks via the store.
- The package-level `agentConfigs` slice in `main.go` is deleted.

**Key reference files:**
- `CHANGELOG.md` (repo root) — existing format `## vX.Y.Z\n\n- feat: ...`. Latest entry is `v0.33.0`. This prompt adds `v0.34.0`.
- `task/executor/k8s/agent-task-executor-rbac.yaml` — existing ServiceAccount, Role, RoleBinding with placeholder `'{{ "NAMESPACE" | env }}'` templating (Jinja-style, rendered by the deploy pipeline). Role currently only grants `batch/jobs`.
- `task/executor/k8s/agent-task-executor-deploy.yaml` — existing Deployment. No changes needed here.
- Prior hardcoded configs (from `task/executor/main.go` before prompt 2 deletion):
  - `claude`: image `docker.quant.benjamin-borbe.de:443/agent-claude`, no secret, no volume.
  - `backtest-agent`: image `docker.quant.benjamin-borbe.de:443/agent-backtest`, `secretName: agent-backtest`, no volume.
  - `trade-analysis-agent`: image `docker.quant.benjamin-borbe.de:443/agent-trade-analysis`, `secretName: agent-trade-analysis`, `volumeClaim: agent-trade-analysis`, `volumeMountPath: /home/claude/.claude`.

**Key facts:**
- CR metadata names are fixed by the spec Verification block — use exactly `agent-claude`, `agent-backtest-agent`, `agent-trade-analysis`.
- The CRD is namespace-scoped — do NOT set `metadata.namespace` in the YAML so `kubectl apply -n <ns>` can target any cluster. (If you prefer explicit, use `'{{ "NAMESPACE" | env }}'` Jinja placeholder — check how other executor manifests render.)
- `heartbeat` is required per the CRD schema — use `15m` for claude and backtest, `5m` for trade-analysis. These are arbitrary placeholder defaults (spec Non-Goals excludes `heartbeat` wiring); tune later when the controller consumes the field.
- Do NOT set `resources` — spec Non-Goals explicitly excludes wiring `spec.resources` into the spawned Job in this work. Setting it would be misleading.
- RBAC must grant:
  - `get/create/update/patch` on `customresourcedefinitions.apiextensions.k8s.io` (cluster-scoped — requires ClusterRole + ClusterRoleBinding, since the resource is cluster-scoped).
  - `get/list/watch` on `agentconfigs.agents.bborbe.dev` in the executor's namespace (can stay in the Role or move to a ClusterRole; prefer Role to keep the blast radius namespace-scoped).
- `docker.quant.benjamin-borbe.de:443` is the private registry. The branch tag (`:dev` / `:prod`) is appended by the executor at lookup time — do NOT include a tag in the CR `spec.image`. The image field is the untagged base name.
- Agent filenames must exactly match what the spec Verification block references:
  - `task/executor/k8s/agent-claude.yaml`
  - `task/executor/k8s/agent-backtest-agent.yaml`
  - `task/executor/k8s/agent-trade-analysis.yaml`
</context>

<requirements>

1. **Create `task/executor/k8s/agent-claude.yaml`**

   ```yaml
   apiVersion: agents.bborbe.dev/v1
   kind: AgentConfig
   metadata:
     name: agent-claude
   spec:
     assignee: claude
     image: docker.quant.benjamin-borbe.de:443/agent-claude
     heartbeat: 15m
   ```

2. **Create `task/executor/k8s/agent-backtest-agent.yaml`**

   ```yaml
   apiVersion: agents.bborbe.dev/v1
   kind: AgentConfig
   metadata:
     name: agent-backtest-agent
   spec:
     assignee: backtest-agent
     image: docker.quant.benjamin-borbe.de:443/agent-backtest
     heartbeat: 15m
     secretName: agent-backtest
   ```

3. **Create `task/executor/k8s/agent-trade-analysis.yaml`**

   ```yaml
   apiVersion: agents.bborbe.dev/v1
   kind: AgentConfig
   metadata:
     name: agent-trade-analysis
   spec:
     assignee: trade-analysis-agent
     image: docker.quant.benjamin-borbe.de:443/agent-trade-analysis
     heartbeat: 5m
     secretName: agent-trade-analysis
     volumeClaim: agent-trade-analysis
     volumeMountPath: /home/claude/.claude
   ```

4. **Update `task/executor/k8s/agent-task-executor-rbac.yaml`**

   Add CRD self-install grants as a **ClusterRole + ClusterRoleBinding** (CRDs are cluster-scoped resources). Add AgentConfig read grants as additional rules on the **existing Role**.

   After the existing Role `rules:` block, extend with:
   ```yaml
     - apiGroups:
         - agents.bborbe.dev
       resources:
         - agentconfigs
       verbs:
         - get
         - list
         - watch
   ```

   Then append a new ClusterRole + ClusterRoleBinding to the same file (YAML multi-doc separator `---`):
   ```yaml
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRole
   metadata:
     name: 'agent-task-executor-{{ "NAMESPACE" | env }}'
   rules:
     - apiGroups:
         - apiextensions.k8s.io
       resources:
         - customresourcedefinitions
       verbs:
         - get
         - create
         - update
         - patch
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: ClusterRoleBinding
   metadata:
     name: 'agent-task-executor-{{ "NAMESPACE" | env }}'
   roleRef:
     apiGroup: rbac.authorization.k8s.io
     kind: ClusterRole
     name: 'agent-task-executor-{{ "NAMESPACE" | env }}'
   subjects:
     - kind: ServiceAccount
       name: agent-task-executor
       namespace: '{{ "NAMESPACE" | env }}'
   ```

   Use `'agent-task-executor-{{ "NAMESPACE" | env }}'` as both the ClusterRole and ClusterRoleBinding name — the namespace suffix prevents dev/prod overwriting each other in a shared cluster. Match the surrounding file's templating style exactly.

5. **Add a CHANGELOG entry**

   In `CHANGELOG.md` at the repo root, add above the `## v0.33.0` line:

   ```
   ## v0.34.0

   - feat: AgentConfig CRD replaces hardcoded agent slice in task/executor; executor self-installs `agentconfigs.agents.bborbe.dev` CRD on startup, watches AgentConfig CRs in its namespace, and resolves task assignees via an in-memory store fed by the informer; adding a new agent is now a `kubectl apply` with no executor rebuild
   - feat: Three example AgentConfig CRs under `task/executor/k8s/` (agent-claude, agent-backtest-agent, agent-trade-analysis) matching the previously hardcoded configuration
   - feat: RBAC extended to grant executor ServiceAccount cluster-scoped write on `customresourcedefinitions` (self-install) and namespace-scoped `get/list/watch` on `agentconfigs.agents.bborbe.dev`
   ```

   Follow existing CHANGELOG.md style (dash-prefixed bullets, past-tense active voice).

6. **Verify YAML syntax**

   Run `cd task/executor && make precommit` — it must still pass (no Go changes here, but precommit validates formatting). If the repo has a YAML linter integrated, it runs automatically. If not, manually eyeball indentation.

</requirements>

<constraints>
- Do NOT include a tag in `spec.image` — the executor appends `:<branch>` at lookup time.
- Do NOT set `spec.resources` — wiring resources into the Job is explicitly a spec Non-Goal for this work.
- Do NOT set `metadata.namespace` in the AgentConfig YAMLs — operators pass `-n <ns>` at apply time.
- Do NOT add a separate `agentconfigs.agents.bborbe.dev` CRD manifest — the executor self-installs it (per prompt 1 and spec Constraint).
- Do NOT modify Go code in this prompt.
- Do NOT commit — dark-factory handles git.
- Use the exact filenames referenced by the spec Verification block: `agent-claude.yaml`, `agent-backtest-agent.yaml`, `agent-trade-analysis.yaml`.
- Match the existing `rbac.yaml` templating style — `'{{ "NAMESPACE" | env }}'` single-quoted Jinja placeholders.
- Changelog version must be `v0.34.0` (one minor above the current `v0.33.0`).
- All existing tests must still pass.
</constraints>

<verification>
```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify the three AgentConfig YAMLs exist and parse:
```bash
for f in task/executor/k8s/agent-claude.yaml task/executor/k8s/agent-backtest-agent.yaml task/executor/k8s/agent-trade-analysis.yaml; do
  test -f "$f" && echo "OK $f" || echo "MISSING $f"
done
```

Verify each CR's assignee matches spec:
```bash
grep -A1 "assignee:" task/executor/k8s/agent-claude.yaml
grep -A1 "assignee:" task/executor/k8s/agent-backtest-agent.yaml
grep -A1 "assignee:" task/executor/k8s/agent-trade-analysis.yaml
```
Must show `claude`, `backtest-agent`, `trade-analysis-agent` respectively.

Verify trade-analysis PVC fields:
```bash
grep -E "volumeClaim|volumeMountPath" task/executor/k8s/agent-trade-analysis.yaml
```
Must show `volumeClaim: agent-trade-analysis` and `volumeMountPath: /home/claude/.claude`.

Verify RBAC includes customresourcedefinitions + agentconfigs:
```bash
grep -E "customresourcedefinitions|agentconfigs|agents.bborbe.dev" task/executor/k8s/agent-task-executor-rbac.yaml
```
Must show both the CRD write grant and the AgentConfig watch grant.

Verify CHANGELOG entry:
```bash
head -15 CHANGELOG.md | grep -E "v0.34.0|AgentConfig CRD"
```
Must show the new entry at the top.

**Note:** Dev-cluster acceptance criteria from the spec Verification section (kubectlquant apply, informer pickup observation, delete/re-apply flow) are manual post-merge checks — they cannot run inside the dark-factory container and are not part of prompt verification.
</verification>
