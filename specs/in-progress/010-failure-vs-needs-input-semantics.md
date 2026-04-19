---
status: verifying
approved: "2026-04-19T10:39:21Z"
generating: "2026-04-19T10:41:09Z"
prompted: "2026-04-19T10:45:12Z"
verifying: "2026-04-19T16:47:15Z"
branch: dark-factory/failure-vs-needs-input-semantics
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

## Goal

Agents and the controller share one coherent vocabulary for task outcomes:

- **Success** ends the task.
- **Task-wrong** (`needs_input`) escalates to a human immediately — no retry, no duplicate Result sections.
- **Infra-failure** (`failed`) still retries up to `max_retries`, then escalates.
- Parser tolerates prose around the JSON so real infra failures aren't synthesised from normal Claude output.

A human reading the frontmatter of an escalated task can tell at a glance whether the task is wrong or the agent is broken.

## Non-goals

- Not replacing spec 008's retry counter — refining what counts as one attempt.
- Not changing AgentStatus enum values or Kafka topic schemas.
- Not introducing a new `task_impossible` phase — reusing existing `human_review`.
- Not changing executor phase filtering or allowed-phase list.
- Not teaching the controller to grade agent output quality — agents self-classify via status.
- Not building a prompt-approval loop — prompt-side changes are a one-time edit to existing output-format docs.

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

- [ ] A task in a zero-trade window completes in exactly one agent job, lands at `phase: human_review` with `retry_count: 0`, and has a single `## Result` block.
- [ ] A genuine infra-failure run (e.g. CLI exits non-zero) still retries up to `max_retries` and then escalates to `human_review` with a `## Retry Escalation` section appended.
- [ ] Unit tests cover all three `AgentStatus` values (`done`, `failed`, `needs_input`) in the content-generator.
- [ ] Unit tests cover prose-prefix, prose-suffix, and nested-braces cases in the Claude result parser.
- [ ] Unit test covers the "phase already `human_review`" skip-retry path in the controller's result writer.
- [ ] A task emitting `needs_input` does not increment `retry_count`.
- [ ] A task whose agent emits prose-wrapped JSON is not counted as an infra failure.
- [ ] Agent prompts document when to emit `needs_input` vs `done` vs `failed`.
- [ ] `make precommit` passes across affected modules.

## Open Questions

None.
