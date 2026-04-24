---
tags:
  - dark-factory
  - spec
status: draft
---

## Summary

- Adds a `retryable` flag to the agent's `failed` result so the controller can distinguish transient infrastructure failures from deterministic ones.
- `failed + retryable=false` escalates immediately to `human_review` on the first result, bypassing the trigger-cap retry loop.
- `failed` without the flag, or with `retryable=true`, keeps the existing spec 015 trigger-cap behavior — back-compatible with every agent shipped today.
- Motivating bug: pr-reviewer hits `gh auth unauthenticated`, retries three times producing byte-identical output, wastes three spawn slots before escalation.
- Opt-in per failure site — individual agents adopt the field over time. No enum change, no Kafka schema change, no phase change.

## Problem

Today the controller has two terminal classes for failure-shaped outcomes: `failed` (retries up to `max_triggers`, then escalates) and `needs_input` (escalates immediately because the task content is wrong). Both buckets are wrong for a failure that is infrastructure in nature but deterministic — for example, an agent discovering that its environment is missing a credential, a binary, or a config file. Retrying such a failure produces byte-identical output on every attempt, burns every retry slot, delays escalation, and fills the task file with duplicate `## Result` sections. The agent already knows retrying is pointless; the controller has no way to hear that.

## Goal

After this work, an agent that detects a deterministic failure in its environment can signal that retrying is futile, and the controller will escalate to `human_review` on the first result without consuming retry slots. Agents that do not signal anything continue to behave exactly as today.

## Non-goals

- NOT adding a new `AgentStatus` enum value. The three-value enum (`done`, `failed`, `needs_input`) stays stable.
- NOT changing the Kafka event schema. The new field is additive inside the existing result JSON payload.
- NOT changing spec 015's trigger-cap behavior for retryable failures.
- NOT changing spec 010's `needs_input` routing.
- NOT requiring agents to adopt the field in this spec. Agent-side adoption is tracked per-agent, separately.
- NOT introducing a new task `phase` or `status` value. Escalation reuses `phase: human_review`.

## Desired Behavior

1. An agent result of `failed` with no `retryable` field is treated exactly as `failed` is treated today — routed through the spec 015 trigger-cap path.
2. An agent result of `failed` with `retryable: true` is treated the same as case 1.
3. An agent result of `failed` with `retryable: false` causes the controller to set `phase: human_review` atomically on the first result, without bumping any counter and without spawning again.
4. When a non-retryable failure escalates, the task file records the cause in a clearly-labelled section — distinct from the trigger-cap escalation message and from the `needs_input` escalation message — so an operator reading the file can tell which of the three escalation paths fired.
5. A `retryable` field emitted alongside a `done` or `needs_input` status is ignored (the flag has no meaning outside `failed`).
6. Evaluation is per-result. If the same task produces `retryable: true` on one attempt and `retryable: false` on a later attempt, each result is evaluated independently — the later result short-circuits to `human_review` even if earlier retries bumped the counter.
7. The four-outcome matrix (done, needs_input, failed-retryable, failed-non-retryable) is documented in the project's failure-semantics guide, and the agent-side contract for setting the flag is documented where agent authors will find it.

## Constraints

- The `AgentStatus` enum in `lib/delivery/status.go` does not change.
- The Kafka topic schemas (`agent-task-v1-event`, `agent-task-v1-request`) do not change — the new field rides inside the existing result payload.
- The existing `phase` allowlist (`planning`, `in_progress`, `ai_review`) is unchanged. Non-retryable escalation writes `phase: human_review`, which is already how spec 010 escalates `needs_input`.
- Agents that do not emit the field must see no behavioral change. The field is a pointer (unset vs. explicit `false` must be distinguishable) and defaults to "retryable" when unset.
- Spec 010 (`needs_input` routing) and spec 015 (atomic trigger-cap) remain authoritative for their respective paths.
- Related context: `docs/task-flow-and-failure-semantics.md`, `docs/agent-job-interface.md`, `docs/controller-design.md`.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Agent emits `failed` without `retryable` field | Treated as retryable; spec 015 trigger-cap path applies | None needed — pre-existing behavior |
| Agent emits `failed` with `retryable: true` | Spec 015 trigger-cap path applies | None needed |
| Agent emits `failed` with `retryable: false` | Controller escalates to `phase: human_review` on first result; no counter bump; no further spawns | Human reviews the task, fixes the underlying environment, flips phase back into the allowlist |
| Agent emits `done` with `retryable` set | Flag ignored; task completes normally | N/A |
| Agent emits `needs_input` with `retryable` set | Flag ignored; spec 010 path applies | N/A |
| Agent flips flag across retries (true then false) | Each result evaluated independently; later `retryable: false` short-circuits to `human_review` regardless of prior counter state | Human reviews — counter state is informational only |
| Malformed `retryable` value (e.g. string instead of bool) | Treat as absent → retryable=true (default); log a warning | Fix the agent's output format |

## Security / Abuse Cases

- The `retryable` flag is agent-controlled input parsed from agent output. A buggy or compromised agent could set `retryable: false` on every failure to force immediate escalation, or `retryable: true` to force maximum retry consumption. Both failure modes are bounded: the worst-case escalation behavior is identical to today's `needs_input` path (one spawn, one escalation), and the worst-case retry behavior is identical to today's trigger-cap path (N spawns, one escalation). No new DoS surface.
- The flag cannot cause the controller to write outside the allowed phase set — escalation targets `human_review`, which is already reachable via `needs_input`.
- No new file, path, or URL is introduced by this flag; input validation is limited to parsing a JSON boolean.

## Acceptance Criteria

- [ ] The agent-result payload parsed by the controller carries a `retryable` signal with pointer-like semantics (unset, `true`, `false` all distinguishable).
- [ ] Controller escalates `failed + retryable=false` to `phase: human_review` on the first matching result, with no counter bump and no further spawn.
- [ ] Controller's handling of `failed` with absent or `retryable=true` is byte-identical to the pre-change spec 015 trigger-cap behavior.
- [ ] Controller's handling of `done` and `needs_input` is unchanged regardless of any `retryable` value.
- [ ] The escalation section written to the task file for a non-retryable failure is visually distinct from the trigger-cap escalation and from the `needs_input` escalation, and names the agent-reported cause.
- [ ] Unit tests cover: retryable=true retries; retryable=false short-circuits; retryable absent equals retryable=true; cross-retry flag flip; flag present on non-failed statuses is ignored; malformed flag value is treated as absent.
- [ ] `docs/task-flow-and-failure-semantics.md` documents the four-outcome matrix and describes each escalation message variant.
- [ ] The agent-author-facing contract for emitting `retryable` is documented in the docs tree where existing agent-contract guidance lives.
- [ ] Manual smoke in dev: a task that emits `failed + retryable=false` produces exactly one spawn and lands in `phase: human_review` immediately.

## Verification

```
cd ~/Documents/workspaces/agent/lib && make precommit
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

Manual dev smoke:

```
# Create a test task whose agent emits failed + retryable=false
# Observe: exactly one K8s Job spawn
# Observe: task frontmatter ends at phase: human_review
# Observe: task file contains a Failure Escalation section naming the agent's message
# Observe: trigger_count incremented at most once
```

## Do-Nothing Option

Keep today's behavior: every `failed` result costs `max_triggers` spawns before escalation. Agents like pr-reviewer that hit deterministic environment failures continue to burn retry budgets producing byte-identical output. Operators continue to wait three cycles for escalation on failures that could never self-heal. Acceptable only if the frequency of deterministic-failure cases stays low — in practice, credentials, missing binaries, and missing config are recurring.

## Candidate docs follow-ups

Domain rules described here that are not yet in `docs/`:

- Agent-side contract for emitting `retryable` (where in the result JSON, pointer semantics, which statuses it applies to). Candidate home: a new section in `docs/agent-job-interface.md` or a dedicated `docs/agent-result-contract.md`.
- Four-outcome matrix and the three distinct escalation messages. Candidate home: extension of `docs/task-flow-and-failure-semantics.md`.

## Related

- `specs/completed/010-failure-vs-needs-input-semantics.md` — established the `needs_input` escalation path this spec extends.
- `specs/in-progress/015-atomic-frontmatter-increment-and-trigger-cap.md` — the trigger-cap path this spec short-circuits for non-retryable failures.
- `docs/task-flow-and-failure-semantics.md` — current failure taxonomy that this spec updates.
- Motivating evidence: pr-reviewer bug `edef6449-dea1-4df1-b0d2-96a4789ba32c` (`gh auth unauthenticated` burns three retries producing byte-identical output).
