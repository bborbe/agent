---
status: created
spec: [015-atomic-frontmatter-increment-and-trigger-cap]
created: "2026-04-24T07:42:14Z"
branch: dark-factory/atomic-frontmatter-increment-and-trigger-cap
---

<summary>
- Two new frontmatter accessors — `TriggerCount()` and `MaxTriggers()` — added to `lib.TaskFrontmatter`, matching the existing pattern for `RetryCount()` and `MaxRetries()`
- `TriggerCount()` defaults to 0 when absent; `MaxTriggers()` defaults to 3 when absent
- Two new Kafka command operation constants added for atomic frontmatter operations: `IncrementFrontmatterCommandOperation` and `UpdateFrontmatterCommandOperation`
- Two new command payload types added: `IncrementFrontmatterCommand` (taskIdentifier + field + delta) and `UpdateFrontmatterCommand` (taskIdentifier + key-value updates map)
- Unit tests cover all four new items: accessor defaults, accessor with explicit value, zero delta increment, nil updates map
- `cd lib && make precommit` passes
</summary>

<objective>
Add the `TriggerCount` / `MaxTriggers` frontmatter accessors and the two new Kafka command types (`IncrementFrontmatterCommand`, `UpdateFrontmatterCommand`) to the `lib/` package. These are the shared contracts needed by prompts 2 (controller) and 3 (executor). This prompt is purely additive; no handler or executor logic changes here.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping, counterfeiter
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages

**Key files to read before editing:**

- `lib/agent_task-frontmatter.go` — existing `TaskFrontmatter` type and accessor methods; `RetryCount()` (line ~40) and `MaxRetries()` (line ~50) are the exact patterns to clone for `TriggerCount()` / `MaxTriggers()`
- `lib/agent_task.go` — `Task`, `TaskIdentifier`, `TaskFrontmatter`, `TaskContent` types
- `lib/agent_cdb-schema.go` — how `TaskV1SchemaID` is declared; place new schema IDs here if needed (read first to understand the pattern)
- Any existing test file in `lib/` (e.g., `lib/agent_task-frontmatter_test.go`) — match the Ginkgo test style exactly

Run these to understand the current frontmatter accessor style:
```bash
grep -n "func (f TaskFrontmatter)" lib/agent_task-frontmatter.go
```
```bash
grep -n "SchemaID\|CommandOperation\|CommandObject" lib/agent_cdb-schema.go lib/*.go 2>/dev/null | head -30
```
```bash
ls lib/
```
</context>

<requirements>

1. **Add `TriggerCount()` accessor to `lib/agent_task-frontmatter.go`**

   Add immediately after `RetryCount()`:
   ```go
   // TriggerCount returns the number of spawn-trigger events that have fired for this task.
   // Returns 0 if the field is absent.
   func (f TaskFrontmatter) TriggerCount() int {
       v, ok := f["trigger_count"]
       if !ok {
           return 0
       }
       switch n := v.(type) {
       case int:
           return n
       case float64:
           return int(n)
       }
       return 0
   }
   ```

2. **Add `MaxTriggers()` accessor to `lib/agent_task-frontmatter.go`**

   Add immediately after `MaxRetries()`:
   ```go
   // MaxTriggers returns the maximum number of spawn-trigger events allowed for this task.
   // Returns 3 if the field is absent, matching the default for max_retries.
   func (f TaskFrontmatter) MaxTriggers() int {
       v, ok := f["max_triggers"]
       if !ok {
           return 3
       }
       switch n := v.(type) {
       case int:
           return n
       case float64:
           return int(n)
       }
       return 3
   }
   ```

3. **Add new command types to `lib/`**

   Create a new file `lib/agent_task-commands.go` with the two new command kinds and their payload types:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package lib

   // IncrementFrontmatterCommandOperation is the Kafka command operation
   // for atomically incrementing a single frontmatter field by a delta.
   // Published by the executor on agent-task-v1-request; handled by the controller.
   const IncrementFrontmatterCommandOperation = "increment_frontmatter"

   // UpdateFrontmatterCommandOperation is the Kafka command operation
   // for atomically setting specific frontmatter keys without touching other keys.
   const UpdateFrontmatterCommandOperation = "update_frontmatter"

   // IncrementFrontmatterCommand is the payload for IncrementFrontmatterCommandOperation.
   // The controller reads the current value of Field from disk, adds Delta, and writes
   // the result atomically — so the write is never idempotent.
   type IncrementFrontmatterCommand struct {
       TaskIdentifier TaskIdentifier `json:"taskIdentifier"`
       Field          string         `json:"field"`
       Delta          int            `json:"delta"`
   }

   // UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
   // The controller applies only the listed key-value pairs; all other frontmatter
   // keys in the task file are left unchanged.
   type UpdateFrontmatterCommand struct {
       TaskIdentifier TaskIdentifier `json:"taskIdentifier"`
       Updates        TaskFrontmatter `json:"updates"`
   }
   ```

   Note: these types do NOT need a new `cdb.SchemaID`. The Kafka routing uses the command operation string inside the envelope. Read `lib/agent_cdb-schema.go` and the existing `task_result_executor.go` in `task/controller/pkg/command/` to understand how the existing schema and operation are declared and used — follow the same pattern for registering these new operation kinds in the controller (that wiring is prompt 2's job; this prompt only defines the payload types and operation constants).

4. **Add unit tests**

   Find or create the test file for `lib/agent_task-frontmatter.go` (it may be `lib/agent_task-frontmatter_test.go`). Add a Ginkgo `Describe` block for `TriggerCount` and `MaxTriggers`:

   ```go
   Describe("TriggerCount", func() {
       It("returns 0 when field is absent", func() {
           fm := lib.TaskFrontmatter{}
           Expect(fm.TriggerCount()).To(Equal(0))
       })
       It("returns the value when set as int", func() {
           fm := lib.TaskFrontmatter{"trigger_count": 2}
           Expect(fm.TriggerCount()).To(Equal(2))
       })
       It("returns the value when set as float64 (JSON default)", func() {
           fm := lib.TaskFrontmatter{"trigger_count": float64(5)}
           Expect(fm.TriggerCount()).To(Equal(5))
       })
   })

   Describe("MaxTriggers", func() {
       It("returns 3 when field is absent", func() {
           fm := lib.TaskFrontmatter{}
           Expect(fm.MaxTriggers()).To(Equal(3))
       })
       It("returns the value when set as int", func() {
           fm := lib.TaskFrontmatter{"max_triggers": 10}
           Expect(fm.MaxTriggers()).To(Equal(10))
       })
       It("returns the value when set as float64", func() {
           fm := lib.TaskFrontmatter{"max_triggers": float64(7)}
           Expect(fm.MaxTriggers()).To(Equal(7))
       })
   })
   ```

   If no test file exists for frontmatter, create `lib/agent_task-frontmatter_test.go` with a proper Ginkgo suite (check for existing suite files in `lib/` first — if `lib/suite_test.go` exists, follow its pattern; otherwise create one).

5. **Run tests**

   ```bash
   cd lib && make test
   ```
   Must exit 0.

</requirements>

<constraints>
- This prompt touches `lib/` ONLY — do NOT modify `task/controller/`, `task/executor/`, or any other package
- `TriggerCount()` default MUST be 0; `MaxTriggers()` default MUST be 3
- Do NOT add a new `cdb.SchemaID` — the operation string constants are sufficient for the routing; the controller will register new handlers under the existing TaskV1SchemaID (prompt 2 handles that)
- `IncrementFrontmatterCommand` and `UpdateFrontmatterCommand` are plain Go structs; no `//counterfeiter:generate` annotation needed
- Use `github.com/bborbe/errors` for any error wrapping — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `cd lib && make precommit` must exit 0
</constraints>

<verification>
Verify new accessors exist:
```bash
grep -n "TriggerCount\|MaxTriggers" lib/agent_task-frontmatter.go
```
Must show both method definitions with correct default values.

Verify new command types exist:
```bash
grep -n "IncrementFrontmatterCommand\|UpdateFrontmatterCommand\|IncrementFrontmatterCommandOperation\|UpdateFrontmatterCommandOperation" lib/agent_task-commands.go
```
Must show all four.

Run tests:
```bash
cd lib && make test
```
Must exit 0.

Run precommit:
```bash
cd lib && make precommit
```
Must exit 0.
</verification>
