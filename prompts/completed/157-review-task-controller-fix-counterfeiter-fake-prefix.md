---
status: completed
summary: Removed Fake prefix from all 6 Counterfeiter --fake-name directives and updated mock references in test files
container: agent-exec-157-review-task-controller-fix-counterfeiter-fake-prefix
dark-factory-version: v0.171.1-3-gd94f1fa
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T21:40:57Z"
completed: "2026-05-25T21:43:06Z"
---

<summary>
- Removes `Fake` prefix from all 6 counterfeiter --fake-name directives
- FakeSyncLoop → SyncLoop, FakeGitRestClient → GitRestClient, etc.
- Regenerates all mocks with corrected names
</summary>

<objective>
All 6 Counterfeiter directives in the project use `--fake-name FakeXxx` but the project convention requires clean names without the `Fake` prefix. The generated mock types should be named `SyncLoop`, `GitRestClient`, `GitClient`, `VaultScanner`, `TaskPublisher`, `ResultWriter` — not `FakeSyncLoop`, etc.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to fix:
- `task/controller/pkg/sync/sync_loop.go`
- `task/controller/pkg/gitrestclient/git_rest_client.go`
- `task/controller/pkg/scanner/vault_scanner.go`
- `task/controller/pkg/publisher/task_publisher.go`
- `task/controller/pkg/result/result_writer.go`
</context>

<requirements>

1. **In each file**, change the `--fake-name` from `FakeXxx` to `Xxx`:

   | File | Current | Fixed |
   |------|---------|-------|
   | `pkg/sync/sync_loop.go` | `--fake-name FakeSyncLoop` | `--fake-name SyncLoop` |
   | `pkg/gitrestclient/git_rest_client.go` (GitRestClient) | `--fake-name FakeGitRestClient` | `--fake-name GitRestClient` |
   | `pkg/gitrestclient/git_rest_client.go` (GitClient) | `--fake-name FakeGitClient` | `--fake-name GitClient` |
   | `pkg/scanner/vault_scanner.go` | `--fake-name FakeVaultScanner` | `--fake-name VaultScanner` |
   | `pkg/publisher/task_publisher.go` | `--fake-name FakeTaskPublisher` | `--fake-name TaskPublisher` |
   | `pkg/result/result_writer.go` | `--fake-name FakeResultWriter` | `--fake-name ResultWriter` |

2. **Regenerate mocks:**
   ```bash
   cd task/controller && make generate
   ```

3. **Update any existing references** to `FakeXxx` types in test files to use `Xxx` instead.

   Run:
   ```bash
   grep -rn "FakeSyncLoop\|FakeGitRestClient\|FakeGitClient\|FakeVaultScanner\|FakeTaskPublisher\|FakeResultWriter" task/controller/
   ```
   Update all matches in test files.

4. **Run tests:**
   ```bash
   cd task/controller && make test
   ```

5. **Run precommit:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Only change counterfeiter directives in source files and update mock references in test files
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
cd task/controller && make precommit
</verification>
