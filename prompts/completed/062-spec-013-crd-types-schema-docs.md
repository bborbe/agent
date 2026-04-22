---
status: completed
spec: [013-agent-concurrency-via-priority-class]
summary: Added PriorityClassName field to ConfigSpec, AgentConfiguration, and CRD OpenAPIV3Schema; wired the field through the convert function; updated docs; added Ginkgo round-trip and Equal tests.
container: agent-062-spec-013-crd-types-schema-docs
dark-factory-version: v0.132.0
created: "2026-04-22T00:00:00Z"
queued: "2026-04-22T05:25:31Z"
started: "2026-04-22T05:25:32Z"
completed: "2026-04-22T05:31:55Z"
branch: dark-factory/agent-concurrency-via-priority-class
---

<summary>
- `ConfigSpec` Go struct gains an optional `PriorityClassName string` field that the CRD schema now declares
- `AgentConfiguration` (the in-memory runtime representation) gains the same field, threaded from `ConfigSpec`
- `desiredCRDSpec()` OpenAPIV3Schema is updated so `priorityClassName` is a validated, optional string property
- `make generatek8s` + `make ensure` are run explicitly to regenerate deepcopy and clientset for the updated type
- `docs/agent-crd-specification.md` documents `priorityClassName` in the authoritative field table
- The `maxConcurrentJobs` "Future Extensions" row is removed and replaced with a note that concurrency is enforced via `priorityClassName` + K8s `ResourceQuota`
- A Ginkgo unit test verifies that encoding and decoding a Config CR preserves the `priorityClassName` field (round-trip test)
- `cd task/executor && make precommit` passes after all changes
</summary>

<objective>
Add the `priorityClassName` field to the `Config` CRD so that downstream executor code (prompt 2) can read it and stamp K8s Jobs. This prompt is purely additive: add the field to the Go type, wire it through to `AgentConfiguration`, update the OpenAPIV3Schema, run code generation, and update the CRD documentation. No Job-spawning or K8s manifest changes happen here — those are prompt 2's scope.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, counterfeiter annotations, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages
- `go-kubernetes-crd-controller-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — CRD type and schema patterns

**Key files to read before editing:**

- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` — `ConfigSpec` struct; add `PriorityClassName` here
- `task/executor/pkg/k8s_connector.go` — `desiredCRDSpec()` function builds the OpenAPIV3Schema; add `priorityClassName` property here
- `task/executor/pkg/agent_configuration.go` — `AgentConfiguration` struct; add `PriorityClassName` here
- `docs/agent-crd-specification.md` — full authoritative CRD documentation; update field table and Future Extensions section

To find where `ConfigSpec` is mapped to `AgentConfiguration`, run:
```bash
grep -rn "AgentConfiguration{" task/executor/pkg/
```
Read that file; add `PriorityClassName: c.Spec.PriorityClassName` (or equivalent variable name) to the mapping.
</context>

<requirements>

1. **Add `PriorityClassName` to `ConfigSpec` in `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go`**

   a. Add as the last field in `ConfigSpec`, after `VolumeMountPath`:
   ```go
   PriorityClassName string `json:"priorityClassName,omitempty"`
   ```

   The field is optional (no `required` tag). The `omitempty` JSON tag ensures the field is absent from the serialized CR when not set.

   b. Update the `ConfigSpec.Equal` method at types.go:109 to include the new field — it compares fields explicitly (not `reflect.DeepEqual` on the whole struct), so omitting this line means CRs differing only in `priorityClassName` would compare equal and reconciliation would miss changes:
   ```go
   s.VolumeMountPath == o.VolumeMountPath &&
   s.PriorityClassName == o.PriorityClassName &&
   reflect.DeepEqual(s.Env, o.Env) &&
   ```

2. **Add `PriorityClassName` to `AgentConfiguration` in `task/executor/pkg/agent_configuration.go`**

   Add as the last field in the `AgentConfiguration` struct:
   ```go
   PriorityClassName string
   ```

3. **Wire `PriorityClassName` from `ConfigSpec` to `AgentConfiguration`**

   Find the mapping function by running:
   ```bash
   grep -rn "AgentConfiguration{" task/executor/pkg/
   ```
   Read that file. In the struct literal (or setter) that builds `AgentConfiguration` from a `Config` CR, add:
   ```go
   PriorityClassName: config.Spec.PriorityClassName,
   ```
   where `config` is the `*agentv1.Config` variable. Match the variable name to what the file already uses.

4. **Update `desiredCRDSpec()` in `task/executor/pkg/k8s_connector.go`**

   In the `Properties` map inside the `"spec"` schema, add a new entry after `"volumeMountPath"`:
   ```go
   "priorityClassName": {
       Type:    "string",
       Pattern: "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$",
   },
   ```

   This field is NOT added to the `Required` slice — it remains optional.

5. **Run code generation (REQUIRED — not part of precommit)**

   ```bash
   cd task/executor && make generatek8s
   cd task/executor && make ensure
   ```

   These regenerate `zz_generated.deepcopy.go` and the typed clientset. Both must exit 0 before continuing.

   After running, verify the deepcopy file includes `PriorityClassName`:
   ```bash
   grep -n "PriorityClassName" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
   ```
   If that file doesn't have the field, check whether `make generatek8s` ran correctly.

6. **Add a round-trip unit test**

   Find the existing test file for the CRD connector or types. Run:
   ```bash
   find task/executor -name "*_test.go" | xargs grep -l "ConfigSpec\|desiredCRD\|OpenAPI" 2>/dev/null
   ```
   If a matching file exists, add the test there. If none exists, create `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go`.

   Add a Ginkgo test that verifies `priorityClassName` survives JSON encode/decode:
   ```go
   package v1_test

   import (
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
       agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
       "encoding/json"
   )

   var _ = Describe("ConfigSpec", func() {
       It("round-trips priorityClassName through JSON", func() {
           spec := agentv1.ConfigSpec{
               Assignee:          "claude-agent",
               Image:             "example/image:latest",
               Heartbeat:         "30m",
               PriorityClassName: "agent-claude",
           }
           data, err := json.Marshal(spec)
           Expect(err).To(BeNil())
           var decoded agentv1.ConfigSpec
           Expect(json.Unmarshal(data, &decoded)).To(Succeed())
           Expect(decoded.PriorityClassName).To(Equal("agent-claude"))
       })

       It("omits priorityClassName from JSON when empty", func() {
           spec := agentv1.ConfigSpec{
               Assignee:  "claude-agent",
               Image:     "example/image:latest",
               Heartbeat: "30m",
           }
           data, err := json.Marshal(spec)
           Expect(err).To(BeNil())
           Expect(string(data)).NotTo(ContainSubstring("priorityClassName"))
       })
   })
   ```

   If a Ginkgo suite file (`suite_test.go`) does not exist in the package, create one:
   ```go
   package v1_test

   import (
       "testing"
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
   )

   func TestV1(t *testing.T) {
       RegisterFailHandler(Fail)
       RunSpecs(t, "V1 Suite")
   }
   ```

7. **Update `docs/agent-crd-specification.md`**

   a. In the authoritative field table (the section listing `spec` fields), add a new row for `priorityClassName`:

   ```
   | `priorityClassName` | string | No | — | Kubernetes PriorityClass name to stamp onto spawned Job PodTemplates. When set, a matching `ResourceQuota` scoped to this class enforces the concurrent pod cap. Absent means no PriorityClass (unbounded concurrency, pre-spec-013 behavior). |
   ```

   b. In the "Future Extensions" section, locate the `maxConcurrentJobs` row and replace it (and any adjacent explanation) with:

   ```
   Concurrency is now enforced K8s-natively: set `spec.priorityClassName` on a Config CR and apply a `ResourceQuota` with a `scopeSelector` matching that PriorityClass. The quota caps how many pods of that class can run simultaneously in a namespace; Jobs beyond the cap create successfully but block on pod admission until a slot frees. See `agent/claude/k8s/` for the four-file bundle (PriorityClass + per-env ResourceQuota + updated Config CR).
   ```

   Remove the `maxConcurrentJobs` row entirely.

8. **Run tests**

   ```bash
   cd task/executor && make test
   ```
   Must exit 0.

</requirements>

<constraints>
- `lib.Task` schema and `agent-task-v1-event` topic are unchanged — do NOT touch `lib/`
- Existing idempotency behaviour (`current_job` label guard) is untouched
- `retry_count` semantics from spec 011 are preserved
- Task controller is unaware of the quota — do NOT touch `task/controller/`
- Do NOT commit — dark-factory handles git
- Do NOT add `priorityClassName` to the `Required` slice in the OpenAPIV3Schema — the field is optional
- Use `github.com/bborbe/errors` for any error wrapping — never `fmt.Errorf`
- All existing tests must pass
- `make generatek8s` and `make ensure` must be run explicitly BEFORE `make precommit`; they are not called by precommit
- `cd task/executor && make precommit` must exit 0
</constraints>

<verification>
Verify `PriorityClassName` is in ConfigSpec:
```bash
grep -n "PriorityClassName" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go
```
Must show the field with `json:"priorityClassName,omitempty"`.

Verify deepcopy was regenerated:
```bash
grep -n "PriorityClassName" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/zz_generated.deepcopy.go
```
Must show the field copy in the deepcopy function.

Verify OpenAPIV3Schema was updated:
```bash
grep -n "priorityClassName" task/executor/pkg/k8s_connector.go
```
Must show the property definition.

Verify AgentConfiguration was updated:
```bash
grep -n "PriorityClassName" task/executor/pkg/agent_configuration.go
```
Must show the field.

Verify the mapping wires the field:
```bash
grep -rn "PriorityClassName" task/executor/pkg/
```
Must show at least the struct field AND a mapping assignment.

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
