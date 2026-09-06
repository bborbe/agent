---
status: prompted
approved: "2026-09-06T15:59:19Z"
generating: "2026-09-06T15:59:20Z"
prompted: "2026-09-06T16:05:34Z"
branch: dark-factory/bug-kafka-result-stub-drops-target-vault
---

## Summary

- `delivery`'s `kafkaResultDeliverer` publishes task updates whose frontmatter is built from the **generated content only** — and `NewPassthroughContentGenerator.Generate` ignores `originalContent` entirely. On a failed/needs_input/unsupported-phase result (empty or body-only `Output`), the published message carries just status (+ phase/assignee overlays) — no `target_vault`.
- The task controller's routing guard (`ShouldProcessResult` in agent-task-controller) reads `target_vault` from the **result message's frontmatter**. Absent → legacy fall-through to `true` → the non-owning controller scans its own vault → file lives in another vault → `not_found` drop + `AgentControllerResultNotFound` alert.
- Observed 2026-09-06 on nuke dev: openclaw controller dropped 5 stub results (content length=106/107, frontmatter keys=1) for Personal-vault sentry-analyzer tasks; the same controller skipped the same tasks cleanly minutes later once a full echo (frontmatter keys=16, carrying `target_vault`) arrived. Zero data loss (owning controller wrote everything) — but the alert is noise that masks real drops.
- Fix: `kafkaResultDeliverer.DeliverResult` stamps `target_vault` into the published frontmatter from `originalContent`'s frontmatter when the generated content lacks it. One change, benefits every agent using the deliverer.

## Problem

The controller's cross-vault routing contract (agent-task-controller spec 044 / `routing.ShouldProcessResult`) assumes the result message carries `target_vault` — stamped at task create, echoed by the agent. The deliverer is the echo point: `kafkaResultDeliverer.DeliverResult` already receives `originalContent` (the full task markdown, `TASK_CONTENT` env, which includes `target_vault`) but the passthrough generator discards it, so the echo is partial — full results echo, stub results don't. A task update whose message omits `target_vault` is indistinguishable from a legacy unstamped task, and the guard's legacy fall-through routes it to every controller, producing the not_found noise the alert (bborbe/nuke #140) set out to eliminate.

## Reproduction

Smallest config that exhibits the bug: one sentry-analyzer (or any agent) task whose task file lives in a different vault than the controller, and an agent run that produces a failed/needs_input/unsupported-phase result with empty or body-only `Output`.

1. On nuke dev, with the sentry-analyzer fleet enabled, run a task such as `Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05.md` (Personal vault, frontmatter `target_vault: personal`) via its executor-spawned Job.
2. Force a stub result — e.g. the agent's Claude step fails (spec 051 failure path) or the pod launches with a phase the agent lacks (`unsupportedPhase` publishes `AgentResultInfo{Status, Message}` with no `Output`).
3. `kafkaResultDeliverer.DeliverResult` runs the passthrough generator: `applyStatusFrontmatter(result.Output, result)` over an empty/body-only `Output` → frontmatter = `status` only (1 key) → published `Task.Frontmatter` has no `target_vault`.
4. Both controllers consume the shared `develop-agent-task-v1-request` topic. `agent-task-controller-openclaw-0` deserializes the stub → `ShouldProcessResult` sees no `target_vault` → legacy fall-through returns `true` → `FindTaskFilePath` scans the openclaw vault → the file lives in the Personal vault → `task file not found ... after 3 attempts, skipping` at `result_writer.go:195` → `agent_controller_results_written_total{result="not_found"}` incremented → `AgentControllerResultNotFound` alert fires.

Observed evidence, verbatim (nuke dev, 2026-09-06, `kubectlnukedev -n dev logs agent-task-controller-openclaw-0`):

```
I0906 14:22:53.347680  task_result_executor.go:48] deserialized task e7068b26-6d98-5570-a40e-aaee2651b186 (content length=106, frontmatter keys=1)
I0906 14:23:16.071630  result_writer.go:195] task file not found for identifier e7068b26-... after 3 attempts, skipping
I0906 14:31:52.772191  task_result_executor.go:48] deserialized task 42cfdb6e-... (content length=107, frontmatter keys=1)
I0906 14:33:22.955525  result_writer.go:195] task file not found for identifier 42cfdb6e-... after 3 attempts, skipping
```

Same controller, full echo (frontmatter keys=16) — skipped cleanly, proving the guard works when the message carries `target_vault`:

```
I0906 14:34:20  task_result_executor.go:59] skipped vault mismatch target_vault="personal" vault="openclaw" task=42cfdb6e-...
```

dark-factory version: `dark-factory --version` (dev build, 2026-09-06).

## Goal

Every published result message — stub or full — carries the task's `target_vault` in its frontmatter, so the routing guard always decides ownership from the message and the non-owning controller skips instead of scanning-and-dropping.

## Desired Behavior

1. A stub result (failed / needs_input / unsupported-phase, empty or body-only `Output`) publishes a message whose frontmatter contains the task's `target_vault`, read from `originalContent`.
2. A full result whose generated content already carries `target_vault` publishes unchanged — the existing value is preserved, never overwritten, never duplicated.
3. When `originalContent` has no frontmatter or no `target_vault` (legacy task / direct CLI run), no `target_vault` key is added and nothing else changes.
4. All other frontmatter and body handling is unchanged — only the missing-`target_vault` case is stamped.

## Acceptance Criteria

- [ ] **AC1 — Stub result carries target_vault.** Driving `kafkaResultDeliverer.DeliverResult` with an `originalContent` whose frontmatter has `target_vault: personal` and a result whose `Output` is empty (failed status) → the published `Task.Frontmatter` contains `target_vault: personal`. Evidence: Ginkgo test row asserts `published.Frontmatter["target_vault"] == "personal"` — `make test` passes with the new row.
- [ ] **AC2 — Full result unchanged.** Same test setup with `Output` containing frontmatter incl. `target_vault` → the published frontmatter preserves the existing `target_vault` (no overwrite, no duplicate key). Evidence: Ginkgo test row asserts the original value present exactly once — `make test` passes with the new row.
- [ ] **AC3 — Missing originalContent leaves frontmatter untouched.** `originalContent` without frontmatter (or without `target_vault`) → published frontmatter gains no `target_vault` key and otherwise matches the pre-change shape. Evidence: Ginkgo test row asserts no `target_vault` key — `make test` passes with the new row.
- [ ] **AC4 — `make precommit` clean.** Evidence: `make precommit` exits 0.
- [ ] **Post-Deploy (Rung-2):** `AgentControllerResultNotFound` stops firing from routing misses across a full day of fleet traffic on nuke dev — evidence: `.claude/scripts/trading-get-alerts.sh nukedev` shows no `AgentControllerResultNotFound`, and `kubectlnukedev -n dev logs agent-task-controller-openclaw-0 --since=24h | grep -c "task file not found ... skipping"` returns 0.
  - `deploy_check:` `kubectlnukedev -n dev get statefulset agent-task-controller-openclaw -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `v0.7.5`

## Non-goals

- Changing `ShouldProcessResult` or the controller routing guard (works correctly when the message carries `target_vault`).
- Controller-side fallback that reads the file's stamped `target_vault` when the message lacks it — not viable for cross-vault: the non-owning controller's git-rest cannot read the other vault's files (that is exactly why the file miss happens).
- Changing the alert expression or threshold to hide the symptom.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Stub published while `originalContent` is empty or has no `target_vault` (legacy task / direct CLI run) | No panic, no bogus `target_vault` key | AC3 test guards; no operator action |
| Full echo that already carries `target_vault` | Not clobbered by a stale `originalContent` value; no duplicate key | AC2 test guards; no operator action |
| `TaskFrontmatter` map already holds `target_vault` | Stamp is a no-op (map write of same key) | AC2 test guards; no operator action |

## Constraints

- Kafka message schema otherwise untouched — only the missing-`target_vault` case is stamped.
- Legacy unstamped tasks keep routing as today (message without `target_vault` still falls through the guard — unchanged).
- Full-result echo behavior preserved exactly.
- No new config fields, flags, or thresholds.

## Verification

```bash
# unit + integration
cd ~/Documents/workspaces/agent && make precommit

# post-deploy (nuke dev, after mirrored-semver deploy)
cd ~/Documents/Obsidian/Personal && .claude/scripts/trading-get-alerts.sh nukedev   # expect: no AgentControllerResultNotFound
kubectlnukedev -n dev logs agent-task-controller-openclaw-0 --since=24h | grep -c "task file not found ... skipping"   # expect: 0
```

## Do-Nothing Option

Cost of not fixing: `AgentControllerResultNotFound` keeps firing on routine cross-vault stub results, triage spends time on known noise, and a real result drop is indistinguishable from it — the exact failure mode the alert (bborbe/nuke #140) was created to surface. The fix is one stamping operation with no config surface; do-nothing saves nothing.

## Workaround

None (alert is noise, not data loss; owning controller writes every result regardless).

## Open Questions

- None.
