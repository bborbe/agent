---
status: draft
---

## Summary

- Distinguish **infrastructure failure** (agent crashed, parse error, network) from **task-level "needs input"** (agent ran cleanly but task is unclear/impossible)
- `AgentStatusFailed` keeps current behaviour: retry counter increments, escalate to `human_review` after N attempts
- `AgentStatusNeedsInput` routes directly to `human_review` with no retry — task content, not infrastructure, is the problem
- Claude result parser tolerates prose preamble/suffix around the JSON object; strict parsing inflates infra-failure counts

## Problem

Today `lib/delivery/content-generator.go` maps both `failed` and `needs_input` to `phase: ai_review`, so both go through the retry counter (spec 008). This is wrong on three axes:

1. **Pollutes the task file.** Each retry appends another `## Result` block. A task that genuinely needs human input accumulates up to N duplicate Result sections before reaching `human_review`. On the next run the LLM reads those previous Result sections and parrots them back as hallucinated errors.
2. **Wastes compute.** A task like "analyze trades between X and Y" with zero trades will produce the same answer every retry. N × 3 min Claude run = N × 3 min burned per impossible task per day.
3. **Hides the signal.** `phase: human_review after retry_count=4` looks like an infra problem; `phase: human_review after needs_input` correctly points the human at the task content.

Observed: smoke test task `0dd09314` (window 2026-04-03 → 2026-04-04, zero live trades in that window). Agent correctly identified no trades but emitted `status: done` with narrative prose prefix. Prose prefix broke strict JSON parser → synthesised `failed` result → retry loop ×4 → `human_review`. Two bugs chained: parser too strict, and the retry would have been wrong even if parsing had succeeded.

## Assumptions

- Claude occasionally emits prose around its final JSON object under reasoning pressure; this is a recurring pattern, not a one-off.
- Agents are free to choose between `done` / `failed` / `needs_input`; prompts define when each is appropriate.
- Existing retry counter semantics from spec 008 remain correct for genuine infra failures — this spec narrows what counts as one.
- `phase: human_review` is outside the executor's `allowedPhases`, so landing there naturally stops respawning.

## Do-Nothing Option

We keep the current conflated path. Impacts:

- Every impossible task burns ~N × 3 min of compute before reaching human review.
- Task files accumulate duplicate `## Result` sections, poisoning the context of the next invocation.
- Every occurrence of Claude prose-prefix counts as one infra failure, inflating the `ai_review` retry counter and masking real infra issues in metrics.
- Humans reviewing a `human_review` task cannot tell whether the agent crashed four times or the task itself was impossible — two very different actions for the human.

Rejected: we already have evidence of all three impacts in production, and fixes are local (three files plus one prompt).

## Desired Behavior

1. When an agent emits `done`, the task moves to `status: completed`, `phase: done`. Unchanged.
2. When an agent emits `needs_input`, the task moves to `status: in_progress`, `phase: human_review` on the **first** result. The retry counter is not incremented. No further job is spawned for this task.
3. When an agent emits `failed`, the task moves to `status: in_progress`, `phase: ai_review`. The retry counter increments. After `retry_count >= max_retries` the task escalates to `human_review` with a `## Retry Escalation` section. Unchanged from spec 008.
4. When Claude's JSON output is surrounded by prose, the result parser still extracts the agent's intended status. Only if no balanced JSON object can be found does the parser treat it as an infra failure.
5. Agent prompts instruct Claude to emit `needs_input` for semantically impossible or underspecified tasks (missing data, contradictory parameters, zero results where results were required).

## Constraints

**Must not change:**

- AgentStatus enum values (`done`, `failed`, `needs_input`) or Kafka topic schemas.
- Spec 008 retry-counter behaviour for genuine `failed` results.
- Executor phase filtering (`allowedPhases = {planning, in_progress, ai_review}`) — `human_review` remains outside it.
- Agent binary APIs — agents keep publishing the same three statuses.

**Must not regress:**

- A task already at `phase: ai_review` with `retry_count > 0` must continue retrying on next deploy (forward-compatible).
- Legitimately-failed runs still escalate after `max_retries`.
- `completed` results still short-circuit the retry counter.

## Failure Modes

| Trigger | Expected | Recovery |
|---|---|---|
| Agent emits `done` with valid JSON | `status: completed`, `phase: done`, single Result section | None needed |
| Agent emits `done` with prose + JSON | Parser extracts JSON, treats as `done` | None needed |
| Agent emits `needs_input` | `phase: human_review` immediately, retry_count unchanged | Human edits task content, resets `status/phase` |
| Agent emits `failed` | `phase: ai_review`, retry_count++; at max, escalate to `human_review` | Retry or human intervention |
| Claude returns non-JSON garbage | Parser returns "no JSON object found" failure; counted as `failed` | Normal retry path |
| Claude returns multiple JSON objects | Parser picks the last top-level balanced object | None needed |
| Agent crashes (panic, CLI error) | Existing executor synthetic-failure path (spec 009) — `failed` | Retry path |
| Task already at `phase: human_review` receives another result | Do not re-increment retry counter | None (terminal state) |

## Security / Abuse Cases

- Parser processes untrusted Claude output. Scan for balanced `{...}` only; no code execution, no regex backtracking. Malformed input yields an infra failure, not a crash.
- An agent could emit `needs_input` on every task to skip retry logic. Mitigated by: escalation is still visible to humans (task surfaces in `human_review`), and agent prompts govern when `needs_input` is appropriate. Abuse is a prompt/agent problem, not an infra problem.

## Verification

From agent repo root:

```
cd lib/claude     && go test ./...
cd lib/delivery   && go test ./...
cd task/controller && go test ./pkg/result/...
make precommit    # at repo root, run once before deploy
```

End-to-end smoke:

1. Rebuild + deploy `task-controller` and `trade-analysis` agent to dev.
2. Create a smoke-test task whose window contains zero live trades (e.g. `2026-04-03 → 2026-04-04`).
3. Watch dev K8s for agent jobs keyed by the task UUID.
4. Expect: exactly one agent job, terminating in `status: in_progress`, `phase: human_review`, `retry_count: 0`, and a single `## Result` block in the task file.
5. Force a genuine infra failure (e.g. kill the pod mid-run) and verify the retry counter still escalates at `max_retries`.

## Acceptance Criteria

- [ ] Smoke-test task in a zero-trade window completes in one job, lands at `phase: human_review` with `retry_count: 0`, and has exactly one `## Result` block.
- [ ] A genuinely-failing run (e.g. CLI exits non-zero) retries up to `max_retries` then escalates to `human_review` with a `## Retry Escalation` section.
- [ ] Unit tests cover all three `AgentStatus` values in `lib/delivery/content-generator`.
- [ ] Unit tests cover prose-prefix, prose-suffix, and nested-braces cases in `lib/claude/task-runner`.
- [ ] Unit test covers "phase already `human_review`" skip-retry path in `task/controller/pkg/result`.
- [ ] Trade-analysis agent prompt documents when to emit `needs_input` vs `done` vs `failed`.
- [ ] `make precommit` passes.

## Open Questions

None.
