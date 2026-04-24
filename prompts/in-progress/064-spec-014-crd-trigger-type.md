---
status: committing
spec: [014-configurable-task-triggers]
summary: Added Trigger struct to Config CRD types with Equal/Validate updates, wired through AgentConfiguration and config_resolver, regenerated deepcopy, added comprehensive Ginkgo tests, and updated docs/CHANGELOG.
container: agent-064-spec-014-crd-trigger-type
dark-factory-version: v0.132.0
created: "2026-04-23T21:15:00Z"
queued: "2026-04-24T07:11:03Z"
started: "2026-04-24T07:16:47Z"
---

<summary>
- Config CRD gains an optional `spec.trigger` object with optional `phases` and `statuses` string-list fields
- A new `Trigger` struct (with `Phases []domain.TaskPhase` and `Statuses []domain.TaskStatus`) is added to the v1 types package
- `ConfigSpec.Equal` returns false when two specs differ only by `Trigger` (nil vs set, same vs different lists, same vs reordered lists)
- `ConfigSpec.Validate` rejects any entry in `Trigger.Phases` or `Trigger.Statuses` outside the canonical domain vocabulary; nil/empty `Trigger` passes validation
- `AgentConfiguration` gains a `Trigger *agentv1.Trigger` field, wired from `ConfigSpec.Trigger` in `config_resolver.go`
- `make generatek8s` regenerates `zz_generated.deepcopy.go` for the new nested struct
- Types-level Ginkgo tests cover Equal (nil vs set, different, reordered), Validate (nil, empty list, valid entries, invalid entries), and JSON round-trip for the new field
- `docs/agent-crd-specification.md` documents the new `spec.trigger` field with phases/statuses semantics and the default-fallback contract
- Existing manifests and Config CRs continue to apply unchanged (no new required fields)
</summary>

<objective>
Add the `Trigger` struct to the `Config` CRD types so the executor can later (prompt 2) consult per-Config trigger conditions instead of a hardcoded phase/status allow-list. This prompt is purely additive: new Go types, updated Equal/Validate, wired through to AgentConfiguration, code generation, and documentation. No handler logic changes here — those are prompt 2's scope.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping, counterfeiter annotations
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, suite files
- `go-validation-framework-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — validation.All/Any/Name from `github.com/bborbe/validation`
- `go-kubernetes-crd-controller-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — CRD type and schema patterns
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf

**Key files to read before editing:**

- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` — `ConfigSpec` struct (~line 33), `Equal` method (~line 111), `Validate` method (~line 124)
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go` — existing Ginkgo tests for ConfigSpec; add new tests here
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct; add `Trigger *agentv1.Trigger` here
- `task/executor/pkg/config_resolver.go` — `convert()` function (~line 66) that maps `ConfigSpec` to `AgentConfiguration`; wire `Trigger` here
- `task/executor/pkg/k8s_connector.go` — `desiredCRDSpec()` builds the OpenAPIV3Schema; add `trigger` property here
- `docs/agent-crd-specification.md` — authoritative CRD field table; add `spec.trigger` section

Run this to confirm the current ConfigSpec fields and Equal/Validate signatures:
```bash
grep -n "func (s ConfigSpec)" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```

Run this to see if `domain` is already imported in types.go:
```bash
head -30 task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```

Run this to find where `AgentConfiguration{` is built from a Config:
```bash
grep -rn "AgentConfiguration{" task/executor/pkg/
```
</context>

<requirements>

1. **Add `Trigger` struct to `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`**

   Add the following two types immediately before the `ConfigSpec` struct (or after the existing resource types — pick the location that reads most naturally next to the other spec-level types):

   ```go
   // Trigger declares the per-agent phase and status conditions under which the executor spawns a Job.
   // Absent or empty lists fall back to the default allow-list (phases: planning/in_progress/ai_review; statuses: in_progress).
   type Trigger struct {
       Phases   domain.TaskPhases   `json:"phases,omitempty"`
       Statuses domain.TaskStatuses `json:"statuses,omitempty"`
   }
   ```

   Where `domain` is `github.com/bborbe/vault-cli/pkg/domain`. Add the import if it is not already present in types.go.

   Add `Trigger *Trigger` as the last field in `ConfigSpec`:
   ```go
   Trigger *Trigger `json:"trigger,omitempty"`
   ```

2. **Update `ConfigSpec.Equal` in types.go**

   The existing method compares fields explicitly. Add `Trigger` comparison at the end of the condition chain using `reflect.DeepEqual`:
   ```go
   reflect.DeepEqual(s.Trigger, o.Trigger)
   ```
   Place this after the existing `PriorityClassName` comparison so that two specs differing only in their `Trigger` field compare as not equal.

3. **Update `ConfigSpec.Validate` in types.go**

   Add validation of the new `Trigger` field after the existing `VolumeMountPath`/`VolumeClaim` check. The rules are:
   - A nil `Trigger` is valid (no check needed).
   - A non-nil `Trigger` with empty `Phases` and empty `Statuses` is valid.
   - Each entry in `Trigger.Phases` must pass `domain.TaskPhase.Validate()`. If any entry is invalid, return a wrapped validation error naming the invalid value.
   - Each entry in `Trigger.Statuses` must pass `domain.TaskStatus.Validate()`. Same rule.

   Use the `validation` package already used in the method:
   ```go
   if s.Trigger != nil {
       for _, phase := range s.Trigger.Phases {
           if err := phase.Validate(ctx); err != nil {
               return errors.Wrapf(ctx, err, "invalid trigger phase %q", phase)
           }
       }
       for _, status := range s.Trigger.Statuses {
           if err := status.Validate(ctx); err != nil {
               return errors.Wrapf(ctx, err, "invalid trigger status %q", status)
           }
       }
   }
   ```

   Use `errors.Wrapf` from `github.com/bborbe/errors` — never `fmt.Errorf`.

4. **Add `Trigger` to `AgentConfiguration` in `task/executor/pkg/agent_configuration.go`**

   Add as the last field in the `AgentConfiguration` struct, using the CRD type:
   ```go
   Trigger *agentv1.Trigger
   ```

   where `agentv1` is already imported (or add the import `agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"`).

5. **Wire `Trigger` in `task/executor/pkg/config_resolver.go`**

   In the `convert(obj agentv1.Config, branch string)` function (at line 66) that builds `AgentConfiguration` from a `Config` CR, add:
   ```go
   Trigger: obj.Spec.Trigger,
   ```
   The receiver is `obj`, not `config`. Place this mapping alongside the other field assignments.

   **Note:** `ConfigSpec.Trigger` is `*Trigger` (a pointer to a struct defined in the same package). The generated deepcopy will handle deep-copying it; the direct copy is safe since the handler only reads the trigger (no mutation).

6. **Propagate `Trigger` in `AgentConfigurations.TaggedConfigurations` in `task/executor/pkg/agent_configuration.go`**

   `TaggedConfigurations` (line 53) rebuilds `AgentConfiguration` field-by-field. If the new `Trigger` field is not added to the rebuild, prompt 2's per-Config trigger feature will silently no-op because the handler will always see `nil` trigger. Add `Trigger: c.Trigger,` to the struct literal at line 56, alongside the other field assignments. If there are any existing tests in `agent_configuration_test.go` that cover `TaggedConfigurations`, extend them to assert `Trigger` is propagated.

7. **Update `configSpecSchema()` in `task/executor/pkg/k8s_connector.go`**

   The spec-level `Properties` map lives in `configSpecSchema()` (line 147), which is wrapped by `desiredCRDSpec()` at line 117. Edit `configSpecSchema` — NOT `desiredCRDSpec`. In its `Properties` map, add a `"trigger"` entry after `"priorityClassName"`:
   ```go
   "trigger": {
       Type: "object",
       Properties: map[string]apiextensionsv1.JSONSchemaProps{
           "phases": {
               Type: "array",
               Items: &apiextensionsv1.JSONSchemaPropsOrArray{
                   Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
               },
           },
           "statuses": {
               Type: "array",
               Items: &apiextensionsv1.JSONSchemaPropsOrArray{
                   Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"},
               },
           },
       },
   },
   ```
   `trigger` is NOT added to the `Required` slice — it remains optional.

   Read the existing `configSpecSchema()` implementation first to match the exact type names and import aliases used for `apiextensionsv1`.

8. **Run code generation (REQUIRED — not part of precommit)**

   ```bash
   cd task/executor && make generatek8s
   cd task/executor && make ensure
   ```

   Both must exit 0. After running, verify the deepcopy file was updated:
   ```bash
   grep -n "Trigger" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
   ```
   Must show the `Trigger` field being deep-copied in `DeepCopyInto`. If the field is absent, `make generatek8s` did not run correctly — investigate before continuing.

9. **Add types-level Ginkgo tests to `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go`**

   Read the existing test file first to understand the `Describe`/`Context`/`It` structure and helper patterns already in use. The existing file declares `ctx` inside each `Describe` via `BeforeEach(func() { ctx = context.Background() })` — reuse that pattern; do NOT add a package-level `var ctx`.

   Place the Equal/Validate tests **inside the existing `Describe("ConfigSpec", ...)` block**. Place the JSON round-trip tests in a **new sibling** `Describe("JSON round-trip for trigger", ...)` block to mirror the existing `Describe("JSON round-trip for priorityClassName", ...)` structure.

   a. `Equal` tests for the new `Trigger` field:
   ```go
   Context("Trigger field", func() {
       It("returns false when one spec has Trigger nil and other has Trigger set", func() {
           a := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: nil}
           b := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhasePlanning}}}
           Expect(a.Equal(b)).To(BeFalse())
       })
       It("returns false when Triggers have different Phases", func() {
           a := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhasePlanning}}}
           b := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhaseAIReview}}}
           Expect(a.Equal(b)).To(BeFalse())
       })
       It("returns false when Phases are same values but different order", func() {
           a := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhasePlanning, domain.TaskPhaseAIReview}}}
           b := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m",
               Trigger: &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhaseAIReview, domain.TaskPhasePlanning}}}
           Expect(a.Equal(b)).To(BeFalse())
       })
       It("returns true when both Triggers are nil", func() {
           a := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m", Trigger: nil}
           b := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m", Trigger: nil}
           Expect(a.Equal(b)).To(BeTrue())
       })
       It("returns true when Triggers are identical", func() {
           t := &agentv1.Trigger{
               Phases:   domain.TaskPhases{domain.TaskPhasePlanning},
               Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
           }
           a := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m", Trigger: t}
           b := agentv1.ConfigSpec{Assignee: "x", Image: "y", Heartbeat: "1m", Trigger: t}
           Expect(a.Equal(b)).To(BeTrue())
       })
   })
   ```

   b. `Validate` tests for the new `Trigger` field:
   ```go
   Context("Trigger validation", func() {
       baseSpec := func() agentv1.ConfigSpec {
           return agentv1.ConfigSpec{Assignee: "agent", Image: "img:latest", Heartbeat: "1m"}
       }
       It("passes with nil Trigger", func() {
           spec := baseSpec()
           Expect(spec.Validate(ctx)).To(Succeed())
       })
       It("passes with empty-list Trigger (both lists empty)", func() {
           spec := baseSpec()
           spec.Trigger = &agentv1.Trigger{}
           Expect(spec.Validate(ctx)).To(Succeed())
       })
       It("passes with valid phase entries", func() {
           spec := baseSpec()
           spec.Trigger = &agentv1.Trigger{
               Phases: domain.TaskPhases{domain.TaskPhasePlanning, domain.TaskPhaseAIReview},
           }
           Expect(spec.Validate(ctx)).To(Succeed())
       })
       It("passes with valid status entries", func() {
           spec := baseSpec()
           spec.Trigger = &agentv1.Trigger{
               Statuses: domain.TaskStatuses{domain.TaskStatusInProgress, domain.TaskStatusCompleted},
           }
           Expect(spec.Validate(ctx)).To(Succeed())
       })
       It("fails with an invalid phase entry", func() {
           spec := baseSpec()
           spec.Trigger = &agentv1.Trigger{
               Phases: domain.TaskPhases{"bogus_phase"},
           }
           Expect(spec.Validate(ctx)).NotTo(Succeed())
       })
       It("fails with an invalid status entry", func() {
           spec := baseSpec()
           spec.Trigger = &agentv1.Trigger{
               Statuses: domain.TaskStatuses{"bogus_status"},
           }
           Expect(spec.Validate(ctx)).NotTo(Succeed())
       })
   })
   ```

   Adjust variable names (`ctx`, `agentv1`, `domain`) to match the imports already used in the test file. Reuse the `ctx` set by the existing `BeforeEach` — do not declare a new one.

   c. JSON round-trip tests for `Trigger` (in a new sibling `Describe("JSON round-trip for trigger", ...)` block):
   ```go
   It("round-trips trigger through JSON", func() {
       spec := agentv1.ConfigSpec{
           Assignee:  "agent",
           Image:     "img:latest",
           Heartbeat: "1m",
           Trigger: &agentv1.Trigger{
               Phases:   domain.TaskPhases{domain.TaskPhasePlanning},
               Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
           },
       }
       data, err := json.Marshal(spec)
       Expect(err).To(BeNil())
       var decoded agentv1.ConfigSpec
       Expect(json.Unmarshal(data, &decoded)).To(Succeed())
       Expect(decoded.Trigger).NotTo(BeNil())
       Expect(decoded.Trigger.Phases).To(ConsistOf(domain.TaskPhasePlanning))
       Expect(decoded.Trigger.Statuses).To(ConsistOf(domain.TaskStatusInProgress))
   })
   It("omits trigger from JSON when nil", func() {
       spec := agentv1.ConfigSpec{Assignee: "agent", Image: "img:latest", Heartbeat: "1m"}
       data, err := json.Marshal(spec)
       Expect(err).To(BeNil())
       Expect(string(data)).NotTo(ContainSubstring("trigger"))
   })
   ```

10. **Update `docs/agent-crd-specification.md`**

   The existing field table uses **3 columns** (`Field | Required | Description`) at line 37. Match that exact format — do not add extra columns. The existing `spec.priorityClassName` row (line 47) is already malformed (4 cells instead of 3); do not propagate that bug. Add rows AFTER `spec.priorityClassName`:

   ```
   | `spec.trigger` | no | Per-agent trigger conditions (optional nested object with `phases` and `statuses` lists). When absent or empty, defaults apply: phases `[planning, in_progress, ai_review]` and statuses `[in_progress]`. |
   | `spec.trigger.phases` | no | Task phases that allow spawning. Valid values: `todo`, `planning`, `in_progress`, `ai_review`, `human_review`, `done`. Empty or absent means default phases apply. |
   | `spec.trigger.statuses` | no | Task statuses that allow spawning. Valid values: `todo`, `in_progress`, `backlog`, `completed`, `hold`, `aborted`. Empty or absent means default statuses apply. |
   ```

   After the table, add a prose paragraph describing the default-fallback contract: "If `spec.trigger` is omitted, or if `trigger.phases` / `trigger.statuses` are absent or empty, the executor applies its built-in defaults (phases: `planning`, `in_progress`, `ai_review`; statuses: `in_progress`). There is no way to configure a Config that triggers on nothing — an empty list is equivalent to the field being absent."

11. **Update `CHANGELOG.md` at repo root**

    Check for existing `## Unreleased` first:
    ```bash
    grep -n "^## Unreleased" CHANGELOG.md | head -3
    ```
    If it exists, append to it. Otherwise insert immediately above the first `## v` heading:
    ```markdown
    ## Unreleased

    - feat: Config CRD gains optional spec.trigger with phases/statuses lists; ConfigSpec.Equal and Validate updated; AgentConfiguration.Trigger wired from config resolver; deepcopy regenerated
    ```

12. **Run tests**

    ```bash
    cd task/executor && make test
    ```
    Must exit 0.

</requirements>

<constraints>
- The Config CRD schema must remain backward compatible: Configs without `trigger` must round-trip and produce identical spawn decisions (handled in prompt 2)
- The verdict/result schema (`{status, message, output}`) is unchanged
- The Kafka task-event envelope is unchanged
- `ConfigSpec.Equal` and `ConfigSpec.Validate` must both be updated — omitting either breaks controller informer logic or admission
- Default phases are exactly `{planning, in_progress, ai_review}`. Default status is exactly `{in_progress}`. Do not invent new defaults.
- `trigger: {}` (empty object, both lists absent) is treated identically to `trigger` being omitted — valid, defaults apply
- Generated deepcopy (`zz_generated.deepcopy.go`) must be regenerated via `make generatek8s` — do NOT hand-write deepcopy
- `make generatek8s` and `make ensure` must be run BEFORE `make precommit`; they are not called by precommit
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`
- Do NOT touch `task/controller/`, `prompt/`, `lib/`, or `agent/claude/`
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `cd task/executor && make precommit` must exit 0
</constraints>

<verification>
Verify `Trigger` struct is in types.go:
```bash
grep -n "type Trigger struct" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Must show the struct definition.

Verify `Trigger` field is in `ConfigSpec`:
```bash
grep -n "Trigger" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Must show both the struct and the field in ConfigSpec with `json:"trigger,omitempty"`.

Verify deepcopy was regenerated:
```bash
grep -n "Trigger" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
```
Must show Trigger being copied in `DeepCopyInto`.

Verify `AgentConfiguration` has `Trigger`:
```bash
grep -n "Trigger" task/executor/pkg/agent_configuration.go
```
Must show the field.

Verify `config_resolver.go` wires `Trigger`:
```bash
grep -n "Trigger" task/executor/pkg/config_resolver.go
```
Must show the mapping assignment.

Verify OpenAPIV3Schema has `trigger`:
```bash
grep -n "trigger" task/executor/pkg/k8s_connector.go
```
Must show the property definition.

Run tests:
```bash
cd task/executor && make test
```
Must exit 0.

Run precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
