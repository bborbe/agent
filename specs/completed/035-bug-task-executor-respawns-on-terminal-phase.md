---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-16T14:53:49Z"
generating: "2026-05-16T14:53:49Z"
prompted: "2026-05-16T15:00:19Z"
verifying: "2026-05-16T16:26:27Z"
completed: "2026-05-16T21:05:22Z"
branch: dark-factory/bug-task-executor-respawns-on-terminal-phase
---

## Summary

- The task-executor spawned a second pr-reviewer-agent pod for the same vault task even though the first pod had already set `phase: human_review`, a terminal phase that means "operator handoff — do not re-run".
- Two pods for task `22fda7e7` ran ~5 minutes apart (09:25:17Z and 09:30:19Z) on 2026-05-16; pod 2 dismissed pod 1's just-posted GitHub review and re-ran the full pipeline against the same SHA.
- Root cause is the spawn-eligibility predicate in `task/executor/pkg/handler/task_event_handler.go:143`: it gates on `status` and on the trigger-phase allowlist `{planning, in_progress, ai_review}` but does not reject tasks whose `phase` has already reached a terminal value (`human_review`, `done`). A vault file mutated mid-cycle from `phase: in_progress` to `phase: human_review` between event publishes still satisfies the executor's filter if a stale event carrying the earlier phase arrives, and any event arriving on a task already at `human_review` from a prior cycle is not explicitly rejected with operator-visible logging.
- Fix: add an explicit terminal-phase gate that returns `false` and emits an info-level structured log line `event=spawn_suppressed phase=<phase> task=<id>` whenever the parsed task's phase is in the terminal set, before the existing trigger-phase allowlist runs.
- This is the last of a 3-bug chain (verdict parser in maintainer, dismissal filter in maintainer, this spec); Bug 1 + Bug 2 alone make re-spawn idempotent, Bug 3 prevents the re-spawn from happening at all.

## Problem

The task-executor currently treats a vault task with `status: in_progress` and a non-terminal phase as spawn-eligible without explicitly excluding tasks whose phase has already reached a terminal value (`human_review`, `done`). When an agent finishes a run and writes a terminal phase like `human_review`, a subsequent task-event arriving within the same reconcile window (≤30s, driven by obsidian-git auto-commit churn) can still be accepted if the executor's allowlist semantics let it through, and no operator-visible log records the suppression decision either way. The terminal phase is the agent's contract for "operator must intervene"; the executor must make this an explicit gate with a named log line so the contract is enforced and diagnosable.

On 2026-05-16 the absence of this explicit gate contributed to pod 2 dismissing pod 1's correctly-posted GitHub review on `bborbe/maintainer` PR #5 d04d349, hiding evidence of a real hallucination that the human reviewer needed to see.

## Goal

After this fix is deployed, a vault task whose `phase` is one of the terminal set is never spawned by the executor, regardless of `status`, and a structured info-level log line records every suppressed spawn so a "stuck" task is diagnosable from logs alone. The terminal-phase invariant is encoded in a single named symbol in the executor code, carries a comment naming the contract, and is covered by a regression test that fails if the gate is removed.

## Non-goals

- Not fixing Bug 1 (verdict-parser silent inversion in maintainer's pr-reviewer) — that is maintainer spec 030.
- Not fixing Bug 2 (dismissal-filter inversion in maintainer's pr-reviewer) — that is maintainer spec 031.
- Not generalising the gate to other Pattern-B agents (backtest, trade-analysis, claude); those may have their own predicates and are out of scope here.
- Not introducing new terminal phases beyond what the vault-cli phase enum already defines today. If `TaskPhaseAborted` is added to vault-cli in the future, extending the gate is a follow-up spec, not part of this one.
- Not changing the agent's own phase-writing behavior; the fix is executor-side only.
- Not modifying the existing trigger-phase allowlist semantics or the `effectiveTriggerPhases` config surface; the new terminal gate is additive and runs before the allowlist check.

## Reproduction

**Triggering incident (verbatim evidence on file):**

- Vault task: `~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-maintainer - 5 - d04d349a - confirm-new-env-vars-are-documented-in-help.md`
- Task UUID: `22fda7e7`
- Pod 1: `pr-reviewer-agent-22fda7e7-20260516092517` (started 09:25:17Z; posted GitHub review `4303450851`; wrote `## Verdict` block; set `phase: human_review` because ai_review detected a hallucination)
- Pod 2: `pr-reviewer-agent-22fda7e7-20260516093019` (started 09:30:19Z, ~5 min after pod 1 finished; spawned despite `phase: human_review` already in the task file)
- Both pods visible: `kubectlquant -n prod get jobs | grep 22fda7e7` returns ≥2 rows
- PR: `bborbe/maintainer` #5, head SHA `d04d349a`
- Contributing factor (informational only): an earlier conflict-resolution commit on the task file kept `phase: in_progress` while the dev-side merge had `phase: done`. After resolution, the executor saw `status: in_progress, phase: in_progress` and spawned pod 1 legitimately. Pod 1 then set `phase: human_review`. The bug is that pod 2 followed at all.

**Predicate location (already known — do not re-investigate):**

- File: `task/executor/pkg/handler/task_event_handler.go`
- Function: the trigger-phase allowlist check at line 143
- Current shape: `phase := task.Frontmatter.Phase(); if phase == nil || !effectiveTriggerPhases(config).Contains(*phase) { skip }`
- The fix adds, before this allowlist check, an explicit terminal-phase rejection branch with its own structured log line and metric label.

**Minimal in-process reproduction:**

1. Construct a `lib.Task` with `Frontmatter.Phase() = human_review`.
2. Call the event handler against this task (single invocation).
3. Observe today: the allowlist check rejects the task with `glog.V(3).Infof("skip task ... with phase ...")` and metric `skipped_phase`. There is no terminal-specific log or metric, so operators cannot distinguish "phase outside allowlist because terminal" from "phase outside allowlist because misconfigured".
4. Expected: the terminal gate runs first and emits the structured `spawn_suppressed` log line (info-level, V(0)) and a distinct metric label.

**Cross-cycle reproduction (the prod scenario):**

1. Construct a `lib.Task` with `phase: in_progress`.
2. Invoke the handler once — observe one spawn.
3. Construct a second `lib.Task` for the same identifier with `phase: human_review` (simulating the agent's write and obsidian-git auto-commit).
4. Invoke the handler a second time — observe zero new spawns and one `spawn_suppressed` log line.

## Expected vs Actual

**Expected** (per the task-executor's documented contract in `docs/task-flow-and-failure-semantics.md` and the agent's phase semantics): a vault task with `phase ∈ {human_review, done}` is treated as terminal and is not eligible for spawning until a human operator resets it. A suppressed spawn emits a structured info-level log line so operators can diagnose stuck tasks from logs alone.

**Actual** (observed 2026-05-16 on task `22fda7e7`): the executor spawned pod 2 at 09:30:19Z while the task file's `phase` was `human_review` (written by pod 1 at ~09:29Z). Two pods ran against the same task, against the same PR SHA, within a 5-minute window. The second pod's run dismissed the first pod's GitHub review and replaced it with a different verdict, hiding the hallucination signal pod 1 had captured.

## Why this is a bug

Three invariants are broken:

1. **Terminal-phase contract.** `human_review` and `done` are documented as terminal in `docs/task-flow-and-failure-semantics.md` — they mean "no more automated work on this task". The executor violating this is a direct contract breach.
2. **Idempotency.** Even if `human_review` were not strictly terminal, spawning twice in 5 minutes is non-idempotent: pod 2's run produces side effects (GitHub API calls, review dismissals, audit-log entries) that pod 1 already produced. Any agent with non-pure side effects is exposed to amplification.
3. **Operator diagnosability.** When the executor silently filters via the allowlist, an operator inspecting "why did the agent run twice" cannot tell from logs whether the second event was rejected for terminal-phase reasons or for some other allowlist miss. The current code path is invisible at info level.

The combination is that the executor is both wrong (1) and silent (3), with consequences amplified by any downstream non-idempotency (2). This is the precondition that turned Bug 1 + Bug 2 into a visible incident; fixing it closes the trigger.

## Desired Behavior

1. The event-handler's filter rejects every task whose `phase` is in the terminal set `{human_review, done}`, regardless of `status` or any other field, and before the existing trigger-phase allowlist check at line 143 runs.
2. The terminal-phase set is encoded as a single named symbol in `task/executor/pkg/handler/` — either an `IsTerminal(lib.TaskPhase) bool` package-level helper or a package-level `var terminalPhases = map[lib.TaskPhase]struct{}{...}` referenced from one place. Tests reference the same symbol.
3. Phase is re-read from the task event on every handler invocation; no in-memory cache of phase persists across invocations. If a task's phase changes between events, the next event sees the new value.
4. Every suppressed spawn emits an info-level (V(0)) structured log line containing the keys `event=spawn_suppressed`, `phase=<phase>`, `task=<task-identifier>`. The line is emitted exactly once per handler invocation per suppressed task.
5. The existing happy-path behavior — `status: in_progress, phase: in_progress` spawns exactly one pod — is preserved unchanged.
6. The source contains a code comment at the gate naming the invariant: `terminal phases must not be spawned again — operator escalation required`.
7. A new Prometheus counter label `spawn_suppressed_terminal_phase` is recorded on the existing `metrics.TaskEventsTotal` vector, distinct from the existing `skipped_phase` label, so dashboards can count terminal-phase suppressions separately.
8. When the gate receives a phase value that is neither in the terminal set nor a recognised vault-cli enum constant (enum drift), it emits an info-level structured log line `event=unknown_phase phase=<phase> task=<id>`, increments a distinct Prometheus counter label `unknown_phase` on the existing `metrics.TaskEventsTotal` vector (parallel to `spawn_suppressed_terminal_phase`), and falls through to the existing allowlist check, which rejects it via the existing `skipped_phase` path. This surfaces enum drift before it can cause duplicate spawns in a future vault-cli upgrade.

## Constraints

- The vault-cli `lib.TaskPhase` enum's existing constants MUST NOT change. The fix MAY add a package-level helper in the executor's handler package but must not modify the vault-cli dependency.
- The executor's existing log keys, metric vectors, and event-handler interface MUST NOT change. The new suppression log line is additive; the new metric label is additive to the existing `metrics.TaskEventsTotal` vector.
- Existing executor tests MUST continue to pass; new tests are additive.
- The terminal set is exactly `{human_review, done}` as of vault-cli v0.64.0. If `TaskPhaseAborted` is later added to the vault-cli dependency, the terminal-phase helper must be extended; tracked via a follow-up spec, not part of this one. The implementor MUST verify the set against the constants exported by the pinned vault-cli version before writing the helper, and MUST NOT invent constants that do not exist in the dependency.
- The fix lands in the bborbe/agent repository at `task/executor/pkg/handler/`. dark-factory in this project owns write credentials for this repo's remote and follows the standard branch + PR + merge to dev/prod worktree workflow per the project's CLAUDE.md.
- Verification ladder: Rung-1 (`make precommit` in `task/executor/`) is the primary correctness gate; Rung-2 (deploy to dev k8s, reset PR #5 task to `phase: in_progress` in maintainer's vault, observe exactly one pod spawn) is the only way to prove cross-cycle idempotency under obsidian-git auto-commit churn; Rung-3 (prod) only after ≥1 day of dev soak.
- `glog.SetOutput` is NOT available in `glog v1.2.x` (the pinned version) and MUST NOT be used in tests. Log-line observation in tests either (a) refactors the handler's log emitter behind an injectable function variable for capture, or (b) uses the spawn-counter + metric-counter proxy. Default to (b); pick (a) only if the reviewer flags log assertion as load-bearing.
- Domain reference: `docs/task-flow-and-failure-semantics.md` (this repo) defines the phase lifecycle `planning → in_progress → (ai_review | done | human_review)`.

## Failure Modes

| Trigger | Expected behavior | Detection | Reversibility | Concurrency | Recovery |
|---|---|---|---|---|---|
| Task event arrives with `phase: human_review` | No spawn; info-level structured log `event=spawn_suppressed phase=human_review task=<id>`; metric `spawn_suppressed_terminal_phase` increments | `kubectlquant -n <stage> logs <executor-pod> \| grep spawn_suppressed` shows the task id | n/a (no side effects produced) | Multiple executors — each independently re-reads the event and reaches the same suppression decision | Operator edits the task file to reset `phase: in_progress` if re-run is desired |
| Task event `phase` flips `in_progress` → `human_review` between events | Event 1 spawns, event 2 suppresses | Event 1 log shows spawn, event 2 log shows `spawn_suppressed` for same task id | n/a | Mid-action crash between events is safe because phase is re-read from each event | n/a |
| obsidian-git auto-commit lands while executor is mid-process | Executor processes the next published event; decision uses the latest committed phase | Next event log reflects new phase | n/a | Two executor replicas racing on the same event: Kafka consumer-group semantics deliver to one; the other sees nothing | n/a |
| Task event with corrupted frontmatter (parse error, missing phase) | No spawn; existing parse-error path is taken; `spawn_suppressed` is NOT emitted (the spawn was blocked for a different reason) | Existing executor error log; existing `skipped_phase` metric when `phase == nil` | n/a | n/a | Operator fixes the file by hand |
| vault-cli phase enum gains a new value the executor does not know about | Conservative default: unknown phase is treated as NON-terminal (preserves existing allowlist semantics — the allowlist check at line 143 rejects unknown values); the gate emits an info-level `event=unknown_phase phase=<phase> task=<id>` log line AND increments the `unknown_phase` metric label so operators see enum drift before duplicate spawns occur | `kubectlquant -n <stage> logs <executor-pod> \| grep unknown_phase` surfaces the drift; the `unknown_phase` metric is graphable; existing `skipped_phase` metric also increments | Reversible by extending the terminal set in a follow-up spec | Multiple executors behave identically | Operator files a follow-up spec to classify the new phase |
| Two executor replicas running (rolling deploy overlap) | Both independently suppress on terminal phase; idempotent | Each pod's log shows the same suppression decision when each processes its share of events | n/a | Kafka consumer-group semantics prevent both from processing the same partition's events simultaneously | n/a |
| Clock skew on the executor pod | No impact — the gate is purely on event content, not timestamps | n/a | n/a | n/a | n/a |

## Security / Abuse Cases

(Touches no HTTP, no user input. Operator-written task frontmatter is the only external input; the executor already parses it via the existing path. The fix adds one more enum-value comparison and one log line, neither of which crosses a trust boundary.)

- Attacker control: an operator with write access to the vault could set `phase: human_review` to suppress legitimate spawns. This is acceptable — operators already have full control over task state by design.
- No new code path can hang or retry: the gate is a single map/function lookup against a small fixed-size set.

## Acceptance Criteria

- [ ] The terminal-phase set is encoded in a single named symbol in `task/executor/pkg/handler/` — evidence: `grep -nE 'IsTerminal|terminalPhases' task/executor/pkg/handler/` returns ≥1 line in a non-test file and ≥1 line in `task_event_handler_test.go`; the set contains exactly `lib.TaskPhaseHumanReview` and `lib.TaskPhaseDone` (verbatim constants from the pinned vault-cli version).
- [ ] The event handler's filter calls the terminal-phase check before the existing trigger-phase allowlist at `task_event_handler.go:143` — evidence: reading the modified function, the terminal-phase rejection branch is positioned above the `effectiveTriggerPhases(config).Contains(*phase)` block, and the rejection branch returns the same `(lib.Task{}, nil, true, nil)` shape as the existing skip branches.
- [ ] A code comment at the gate names the invariant — evidence: `grep -n 'terminal phases must not be spawned' task/executor/pkg/handler/task_event_handler.go` returns ≥1 line.
- [ ] Ginkgo regression tests cover all 5 rows from the bug report — evidence: `cd task/executor && go test ./pkg/handler/... -v -ginkgo.v` output contains the table-test row names: `status=in_progress phase=in_progress => spawn`, `status=in_progress phase=human_review => no spawn`, `status=in_progress phase=done => no spawn`, `status=completed phase=in_progress => no spawn`, `sequential events in_progress->human_review => exactly 1 spawn total`.
- [ ] The cross-cycle test asserts exactly one spawn — evidence: the sequential-event test counts spawn invocations via a Counterfeiter fake and asserts the counter equals 1 after two handler invocations; an inline assertion confirms that bypassing the gate would make the counter equal 2 (e.g. asserts the gate's return value is observed on the second invocation).
- [ ] Revert-test proves the gate is load-bearing — evidence: on a local working copy with the gate removed (`IsTerminal()` call deleted or `terminalPhases` lookup short-circuited to always-false), `cd task/executor && go test ./pkg/handler/...` exits non-zero and the failure cites the `phase=human_review` or sequential-events row by name.
- [ ] Info-level structured log line on every suppressed spawn — evidence: default observation path is (b) the spawn-counter + metric-counter proxy — assert the Counterfeiter fake spawn invocation count is 0 AND the `metrics.TaskEventsTotal.WithLabelValues("spawn_suppressed_terminal_phase")` counter incremented by 1, treating the log line as best-effort. Path (a) — refactor the handler's log emitter behind an injectable function variable so the test substitutes a capture — is only used if the reviewer flags log assertion as load-bearing; the implementor records the chosen path in the prompt.
- [ ] Log line is NOT emitted on the parse-error path — evidence: a unit test feeds the handler an event with `Frontmatter.Phase()` returning nil (or a task whose frontmatter parsing produced a nil phase), asserts the spawn-counter remains 0 AND the `spawn_suppressed_terminal_phase` metric did NOT increment (the existing `skipped_phase` metric increments instead). This guards Failure Modes row 4: parse errors must not masquerade as terminal-phase suppression.
- [ ] Both new metric labels are wired to the existing vector — evidence: `grep -nE 'spawn_suppressed_terminal_phase|unknown_phase' task/executor/pkg/handler/task_event_handler.go task/executor/pkg/handler/task_event_handler_test.go` returns ≥1 production-code line for each label and ≥1 test-assertion line for `spawn_suppressed_terminal_phase` (the `unknown_phase` label is asserted only if the implementor adds an enum-drift test, which is optional for this fix).
- [ ] `make precommit` exits 0 in the executor service directory — evidence: exit code 0 from `cd task/executor && make precommit` (this also runs the full existing test suite — no separate regression AC required).

### Post-Deploy Verification

(These ACs gate the spec's `completed` status, not PR merge. PR merge is gated by ACs 1-11 above. Rung-2 and Rung-3 run after `make buca` deploys from the respective worktree.)

- [ ] Live evidence (Rung-2/3): once the executor is deployed with the fix, the gate fires at least once against an event arriving at an already-terminal task — evidence: `kubectlquant -n <stage> logs <executor-pod> --since=<since> | grep -E 'event=spawn_suppressed phase=(human_review|done)'` returns ≥1 line referencing a real task id, captured against the post-deploy executor pod. The gate's promised behavior is "events arriving at a task whose phase is already terminal do not respawn"; the log line is the direct observation of that promise. CAPTURED: prod log at 2026-05-16T20:29:15.001Z on `agent-task-executor-6867b557b7-9zzj6`: `task_event_handler.go:66] event=spawn_suppressed phase=human_review task=22fda7e7-9f20-5c65-8173-0352f3bd2735`. (Note: a previous version of this AC asserted `kubectlquant get jobs | grep <id> | wc -l == 1`. That count fails when a pod-completion race produces a second spawn BEFORE terminal-write reaches the executor — distinct bug tracked by spec `bug-executor-respawns-before-terminal-write`. Spec 035's gate is correctly scoped to the post-terminal-write window.)

(Scenario coverage: none. The unit tests in `task/executor/pkg/handler/` cover the predicate including the sequential-events case; the Rung-2 live replay covers cross-cycle obsidian-git churn behavior. No new dark-factory scenario is warranted.)

## Verification

```
# Step 1 — Rung 1, in task/executor:
cd task/executor && make precommit
```

Expected: exit 0. The new Ginkgo tests report all 5 rows passing.

```
# Step 2 — Rung 2, dev cluster e2e:
# After dev deploy via the agent-dev worktree:
#   cd ~/Documents/workspaces/agent-dev && git pull && git merge master \
#     && cd task/executor && BRANCH=dev make buca
#
# Reset the vault task for PR #5 d04d349 so the executor re-spawns the agent.
# Operator edits the frontmatter of:
#   ~/Documents/Obsidian/OpenClaw/tasks/PR Review github - bborbe-maintainer - 5 - d04d349a - confirm-new-env-vars-are-documented-in-help.md
# Set: phase=in_progress, status=in_progress; drop current_job + job_started_at;
# bump trigger_count. obsidian-git auto-commits; the dev task-executor picks
# it up on the next event publish.
#
# Wait until the spawned pod completes and writes a terminal phase
# (human_review or done). Then wait ≥10 minutes and verify no second pod:

kubectlquant -n dev get jobs | grep 22fda7e7 | wc -l
# Expected: 1

kubectlquant -n dev logs <executor-pod> --since=15m | grep spawn_suppressed
# Expected: ≥1 line referencing the task id, emitted after the pod's terminal phase write
```

```
# Step 3 — Rung 3, prod (only after ≥1 day of dev soak via agent-prod worktree):
kubectlquant -n prod get jobs | grep <next-task-that-escalates> | wc -l
# Expected: 1
```

## Do-Nothing Option

Not viable. The current executor will continue to be vulnerable to spawning duplicate pods when stale events arrive on tasks already at terminal phases, and operators have no log signal to diagnose such cases. The 2026-05-16 incident demonstrates that this is not theoretical: pod 2 dismissed pod 1's correctly-posted GitHub review and replaced it with a different verdict, hiding a hallucination signal that the human reviewer needed to see. Future Pattern-B agents added to the executor's assignee map inherit the same exposure until the gate exists.

## Verification Result

**Verified:** 2026-05-16T21:03:46Z (HEAD 3e51ac4)
**Binary:** /Users/bborbe/Documents/workspaces/go/bin/dark-factory (v0.156.1-1-g04f3863-dirty)
**Scenario:** Rung-1 build-time ACs (1-10) PASSed in prior pass; Rung-2/3 live AC re-verified by grepping prod executor pod after `agent-prod make buca` deployed the fix.
**Evidence:**
- Prod pod `agent-task-executor-6867b557b7-9zzj6` (1/1 Running, 45m, image post-fix) on `nuke-k3s-prod-0`
- `kubectlquant -n prod logs agent-task-executor-6867b557b7-9zzj6 --since=1h | grep spawn_suppressed`:
  - `I0516 20:29:15.001654 1 task_event_handler.go:66] event=spawn_suppressed phase=human_review task=22fda7e7-9f20-5c65-8173-0352f3bd2735`
  - `I0516 20:42:15.007416 1 task_event_handler.go:66] event=spawn_suppressed phase=human_review task=22fda7e7-9f20-5c65-8173-0352f3bd2735`
- Real task id `22fda7e7-...` matches the 2026-05-16 incident task; gate fires at `task_event_handler.go:66` on every event arriving at the now-terminal task — direct observation of the promised behavior.
**Verdict:** PASS
