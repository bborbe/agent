---
status: approved
spec: [009-executor-job-failure-detection]
created: "2026-04-18T20:00:00Z"
queued: "2026-04-18T19:29:44Z"
branch: dark-factory/executor-job-failure-detection
---

<summary>
- The executor watches `batch/v1 Jobs` in its own namespace via a shared informer keyed by `agent.benjamin-borbe.de/task-id` label
- When a Job reaches `Failed` terminal state: executor publishes a synthetic failure result to Kafka and deletes the Job; the controller's retry counter picks it up identically to an agent-published failure
- When a Job reaches `Succeeded` terminal state and the task is still tracked in the `TaskStore` (agent did not yet publish a completed result): executor publishes a synthetic "succeeded without result" failure and deletes the Job
- When a Job reaches `Succeeded` and the task is NOT in the `TaskStore` (agent published before informer fired): executor skips synthetic emission and deletes the Job
- The executor gains RBAC `delete` permission on `jobs.batch` in its namespace (required for cleanup)
- `cd task/executor && make precommit` passes
- CHANGELOG updated with `## Unreleased` section
</summary>

<objective>
Complete spec 009 by adding a K8s Job informer to the executor that reacts to terminal Job states. When a Job fails (OOM, eviction, backoffLimit), the executor publishes a synthetic failure result to Kafka, which flows through the controller's existing retry counter exactly like an agent-published failure. Silent failures — that previously left the task stuck forever — now count as retry attempts and eventually escalate to `human_review`. Preconditions: prompts 1 and 2 must be applied (`lib.SpawnNotification()`, `ResultPublisher`, `TaskStore`, and the `agent.benjamin-borbe.de/task-id` label on Jobs must already exist).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — interface → constructor → struct, error wrapping
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega patterns, Counterfeiter mocks
- `~/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — `run.CancelOnFirstErrorWait`, goroutine lifecycle
- `~/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md` — non-blocking select in loops
- `~/.claude/plugins/marketplaces/coding/docs/go-kubernetes-crd-controller-guide.md` — shared informer pattern

**Preconditions — verify before starting:**
```bash
# Prompt 1: SpawnNotification in lib and controller
grep -n "SpawnNotification" lib/agent_task-frontmatter.go task/controller/pkg/result/result_writer.go
# Prompt 2: ResultPublisher, TaskStore, task-id label
ls task/executor/pkg/result_publisher.go task/executor/pkg/task_store.go
grep -n "agent.benjamin-borbe.de/task-id" task/executor/pkg/spawner/job_spawner.go
```
All must pass. If missing, STOP and report.

**Key files to read before editing:**

- `task/executor/main.go` — current `service.Run` call; add the job informer runner here
- `task/executor/pkg/factory/factory.go` — factory functions; add a `CreateJobWatcher` factory function
- `task/executor/pkg/k8s_connector.go` — how the existing Config informer is set up using `SharedInformerFactory`; replicate this pattern for batch/v1 Jobs
- `task/executor/pkg/task_store.go` — `TaskStore` methods from prompt 2 (`Store`, `Load`, `Delete`)
- `task/executor/pkg/result_publisher.go` — `ResultPublisher.PublishFailure` from prompt 2
- `task/executor/pkg/spawner/job_spawner.go` — `IsJobActive` method; reuse the K8s client and namespace if available through the spawner, OR pass the kubeClient directly to the watcher

**Informer label selector:**
The informer should watch ONLY Jobs with `agent.benjamin-borbe.de/task-id` label present (to avoid reacting to non-executor Jobs):
```go
import "k8s.io/apimachinery/pkg/labels"

labelSelector := labels.SelectorFromSet(labels.Set{}).String() // watch all in namespace
// OR filter to only executor-managed jobs:
labelSelector := "agent.benjamin-borbe.de/task-id"  // label exists filter
```
Use `informers.NewFilteredSharedInformerFactory` with a `TweakListOptionsFunc` to add `LabelSelector: "agent.benjamin-borbe.de/task-id"` to the list/watch options.

**Terminal Job state detection:**

A `batch/v1 Job` is Failed when:
```go
import batchv1 "k8s.io/api/batch/v1"

for _, cond := range job.Status.Conditions {
    if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
        return true, cond.Message
    }
}
```

A Job is Succeeded when:
```go
for _, cond := range job.Status.Conditions {
    if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
        return true
    }
}
```

Extract the task identifier from the job label:
```go
taskIDStr, ok := job.Labels["agent.benjamin-borbe.de/task-id"]
if !ok || taskIDStr == "" {
    // not an executor-managed job, skip
    return
}
taskID := lib.TaskIdentifier(taskIDStr)
```

**Job deletion:**
Use the K8s client to delete the Job after publishing the synthetic result:
```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
)

propagation := metav1.DeletePropagationBackground
err := batchClient.Delete(ctx, job.Name, metav1.DeleteOptions{
    PropagationPolicy: &propagation,
})
```
Use `PropagationPolicy: Background` so child pods are cleaned up asynchronously.

**Existing informer pattern (from `k8s_connector.go`):**
The connector uses `informers.NewSharedInformerFactory` for Config resources. For Jobs, use the standard `k8s.io/client-go/informers` package:
```go
import k8sinformers "k8s.io/client-go/informers"

factory := k8sinformers.NewSharedInformerFactoryWithOptions(
    kubeClient,
    5*time.Minute, // resync period
    k8sinformers.WithNamespace(string(namespace)),
    k8sinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
        opts.LabelSelector = "agent.benjamin-borbe.de/task-id"
    }),
)
jobInformer := factory.Batch().V1().Jobs().Informer()
```
</context>

<requirements>

1. **Create `task/executor/pkg/job_watcher.go`** — Job informer component

   ```go
   package pkg

   import (
       "context"
       "time"

       "github.com/bborbe/errors"
       "github.com/golang/glog"
       corev1 "k8s.io/api/core/v1"
       batchv1 "k8s.io/api/batch/v1"
       metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
       k8sinformers "k8s.io/client-go/informers"
       "k8s.io/client-go/kubernetes"
       "k8s.io/client-go/tools/cache"

       lib "github.com/bborbe/agent/lib"
       libk8s "github.com/bborbe/k8s"
   )

   //counterfeiter:generate -o ../mocks/job_watcher.go --fake-name FakeJobWatcher . JobWatcher

   // JobWatcher watches batch/v1 Jobs in the executor's namespace and publishes
   // synthetic failure results for terminal-state Jobs that belong to spawned tasks.
   type JobWatcher interface {
       // Run starts the Job informer and blocks until ctx is cancelled.
       Run(ctx context.Context) error
       // HandleJob processes a single Job (invoked by the informer event handlers
       // and by unit tests directly, avoiding the need for a fake informer).
       HandleJob(ctx context.Context, job *batchv1.Job)
   }

   // NewJobWatcher creates a JobWatcher.
   func NewJobWatcher(
       kubeClient kubernetes.Interface,
       namespace libk8s.Namespace,
       taskStore *TaskStore,
       publisher ResultPublisher,
   ) JobWatcher {
       return &jobWatcher{
           kubeClient: kubeClient,
           namespace:  namespace,
           taskStore:  taskStore,
           publisher:  publisher,
       }
   }

   type jobWatcher struct {
       kubeClient kubernetes.Interface
       namespace  libk8s.Namespace
       taskStore  *TaskStore
       publisher  ResultPublisher
   }

   func (w *jobWatcher) Run(ctx context.Context) error {
       factory := k8sinformers.NewSharedInformerFactoryWithOptions(
           w.kubeClient,
           5*time.Minute,
           k8sinformers.WithNamespace(string(w.namespace)),
           k8sinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
               opts.LabelSelector = "agent.benjamin-borbe.de/task-id"
           }),
       )
       informer := factory.Batch().V1().Jobs().Informer()

       _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
           UpdateFunc: func(oldObj, newObj interface{}) {
               job, ok := newObj.(*batchv1.Job)
               if !ok {
                   return
               }
               w.HandleJob(ctx, job)
           },
           AddFunc: func(obj interface{}) {
               job, ok := obj.(*batchv1.Job)
               if !ok {
                   return
               }
               w.HandleJob(ctx, job)
           },
       })
       if err != nil {
           return errors.Wrapf(ctx, err, "add job informer event handler")
       }

       factory.Start(ctx.Done())
       if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
           return errors.Errorf(ctx, "timed out waiting for job informer cache sync")
       }
       glog.V(2).Infof("job informer started in namespace %s", w.namespace)
       <-ctx.Done()
       return nil
   }

   func (w *jobWatcher) HandleJob(ctx context.Context, job *batchv1.Job) {
       taskIDStr, ok := job.Labels["agent.benjamin-borbe.de/task-id"]
       if !ok || taskIDStr == "" {
           return
       }
       taskID := lib.TaskIdentifier(taskIDStr)

       if isJobFailed(job) {
           reason := jobFailureReason(job)
           glog.V(2).Infof("job %s/%s failed (task %s): %s", job.Namespace, job.Name, taskID, reason)
           w.handleTerminal(ctx, taskID, job, reason, true)
           return
       }
       if isJobSucceeded(job) {
           glog.V(2).Infof("job %s/%s succeeded (task %s)", job.Namespace, job.Name, taskID)
           w.handleTerminal(ctx, taskID, job, "job completed without publishing result", false)
       }
   }

   // handleTerminal publishes a synthetic failure (when appropriate) and deletes the Job.
   // alwaysPublish is true for Failed jobs; for Succeeded jobs it only publishes if the
   // task is still in the TaskStore (agent has not yet published a result).
   func (w *jobWatcher) handleTerminal(
       ctx context.Context,
       taskID lib.TaskIdentifier,
       job *batchv1.Job,
       reason string,
       alwaysPublish bool,
   ) {
       task, ok := w.taskStore.Load(taskID)
       if !ok {
           if alwaysPublish {
               glog.Warningf("task %s not in task store; job %s/%s failed but cannot publish synthetic failure (no original task content)", taskID, job.Namespace, job.Name)
           } else {
               glog.V(3).Infof("task %s not in task store; job %s/%s succeeded — agent likely published result already", taskID, job.Namespace, job.Name)
           }
       } else {
           // task is in store; always publish a synthetic failure regardless of alwaysPublish:
           //   - Failed job: caller set alwaysPublish=true
           //   - Succeeded job with task still in store: agent did not publish success yet,
           //     so we emit a synthetic failure ("job completed without publishing result")
           if err := w.publisher.PublishFailure(ctx, task, job.Name, reason); err != nil {
               glog.Errorf("publish synthetic failure for task %s (job %s): %v", taskID, job.Name, err)
               // Do not return — still attempt job deletion
           } else {
               glog.V(2).Infof("published synthetic failure for task %s (job %s)", taskID, job.Name)
           }
           w.taskStore.Delete(taskID)
       }

       propagation := metav1.DeletePropagationBackground
       if err := w.kubeClient.BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
           PropagationPolicy: &propagation,
       }); err != nil {
           glog.Warningf("delete job %s/%s failed: %v", job.Namespace, job.Name, err)
       } else {
           glog.V(2).Infof("deleted terminal job %s/%s", job.Namespace, job.Name)
       }
   }

   func isJobFailed(job *batchv1.Job) bool {
       for _, c := range job.Status.Conditions {
           if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
               return true
           }
       }
       return false
   }

   func isJobSucceeded(job *batchv1.Job) bool {
       for _, c := range job.Status.Conditions {
           if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
               return true
           }
       }
       return false
   }

   func jobFailureReason(job *batchv1.Job) string {
       for _, c := range job.Status.Conditions {
           if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue && c.Message != "" {
               return c.Message
           }
       }
       return "unknown failure reason"
   }
   ```

   Adjust imports based on what is actually vendored. Verify with:
   ```bash
   ls task/executor/vendor/k8s.io/client-go/informers/
   ```

2. **Generate the `FakeJobWatcher` counterfeiter mock**

   ```bash
   cd task/executor && go generate ./pkg/...
   ```
   Verify `mocks/job_watcher.go` is created.

3. **Add `CreateJobWatcher` factory function to `task/executor/pkg/factory/factory.go`**

   ```go
   // CreateJobWatcher creates a JobWatcher that reacts to terminal batch/v1 Job states.
   func CreateJobWatcher(
       kubeClient kubernetes.Interface,
       namespace libk8s.Namespace,
       taskStore *pkg.TaskStore,
       publisher pkg.ResultPublisher,
   ) pkg.JobWatcher {
       return pkg.NewJobWatcher(kubeClient, namespace, taskStore, publisher)
   }
   ```

4. **Wire `JobWatcher` into `task/executor/main.go`**

   a. Create the watcher after creating `resultPublisher` and `taskStore`:
   ```go
   jobWatcher := factory.CreateJobWatcher(kubeClient, a.Namespace, taskStore, resultPublisher)
   ```

   b. Add the watcher as a new runner in `service.Run`:
   ```go
   return service.Run(
       ctx,
       func(ctx context.Context) error {
           return connector.Listen(ctx, a.Namespace, resourceEventHandler)
       },
       func(ctx context.Context) error {
           return consumer.Consume(ctx)
       },
       func(ctx context.Context) error {
           return jobWatcher.Run(ctx)
       },
       a.createHTTPServer(eventHandlerConfig),
   )
   ```

5. **Write unit tests for `JobWatcher` in `task/executor/pkg/job_watcher_test.go`**

   Use Ginkgo v2 + Gomega. Follow the external test package convention (`package pkg_test`).

   Set up:
   - `fakePublisher *mocks.FakeResultPublisher`
   - `taskStore *pkg.TaskStore`
   - `kubeClient kubernetes.Interface` (use `k8s.io/client-go/kubernetes/fake`)
   - Populate `taskStore` with a test task

   Test cases:

   a. **Failed job publishes synthetic failure and deletes job**
   ```go
   It("publishes synthetic failure and deletes job on Failed state", func() {
       // Create a Job in the fake client with task-id label
       // Set Failed condition on the job
       // Call watcher.handleJob(ctx, job) directly (or trigger via informer)
       // Assert fakePublisher.PublishFailureCallCount() == 1
       // Assert job is deleted from fake client
   })
   ```

   b. **Succeeded job with task in store publishes synthetic failure**
   ```go
   It("publishes synthetic failure for Succeeded job when task is in store", func() {
       // Set up task in taskStore
       // Create Succeeded job
       // handleJob → PublishFailure called once
       // job deleted
   })
   ```

   c. **Succeeded job with task NOT in store skips publish**
   ```go
   It("skips synthetic failure for Succeeded job when task is not in store", func() {
       // Do NOT add task to taskStore
       // Create Succeeded job
       // handleJob → PublishFailure NOT called
       // job still deleted
   })
   ```

   d. **Job with no task-id label is ignored**
   ```go
   It("ignores jobs without task-id label", func() {
       // Job with no agent.benjamin-borbe.de/task-id label
       // handleJob → no publish, no delete
       // fakePublisher.PublishFailureCallCount() == 0
   })
   ```

   e. **Failed job with task not in store logs warning but still deletes**
   ```go
   It("deletes Failed job even when task is not in taskStore", func() {
       // Failed job, task NOT in store
       // handleJob → PublishFailure NOT called (or called with zero count)
       // job deleted
   })
   ```

   Tests call `watcher.HandleJob(ctx, job)` directly (the method is exported on the interface per Requirement 1). No need to drive the informer from tests. Use `k8s.io/client-go/kubernetes/fake.NewSimpleClientset(...)` to pre-create Jobs so deletion assertions can verify the Job is gone after `HandleJob` returns.

6. **Update CHANGELOG.md with `## Unreleased` section**

   Check first:
   ```bash
   grep -n "Unreleased" CHANGELOG.md | head -3
   ```
   If an `## Unreleased` section already exists, APPEND to it. Otherwise, INSERT above the first `## v` line.

   Add:
   ```markdown
   ## Unreleased

   - feat: Executor watches batch/v1 Jobs and publishes synthetic failure results for OOMKilled, evicted, and backoffLimit-exceeded Jobs; feeds controller's retry counter identically to agent-published failures
   - feat: Executor records `current_job` and `job_started_at` in task frontmatter after spawning a Job (spawn notification bypasses retry counter)
   - feat: Executor deletes terminal Jobs after publishing synthetic failure result, preventing stale Job accumulation
   - fix: Executor no longer logs "job already exists, treating as success" for failed Jobs — failure is now detected and reported via Kafka
   ```

7. **Add taskStore cleanup for completed events in `task/executor/pkg/handler/task_event_handler.go`**

   Without this, the informer may emit a synthetic failure for a task the agent already completed (race: agent publishes success → controller writes `status: completed` → new task event arrives at executor → informer then fires on the same Job's `Succeeded` transition).

   Add at the top of `ConsumeMessage`, immediately after the task is unmarshalled and BEFORE any status/phase filter returns early:
   ```go
   // Clean up taskStore for completed tasks so the job informer does not emit
   // a spurious synthetic failure after the agent has already published success.
   if string(task.Frontmatter.Status()) == "completed" {
       h.taskStore.Delete(task.TaskIdentifier)
       glog.V(3).Infof("task %s completed: removed from task store", task.TaskIdentifier)
   }
   ```
   Add a test case in `task_event_handler_test.go`:
   ```go
   It("removes task from taskStore when event has status=completed", func() {
       taskStore.Store(lib.TaskIdentifier("test-task-uuid-1234"), lib.Task{/* ... */})
       msg := buildTaskMsg(lib.TaskFrontmatter{
           "task_identifier": "test-task-uuid-1234",
           "status":          "completed",
           "phase":           "done",
       })
       Expect(handler.ConsumeMessage(ctx, msg)).To(Succeed())
       _, ok := taskStore.Load(lib.TaskIdentifier("test-task-uuid-1234"))
       Expect(ok).To(BeFalse())
   })
   ```

   Residual race: if the informer fires BEFORE the handler receives the completed event, the executor still emits a synthetic failure. This is accepted per spec §Desired Behavior #7 (mild false positive, controller counter double-increments).

8. **Update RBAC — add `delete` on `jobs.batch` to executor K8s manifests**

   Find the executor's ClusterRole or Role in `task/executor/k8s/` or similar path:
   ```bash
   find task/executor -name "*.yaml" | xargs grep -l "jobs\|batch" 2>/dev/null
   ```
   Add `delete` to the `jobs` resource rules. The executor already has `get`, `list`, `watch` on jobs (from `IsJobActive`). Extend it to include `delete`:
   ```yaml
   - apiGroups: ["batch"]
     resources: ["jobs"]
     verbs: ["get", "list", "watch", "delete"]
   ```
   If the manifest is in a different repo or doesn't exist in this repo, note it in the completion report.

</requirements>

<constraints>
- Do NOT change the controller package, `lib.Task` struct, or any Kafka topic schema
- Do NOT use `time.Now()` — use `libtime.CurrentDateTimeGetter` via `ResultPublisher` (already injected)
- Do NOT use raw `go func()` — `JobWatcher.Run` blocks on `<-ctx.Done()` and is started via `service.Run`; the informer's goroutines are started by the factory internally
- Do NOT use `context.Background()` anywhere in `pkg/` — always propagate `ctx`
- Use `github.com/bborbe/errors.Wrapf(ctx, err, "...")` for all error wrapping — never `fmt.Errorf`
- Job deletion errors must NOT stop the informer — log and continue (see `handleTerminal` above)
- Synthetic failure publish errors must NOT stop the informer — log and continue (job still gets deleted)
- `agent.benjamin-borbe.de/task-id` label must be present on the Job (set in prompt 2); jobs without this label are silently skipped
- For terminal Jobs observed on informer startup (re-list): the informer fires `AddFunc` for all existing objects, so startup detection is automatic — no special re-list needed
- Scope: Pattern B ephemeral Jobs only — do NOT watch Deployments or Pattern A agents
- Do NOT implement timeout detection for stuck Running Jobs (out of scope per spec)
- Do NOT commit — dark-factory handles git
- All existing tests must pass
- `cd task/executor && make precommit` must exit 0

**Failure mode coverage (from spec §Failure Modes):**

| Trigger | Covered by |
|---------|-----------|
| Pod OOMKilled, Job reaches Failed | Requirement 1: `isJobFailed` → `handleTerminal(alwaysPublish=true)` |
| Node eviction, Job reaches Failed | Same as OOM — Job condition `JobFailed` regardless of eviction cause |
| Agent publishes failure, Job then Succeeded | `handleTerminal(alwaysPublish=false)` — task still in store (race) → may double-increment (accepted per spec DB#7) |
| Agent publishes success, Job Succeeded | Task removed from store by clean event path → skip (see note below) |
| `current_job` set but Job gone | Prompt 2 handles in task_event_handler via IsJobActive check |
| Informer desynced at startup | Factory calls `WaitForCacheSync`; re-list fires AddFunc for all existing terminal Jobs |
| Executor restarts mid-OOM | Informer re-lists on connect; sees Failed job; publishes synthetic failure |

TaskStore cleanup on completed events is Requirement 7; the residual race is accepted per spec §Desired Behavior #7.
</constraints>

<verification>
Verify preconditions:
```bash
grep -n "SpawnNotification" lib/agent_task-frontmatter.go task/controller/pkg/result/result_writer.go
ls task/executor/pkg/result_publisher.go task/executor/pkg/task_store.go
grep -n "agent.benjamin-borbe.de/task-id" task/executor/pkg/spawner/job_spawner.go
```
All must pass.

Verify job_watcher.go exists:
```bash
ls task/executor/pkg/job_watcher.go task/executor/pkg/job_watcher_test.go
```

Verify mock generated:
```bash
ls task/executor/mocks/job_watcher.go
```

Verify factory function exists:
```bash
grep -n "CreateJobWatcher" task/executor/pkg/factory/factory.go
```

Verify watcher is wired into main.go:
```bash
grep -n "jobWatcher\|JobWatcher" task/executor/main.go
```
Must show creation and use in service.Run.

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

Verify CHANGELOG:
```bash
grep -n -A5 "Unreleased" CHANGELOG.md | head -12
```
Must show the new feat/fix bullets.

Verify RBAC update:
```bash
grep -rn "delete" task/executor/k8s/ 2>/dev/null | grep -i "job\|batch"
```
Must show `delete` verb on jobs.

Verify terminal state helpers:
```bash
grep -n "isJobFailed\|isJobSucceeded\|jobFailureReason" task/executor/pkg/job_watcher.go
```
Must show all three functions.

Verify job deletion is implemented:
```bash
grep -n "Delete\|PropagationBackground" task/executor/pkg/job_watcher.go
```
Must show deletion with background propagation.

Verify taskStore cleanup on completed events in handler:
```bash
grep -n "completed.*taskStore\|taskStore.*Delete" task/executor/pkg/handler/task_event_handler.go
```
Must show the completed-event cleanup path.
</verification>
