---
status: completed
spec: [001-git-to-kafka-task-sync]
summary: Wired VaultScanner to TaskPublisher via SyncLoop in task/controller, completing the Git-to-Kafka task sync pipeline with concurrent HTTP server and graceful shutdown
container: agent-003-spec-001-kafka-publish-wiring
dark-factory-version: v0.67.3-dirty
created: "2026-03-26T09:00:00Z"
queued: "2026-03-26T18:33:16Z"
started: "2026-03-26T19:05:52Z"
completed: "2026-03-26T19:22:00Z"
branch: dark-factory/git-to-kafka-task-sync
---

<summary>
- Changed tasks are published as events to Kafka; deleted tasks produce deletion events
- The scanner from prompt 2 and a new publisher are wired together via a sync loop
- The sync loop runs concurrently with the existing HTTP server
- Graceful shutdown stops both the HTTP server and the sync loop
- Existing health and metrics endpoints continue to work unchanged
- This completes the Git-to-Kafka task sync pipeline
</summary>

<objective>
Wire the VaultScanner (from prompt 2) to a TaskPublisher that sends Kafka events, then integrate both into task/controller's main.go so the service runs the sync loop concurrently with its HTTP server. This completes the Git-to-Kafka task sync pipeline.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `/home/node/.claude/docs/go-patterns.md` for interface/constructor/struct patterns.
Read `/home/node/.claude/docs/go-testing.md` for Ginkgo/Gomega and counterfeiter conventions.
Read `/home/node/.claude/docs/go-factory-pattern.md` for factory function rules (zero logic, Create* prefix).
Read `/home/node/.claude/docs/go-composition.md` for DI and service composition.

Key files to read before making changes:
- `task/controller/main.go` — application struct with GitURL, GitToken, KafkaBrokers, GitBranch, PollInterval, TaskDir fields + gitClient setup (from prompt 1)
- `task/controller/pkg/gitclient/git_client.go` — GitClient interface (from prompt 1)
- `task/controller/pkg/scanner/vault_scanner.go` — VaultScanner interface and ScanResult type (added in prompt 2)
- `lib/agent_task.go` — `lib.Task` struct (frozen — do not modify)
- `lib/agent_task-identifier.go` — `lib.TaskIdentifier` (frozen — do not modify)
- `lib/agent_cdb-schema.go` — `lib.TaskV1SchemaID`
- `github.com/bborbe/kafka` — `kafka.SyncProducer`, `kafka.NewSyncProducer`, `kafka.Brokers`
- `github.com/bborbe/cqrs/cdb` — `cdb.SchemaID`, `cdb.Topic()` or equivalent for deriving the Kafka topic name
- `github.com/bborbe/cqrs/base` — `base.Object`, `base.NewIdentifier`, `base.NewDateTime`
</context>

<requirements>
1. Create `task/controller/pkg/publisher/task_publisher.go`.

   Define the interface:
   ```go
   //counterfeiter:generate -o ../../mocks/task_publisher.go --fake-name FakeTaskPublisher . TaskPublisher
   type TaskPublisher interface {
       // PublishChanged publishes an upsert event for the given task.
       PublishChanged(ctx context.Context, task lib.Task) error
       // PublishDeleted publishes a deletion event for the given task identifier.
       PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error
   }
   ```

   Define the constructor:
   ```go
   func NewTaskPublisher(
       syncProducer kafka.SyncProducer,
       schemaID cdb.SchemaID,
       branch string,
   ) TaskPublisher
   ```

   Private struct fields:
   - `syncProducer kafka.SyncProducer`
   - `topic string` — computed once in constructor as `cdb.TopicName(schemaID, branch)` (or equivalent cdb API — check the cdb package for the correct function name)

   `PublishChanged(ctx context.Context, task lib.Task) error`:
   - Set `task.Object` using `base.NewObject[base.Identifier]()` or the appropriate cqrs/base constructor to populate `Identifier`, `Created`, `Modified` with a new UUID and current time.
   - Marshal `task` to JSON with `encoding/json`.
   - Build a `*sarama.ProducerMessage` with `Topic: p.topic`, `Key: sarama.ByteEncoder(task.TaskIdentifier.Bytes())`, `Value: sarama.ByteEncoder(jsonBytes)`.
   - Call `p.syncProducer.SendMessage(ctx, msg)`.
   - Wrap errors with `errors.Wrapf(ctx, err, "publish changed task %s failed", task.TaskIdentifier)`.

   `PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error`:
   - Build a tombstone message: `*sarama.ProducerMessage` with `Topic: p.topic`, `Key: sarama.ByteEncoder(id.Bytes())`, `Value: nil` (Kafka tombstone for compacted topics).
   - Call `p.syncProducer.SendMessage(ctx, msg)`.
   - Wrap errors with `errors.Wrapf(ctx, err, "publish deleted task %s failed", id)`.

2. Create `task/controller/pkg/publisher/task_publisher_test.go` with a Ginkgo suite:
   - Suite bootstrap: `task/controller/pkg/publisher/publisher_suite_test.go`
   - Use `FakeKafkaSyncProducer` (generated mock from `github.com/bborbe/kafka/mocks`) or create a counterfeiter mock for `kafka.SyncProducer` in the mocks directory.
   - Test cases:
     - `PublishChanged` sends a message with the correct topic, the task identifier as key, and valid JSON as value
     - `PublishChanged` returns an error when `SendMessage` fails
     - `PublishDeleted` sends a tombstone message (nil value) with the correct topic and key
     - `PublishDeleted` returns an error when `SendMessage` fails
   - Use `package publisher_test` (external test package).

3. Create `task/controller/pkg/sync/sync_loop.go` — a simple orchestrator that connects scanner results to publisher calls.

   Define the interface:
   ```go
   //counterfeiter:generate -o ../../mocks/sync_loop.go --fake-name FakeSyncLoop . SyncLoop
   type SyncLoop interface {
       Run(ctx context.Context) error
   }
   ```

   Constructor:
   ```go
   func NewSyncLoop(
       scanner scanner.VaultScanner,
       publisher publisher.TaskPublisher,
   ) SyncLoop
   ```

   `Run(ctx context.Context) error`:
   - Start `scanner.Run(ctx)` in a goroutine (capture its error via a channel or `run.Func`).
   - Read from `scanner.Results()` in a `for` loop with a `select` on `ctx.Done()`.
   - For each `ScanResult`:
     - For each task in `result.Changed`: call `publisher.PublishChanged(ctx, task)`. Log errors at warning level; do not return.
     - For each id in `result.Deleted`: call `publisher.PublishDeleted(ctx, id)`. Log errors at warning level; do not return.
   - Return when ctx is cancelled.

4. Create `task/controller/pkg/sync/sync_loop_test.go` with a Ginkgo suite:
   - Suite bootstrap: `task/controller/pkg/sync/sync_suite_test.go`
   - Use `FakeVaultScanner` and `FakeTaskPublisher` mocks (generated by counterfeiter).
   - Test cases:
     - Changed task in scan result → calls `PublishChanged` with correct task
     - Deleted identifier in scan result → calls `PublishDeleted` with correct identifier
     - `PublishChanged` returns error → logs warning, does not stop the loop
     - `PublishDeleted` returns error → logs warning, does not stop the loop
     - Context cancelled → Run returns nil
   - Use `package sync_test` (external test package).

5. Create `task/controller/pkg/factory/factory.go` with a `CreateSyncLoop` factory function:

   ```go
   func CreateSyncLoop(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       syncProducer kafka.SyncProducer,
       schemaID cdb.SchemaID,
       branch string,
   ) sync.SyncLoop {
       vaultScanner := scanner.NewVaultScanner(gitClient, taskDir, pollInterval)
       taskPublisher := publisher.NewTaskPublisher(syncProducer, schemaID, branch)
       return sync.NewSyncLoop(vaultScanner, taskPublisher)
   }
   ```

   This function must have zero business logic — no conditionals, no I/O, no `context.Background()`.

6. Update `task/controller/main.go`:
   - In `Run()`, the gitClient is already created and cloned (from prompt 1).
   - Create a `kafka.SyncProducer` by calling `kafka.NewSyncProducer(ctx, kafka.Brokers(a.KafkaBrokers))`. Return the error if creation fails.
   - Defer `syncProducer.Close()`.
   - Call `factory.CreateSyncLoop(gitClient, a.TaskDir, a.PollInterval, syncProducer, lib.TaskV1SchemaID, a.GitBranch)` to get a `SyncLoop`.
   - Pass `syncLoop.Run` as a `run.Func` to `service.Run()` alongside `a.createHTTPServer()`:
     ```go
     return service.Run(
         ctx,
         a.createHTTPServer(),
         syncLoop.Run,
     )
     ```
   - Kafka brokers string must be parsed to `kafka.Brokers` — check if `kafka.Brokers` is a string alias or requires splitting. Use the appropriate constructor from the kafka package.

7. Run `make generate` in `task/controller/` to regenerate all mocks.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib.Task`, `lib.TaskV1SchemaID`, or `lib.TaskIdentifier` — these are frozen
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have counterfeiter annotations; mocks must be generated with `make generate`
- Use `github.com/bborbe/errors` for error wrapping — never `fmt.Errorf`
- Existing HTTP server (healthz/readiness/metrics) must continue to work unchanged
- The sync loop must respect context cancellation — when ctx is cancelled, both scanner and publisher stop
- Git operations use subprocess in scanner — publisher must not touch git
- Kafka connection errors on startup are fatal (return error from Run())
- Kafka publish errors per-message are warnings only (log and continue loop)
- `make test` must pass before declaring done
</constraints>

<verification>
Run `make test` in `task/controller/` — must pass.
Run `make precommit` in `task/controller/` — must pass with exit code 0.
</verification>
