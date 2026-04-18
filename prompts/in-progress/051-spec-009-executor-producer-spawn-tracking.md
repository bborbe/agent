---
status: committing
spec: [009-executor-job-failure-detection]
summary: Extended task/executor with job task-id labels, (string,error) SpawnJob return, ResultPublisher interface with Kafka publishing, thread-safe TaskStore, spawn notification publishing after job spawn, and current_job frontmatter idempotency check
container: agent-051-spec-009-executor-producer-spawn-tracking
dark-factory-version: v0.125.1
created: "2026-04-18T20:00:00Z"
queued: "2026-04-18T19:29:44Z"
started: "2026-04-18T19:40:57Z"
branch: dark-factory/executor-job-failure-detection
---

<summary>
- Spawned K8s Jobs gain an `agent.benjamin-borbe.de/task-id` label so the job informer can look up the owning task
- The executor gains a Kafka sync producer and a `ResultPublisher` interface for publishing `lib.Task` results to `agent-task-v1-request`
- After spawning a Job, the executor publishes a spawn notification (`spawn_notification: true`) with `current_job` and `job_started_at` in frontmatter — this writes those fields to the task file without incrementing the retry counter (relies on prompt 1's controller change)
- An in-memory `taskStore` (map from task identifier to the spawned task) is maintained for the job informer (prompt 3) to retrieve the original task content when publishing failure results
- The task event handler checks `task.Frontmatter.CurrentJob()` in addition to the existing K8s `IsJobActive()` call for idempotent spawn detection; if `current_job` is set but the K8s job is gone, it proceeds to spawn (clears stale state automatically)
- `make precommit` passes in `task/executor/`
</summary>

<objective>
Extend `task/executor` with a Kafka result producer and spawn-tracking state so that: (a) every spawned Job is labelled with the task identifier for informer lookup; (b) a "spawn notification" is published to the controller immediately after spawn, writing `current_job` and `job_started_at` to the task file without false retry-counter increments; and (c) the original task is stored in memory so the job informer (prompt 3) can append failure content when emitting synthetic failure results. Precondition: prompt 1 must already be applied (controller `result_writer` must skip retry for `spawn_notification: true`).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — interface → constructor → struct, error wrapping
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns, Counterfeiter mocks
- `~/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — safe map access, no raw goroutines
- `~/.claude/plugins/marketplaces/coding/docs/go-time-injection.md` — `libtime.CurrentDateTimeGetter` injection

**Precondition — verify prompt 1 was applied:**
```bash
grep -n "SpawnNotification" lib/agent_task-frontmatter.go task/controller/pkg/result/result_writer.go
```
Both must show the new accessor/condition. If missing, STOP and report that prompt 1 has not been applied.

**Key files to read before editing:**

- `task/executor/pkg/spawner/job_spawner.go`
  - `SpawnJob` currently returns only `error`. The `jobName` local is computed at the top via `jobName := jobNameFromTask(assignee, now)` (see current file line 69). Use this variable for the `(string, error)` return.
  - After `job, err := jobBuilder.Build(ctx)` and before the existing `Create` call, patch the built job directly to add the task-id label (the `bborbe/k8s` builder does not expose a generic `SetLabel`; use the same direct-patch pattern already used in `applyEphemeralStorage` / `applySecretEnvFrom` in this same file):
    ```go
    if job.Labels == nil { job.Labels = map[string]string{} }
    job.Labels["agent.benjamin-borbe.de/task-id"] = string(task.TaskIdentifier)
    if job.Spec.Template.Labels == nil { job.Spec.Template.Labels = map[string]string{} }
    job.Spec.Template.Labels["agent.benjamin-borbe.de/task-id"] = string(task.TaskIdentifier)
    ```
  - **`IsAlreadyExists` branch handling:** the current code returns `nil` when the Job already exists (lines 122-126). Preserve idempotency but return `(jobName, nil)` so the caller can still publish a spawn notification with the existing job name. Do NOT return `("", nil)` — that would break the informer's label lookup path.

- `task/executor/pkg/spawner/job_spawner_test.go`
  - Ginkgo v2 + Gomega + `k8s.io/client-go/kubernetes/fake` — follow existing test patterns

- `task/executor/pkg/handler/task_event_handler.go`
  - `ConsumeMessage` is the place to:
    1. Check `task.Frontmatter.CurrentJob()` (from frontmatter) alongside existing `IsJobActive()`
    2. Call `jobSpawner.SpawnJob` and capture the returned job name
    3. Publish spawn notification via `ResultPublisher.PublishSpawnNotification`

- `task/executor/pkg/factory/factory.go` — `CreateConsumer` function; extend its parameter list or signature as needed to accept the Kafka sync producer and `ResultPublisher`

- `task/executor/main.go` — create Kafka sync producer, pass it to factory

- `lib/delivery/result-deliverer.go` (~line 84) — shows the `cdb.NewCommandObjectSender` + `cdb.CommandObject` pattern for publishing to `agent-task-v1-request`; replicate this pattern for the executor's `ResultPublisher`

- `lib/agent_cdb-schema.go` — `lib.TaskV1SchemaID` is the schema ID to use for the Kafka command topic

**Job name access:**
Job names come from `jobNameFromTask(assignee, now)` (line 69) returning `<assignee>-<YYYYMMDDHHMMSS>`. Refactor `SpawnJob` to RETURN `(string, error)` — return `jobName` on success, `jobName` on `AlreadyExists`, and `""` on any other error. Update the interface, the counterfeiter mock annotation, and all callers.

**Kafka producer pattern (from `lib/delivery/result-deliverer.go`):**
```go
commandObjectSender := cdb.NewCommandObjectSender(
    syncProducer,    // libkafka.SyncProducer
    branch,          // base.Branch
    log.DefaultSamplerFactory,
)
// To send:
commandObject := cdb.CommandObject{
    Command: commandCreator.NewCommand(
        base.UpdateOperation,
        cqrsiam.Initiator("executor"),
        "",
        event,
    ),
    SchemaID: lib.TaskV1SchemaID,
}
return commandObjectSender.SendCommandObject(ctx, commandObject)
```
The method name is `SendCommandObject` (confirmed in `lib/delivery/result-deliverer.go:162`). Use `cqrsiam.Initiator("executor")` (not "agent") to identify the source.

**`libtime.CurrentDateTimeGetter` injection:**
- `main.go` already has `currentDateTimeGetter := libtime.NewCurrentDateTime()`
- Pass it down to `ResultPublisher` (needed for `job_started_at` timestamp)

**Task store thread safety:**
- The `taskStore` is shared between the task event handler (writes on spawn) and the job informer (reads on terminal job event, prompt 3)
- Use `sync.RWMutex` to protect the map
- Put the store + mutex in a struct, export methods `Store(id lib.TaskIdentifier, task lib.Task)` and `Load(id lib.TaskIdentifier) (lib.Task, bool)` and `Delete(id lib.TaskIdentifier)`
- Place this in `task/executor/pkg/task_store.go`
</context>

<requirements>

1. **Add task-id label to spawned Jobs in `task/executor/pkg/spawner/job_spawner.go`**

   After determining the job name (find the `fmt.Sprintf` that builds it) and before `jobBuilder.Build(ctx)`:
   - Set the label `agent.benjamin-borbe.de/task-id` on the job. If `SetLabel(key, value string)` exists on the builder, use it. Otherwise, after `job, err := jobBuilder.Build(ctx)`, patch both `job.Labels` and `job.Spec.Template.Labels` maps directly (initialize maps if nil).
   - The label value is `string(task.TaskIdentifier)`.

2. **Change `SpawnJob` return type to `(string, error)`**

   In `task/executor/pkg/spawner/job_spawner.go`:
   - Change the function signature from `SpawnJob(ctx, task, config) error` to `SpawnJob(ctx, task, config) (string, error)` where the string is the spawned job name.
   - On successful create: `return jobName, nil`
   - On `IsAlreadyExists`: `return jobName, nil` (keep existing idempotency; caller still gets the name so spawn notification can be written)
   - On `Build` error: `return "", err`
   - On other `Create` error: `return "", err`
   - Update the `JobSpawner` interface accordingly.

3. **Regenerate the counterfeiter mock for `JobSpawner`**

   The counterfeiter annotation is in `job_spawner.go`:
   ```go
   //counterfeiter:generate -o ../../mocks/job_spawner.go --fake-name FakeJobSpawner . JobSpawner
   ```
   After updating the interface, run:
   ```bash
   cd task/executor && go generate ./pkg/spawner/...
   ```
   Commit the updated mock file. Then update all callers of the mock in tests to use the new `(string, error)` return (use `FakeJobSpawner.SpawnJobReturns("job-name", nil)` pattern from counterfeiter).

4. **Update the caller in `task_event_handler.go`**

   Change:
   ```go
   if err := h.jobSpawner.SpawnJob(ctx, task, *config); err != nil {
   ```
   To:
   ```go
   jobName, err := h.jobSpawner.SpawnJob(ctx, task, *config)
   if err != nil {
   ```
   Then after success, call `h.publishSpawnNotification(ctx, task, jobName)` and `h.taskStore.Store(task.TaskIdentifier, task)`.

5. **Create `task/executor/pkg/task_store.go`** — thread-safe task store

   ```go
   package pkg

   import "sync"

   // TaskStore is a thread-safe map from TaskIdentifier to Task.
   // It is populated when the executor spawns a Job and consumed by the job
   // informer (see JobWatcher) when publishing synthetic failure results.
   type TaskStore struct {
       mu    sync.RWMutex
       tasks map[lib.TaskIdentifier]lib.Task
   }

   // NewTaskStore creates an empty TaskStore.
   func NewTaskStore() *TaskStore {
       return &TaskStore{tasks: make(map[lib.TaskIdentifier]lib.Task)}
   }

   // Store saves the task for the given identifier (called on job spawn).
   func (s *TaskStore) Store(id lib.TaskIdentifier, task lib.Task) {
       s.mu.Lock()
       defer s.mu.Unlock()
       s.tasks[id] = task
   }

   // Load retrieves the task for the given identifier.
   func (s *TaskStore) Load(id lib.TaskIdentifier) (lib.Task, bool) {
       s.mu.RLock()
       defer s.mu.RUnlock()
       t, ok := s.tasks[id]
       return t, ok
   }

   // Delete removes the task for the given identifier (called on job termination).
   func (s *TaskStore) Delete(id lib.TaskIdentifier) {
       s.mu.Lock()
       defer s.mu.Unlock()
       delete(s.tasks, id)
   }
   ```
   Use `lib "github.com/bborbe/agent/lib"` import. Add the package-level import to the file.

6. **Create `task/executor/pkg/result_publisher.go`** — Kafka result publisher

   ```go
   package pkg

   import (
       "context"

       "github.com/bborbe/cqrs/base"
       "github.com/bborbe/cqrs/cdb"
       cqrsiam "github.com/bborbe/cqrs/iam"
       "github.com/bborbe/errors"
       libkafka "github.com/bborbe/kafka"
       "github.com/bborbe/log"
       libtime "github.com/bborbe/time"
       "github.com/google/uuid"

       lib "github.com/bborbe/agent/lib"
   )

   //counterfeiter:generate -o ../mocks/result_publisher.go --fake-name FakeResultPublisher . ResultPublisher

   // ResultPublisher publishes agent-task-v1-request commands to Kafka so the
   // controller writes them to the vault task file.
   type ResultPublisher interface {
       // PublishSpawnNotification publishes current_job and job_started_at without
       // triggering the controller's retry counter (spawn_notification: true).
       PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error
       // PublishFailure publishes a synthetic failure result that increments the
       // controller's retry counter. jobName and reason are appended to the task body.
       PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error
   }

   // NewResultPublisher creates a ResultPublisher.
   func NewResultPublisher(
       syncProducer libkafka.SyncProducer,
       branch base.Branch,
       currentDateTime libtime.CurrentDateTimeGetter,
   ) ResultPublisher {
       return &resultPublisher{
           commandObjectSender: cdb.NewCommandObjectSender(
               syncProducer,
               branch,
               log.DefaultSamplerFactory,
           ),
           currentDateTime: currentDateTime,
       }
   }

   type resultPublisher struct {
       commandObjectSender cdb.CommandObjectSender
       currentDateTime     libtime.CurrentDateTimeGetter
   }

   func (p *resultPublisher) PublishSpawnNotification(
       ctx context.Context,
       task lib.Task,
       jobName string,
   ) error {
       fm := lib.TaskFrontmatter{}
       for k, v := range task.Frontmatter {
           fm[k] = v
       }
       fm["spawn_notification"] = true
       fm["current_job"] = jobName
       fm["job_started_at"] = p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00")

       return p.publish(ctx, task.TaskIdentifier, fm, task.Content)
   }

   func (p *resultPublisher) PublishFailure(
       ctx context.Context,
       task lib.Task,
       jobName string,
       reason string,
   ) error {
       fm := lib.TaskFrontmatter{}
       for k, v := range task.Frontmatter {
           fm[k] = v
       }
       fm["status"] = "in_progress"
       fm["phase"] = "ai_review"
       fm["current_job"] = ""

       body := string(task.Content) + "\n\n## Job Failure\n\nJob `" + jobName + "` failed: " + reason + "\n"
       return p.publish(ctx, task.TaskIdentifier, fm, lib.TaskContent(body))
   }

   func (p *resultPublisher) publish(
       ctx context.Context,
       taskID lib.TaskIdentifier,
       fm lib.TaskFrontmatter,
       content lib.TaskContent,
   ) error {
       now := p.currentDateTime.Now()
       t := lib.Task{
           Object: base.Object[base.Identifier]{
               Identifier: base.Identifier(uuid.New().String()),
               Created:    now,
               Modified:   now,
           },
           TaskIdentifier: taskID,
           Frontmatter:    fm,
           Content:        content,
       }

       event, err := base.ParseEvent(ctx, t)
       if err != nil {
           return errors.Wrapf(ctx, err, "parse event for task %s", taskID)
       }

       requestIDCh := make(chan base.RequestID, 1)
       requestIDCh <- base.NewRequestID()
       commandCreator := base.NewCommandCreator(requestIDCh)
       commandObject := cdb.CommandObject{
           Command: commandCreator.NewCommand(
               base.UpdateOperation,
               cqrsiam.Initiator("executor"),
               "",
               event,
           ),
           SchemaID: lib.TaskV1SchemaID,
       }
       if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
           return errors.Wrapf(ctx, err, "send command for task %s", taskID)
       }
       return nil
   }
   ```

   After writing this file, run:
   ```bash
   cd task/executor && go generate ./pkg/...
   ```
   to generate `mocks/result_publisher.go`.

7. **Update `task_event_handler.go`** to inject and use `ResultPublisher` and `TaskStore`

   a. Add `resultPublisher pkg.ResultPublisher` and `taskStore *pkg.TaskStore` fields to `taskEventHandler` struct.

   b. Update `NewTaskEventHandler` constructor:
   ```go
   func NewTaskEventHandler(
       jobSpawner spawner.JobSpawner,
       branch base.Branch,
       resolver pkg.ConfigResolver,
       resultPublisher pkg.ResultPublisher,
       taskStore *pkg.TaskStore,
   ) TaskEventHandler {
       return &taskEventHandler{
           jobSpawner:      jobSpawner,
           branch:          branch,
           resolver:        resolver,
           resultPublisher: resultPublisher,
           taskStore:       taskStore,
       }
   }
   ```

   c. Extend the idempotency check in `ConsumeMessage`. After the stage/assignee/config checks and BEFORE the existing `IsJobActive` call, add:
   ```go
   // If current_job is set in frontmatter, a prior spawn notification was written
   // to the task file. Verify the job is still active; if not, proceed to spawn.
   if currentJob := task.Frontmatter.CurrentJob(); currentJob != "" {
       active, err := h.jobSpawner.IsJobActive(ctx, task.TaskIdentifier)
       if err != nil {
           metrics.TaskEventsTotal.WithLabelValues("error").Inc()
           return errors.Wrapf(ctx, err, "check current_job active for task %s", task.TaskIdentifier)
       }
       if active {
           glog.V(3).Infof("skip task %s: current_job %s still active (from frontmatter)", task.TaskIdentifier, currentJob)
           metrics.TaskEventsTotal.WithLabelValues("skipped_active_job").Inc()
           return nil
       }
       glog.V(2).Infof("task %s: current_job %s no longer active, proceeding to spawn", task.TaskIdentifier, currentJob)
   }
   ```
   The `skipped_active_job` and `error` metric labels are already defined in `task/executor/pkg/metrics/metrics.go` (no new labels needed). KEEP the existing `IsJobActive` call that follows — it guards against K8s jobs not yet reflected in frontmatter.

   d. After the successful `SpawnJob` call:
   ```go
   jobName, err := h.jobSpawner.SpawnJob(ctx, task, *config)
   if err != nil {
       metrics.TaskEventsTotal.WithLabelValues("error").Inc()
       return errors.Wrapf(ctx, err, "spawn job for task %s failed", task.TaskIdentifier)
   }
   h.taskStore.Store(task.TaskIdentifier, task)
   if err := h.resultPublisher.PublishSpawnNotification(ctx, task, jobName); err != nil {
       // Log but don't fail — job is already spawned, spawn notification is best-effort
       glog.Warningf("publish spawn notification for task %s failed (job %s still running): %v",
           task.TaskIdentifier, jobName, err)
   }
   ```

8. **Update `task/executor/pkg/factory/factory.go` — `CreateConsumer`**

   Add `resultPublisher pkg.ResultPublisher` and `taskStore *pkg.TaskStore` parameters, pass them through to `handler.NewTaskEventHandler`.

9. **Update `task/executor/main.go`**

   a. Create a Kafka sync producer (same sarama client used for consumer):
   ```go
   syncProducer, err := libkafka.NewSyncProducer(ctx, saramaClient)
   if err != nil {
       return errors.Wrapf(ctx, err, "create kafka sync producer")
   }
   defer syncProducer.Close()
   ```

   b. Create a `ResultPublisher` and `TaskStore`:
   ```go
   resultPublisher := pkg.NewResultPublisher(syncProducer, a.Branch, currentDateTimeGetter)
   taskStore := pkg.NewTaskStore()
   ```

   c. Pass them to `factory.CreateConsumer`.

10. **Update tests in `task/executor/pkg/handler/task_event_handler_test.go`**

    a. Import the mocks package for `FakeResultPublisher` and add `FakeTaskEventHandler` counterfeiter mock update (regenerate if needed).

    b. Add `fakeResultPublisher *mocks.FakeResultPublisher` and `taskStore *pkg.TaskStore` to the test var block.

    c. In `BeforeEach`, construct them:
    ```go
    fakeResultPublisher = &mocks.FakeResultPublisher{}
    taskStore = pkg.NewTaskStore()
    handler = handler.NewTaskEventHandler(fakeJobSpawner, branch, fakeConfigResolver, fakeResultPublisher, taskStore)
    ```

    d. Update all existing test cases that call `NewTaskEventHandler` to include the two new arguments.

    e. For existing tests where a job IS spawned, set up the fake spawner to return a job name:
    ```go
    fakeJobSpawner.SpawnJobReturns("claude-20260418120000", nil)
    ```

    f. Add a test case verifying that `PublishSpawnNotification` is called after a successful spawn:
    ```go
    It("publishes spawn notification after successful spawn", func() {
        fakeJobSpawner.SpawnJobReturns("claude-20260418120000", nil)
        // ... set up a valid in-progress ai_review task event ...
        Expect(handler.ConsumeMessage(ctx, validMsg)).To(Succeed())
        Expect(fakeResultPublisher.PublishSpawnNotificationCallCount()).To(Equal(1))
        _, calledTask, calledJobName := fakeResultPublisher.PublishSpawnNotificationArgsForCall(0)
        Expect(string(calledTask.TaskIdentifier)).To(Equal("test-task-uuid-1234"))
        Expect(calledJobName).To(Equal("claude-20260418120000"))
    })
    ```

    g. Add a test case verifying that the task is stored in taskStore after spawn:
    ```go
    It("stores task in taskStore after successful spawn", func() {
        fakeJobSpawner.SpawnJobReturns("claude-20260418120000", nil)
        Expect(handler.ConsumeMessage(ctx, validMsg)).To(Succeed())
        _, ok := taskStore.Load(lib.TaskIdentifier("test-task-uuid-1234"))
        Expect(ok).To(BeTrue())
    })
    ```

    h. Add a test case verifying the frontmatter `current_job` idempotency check:
    ```go
    It("skips spawn when current_job in frontmatter and K8s job is active", func() {
        fakeJobSpawner.IsJobActiveReturns(true, nil)
        msg := buildTaskMsg(lib.TaskFrontmatter{
            "task_identifier": "test-task-uuid-1234",
            "status":          "in_progress",
            "phase":           "ai_review",
            "stage":           string(branch),
            "assignee":        "claude",
            "current_job":     "claude-20260418000000",
        })
        Expect(handler.ConsumeMessage(ctx, msg)).To(Succeed())
        Expect(fakeJobSpawner.SpawnJobCallCount()).To(Equal(0))
    })
    ```

11. **Update `task/executor/pkg/spawner/job_spawner_test.go`** to reflect the `(string, error)` return change

    Update every `Expect(jobSpawner.SpawnJob(...)).To(Succeed())` to:
    ```go
    jobName, err := jobSpawner.SpawnJob(ctx, task, config)
    Expect(err).NotTo(HaveOccurred())
    Expect(jobName).NotTo(BeEmpty())
    ```

    Add a test verifying the task-id label is set on the spawned job:
    ```go
    It("sets agent.benjamin-borbe.de/task-id label on spawned job", func() {
        jobName, err := jobSpawner.SpawnJob(ctx, task, config)
        Expect(err).NotTo(HaveOccurred())
        Expect(jobName).NotTo(BeEmpty())
        jobs, _ := kubeClient.BatchV1().Jobs(string(namespace)).List(ctx, metav1.ListOptions{})
        Expect(jobs.Items).To(HaveLen(1))
        Expect(jobs.Items[0].Labels).To(HaveKeyWithValue("agent.benjamin-borbe.de/task-id", string(task.TaskIdentifier)))
        Expect(jobs.Items[0].Spec.Template.Labels).To(HaveKeyWithValue("agent.benjamin-borbe.de/task-id", string(task.TaskIdentifier)))
    })
    ```

</requirements>

<constraints>
- Do NOT change the controller package — all changes are in `task/executor/` and `lib/`
- Do NOT change agent code — agents continue publishing results unchanged
- Do NOT change the Kafka event/request schema or `lib.Task` struct
- Do NOT commit — dark-factory handles git
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for all error wrapping — never `fmt.Errorf`
- Use `libtime.CurrentDateTimeGetter` for timestamps — never `time.Now()` directly
- `TaskStore` must be safe for concurrent use (RWMutex)
- Spawn notification publish failure must be non-fatal (log warning, don't return error) — the job is already running
- Do NOT use raw `go func()` — the resultPublisher is called synchronously in the message handler; the informer (prompt 3) will use `service.Run`
- Counterfeiter mocks must be regenerated after interface changes: `cd task/executor && go generate ./pkg/... ./pkg/spawner/...`
- All existing tests must continue to pass
- `cd task/executor && make precommit` must exit 0
</constraints>

<verification>
Verify prompt 1 precondition:
```bash
grep -n "SpawnNotification" lib/agent_task-frontmatter.go task/controller/pkg/result/result_writer.go
```
Must show both.

Verify task-id label is added to spawner:
```bash
grep -n "task-id\|agent.benjamin-borbe.de" task/executor/pkg/spawner/job_spawner.go
```
Must show the label being set.

Verify SpawnJob interface returns (string, error):
```bash
grep -n "SpawnJob" task/executor/pkg/spawner/job_spawner.go
```
Must show `(string, error)` return type.

Verify ResultPublisher and TaskStore exist:
```bash
ls task/executor/pkg/result_publisher.go task/executor/pkg/task_store.go
```
Both must exist.

Verify mock was generated:
```bash
ls task/executor/mocks/result_publisher.go
```
Must exist.

Verify handler injects ResultPublisher and TaskStore:
```bash
grep -n "resultPublisher\|taskStore" task/executor/pkg/handler/task_event_handler.go
```
Must show both fields and their usage.

Run executor tests:
```bash
cd task/executor && make test
```
Must exit 0.

Run full precommit:
```bash
cd task/executor && make precommit
```
Must exit 0.

Verify spawn notification is published in tests:
```bash
grep -n "PublishSpawnNotification\|PublishSpawnNotificationCallCount" task/executor/pkg/handler/task_event_handler_test.go
```
Must show the test assertion.
</verification>
