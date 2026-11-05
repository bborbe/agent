---
status: approved
approved: "2026-11-05T17:29:13Z"
branch: dark-factory/bug-empty-output-section-is-not-complete
---

# Summary

- `agentStep.ShouldRun` treats an **empty** output section as completed work.
- A task body that already carries the heading — as a bare `## Result` with nothing under it — therefore skips every step.
- `StepRunner.Run` returns a nil result, the caller reports `agent run returned no result`, and the Job fails in ~56 ms without ever invoking claude.
- The fix is that an empty section is not completion: it must re-run.

# Problem

`ShouldRun` returns `false` whenever the output section exists, unless a failure marker is present. `failureMarked` recognises two markers: a `## Failure` section, and an output-section body that parses to a `needs_input`/`failed` `AgentResult`. An **empty** body satisfies neither — `parseAgentResultBody("")` returns `ok == false` — so it falls through to `return false, nil` and the step is skipped.

The result is a task that can never run. Its body carries the output heading from the moment it is filed (a template, a hand-written skeleton, or an earlier partial edit), so every dispatch skips, returns nil, and fails. Because no result is delivered, nothing surfaces except a pod-level alert and a Sentry event.

`bborbe/agent` spec 051 (`specs/in-progress/051-bug-agent-step-shouldrun-idempotency-skip-poisons-redispatch.md`, `status: verifying`) fixed the neighbouring case — a *failed* run leaving a populated section behind now re-runs. Its Acceptance Criterion 2 deliberately preserved the skip for "a genuine success body … or a non-JSON prose body". An empty body is neither: it is the absence of output, and the spec-051 fix left it on the skip side.

# Goal

`ShouldRun` returns `true` when the output section exists but carries no content, so a dispatch re-invokes the step. A section with content keeps today's skip, preserving the single-step idempotency spec 051 established.

# Non-goals

- **No change to the `## Failure` rule.** Spec 051's failure-marker behaviour is unchanged; this only adds a third reason to re-run.
- **No change to `StepRunner` semantics or the `Step` interface.** The fix is internal to `claude/agent-step.go`.
- **No change to the executor.** Suppressing dispatch for an already-complete phase is a separate concern with its own failure mode (a pre-seeded heading would advance the task to `done` without the work being done) and is not part of this fix.
- **No change to what counts as a success body.** A non-empty body that is not a failure marker still skips, exactly as spec 051 requires.

# Assumptions

1. An empty output section means no step wrote output — a successful run always writes something, because the LLM's response is the section body.
2. The section body a caller sees through `md.FindSection` is the text under the heading, so an empty section yields an empty or whitespace-only string.
3. A re-run is bounded: the executor's spawn cap and `trigger_count` already stop an unbounded loop.

# Alternatives considered

- **Suppress the dispatch in `agent-task-executor` instead.** Rejected: it does not fix this shape. The executor would skip the Job and advance the phase to `done` with an empty `## Result`, converting a loud 56 ms failure into a silent false completion — strictly worse.
- **Treat an empty section as a `needs_input` failure marker.** Rejected: it would write a `## Failure` section for a task that never ran, polluting the failure record and the failure-marker convention.
- **Do nothing.** The task is permanently undispatchable and the only symptom is alert noise; the operator cannot distinguish it from a busy agent.

# Reproduction

Confirmed 2026-11-05 on sun-k3s, `agent-claude` `v0.26.6` (pinning `bborbe/agent v0.139.2`), `agent-task-executor` `v0.50.2`. Re-confirmed at this worktree's HEAD (`v0.139.4`): `ShouldRun`, `failureMarked` and `parseAgentResultBody` are unchanged.

1. File a task whose body ends with a bare `## Result` heading and carries no `## Failure`:

```
Question: …

## Result
```

2. Dispatch it to an agent-claude-derived image. Job `service-analysis-agent-86f081cc-20261105170146`, namespace `analysis-sandbox` on sun:

```bash
kubectlsun -n analysis-sandbox logs job/service-analysis-agent-86f081cc-20261105170146 \
  > /tmp/repro-86f081cc.log
```

Observed:

```
I1105 17:01:47.909793  1 main.go:220] agent-claude started phase=execution
E1105 17:01:47.966188  1 service_main.go:91] application failed: agent run returned no result
```

Delta 56.4 ms. The captured `TASK_CONTENT` argument shows the body ending at `## Result` with nothing after it. The marker `spawning claude CLI` (`claude/claude-runner.go:297`, glog `-v=2`) occurs **0** times in that log — claude never started. It occurs 2× in a successful run (`sentry-analyzer-agent-0a936645-20261105164408`) and 1× in another (`service-analysis-agent-service--20261105164206`), so the absence is a real signal rather than a verbosity artefact.

The same shape produced the original incident: task `Service Analysis - Store Classification git-rest`, job `service-analysis-agent-6b1f0e2a`, three of four attempts failing this way, `trigger_count: 7`. Its body carried an empty `## Result` heading from creation (`git show baf5a28d22:"25 Tasks/Service Analysis - Store Classification git-rest.md"`).

**Root-cause code path at this HEAD:**

```
claude/agent-step.go:87-96   ShouldRun   — section exists → failureMarked? → no → return false
claude/agent-step.go:103-117 failureMarked — ## Failure? no; parse body "" → ok == false → return false
claude/agent-step.go:124-134 parseAgentResultBody — no JSON object in "" → ok == false
```

# Expected vs Actual

**Expected** (per the `ShouldRun` doc comment: *"if the LLM already wrote its section in a prior Job that crashed before phase advance, skip the re-invocation"*): the skip exists to avoid re-invoking claude for work that already completed. A section with no content is not completed work — re-dispatch must invoke.

**Actual:** `ShouldRun` treats an empty section as completed work, so the step is skipped forever and the dispatch fails with no result.

# Why this is a bug

The `Step` contract describes `ShouldRun` as reporting whether the step *has already completed*. An empty output section is the absence of completion, not its evidence. Spec 051 established that a failure marker must force a re-run precisely because "an existing section" does not imply "a successful run"; the empty body is the limiting case of that same reasoning and was left uncovered.

# Acceptance Criteria

- [ ] `ShouldRun` returns `true` when the output section exists with an empty or whitespace-only body and no `## Failure` section — evidence: new Ginkgo rows under `Describe("ShouldRun")` in `claude/agent-step_test.go`, named `"empty section body → returns true"` and `"whitespace-only section body → returns true"`, both green in `go test ./claude/ -v` output.
- [ ] `ShouldRun` returns `false` when the output section carries non-empty content and no `## Failure` section — evidence: the existing idempotency row stays green, plus a row named `"non-empty prose body → returns false"`; `go test ./claude/ -v` names all three.
- [ ] `ShouldRun` returns `true` when a `## Failure` section is present, whatever the output section's body — evidence: the spec-051 rows remain green and unmodified in `go test ./claude/ -v` output.
- [ ] `ShouldRun` returns `true` when the output section is absent — evidence: the existing "section missing" row remains green.
- [ ] `make precommit` exits 0 at the repo root.
- [ ] **Post-Deploy (Rung-3):** a task whose body ends with a bare `## Result` runs claude instead of skipping — evidence: dispatch such a task to a consumer image built on this fix and confirm its Job log carries `spawning claude CLI` at least once and does **not** carry `agent run returned no result`.
  - `deploy_check:` `kubectlsun -n analysis-sandbox get deploy/agent-task-executor -o jsonpath='{.spec.template.spec.containers[0].image}'`
  - `deploy_target:` `$(git -C /Users/bborbe/Documents/workspaces/agent-claude describe --tags --abbrev=0)` — the consumer tag carrying this library version

# Verification

## Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — format, generate, test, lint, license all clean
- `go test ./claude/ -v` — the new rows and every existing `ShouldRun` row pass
- `grep -n 'TrimSpace' claude/agent-step.go` — the empty-body check is present in `ShouldRun` or the helper it calls

## Operator-executable (runs on the host after merge)

1. Release the library, bump `bborbe/agent-claude`'s pin, publish the image, and deploy it to one test Config.
2. Replay the reproduction: the Job log carries `spawning claude CLI`, and the task's `## Result` gains content.

# Desired Behavior

1. When the output section exists and its body is empty or whitespace-only, and no `## Failure` section is present, `ShouldRun` returns `true`.
2. When the output section exists and its body is non-empty and not a failure marker, `ShouldRun` returns `false` — unchanged from today.
3. When a `## Failure` section is present, `ShouldRun` returns `true` — unchanged from spec 051.
4. When the output section is absent, `ShouldRun` returns `true` — unchanged.
5. The empty-body decision is made on the section body after `strings.TrimSpace`, so a body of only newlines or spaces counts as empty.
6. No new log line is emitted on the empty-body path beyond what the step already logs when it runs; the re-run is visible through the normal `spawning claude CLI` marker.

# Constraints

- **Spec 051's rows must stay green and unmodified.** The failure-marker behaviour and the non-empty-prose skip are contracts other agents depend on.
- **`parseAgentResultBody` is unchanged.** The empty check is a separate condition in `ShouldRun`/`failureMarked`, not a change to JSON parsing.
- **No new exported API.** The change is internal to `claude/agent-step.go`.
- **The `Step` interface and `StepRunner` are untouched.**

# Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| A step whose successful output is legitimately an empty section | The step re-runs on every dispatch | Bounded by the executor's spawn cap and `trigger_count`. Detection: repeated Jobs for one task with `spawning claude CLI` in each. No such agent is known today |
| Section body is only a fenced code block that renders empty | Treated as non-empty (it has text), so it still skips | By design — the check is on text, not rendering |
| Section body is whitespace from a formatting quirk | Treated as empty and re-runs | Correct: nothing was written. Detection: the re-run's output replaces the whitespace |
| A caller passes a body whose whitespace is significant | Re-runs once, writes real content, then skips | Self-correcting after one dispatch |

# Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Empty/whitespace-only body check in `ShouldRun`, plus the Ginkgo rows for empty, whitespace-only, non-empty prose, failure marker and missing section | 1, 2, 3, 4, 5 | 1, 2, 3, 4, 5 | — |

Rationale: single-layer, single-behavior change in one file plus its test. The post-deploy AC is operator-executable and runs after the library release reaches a consumer image; it needs no prompt.

**Not covered by this prompt — operator-executable:** releasing `bborbe/agent`, bumping the `bborbe/agent-claude` pin, publishing the image and deploying it to a test Config. Until that lands the fix reaches no cluster.
