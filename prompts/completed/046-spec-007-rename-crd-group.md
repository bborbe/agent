---
status: completed
spec: [007-agent-config-crd]
summary: Renamed AgentConfig CRD API group from agents.bborbe.dev to agent.benjamin-borbe.de and Kind from AgentConfig to Config across task/executor service, generated new k8s client code, updated all Go symbols, YAML manifests, docs, spec, and CHANGELOG with v0.35.0 entry.
container: agent-046-spec-007-rename-crd-group
dark-factory-version: v0.121.1-dirty
created: "2026-04-16T20:00:00Z"
queued: "2026-04-17T05:50:24Z"
started: "2026-04-17T12:51:00Z"
completed: "2026-04-17T13:11:47Z"
---

<summary>
- Renames the AgentConfig CRD to match the real bborbe domain convention used by every other production CRD in the cluster
- The API group moves from the throwaway `agents.bborbe.dev` to the canonical `agent.benjamin-borbe.de`
- The CR Kind shortens from the redundant `AgentConfig` to the idiomatic `Config` (matching `Alert`, `Schema` in sibling CRDs)
- No cluster migration is needed — the CRD was never applied to a live cluster, so this is a pure code rename
- Bundles the rename, the `agent-claude` example CR, and the RBAC group rename into one v0.35.0 release; trading-specific CRs (backtest-agent, trade-analysis) live in the trading repo
- Example CR file, RBAC grants, the CHANGELOG entry, the spec file, the schema doc, the repo `README.md` / `CLAUDE.md` components lists, the `specs/ideas/agent-definition-crd.md` idea file, and the `lib/agent_task-assignee.go` doc comment all speak the new group/kind after this prompt
- After the rename a fresh dev deploy self-installs `configs.agent.benjamin-borbe.de` and watches `kind: Config` resources via the informer
</summary>

<objective>
Rename the AgentConfig CRD API group from `agents.bborbe.dev` to `agent.benjamin-borbe.de` and the Kind from `AgentConfig` to `Config` across the `task/executor` service, documentation, spec, and example manifests. Ship the rename together with the `agent-claude` example CR and the RBAC extension as a single atomic v0.35.0 release. Remove the two trading-specific CRs (backtest-agent, trade-analysis) — they belong in the trading repo (`trading/agent/backtest/k8s/`, `trading/agent/trade-analysis/k8s/`). After this prompt the executor self-installs `configs.agent.benjamin-borbe.de`, watches `kind: Config` resources, and the repository contains zero references to the old names.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

**Authoritative references:**
- `~/.claude/plugins/marketplaces/coding/docs/go-kubernetes-crd-controller-guide.md` — section 2 (repo layout) and section 3 (types + register) are authoritative for the Go-side rename shape.
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `errors.Wrapf` usage (unchanged, but renamed error messages must keep the convention).
- `~/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — format rules for the new v0.35.0 entry.
- `docs/agent-crd-specification.md` at repo root — the authoritative schema doc. Every `agents.bborbe.dev` → `agent.benjamin-borbe.de` and `AgentConfig` → `Config` mention must be updated here.
- `specs/in-progress/007-agent-config-crd.md` — the spec that shipped v0.33.0 / v0.34.0. The Desired Behavior, Failure Modes, Assumptions, Security, Acceptance Criteria, and Verification sections all reference the old names.

**Naming convention this rename adopts (mandatory):**

| Before | After |
|---|---|
| Group | `agents.bborbe.dev` → `agent.benjamin-borbe.de` |
| CRD full name | `agentconfigs.agents.bborbe.dev` → `configs.agent.benjamin-borbe.de` |
| Kind | `AgentConfig` → `Config` |
| List Kind | `AgentConfigList` → `ConfigList` |
| Plural | `agentconfigs` → `configs` |
| Singular | `agentconfig` → `config` |
| Short name | `ac` → `cfg` |
| Apis dir | `task/executor/k8s/apis/agents.bborbe.dev/` → `task/executor/k8s/apis/agent.benjamin-borbe.de/` |
| Apis parent package | `package agents` → `package agent` |

**Reference implementations to mirror (read before editing):**
- `~/Documents/workspaces/alert/k8s/apis/monitoring.benjamin-borbe.de/` — directory layout under a dotted group, `package monitoring` parent, `Kind: Alert` (not `MonitoringAlert`). Same shape this rename adopts.
- Production CRDs already in the bborbe cluster confirming the `<resource>.<group>.benjamin-borbe.de` convention: `alerts.monitoring.benjamin-borbe.de`, `schemas.cdb.benjamin-borbe.de`, `schemas.raw.benjamin-borbe.de`. There is no `*.bborbe.dev` CRD in production — the old group was a mistake.

**Starting state of the repo (must be read before editing):**
- Working tree is **clean** (no staged, no unstaged changes).
- `CHANGELOG.md` at HEAD has `## v0.34.0` with three bullets (informer wiring, example CRs, RBAC). All three use the old names (`AgentConfig`, `agentconfigs.agents.bborbe.dev`).
- RBAC manifests are already split into one-resource-per-file: `agent-task-executor-{sa,role,rolebinding,clusterrole,clusterrolebinding}.yaml`.
- `agent-task-executor-role.yaml` still references the old group `agents.bborbe.dev` / resource `agentconfigs` — this prompt renames it.
- `agent-claude.yaml` does **not** exist yet — this prompt creates it (step 18).
- Trading-specific CRs (backtest-agent, trade-analysis) are **not** in this repo — they live in the trading repo.
- The in-tree files under `task/executor/k8s/apis/agents.bborbe.dev/v1/` and `task/executor/pkg/k8s_connector.go` contain the old names.

**Internal Go struct naming — do NOT rename:**
- `pkg.AgentConfiguration` (in `task/executor/pkg/agent_configuration.go`) is a **different** type from the CRD — it is the runtime conversion target. Leave it alone. Leave `AgentConfigurations` alone. Leave the factory method `CreateConsumer`'s parameter name `resolver` alone. The rename is scoped to the CRD-shaped types (`AgentConfig` → `Config`, `AgentConfigList` → `ConfigList`) and to names whose current spelling embeds the old CRD name (`EventHandlerAgentConfig`, `AgentConfigResolver`, `CreateEventHandlerAgentConfig`, etc.).

**Rename mapping for Go symbols (authoritative):**

| Old symbol | New symbol |
|---|---|
| `v1.AgentConfig` | `agentv1.Config` |
| `v1.AgentConfigSpec` | `agentv1.ConfigSpec` |
| `v1.AgentConfigList` | `agentv1.ConfigList` |
| `pkg.EventHandlerAgentConfig` | `pkg.EventHandlerConfig` |
| `pkg.NewEventHandlerAgentConfig` | `pkg.NewEventHandlerConfig` |
| `pkg.NewResourceEventHandlerAgentConfig` | `pkg.NewResourceEventHandlerConfig` |
| `pkg.AgentConfigResolver` | `pkg.ConfigResolver` |
| `pkg.NewAgentConfigResolver` | `pkg.NewConfigResolver` |
| `pkg.ErrAgentConfigNotFound` | `pkg.ErrConfigNotFound` |
| `factory.CreateEventHandlerAgentConfig` | `factory.CreateEventHandlerConfig` |
| `factory.CreateResourceEventHandlerAgentConfig` | `factory.CreateResourceEventHandlerConfig` |
| `factory.CreateAgentConfigResolver` | `factory.CreateConfigResolver` |
| Mock `FakeAgentConfigResolver` | `FakeConfigResolver` (regenerated) |
| Import alias `v1 "…/agents.bborbe.dev/v1"` | `agentv1 "…/agent.benjamin-borbe.de/v1"` |

**File renames (move, do not copy):**

| Old path | New path |
|---|---|
| `task/executor/k8s/apis/agents.bborbe.dev/register.go` | `task/executor/k8s/apis/agent.benjamin-borbe.de/register.go` |
| `task/executor/k8s/apis/agents.bborbe.dev/v1/doc.go` | `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/doc.go` |
| `task/executor/k8s/apis/agents.bborbe.dev/v1/register.go` | `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/register.go` |
| `task/executor/k8s/apis/agents.bborbe.dev/v1/types.go` | `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` |
| `task/executor/k8s/apis/agents.bborbe.dev/v1/types_test.go` | `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go` |
| `task/executor/k8s/apis/agents.bborbe.dev/v1/zz_generated.deepcopy.go` | regenerated by `make generatek8s` — do NOT hand-move |
| `task/executor/pkg/event_handler_agent_config.go` | `task/executor/pkg/event_handler_config.go` |
| `task/executor/pkg/resource_event_handler_agent_config.go` | `task/executor/pkg/resource_event_handler_config.go` |
| `task/executor/pkg/agent_config_resolver.go` | `task/executor/pkg/config_resolver.go` |
| `task/executor/pkg/agent_config_resolver_test.go` | `task/executor/pkg/config_resolver_test.go` |
| `task/executor/k8s/client/**` | entire tree regenerated under new group path |

Keep `task/executor/pkg/k8s_connector.go` and `task/executor/pkg/k8s_connector_test.go` with their current filenames (name is generic, only internal strings/constants change).

**Codegen workflow (must run exactly in this order, per `task/executor/Makefile`):**
1. `cd task/executor && go mod tidy`
2. `cd task/executor && make generatek8s` — writes `k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go` and regenerates the entire `k8s/client/` tree under the new group path.
3. `cd task/executor && make ensure` — `go mod tidy` + `go mod verify` + `rm -rf vendor`.
4. `cd task/executor && make generate` — counterfeiter; regenerates `mocks/` including `FakeConfigResolver` and `FakeK8sConnector`.
5. `cd task/executor && make precommit` — must pass with exit code 0.

**Critical: before running codegen, the OLD `task/executor/k8s/client/` tree must be deleted** — otherwise leftover files under the old group path will be committed. After the move the `k8s/client/` directory should only contain files under `clientset/versioned/typed/agent.benjamin-borbe.de/`, `informers/externalversions/agent.benjamin-borbe.de/`, `listers/agent.benjamin-borbe.de/`, and `applyconfiguration/agent.benjamin-borbe.de/` — no `agents.bborbe.dev/` subdirectories.

**Release convention:**
- Bump to `v0.35.0` in `CHANGELOG.md` (one minor above `v0.34.0`). SemVer pre-1.0 permits a minor bump for a breaking change; the `feat!` marker signals the break independently.
- The existing `## v0.34.0` block has three bullets with old names — rewrite them in place to use the new CRD names (so the changelog stays truthful about what shipped).
- Insert a fresh `## v0.35.0` block above it with: the rename bullet and the `agent-claude` CR bullet.
</context>

<requirements>

1. **Move the apis directory**

   ```bash
   cd task/executor/k8s/apis
   git mv agents.bborbe.dev agent.benjamin-borbe.de
   # remove the stale generated deepcopy — it will be regenerated
   rm -f agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
   ```

2. **Rewrite `task/executor/k8s/apis/agent.benjamin-borbe.de/register.go`**

   Replace the entire file body (keep the BSD license header):
   ```go
   package agent

   // GroupName is the Kubernetes API group for Config resources.
   const GroupName = "agent.benjamin-borbe.de"
   ```

3. **Rewrite `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/doc.go`**

   Keep the BSD license header. Body:
   ```go
   // +k8s:deepcopy-gen=package,register
   // +groupName=agent.benjamin-borbe.de

   // Package v1 is the v1 version of the Config API.
   package v1
   ```

4. **Rewrite `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/register.go`**

   Update the package import path from `…/agents.bborbe.dev` to `…/agent.benjamin-borbe.de`, rename the import alias from `agents` to `agent`, and rename the registered types. Concretely:
   - Import `agent "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de"`.
   - `SchemeGroupVersion = schema.GroupVersion{Group: agent.GroupName, Version: "v1"}`.
   - `addKnownTypes` registers `&Config{}` and `&ConfigList{}` (not `&AgentConfig{}` / `&AgentConfigList{}`).
   - Comments that say "AgentConfig" must say "Config".

5. **Rewrite `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`**

   Every occurrence of the CRD type names changes. Keep the BSD license header, imports, and method semantics. Concretely:
   - Rename the struct `AgentConfig` → `Config`.
   - Rename the struct `AgentConfigSpec` → `ConfigSpec`.
   - Rename the struct `AgentConfigList` → `ConfigList`.
   - The `AgentResources` struct **stays** named `AgentResources` (it is a nested value type, not a CRD-shaped type — keeping the name avoids collision with the too-generic `Resources`).
   - Update `var _ libk8s.Type = AgentConfig{}` → `var _ libk8s.Type = Config{}` (and the preceding comment).
   - Update every method receiver and every type name inside method bodies. The `Equal` switch becomes:
     ```go
     switch o := other.(type) {
     case Config:
         return a.Spec.Equal(o.Spec)
     case *Config:
         return a.Spec.Equal(o.Spec)
     default:
         return false
     }
     ```
   - Update every doc comment that mentions "AgentConfig" to say "Config" (e.g. `// Config declares a single agent type …`).

6. **Update `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go`**

   Replace the import alias `v1 "…/agents.bborbe.dev/v1"` with `agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"`. Rewrite every `v1.AgentConfig`, `v1.AgentConfigSpec` reference to `agentv1.Config` / `agentv1.ConfigSpec`. Update `Describe("AgentConfig", …)` and `Describe("AgentConfigSpec", …)` to `Describe("Config", …)` and `Describe("ConfigSpec", …)`. Keep all assertions and sub-spec structure identical — only identifiers change.

7. **Rename and update `task/executor/pkg/event_handler_agent_config.go` → `event_handler_config.go`**

   ```bash
   cd task/executor/pkg && git mv event_handler_agent_config.go event_handler_config.go
   ```
   Then edit the file:
   - Import alias becomes `agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"`.
   - `EventHandlerAgentConfig` → `EventHandlerConfig`.
   - `NewEventHandlerAgentConfig` → `NewEventHandlerConfig`.
   - Generic type parameters `k8s.EventHandler[v1.AgentConfig]` → `k8s.EventHandler[agentv1.Config]` and `k8s.NewEventHandler[v1.AgentConfig]()` → `k8s.NewEventHandler[agentv1.Config]()`.
   - Update doc comments to drop the "AgentConfig" phrasing in favour of "Config".

8. **Rename and update `task/executor/pkg/resource_event_handler_agent_config.go` → `resource_event_handler_config.go`**

   ```bash
   cd task/executor/pkg && git mv resource_event_handler_agent_config.go resource_event_handler_config.go
   ```
   Edit:
   - Import alias `agentv1 "…/agent.benjamin-borbe.de/v1"`.
   - `NewResourceEventHandlerAgentConfig` → `NewResourceEventHandlerConfig`.
   - Parameter type `EventHandlerAgentConfig` → `EventHandlerConfig`.
   - Generic call `k8s.NewResourceEventHandler[v1.AgentConfig](ctx, handler)` → `k8s.NewResourceEventHandler[agentv1.Config](ctx, handler)`.

9. **Rename and update `task/executor/pkg/agent_config_resolver.go` → `config_resolver.go`**

    ```bash
    cd task/executor/pkg && git mv agent_config_resolver.go config_resolver.go
    ```
    Edit:
    - Import alias `agentv1 "…/agent.benjamin-borbe.de/v1"`.
    - `ErrAgentConfigNotFound` → `ErrConfigNotFound`; update the underlying `stderrors.New("agent config not found")` literal to `stderrors.New("config not found")`.
    - `AgentConfigResolver` → `ConfigResolver`.
    - `NewAgentConfigResolver` → `NewConfigResolver`.
    - Struct `agentConfigResolver` → `configResolver`.
    - Counterfeiter directive updates to: `//counterfeiter:generate -o ../mocks/config_resolver.go --fake-name FakeConfigResolver . ConfigResolver`.
    - `k8s.Provider[v1.AgentConfig]` → `k8s.Provider[agentv1.Config]` (parameter and struct field).
    - Signature `convert(obj v1.AgentConfig, branch string) AgentConfiguration` → `convert(obj agentv1.Config, branch string) AgentConfiguration`.

10. **Rename and update `task/executor/pkg/agent_config_resolver_test.go` → `config_resolver_test.go`**

    ```bash
    cd task/executor/pkg && git mv agent_config_resolver_test.go config_resolver_test.go
    ```
    Rewrite every reference following the symbol map in `<context>`. Specifically: import alias → `agentv1`, every `v1.AgentConfig`/`v1.AgentConfigSpec` → `agentv1.Config`/`agentv1.ConfigSpec`, every `pkg.NewAgentConfigResolver` → `pkg.NewConfigResolver`, every `pkg.ErrAgentConfigNotFound` → `pkg.ErrConfigNotFound`, every `Describe("AgentConfigResolver", …)` → `Describe("ConfigResolver", …)`. Keep all assertion bodies identical — only identifiers change.

11. **Update `task/executor/pkg/k8s_connector.go` (keep filename)**

    Internal strings and generated-client imports change; filename and exported `K8sConnector` interface stay.
    - Import paths of the generated clientset/informers packages (`…/k8s/client/clientset/versioned` and `…/k8s/client/informers/externalversions`) are unchanged, but the informer factory group accessor renames from `Agents()` to `Agent()` (group without dots becomes the accessor name).
    - In `Listen`, replace `factory.Agents().V1().AgentConfigs().Informer()` with `factory.Agent().V1().Configs().Informer()`.
    - In `SetupCustomResourceDefinition`, replace every occurrence of the string literal `"agentconfigs.agents.bborbe.dev"` (both in `Get` and in the `ObjectMeta{Name: …}`) with `"configs.agent.benjamin-borbe.de"`.
    - In `desiredCRDSpec()`:
      - `Group: "agents.bborbe.dev"` → `Group: "agent.benjamin-borbe.de"`
      - `Kind: "AgentConfig"` → `Kind: "Config"`
      - `ListKind: "AgentConfigList"` → `ListKind: "ConfigList"`
      - `Plural: "agentconfigs"` → `Plural: "configs"`
      - `Singular: "agentconfig"` → `Singular: "config"`
      - `ShortNames: []string{"ac"}` → `ShortNames: []string{"cfg"}`
    - Leave the OpenAPI v3 schema body (`assignee`/`image`/`heartbeat`/`resources`/`env`/`secretName`/`volumeClaim`/`volumeMountPath`) unchanged — only the enclosing CRD identifiers rename.

12. **Update `task/executor/pkg/k8s_connector_test.go`**

    - Replace every string literal `"agentconfigs.agents.bborbe.dev"` with `"configs.agent.benjamin-borbe.de"` (both in the `Get` path assertion and in the seeded `existingCRD`).
    - Replace every CR-embedded `Group: "agents.bborbe.dev"`, `Kind: "AgentConfig"`, `Plural: "agentconfigs"` with the new values.
    - Keep test structure, fake clientset usage, and assertion semantics identical.

13. **Update `task/executor/pkg/factory/factory.go`**

    - `CreateEventHandlerAgentConfig` → `CreateEventHandlerConfig`. Return type `pkg.EventHandlerAgentConfig` → `pkg.EventHandlerConfig`. Body: `return pkg.NewEventHandlerConfig()`.
    - `CreateResourceEventHandlerAgentConfig` → `CreateResourceEventHandlerConfig`. Parameter type `pkg.EventHandlerAgentConfig` → `pkg.EventHandlerConfig`. Body: `return pkg.NewResourceEventHandlerConfig(ctx, handler)`.
    - `CreateAgentConfigResolver` → `CreateConfigResolver`. Parameter type `pkg.EventHandlerAgentConfig` → `pkg.EventHandlerConfig`. Return type `pkg.AgentConfigResolver` → `pkg.ConfigResolver`. Body: `return pkg.NewConfigResolver(handler, string(branch))`.
    - `CreateConsumer` parameter `resolver pkg.AgentConfigResolver` → `resolver pkg.ConfigResolver`. Leave the parameter **name** `resolver` unchanged.
    - Update doc comments accordingly.

14. **Update `task/executor/pkg/handler/task_event_handler.go`**

    - Parameter `resolver pkg.AgentConfigResolver` → `resolver pkg.ConfigResolver` in `NewTaskEventHandler`.
    - Field `resolver pkg.AgentConfigResolver` → `resolver pkg.ConfigResolver` in `taskEventHandler`.
    - `stderrors.Is(err, pkg.ErrAgentConfigNotFound)` → `stderrors.Is(err, pkg.ErrConfigNotFound)`.
    - Leave all other logic (phases, branch filter, metric labels, log messages) **unchanged**. Metric label values like `"skipped_unknown_assignee"` are public-interface strings; do NOT touch them.

15. **Update `task/executor/pkg/handler/task_event_handler_test.go`**

    - Every `mocks.FakeAgentConfigResolver` → `mocks.FakeConfigResolver` (the regenerated mock).
    - Every `pkg.ErrAgentConfigNotFound` → `pkg.ErrConfigNotFound`.
    - Leave all other test logic identical.

16. **Update `task/executor/main.go`**

    - `factory.CreateEventHandlerAgentConfig()` → `factory.CreateEventHandlerConfig()`.
    - `factory.CreateResourceEventHandlerAgentConfig(...)` → `factory.CreateResourceEventHandlerConfig(...)`.
    - `factory.CreateAgentConfigResolver(...)` → `factory.CreateConfigResolver(...)`.
    - Error wrap message `"setup AgentConfig CRD"` → `"setup Config CRD"`.
    - Local var `eventHandlerAgentConfig` → `eventHandlerConfig` (optional but preferred for consistency).

17. **Regenerate codegen + mocks + verify**

    After all hand-written edits are on disk, delete the stale generated client tree and rerun codegen:
    ```bash
    cd task/executor
    rm -rf k8s/client
    rm -rf mocks
    go mod tidy
    make generatek8s
    make ensure
    make generate
    make precommit
    ```
    `make precommit` must exit 0. If it fails because of leftover `v1.AgentConfig` / `pkg.AgentConfigResolver` references, search and fix them — do NOT paper over with `_ =` or build tags.

18. **Rewrite the agent-claude example CR YAML**

    Create `task/executor/k8s/agent-claude.yaml` with `apiVersion: agent.benjamin-borbe.de/v1` and `kind: Config`. This is the only CR that ships in the agent repo — trading-specific CRs live in the trading repo.

    `task/executor/k8s/agent-claude.yaml`:
    ```yaml
    apiVersion: agent.benjamin-borbe.de/v1
    kind: Config
    metadata:
      name: agent-claude
    spec:
      assignee: claude
      image: docker.quant.benjamin-borbe.de:443/agent-claude
      heartbeat: 15m
    ```

    Confirm the two trading YAMLs are gone:
    ```bash
    test ! -f task/executor/k8s/agent-backtest-agent.yaml && echo "OK"
    test ! -f task/executor/k8s/agent-trade-analysis.yaml && echo "OK"
    ```

19. **Update `task/executor/k8s/agent-task-executor-role.yaml`**

    The RBAC manifests are already split into one-resource-per-file. Only the Role file needs editing — replace the AgentConfig rule:
      ```yaml
        - apiGroups:
            - agent.benjamin-borbe.de
          resources:
            - configs
          verbs:
            - get
            - list
            - watch
      ```
    Leave the `batch/jobs` rule unchanged. The other RBAC files (sa, rolebinding, clusterrole, clusterrolebinding) need no edits.

20. **Update `docs/agent-crd-specification.md`**

    Perform a global rewrite of the doc:
    - Every `apiVersion: agents.bborbe.dev/v1` → `apiVersion: agent.benjamin-borbe.de/v1`.
    - Every `kind: AgentConfig` → `kind: Config`.
    - Update the H1 title to `# Config CRD Specification`.
    - Update the opening sentence from "AgentConfig is a Kubernetes Custom Resource Definition …" to "Config (`agent.benjamin-borbe.de/v1`) is a Kubernetes Custom Resource Definition …".
    - Every prose reference to "AgentConfig" / "AgentConfig CRD" / "AgentConfig CRs" updates to the new name ("Config" / "Config CRD" / "Config CRs").
    - Every reference to the group `agents.bborbe.dev` updates to `agent.benjamin-borbe.de`.
    - The field table (`spec.assignee`, `spec.image`, …) stays unchanged — it describes the schema, not the Kind.

21. **Update `specs/in-progress/007-agent-config-crd.md`**

    Global rewrite:
    - Every `apiVersion: agents.bborbe.dev/v1` → `apiVersion: agent.benjamin-borbe.de/v1`.
    - Every `AgentConfig` (the Kind/CRD noun) → `Config`. Leave the Go internal type `AgentConfiguration` unchanged (it is still the conversion target).
    - Every `agentconfigs.agents.bborbe.dev` → `configs.agent.benjamin-borbe.de`.
    - Every `agents.bborbe.dev` → `agent.benjamin-borbe.de`.
    - In the Verification section, `kubectlquant -n dev get crd agentconfigs.agents.bborbe.dev` → `kubectlquant -n dev get crd configs.agent.benjamin-borbe.de`; `kubectlquant -n dev get agentconfigs` → `kubectlquant -n dev get configs`; `kubectlquant -n dev delete agentconfig agent-trade-analysis` → `kubectlquant -n dev delete config agent-trade-analysis`.
    - Do NOT change the frontmatter `status:`, `approved:`, `generating:`, `verifying:`, or `branch:` fields.

21a. **Update four repo-level references that sit outside `task/executor/`**

    Rename the old CRD name in these four additional locations:
    - `README.md` — line ~9 in the components table: `AgentConfig CRD` → `Config CRD`.
    - `CLAUDE.md` — line ~107 in the components list: `AgentConfig CRD` → `Config CRD`.
    - `lib/agent_task-assignee.go` — doc comment `// Matched against AgentConfig CRD spec.assignee.` → `// Matched against Config CRD spec.assignee.`.
    - `specs/ideas/agent-definition-crd.md` — global rewrite like step 21: every `AgentConfig` (Kind/CRD noun) → `Config`, every `agentconfigs.agents.bborbe.dev` → `configs.agent.benjamin-borbe.de`, every `agents.bborbe.dev` → `agent.benjamin-borbe.de`, every `apiVersion: agents.bborbe.dev/v1` → `apiVersion: agent.benjamin-borbe.de/v1`. This is an early idea file kept in-tree for history — it must still speak the current name so anyone reading the idea list does not confuse it with dead state.

22. **Rewrite the CHANGELOG**

    Open `CHANGELOG.md`. The existing `## v0.34.0` block has three bullets using old names. **Rewrite all three in place** to use the new names, then insert a fresh `## v0.35.0` block above it.

    Rewrite the v0.34.0 bullets:
    - Bullet 1: `AgentConfig` → `Config`, `AgentConfigResolver` → `ConfigResolver`
    - Bullet 2: `AgentConfig CRs` → `Config CRs`, note that trading CRs moved to trading repo
    - Bullet 3: `agentconfigs.agents.bborbe.dev` → `configs.agent.benjamin-borbe.de`

    Then insert a fresh `## v0.35.0` block immediately above `## v0.34.0`:

    ```
    ## v0.35.0

    - feat!: Rename AgentConfig CRD to Config and move the API group from `agents.bborbe.dev` to `agent.benjamin-borbe.de` to match the bborbe convention (`alerts.monitoring.benjamin-borbe.de`, `schemas.cdb.benjamin-borbe.de`, …); CRD is now `configs.agent.benjamin-borbe.de` with short name `cfg`; no cluster migration needed because the old CRD was never applied
    - feat: Example Config CR `agent-claude` under `task/executor/k8s/`; trading-specific CRs (backtest-agent, trade-analysis) ship from the trading repo
    ```

    The `feat!` marker signals the breaking rename (pre-1.0 SemVer carve-out). Leave `## v0.33.0` and below untouched.

23. **Final verification sweep**

    After all edits and codegen, sweep the whole repo from the root (not just `task/executor/`):
    ```bash
    grep -RIn "agents\.bborbe\.dev\|agentconfigs" . 2>&1 \
         | grep -v "^\./\.git/" \
         | grep -v "^\./prompts/" \
         | grep -v "^\./CHANGELOG\.md:" \
         | grep -v "k8s/client/" \
         | grep -v "mocks/"
    ```
    Must return zero hits. Exclusions explained:
    - `./.git/` — pack data, not source.
    - `./prompts/` — this prompt file and historical prompts mention the old names by design.
    - `./CHANGELOG.md` — v0.33.0 / v0.34.0 entries use the historical names on purpose (that is what shipped).
    - `k8s/client/` and `mocks/` — regenerated files; the codegen step above moves them under the new group path; any accidental generator glitch should be flagged but will not block.

    Then a Kind-name sweep that allows the intentionally-kept internal Go type `AgentConfiguration`:
    ```bash
    grep -RIn "AgentConfig\b" . 2>&1 \
         | grep -v "^\./\.git/" \
         | grep -v "^\./prompts/" \
         | grep -v "^\./CHANGELOG\.md:" \
         | grep -v "k8s/client/" \
         | grep -v "mocks/" \
         | grep -v -E "AgentConfiguration(s)?\b"
    ```
    Must return zero hits.

    Finally, confirm the regenerated client tree is fully under the new group:
    ```bash
    grep -RIn "agents\.bborbe\.dev\|agentconfigs" task/executor/k8s/client/ task/executor/mocks/ 2>&1 | head
    ```
    Must return zero hits.

</requirements>

<constraints>
- Do NOT push to a live cluster. No `kubectl apply`, no `kubectlquant`, no cluster-touching command.
- Commit atomically — the entire rename plus the `agent-claude` CR plus the RBAC update plus the CHANGELOG bump go in a **single** commit. dark-factory handles the commit.
- Do NOT rename the internal Go type `pkg.AgentConfiguration` (in `agent_configuration.go`) or `AgentConfigurations`. Those are the conversion targets, not the CRD type.
- Do NOT rename the nested struct `AgentResources` in `types.go` — keeping that name avoids the too-generic `Resources` symbol.
- Do NOT rename metric label values (`skipped_unknown_assignee`, `skipped_status`, etc.) — they are external interface strings.
- Do NOT touch the frontmatter fields (`status`, `approved`, `generating`, `verifying`, `branch`) of `specs/in-progress/007-agent-config-crd.md` — only body content renames.
- Do NOT hand-edit `k8s/client/**` or `k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go` — all four subtrees (`clientset`, `informers`, `listers`, `applyconfiguration`) and the deepcopy file are produced by `make generatek8s`.
- Do NOT chain `generatek8s` into `precommit` or `generate` — it stays a manually-run target. `make precommit` alone must pass after the rename.
- Do NOT add a separate CRD YAML manifest — the schema lives in Go code; the executor self-installs.
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for any new error wraps — never `fmt.Errorf`.
- The CRD name `configs.agent.benjamin-borbe.de` must match the rule `<plural>.<group>`; `Plural: "configs"` and `Group: "agent.benjamin-borbe.de"` must stay in sync with the literal CRD name.
- `Scope: apiextensionsv1.NamespaceScoped` stays — the rename does not change CRD scope.
- Release version is `v0.35.0` — one minor above the shipped `v0.34.0` tag. The `## v0.34.0` block in `CHANGELOG.md` is preserved untouched (it documents a real tag); the new `## v0.35.0` block is inserted above it.
- All existing tests must still pass after the rename — type names + CRD name + file names are the only semantic changes.
- Do NOT commit — dark-factory handles git.

Failure-mode coverage (from spec §Failure Modes) confirming no regression:
- "CRD missing at startup" — requirement 11 (Create branch with new CRD name).
- "CRD exists with older schema" — requirement 11 (Update branch with new CRD name).
- "API server unavailable at startup" — unchanged (`SetupCustomResourceDefinition` still returns wrapped error).
- "AgentConfig CR deleted while executor running" — unchanged (warning + `skipped_unknown_assignee` metric increment via renamed `ErrConfigNotFound`).
- "AgentConfig CR updated" — unchanged (informer push path, only the watched type renames).
- "RBAC missing" — requirement 19 (renamed `apiGroups: [agent.benjamin-borbe.de]`, `resources: [configs]`).
- "`volumeClaim` set but `volumeMountPath` missing" — unchanged (spawner guard + `ConfigSpec.Validate` guard kept intact).
- "Secret referenced by `spec.secretName` does not exist" — unchanged (K8s handles).
</constraints>

<verification>
```bash
cd task/executor && make precommit
```
Must pass with exit code 0.

Verify agent-claude.yaml was created:
```bash
test -f task/executor/k8s/agent-claude.yaml && echo "CLAUDE-OK"
```

Verify the apis directory moved:
```bash
test -d task/executor/k8s/apis/agent.benjamin-borbe.de/v1 && echo "OK"
test ! -d task/executor/k8s/apis/agents.bborbe.dev && echo "OLD-GONE"
```

Verify generated client is under the new group path:
```bash
ls task/executor/k8s/client/clientset/versioned/typed/agent.benjamin-borbe.de/v1/config.go \
   task/executor/k8s/client/informers/externalversions/agent.benjamin-borbe.de/v1/config.go \
   task/executor/k8s/client/listers/agent.benjamin-borbe.de/v1/config.go
```
All three must exist. Old paths must not:
```bash
test ! -d task/executor/k8s/client/clientset/versioned/typed/agents.bborbe.dev && echo "OK"
```

Verify the CRD literal name:
```bash
grep -n "configs.agent.benjamin-borbe.de" task/executor/pkg/k8s_connector.go
```
Must show two matches (one in `Get`, one in the `ObjectMeta`).

Verify no leftover old-name references anywhere in source (prompts/, CHANGELOG.md historical entries, and .git/ are allowed to retain old names):
```bash
grep -RIn "agents\.bborbe\.dev\|agentconfigs" . 2>&1 \
  | grep -v "^\./\.git/" \
  | grep -v "^\./prompts/" \
  | grep -v "^\./CHANGELOG\.md:"
```
Must return zero hits.

```bash
grep -RIn "AgentConfig\b" . 2>&1 \
  | grep -v "^\./\.git/" \
  | grep -v "^\./prompts/" \
  | grep -v "^\./CHANGELOG\.md:" \
  | grep -v -E "AgentConfiguration(s)?\b"
```
Must return zero hits. (The final `grep -v` excludes the internal Go struct `AgentConfiguration`/`AgentConfigurations` which is intentionally kept.)

Verify the agent-claude CR parses under the new group:
```bash
grep -E "^apiVersion: agent\.benjamin-borbe\.de/v1$" task/executor/k8s/agent-claude.yaml > /dev/null && echo "OK"
grep -E "^kind: Config$" task/executor/k8s/agent-claude.yaml > /dev/null && echo "KIND-OK"
```
Must print `OK` and `KIND-OK`.

Verify RBAC points at the new group + resource:
```bash
grep -n "agent\.benjamin-borbe\.de\|configs" task/executor/k8s/agent-task-executor-role.yaml
```
Must show `- agent.benjamin-borbe.de` under `apiGroups` and `- configs` under `resources`.

Verify CHANGELOG has the new entry at the top:
```bash
head -20 CHANGELOG.md | grep -E "^## v0\.35\.0$"
head -20 CHANGELOG.md | grep -E "Rename AgentConfig CRD to Config"
```
Must match both lines.

Verify v0.34.0 header is preserved (shipped release, not rewritten):
```bash
grep -E "^## v0\.34\.0$" CHANGELOG.md
```
Must return exactly one hit (the shipped v0.34.0 block, untouched).

Verify v0.35.0 appears ABOVE v0.34.0 in the file:
```bash
awk '/^## v0\.35\.0$/ {p35=NR} /^## v0\.34\.0$/ {p34=NR} END {exit !(p35 && p34 && p35 < p34)}' CHANGELOG.md && echo "ORDER-OK"
```
Must print `ORDER-OK`.

Verify `kubectl apply --dry-run=client` accepts the renamed CR (only runs if `kubectl` is available in the container; skip otherwise):
```bash
if command -v kubectl >/dev/null 2>&1; then
  kubectl apply --dry-run=client -f task/executor/k8s/agent-claude.yaml
fi
```
Optional — zero exit code if run.

**Note:** Dev-cluster acceptance criteria from the spec Verification section (`kubectlquant -n dev get crd configs.agent.benjamin-borbe.de`, informer pickup observation, delete/re-apply flow) are manual post-merge checks — they cannot run inside the dark-factory container and are not part of prompt verification.
</verification>
