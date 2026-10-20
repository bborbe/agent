---
status: completed
summary: Added TaskFrontmatter.Cluster() accessor with tests, terminology docs, and changelog entry
execution_id: agent-cluster-field-exec-238-task-frontmatter-cluster-accessor
dark-factory-version: v0.198.7
created: "2026-10-20T21:10:00Z"
queued: "2026-10-20T21:00:27Z"
started: "2026-10-20T21:00:30Z"
completed: "2026-10-20T21:07:10Z"
---

<summary>
- Task files can carry an optional `cluster` frontmatter field naming the cluster that should run the task (canonical values `nukedev`, `nukeprod`, `sun`)
- Code that reads task frontmatter gets a typed accessor for that field, next to the existing `stage` accessor
- A task without the field, or with a non-text value, reports an empty cluster, meaning any cluster may run it
- Existing tasks and all existing behavior are unchanged; the accessor is purely additive
- The new accessor is covered by unit tests for the absent, string, and non-string cases
- The terminology documentation and the changelog describe the new optional field
</summary>

<objective>
Add a `Cluster()` accessor to `TaskFrontmatter` in the root `lib` package so consumers can read the optional `cluster` frontmatter key for cluster routing. Backward compatible; no other behavior changes.
</objective>

<context>
Read `CLAUDE.md` in the repo root for project conventions (Ginkgo/Gomega, external `_test` packages, single global `CHANGELOG.md`).

Read these files before making changes:
- `agent_task-frontmatter.go` — `type TaskFrontmatter map[string]interface{}` and its typed accessors. Existing sibling to copy the style from:
  ```go
  // Stage returns the execution stage from the "stage" key.
  // Returns "prod" if the key is absent or empty.
  func (f TaskFrontmatter) Stage() string {
  	v, _ := f["stage"].(string)
  	if v == "" {
  		return "prod"
  	}
  	return v
  }
  ```
  and `CurrentJob()`, which uses the `v, _ := f["current_job"].(string)` idiom.
- `agent_task-frontmatter_test.go` — package `lib_test`, imports `. "github.com/onsi/ginkgo/v2"`, `. "github.com/onsi/gomega"`, and `lib "github.com/bborbe/agent"`. Tests live in `Describe("TaskFrontmatter", ...)` with one nested `Describe` per accessor, e.g. `Describe("Stage", ...)` using `fm := lib.TaskFrontmatter{"stage": "dev"}`.
- `docs/task-flow-and-failure-semantics.md` — the `## Terminology` table is where task frontmatter fields are documented (rows such as **Max triggers**, **Trigger counter**). `stage` itself is not documented anywhere, so the `cluster` row is added to this table.
- `CHANGELOG.md` — top section is currently `## v0.105.2`; there is no `## Unreleased` section yet. `docs/dod.md` requires an entry under `## Unreleased`.
</context>

<requirements>
1. In `agent_task-frontmatter.go`, add this method directly after `Stage()` (before `RetryCount()`):
   ```go
   // Cluster returns the target cluster from the "cluster" key (e.g. "nukedev", "nukeprod", "sun").
   // Returns "" when the key is absent or not a string, meaning any cluster may run the task.
   func (f TaskFrontmatter) Cluster() string {
   	v, _ := f["cluster"].(string)
   	return v
   }
   ```
   No new imports. Do not normalize, trim, lowercase, or validate the value, and do not add a typed enum or constants for the canonical values.

2. In `agent_task-frontmatter_test.go`, add a `Describe("Cluster", ...)` block directly after the `Describe("Stage", ...)` block, with three `It` cases using the existing style:
   - absent key: `fm := lib.TaskFrontmatter{}` -> `Expect(fm.Cluster()).To(Equal(""))`
   - string value: `fm := lib.TaskFrontmatter{"cluster": "nukedev"}` -> `Equal("nukedev")`
   - non-string value: `fm := lib.TaskFrontmatter{"cluster": 42}` -> `Equal("")`

3. In `docs/task-flow-and-failure-semantics.md`, add one row to the `## Terminology` table (after the **Max triggers** row) documenting the field:
   `| **Cluster** | Optional `cluster` frontmatter field naming the cluster that should run the task. Canonical values: `nukedev`, `nukeprod`, `sun`. Read via `TaskFrontmatter.Cluster()`. Absent (or a non-string value) reads as `""`, meaning any cluster may run the task. |`
   Keep the table's column format (two columns, pipe-delimited) intact.

4. In `CHANGELOG.md`, insert a new section immediately above `## v0.105.2` (one blank line between the sections) containing exactly:
   ```
   ## Unreleased

   - feat: TaskFrontmatter.Cluster() accessor for optional cluster routing
   ```
</requirements>

<constraints>
- Backward compatible: do not change any existing accessor, struct, signature, or JSON/YAML shape; no other behavior change
- Do NOT add validation, normalization, defaults, constants, or a new type for cluster values; the accessor returns the raw string or ""
- Do NOT wire the accessor into any controller/executor in this prompt
- Do NOT edit generated files or `mocks/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
</constraints>

<verification>
Run `make precommit` in the repo root — must pass.
Run `grep -n 'func (f TaskFrontmatter) Cluster' agent_task-frontmatter.go` — must print exactly one match.
Run `grep -n 'Unreleased' CHANGELOG.md` — must print the new section header.
Run `grep -n '\*\*Cluster\*\*' docs/task-flow-and-failure-semantics.md` — must print exactly one match.
</verification>
