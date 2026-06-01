---
status: approved
spec: [043-executor-zombie-job-detection]
created: "2026-06-01T20:30:00Z"
queued: "2026-06-01T20:11:58Z"
---

<summary>
- Adds a single envtest that exercises the Pods informer wiring against a real in-process kube-apiserver (sigs.k8s.io/controller-runtime/pkg/envtest).
- Test creates a Pod whose container has an obviously-bogus image reference, then forces the Pod into an ImagePullBackOff-equivalent status via a Status subresource update (envtest does not run a kubelet, so we simulate the status the informer would observe in a real cluster).
- Verifies the executor's `JobWatcher.HandlePod` (driven by the real informer) emits exactly one `PublishFailure` with reason `"image_pull_backoff"` within `2 × zombieSweeperIntervalSeconds`.
- Introduces `sigs.k8s.io/controller-runtime/pkg/envtest` as a test-only dependency on the executor module.
- After this prompt, the round-trip from "Pod transitions to ImagePullBackOff in the API server" to "executor emits PublishFailure with the right reason" is covered against real informer machinery, not a hand-rolled mock.
</summary>

<objective>
Prove against a real (in-process) Kubernetes API server that the executor's Pods informer correctly classifies an ImagePullBackOff Pod and emits one `PublishFailure` with reason `"image_pull_backoff"` within `2 × zombieSweeperIntervalSeconds` of the Pod entering that state.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Spec: `specs/in-progress/043-executor-zombie-job-detection.md` (Acceptance Criterion 9; Scenario coverage note that explicitly limits scenarios; the rest of the manual verification is for `agent-dev` post-deploy and is NOT in scope for this prompt).

Files to read before changing:
- `task/executor/pkg/job_watcher.go` (as updated by prompt 2) — `JobWatcher.Run` starts both Jobs and Pods informers via a shared factory; the envtest drives `Run` against a real apiserver.
- `task/executor/pkg/result_publisher.go` — but the test does NOT use the real publisher; it injects a `FakeResultPublisher` from `task/executor/mocks/result_publisher.go` so the test asserts on `PublishFailureCallCount()` / `PublishFailureArgsForCall(0)`.
- `task/executor/pkg/task_store.go` — the envtest seeds one task in the store so the watcher's lookup succeeds.
- `task/executor/go.mod` — the executor module currently depends on `k8s.io/client-go v0.36.1`. `sigs.k8s.io/controller-runtime/pkg/envtest` is compatible with that line of client-go; pick a controller-runtime version aligned with the client-go major (e.g. `v0.21.x` for client-go 0.36 — check the controller-runtime release notes for the exact pairing).
- `task/executor/Makefile` — `make precommit` is the verification entrypoint. Envtest binaries (etcd, kube-apiserver) come from `setup-envtest`; the Makefile may need a new target `envtest-setup` that downloads them, OR the test can use `setup-envtest` programmatically. Read the Makefile first; if `setup-envtest` is not yet wired, add the minimal lines (see requirement 5).

Coding plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-kubernetes-crd-controller-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mod-dependency-fix-guide.md`
</context>

<requirements>
### 1. Add the envtest dependency

In `task/executor/go.mod`, add `sigs.k8s.io/controller-runtime` (test-only — only imported from `_test.go` files in the executor module). Pin the version to the line compatible with `k8s.io/client-go v0.36.1` — controller-runtime `v0.21.x` is the conventional pairing; if `v0.21.x` does not exist when this prompt is executed, use the latest `v0.MAJOR.x` whose go.mod requires `k8s.io/client-go v0.36.x`. To verify pairing without guessing, run:

```bash
cd task/executor
go get -t sigs.k8s.io/controller-runtime@latest
go mod tidy
```

If the resulting `go.mod` ends up with a client-go bump that conflicts with the rest of the workspace's pinned `v0.36.1`, instead pin the controller-runtime version explicitly:

```bash
go get -t sigs.k8s.io/controller-runtime@v0.21.0
```

Adjust until `go mod tidy && go build ./...` succeeds. Document the chosen version in a comment in the new test file's import block.

**After `go mod tidy`, verify `grep 'k8s.io/client-go' go.mod` still shows `v0.36.1`** — if `go mod tidy` bumped it (controller-runtime's go.mod typically requires a recent client-go), downgrade `controller-runtime` until client-go is stable at `v0.36.1`. Iterate: `go get -t sigs.k8s.io/controller-runtime@v0.21.<N-1>` and re-tidy until the pin holds.

**Verify `make precommit` exits 0 end-to-end after the dep add** — including `vulncheck` / `osv-scanner` / `trivy` against the expanded transitive closure (controller-tools, klog v2, etc.). If any of these scanners report new findings, fix them in this prompt; do NOT defer.

### 2. Set up envtest binaries

The envtest framework needs `etcd` and `kube-apiserver` binaries on disk. The conventional source is the `setup-envtest` tool. In `task/executor/Makefile`, add (if not already present) a target:

```makefile
ENVTEST_K8S_VERSION ?= 1.31.0
ENVTEST_DIR := $(shell go env GOPATH)/pkg/envtest

.PHONY: envtest-setup
envtest-setup:
	@command -v setup-envtest >/dev/null 2>&1 || go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@setup-envtest use $(ENVTEST_K8S_VERSION) -p path > /dev/null
```

Add `envtest-setup` as a dependency of the existing `test` (or `precommit`) target so `make precommit` automatically downloads the binaries on first run. The exact wiring depends on the existing Makefile shape — preserve existing targets and add the new dependency in a backward-compatible way (e.g. `test: envtest-setup` if the existing `test` target is `test:`).

**Do NOT export `KUBEBUILDER_ASSETS` at file scope.** A file-scope `export KUBEBUILDER_ASSETS := $(shell setup-envtest use ...)` is evaluated on every Make invocation before any target runs — including `envtest-setup` itself — so on a clean machine (where `setup-envtest` is not yet installed) the `$(shell ...)` resolves to an empty string and is captured for the lifetime of the Make run.

Instead, set `KUBEBUILDER_ASSETS` as a **recipe-line prefix inside the test target** so it evaluates after `envtest-setup` has run:

```makefile
.PHONY: test-envtest
test-envtest: envtest-setup
	ENVTEST_REQUIRED=1 KUBEBUILDER_ASSETS=$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path) \
		go test -tags=envtest ./pkg/...
```

The `$$` escapes for Make so the shell expands `setup-envtest` at recipe execution time.

### 3. Add the envtest

Create `task/executor/pkg/job_watcher_envtest_test.go`:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build envtest

package pkg_test

import (
    "context"
    "fmt"
    "testing"
    "time"

    libk8s "github.com/bborbe/k8s"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "sigs.k8s.io/controller-runtime/pkg/envtest"

    lib "github.com/bborbe/agent/lib"
    pkg "github.com/bborbe/agent/task/executor/pkg"
    mocks "github.com/bborbe/agent/task/executor/mocks"
)
```

The `//go:build envtest` build tag isolates the heavy test from the default `go test ./...` run; `make precommit` invokes `go test -tags=envtest ./pkg/...` to include it. This is the conventional pattern for envtest in projects that also want fast unit tests.

Test body:

```go
func TestEnvtest(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "executor envtest suite")
}

var _ = Describe("JobWatcher (envtest)", func() {
    var (
        testEnv    *envtest.Environment
        cfg        *rest.Config
        kubeClient kubernetes.Interface
        ctx        context.Context
        cancel     context.CancelFunc
    )

    BeforeEach(func() {
        testEnv = &envtest.Environment{}
        var err error
        cfg, err = testEnv.Start()
        Expect(err).NotTo(HaveOccurred())
        kubeClient, err = kubernetes.NewForConfig(cfg)
        Expect(err).NotTo(HaveOccurred())
        ctx, cancel = context.WithCancel(context.Background())
    })

    AfterEach(func() {
        cancel()
        Expect(testEnv.Stop()).To(Succeed())
    })

    It("classifies ImagePullBackOff and publishes one failure within the bound", func() {
        ns := "default"
        taskID := lib.TaskIdentifier("envtest-task-1")
        jobName := "envtest-job-1"
        publisher := &mocks.FakeResultPublisher{}
        store := pkg.NewTaskStore()
        store.Store(taskID, lib.Task{
            TaskIdentifier: taskID,
            Frontmatter: lib.TaskFrontmatter{
                "current_job": jobName,
                "assignee":    "envtest-agent",
            },
        })
        watcher := pkg.NewJobWatcher(kubeClient, libk8s.Namespace(ns), store, publisher)

        // Start the watcher in a goroutine; cancel via ctx in AfterEach.
        runErrCh := make(chan error, 1)
        go func() { runErrCh <- watcher.Run(ctx) }()

        // Create a Pod with the task-id label and a bogus image. envtest does
        // not run a kubelet, so we will inject the ImagePullBackOff status
        // ourselves via the Status subresource below; the informer sees the
        // status update the same way it would in a real cluster.
        pod := &corev1.Pod{
            ObjectMeta: metav1.ObjectMeta{
                Name:      "envtest-pod-1",
                Namespace: ns,
                Labels: map[string]string{
                    "agent.benjamin-borbe.de/task-id": string(taskID),
                },
                OwnerReferences: []metav1.OwnerReference{
                    {APIVersion: "batch/v1", Kind: "Job", Name: jobName, UID: "fake-job-uid"},
                },
            },
            Spec: corev1.PodSpec{
                RestartPolicy: corev1.RestartPolicyNever,
                Containers: []corev1.Container{
                    {Name: "agent", Image: "docker.example.com/does-not-exist:envtest"},
                },
            },
        }
        _, err := kubeClient.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{})
        Expect(err).NotTo(HaveOccurred(), "if Create returns 422, add any other missing required-by-validator defaults; do not silently catch")

        // Status subresource update flow — 4 steps to avoid ResourceVersion races
        // and default-mutator overwrites:
        //   1. Get the canonical Pod (fresh ResourceVersion).
        //   2. Mutate Status on the fetched object.
        //   3. UpdateStatus with the fetched object.
        //   4. Get again to confirm the status survived (no mutator clobbered it).
        // Step 1: Get
        fetched, err := kubeClient.CoreV1().Pods(ns).Get(ctx, "envtest-pod-1", metav1.GetOptions{})
        Expect(err).NotTo(HaveOccurred())
        // Step 2: mutate Status on the freshly-fetched object
        fetched.Status.Phase = corev1.PodPending
        fetched.Status.ContainerStatuses = []corev1.ContainerStatus{
            {
                Name: "agent",
                State: corev1.ContainerState{
                    Waiting: &corev1.ContainerStateWaiting{
                        Reason:  "ImagePullBackOff",
                        Message: "Back-off pulling image",
                    },
                },
            },
        }
        // Step 3: UpdateStatus
        _, err = kubeClient.CoreV1().Pods(ns).UpdateStatus(ctx, fetched, metav1.UpdateOptions{})
        Expect(err).NotTo(HaveOccurred())
        // Step 4: Get to confirm the status survived (no default mutator reverted Phase
        // to Pending or dropped the Waiting state mid-update).
        confirmed, err := kubeClient.CoreV1().Pods(ns).Get(ctx, "envtest-pod-1", metav1.GetOptions{})
        Expect(err).NotTo(HaveOccurred())
        Expect(confirmed.Status.ContainerStatuses).To(HaveLen(1))
        Expect(confirmed.Status.ContainerStatuses[0].State.Waiting).NotTo(BeNil())
        Expect(confirmed.Status.ContainerStatuses[0].State.Waiting.Reason).To(Equal("ImagePullBackOff"))

        // Acceptance bound: 2 * zombieSweeperIntervalSeconds = 2 * 60s = 120s.
        // In practice the informer reacts in well under a second once the
        // status update lands; we use a generous wait with polling to stay
        // well inside the bound while keeping the test fast.
        Eventually(publisher.PublishFailureCallCount, 30*time.Second, 100*time.Millisecond).
            Should(Equal(1), "expected one PublishFailure call within bound")

        // Confirm "exactly one" — Eventually passes at the FIRST observation of 1;
        // Consistently verifies no second call lands over a short follow-up window.
        Consistently(publisher.PublishFailureCallCount, 2*time.Second, 200*time.Millisecond).
            Should(Equal(1), "expected exactly one PublishFailure call (no duplicates)")

        _, _, gotJobName, gotReason := publisher.PublishFailureArgsForCall(0)
        Expect(gotJobName).To(Equal(jobName))
        Expect(gotReason).To(Equal(string(pkg.ZombieReasonImagePullBackOff)))
    })
})
```

Verify the signature of `FakeResultPublisher.PublishFailureArgsForCall` against the regenerated mock. The counterfeiter mock returns positional arguments matching the interface — for `PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error` the returns are `(context.Context, lib.Task, string, string)`. The test reads positional element 2 (jobName) and element 3 (reason). Adjust if the regenerated mock differs.

### 4. Wire envtest into precommit

Confirm the executor's existing `make test` (or wherever `go test` runs from inside `make precommit`) uses the `envtest` build tag when envtest binaries are present. Two acceptable shapes:

4a. Add a new line in the Makefile's test target:
```makefile
test: envtest-setup
	go test ./...
	ENVTEST_REQUIRED=1 KUBEBUILDER_ASSETS=$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path) \
		go test -tags=envtest ./pkg/...
```

4b. Or a dedicated `make test-envtest` target that `precommit` invokes (preferred — matches section 2's recipe):
```makefile
.PHONY: test-envtest
test-envtest: envtest-setup
	ENVTEST_REQUIRED=1 KUBEBUILDER_ASSETS=$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path) \
		go test -tags=envtest ./pkg/...

precommit: ... test-envtest
```

Pick the shape closest to the existing Makefile's pattern. Both forms satisfy AC #9 ("envtest passes (exit code 0)") and both set `ENVTEST_REQUIRED=1` so a missing `KUBEBUILDER_ASSETS` becomes a `Fail` instead of a silent skip (see section 5).

### 5. Test environment skip-when-unavailable (with required-mode gate)

The envtest binaries are not present on every developer workstation (only in CI / `make precommit`). Add a `BeforeSuite` that skips for interactive use but **fails when invoked from `make precommit`** (so a misconfigured precommit cannot silently exit 0 with zero envtest coverage):

```go
var _ = BeforeSuite(func() {
    if os.Getenv("KUBEBUILDER_ASSETS") == "" {
        if os.Getenv("ENVTEST_REQUIRED") == "1" {
            Fail("KUBEBUILDER_ASSETS not set but ENVTEST_REQUIRED=1; envtest binaries must be available under precommit")
        }
        Skip("KUBEBUILDER_ASSETS not set; run via `make test-envtest` or `make precommit`")
    }
})
```

Add `os` to the imports.

The Makefile's `test-envtest` recipe (shown in section 2 above) sets `ENVTEST_REQUIRED=1` before invoking `go test`, so the skip becomes a `Fail` whenever the suite runs under Make. Interactive `go test -tags=envtest ./...` without `KUBEBUILDER_ASSETS` set still skips cleanly (no `ENVTEST_REQUIRED` in the env).

### 6. Verify

```
cd task/executor && make precommit
```

Must exit 0. On a clean machine the first run downloads the envtest binaries (`setup-envtest` cache lives in `~/.local/share/kubebuilder-envtest/`); subsequent runs reuse the cache. If the download fails (network unavailable), `make precommit` MUST still exit non-zero — the test is not optional once wired into precommit.

### 7. Non-requirements (explicit out-of-scope)

- This prompt does NOT add envtests for the deadline sweeper, the doctrine publisher, or the type-mismatch path. Those are adequately covered by the prompts 1, 2, and 4 unit tests; the spec's Scenario coverage section pins envtest scope to exactly the ImagePullBackOff path.
- This prompt does NOT introduce a `setup-envtest` binary check that opportunistically skips when assets are absent under `make precommit` — that would defeat AC #9.
- This prompt does NOT modify the JobWatcher implementation; it only verifies the behavior from prompt 2 against real informer wiring.

### 8. Edge case to confirm

The `JobWatcher.Run` starts informers via a `SharedInformerFactoryWithOptions`. envtest provides a real apiserver, so the informer connects normally. The single subtlety: `WaitForCacheSync` must complete before the test creates the Pod (otherwise the AddFunc event handler may not fire for objects created during the initial list+watch). The test relies on the watcher's `Run` blocking on `<-ctx.Done()` AFTER `WaitForCacheSync` returns; ensure the `Eventually` poll begins AFTER the Pod's status update lands (the Create+UpdateStatus sequence is sequential, so by the time `UpdateStatus` returns the watch is already established). If flakes appear, add a `time.Sleep(500*time.Millisecond)` between starting the watcher goroutine and creating the Pod — but only if necessary; the synchronous Update return should suffice.
</requirements>

<constraints>
- **Depends on prompt 2 (Pod-state classifier) being completed first.** If `JobWatcher.HandlePod` or `pkg.ZombieReasonImagePullBackOff` do not exist in `task/executor/pkg/`, stop and report blocker — do NOT attempt to implement them in this prompt.
- `github.com/bborbe/errors.Wrapf(ctx, err, ...)` for wrapping; no `fmt.Errorf`; no bare `return err`.
- Ginkgo/Gomega tests; counterfeiter `FakeResultPublisher` for capturing publish calls.
- `libtime.CurrentDateTimeGetter` injection is NOT required in this prompt — the envtest exercises the Pods informer path which does not perform deadline math; the test asserts the publisher is called within a wall-clock bound.
- envtest is gated by the `envtest` build tag — `go test ./...` (default) MUST remain fast (~seconds). Only `go test -tags=envtest ./...` invokes the heavy path.
- Do NOT add envtests beyond the single ImagePullBackOff path — Scenario coverage in the spec explicitly limits scope.
- Do NOT commit — dark-factory handles git.
- Verification command is `cd task/executor && make precommit`.
- The Acceptance bound is `2 * zombieSweeperIntervalSeconds` = 120s; the test polls with `Eventually(...).WithTimeout(30*time.Second)` which is well inside the bound and keeps test wall time low.
</constraints>

<verification>
```
cd task/executor && make precommit
```

Must exit 0. Specifically:
- The envtest spins up an in-process kube-apiserver.
- Creates a Pod labelled `agent.benjamin-borbe.de/task-id=envtest-task-1`.
- Sets the Pod status to `ImagePullBackOff` via the Status subresource.
- The executor's `JobWatcher.HandlePod` (driven by the real informer) fires and calls `publisher.PublishFailure` exactly once.
- The captured reason argument equals `"image_pull_backoff"`.
- All happens within 30s wall-clock (well under the spec's 120s bound).
</verification>
