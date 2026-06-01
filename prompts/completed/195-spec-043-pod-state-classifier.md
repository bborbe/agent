---
status: completed
spec: [043-executor-zombie-job-detection]
summary: Added ZombieReason enum, Pods informer to JobWatcher, and updated job failure classification to emit stable reason strings
container: agent-zombie-detect-exec-195-spec-043-pod-state-classifier
dark-factory-version: v0.173.0
created: "2026-06-01T20:30:00Z"
queued: "2026-06-01T20:11:58Z"
started: "2026-06-01T20:16:41Z"
completed: "2026-06-01T20:20:30Z"
---

<summary>
- Introduces a closed reason enum used by every zombie / type-mismatch failure publish (image_pull_backoff, pod_evicted, pod_not_scheduled, pod_crash_no_stdout, deadline_exceeded, executor_watch_lost, type_mismatch).
- Extends the existing job watcher to also watch Pods so the executor classifies failures the Job-condition path misses: ImagePullBackOff, evicted, crashed-before-stdout.
- Each Pod-state failure emits exactly one event via the (already-doctrine-correct) `PublishFailure` from prompt 1.
- Maps existing Job-condition reasons to the typed enum so the on-disk `## Failure` body section contains a stable grep-able string instead of arbitrary k8s messages.
- Pod_not_scheduled is deferred to the sweeper (prompt 4) because it needs a grace window the informer cannot evaluate.
</summary>

<objective>
Make `task/executor/pkg/job_watcher.go` detect Pod-level failure conditions (ImagePullBackOff, evicted, crash-no-stdout) and emit them through `PublishFailure` with a stable reason string from a fixed enum. Also narrow the existing Job-condition path's reason string into the same enum.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Spec: `specs/in-progress/043-executor-zombie-job-detection.md` (Desired Behavior 3, 8, 9; Acceptance Criterion 5; Failure Modes rows for ImagePullBackOff, evicted, crash-no-stdout).

Files to read before changing:
- `task/executor/pkg/job_watcher.go` — current implementation. `HandleJob` (line 98), `publishSyntheticFailure` (line 143), `isJobFailed` (line 178), `jobFailureReason` (line 196).
- `task/executor/pkg/result_publisher.go` — `PublishFailure` is now retry-aware (per prompt 1). This prompt only calls it; do not modify the publisher.
- `task/executor/pkg/task_store.go` — `TaskStore.Load`. The Pods informer looks up the owning task via the same `agent.benjamin-borbe.de/task-id` label that Jobs carry.
- `task/executor/pkg/spawner/job_spawner.go:275` — `applyTaskIDLabel` sets `job.Spec.Template.Labels[taskIDLabelKey]`, which means Pods spawned by the Job inherit the label. The Pods informer uses the same label selector.
- `task/executor/mocks/result_publisher.go` — `FakeResultPublisher` for tests.

Coding plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-enum-type-pattern.md`
</context>

<requirements>
### 1. Add the reason enum

Create `task/executor/pkg/zombie_reason.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

// ZombieReason is the closed set of machine-readable reason strings emitted in
// the ## Failure body section. Operators grep on these values to triage.
// Adding a new value requires updating this list and the documentation; renaming
// or removing a value is a breaking change to the on-disk task body contract.
type ZombieReason string

const (
    ZombieReasonImagePullBackOff  ZombieReason = "image_pull_backoff"
    ZombieReasonPodEvicted        ZombieReason = "pod_evicted"
    ZombieReasonDeadlineExceeded  ZombieReason = "deadline_exceeded"
    ZombieReasonPodNotScheduled   ZombieReason = "pod_not_scheduled"
    ZombieReasonPodCrashNoStdout  ZombieReason = "pod_crash_no_stdout"
    ZombieReasonExecutorWatchLost ZombieReason = "executor_watch_lost"
    ZombieReasonTypeMismatch      ZombieReason = "type_mismatch"
)

// String returns the reason as a string (for use with PublishFailure).
func (r ZombieReason) String() string { return string(r) }
```

Add `task/executor/pkg/zombie_reason_test.go` (external test package `pkg_test`) that asserts each constant's string value is the spec's verbatim lower_snake string. This is the level-1 boundary test confirming the reason strings are stable wire contract.

### 2. Narrow the Job-condition reason into the enum

Rationale for bundling `BackoffLimitExceeded` under `ZombieReasonDeadlineExceeded`: both are k8s killing the pod for resource-policy reasons (activeDeadlineSeconds expiry vs. backoffLimit exhaustion); operators triaging see the same "killed by controller, not by app" semantics. If operators later need to distinguish, file a follow-up to split into a separate enum value.

In `task/executor/pkg/job_watcher.go`, replace the existing `jobFailureReason(job *batchv1.Job) string` helper with:

```go
// jobFailureReason maps a failed Job's conditions to a ZombieReason. Returns
// ZombieReasonDeadlineExceeded when any Failed condition has Reason
// "DeadlineExceeded" or "BackoffLimitExceeded" (kubelet killed the pod for
// running past activeDeadlineSeconds or exhausting BackoffLimit). Returns
// ZombieReasonPodCrashNoStdout for any other Failed condition (the pod
// terminated non-zero and no AgentResult was observed; the Job-condition
// informer only fires AFTER terminal state, so absence of an AgentResult is
// implicit at this point).
func jobFailureReason(job *batchv1.Job) ZombieReason {
    for _, c := range job.Status.Conditions {
        if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
            switch c.Reason {
            case "DeadlineExceeded", "BackoffLimitExceeded":
                return ZombieReasonDeadlineExceeded
            }
        }
    }
    return ZombieReasonPodCrashNoStdout
}
```

Update `HandleJob` (line 98) to consume the new return type. No source change at L106 — the local variable's static type changes from `string` to `ZombieReason` because the helper return type changed. Update the `glog.V(2).Infof` log line at line 107 to use `reason` (a `ZombieReason` prints fine via `%s`).

Update `handleTerminal` (line 128) and `publishSyntheticFailure` (line 143) signatures so the `reason` parameter is `ZombieReason` instead of `string`. Inside `publishSyntheticFailure`, the call `w.publisher.PublishFailure(ctx, task, job.Name, reason)` requires a string — change it to `w.publisher.PublishFailure(ctx, task, job.Name, reason.String())`.

Note: `publishSyntheticFailure` retains its existing `taskStore.Delete(taskID)` call (current `job_watcher.go:155`). Only the Job-condition path (`HandleJob` → `publishSyntheticFailure`) owns the final TaskStore delete; `HandlePod` does NOT delete — see the inline comment in requirement 4 below.

### 3. No changes to `result_publisher.go`

The body format from prompt 1 (`1-spec-043-doctrine-publishers-and-dedupe.md`) already satisfies spec AC #4 — that prompt rewrites `PublishTypeMismatchFailure` end-to-end including adding `reason=type_mismatch` to the `## Failure` body. Drop any `result_publisher_test.go` assertions for type-mismatch body content from this prompt's scope; prompt 1 owns those tests.

### 3. Add the Pods informer

In `task/executor/pkg/job_watcher.go`, extend the `JobWatcher` interface so unit tests can drive the new Pod path directly without an informer:

```go
type JobWatcher interface {
    Run(ctx context.Context) error
    HandleJob(ctx context.Context, job *batchv1.Job)
    HandlePod(ctx context.Context, pod *corev1.Pod)
}
```

Regenerate the counterfeiter mock for `JobWatcher`: this is auto-handled by `make generate` invoked from `make precommit`. The `//counterfeiter:generate` directive at line 24 already targets the interface — the mock will pick up the new method.

In `jobWatcher.Run`, after registering the existing Jobs informer event handler, add a Pods informer using the SAME `k8sinformers.SharedInformerFactoryWithOptions` factory already created at line 59 (same namespace, same label selector `agent.benjamin-borbe.de/task-id`, same 5-minute resync period). Pods inherit the task-id label from the Job's pod template (verified in `task/executor/pkg/spawner/job_spawner.go:applyTaskIDLabel`).

```go
podInformer := factory.Core().V1().Pods().Informer()
_, err = podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc: func(obj interface{}) {
        pod, ok := obj.(*corev1.Pod)
        if !ok {
            return
        }
        w.HandlePod(ctx, pod)
    },
    UpdateFunc: func(_, newObj interface{}) {
        pod, ok := newObj.(*corev1.Pod)
        if !ok {
            return
        }
        w.HandlePod(ctx, pod)
    },
})
if err != nil {
    return errors.Wrapf(ctx, err, "add pod informer event handler")
}
```

The existing single `factory.Start(ctx.Done())` call covers both informers (the shared factory starts all informers registered against it). Extend the existing `cache.WaitForCacheSync` call so it waits for BOTH `informer.HasSynced` and `podInformer.HasSynced`:

```go
if !cache.WaitForCacheSync(ctx.Done(), informer.HasSynced, podInformer.HasSynced) {
    return errors.Errorf(ctx, "timed out waiting for job/pod informer cache sync")
}
```

### 4. Implement `HandlePod`

Add to `task/executor/pkg/job_watcher.go`:

```go
func (w *jobWatcher) HandlePod(ctx context.Context, pod *corev1.Pod) {
    taskIDStr, ok := pod.Labels["agent.benjamin-borbe.de/task-id"]
    if !ok || taskIDStr == "" {
        return
    }
    taskID := lib.TaskIdentifier(taskIDStr)

    reason := classifyPodFailure(pod)
    if reason == "" {
        return
    }

    task, ok := w.taskStore.Load(taskID)
    if !ok {
        glog.V(3).Infof(
            "pod %s/%s (task %s) in %s state but task not in store; sweeper will handle if still in flight",
            pod.Namespace, pod.Name, taskID, reason,
        )
        return
    }

    jobName := ownerJobName(pod)
    if jobName == "" {
        glog.V(2).Infof(
            "pod %s/%s (task %s) in %s state but has no Job ownerRef; ignoring",
            pod.Namespace, pod.Name, taskID, reason,
        )
        return
    }

    if err := w.publisher.PublishFailure(ctx, task, jobName, reason.String()); err != nil {
        glog.Errorf(
            "publish pod-state failure for task %s (pod %s reason %s): %v",
            taskID, pod.Name, reason, err,
        )
        return
    }
    glog.V(2).Infof(
        "published pod-state failure for task %s (pod %s reason %s)",
        taskID, pod.Name, reason,
    )
    // Do NOT call w.taskStore.Delete here. The pod may transition again (e.g. evicted then
    // rescheduled). The Job-condition path or the deadline sweeper performs the final delete
    // when terminal state is observed. Dedupe in PublishFailure (prompt 1) prevents
    // double-publish for the same job name.
}

// classifyPodFailure returns a non-empty ZombieReason when the Pod is in a
// terminal failure state we recognize. Returns "" for healthy, pending-without-
// excessive-delay, and any state we should not act on from the informer path.
// pod_not_scheduled is deliberately NOT returned here — it requires a grace
// window the informer cannot evaluate (a freshly created Pod is always briefly
// Pending before scheduling). The deadline sweeper (separate prompt) owns that
// classification.
func classifyPodFailure(pod *corev1.Pod) ZombieReason {
    for _, cs := range pod.Status.ContainerStatuses {
        if cs.State.Waiting != nil {
            switch cs.State.Waiting.Reason {
            case "ImagePullBackOff", "ErrImagePull":
                return ZombieReasonImagePullBackOff
            }
        }
    }
    if pod.Status.Reason == "Evicted" {
        return ZombieReasonPodEvicted
    }
    if pod.Status.Phase == corev1.PodFailed {
        for _, cs := range pod.Status.ContainerStatuses {
            if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
                return ZombieReasonPodCrashNoStdout
            }
        }
    }
    return ""
}

// ownerJobName returns the name of the Job that owns the Pod, or "" when no
// Job ownerRef is present.
func ownerJobName(pod *corev1.Pod) string {
    for _, ref := range pod.OwnerReferences {
        if ref.Kind == "Job" {
            return ref.Name
        }
    }
    return ""
}
```

### 5. Unit tests

In `task/executor/pkg/job_watcher_test.go` (extend the existing file; add new `Describe` blocks):

5a. **`Describe("HandlePod")` — table-driven Pod-state classifier.** Each entry constructs a `corev1.Pod` with the right state plus the `agent.benjamin-borbe.de/task-id` label and a Job ownerRef, seeds the `TaskStore` with a matching task, calls `HandlePod`, and asserts:
- `FakeResultPublisher.PublishFailureCallCount() == 1`
- The third argument (`reason string`) equals the expected `ZombieReason.String()`

Rows:
- ImagePullBackOff: `pod.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}` → expect `"image_pull_backoff"`.
- ErrImagePull: same with `Reason: "ErrImagePull"` → expect `"image_pull_backoff"` (same reason, different k8s string).
- Evicted: `pod.Status.Reason = "Evicted"` → expect `"pod_evicted"`.
- Crash: `pod.Status.Phase = corev1.PodFailed`, `pod.Status.ContainerStatuses[0].State.Terminated = &corev1.ContainerStateTerminated{ExitCode: 137}` → expect `"pod_crash_no_stdout"`.
- Healthy Running pod: assert `PublishFailureCallCount() == 0`.

5b. **`Describe("HandlePod no task in store")`** — Pod with label set but `TaskStore` empty; assert `PublishFailureCallCount() == 0` and no panic.

5c. **`Describe("HandlePod no ownerRef")`** — Pod in ImagePullBackOff but with empty `OwnerReferences`; assert `PublishFailureCallCount() == 0`.

5d. **`Describe("jobFailureReason mapping")`** — three rows: Failed condition `Reason: "DeadlineExceeded"` → `ZombieReasonDeadlineExceeded`; Failed condition `Reason: "BackoffLimitExceeded"` → `ZombieReasonDeadlineExceeded`; Failed condition `Reason: ""` → `ZombieReasonPodCrashNoStdout`.

5e. **`Describe("HandleJob with DeadlineExceeded")`** — regression test: Job with condition `Reason: "DeadlineExceeded"` triggers `PublishFailure` with `reason == "deadline_exceeded"`. Use `FakeResultPublisher.PublishFailureArgsForCall(0)` to read back the third argument.

All tests use Ginkgo/Gomega + the regenerated `FakeJobWatcher` and existing `FakeResultPublisher`. Construct the `jobWatcher` directly with a fake `kubernetes.Interface` (`fake.NewSimpleClientset()` from `k8s.io/client-go/kubernetes/fake`) so `HandlePod` and `HandleJob` can be driven without a real informer (the existing tests already use this pattern for `HandleJob`).

### 6. Verify

```
cd task/executor && make precommit
```

Must exit 0. The build will regenerate the counterfeiter mock for `JobWatcher` automatically (`go generate` triggered by precommit). If `make precommit` fails because the mock is out of date, run `make generate` once explicitly.
</requirements>

<constraints>
- `github.com/bborbe/errors.Wrapf(ctx, err, ...)` for wrapping; no `fmt.Errorf`; no bare `return err`.
- Ginkgo/Gomega + counterfeiter mocks for tests.
- glog non-error logs gated with `V(n)` (use `V(2)` for the standard success path, `V(3)` for defensive skip-noise paths).
- Do NOT modify `result_publisher.go` — the doctrine work (including the type-mismatch body format) landed in prompt 1.
- Do NOT introduce the deadline sweeper or CRD knobs here — prompts 3 and 4.
- Do NOT commit — dark-factory handles git.
- Verification command is `cd task/executor && make precommit`.
</constraints>

<verification>
```
cd task/executor && make precommit
```

Must exit 0. Specifically:
- `ZombieReason` constants exist with the seven values from spec DB #8.
- Pod with `Status.ContainerStatuses[].State.Waiting.Reason == "ImagePullBackOff"` triggers one `PublishFailure` call with reason `"image_pull_backoff"`.
- Pod with `Status.Reason == "Evicted"` triggers one `PublishFailure` call with reason `"pod_evicted"`.
- Pod with `Status.Phase == PodFailed` and a non-zero terminated container triggers one `PublishFailure` call with reason `"pod_crash_no_stdout"`.
- Job condition `Reason == "DeadlineExceeded"` triggers `PublishFailure` with reason `"deadline_exceeded"`.
- Job condition `Reason == "BackoffLimitExceeded"` triggers `PublishFailure` with reason `"deadline_exceeded"`.
</verification>
