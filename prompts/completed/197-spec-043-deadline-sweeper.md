---
status: completed
spec: [043-executor-zombie-job-detection]
summary: Add deadline sweeper goroutine (ZombieSweeper) that classifies zombie tasks and publishes failures, wired into executor service.Run lifecycle
container: agent-zombie-detect-exec-197-spec-043-deadline-sweeper
dark-factory-version: v0.173.0
created: "2026-06-01T20:30:00Z"
queued: "2026-06-01T20:11:58Z"
started: "2026-06-01T20:30:57Z"
completed: "2026-06-01T20:40:50Z"
---

<summary>
- Adds a background goroutine that periodically walks the in-memory TaskStore looking for zombie jobs the k8s-native and Pods-informer paths missed.
- Classifies a task as zombie iff `elapsed > deadline AND pod not Running AND no recent heartbeat`, emitting `deadline_exceeded` or `executor_watch_lost` accordingly.
- Wires the sweeper interval and timeout from the AgentConfig CRD knobs added in prompt 3.
- Plugs the sweeper into the executor's existing `service.Run` lifecycle alongside the consumer, deferred-respawn loop, and Job/Pod informer.
- After this prompt, persistent zombies surface within bounded time (`max_triggers × zombieJobTimeoutSeconds`) via the existing `applyTriggerCap` chokepoint.
</summary>

<objective>
Add a deadline sweeper goroutine that classifies and publishes zombie failures for tasks whose Jobs are past their deadline with no recent heartbeat and no healthy Pod, and wire it into the executor's `service.Run` lifecycle.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Spec: `specs/in-progress/043-executor-zombie-job-detection.md` (Desired Behavior 4, 7; Acceptance Criteria 5, 6, 7; Failure Modes rows for `Pod unschedulable beyond grace`, `executor_watch_lost`, `Sweeper fires after Job-condition informer already fired`).

Files to read before changing:
- `task/executor/pkg/task_store.go` — `TaskStore.Snapshot` (line 50) returns a shallow copy safe for read-only iteration; this is the sweeper's iteration source.
- `task/executor/pkg/job_watcher.go` (as updated by prompt 2) — the existing informer-driven path. The sweeper is a safety net, NOT a replacement.
- `task/executor/pkg/result_publisher.go` (as updated by prompt 1) — `PublishFailure` is dedupe-protected; the sweeper can safely fire even when the informer already fired.
- `task/executor/pkg/agent_configuration.go` (as updated by prompt 3) — `EffectiveZombieJobTimeoutSeconds`. Note: the SWEEPER INTERVAL is not on `AgentConfiguration` — it is a single executor-wide value sourced from the CRD. Choose to read it from `ConfigSpec` via a "first non-nil wins" rule across all watched configs (see requirement 4 below), with the default applied when nothing is set.
- `task/executor/pkg/handler/task_event_handler.go` — `RunDeferredRespawnLoop` (line 558) is the existing template for "periodic goroutine plugged into `service.Run`". Mirror its shape (ticker + select on `ctx.Done()`).
- `task/executor/main.go` — `application.Run` (line 54). The sweeper's `Run(ctx)` method gets added to the existing `service.Run(...)` argument list at line 121.
- `task/executor/pkg/factory/factory.go` — add a `CreateZombieSweeper` constructor here, matching the style of `CreateJobWatcher` (line 31).
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` (as updated by prompt 3) — `DefaultZombieSweeperIntervalSeconds`, `DefaultZombieJobTimeoutSeconds`, `ConfigSpec.ZombieSweeperIntervalSeconds`.
- `task/executor/pkg/event_handler_config.go` — `EventHandlerConfig` is the in-memory store of all watched Config CRs (type alias `k8s.EventHandler[agentv1.Config]` from `github.com/bborbe/k8s`). The sweeper reads the sweeper interval from here via the existing `Provider[T].Get(ctx) ([]T, error)` method — there is no `Configs()` accessor and one CANNOT be added (it is a third-party generic alias). See `task/executor/pkg/probe/probe.go:110` for the existing usage pattern: `configs, err := r.configProvider.Get(ctx)`.
- `task/executor/pkg/job_watcher.go` (as updated by prompt 2) — prompt 2 introduces a Pod informer and exposes a Pod lister (e.g. `corev1listers.PodLister` from the shared informer factory). The sweeper REUSES that lister instead of issuing per-tick LIST calls to the API server.

Coding plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-time-injection.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md`
</context>

<requirements>
### 1. Define the `ZombieSweeper` interface

Create `task/executor/pkg/zombie_sweeper.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
    "context"
    "time"

    "github.com/bborbe/errors"
    libk8s "github.com/bborbe/k8s"
    libtime "github.com/bborbe/time"
    "github.com/golang/glog"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/labels"
    corev1listers "k8s.io/client-go/listers/core/v1"

    lib "github.com/bborbe/agent/lib"
    agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
)

//counterfeiter:generate -o ../mocks/zombie_sweeper.go --fake-name FakeZombieSweeper . ZombieSweeper

// ZombieSweeper is a background goroutine that periodically classifies stuck
// tasks as zombies and emits failure events. It is the safety net for the
// informer-driven paths in JobWatcher (which handle the cases k8s notifies us
// about). The sweeper handles: pods unschedulable beyond a grace window,
// executor restart losing watch on a Job, and any deadline path the informer
// misses (Job-condition deferred indefinitely, informer cache drift).
type ZombieSweeper interface {
    // Run blocks until ctx is cancelled. Each tick (interval sourced from the
    // first non-nil ConfigSpec.ZombieSweeperIntervalSeconds across the resolver's
    // configs, else DefaultZombieSweeperIntervalSeconds) it calls SweepOnce.
    Run(ctx context.Context) error
    // SweepOnce performs a single sweep pass. Exposed for unit tests so they
    // do not have to manage tickers. Returns an error only on context
    // cancellation paths; per-task classification errors are logged.
    SweepOnce(ctx context.Context) error
}
```

Note the `counterfeiter:generate` directive — `make precommit` will regenerate the fake.

### 2. Define the constructor and impl

In the same file:

```go
// NewZombieSweeper creates a ZombieSweeper.
func NewZombieSweeper(
    podLister corev1listers.PodLister,
    namespace libk8s.Namespace,
    taskStore *TaskStore,
    publisher ResultPublisher,
    configProvider EventHandlerConfig,
    currentDateTime libtime.CurrentDateTimeGetter,
) ZombieSweeper {
    return &zombieSweeper{
        podLister:       podLister,
        namespace:       namespace,
        taskStore:       taskStore,
        publisher:       publisher,
        configProvider:  configProvider,
        currentDateTime: currentDateTime,
    }
}

type zombieSweeper struct {
    podLister       corev1listers.PodLister
    namespace       libk8s.Namespace
    taskStore       *TaskStore
    publisher       ResultPublisher
    configProvider  EventHandlerConfig
    currentDateTime libtime.CurrentDateTimeGetter
}
```

`EventHandlerConfig` is the type alias `k8s.EventHandler[agentv1.Config]` defined in `task/executor/pkg/event_handler_config.go`. It exposes `Get(ctx context.Context) ([]agentv1.Config, error)` via the embedded `Provider[T]` interface — use that. Do NOT invent or add a `Configs()` method: `EventHandlerConfig` is a generic alias from `github.com/bborbe/k8s` and methods cannot be added to it from this package.

### 3. Implement the sweep predicate

In the same file, add the classification logic:

```go
const (
    // podNotScheduledGraceWindow is the age threshold past which a Pending Pod
    // with PodScheduled=False is classified pod_not_scheduled. Must exceed
    // typical scheduler latency comfortably; 2 minutes is empirically generous.
    podNotScheduledGraceWindow = 2 * time.Minute
)

// NOTE on "no recent heartbeat" from spec DB #9 / AC #6:
// The spec predicate is `elapsed > deadline AND pod not Running AND no recent
// heartbeat`. This codebase has NO separate heartbeat channel today — the only
// liveness signal for a running job is "is a Pod currently Running?". Therefore
// "no recent heartbeat" is implemented as "no Pod in PodRunning phase observed
// for this task". If a per-job heartbeat is added later (a follow-up spec),
// this predicate gets a real check; for now `classify` treats `pod not Running`
// as covering both halves of the conjunction.

func (s *zombieSweeper) Run(ctx context.Context) error {
    // Resolve interval once per Run by fetching configs; reusing the same
    // interval across ticks is acceptable — the executor pod is short-lived
    // relative to CRD reconfiguration cycles.
    interval, err := s.resolveSweeperInterval(ctx)
    if err != nil {
        return errors.Wrapf(ctx, err, "resolve sweeper interval")
    }
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    glog.V(2).Infof("zombie sweeper started interval=%s", interval)
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            if err := s.SweepOnce(ctx); err != nil {
                // Per-tick failures (transient lister errors, ctx-scoped
                // failures from publisher) must NOT kill the sweeper goroutine
                // — that would tear down the executor via service.Run. Log and
                // continue.
                glog.Errorf("zombie sweeper tick: %v", err)
            }
        }
    }
}

func (s *zombieSweeper) SweepOnce(ctx context.Context) error {
    snapshot := s.taskStore.Snapshot()
    now := s.currentDateTime.Now().Time()
    // Fetch configs ONCE per tick — used by taskDeadline() for every task in
    // the snapshot. Avoids N calls into the provider per sweep.
    cfgs, err := s.configProvider.Get(ctx)
    if err != nil {
        return errors.Wrapf(ctx, err, "list configs")
    }
    for taskID, task := range snapshot {
        jobName := task.Frontmatter.CurrentJob()
        if jobName == "" {
            // No active job recorded; nothing to sweep.
            continue
        }
        jobStartedAt, err := task.Frontmatter.JobStartedAt()
        if err != nil || jobStartedAt.IsZero() {
            glog.V(3).Infof(
                "zombie sweeper: task %s job_started_at unparseable or zero; skipping",
                taskID,
            )
            continue
        }
        deadline := s.taskDeadline(task, cfgs)
        elapsed := now.Sub(jobStartedAt)
        if elapsed < deadline {
            continue
        }
        reason := s.classify(taskID, task, jobName, jobStartedAt, now)
        if reason == "" {
            continue
        }
        if err := s.publisher.PublishFailure(ctx, task, jobName, reason.String()); err != nil {
            glog.Errorf(
                "zombie sweeper: publish failure for task %s (job %s reason %s): %v",
                taskID, jobName, reason, err,
            )
            continue
        }
        glog.V(2).Infof(
            "zombie sweeper: published failure for task %s (job %s reason %s elapsed=%s)",
            taskID, jobName, reason, elapsed,
        )
    }
    return nil
}
```

`taskDeadline` resolves the per-task deadline against the configs fetched once per tick by `SweepOnce`. `resolveSweeperInterval` does the same for the sweeper interval at `Run` startup:

```go
func (s *zombieSweeper) taskDeadline(task lib.Task, cfgs []agentv1.Config) time.Duration {
    assignee := task.Frontmatter.Assignee().String()
    for _, cfg := range cfgs {
        if cfg.Spec.Assignee == assignee && cfg.Spec.ZombieJobTimeoutSeconds != nil {
            return time.Duration(*cfg.Spec.ZombieJobTimeoutSeconds) * time.Second
        }
    }
    return time.Duration(agentv1.DefaultZombieJobTimeoutSeconds) * time.Second
}

func (s *zombieSweeper) resolveSweeperInterval(ctx context.Context) (time.Duration, error) {
    cfgs, err := s.configProvider.Get(ctx)
    if err != nil {
        return 0, errors.Wrapf(ctx, err, "list configs")
    }
    for _, cfg := range cfgs {
        if cfg.Spec.ZombieSweeperIntervalSeconds != nil {
            return time.Duration(*cfg.Spec.ZombieSweeperIntervalSeconds) * time.Second, nil
        }
    }
    return time.Duration(agentv1.DefaultZombieSweeperIntervalSeconds) * time.Second, nil
}
```

The "first non-nil wins" semantics is acceptable because the sweeper is a single executor-wide goroutine — there is no per-agent interval. The default is the documented behavior when no Config sets the field.

### 4. Implement `classify`

```go
// classify determines whether a past-deadline task is a zombie and which
// reason applies. Returns "" when the task is NOT a zombie (Pod still Running
// — implicit heartbeat). Inspects Pod state via the shared Pod informer's
// lister (introduced by prompt 2). Spec Failure-Mode row "k8s API rate-limit
// (429)" mandates: "Sweeper relies on informer cache (no per-cycle list)" —
// we MUST NOT issue API LIST calls here.
func (s *zombieSweeper) classify(
    taskID lib.TaskIdentifier,
    task lib.Task,
    jobName string,
    jobStartedAt time.Time,
    now time.Time,
) ZombieReason {
    selector := labels.SelectorFromSet(labels.Set{
        "agent.benjamin-borbe.de/task-id": string(taskID),
    })
    pods, err := s.podLister.Pods(s.namespace.String()).List(selector)
    if err != nil {
        glog.Errorf("zombie sweeper: lister pods for task %s: %v", taskID, err)
        return ""
    }
    // Zero pods AND past-deadline AND a Job was supposed to be running →
    // executor lost the watch (Job exists in k8s but Pod GC happened, or the
    // Job never created a Pod and was restarted across executor lifetimes).
    // "No recent heartbeat" reduces to "no Pod observed" since this codebase
    // has no separate heartbeat channel.
    if len(pods) == 0 {
        return ZombieReasonExecutorWatchLost
    }
    for _, pod := range pods {
        // Healthy Running — NOT a zombie. A Running pod is the implicit
        // heartbeat in the current system (no separate heartbeat channel).
        if pod.Status.Phase == corev1.PodRunning {
            return ""
        }
        // Pending past the unschedulable grace window with PodScheduled=False.
        if pod.Status.Phase == corev1.PodPending {
            age := now.Sub(pod.CreationTimestamp.Time)
            if age > podNotScheduledGraceWindow && hasPodScheduledFalse(pod) {
                return ZombieReasonPodNotScheduled
            }
        }
    }
    // Past deadline, no Running pod, no specific Pod-state reason — fall
    // back to deadline_exceeded.
    return ZombieReasonDeadlineExceeded
}

// hasPodScheduledFalse returns true when the Pod has a PodScheduled=False
// condition (kube-scheduler could not place the pod).
func hasPodScheduledFalse(pod *corev1.Pod) bool {
    for _, c := range pod.Status.Conditions {
        if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
            return true
        }
    }
    return false
}
```

### 5. Wire into the factory and main

In `task/executor/pkg/factory/factory.go`, add:

```go
// CreateZombieSweeper creates a deadline sweeper that classifies stuck tasks as
// zombies and emits failure events via the publisher. Interval and per-task
// deadline are sourced from the AgentConfig CRD knobs (see ConfigSpec). The
// podLister parameter is the shared Pod informer's lister introduced by
// prompt 2 — the sweeper reuses it (no per-cycle API LIST).
func CreateZombieSweeper(
    podLister corev1listers.PodLister,
    namespace libk8s.Namespace,
    taskStore *pkg.TaskStore,
    publisher pkg.ResultPublisher,
    configProvider pkg.EventHandlerConfig,
    currentDateTime libtime.CurrentDateTimeGetter,
) pkg.ZombieSweeper {
    return pkg.NewZombieSweeper(
        podLister,
        namespace,
        taskStore,
        publisher,
        configProvider,
        currentDateTime,
    )
}
```

In `task/executor/main.go`, inside `application.Run` (line 54), AFTER the existing `jobWatcher := factory.CreateJobWatcher(...)` line (line 96), add (where `podLister` is the lister exposed by the Pod informer from prompt 2 — fetch it from `jobWatcher` or the shared informer factory wired in prompt 2; the exact accessor depends on prompt 2's API):

```go
zombieSweeper := factory.CreateZombieSweeper(
    jobWatcher.PodLister(), // or whatever accessor prompt 2 exposes
    a.Namespace,
    taskStore,
    resultPublisher,
    eventHandlerConfig,
    currentDateTimeGetter,
)
```

In the `service.Run(...)` call (line 121), add `zombieSweeper.Run,` as a new argument alongside `jobWatcher.Run` and `taskEventHandler.RunDeferredRespawnLoop`. The argument list becomes:

```go
return service.Run(
    ctx,
    func(ctx context.Context) error {
        return connector.Listen(ctx, a.Namespace, resourceEventHandler)
    },
    consumer.Consume,
    taskEventHandler.RunDeferredRespawnLoop,
    jobWatcher.Run,
    zombieSweeper.Run,
    a.createHTTPServer(eventHandlerConfig, healthcheckRunner),
    healthcheckCron.Run,
)
```

Sibling entry-point check: this codebase has a single `task/executor/main.go` — verified by `find ~/Documents/workspaces/agent-zombie-detect/task/executor -name "main.go"`. No `cmd/run-once/`, no other binaries. The factory's `CreateJobWatcher` and the new `CreateZombieSweeper` are called from one site each.

### 6. Unit tests for `SweepOnce`

Create `task/executor/pkg/zombie_sweeper_test.go`. Use Ginkgo/Gomega + `FakeResultPublisher`. For the Pod lister, build a fresh shared informer factory off `fake.NewSimpleClientset()` and use `factory.Core().V1().Pods().Lister()` — seed the informer's indexer with test pods via `factory.Core().V1().Pods().Informer().GetIndexer().Add(pod)` so the lister returns them deterministically without running the informer goroutine.

Time helper: use `libtime.NewCurrentDateTime()` + `currentDateTime.SetNow(libtimetest.ParseDateTime(...))` — that is the established pattern in this codebase (see `task/executor/pkg/result_publisher_test.go:122-123` and `task/executor/pkg/handler/task_event_handler_test.go:58, 1211`). Import `libtimetest "github.com/bborbe/time/test"`.

For `EventHandlerConfig`: use the real `pkg.NewEventHandlerConfig()` impl. It is a `k8s.EventHandler[agentv1.Config]` whose `Get(ctx)` reads the in-memory state — seed it via its existing add/upsert event handler interface (read `event_handler_config.go` and the `github.com/bborbe/k8s` `EventHandler` upstream contract to see how `OnAdd`/`Upsert` is invoked; the probe and resource_event_handler tests already do this — copy the shortest pattern you find).

Test table — at least these four cells (Acceptance Criterion 6 requires them):

6a. **deadline-exceeded-and-not-running → zombie** — TaskStore has one task with `current_job: "j1"`, `job_started_at: now-30min`, `assignee: "a"`. Config has `zombieJobTimeoutSeconds: 60`. Pod lister has one Pod in `Status.Phase: PodFailed`. → `PublishFailureCallCount() == 1` with reason `"deadline_exceeded"`.

6b. **deadline-exceeded-but-running → NOT zombie** — same as 6a but the Pod is in `Status.Phase: PodRunning`. → `PublishFailureCallCount() == 0`.

6c. **under-deadline → NOT zombie** — TaskStore has a task with `job_started_at: now-30sec`, deadline 60s (elapsed strictly less than deadline). → `PublishFailureCallCount() == 0`.

6d. **watch-lost → `executor_watch_lost`** — TaskStore has a task with `job_started_at: now-30min`, but the Pod lister's indexer is empty (zero pods matching the task-id label selector). → `PublishFailureCallCount() == 1` with reason `"executor_watch_lost"`.

6e. **pod_not_scheduled** — TaskStore has a task past deadline; Pod is `Status.Phase: PodPending` with `CreationTimestamp: now-5min` and condition `PodScheduled=False`. → `PublishFailureCallCount() == 1` with reason `"pod_not_scheduled"`.

6f. **interval default** — `resolveSweeperInterval(ctx)` returns `60*time.Second` when no Config sets `ZombieSweeperIntervalSeconds`.

6g. **interval override** — `resolveSweeperInterval(ctx)` returns `15*time.Second` when a Config sets `ZombieSweeperIntervalSeconds: ptrInt32(15)`.

6h. **deadline default** — `taskDeadline(task, cfgs)` returns `1800*time.Second` when `cfgs` is empty or no Config matches the assignee with `ZombieJobTimeoutSeconds` set.

Construct Pods directly via `&corev1.Pod{ObjectMeta: ..., Status: corev1.PodStatus{...}}` with the label `agent.benjamin-borbe.de/task-id: <task-id>` set on `ObjectMeta.Labels` so the lister's selector matches.

### 7. Verify

```
cd task/executor && make precommit
```

Must exit 0. The counterfeiter mock for `ZombieSweeper` is regenerated by `make generate` (transitively invoked by precommit).
</requirements>

<constraints>
- `github.com/bborbe/errors.Wrapf(ctx, err, ...)` for wrapping; no `fmt.Errorf`; no bare `return err`.
- `libtime.CurrentDateTimeGetter` for all time math; NEVER `time.Now()` directly anywhere in the sweeper or its tests. Use `s.currentDateTime.Now().Time()` for `time.Time` values.
- Ginkgo/Gomega + counterfeiter mocks for tests.
- glog non-error logs gated with `V(2)` (success) or `V(3)` (noisy skips).
- `service.Run` for goroutine lifecycle — the sweeper's `Run(ctx)` is added as one more argument to the existing `service.Run` call in `main.go`.
- Do NOT change the controller-side `applyTriggerCap` chokepoint — the sweeper's role is to make sure `trigger_count` increments fire so the chokepoint can eventually run.
- Do NOT introduce a Prometheus metric in this prompt. The spec Non-goals list "expanded observability metrics" as out-of-scope; the spec specifies log lines (e.g. `event=zombie_dedupe`) for observability, not new counters. The existing `metrics.TaskEventsTotal` counter family is NOT extended.
- Do NOT commit — dark-factory handles git.
- Verification command is `cd task/executor && make precommit`.
- The four-cell test table in requirement 6 (6a-6d) is a strict acceptance — fewer than four cells fails AC #6 only (AC #5 is the Pod-state classifier from prompt 2).
</constraints>

<verification>
```
cd task/executor && make precommit
```

Must exit 0. Specifically:
- `SweepOnce` classifies a past-deadline task with a Failed Pod as zombie and publishes one failure with reason `"deadline_exceeded"`.
- `SweepOnce` classifies a past-deadline task with NO Pods (empty lister indexer) as `executor_watch_lost` and publishes one failure.
- `SweepOnce` skips a past-deadline task whose Pod is `PodRunning`.
- `SweepOnce` skips an under-deadline task.
- `SweepOnce` classifies a past-deadline Pending Pod older than the grace window with PodScheduled=False as `pod_not_scheduled`.
- `resolveSweeperInterval` returns `60s` by default and `15s` when configured.
- `main.go` constructs and passes `zombieSweeper.Run` to `service.Run`.
</verification>
