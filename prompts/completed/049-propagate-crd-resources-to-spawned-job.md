---
status: completed
summary: Reshaped AgentResources to nested requests/limits, propagated Resources through AgentConfiguration, config_resolver, and SpawnJob (including ephemeral-storage post-build patch), migrated agent-claude.yaml manifest, and extended tests in all three test files.
container: agent-049-propagate-crd-resources-to-spawned-job
dark-factory-version: v0.125.1
created: "2026-04-18T00:00:00Z"
queued: "2026-04-18T15:56:16Z"
started: "2026-04-18T15:56:19Z"
completed: "2026-04-18T16:06:18Z"
---

<summary>
- Agent Configs specifying CPU, memory, and ephemeral-storage in the CRD now reach the spawned K8s Job
- Agents that need 1Gi of memory (Claude Code CLI, trade-analysis) stop getting OOMKilled by the namespace LimitRange
- Agents without resources declared continue to use the existing K8s builder defaults
- Resource requests and limits are configured INDEPENDENTLY (not pinned to the same value)
- CRD schema gains a nested `resources: { requests: {...}, limits: {...} }` shape (breaking change to AgentResources)
- Existing K8s manifests using the old flat shape are migrated to the new nested shape
- Config resolution, deep-copy (tagged configurations), and job construction all carry the structured resources end to end
- Unit tests cover the new field on AgentConfiguration, the CRD-to-configuration conversion, and the Job container's final ResourceRequirements
</summary>

<objective>
Propagate the `Resources` field from the `Config` CRD (group `agent.benjamin-borbe.de/v1`) through
the executor's `AgentConfiguration` into the spawned K8s Job so the container gets the configured
CPU, memory, and ephemeral-storage as independent `Requests` and `Limits`. Today the field is
silently dropped, and pods inherit the namespace LimitRange default (50Mi) which OOMKills
Claude-Code-based agents. Additionally, reshape `AgentResources` so requests and limits can be
configured independently (current struct pins them to the same value).
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for the Definition of Done.

Key files (repo-relative to `~/Documents/workspaces/agent`):

- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`
  - `AgentResources` struct currently has flat fields `CPU`, `Memory`, `EphemeralStorage` (all `string`, JSON tags `cpu`, `memory`, `ephemeral-storage`).
  - `ConfigSpec.Resources *AgentResources` already exists with `json:"resources,omitempty"`.
  - `ConfigSpec.Equal` already compares `Resources` via `reflect.DeepEqual` — this continues to work after the reshape without code changes.

- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go`
  - Generated deepcopy for `AgentResources` is a trivial `*out = *in`. After the reshape it stays trivial (value-typed `AgentResourceList` embedded in `AgentResources`).

- `task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/agentresources.go`
  - Generated applyconfiguration. Must be updated/regenerated to match the new shape.

- `task/executor/pkg/k8s_connector.go` (~lines 154-161)
  - CRD JSONSchema builder for the `resources` property. Currently declares flat `cpu`, `memory`, `ephemeral-storage` children. Must be updated to the new nested `requests`/`limits` shape.

- `task/executor/pkg/agent_configuration.go`
  - `AgentConfiguration` struct has `Assignee`, `Image`, `Env`, `VolumeClaim`, `VolumeMountPath`, `SecretName`. No resources field today.
  - `TaggedConfigurations(branch string)` copies every field into a new struct (note: `Env` map is copied by reference — this prompt's new `Resources` field MUST be deep-copied). Any new field MUST be added here.

- `task/executor/pkg/agent_configuration_test.go`
  - Ginkgo v2 + Gomega. Follow the style of the existing `TaggedConfigurations` tests.

- `task/executor/pkg/config_resolver.go`
  - `convert(obj agentv1.Config, branch string) AgentConfiguration` maps CRD → AgentConfiguration. Today it does NOT copy `obj.Spec.Resources`.

- `task/executor/pkg/config_resolver_test.go`
  - Existing test "returns converted AgentConfiguration with image tag appended" covers the other fields (SecretName, VolumeClaim, VolumeMountPath). Extend the same pattern for Resources.

- `task/executor/pkg/spawner/job_spawner.go`
  - `SpawnJob` uses `k8s.NewContainerBuilder()` from `github.com/bborbe/k8s`.
  - Builder exposes `SetCpuLimit`, `SetCpuRequest`, `SetMemoryLimit`, `SetMemoryRequest` — use these independently when the corresponding config values are non-empty.
  - Builder does NOT expose ephemeral-storage setters. To set ephemeral-storage, modify the built `*batchv1.Job` after `jobBuilder.Build(ctx)`, similarly to how `applySecretEnvFrom` patches the job post-build.
  - Existing pattern to imitate: `applySecretEnvFrom(config, job)` in the same file.

- `task/executor/pkg/spawner/job_spawner_test.go`
  - Ginkgo v2 + Gomega with `k8s.io/client-go/kubernetes/fake`. Follow existing `SpawnJob` test style.

- K8s manifests that use the old flat `resources` shape and must be migrated:
  - `~/Documents/workspaces/agent/agent/claude/k8s/agent-claude.yaml`
  - `~/Documents/workspaces/trading/agent/trade-analysis/k8s/agent-trade-analysis.yaml`
  - (`~/Documents/workspaces/trading/agent/backtest/k8s/agent-backtest-agent.yaml` does NOT currently declare `resources` — no change needed.)

Important facts:

- The `github.com/bborbe/k8s` container builder has default resources (`cpuLimit="50m"`, `cpuRequest="20m"`, `memoryLimit="50Mi"`, `memoryRequest="20Mi"`). When a config provides a value for a specific setter, that setter overrides the default. When the config does NOT provide a value, those defaults must remain untouched (existing tests rely on them).
- **Requests and limits are INDEPENDENT.** Populating only `Requests.CPU` must leave the builder's default CPU limit untouched; populating only `Limits.Memory` must leave the builder's default memory request untouched.
- For ephemeral-storage, use the K8s resource name `corev1.ResourceEphemeralStorage` (string value `"ephemeral-storage"`). Parse the value with `resource.MustParse` from `k8s.io/apimachinery/pkg/api/resource`.
- The CRD JSON tag is `ephemeral-storage` (hyphen, not underscore).
- **Re-use `agentv1.AgentResources` directly on `pkg.AgentConfiguration`** (do NOT mirror into flat fields). Importing `agentv1` into `pkg` is acceptable per user direction. Use a pointer (`*agentv1.AgentResources`) to preserve the "nil means unset" semantic. This matches how `obj.Spec.Resources` is modelled on the CRD side and avoids duplication between CRD type and pkg type.
- Do NOT change any interface signatures (`JobSpawner`, `ConfigResolver`). Only struct field additions / CRD type reshape. Counterfeiter mocks should not need regeneration.
- **CRD apply order after merge:** the CRD schema must be applied BEFORE existing `Config` resources. Old-shape `Config` YAMLs will fail validation against the new schema — both manifests (`agent-claude.yaml`, `agent-trade-analysis.yaml`) are migrated in this prompt.
</context>

<requirements>

1. **Reshape `AgentResources` in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`.**

   Replace the existing flat struct:
   ```go
   // Old
   type AgentResources struct {
       CPU              string `json:"cpu,omitempty"`
       Memory           string `json:"memory,omitempty"`
       EphemeralStorage string `json:"ephemeral-storage,omitempty"`
   }
   ```
   with:
   ```go
   // AgentResources holds optional resource requests and limits for the agent container.
   type AgentResources struct {
       // Requests declares the minimum resources the container needs.
       Requests AgentResourceList `json:"requests,omitempty"`
       // Limits declares the maximum resources the container may use.
       Limits AgentResourceList `json:"limits,omitempty"`
   }

   // AgentResourceList describes a CPU / memory / ephemeral-storage triple
   // used by both Requests and Limits on AgentResources.
   type AgentResourceList struct {
       // CPU is the CPU resource value (e.g. "500m").
       CPU string `json:"cpu,omitempty"`
       // Memory is the memory resource value (e.g. "256Mi").
       Memory string `json:"memory,omitempty"`
       // EphemeralStorage is the ephemeral-storage resource value (e.g. "1Gi").
       EphemeralStorage string `json:"ephemeral-storage,omitempty"`
   }
   ```

   Value-typed fields on `AgentResources` (not pointers) — an omitted key in YAML becomes a zero `AgentResourceList{}` where all three strings are empty, and empty strings are treated as "unset" downstream.

2. **`ConfigSpec.Equal` requires no change.** The existing `reflect.DeepEqual(s.Resources, o.Resources)` comparison continues to work on the new shape. Verify this is still the case after the reshape; if Go's deep-equal semantics on the new nested struct do not match field-by-field comparison, rewrite `Equal` explicitly — otherwise leave it untouched.

3. **Update generated deepcopy in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go`.**

   Because `AgentResourceList` contains only value-typed strings, the generated `AgentResources.DeepCopyInto` can remain a trivial `*out = *in` (all sub-fields are value-copyable). Explicitly:
   - Keep `AgentResources.DeepCopyInto` / `DeepCopy` functions — they stay correct.
   - Add generated methods for the new `AgentResourceList` type:
     ```go
     // DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
     func (in *AgentResourceList) DeepCopyInto(out *AgentResourceList) {
         *out = *in
         return
     }

     // DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AgentResourceList.
     func (in *AgentResourceList) DeepCopy() *AgentResourceList {
         if in == nil {
             return nil
         }
         out := new(AgentResourceList)
         in.DeepCopyInto(out)
         return out
     }
     ```
   - Keep the existing `ConfigSpec.DeepCopyInto` logic for `Resources` (nil-safe new + deep-copy of `AgentResources`). The existing pattern `*out = new(AgentResources); **out = **in` still deep-copies correctly because `AgentResources` is made of value-typed `AgentResourceList`s which are themselves made of strings.
   - Preserve the `// Code generated by deepcopy-gen. DO NOT EDIT.` header and `//go:build !ignore_autogenerated` tag.

4. **Update generated applyconfiguration at `task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/agentresources.go`.**

   Replace the existing flat `AgentResourcesApplyConfiguration` with a new file matching the new shape. Approach:
   - Rewrite `agentresources.go` so `AgentResourcesApplyConfiguration` has `Requests *AgentResourceListApplyConfiguration` and `Limits *AgentResourceListApplyConfiguration` fields with `WithRequests` / `WithLimits` builder methods, plus a constructor `func AgentResources() *AgentResourcesApplyConfiguration`.
   - Create a NEW file `task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/agentresourcelist.go` containing `AgentResourceListApplyConfiguration` with `*string` fields `CPU`, `Memory`, `EphemeralStorage`, a constructor `func AgentResourceList() *AgentResourceListApplyConfiguration`, and `WithCPU` / `WithMemory` / `WithEphemeralStorage` builder methods.
   - Preserve the `// Code generated by applyconfiguration-gen. DO NOT EDIT.` header on both files.
   - Follow the exact style of the existing sibling files (`configspec.go`, old `agentresources.go`).
   - `task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/configspec.go` already has `Resources *AgentResourcesApplyConfiguration`; no change needed there (the inner type has changed but the reference stays the same).
   - `task/executor/k8s/client/applyconfiguration/utils.go` has a `ForKind` switch that currently includes `AgentResources`. Add a new case for `AgentResourceList`, returning `&agentbenjaminborbedev1.AgentResourceListApplyConfiguration{}` (match the exact import alias used by the file).
  - `task/executor/k8s/client/applyconfiguration/internal/internal.go` (if present) may embed a generated OpenAPI schema referencing `AgentResources` — inspect it and update if the schema is embedded; skip if the file does not exist or references no AgentResources symbol.

5. **Update the CRD JSONSchema in `task/executor/pkg/k8s_connector.go`.**

   Find the `"resources"` entry (~lines 154-161) and replace the flat shape with the nested `requests`/`limits` shape:
   ```go
   "resources": {
       Type: "object",
       Properties: map[string]apiextensionsv1.JSONSchemaProps{
           "requests": {
               Type: "object",
               Properties: map[string]apiextensionsv1.JSONSchemaProps{
                   "cpu":               {Type: "string"},
                   "memory":            {Type: "string"},
                   "ephemeral-storage": {Type: "string"},
               },
           },
           "limits": {
               Type: "object",
               Properties: map[string]apiextensionsv1.JSONSchemaProps{
                   "cpu":               {Type: "string"},
                   "memory":            {Type: "string"},
                   "ephemeral-storage": {Type: "string"},
               },
           },
       },
   },
   ```

6. **Add `Resources *agentv1.AgentResources` to `AgentConfiguration`** in `task/executor/pkg/agent_configuration.go`.

   - Add an import for `agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"` (use the canonical module path from `go.mod` — grep the existing `config_resolver.go` for the correct import path and reuse it verbatim).
   - Append the field with a doc comment:
     ```go
     // Resources declares optional resource requests and limits for the agent container.
     // Nil means "do not set, keep the k8s builder default".
     Resources *agentv1.AgentResources
     ```

7. **Propagate `Resources` in `TaggedConfigurations`** (same file).

   Inside the loop that constructs each result `AgentConfiguration`, add:
   ```go
   Resources: c.Resources.DeepCopy(),
   ```
   `DeepCopy()` on a nil pointer returns nil (see generated deepcopy), so this is safe when unset.

8. **Copy `Resources` in `convert`** in `task/executor/pkg/config_resolver.go`.

   In `convert(obj agentv1.Config, branch string) AgentConfiguration`, populate the new field via deep-copy:
   ```go
   return AgentConfiguration{
       Assignee:        obj.Spec.Assignee,
       Image:           obj.Spec.Image + ":" + branch,
       Env:             copyEnv(obj.Spec.Env),
       SecretName:      obj.Spec.SecretName,
       VolumeClaim:     obj.Spec.VolumeClaim,
       VolumeMountPath: obj.Spec.VolumeMountPath,
       Resources:       obj.Spec.Resources.DeepCopy(),
   }
   ```

9. **Apply resources in `SpawnJob`** in `task/executor/pkg/spawner/job_spawner.go`.

   a. Before `jobBuilder.Build(ctx)`, apply CPU and memory from the config independently. Place these calls immediately after `containerBuilder.SetEnvBuilder(envBuilder)`:
      ```go
      if config.Resources != nil {
          if v := config.Resources.Requests.CPU; v != "" {
              containerBuilder.SetCpuRequest(v)
          }
          if v := config.Resources.Limits.CPU; v != "" {
              containerBuilder.SetCpuLimit(v)
          }
          if v := config.Resources.Requests.Memory; v != "" {
              containerBuilder.SetMemoryRequest(v)
          }
          if v := config.Resources.Limits.Memory; v != "" {
              containerBuilder.SetMemoryLimit(v)
          }
      }
      ```

   b. After `jobBuilder.Build(ctx)` and `applySecretEnvFrom(config, job)`, call a new helper:
      ```go
      applyEphemeralStorage(config, job)
      ```
      Implement as:
      ```go
      // applyEphemeralStorage sets ephemeral-storage as Requests and/or Limits on the
      // first container of the job based on config.Resources.
      // Each value is applied independently — empty means "leave unset".
      // The bborbe/k8s container builder does not expose setters for ephemeral-storage,
      // so we patch the built job directly.
      func applyEphemeralStorage(config pkg.AgentConfiguration, job *batchv1.Job) {
          if config.Resources == nil {
              return
          }
          c := &job.Spec.Template.Spec.Containers[0]
          if v := config.Resources.Requests.EphemeralStorage; v != "" {
              if c.Resources.Requests == nil {
                  c.Resources.Requests = corev1.ResourceList{}
              }
              c.Resources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse(v)
          }
          if v := config.Resources.Limits.EphemeralStorage; v != "" {
              if c.Resources.Limits == nil {
                  c.Resources.Limits = corev1.ResourceList{}
              }
              c.Resources.Limits[corev1.ResourceEphemeralStorage] = resource.MustParse(v)
          }
      }
      ```

   c. Add the required imports to `job_spawner.go`:
      ```go
      "k8s.io/apimachinery/pkg/api/resource"
      ```
      (`corev1` and `batchv1` already imported.)

10. **Migrate K8s manifests** using the old flat shape to the new nested shape.

    - `agent/claude/k8s/agent-claude.yaml` (in THIS repo): change
      ```yaml
      resources:
        cpu: 500m
        memory: 1Gi
        ephemeral-storage: 2Gi
      ```
      to
      ```yaml
      resources:
        requests:
          cpu: 500m
          memory: 1Gi
          ephemeral-storage: 2Gi
        limits:
          cpu: 500m
          memory: 1Gi
          ephemeral-storage: 2Gi
      ```

    **Out of scope for this prompt (reviewer must raise companion PR in `trading` repo):**
    - `trading/agent/trade-analysis/k8s/agent-trade-analysis.yaml`: same migration (identical values).
    - `trading/agent/backtest/k8s/agent-backtest-agent.yaml`: no change (no `resources` block today).

    Surface the companion-PR requirement in the final report — the dark-factory pipeline runs in the `agent` repo worktree and cannot edit files outside it.

11. **Extend unit tests for `AgentConfigurations.TaggedConfigurations`** in `task/executor/pkg/agent_configuration_test.go`.

    - In the `BeforeEach` fixture, set `Resources` on one of the configs to a populated `&agentv1.AgentResources{Requests: agentv1.AgentResourceList{CPU: "500m", Memory: "1Gi", EphemeralStorage: "2Gi"}, Limits: agentv1.AgentResourceList{CPU: "1", Memory: "2Gi", EphemeralStorage: "4Gi"}}` (use DIFFERENT values for requests vs limits so the test actually proves independence).
    - Add a new `It("preserves resource requests and limits independently", ...)` asserting after `TaggedConfigurations("prod")`:
      ```go
      Expect(result[...].Resources).NotTo(BeNil())
      Expect(result[...].Resources.Requests.CPU).To(Equal("500m"))
      Expect(result[...].Resources.Limits.CPU).To(Equal("1"))
      Expect(result[...].Resources.Requests.Memory).To(Equal("1Gi"))
      Expect(result[...].Resources.Limits.Memory).To(Equal("2Gi"))
      Expect(result[...].Resources.Requests.EphemeralStorage).To(Equal("2Gi"))
      Expect(result[...].Resources.Limits.EphemeralStorage).To(Equal("4Gi"))
      ```
    - Add a second `It(...)` asserting `result[...].Resources` is a DIFFERENT pointer than the input (deep-copied, not aliased). Mutate the output and re-read the input to prove independence.

12. **Extend `ConfigResolver` test** in `task/executor/pkg/config_resolver_test.go`.

    - Extend the existing `It("returns converted AgentConfiguration with image tag appended", ...)` to set on `Spec`:
      ```go
      Resources: &agentv1.AgentResources{
          Requests: agentv1.AgentResourceList{CPU: "500m", Memory: "1Gi", EphemeralStorage: "2Gi"},
          Limits:   agentv1.AgentResourceList{CPU: "1", Memory: "2Gi", EphemeralStorage: "4Gi"},
      },
      ```
      and add expectations covering all six populated fields, plus that the pointer is non-nil.
    - Add a second `It("leaves Resources nil when Spec.Resources is nil", ...)` covering the nil-pointer branch.

13. **Extend `SpawnJob` tests** in `task/executor/pkg/spawner/job_spawner_test.go`.

    Add a new `It("applies resource requests and limits from config.Resources", ...)`:
    - Build a `pkg.AgentConfiguration` with `Resources: &agentv1.AgentResources{Requests: agentv1.AgentResourceList{CPU: "500m", Memory: "1Gi", EphemeralStorage: "2Gi"}, Limits: agentv1.AgentResourceList{CPU: "1", Memory: "2Gi", EphemeralStorage: "4Gi"}}`.
    - Call `jobSpawner.SpawnJob(ctx, task, config)`, list jobs, grab `container := jobs.Items[0].Spec.Template.Spec.Containers[0]`.
    - Assert independent values for request vs limit on CPU, memory, ephemeral-storage.

    Add a second `It("uses k8s builder defaults when Resources is nil", ...)` verifying a config with `Resources: nil` yields the builder defaults (`cpu` limit `50m`, request `20m`, `memory` limit `50Mi`, request `20Mi`) and no ephemeral-storage entry in `Requests` or `Limits`.

    Add a third `It("leaves CPU limit at builder default when only Requests.CPU is set", ...)` — one-sided config:
    ```go
    Resources: &agentv1.AgentResources{
        Requests: agentv1.AgentResourceList{CPU: "500m"},
    }
    ```
    Assert `container.Resources.Requests.Cpu().String() == "500m"` AND `container.Resources.Limits.Cpu().String() == "50m"` (builder default untouched). This proves independence.

14. **Regenerate counterfeiter mocks only if interface signatures changed.** `JobSpawner`, `ConfigResolver`, and sibling interfaces are unchanged by this prompt — skip regeneration. If `make precommit` complains about stale mocks, run `go generate ./...` and commit only the resulting mock diff.

15. **Update `CHANGELOG.md`** in the `agent` repo.

    There is NO existing `## Unreleased` section today — the file starts with `## v0.38.0`. CREATE a new `## Unreleased` header immediately above `## v0.38.0`, with these bullets:
    ```
    ## Unreleased

    - BREAKING: `agent.benjamin-borbe.de/v1` `AgentResources` now has nested `requests` and `limits` sub-objects instead of flat `cpu`/`memory`/`ephemeral-storage`. Update existing `Config` manifests before re-applying. Apply the updated CRD first, then re-apply any `Config` resources.
    - Propagate `Resources` from `Config` CRD (cpu/memory/ephemeral-storage, requests and limits independent) to spawned agent Job container; fixes OOMKill of Claude-Code-based agents that inherited the namespace LimitRange default of 50Mi.
    ```

16. **Do NOT modify** (out of scope):
    - `task/controller/**`
    - `go.mod` / `go.sum` / `vendor/**`

</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Do NOT bump Go version or dependencies.
- Do NOT modify the controller package.
- Do NOT change `JobSpawner` or `ConfigResolver` interface signatures.
- Use `github.com/bborbe/errors` for any new error wrapping — never `fmt.Errorf`.
- All new exported identifiers (types, fields, functions) need doc comments.
- Tests use Ginkgo v2 + Gomega. Match existing style.
- Use repo-relative paths in code references; absolute `~/Documents/workspaces/...` paths only for cross-repo YAMLs (trading repo).
- When a config's resource value is empty, the k8s container builder's default must remain untouched.
- Requests and limits must be configured INDEPENDENTLY — setting one must not affect the other.
</constraints>

<verification>
Run precommit from the executor module:

```bash
cd task/executor && make precommit
```
Must exit 0.

Verify the reshaped CRD type:

```bash
grep -n "AgentResourceList\|Requests\|Limits" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Must show the new nested shape.

Verify CRD schema builder was updated:

```bash
grep -n -A2 "\"requests\"\|\"limits\"" task/executor/pkg/k8s_connector.go
```
Must show both sub-objects under `resources`.

Verify deep-copy generated code:

```bash
grep -n "AgentResourceList" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
```
Must show `DeepCopyInto` / `DeepCopy` for the new type.

Verify applyconfiguration was updated:

```bash
ls task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/
grep -n "Requests\|Limits" task/executor/k8s/client/applyconfiguration/agent.benjamin-borbe.de/v1/agentresources.go
```
Must list `agentresourcelist.go` and show the new fields on the apply config.

Verify AgentConfiguration got the Resources field:

```bash
grep -n "Resources" task/executor/pkg/agent_configuration.go task/executor/pkg/config_resolver.go task/executor/pkg/spawner/job_spawner.go
```
Must show the field declared, deep-copied in `TaggedConfigurations`, populated in `convert`, and applied in `SpawnJob` / `applyEphemeralStorage`.

Verify test coverage:

```bash
grep -n "Resources\|AgentResourceList" \
  task/executor/pkg/agent_configuration_test.go \
  task/executor/pkg/config_resolver_test.go \
  task/executor/pkg/spawner/job_spawner_test.go
```
Must show test cases referencing the new shape in all three files.

Verify K8s manifest migrated (agent repo only — trading repo changes are reported separately):

```bash
grep -n -A2 "resources:" agent/claude/k8s/agent-claude.yaml
```
Must show `requests:` as the next key, not `cpu:` directly.

Verify the CHANGELOG entry:

```bash
grep -n -A3 "Unreleased" CHANGELOG.md | head -10
```
Must show both new bullets (BREAKING note + propagation fix).

**Report to reviewer (not a repo check):**
- The trading-repo manifests (`agent-trade-analysis.yaml`) are in `~/Documents/workspaces/trading/` and require a separate PR in that repo.
- **Post-merge apply order:** apply the updated CRD schema FIRST (from `agent` repo deploy), THEN re-apply `Config` resources. Old-shape Configs will fail validation against the new schema.

Manual post-merge smoke test (NOT part of DoD, record here for reviewer):

```
kubectlquant -n dev describe pod <agent-claude-pod>
# Expect: Requests: cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi
#         Limits:   cpu: 500m, memory: 1Gi, ephemeral-storage: 2Gi
```
</verification>
