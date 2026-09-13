---
status: approved
approved: "2026-08-29T19:07:04Z"
generating: "2026-08-29T19:10:07Z"
branch: dark-factory/bug-agent-step-shouldrun-idempotency-skip-poisons-redispatch
---

# agentStep.ShouldRun idempotency skip poisons re-dispatch after a failed run

## Summary

- `claude/agentStep.ShouldRun` returns `false` whenever the step's output section (`## Analysis`) already exists in the task body — its documented single-step idempotency check.
- A **failed** run leaves that output section behind (a `needs_input`/failure JSON body written by `agentStep.Run` when the runner returns bytes, or a stale success body when a later run died at the job level). The guard cannot distinguish "ran and succeeded" from "ran and failed".
- Every subsequent re-dispatch therefore skips the step → no claude invocation → nil result → executor reports `all steps skipped for phase planning` → Job fails `deadline_exceeded` → `trigger_count` churns → **zero per-alert tasks**, silently.
- Verified live 2026-08-28/08-29 on the `sentry-collector-agent` daily fan-out: two daily cycles produced zero per-alert `Analyze Sentry issue *` tasks; manually clearing `## Analysis` + resetting `trigger_count` made the SAME image/wiring/prompt succeed end-to-end (fetched 68, published 68, 72 per-alert files in vault). The "all steps skipped for phase planning" nil-result error surfaces in the collector agent's `main.go:314` (`agent run returned nil result (all steps skipped for phase %s)`), not in the executor.
- Fix: `ShouldRun` must skip only on a **success** section — a failure marker (a `## Failure` section present, or the output section body parsing to a `needs_input`/`failed` AgentResult) must still re-run. The repo-wide `## Failure` convention already exists (this repo's `delivery/content-generator.go`, and `agent-task-executor`'s `pkg/result_publisher.go`) as the failure marker to key on.

## Problem

The sentry daily fan-out pipeline (goal [[Build Sentry Issue Analyzer Agent]], task [[Fix agent-task-controller-executor Stripping Routing Assignee on Task Reset]]) must land one `Analyze Sentry issue *` task per unresolved prod alert every ~00:00Z cycle. The 08-28 and 08-29 cycles silently produced zero per-alert tasks. Root cause (code-verified): `claude/agent-step.go:69-74` `agentStep.ShouldRun` returns `!exists` for the output section. A failed run writes `## Analysis` (a `needs_input`/failure body via `Run`'s `md.ReplaceSection`, or a stale success body from an earlier partial run), so every re-dispatch sees the section present → step skipped → nil result → `deadline_exceeded` → `trigger_count` churn → the collector leg is dead with no error surfaced anywhere.

This is not collector-specific: **any single-AgentStep agent** whose run fails after writing its output section permanently loses re-dispatch (trade-analysis, pr-reviewer-style, healthcheck steps all use `NewAgentStep`).

## Goal

A failed `AgentStep` run must not permanently poison re-dispatch. Re-dispatch of a task whose previous run failed re-runs the step (claude invoked again, output section rewritten); re-dispatch of a task whose previous run succeeded keeps today's skip (single-step idempotency preserved). The `## Failure` section convention is the failure marker: presence of a failure marker forces a re-run, absence of a success section forces a run, a success section alone skips.

## Non-goals

- No change to `agent-task-executor` behavior (spawn trigger cap, `deadline_exceeded` mapping, `## Failure` job-failure writer) — the executor stays v0.6.4; that is a separate concern.
- No change to the collector's prompt or `scripts/sentry-create-tasks.sh` — the 08-28 `needs_input` was a one-off transient (tool-match denial disproven by the controlled re-arm); this spec fixes the re-dispatch poison, not the transient.
- No new section-name vocabulary — only the existing `## Failure` convention is used.
- No change to the `Step` interface or `StepRunner` semantics — the fix is internal to `claude/agent-step.go`.
- No change to prose-section agents' semantics (trade-analysis, pr-reviewer) — an unparseable body with no `## Failure` marker still skips on re-dispatch.

## Reproduction

**Environment:** `sentry-collector-agent` daily fan-out, executor v0.6.4, controller v0.5.2, collector image `agent-sentry-issue-analyzer:prod` (claude CLI 2.1.247, built 08-27), prod nukeprod cluster.

**Observed (2026-08-28):** the daily collector Job hit `needs_input` — "The script `scripts/sentry-create-tasks.sh` cannot be executed in this environment — each invocation triggers a permission prompt that is being declined" → Job `deadline_exceeded`. The run left `## Analysis` in the task body (with a failure/needs_input body). Every subsequent re-dispatch logged `all steps skipped for phase planning` (nil result) → `deadline_exceeded` → `trigger_count` churned 2→7.

**Observed (2026-08-29, controlled re-arm):**
1. Task `Sentry Alert Fan-Out - 2026-08-29` body contains `## Analysis` (with a body) from the prior failed run.
2. Re-dispatch at 17:59 → executor spawns job `sentry-collector-agent-b7439fc8-20260829175911` → agent runs → `all steps skipped for phase planning` → nil result → `deadline_exceeded`. ZERO per-alert tasks.
3. **Manual clearing of `## Analysis` + resetting `trigger_count` 4→1 (below the executor v0.6.4 default-3 spawn cap) → re-dispatch at 20:05 → step RUNS** — claude invoked `Bash(scripts/sentry-create-tasks.sh)`, fetched 68, published 68, `status: done` (job `...175309`, `succeeded`). Controller created 37 + reopened 37 per-alert files; vault holds 72 `Analyze Sentry issue *` files.

The controlled re-arm proves the ONLY difference between skip and run was the presence of `## Analysis`.

**Root-cause code path:**
- `claude/agent-step.go:71-74` — `func (s *agentStep) ShouldRun(...) { _, exists := md.FindSection(s.cfg.OutputSection); return !exists, nil }` — skips whenever the section exists, regardless of what the run that wrote it did.
- `claude/agent-step.go:108-111` — `Run` writes `result.Result` verbatim into the output section on a successful runner return, even when that body is a `needs_input`/failure JSON.
- The nil-result "all steps skipped" error is emitted by `agent-sentry-issue-analyzer`'s `main.go:314` (and `cmd/run-task/main.go:131` in that same repo) — the collector agent treating a fully-skipped phase as a run failure, which the executor then maps to `deadline_exceeded`.

## Expected vs Actual

**Expected** (per `claude/agent-step.go:65-70` doc comment "Single-step idempotency check: if the LLM already wrote its section in a prior Job that crashed before phase advance, skip the re-invocation"): the skip exists to avoid re-invoking claude for work that already completed. A *failed* run is not completed work — re-dispatch must re-invoke.

**Actual:** `ShouldRun` treats any existing section as completed work. A failed run leaves the section (failure body or stale success body) and permanently suppresses re-dispatch with no error, no log, no escalation — silent `deadline_exceeded` churn.

## Why this is a bug

The `Step` contract (`agent_step.go` interface doc: "ShouldRun … returns false if the step has already completed (idempotency guard)") requires the guard to reflect *completion*. A section written by a failed/needs_input run is not completion — it is a partial/failed artifact. The repo already has an unambiguous failure-marker convention (`## Failure` sections produced by this repo's `delivery/content-generator.go:41,202,230` on `Failed`/`NeedsInput` status and by `agent-task-executor`'s `pkg/result_publisher.go:163,228` on job failure), so `ShouldRun` can cheaply distinguish the two states without parsing LLM prose. The bug violates the documented "idempotency" intent: it converts a transient failure into a permanent silent outage.

## Workaround

Until the fix ships: manually clear the `## Analysis` section from the task body and reset `trigger_count` to 1 (below the executor spawn cap), then let the executor re-dispatch. This is what unblocked the 08-29 cycle. Ops-only, per-task, does not scale.

## Acceptance Criteria

- [ ] **AC1 — Failure-marked output section still re-runs.** `ShouldRun` returns `true` when the output section exists AND the task carries a failure marker — either a `## Failure` section is present, or the output section body parses to an `AgentResult` with `status: needs_input` / `status: failed`. Evidence shape: Ginkgo test rows in `claude/agent-step_test.go` under `Describe("ShouldRun")` — build a `Markdown` with `## Analysis` body `{"status":"needs_input",...}` → `ShouldRun` true; with a `## Failure` section → `ShouldRun` true; `cd /workspace && go test -mod=mod ./claude/` passes with the new rows named in output.
- [ ] **AC2 — Success output section still skips (idempotency preserved).** `ShouldRun` returns `false` when the output section exists with a genuine success body (`{"status":"done",...}`) or a non-JSON prose body, and no `## Failure` section is present. Evidence shape: existing Ginkgo row "when section already exists → returns false" stays green (fixture updated to a done-body body while keeping the same row name); new row "success body → false". `cd /workspace && go test -mod=mod ./claude/` passes.
- [ ] **AC3 — Runner error path unchanged.** `Run` with a runner error still returns `AgentStatusFailed` and does NOT write the output section (existing behavior). Evidence shape: existing Ginkgo row "when runner returns error → returns Result with Failed status" passes (the no-section-write assertion is a new row, not an edit to the existing one); `md.FindSection("## Analysis")` not found after the error-path run. `cd /workspace && go test -mod=mod ./claude/` passes.
- [ ] **AC4 — Failed/needs_input runner output does not write a success-looking section.** `Run` with a runner return whose body parses to `status: needs_input` or `status: failed` returns that status (not `Done`) and does NOT write the output section — the deliverer writes `## Failure` on `Failed`/`NeedsInput` as today (`delivery/content-generator.go:196-203`), so `Run` must not leave a success-looking `## Analysis`. Evidence shape: new Ginkgo row in `Describe("Run")` — `Run` with mock runner returning `{"status":"needs_input","message":"permission denied"}` → result status `NeedsInput`, `md.FindSection("## Analysis")` not found. `cd /workspace && go test -mod=mod ./claude/` passes.
- [ ] **AC5 — `make precommit` clean at repo root.** Evidence shape: `cd /workspace && make precommit` exits 0 (single-module repo; the Makefile and go.mod are at root — there is no `claude/Makefile`).
- [ ] **Post-Deploy (Rung-3):** prod re-dispatch after a prior failed run actually re-runs the collector step and lands per-alert tasks — evidence: on nukeprod, force a failure then re-dispatch the daily collector task (or re-run after a naturally failed cycle); collector agent logs show the step invoked (no `all steps skipped for phase planning` — the nil-result error in `agent-sentry-issue-analyzer/main.go:314` is absent), the task body gains a fresh `## Analysis` with `{"status":"done"}` and `fetched:`/`published:` counts, and per-alert `Analyze Sentry issue *` files dated that day are created — matching the count, zero `deadline_exceeded`/trigger-count churn.
  - `deploy_check:` `docker manifest inspect $(kubectlnukeprod -n prod get config sentry-collector-agent -o jsonpath='{.spec.image}') | grep '"digest"'`
  - `deploy_target:` the released `sha256:<digest>` of the fixed image — the collector uses a rolling `:prod` tag, so the only per-release freshness signal is the image digest. Resolve it via the deploy_check against the correct registry `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:prod` (the image exists only there, not on `docker.io`), and record the exact digest here before running verify. Phase 0.5 compares the deploy_check stdout (a `"digest": "sha256:..."` line) against this literal digest substring; a bare `:prod` tag comparison cannot distinguish stale from fresh and is rejected.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

The agent repo is a single module — the only Makefile and go.mod are at the repo root. `make` does not search parent directories, so commands run from `/workspace`, never from `/workspace/claude`.

```bash
cd /workspace && make precommit
# must exit 0; Ginkgo rows for AC1-AC4 present in output
cd /workspace && make test
# root module still green
cd /workspace && go test -mod=mod ./claude/
# targeted Ginkgo output shows the named AC1-AC4 rows
```

### Operator-executable (runs on host after PR merge, spec verification ladder)

```bash
# 1. Release + publish the fixed lib: paired tags vX.Y.Z + lib/vX.Y.Z (same commit)
# 2. Bump github.com/bborbe/agent/lib in agent-sentry-issue-analyzer, release its image (rolling :prod tag)
# 3. Resolve the released image digest for the freshness gate (the image exists only on the
#    nuke prod registry — docker.io has no such manifest; the cluster Config points at
#    docker.prod.nuke.benjamin-borbe.de:443/...:prod):
docker manifest inspect $(kubectlnukeprod -n prod get config sentry-collector-agent -o jsonpath='{.spec.image}') | grep '"digest"'
# record the sha256 digest as the AC6 deploy_target before running verify

# 4. Deploy collector to prod; confirm the running image matches the released digest:
kubectlnukeprod -n prod get config sentry-collector-agent -o jsonpath='{.spec.image}'
# must print .../agent-sentry-issue-analyzer:prod pointing at the fixed image (digest per deploy_target)

# 5. Live re-dispatch smoke after a forced failure:
#    - induce/observe a failed collector run (e.g. by the next naturally-failing cycle)
#    - confirm re-dispatch re-runs the step (executor log: no "all steps skipped")
#    - confirm fresh ## Analysis with {"status":"done"} + fetched/published counts
#    - confirm per-alert "Analyze Sentry issue *" files dated that day are created
#    - confirm trigger_count does not churn past the first failed attempt
```

## Desired Behavior

1. `agentStep.ShouldRun` returns `true` when the output section is absent (today's behavior).
2. `agentStep.ShouldRun` returns `true` when the output section is present but the task carries a failure marker — a `## Failure` section, or an output-section body that parses to an `AgentResult` whose `status` is `needs_input` or `failed`.
3. `agentStep.ShouldRun` returns `false` only when the output section is present and represents a genuine success (body parses to `status: done`, or unparseable prose) with no `## Failure` section.
4. `agentStep.Run` on a runner error returns `AgentStatusFailed` and does not write the output section (unchanged).
5. `agentStep.Run` on a runner return whose body is a `needs_input`/`failed` AgentResult returns that status and does not write the output section — the deliverer writes the `## Failure` marker on `Failed`/`NeedsInput` status as today (`delivery/content-generator.go:196-203`), so `Run` never leaves a success-looking `## Analysis` — consistent with AC1's re-run and AC4's no-write.
6. The `## Failure` marker used by `ShouldRun`/`Run` is the existing repo convention (`## Failure` heading produced by this repo's `delivery/content-generator.go` and `agent-task-executor`'s `pkg/result_publisher.go`) — no new section-name vocabulary.
7. Backward compatibility: agents that write prose sections (trade-analysis, pr-reviewer) keep today's semantics — an unparseable body with no `## Failure` marker still skips on re-dispatch.

## Constraints

- Fix lands in `bborbe/agent/claude/agent-step.go` (the lib that all `NewAgentStep` consumers use); no change to the `Step` interface or `StepRunner` semantics.
- The single-step idempotency intent is preserved: a genuine success section still skips (AC2). Do NOT remove idempotency — the bug is that failure looks like success, not that idempotency exists.
- Failure-marker detection must be cheap (in-memory markdown inspection, no I/O) — consistent with the `ShouldRun` interface doc "Guards must be cheap".
- Reuse the existing `## Failure` heading string; if a shared constant does not exist, introduce one in `claude/` (agent decides) — do not introduce a new section-name convention.
- Document the changed idempotency semantic in `docs/task-flow-and-failure-semantics.md` (the repo's durable doc that already references `## Failure`): a failure marker forces `ShouldRun` to re-run; absence of a success section forces a run; only a genuine success section skips. Specs die after implementation; the rule must survive in the doc.
- Parsing the output-section body as `AgentResult` is best-effort: unparseable bodies fall through to the success/skip path (prose agents unchanged).
- Do NOT change `agentTaskExecutor`/`agent-task-executor` behavior in this spec — the executor spawn cap and `deadline_exceeded` mapping are out of scope (separate concern; executor stays v0.6.4).
- No change to the collector's prompt or `scripts/sentry-create-tasks.sh` — the 08-28 `needs_input` was a one-off transient (tool-match denial disproven); this spec fixes the re-dispatch poison, not the transient.
- Ginkgo v2 / Gomega; counterfeiter mocks (`mocks/`); external test packages (`*_test`).
- `github.com/bborbe/errors.Wrapf` for wrapping; never `fmt.Errorf`, never bare `return err`.

## Failure Modes

| Trigger | Detection | Expected behavior | Recovery |
|---|---|---|---|
| Output-section body is unparseable prose (non-AgentStep JSON agent) | No `## Failure` section present | `ShouldRun` returns false (skip) — prose agents keep today's idempotency | None — behavior intentionally preserved (DB7) |
| Failed run wrote a `needs_input` body but no `## Failure` section | `ShouldRun` parses the body → status `needs_input` | `ShouldRun` true → step re-runs on re-dispatch | Re-dispatch re-invokes claude; a successful re-run overwrites the section |
| Executor wrote `## Failure` on job-level `deadline_exceeded` | `## Failure` section present | `ShouldRun` true → step re-runs | Re-dispatch re-invokes claude; fresh `## Analysis` on success |
| Runner error (crash, timeout) before any write | `Run` returns `AgentStatusFailed`, no section written | Section absent → next re-dispatch re-runs (today's behavior) | Executor retry / trigger_count path as today |
| Two concurrent re-dispatches both pass `ShouldRun` | Both invoke claude, both write the section | Last write wins (idempotent `ReplaceSection`); controller dedups per-alert files (`ErrTaskAlreadyExists`) | Non-destructive — controller dedup makes double fan-out benign |
| Body parses as `AgentResult` with an unknown status value | JSON parse succeeds, status not in {done, needs_input, failed} | Treat as success/skip (fall through), no new vocabulary | None — defensive; unknown status is not a failure marker |

## Suggested Decomposition

Single-layer change (one file + its tests) — one prompt is sufficient:

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `agentStep.ShouldRun` failure-marker detection + `Run` failure-status propagation + Ginkgo tests | 1-7 | 1-5 | — |

Rationale: both `ShouldRun` and `Run` must change coherently (AC1/AC2/AC4 are one contract — `ShouldRun` re-runs failures, `Run` doesn't write success-looking sections); splitting them would ship a half-fix. AC6 is the operator-side E2E, not a prompt.

## Do-Nothing Option

Cost: every future transient collector (or any single-AgentStep) failure becomes a permanent silent outage for that task family. The sentry backlog keeps accumulating untriaged alerts, the parent goal's prod-stability criterion can never be exercised, and the only recourse remains manual `## Analysis` clearing — which this session already had to do once. The `trigger_count` churn (now unbounded since controller v0.5.2 made caps opt-in) would keep re-dispatching into the void indefinitely.

## Open Questions

- **Resolved by AC1 (moot):** failure marker is BOTH the `## Failure` section AND a body-parsed `needs_input`/`failed` AgentResult — AC1's two named test rows (needs_input-body → true; `## Failure` section → true) already mandate both detection paths; the prompt-creator implements both, no re-derivation needed.
- Should `ShouldRun`/`Run` share one exported constant for the `## Failure` heading? **Recommend: yes, add a small shared constant in `claude/`** — prevents drift between the two detection sites. Bounded, local, reversible — agent decides at impl time.
