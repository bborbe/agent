---
status: completed
spec: [021-clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation]
summary: Extended vault_scanner.go to detect empty→named assignee transitions and atomically reset trigger_count/retry_count to 0; added 5 targeted tests covering all transition cases; updated docs and CHANGELOG.
container: agent-104-spec-021-scanner-counter-reset
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-10T16:45:00Z"
queued: "2026-05-10T20:09:43Z"
started: "2026-05-10T20:22:20Z"
completed: "2026-05-10T20:29:23Z"
branch: dark-factory/clear-assignee-on-escalation-and-reset-trigger-count-on-redelegation
---

<summary>
- When the vault scanner observes a task whose `assignee` transitions from empty (or absent) to a non-empty agent name, it atomically resets `trigger_count: 0` and `retry_count: 0` in the task file and queues the file for git commit
- The reset fires exactly once per empty-to-named transition: a change-name→different-name, a named→empty, or a first-scan observation of a non-empty assignee do not trigger a reset
- The scanner's in-memory state records each file's previous assignee so duplicate scan passes on the same transition are idempotent
- Two docs (`docs/task-flow-and-failure-semantics.md` and `docs/controller-design.md`) are updated to reflect the new shape: `assignee: ""` is the single inbox signal; `phase: human_review` is reserved for genuine human-work tasks
- CHANGELOG records the operator-visible behavior change
</summary>

<objective>
Extend `vault_scanner.go` so that when a task file's `assignee` field transitions from empty to a non-empty agent name (the operator re-delegates a parked task), the scanner writes `trigger_count: 0` and `retry_count: 0` back to the file, giving the re-delegated agent a fresh spawn budget without requiring manual counter edits. Update the two referenced docs to document the new assignee-as-inbox-signal doctrine. This is prompt 2 of 2 for spec-021; prompt 1 added the assignee-clear logic in `result_writer.go`.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`; never `fmt.Errorf`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, Counterfeiter, external test packages, ≥80% coverage
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Prerequisite: prompt 1 of spec-021 has shipped.** Verify before editing:
```bash
grep -n 'merged\["assignee"\] = ""' task/controller/pkg/result/result_writer.go
```
If that grep returns zero matches, STOP and report `status: failed` with message "spec-021 result_writer changes not yet deployed (prompt 1 of spec-021)".

**Key files to read in full before editing:**

- `task/controller/pkg/scanner/vault_scanner.go` — full read; focus on `fileEntry` struct, `processFile`, `injectAndStore`, `scanFiles`, `runCycle`, and how `newLocalFileOps` / `NewGitRestVaultScanner` work
- `task/controller/pkg/scanner/vault_scanner_test.go` — full read; understand the `testGitClient` double, `mustInitGitRepo` helper, and the internal (`package scanner`) test conventions
- `lib/agent_task-frontmatter.go` — confirm `Assignee()`, `TriggerCount()`, `RetryCount()` return types and YAML key names (`assignee`, `trigger_count`, `retry_count`)
- `docs/task-flow-and-failure-semantics.md` — read fully to understand what needs updating
- `docs/controller-design.md` — read fully to understand what needs updating

**Inline reference — current `fileEntry` struct:**
```go
type fileEntry struct {
    hash           [32]byte
    taskIdentifier lib.TaskIdentifier
}
```

**Inline reference — current `processFile` decision flow (simplified):**
```go
// after UUID validation and dedup...
v.hashes[relPath] = fileEntry{hash: hash, taskIdentifier: lib.TaskIdentifier(taskID)}
if frontmatter.Status() == "" { return nil, "", false }
if frontmatter.Assignee() == "" { return nil, "", false }
body := extractBody(content)
return &lib.Task{...}, "", false
```

The scanner tests use the **internal** package (`package scanner`), access unexported fields directly (`s.hashes`, `s.ops`), and use a hand-written `testGitClient` double (NOT the counterfeiter mock — using it would cause an import cycle with `mocks/` which imports `scanner`).
</context>

<requirements>

1. **Add `assignee` field to `fileEntry`**

   Change:
   ```go
   type fileEntry struct {
       hash           [32]byte
       taskIdentifier lib.TaskIdentifier
   }
   ```
   To:
   ```go
   type fileEntry struct {
       hash           [32]byte
       taskIdentifier lib.TaskIdentifier
       assignee       lib.TaskAssignee
   }
   ```

   Update every place that constructs a `fileEntry` literal:
   - `injectAndStore`: `fileEntry{hash: [32]byte{}, taskIdentifier: lib.TaskIdentifier(id)}` — add `assignee: ""` (empty, since task just had UUID injected — operator hasn't set one yet)
   - `processFile` normal path (see step 4 below)

2. **Capture previous entry and detect empty-to-named transition in `processFile`**

   In `processFile`, immediately after the UUID-validity and uniqueness checks (just before the existing `v.hashes[relPath] = fileEntry{...}` assignment), insert the transition detection block.

   The full flow becomes:

   ```go
   // --- existing code above this point: hash, frontmatter parse, dedup, UUID validation ---

   currentAssignee := frontmatter.Assignee()
   prevEntry := v.hashes[relPath] // read BEFORE updating

   // Detect empty → named assignee transition (operator re-delegated a parked task).
   // Conditions:
   //   - current assignee is non-empty (new state is assigned)
   //   - previous entry exists (prevEntry.taskIdentifier != "") so this isn't the first scan
   //   - previous assignee was empty (the transition is from empty, not from a different name)
   if currentAssignee != "" && prevEntry.taskIdentifier != "" && prevEntry.assignee == "" {
       wrote, werr := v.writeCounterReset(ctx, relPath, content, fmMap)
       if werr {
           return nil, "", true
       }
       if wrote != "" {
           // Store zero-hash sentinel so next scan re-processes and publishes the task.
           // Store new assignee so the transition is not re-triggered on the next pass.
           v.hashes[relPath] = fileEntry{
               hash:           [32]byte{},
               taskIdentifier: lib.TaskIdentifier(taskID),
               assignee:       currentAssignee,
           }
           return nil, wrote, false
       }
   }

   // Normal path: update stored entry with current state
   v.hashes[relPath] = fileEntry{
       hash:           hash,
       taskIdentifier: lib.TaskIdentifier(taskID),
       assignee:       currentAssignee,
   }

   if frontmatter.Status() == "" {
       glog.Warningf("skipping %s: invalid frontmatter: status is empty", relPath)
       return nil, "", false
   }
   if frontmatter.Assignee() == "" {
       return nil, "", false
   }
   body := extractBody(content)
   return &lib.Task{
       TaskIdentifier: lib.TaskIdentifier(taskID),
       Frontmatter:    frontmatter,
       Content:        lib.TaskContent(body),
   }, "", false
   ```

   Read the full `processFile` function before editing — the exact placement matters. The transition check must come AFTER UUID validation and uniqueness check (so we have a valid `taskID`), and BEFORE the existing `v.hashes[relPath] = ...` assignment (so we can read `prevEntry`).

3. **Implement `writeCounterReset` helper**

   Add a new unexported method to `*vaultScanner`:

   ```go
   // writeCounterReset rewrites the task file with trigger_count: 0 and retry_count: 0.
   // fmMap is the already-parsed frontmatter map for this file.
   // Returns (relPath, false) on success, ("", true) on write error.
   func (v *vaultScanner) writeCounterReset(
       ctx context.Context,
       relPath string,
       content []byte,
       fmMap map[string]interface{},
   ) (string, bool) {
       resetFm := make(map[string]interface{}, len(fmMap))
       for k, val := range fmMap {
           resetFm[k] = val
       }
       resetFm["trigger_count"] = 0
       resetFm["retry_count"] = 0

       newFmYAML, err := yaml.Marshal(resetFm)
       if err != nil {
           glog.Warningf("writeCounterReset: marshal failed for %s: %v", relPath, err)
           return "", false // non-fatal: skip reset this cycle
       }

       body := extractBody(content)
       newContent := []byte("---\n" + string(newFmYAML) + "---\n" + body)

       if writeErr := v.ops.writeFile(ctx, relPath, newContent); writeErr != nil {
           glog.Warningf("writeCounterReset: write failed for %s: %v", relPath, writeErr)
           return "", true
       }
       glog.V(2).Infof("writeCounterReset: reset trigger_count/retry_count for %s", relPath)
       return relPath, false
   }
   ```

   Key points:
   - Copy `fmMap` before mutation so the caller's map is not modified
   - YAML integers `0` serialize as `0` (not `null`), which is correct
   - Uses the same `extractBody` already defined in `vault_scanner.go`
   - Uses `v.ops.writeFile` (same abstraction as UUID injection) so both local and git-rest modes work
   - A marshal error is non-fatal (skip this cycle, try again next pass); a write error is a hard `writeError = true`

4. **Update `injectAndStore` to populate `assignee` in `fileEntry`**

   In `injectAndStore`, change:
   ```go
   v.hashes[relPath] = fileEntry{hash: [32]byte{}, taskIdentifier: lib.TaskIdentifier(id)}
   ```
   To:
   ```go
   v.hashes[relPath] = fileEntry{hash: [32]byte{}, taskIdentifier: lib.TaskIdentifier(id), assignee: ""}
   ```

   The newly-injected task has no operator-set assignee yet.

5. **Update `runCycle` commit message to cover both UUID injection and counter resets**

   In `runCycle`, the commit message currently says "add task_identifier to tasks". Since counter-reset writes also end up in `written`, change the message to cover both:

   ```go
   if err := v.gitClient.CommitAndPush(ctx, "[agent-task-controller] update task metadata"); err != nil {
   ```

6. **Run `make test` to verify the compilation and existing tests**

   ```bash
   cd task/controller && make test
   ```

   Fix any compile errors (typically `fileEntry` literal constructors missing the new `assignee` field).

7. **Add new tests in `vault_scanner_test.go` — inside `Describe("processFile edge cases")`**

   All new tests follow the internal-package convention: use `*vaultScanner` directly (`s.runCycle(ctx, results)` or `s.processFile(ctx, relPath)`), write files with `os.WriteFile`, read them with `os.ReadFile` or `yaml.Unmarshal`.

   Use valid UUID strings for `task_identifier` (8-4-4-4-12 hex format) so the uniqueness/UUID checks pass.

   **Test A — empty → named triggers exactly one reset (AC 6)**

   ```go
   It("resets trigger_count and retry_count when assignee transitions from empty to named", func() {
       taskID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
       absPath := filepath.Join(tmpDir, taskDir, "parked.md")
       initialContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: \ntrigger_count: 3\nretry_count: 2\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(initialContent), 0600)).To(Succeed())

       // First scan: file is parked (assignee empty), no task published, no reset
       s.runCycle(ctx, results)
       var r1 ScanResult
       Expect(results).To(Receive(&r1))
       Expect(r1.Changed).To(BeEmpty())

       // Operator re-delegates: set assignee to claude
       delegatedContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claude\ntrigger_count: 3\nretry_count: 2\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(delegatedContent), 0600)).To(Succeed())

       // Second scan: detects transition, writes reset, no task published yet (zero-hash sentinel)
       s.runCycle(ctx, results)
       var r2 ScanResult
       Expect(results).To(Receive(&r2))
       Expect(r2.Changed).To(BeEmpty()) // task not published yet — reset write queued

       // Verify the file was rewritten with reset counters
       written, _ := os.ReadFile(absPath)
       var fm map[string]interface{}
       fmStr := extractFrontmatterStr(string(written))
       Expect(yaml.Unmarshal([]byte(fmStr), &fm)).To(Succeed())
       Expect(fm["trigger_count"]).To(BeNumerically("==", 0))
       Expect(fm["retry_count"]).To(BeNumerically("==", 0))
       Expect(fm["assignee"]).To(Equal("claude")) // assignee preserved

       // Third scan: reads reset file, publishes task with fresh counters
       s.runCycle(ctx, results)
       var r3 ScanResult
       Expect(results).To(Receive(&r3))
       Expect(r3.Changed).To(HaveLen(1))
       Expect(string(r3.Changed[0].Frontmatter.Assignee())).To(Equal("claude"))
       Expect(r3.Changed[0].Frontmatter.TriggerCount()).To(Equal(0))
       Expect(r3.Changed[0].Frontmatter.RetryCount()).To(Equal(0))
   })
   ```

   Add a local helper `extractFrontmatterStr` at the top of the test file (or inline):
   ```go
   func extractFrontmatterStr(fileContent string) string {
       const pfx = "---\n"
       if !strings.HasPrefix(fileContent, pfx) { return "" }
       rest := fileContent[len(pfx):]
       idx := strings.Index(rest, "\n---")
       if idx == -1 { return "" }
       return rest[:idx]
   }
   ```

   **Test B — named → named does NOT trigger reset (AC 7)**

   ```go
   It("does not reset counters when assignee changes from one name to another (named → named)", func() {
       taskID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
       absPath := filepath.Join(tmpDir, taskDir, "named-named.md")
       // Initial state: assignee already set to claudeA
       initialContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claudeA\ntrigger_count: 2\nretry_count: 1\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(initialContent), 0600)).To(Succeed())

       // First scan: task published with claudeA
       s.runCycle(ctx, results)
       Expect(results).To(Receive())

       // Operator changes to claudeB
       changedContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claudeB\ntrigger_count: 2\nretry_count: 1\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(changedContent), 0600)).To(Succeed())

       // Second scan: named → named, no reset, task published with new assignee
       s.runCycle(ctx, results)
       var r2 ScanResult
       Expect(results).To(Receive(&r2))
       Expect(r2.Changed).To(HaveLen(1))

       // Counters must NOT have been reset
       written, _ := os.ReadFile(absPath)
       var fm map[string]interface{}
       Expect(yaml.Unmarshal([]byte(extractFrontmatterStr(string(written))), &fm)).To(Succeed())
       Expect(fm["trigger_count"]).To(BeNumerically("==", 2))
       Expect(fm["retry_count"]).To(BeNumerically("==", 1))
   })
   ```

   **Test C — named → empty does NOT trigger reset (AC 8)**

   ```go
   It("does not reset counters when assignee is cleared from named to empty (named → empty)", func() {
       taskID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
       absPath := filepath.Join(tmpDir, taskDir, "named-empty.md")
       initialContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claude\ntrigger_count: 2\nretry_count: 1\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(initialContent), 0600)).To(Succeed())

       // First scan: task published with claude
       s.runCycle(ctx, results)
       Expect(results).To(Receive())

       // Operator clears assignee
       clearedContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: \ntrigger_count: 2\nretry_count: 1\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(clearedContent), 0600)).To(Succeed())

       // Second scan: named → empty, no reset, task skipped (empty assignee)
       s.runCycle(ctx, results)
       var r2 ScanResult
       Expect(results).To(Receive(&r2))
       Expect(r2.Changed).To(BeEmpty()) // skipped because assignee empty

       // File counters must NOT have changed
       written, _ := os.ReadFile(absPath)
       var fm map[string]interface{}
       Expect(yaml.Unmarshal([]byte(extractFrontmatterStr(string(written))), &fm)).To(Succeed())
       Expect(fm["trigger_count"]).To(BeNumerically("==", 2))
       Expect(fm["retry_count"]).To(BeNumerically("==", 1))
   })
   ```

   **Test D — double observation of same transition fires reset only once (AC 9)**

   ```go
   It("emits counter reset exactly once even if the same empty→named transition is observed twice in consecutive scans", func() {
       taskID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
       absPath := filepath.Join(tmpDir, taskDir, "idempotent.md")
       initialContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: \ntrigger_count: 3\nretry_count: 2\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(initialContent), 0600)).To(Succeed())

       // First scan: parked, no task
       s.runCycle(ctx, results)
       Expect(results).To(Receive())

       // Operator re-delegates
       delegatedContent := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claude\ntrigger_count: 3\nretry_count: 2\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(delegatedContent), 0600)).To(Succeed())

       // Second scan: detects transition, writes reset once
       s.runCycle(ctx, results)
       Expect(results).To(Receive())

       // Capture file state after first reset
       afterFirstReset, _ := os.ReadFile(absPath)
       var fm1 map[string]interface{}
       Expect(yaml.Unmarshal([]byte(extractFrontmatterStr(string(afterFirstReset))), &fm1)).To(Succeed())
       Expect(fm1["trigger_count"]).To(BeNumerically("==", 0))
       Expect(fm1["retry_count"]).To(BeNumerically("==", 0))

       // Third scan: zero-hash sentinel forces re-process, but prevEntry.assignee = "claude"
       // → no second transition detected → no second reset
       s.runCycle(ctx, results)
       Expect(results).To(Receive())

       // Strongest assertion: file content must be byte-identical to post-first-reset
       // (catches a regression where reset fires twice — even though the values
       // would still be 0, the second write would still touch the file)
       afterSecondScan, _ := os.ReadFile(absPath)
       Expect(afterSecondScan).To(Equal(afterFirstReset))

       // Also assert via parsed frontmatter for clarity in failure output
       var fm2 map[string]interface{}
       Expect(yaml.Unmarshal([]byte(extractFrontmatterStr(string(afterSecondScan))), &fm2)).To(Succeed())
       Expect(fm2["trigger_count"]).To(BeNumerically("==", 0))
       Expect(fm2["retry_count"]).To(BeNumerically("==", 0))
   })
   ```

   **Test E — first scan of file with non-empty assignee does NOT trigger reset**

   ```go
   It("does not reset counters on first scan of a file that already has a non-empty assignee", func() {
       taskID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
       absPath := filepath.Join(tmpDir, taskDir, "already-assigned.md")
       content := "---\ntask_identifier: " + taskID + "\nstatus: in_progress\nphase: ai_review\nassignee: claude\ntrigger_count: 5\nretry_count: 4\n---\n# body\n"
       Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())

       // First scan ever — prevEntry.taskIdentifier == "" (zero value)
       s.runCycle(ctx, results)
       var r1 ScanResult
       Expect(results).To(Receive(&r1))
       Expect(r1.Changed).To(HaveLen(1)) // task published normally

       // Counters must NOT have been reset
       written, _ := os.ReadFile(absPath)
       var fm map[string]interface{}
       Expect(yaml.Unmarshal([]byte(extractFrontmatterStr(string(written))), &fm)).To(Succeed())
       Expect(fm["trigger_count"]).To(BeNumerically("==", 5))
       Expect(fm["retry_count"]).To(BeNumerically("==", 4))
   })
   ```

   Add `"strings"` import to `vault_scanner_test.go` if not already present (needed for `extractFrontmatterStr`).

8. **Run iterative tests**

   ```bash
   cd task/controller && make test
   ```

   Fix any failures. Common issues:
   - `fileEntry` literal in tests missing `assignee` field → add `assignee: ""` or `assignee: "claude"` as appropriate
   - `extractFrontmatterStr` function placement — define it at package level in the test file
   - `yaml` import needed in the test file — already imported if pre-existing tests use it (check, add if missing)

9. **Check coverage for `pkg/scanner/` package**

   ```bash
   cd task/controller && go test -coverprofile=/tmp/scanner-cover.out -mod=vendor ./pkg/scanner/...
   go tool cover -func=/tmp/scanner-cover.out | grep "total:"
   ```

   Coverage must be ≥80%.

10. **Update `docs/task-flow-and-failure-semantics.md`**

    Read the file in full. Locate the **Terminology** table and the **Escalation** section. Make the following targeted changes:

    a. In the Terminology table, update the `Escalation` row to reflect the spec-021 doctrine. The current row says the controller flips `phase` to `human_review` on cap or terminal outcome (per spec 015). The new doctrine: the controller clears `assignee` to empty on EVERY escalation path (trigger cap, retry cap, `needs_input`); for `needs_input` the controller also sets `phase: human_review`; for cap escalations the controller leaves `phase` at the lifecycle stage where the cap fired (`planning`, `in_progress`, or `ai_review`).

       Rewrite the row's "Behavior" / "Effect" column with that exact substance. If the existing prose differs from the hypothetical wording above, ignore the hypothetical and adapt to whatever the row actually says — the key invariant is: empty assignee on every path, phase: human_review only for needs_input, phase preserved for caps. Reference spec 021.

    b. Locate or add an **Inbox Signal** section (or add a note to the Status Taxonomy section):

       Add the following note near the top of the Escalation / Status section:

       ```markdown
       ## Inbox Signal (spec 021)

       `assignee == ""` is the single canonical signal for "task needs attention". Operator boards and tooling that surface unclaimed work should filter on `assignee`, not on `phase`.

       - `phase: human_review` means a human must do the actual work (agent emitted `needs_input`).
       - `phase: ai_review` / `in_progress` / `planning` with `assignee: ""` means an agent hit a cap; fix the underlying issue and re-delegate by setting `assignee` to an agent name.
       ```

    c. Find any documentation that says trigger-cap or retry-cap escalation sets `phase: human_review` and update it to say the phase is left at the pre-cap lifecycle stage. Confirm by searching:
       ```bash
       grep -n "human_review" docs/task-flow-and-failure-semantics.md
       ```
       For each match, verify the context and correct it if it incorrectly implies cap escalations set human_review.

11. **Update `docs/controller-design.md`**

    Read the file in full. Locate the **Core Logic** section, specifically the "Command Processing" flow.

    a. Find text describing the escalation step (currently something like):
       > if retry_count >= max_retries → set phase: human_review, append ## Retry Escalation

       Update to describe the new behavior:
       ```
       if trigger_count >= max_triggers → clear assignee: "", preserve lifecycle phase, append ## Trigger Cap Escalation (once)
       if retry_count >= max_retries   → clear assignee: "", preserve lifecycle phase, append ## Retry Escalation (once)
       if agent emits needs_input (phase: human_review) → set phase: human_review, clear assignee: ""
       ```

    b. Add a new subsection after **Frontmatter Merge**:

       ```markdown
       ## Assignee-Clear on Escalation (spec 021)

       Every escalation path writes `assignee: ""` so the task surfaces in operator inbox:

       | Escalation trigger | `phase` written | `assignee` written |
       |---|---|---|
       | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
       | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
       | Agent emits `needs_input` | `human_review` | `""` |

       Once a task is parked (escalation section present, `assignee: ""`), repeated stale agent
       result publishes are idempotent: the escalation section is not duplicated, the lifecycle
       phase is restored from the on-disk value, and assignee stays empty.

       ## Empty-to-Named Reset (spec 021)

       When the vault scanner observes a task file whose `assignee` transitions from empty (or absent) to a non-empty agent name, it writes `trigger_count: 0` and `retry_count: 0` back to the file atomically and queues a git commit. This refills the per-attempt budgets for the re-delegated agent without requiring manual counter edits. The reset fires exactly once per empty-to-named transition (named→named and named→empty transitions do not trigger a reset).
       ```

12. **Update `CHANGELOG.md` at repo root**

    If `## Unreleased` section exists (prompt 1 may have added entries), append to it. Otherwise create above the latest version header.

    Add (below any entries from prompt 1):
    ```markdown
    - feat: reset `trigger_count` and `retry_count` to 0 when vault scanner detects `assignee` transition from empty to named (operator re-delegation refills spawn budget automatically)
    - docs: update `task-flow-and-failure-semantics.md` and `controller-design.md` to document `assignee: ""` as single inbox signal and new escalation shape
    ```

13. **Run final precommit**

    ```bash
    cd task/controller && make precommit
    ```

    Must exit 0.

</requirements>

<constraints>
- The atomic-write contract from spec 006 is preserved: `writeCounterReset` uses `v.ops.writeFile` (same as UUID injection), not direct `os.WriteFile`; the resulting write is committed by the existing `CommitAndPush` in `runCycle`
- The counter reset fires on empty-to-named transition ONLY. The guards are: `currentAssignee != ""`, `prevEntry.taskIdentifier != ""` (not first scan), `prevEntry.assignee == ""` (previous was empty)
- After writing the reset, store the zero-hash sentinel `[32]byte{}` in `v.hashes[relPath]` so the next scan re-processes and publishes the task with the reset counters; store the NEW assignee (not "") so the transition is not re-triggered
- `writeCounterReset` copies `fmMap` before mutation — the caller's map must not be modified
- A marshal failure in `writeCounterReset` is non-fatal (skip with a warning, return `"", false`); a `writeFile` failure is fatal (return `"", true` for `writeError`)
- Tests use `package scanner` (internal) with hand-written `testGitClient` double — do NOT use `mocks.FakeGitClient` (import cycle)
- All test task_identifier values must be valid UUIDs (8-4-4-4-12 hex) so the UUID validation in `processFile` passes
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`
- Do NOT commit — dark-factory handles git
- `make precommit` in `task/controller` must exit 0
- Existing tests in `vault_scanner_test.go` must still pass unchanged
- Doc updates are prose changes only — do not alter the Go source
</constraints>

<verification>

Verify prerequisite (prompt 1) shipped:
```bash
grep -n 'merged\["assignee"\] = ""' task/controller/pkg/result/result_writer.go
```
Expected: at least two matches.

Verify `fileEntry` has `assignee` field:
```bash
grep -A4 "type fileEntry struct" task/controller/pkg/scanner/vault_scanner.go
```
Expected: `assignee lib.TaskAssignee` field present.

Verify `writeCounterReset` method exists:
```bash
grep -n "func.*writeCounterReset" task/controller/pkg/scanner/vault_scanner.go
```
Expected: one match.

Verify transition detection in `processFile`:
```bash
grep -n "prevEntry.assignee" task/controller/pkg/scanner/vault_scanner.go
```
Expected: at least one match inside `processFile`.

Verify new tests exist:
```bash
grep -n "empty.*named\|named.*named\|named.*empty\|idempotent\|already-assigned\|counter reset\|trigger reset" task/controller/pkg/scanner/vault_scanner_test.go
```
Expected: multiple matches.

Verify docs updated:
```bash
grep -n "assignee.*inbox\|inbox.*assignee\|Empty-to-Named\|Assignee-Clear" docs/controller-design.md docs/task-flow-and-failure-semantics.md
```
Expected: matches in both files.

Verify CHANGELOG updated:
```bash
grep -n "trigger_count.*retry_count.*0\|reset.*trigger\|spawn budget\|inbox signal" CHANGELOG.md
```
Expected: at least one match.

Run tests:
```bash
cd task/controller && make test
```
Expected: all pass.

Run coverage:
```bash
cd task/controller && go test -coverprofile=/tmp/scanner-cover.out -mod=vendor ./pkg/scanner/... && go tool cover -func=/tmp/scanner-cover.out | grep "total:"
```
Expected: ≥80%.

Run precommit:
```bash
cd task/controller && make precommit
```
Expected: exit 0.

</verification>
