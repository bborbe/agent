---
status: approved
spec: ["001"]
created: "2026-03-26T09:00:00Z"
queued: "2026-03-26T18:33:16Z"
branch: dark-factory/git-to-kafka-task-sync
---

<summary>
- Task controller periodically pulls the vault and scans the task directory for changes
- New and modified task files are detected via content hashing against an in-memory map
- Deleted task files are detected when previously seen files disappear
- Each changed file's frontmatter is parsed to extract status, assignee, and phase
- Files with invalid frontmatter are skipped with a warning; files without assignee are skipped silently
- Git pull failures skip the cycle and retry on the next tick
- The scanner emits changed tasks and deleted identifiers per cycle
- The scan loop respects context cancellation for graceful shutdown
</summary>

<objective>
Implement the core git-poll-and-scan loop in task/controller as a self-contained VaultScanner service. It pulls the vault repo, detects file changes via content hashing, parses frontmatter, and emits structured task change events — all without touching Kafka. The next prompt wires this scanner to a Kafka publisher.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `/home/node/.claude/docs/go-patterns.md` for interface/constructor/struct patterns.
Read `/home/node/.claude/docs/go-testing.md` for Ginkgo/Gomega and counterfeiter conventions.
Read `/home/node/.claude/docs/go-factory-pattern.md` for factory function rules.
Read `/home/node/.claude/docs/go-context-cancellation.md` for non-blocking select in loops.

Key files to read before making changes:
- `task/controller/main.go` — application struct fields added in prompt 1 (GitURL, GitToken, PollInterval, GitBranch, TaskDir)
- `task/controller/pkg/gitclient/git_client.go` — GitClient interface (added in prompt 1)
- `lib/agent_task.go` — `lib.Task` struct (frozen — do not modify)
- `lib/agent_task-identifier.go` — `lib.TaskIdentifier` type (frozen — do not modify)
- `lib/agent_cdb-schema.go` — `lib.TaskV1SchemaID`
- The vault-cli domain types at `github.com/bborbe/vault-cli/pkg/domain`: `Task`, `TaskStatus`, `TaskPhase`
</context>

<requirements>
1. Create `task/controller/pkg/scanner/vault_scanner.go`.

   Define the scanner output type:
   ```go
   // ScanResult holds the outcome of a single vault scan cycle.
   type ScanResult struct {
       Changed []lib.Task             // tasks whose content changed (new or modified)
       Deleted []lib.TaskIdentifier   // task identifiers that were previously known but are now gone
   }
   ```

   Define the interface:
   ```go
   //counterfeiter:generate -o ../../mocks/vault_scanner.go --fake-name FakeVaultScanner . VaultScanner
   type VaultScanner interface {
       // Run starts the polling loop. Blocks until ctx is cancelled.
       Run(ctx context.Context) error
       // Results returns a channel that receives ScanResult on each cycle.
       Results() <-chan ScanResult
   }
   ```

   Define the constructor:
   ```go
   func NewVaultScanner(
       gitClient gitclient.GitClient,
       taskDir string,       // relative path within vault, e.g. "24 Tasks"
       pollInterval time.Duration,
   ) VaultScanner
   ```

   Private struct fields:
   - `gitClient gitclient.GitClient`
   - `taskDir string`
   - `pollInterval time.Duration`
   - `results chan ScanResult`
   - `hashes map[string][32]byte` — stores content hash per file path (relative to vault root)

2. Implement `Run(ctx context.Context) error`:
   - Create a `time.NewTicker(v.pollInterval)`.
   - Enter a `for` loop with a `select` that handles `ctx.Done()` (return `nil`) and the ticker channel.
   - On each tick: call `v.runCycle(ctx)`.
   - The select must be non-blocking on ctx (following `go-context-cancellation.md` pattern).

3. Implement `runCycle(ctx context.Context)`:
   - Call `v.gitClient.Pull(ctx)`.
   - If pull fails, log at warning level (`glog.Warningf("git pull failed: %v", err)`) and return (skip this cycle).
   - Walk `filepath.Join(v.gitClient.Path(), v.taskDir)` using `filepath.WalkDir`.
   - For each `.md` file found, compute `sha256.Sum256(fileContent)` and compare against `v.hashes[relPath]`.
   - Files with a new or changed hash are added to the `changed` list; update `v.hashes[relPath]`.
   - Files previously in `v.hashes` but not found during this walk are added to the `deleted` list and removed from `v.hashes`.
   - For each changed file, call `v.parseTask(ctx, absPath, relPath)` to get `*lib.Task`. If nil (skip), do not add to changed list.
   - Send a `ScanResult{Changed: changed, Deleted: deleted}` on `v.results` channel (non-blocking: if no receiver, drop it with a `select { case v.results <- result: default: }`).

4. Implement `parseTask(ctx context.Context, absPath, relPath string) *lib.Task`:
   - Read the file content.
   - Extract the YAML frontmatter block (between the first `---` and the second `---` delimiters).
   - Unmarshal using `gopkg.in/yaml.v3` into a `domain.Task` struct (`github.com/bborbe/vault-cli/pkg/domain`).
   - If unmarshal fails or the status field is empty: `glog.Warningf("skipping %s: invalid frontmatter: %v", relPath, err)` and return `nil`.
   - If `domainTask.Assignee == ""`: return `nil` (silent skip).
   - Build and return a `*lib.Task`:
     ```go
     return &lib.Task{
         TaskIdentifier: lib.TaskIdentifier(relPath),
         Name:           lib.TaskName(domainTask.Name),
         Status:         domainTask.Status,
         Phase:          domainTask.Phase, // lib.Task.Phase is *domain.TaskPhase — check if domain.Task.Phase is already a pointer; if not, use &domainTask.Phase
         Assignee:       lib.TaskAssignee(domainTask.Assignee),
         Content:        lib.TaskContent(fileContent),
     }
     ```
   - Do NOT set `base.Object` fields — those are set by the publisher in the next prompt.

5. Implement `Results() <-chan ScanResult` — returns `v.results` channel.

6. Create `task/controller/pkg/scanner/vault_scanner_test.go` with a Ginkgo suite:
   - Suite bootstrap: `task/controller/pkg/scanner/scanner_suite_test.go`
   - Use `os.MkdirTemp` to create a fake vault directory.
   - Initialize it as a git repo with `git init` + `git commit --allow-empty -m "init"` via `exec.Command`.
   - Create `24 Tasks/` subdirectory.
   - Test cases for `parseTask`:
     - Valid frontmatter + assignee → returns `*lib.Task` with correct fields
     - Valid frontmatter + empty assignee → returns `nil`
     - Missing frontmatter delimiters → returns `nil` (warning logged)
     - Malformed YAML → returns `nil` (warning logged)
   - Test cases for hash-based change detection (call `runCycle` directly via white-box test in `package scanner`, not `package scanner_test`):
     - New file → appears in `ScanResult.Changed`
     - Unchanged file → not in `ScanResult.Changed`
     - File content changes → appears in `ScanResult.Changed`
     - File deleted → appears in `ScanResult.Deleted`
   - Use `package scanner` (internal) for runCycle tests; `package scanner_test` for public interface tests.

7. Run `make generate` in `task/controller/` to regenerate mocks.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib.Task`, `lib.TaskV1SchemaID`, or `lib.TaskIdentifier` — these are frozen
- Do NOT wire the scanner into `main.go` yet — that is done in prompt 3
- Git operations go through GitClient interface (from prompt 1) — scanner does NOT call git directly
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have counterfeiter annotations; mocks must be generated
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- The sync loop must respect context cancellation (non-blocking select in the ticker loop)
- Warn once per bad file per cycle; do not deduplicate across cycles in this prompt
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
</verification>
