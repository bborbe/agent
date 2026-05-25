---
status: committing
summary: Verified blank lines exist between counterfeiter directives and GoDoc comments in all three files
container: agent-exec-158-review-task-controller-fix-godoc-directive-bleed
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T21:43:08Z"
completed: "2026-05-25T21:47:03Z"
---

<summary>
- Adds blank line between counterfeiter directive and GoDoc comment in 3 files
- Prevents directive text appearing in generated documentation for exported types
</summary>

<objective>
The counterfeiter generate directives in `pkg/result/result_writer.go`, `pkg/publisher/task_publisher.go`, and `pkg/gitrestclient/git_rest_client.go` are placed immediately before the GoDoc comment with no blank line. This causes the directive text (e.g., `//counterfeiter:generate ...`) to appear in godoc output for the exported type. After this fix, a blank line separates the directive from the doc comment, keeping generated docs clean.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to fix (read each before editing):
- `task/controller/pkg/result/result_writer.go` — directive at line 24, GoDoc at line 25
- `task/controller/pkg/publisher/task_publisher.go` — directive at line 19, GoDoc at line 20
- `task/controller/pkg/gitrestclient/git_rest_client.go` — directive at line 22, GoDoc at line 23
</context>

<requirements>

For each file, add a blank line between the `//counterfeiter:generate` line and the `// <TypeName> ...` GoDoc comment.

**Before:**
```go
//counterfeiter:generate -o ../../mocks/result_writer.go --fake-name FakeResultWriter . ResultWriter
// ResultWriter writes a Task back to the vault task file.
type ResultWriter interface {
```

**After:**
```go
//counterfeiter:generate -o ../../mocks/result_writer.go --fake-name FakeResultWriter . ResultWriter

// ResultWriter writes a Task back to the vault task file.
type ResultWriter interface {
```

Apply this same fix to all three files listed above.

Run tests and precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</requirements>

<constraints>
- Only add blank lines — no other changes
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
