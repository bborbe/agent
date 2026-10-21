---
status: completed
execution_id: agent-task-vault-exec-243-task-vault-field
dark-factory-version: v0.198.7
created: "2026-10-21T00:00:00Z"
queued: "2026-10-21T16:31:14Z"
started: "2026-10-21T16:31:29Z"
completed: "2026-10-21T16:39:39Z"
---

<summary>
- Task events can now carry the slug of the vault the task file lives in
- A consumer sharing one topic across several vaults can tell its own tasks apart without reading git
- The field is optional: events from older controllers simply omit it and stay valid
- Tasks with no vault set serialize exactly as before, with no extra key on the wire
- Validation is unchanged: an empty vault is still a valid task
- Tests cover round-tripping the vault through JSON, omission when empty, and reading older payloads that lack the key
- The changelog gains an Unreleased entry describing the new field
</summary>

<objective>
Add an optional `Vault` field to the `Task` payload type so task/controller can stamp the vault slug on every task event and consumers sharing one Kafka topic across vaults can filter their own tasks without reading git. This is a backwards-compatible, additive wire change.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `agent_task.go` — the `Task` struct (embeds `base.Object[base.Identifier]`, then `TaskIdentifier`, `Frontmatter`, `Content`) and its `Validate` method, which validates only `TaskIdentifier` and `Content`.
Read `agent_task_test.go` — the `Describe("Task", ...)` block with `Describe("Validate", ...)` is the test-pattern exemplar (Ginkgo v2 + Gomega, package `lib_test`, `lib "github.com/bborbe/agent"`, `ctx` set in `BeforeEach`).
Read the top of `CHANGELOG.md` — the file currently starts its entries at `## v0.112.0`; there is no `## Unreleased` section yet.
</context>

<requirements>
1. In `agent_task.go`, add this field to the `Task` struct immediately after `TaskIdentifier` (before `Frontmatter`), with a doc comment above it:
   ```go
   // Vault is the slug of the vault the task file lives in. task/controller stamps it
   // on every agent-task-v1-event so a consumer sharing one topic across vaults can
   // tell its own tasks apart without reading git. Empty on events from older controllers.
   Vault string `json:"vault,omitempty"`
   ```
   Keep the surrounding fields' alignment gofmt-clean (run `gofmt` / let `make precommit` format).

2. Do NOT change `Task.Validate`. `Vault` is not validated; an empty `Vault` stays valid.

3. In `agent_task_test.go`, extend the existing `Describe("Task", ...)` block with a new `Describe("Vault", ...)` (sibling of `Describe("Validate", ...)`). Add `"encoding/json"` to the stdlib import group. Cases, each traversing the real `encoding/json` boundary:
   - JSON round-trip preserves `Vault`: marshal a `lib.Task` with `TaskIdentifier`, `Content` and `Vault: "openclaw"`, unmarshal into a fresh `lib.Task`, expect `Vault` equals `"openclaw"` and `Validate(ctx)` is nil.
   - Empty `Vault` marshals without a `"vault"` key: marshal a `lib.Task` with `Vault` unset, unmarshal the output into `map[string]interface{}`, expect the map to not have key `"vault"` (`NotTo(HaveKey("vault"))`).
   - JSON lacking `"vault"` unmarshals to empty `Vault` and `Validate` still passes: unmarshal a literal payload containing only `taskIdentifier` and `content` (e.g. `{"taskIdentifier":"task-uuid-123","content":"some content"}`) into `lib.Task`, expect `Vault` is `""` and `Validate(ctx)` is nil.
   Check every `json.Marshal` / `json.Unmarshal` error with `Expect(err).To(BeNil())` per the surrounding test style.

4. In `CHANGELOG.md`, insert a new section directly above `## v0.112.0` (after the intro bullet list, with a blank line before and after):
   ```
   ## Unreleased

   - feat: `Task.Vault` carries the vault slug on task events, so a consumer sharing one topic across vaults can filter its own tasks without reading git
   ```

5. Touch no other files. Before finishing, re-run `<verification>` and confirm it passes; walk each requirement against the diff.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- The change is additive and backwards-compatible: events from older controllers (no `vault` key) must still unmarshal and validate
- `Vault` is optional and not validated; no new validation rule, no constructor change, no other struct touched
- Only `agent_task.go`, `agent_task_test.go`, and `CHANGELOG.md` may change
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` at the repo root — must pass. (The Makefile derives ROOTDIR from `git rev-parse --show-toplevel`; `.git` is masked in the container, so ROOTDIR must be supplied explicitly.)
Run `grep -q 'json:"vault,omitempty"' agent_task.go` — must succeed.
Run `awk '/^## /{sec=$0} /Task.Vault. carries the vault slug/{print "sits under: " sec}' CHANGELOG.md` — must print `sits under: ## Unreleased`.
</verification>
