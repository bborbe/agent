---
status: completed
summary: Added NextPhase field to AgentResultInfo and AgentResultLike interface, wired resolveNextPhase helper in kafkaResultDeliverer, extended tests for all Status×NextPhase combinations.
container: agent-078-add-next-phase-to-agent-result
dark-factory-version: v0.132.0
created: "2026-04-24T14:00:00Z"
queued: "2026-04-24T14:02:48Z"
started: "2026-04-24T14:02:50Z"
completed: "2026-04-24T14:08:32Z"
---

<summary>
- Agents can now request a task phase transition by setting a `NextPhase` field on their result envelope
- The controller writes the requested phase into the task frontmatter on `status: done` — transitioning to the next step of a multi-phase agent's workflow (e.g. planning → in_progress → done)
- On `status: failed` or `status: needs_input`, `NextPhase` is ignored — escalation to `human_review` continues to win (matches prompt 077's failure-routing rules)
- Empty `NextPhase` preserves existing behavior: `done` → `phase: done`, non-done → `phase: human_review` — phase-unaware agents continue to work unchanged
- Valid values are the existing `TaskPhase` enum (`planning`, `in_progress`, `ai_review`, `human_review`, `done`); controller validates and rejects anything else, falling back to the existing default
- `needs_input` still routes to `human_review` even if the agent set `NextPhase` — semantic-task-broken always requires human review
- Tests cover every combination of `Status` × `NextPhase` in both the content generator and the kafka deliverer
</summary>

<objective>
Add a `NextPhase` field to the agent result envelope so multi-phase agents can drive their own phase transitions. After this change, an agent returning `status: done` with `NextPhase: "in_progress"` causes the controller to write `phase: in_progress` to the task file, triggering the next phase's Job spawn. This unblocks the hypothesis agent v2 two-phase design (planning extracts fields → in_progress probes artifacts).
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`

**Key files to read in full before editing:**

- `lib/delivery/status.go` — `AgentStatus` constants AND the `AgentResultInfo` struct. Extend this struct with the new `NextPhase` field (step 1).
- `lib/delivery/result-deliverer.go` — `kafkaResultDeliverer.DeliverResult` sets `frontmatter["phase"]` explicitly per status in its `switch result.Status` block — this is the single production seam that maps `NextPhase` to task frontmatter (step 4). Anchor by method name, not line numbers.
- `lib/delivery/content-generator.go` — `applyStatusFrontmatter` writes phase into the embedded frontmatter. The kafka deliverer's switch OVERRIDES this, so the kafka path is authoritative for Kafka publishes.
- `lib/claude/types.go` — `AgentResultLike` interface. Agents return types implementing this.
- `lib/claude/result-deliverer.go` — `resultDelivererAdapter[T].DeliverResult` converts `T` to `AgentResultInfo` — anchor by method name.
- `lib/delivery/content-generator_test.go`, `lib/delivery/result-deliverer_test.go` — existing tests; extend, don't create parallel files.
- `github.com/bborbe/vault-cli/pkg/domain.TaskPhase` — enum (`TaskPhasePlanning`, `TaskPhaseInProgress`, `TaskPhaseAIReview`, `TaskPhaseHumanReview`, `TaskPhaseDone`). Use `TaskPhase(s).Validate` for enum validation.

**Design contract:**

| Agent `Status` | Agent `NextPhase` | Resulting `phase:` in task |
|----------------|-------------------|---------------------------|
| `done` | empty | `done` (existing default) |
| `done` | `"planning"` | `planning` |
| `done` | `"in_progress"` | `in_progress` |
| `done` | `"ai_review"` | `ai_review` |
| `done` | `"human_review"` | `human_review` |
| `done` | `"done"` | `done` (explicit — same as empty) |
| `done` | any other value | `done` (invalid rejected; fall back to default); log a warning |
| `needs_input` | any value | `human_review` (semantic-task-broken always escalates) |
| `failed` | any value | `human_review` (matches prompt 074 + 077 — infra failure escalates) |

**Why NextPhase is agent-requested, not controller-inferred:**
- The agent is the only entity that knows which phase's work just completed and which phase should run next
- Avoids the controller having to maintain per-agent state machines
- Phase-unaware agents leave `NextPhase` empty and inherit the existing default behavior (no regression)

Grep before editing (all paths repo-relative, container-safe):
```bash
grep -n "AgentResultInfo\b" lib/delivery/*.go | grep -v _test.go | head -20
grep -n "frontmatter\[\"phase\"\]\|applyStatusFrontmatter\|NextPhase" lib/delivery/*.go lib/claude/*.go | grep -v _test.go | head -30
grep -rn "AgentResultLike\|GetStatus\|GetMessage\|GetFiles\|RenderResultSection" lib/claude/ | grep -v _test.go | head -10
```
</context>

<requirements>

1. **Extend `AgentResultInfo` in `lib/delivery/status.go`**

   The struct currently has `Status`, `Output`, `Message` fields. Add a `NextPhase` field:

   ```go
   // AgentResultInfo holds the minimum fields a ContentGenerator needs from any agent result.
   type AgentResultInfo struct {
       Status  AgentStatus
       Output  string // human-readable summary or result body
       Message string // error or status message
       // NextPhase is the task phase the agent requests the controller to write
       // when Status == AgentStatusDone. Ignored on Failed/NeedsInput (failure
       // paths always escalate to human_review). Empty means "use default"
       // (phase: done on Status: done). Valid values are vault-cli TaskPhase
       // enum strings: planning, in_progress, ai_review, human_review, done.
       NextPhase string
   }
   ```

2. **Extend `AgentResultLike` in `lib/claude/types.go`**

   Add a method to the interface:

   ```go
   type AgentResultLike interface {
       GetStatus() AgentStatus
       GetMessage() string
       GetFiles() []string
       GetNextPhase() string        // new
       RenderResultSection() string
   }
   ```

   The existing concrete `claude.AgentResult` struct at `lib/claude/types.go` must gain a `NextPhase` field plus a `GetNextPhase()` method:

   ```go
   type AgentResult struct {
       Status    AgentStatus
       Message   string
       Files     []string
       NextPhase string   // new
   }

   func (r AgentResult) GetNextPhase() string { return r.NextPhase }
   ```

3. **Update `resultDelivererAdapter[T].DeliverResult` in `lib/claude/result-deliverer.go`**

   The adapter converts `T` → `AgentResultInfo` before calling the inner deliverer. Add `NextPhase: result.GetNextPhase()` to the conversion:

   ```go
   func (a *resultDelivererAdapter[T]) DeliverResult(ctx context.Context, result T) error {
       return a.inner.DeliverResult(ctx, delivery.AgentResultInfo{
           Status:    result.GetStatus(),
           Output:    result.RenderResultSection(),
           Message:   result.GetMessage(),
           NextPhase: result.GetNextPhase(),
       })
   }
   ```

4. **Update `kafkaResultDeliverer.DeliverResult` in `lib/delivery/result-deliverer.go`**

   Current switch (after prompt 077):
   ```go
   switch result.Status {
   case AgentStatusDone:
       frontmatter["status"] = "completed"
       frontmatter["phase"] = "done"
   case AgentStatusNeedsInput:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   default:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   }
   ```

   Replace with:
   ```go
   switch result.Status {
   case AgentStatusDone:
       frontmatter["status"] = "completed"
       frontmatter["phase"] = resolveNextPhase(ctx, d.taskID, result.NextPhase)
   case AgentStatusNeedsInput:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   default:
       frontmatter["status"] = "in_progress"
       frontmatter["phase"] = "human_review"
   }
   ```

   Add a helper at package level:

   ```go
   // resolveNextPhase returns the validated phase string for a done agent result.
   // An empty NextPhase defaults to "done" (existing behavior). An invalid value
   // is logged with task-id context and also falls back to "done" — we never refuse
   // to write a result just because the agent requested a bogus phase.
   func resolveNextPhase(ctx context.Context, taskID lib.TaskIdentifier, requested string) string {
       if requested == "" {
           return "done"
       }
       phase := domain.TaskPhase(requested)
       if err := phase.Validate(ctx); err != nil {
           glog.Warningf("task %s: ignoring invalid NextPhase %q: %v", taskID, requested, err)
           return "done"
       }
       return requested
   }
   ```

   Add imports if not already present:
   ```go
   domain "github.com/bborbe/vault-cli/pkg/domain"
   "github.com/golang/glog"
   ```

   `ctx` and `taskID` are threaded from `DeliverResult`'s existing signature — no new `context.Background()` is introduced. Validation failures include the task id so operators can trace which agent published the bogus value.

5. **Update `lib/delivery/content-generator.go` `applyStatusFrontmatter`**

   Current logic sets `phase: done` for `AgentStatusDone`. Keep as-is — the content generator doesn't see `NextPhase`. The kafka deliverer's explicit switch in step 4 overrides whatever the content generator wrote, so the end-state phase is correct.

   **Do NOT modify `applyStatusFrontmatter`.** Test coverage will verify the override semantics.

6. **Tests — `lib/delivery/result-deliverer_test.go`**

   Extend with these new `It(...)` cases inside the existing kafka-deliverer `Describe` block. Full `Status × NextPhase` coverage:

   **`done` status:**
   - `It("sets phase=done when done result has empty NextPhase", ...)` — default preserved
   - `It("sets phase=in_progress when done result requests NextPhase=in_progress", ...)` — hypothesis-v2 planning handoff
   - `It("sets phase=planning when done result requests NextPhase=planning", ...)` — reverse transition support
   - `It("sets phase=done when done result requests NextPhase=done explicitly", ...)` — same as empty
   - `It("sets phase=human_review when done result requests NextPhase=human_review", ...)` — agent self-escalates
   - `It("falls back to phase=done when NextPhase is invalid", ...)` — graceful degradation + Warningf logged

   **077 precedence — non-done statuses ignore NextPhase:**
   - `It("sets phase=human_review when failed result requests NextPhase=in_progress (NextPhase ignored)", ...)` — failure escalation wins
   - `It("emits ## Failure body section when failed result has NextPhase set (body shape from 077 unchanged)", ...)` — 077's body shape is not affected by NextPhase presence
   - `It("sets phase=human_review when needs_input result requests NextPhase=done (NextPhase ignored)", ...)` — semantic-task-broken escalation wins

   **Boundary:** capture the serialized `cdb.CommandObject` at the `SyncProducer` seam (same capturing-producer pattern used by the 077 and spec 016 tests), then deserialize the Kafka message payload and assert on the resulting `Frontmatter["phase"]` value. Do NOT shortcut by inspecting the in-memory `lib.Task` before `ParseEvent` — the test must traverse the same serialization path production traffic takes. The existing test file already has this capturing pattern from 077; reuse it.

7. **Tests — `lib/claude/result-deliverer_test.go`** (if it exists)

   If test file exists, add an assertion that the adapter propagates `NextPhase` from `AgentResultLike` to `AgentResultInfo`. Use a counterfeiter mock for the inner `delivery.ResultDeliverer` and verify the passed `AgentResultInfo.NextPhase` matches `result.GetNextPhase()`.

   If no such test file exists, add a minimal one following the existing Ginkgo-suite pattern in `lib/claude/`.

8. **No downstream consumer changes in this prompt**

   Downstream consumers live in sibling repos (`~/Documents/workspaces/trading/agent/trade-analysis`, `~/Documents/workspaces/trading/agent/hypothesis`, `~/Documents/workspaces/code-reviewer/agent/pr-reviewer`, etc.) and update separately when they bump the `github.com/bborbe/agent/lib` dependency.

   This is a **breaking interface change**: any concrete type stored in an `AgentResultLike` variable must now also implement `GetNextPhase() string`, otherwise it won't compile against the new lib. Callout covered in the CHANGELOG entry (step 9).

9. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased`:

   ```markdown
   - Agents can request a phase transition via new `NextPhase` field on `AgentResultInfo` and `AgentResultLike` — `kafkaResultDeliverer` writes the requested phase on `status: done`; failure/needs_input paths continue to escalate to `human_review` (074/077 rules win).
   - BREAKING: `AgentResultLike` interface gains a `GetNextPhase() string` method — downstream consumers of `lib/claude` (pr-reviewer, backtest-agent, trade-analysis, hypothesis) must add this method to their concrete `AgentResult` types when bumping to this lib version.
   ```

10. **Verification commands** (repo-relative)

    Must exit 0:
    ```bash
    cd lib && make precommit
    ```

    Spot checks:
    ```bash
    grep -n 'NextPhase' lib/delivery/result-deliverer.go lib/claude/types.go lib/claude/result-deliverer.go
    ```
    Must show matches in all three files.

    ```bash
    grep -n 'GetNextPhase' lib/claude/types.go
    ```
    Must show the interface method + the struct method.

    ```bash
    grep -n 'resolveNextPhase' lib/delivery/result-deliverer.go
    ```
    Must show both the function definition and its call site in the `done` case.

</requirements>

<constraints>
- Only edit these files:
  - `lib/delivery/result-deliverer.go` (add field, add switch-helper, update kafka deliverer done-case)
  - `lib/delivery/result-deliverer_test.go` (new test cases)
  - `lib/claude/types.go` (add method to interface, add field + method to struct)
  - `lib/claude/result-deliverer.go` (update adapter to pass NextPhase through)
  - `lib/claude/result-deliverer_test.go` (new adapter test) — create if absent, following lib suite patterns
  - `CHANGELOG.md`
- Do NOT modify `content-generator.go` — the kafka deliverer overrides, and the content-generator should not know about `NextPhase` (path of least coupling).
- Do NOT modify `status.go`, `markdown.go`, `print.go`.
- Do NOT modify `task/executor/` or `task/controller/` — this prompt stays in lib. Their result-handling pathway already reads `Frontmatter` from the Kafka command and forwards it to `resultWriter`.
- Do NOT rename `AgentResultInfo`, `AgentResultLike`, or the `Status` enum.
- `NextPhase` is a plain `string` — not `domain.TaskPhase` — to avoid importing `vault-cli` types into `AgentResultLike` (interface lives in `lib/claude`; keeping it primitive avoids a transitive dep). Validation happens at the use site (`resolveNextPhase`).
- `NextPhase` is IGNORED when status is not `done`. Failure and `needs_input` always set `human_review` regardless of `NextPhase`.
- Empty `NextPhase` on `done` falls back to `phase: done` — matches the existing single-phase agent behavior; zero regression.
- Invalid `NextPhase` on `done` is logged via `glog.Warningf` and falls back to `done`. Never fails the result write.
- Use `github.com/bborbe/errors` for any new error paths (unlikely — this prompt introduces none).
- Ginkgo v2 only. External test packages match existing file conventions.
- This is a BREAKING CHANGE for `lib/claude` consumers — they must implement `GetNextPhase()`. Clearly flagged in CHANGELOG.
- Do NOT commit — dark-factory handles git.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify the new field is in the envelope:
```bash
grep -nA1 'Status\s*AgentStatus' lib/delivery/result-deliverer.go
```
Must show the struct now includes `NextPhase string` below `Message`.

Verify the interface method exists:
```bash
grep -nA8 'type AgentResultLike' lib/claude/types.go
```
Must show `GetNextPhase() string` in the method list.

Verify the adapter propagates:
```bash
grep -nA6 'func (a \*resultDelivererAdapter' lib/claude/result-deliverer.go
```
Must show `NextPhase: result.GetNextPhase()` in the `AgentResultInfo` literal.

Verify the helper + call site:
```bash
grep -nB1 -A5 'func resolveNextPhase' lib/delivery/result-deliverer.go
```
Must show the function definition with `domain.TaskPhase(requested).Validate` and the `glog.Warningf` fallback.

```bash
grep -n 'resolveNextPhase(result.NextPhase)' lib/delivery/result-deliverer.go
```
Must show exactly one match (in the `AgentStatusDone` case).

Verify failure paths still set human_review:
```bash
grep -nA3 'case AgentStatusNeedsInput\|default:' lib/delivery/result-deliverer.go
```
Must show both arms still write `"human_review"` — unaffected by NextPhase.

Run focused tests:
```bash
cd lib && go test -v ./delivery/... ./claude/...
```
Must exit 0. Output must include PASS lines for the new NextPhase test cases.

Run full precommit:
```bash
cd lib && make precommit
```
Must exit 0.

Verify CHANGELOG updated with BREAKING callout:
```bash
grep -n 'NextPhase\|BREAKING' CHANGELOG.md
```
Must show the feat entry and the BREAKING entry under Unreleased.

Post-merge live verification (NOT part of this prompt's execution — documented for the human):
1. Deploy new lib release. Downstream agents that vendor `lib/claude` will fail to compile until they add `GetNextPhase()` to their `AgentResult` types — this is expected and flagged by the BREAKING changelog entry.
2. Once a multi-phase agent (hypothesis v2) is deployed, verify: posting a task with `phase: planning` spawns a Job; agent returns `status: done, NextPhase: "in_progress"`; task file on disk now shows `phase: in_progress`; controller re-triggers → second Job spawns under `phase: in_progress`; agent returns `status: done, NextPhase: ""` → task file shows `phase: done, status: completed`.
</verification>
