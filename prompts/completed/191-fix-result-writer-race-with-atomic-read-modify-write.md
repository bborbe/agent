---
status: completed
summary: Replaced AtomicWriteAndCommitPush with AtomicReadModifyWriteAndCommitPush in result writer, adding buildResultModifyFn that re-reads on-disk content per retry to eliminate write-after-read race
container: agent-exec-191-fix-result-writer-race-with-atomic-read-modify-write
dark-factory-version: v0.173.0
created: "2026-05-26T11:40:54Z"
queued: "2026-05-26T11:40:54Z"
started: "2026-05-26T11:40:56Z"
completed: "2026-05-26T11:45:20Z"
---
<summary>
- Fixes a healthcheck loop where agent terminal results (status: completed, phase: done) get reverted to status: in_progress by a stale on-disk snapshot
- Eliminates a read/write race between the controller's WriteResult path and the executor's partial-update path on the same task file
- Converts the result-writeback to re-parse fresh on-disk content inside a git-retry callback, so concurrent updates can no longer be silently overwritten
- No interface or behavior change for callers; merge semantics and cap/escalation rules stay identical
- Preserves all existing result_writer_test.go assertions; only the mock-method expectation flips from one Atomic helper to the other
- Adds one regression test simulating an interleaved partial update between the read and the write
</summary>

<objective>
Fix a write-after-read race in the agent task controller's result-writeback. The controller's `WriteResult` currently performs read-merge-write via `gitClient.AtomicWriteAndCommitPush`. When the executor's partial-update path writes between the read and the write, the controller's stale snapshot wins and rolls back state — observable as healthcheck probe tasks looping until trigger-cap escalation. Convert `WriteResult` to use `gitClient.AtomicReadModifyWriteAndCommitPush`, mirroring the exact pattern already used by `task_update_frontmatter_executor.go`. The modify callback re-runs against fresh on-disk content on every git retry, so concurrent partial updates are preserved instead of being clobbered.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` for retry/CAS guidance.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` for Ginkgo + counterfeiter test conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` for `github.com/bborbe/errors` Wrap/Wrapf usage.

Files to read before making changes:
- `task/controller/pkg/result/result_writer.go` — the file being changed. Read all of it; the helpers `mergeFrontmatter`, `applyRetryCounter`, `ExtractFrontmatter`, `ExtractBody`, `clearAssignee`, `ClearAssigneeIfHumanReview`, `applyTriggerCap`, `applyRetryCap`, `containsEscalationSection`, `restoreExistingPhase` all stay unchanged but the new modifyFn calls into them.
- `task/controller/pkg/command/task_update_frontmatter_executor.go` — the structural exemplar. The `AtomicReadModifyWriteAndCommitPush` call at lines 73-90 and the `buildUpdateModifyFn` callback at lines 95-125 are the exact shape to mirror.
- `task/controller/pkg/command/task_update_frontmatter_executor_test.go` lines 61-76 — the canonical `AtomicReadModifyWriteAndCommitPushStub` wiring (reads from disk, calls modify, writes back) used to make the fake `GitClient` mock execute the callback.
- `task/controller/pkg/gitrestclient/git_rest_client.go` lines 290-332 — the `GitClient` interface. The exact signature of `AtomicReadModifyWriteAndCommitPush` is:
  ```go
  AtomicReadModifyWriteAndCommitPush(
      ctx context.Context,
      absPath string,
      modify func(current []byte) ([]byte, error),
      message string,
  ) error
  ```
- `task/controller/pkg/result/result_writer_test.go` — every existing test stubs `fakeGit.AtomicWriteAndCommitPushStub` and asserts via `fakeGit.AtomicWriteAndCommitPushCallCount()` / `AtomicWriteAndCommitPushArgsForCall(...)`. All of these must be flipped to the `AtomicReadModifyWriteAndCommitPush` equivalents.
- `task/controller/mocks/git_client.go` — counterfeiter-generated fake. Confirm `AtomicReadModifyWriteAndCommitPushStub`, `AtomicReadModifyWriteAndCommitPushCallCount`, and `AtomicReadModifyWriteAndCommitPushArgsForCall(i) (context.Context, string, func(current []byte) ([]byte, error), string)` are present (they are — lines 12, 129, 151, 163).
</context>

<requirements>
### 1. Rewrite `WriteResult` to use `AtomicReadModifyWriteAndCommitPush`

In `task/controller/pkg/result/result_writer.go`, replace the body of `func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error` (currently lines 102-148). Keep the method signature and the `ResultWriter` interface identical.

New behavior:

1. Keep the leading glog.V(2)/V(3) lines.
2. Keep the call to `FindTaskFilePath` to resolve `matchedRelPath` — that call only reads metadata to locate the file and does not need to be inside the retry callback. The `existingFrontmatter` return value from `FindTaskFilePath` MUST NOT be used to drive the merge anymore; it is the stale snapshot that causes the bug. (You may still receive it via `_` or discard it.)
3. Keep the not-found branch unchanged: log a warning, increment `metrics.ResultsWrittenTotal.WithLabelValues("not_found").Inc()`, return `nil`.
4. Compute `absPath := filepath.Join(r.gitClient.Path(), matchedRelPath)`.
5. Build the commit message the same as today: `fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier)`.
6. Call `r.gitClient.AtomicReadModifyWriteAndCommitPush(ctx, absPath, r.buildResultModifyFn(ctx, req), commitMessage)`.
7. On error: increment `metrics.ResultsWrittenTotal.WithLabelValues("error").Inc()` and return `errors.Wrapf(ctx, err, "atomic read-modify-write and push failed")`.
8. On success: keep the existing trailing `glog.V(2).Infof(...)` log and `metrics.ResultsWrittenTotal.WithLabelValues("success").Inc()`, return `nil`.

### 2. Add the `buildResultModifyFn` method

Add a new unexported method on `*resultWriter` immediately after `WriteResult`:

```go
// buildResultModifyFn returns a modify callback for AtomicReadModifyWriteAndCommitPush
// that re-reads the on-disk frontmatter+body on every git retry, then applies the
// merge / retry-counter / cap rules. Re-reading per retry eliminates the read-then-write
// race against the partial-update executor (task_update_frontmatter_executor.go) that
// previously caused stale-snapshot writes to roll back terminal status.
func (r *resultWriter) buildResultModifyFn(
    ctx context.Context,
    req lib.Task,
) func(current []byte) ([]byte, error) {
    return func(current []byte) ([]byte, error) {
        frontmatterStr, err := ExtractFrontmatter(ctx, current)
        if err != nil {
            return nil, errors.Wrapf(ctx, err, "extract frontmatter")
        }
        bodyStr, err := ExtractBody(ctx, current)
        if err != nil {
            return nil, errors.Wrapf(ctx, err, "extract body")
        }
        var currentOnDisk lib.TaskFrontmatter
        if err := yaml.Unmarshal([]byte(frontmatterStr), &currentOnDisk); err != nil {
            return nil, errors.Wrapf(ctx, err, "unmarshal current frontmatter")
        }

        merged := mergeFrontmatter(currentOnDisk, req.Frontmatter)
        body := r.applyRetryCounter(merged, currentOnDisk, string(req.Content))

        marshaledFrontmatter, err := yaml.Marshal(map[string]any(merged))
        if err != nil {
            return nil, errors.Wrapf(ctx, err, "marshal frontmatter")
        }
        // Discard the on-disk body — WriteResult fully replaces body with req.Content
        // (post-applyRetryCounter modifications), matching the prior AtomicWriteAndCommitPush
        // semantics. bodyStr is read above only to validate the file has well-formed
        // delimiters; an extraction error must surface so we do not silently overwrite a
        // corrupted file.
        _ = bodyStr
        return []byte("---\n" + string(marshaledFrontmatter) + "---\n" + body), nil
    }
}
```

Notes:
- `mergeFrontmatter(currentOnDisk, req.Frontmatter)` — incoming wins, matching today's semantics (see line 122 of the old WriteResult).
- `applyRetryCounter(merged, currentOnDisk, string(req.Content))` — second arg MUST be the fresh on-disk frontmatter, not the stale snapshot from `FindTaskFilePath`. This is the load-bearing change for the spawn_notification + cap-stickiness paths exercised by the existing tests.
- The final byte layout `"---\n" + marshaledFrontmatter + "---\n" + body` matches the old code at line 130-132 exactly — keep it byte-identical so the frontmatter-with-nested-values, content-with-YAML-delimiters, and realistic-end-to-end tests still pass.
- **Yaml parse/marshal style.** The structural exemplar at `task_update_frontmatter_executor.go` uses unexported helpers `parseTaskFrontmatter` (line 109) + `marshalFileContent` (line 123) defined in `task_increment_frontmatter_executor.go`. Those helpers live in `pkg/command/`, are unexported, and reuse from `pkg/result/` would require either exporting them or hoisting them to a shared location — both larger refactors than this race fix warrants. Inlining `yaml.Unmarshal` / `yaml.Marshal` here is acceptable because: (a) the old `WriteResult` at line 125 already used `yaml.Marshal` directly, so we are not introducing a new pattern, only re-parsing on the way in; (b) the byte-layout sandwich (`---\n` + frontmatter + `---\n` + body) is reproduced byte-identically. Add a `// NOTE: inlined yaml ops mirror the pre-race version of WriteResult; pkg/command/ helpers are unexported and the lift-and-share is out of scope for this race fix.` comment above the `yaml.Unmarshal` call so future readers don't churn on the divergence.

### 3. Remove the now-unused FindTaskFilePath frontmatter return wiring inside WriteResult

In the new `WriteResult`, capture the existing-frontmatter return as `_`:

```go
matchedRelPath, _, err := FindTaskFilePath(ctx, r.gitClient, r.taskDir, req.TaskIdentifier)
```

Do NOT change `FindTaskFilePath`'s signature — other callers still use the frontmatter return (e.g., `task_update_frontmatter_executor.go` line 53 uses `_` already; `task_increment_frontmatter_executor.go` and the scanner may use the full return tuple). Leave it alone.

### 4. Update existing tests in `result_writer_test.go`

In `task/controller/pkg/result/result_writer_test.go`:

4a. **Replace the `BeforeEach` stub** (currently lines 82-84):

```go
fakeGit.AtomicWriteAndCommitPushStub = func(ctx context.Context, absPath string, content []byte, message string) error {
    return os.WriteFile(absPath, content, 0600)
}
```

with the read-modify-write equivalent, copied from `task_update_frontmatter_executor_test.go` lines 61-76:

```go
fakeGit.AtomicReadModifyWriteAndCommitPushStub = func(
    ctx context.Context,
    absPath string,
    modify func([]byte) ([]byte, error),
    message string,
) error {
    current, err := os.ReadFile(absPath) // #nosec G304 -- test helper
    if err != nil {
        return err
    }
    updated, err := modify(current)
    if err != nil {
        return err
    }
    return os.WriteFile(absPath, updated, 0600) // #nosec G306 -- test helper
}
```

4b. **Flip every callsite in the file** that references `AtomicWriteAndCommitPush` (the mock methods, not the production interface — production code no longer calls it from this file but the interface still exposes it). The 13 callsites are at lines **82, 133, 134, 227, 246, 361, 475, 817, 1421, 1426, 1441, 1467, 1468**. For lines 1–12 (everything except 1468) flip mechanically:

- `fakeGit.AtomicWriteAndCommitPushCallCount()` → `fakeGit.AtomicReadModifyWriteAndCommitPushCallCount()`
- `fakeGit.AtomicWriteAndCommitPushArgsForCall(i)` → `fakeGit.AtomicReadModifyWriteAndCommitPushArgsForCall(i)` (return tuple is `(context.Context, string, func(current []byte) ([]byte, error), string)` — the 4th element `msg` is what existing tests assert against).

Line **1468** (inside a `PIt` pending spec) needs different handling: today it destructures `_, _, content, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)` and then does `string(content)` to assert against the bytes the writer would have committed. After the flip, the third tuple element is the `modify` callback, not the bytes — `string(content)` will NOT compile. The `PIt` is pending so the spec doesn't run, but Go still compiles all source so `make precommit` will fail at the build step. Fix it like this: after `writer.WriteResult(...)` returns, read the file from disk and assert against that:

```go
// Was:
//   _, _, content, _ := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
//   s := string(content)
// Now: read the actual file the stub wrote.
written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "<the-test's-task-file>"))
Expect(readErr).NotTo(HaveOccurred())
s := string(written)
```

(The stub already writes to disk; this mirrors how the new race regression test in step 5 asserts.) Use whichever filename that `PIt` block had set up with `writeTaskFile(...)` — do not invent a new one. The rest of the spec body (the `Expect(s).To(ContainSubstring(...))` assertions) stays unchanged.

4c. **Do not change any other assertion in the file.** The body-text and frontmatter assertions verify the merge / cap / escalation behavior, which is unchanged because the modifyFn delegates to the same `mergeFrontmatter` / `applyRetryCounter` helpers.

### 5. Add a new regression test for the interleaved-write race

Add a new `Context("interleaved partial update between read and write (race-fix regression)", ...)` block inside the existing `Describe("WriteResult", ...)` in `result_writer_test.go`. The test must demonstrate that the modifyFn re-reads fresh disk state, so a partial update that lands between read and modify is preserved instead of being clobbered.

Use this exact structure (uses the same helpers already in the file: `writeTaskFile`, `identifier`, `tmpDir`, `taskDir`, `fakeGit`, `writer`):

```go
Context("interleaved partial update between read and write (race-fix regression)", func() {
    It(
        "preserves a partial frontmatter update that landed between read and modify",
        func() {
            // Initial on-disk state: task in_progress, healthcheck-style probe.
            writeTaskFile(
                "probe-task.md",
                "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: planning\nassignee: claude\ntrigger_count: 1\n---\nProbe body\n",
            )

            // Override the BeforeEach stub for this test: simulate a partial
            // update from task_update_frontmatter_executor landing on disk
            // BETWEEN the moment AtomicReadModifyWriteAndCommitPush would
            // fetch current bytes and the moment modify is invoked. We do
            // this by mutating the file inside the stub, before calling
            // modify with the freshly-re-read bytes. This is exactly what
            // git-rest does on a CAS retry: it re-reads from disk on each
            // attempt, so modify sees the interleaved write.
            fakeGit.AtomicReadModifyWriteAndCommitPushStub = func(
                ctx context.Context,
                absPath string,
                modify func([]byte) ([]byte, error),
                message string,
            ) error {
                // Simulate the interleaved partial update: another writer
                // (the executor) added spawn_notification: true and bumped
                // trigger_count to 2 between our initial read and the modify
                // call.
                interleaved := "---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: planning\nassignee: claude\ntrigger_count: 2\nspawn_notification: true\n---\nProbe body\n"
                if err := os.WriteFile(absPath, []byte(interleaved), 0600); err != nil { // #nosec G306 -- test helper
                    return err
                }
                current, err := os.ReadFile(absPath) // #nosec G304 -- test helper
                if err != nil {
                    return err
                }
                updated, err := modify(current)
                if err != nil {
                    return err
                }
                return os.WriteFile(absPath, updated, 0600) // #nosec G306 -- test helper
            }

            // Agent publishes terminal completion: status: completed, phase: done.
            taskFile = lib.Task{
                TaskIdentifier: identifier,
                Frontmatter: lib.TaskFrontmatter{
                    "task_identifier": "test-task-uuid-1234",
                    "status":          "completed",
                    "phase":           "done",
                },
                Content: lib.TaskContent("Probe completed successfully.\n"),
            }

            Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())

            written, readErr := os.ReadFile(
                filepath.Join(tmpDir, taskDir, "probe-task.md"),
            )
            Expect(readErr).NotTo(HaveOccurred())
            s := string(written)

            // 1. Agent's terminal status wins over the stale-disk in_progress.
            Expect(s).To(ContainSubstring("status: completed"))
            Expect(s).To(ContainSubstring("phase: done"))
            Expect(s).NotTo(ContainSubstring("status: in_progress"))
            Expect(s).NotTo(ContainSubstring("phase: planning"))

            // 2. The interleaved partial update's trigger_count: 2 is preserved
            //    (because modifyFn re-read fresh disk content; the stale
            //    snapshot from FindTaskFilePath would have written trigger_count: 1
            //    and dropped the bump). This is the load-bearing assertion that
            //    proves the race is fixed.
            Expect(s).To(ContainSubstring("trigger_count: 2"))
            Expect(s).NotTo(ContainSubstring("trigger_count: 1"))

            // 3. New body fully replaces old body (WriteResult semantics).
            Expect(s).To(ContainSubstring("Probe completed successfully."))
            Expect(s).NotTo(ContainSubstring("Probe body"))

            // 4. Exactly one AtomicReadModifyWriteAndCommitPush call.
            Expect(fakeGit.AtomicReadModifyWriteAndCommitPushCallCount()).To(Equal(1))

            // Note: do NOT assert against spawn_notification here.
            // applyRetryCounter early-returns when merged.Status() == "completed"
            // (result_writer.go:151-153) BEFORE the spawn_notification delete
            // path at line 183, so the on-disk spawn_notification: true is
            // preserved verbatim through the merge. That's a property of the
            // existing helper, not part of the race fix this test proves.
        },
    )
})
```

Why this proves the race is fixed:
- Before the fix: `WriteResult` builds `merged` from the snapshot returned by `FindTaskFilePath` (which read the file *before* the interleaved write). After the merge, `trigger_count: 2` from the interleaved partial update is missing from `merged` because the snapshot predates it. The final write clobbers the partial update and trigger_count drops back to 1.
- After the fix: `modifyFn` re-parses bytes that include the interleaved partial update. `mergeFrontmatter(currentOnDisk, req.Frontmatter)` preserves `trigger_count: 2` because `req.Frontmatter` does not contain the `trigger_count` key (the agent never publishes it), so the on-disk value survives.

### 6. Verify no other call sites in `result_writer.go` use `AtomicWriteAndCommitPush`

After the rewrite, `rg -n "AtomicWriteAndCommitPush" task/controller/pkg/result/result_writer.go` must return zero matches. The mock and the interface still expose the old method (other production code still uses it), but `result_writer.go` itself must not.

### 7. Imports

Do NOT hand-edit imports speculatively. `goimports` runs as part of `make precommit` and will add/remove imports automatically. The new `buildResultModifyFn` only uses packages already imported by `result_writer.go` today:

- `context`, `gopkg.in/yaml.v3`, `github.com/bborbe/errors`, `github.com/bborbe/agent/lib` — already present.

The following imports stay because OTHER code in the same file (not just `WriteResult`) uses them — do not remove them when refactoring `WriteResult`:
- `fmt` → `fmt.Sprintf` for the commit message inside the new `WriteResult` AND for escalation-section building elsewhere.
- `path/filepath` → `filepath.Join` for `absPath` inside the new `WriteResult`.
- `strings` → used by `ExtractFrontmatter` / `ExtractBody` / escalation helpers.
- `time`, `libtime` (`github.com/bborbe/time`) → used by escalation timestamps.
- `glog` → used by the trailing log lines in `WriteResult` and by other helpers.
- `gitclient`, `metrics` packages → still used everywhere.

If `goimports` removes any of these, it means a helper they support was also deleted by mistake — investigate before accepting the diff.
</requirements>

<constraints>
- Do NOT change the `ResultWriter` interface signature or `NewResultWriter` constructor signature.
- Do NOT change `mergeFrontmatter`, `applyRetryCounter`, `applyTriggerCap`, `applyRetryCap`, `clearAssignee`, `ClearAssigneeIfHumanReview`, `restoreExistingPhase`, `containsEscalationSection`, `escalationSection`, `triggerEscalationSection`, `ExtractFrontmatter`, `ExtractBody`, or `FindTaskFilePath`. The only signature touched is the body of `WriteResult` plus the new `buildResultModifyFn` helper.
- Do NOT change `task_update_frontmatter_executor.go` or any code under `task/executor/`.
- Do NOT change `task/controller/mocks/git_client.go` — counterfeiter-generated, regenerated by `make generate` if anyone changes the interface (which we are not).
- Do NOT add a CHANGELOG entry — the caller's autorelease handles versioning.
- Do NOT commit — dark-factory handles git.
- Existing tests in `result_writer_test.go` must still pass (mock-method names flipped, behavior assertions unchanged).
- Error wrapping uses `errors.Wrapf(ctx, err, ...)` from `github.com/bborbe/errors`, matching the rest of the file.
- Test framework: Ginkgo v2 + counterfeiter, matching the existing `result_writer_test.go` and `task_update_frontmatter_executor_test.go` style.
</constraints>

<verification>
1. From repo root:
   ```bash
   cd task/controller && make precommit
   ```
   Must pass.

2. Confirm the production callsite flipped:
   ```bash
   rg -n "AtomicWriteAndCommitPush" task/controller/pkg/result/result_writer.go
   ```
   Must return zero matches.

   ```bash
   rg -n "AtomicReadModifyWriteAndCommitPush" task/controller/pkg/result/result_writer.go
   ```
   Must return exactly one match (the call in `WriteResult`).

3. Confirm the new regression test exists and runs:
   ```bash
   cd task/controller && go test ./pkg/result/... -run TestResult -v 2>&1 | grep -i "interleaved partial update"
   ```
   Must list the new `It` block as a passing spec.

4. Confirm `task_update_frontmatter_executor.go` is unchanged:
   ```bash
   git diff --stat task/controller/pkg/command/task_update_frontmatter_executor.go
   ```
   Must show zero changes.
</verification>
