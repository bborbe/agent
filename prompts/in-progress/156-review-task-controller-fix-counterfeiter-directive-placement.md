---
status: approved
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
---

<summary>
- Moves counterfeiter directive for GitClient interface to immediately above the interface definition
- Currently the directive is at end of file; interface is near the end
- Also fixes counterfeiter --fake-name from FakeGitClient to GitClient
</summary>

<objective>
The counterfeiter directive for `GitClient` in `pkg/gitrestclient/git_rest_client.go` is at line 266 (end of file) while the interface definition is at line 269. Per `go-patterns.md`, the directive must be placed directly above the interface it generates mocks for. Additionally, the `--fake-name` uses `FakeGitClient` which should be `GitClient` per the project's counterfeiter convention.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/gitrestclient/git_rest_client.go` — lines 260-290, full GitClient interface block
</context>

<requirements>

1. **Move the counterfeiter directive** from line 266 (or wherever it currently is after recent edits) to immediately above the `GitClient` interface definition.

   **Before:**
   ```go
   // ... other code ...
   }  // ← end of file or near end

   //counterfeiter:generate -o ../../mocks/git_client.go --fake-name FakeGitClient . GitClient
   // GitClient is the interface for vault file operations...
   type GitClient interface {
   ```

   **After:**
   ```go
   //counterfeiter:generate -o ../../mocks/git_client.go --fake-name GitClient . GitClient

   // GitClient is the interface for vault file operations...
   type GitClient interface {
   ```

   Also fix the `--fake-name` from `FakeGitClient` to `GitClient` (remove the `Fake` prefix).

2. **Regenerate mocks:**
   ```bash
   cd task/controller && make generate
   ```

3. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change `task/controller/pkg/gitrestclient/git_rest_client.go`
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
