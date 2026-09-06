---
status: draft
kind: bug
---

# Kafka result stubs drop target_vault, tripping cross-vault not_found drops

## Summary

- `delivery`'s `kafkaResultDeliverer` publishes task updates whose frontmatter is built from the **generated content only** — and `NewPassthroughContentGenerator.Generate` ignores `originalContent` entirely. On a failed/needs_input/unsupported-phase result (empty or body-only `Output`), the published message carries just status (+ phase/assignee overlays) — no `target_vault`.
- The task controller's routing guard (`ShouldProcessResult` in agent-task-controller) reads `target_vault` from the **result message's frontmatter**. Absent → legacy fall-through to `true` → the non-owning controller scans its own vault → file lives in another vault → `not_found` drop + `AgentControllerResultNotFound` alert.
- Observed 2026-09-06 on nuke dev: openclaw controller dropped 5 stub results (content length=106/107, frontmatter keys=1) for Personal-vault sentry-analyzer tasks; the same controller skipped the same tasks cleanly minutes later once a full echo (frontmatter keys=16, carrying `target_vault`) arrived. Zero data loss (owning controller wrote everything) — but the alert is noise that masks real drops.
- Fix: `kafkaResultDeliverer.DeliverResult` stamps `target_vault` into the published frontmatter from `originalContent`'s frontmatter when the generated content lacks it. One change, benefits every agent using the deliverer.

## Reproduction

Deploy a sentry-analyzer (or any agent) task whose file lives in a different vault than the controller, then force a stub result (fail the Claude step, or launch with a phase the agent lacks):

1. Task `Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05.md` (Personal vault, `target_vault: personal`) runs `sentry-issue-analyzer` (or deep analyzer) on nuke dev.
2. Agent publishes a failed/needs_input/unsupported-phase result with empty or body-only `Output` — e.g. `unsupportedPhase` publishes `AgentResultInfo{Status, Message}` with no Output.
3. `kafkaResultDeliverer.DeliverResult`: passthrough generator returns `applyStatusFrontmatter(result.Output, result)` → frontmatter = just `status` (1 key); `ParseMarkdownFrontmatter` yields 1 key; published `Task.Frontmatter` has no `target_vault`.
4. Controller `agent-task-controller-openclaw-0` deserializes → `ShouldProcessResult` sees no `target_vault` → returns `true` → `FindTaskFilePath` scans the openclaw vault → file is in the Personal vault → `task file not found ... after 3 attempts, skipping` at `result_writer.go:195`, `agent_controller_results_written_total{result="not_found"}` incremented.

Verbatim evidence (nuke dev, 2026-09-06, `kubectlnukedev -n dev logs agent-task-controller-openclaw-0`):

```
I0906 14:22:53.347680  task_result_executor.go:48] deserialized task e7068b26-6d98-5570-a40e-aaee2651b186 (content length=106, frontmatter keys=1)
I0906 14:23:16.071630  result_writer.go:195] task file not found for identifier e7068b26-... after 3 attempts, skipping
I0906 14:31:52.772191  task_result_executor.go:48] deserialized task 42cfdb6e-... (content length=107, frontmatter keys=1)
I0906 14:33:22.955525  result_writer.go:195] task file not found for identifier 42cfdb6e-... after 3 attempts, skipping
```

Same controller, full echo (frontmatter keys=16) — skipped cleanly:

```
I0906 14:34:20  task_result_executor.go:59] skipped vault mismatch target_vault="personal" vault="openclaw" task=42cfdb6e-...
```

## Expected vs Actual

| Expected | Actual |
|---|---|
| Every task update the controller receives carries the task's `target_vault`, so the routing guard always decides ownership from the message. | Stub results (failed / needs_input / unsupported-phase, empty Output) publish with 1 frontmatter key — no `target_vault` — so the guard falls through to `true` and every non-owning controller scans + drops. |
| Non-owning controller never increments `result="not_found"` for a file that exists in another vault. | 5 such drops on nuke dev 2026-09-06 → `AgentControllerResultNotFound` alert. |
| Agent's published message preserves the vault identity the task was created under (the deliverer already holds it as `originalContent`). | `NewPassthroughContentGenerator.Generate` discards `originalContent` (`Generate(_ context.Context, _ string, result)`), so stub messages carry no vault identity at all. |

## Why this is a bug

The controller's cross-vault routing contract (agent-task-controller spec 044 / `routing.ShouldProcessResult`) assumes the result message carries `target_vault` — stamped at task create, echoed by the agent. The deliverer is the echo point: `kafkaResultDeliverer.DeliverResult` already receives `originalContent` (the full task markdown, `TASK_CONTENT` env, which includes `target_vault`) but the passthrough generator discards it, so the echo is partial — full results echo, stub results don't. A task update whose message omits `target_vault` is indistinguishable from a legacy unstamped task, and the guard's legacy fall-through routes it to every controller, producing the not_found noise the alert (bborbe/nuke #140) set out to eliminate.

## Goal

`kafkaResultDeliverer.DeliverResult` stamps `target_vault` into the published frontmatter whenever the generated content's frontmatter lacks it, reading the value from `originalContent`'s frontmatter. Every published result — stub or full — then carries vault identity, so `ShouldProcessResult` decides ownership from the message and the non-owning controller skips instead of dropping.

## Acceptance Criteria

- [ ] **AC1 — Stub result carries target_vault.** Ginkgo test in `delivery/` driving `kafkaResultDeliverer.DeliverResult` with an `originalContent` whose frontmatter has `target_vault: personal` and a result whose `Output` is empty (failed status) → the published `Task.Frontmatter` contains `target_vault: personal`. Evidence: test row in `make test`.
- [ ] **AC2 — Full result unchanged.** Same test setup with `Output` containing frontmatter incl. `target_vault` → the existing value is preserved (no overwrite, no duplicate). Evidence: test row in `make test`.
- [ ] **AC3 — Missing originalContent leaves frontmatter untouched.** `originalContent` without frontmatter (or without `target_vault`) → no `target_vault` key added, nothing else changes. Evidence: test row in `make test`.
- [ ] **AC4 — `make precommit` clean.** Evidence: `cd ~/Documents/workspaces/agent && make precommit` exits 0.
- [ ] **AC5 — Post-deploy (nuke dev):** `AgentControllerResultNotFound` stops firing from routing misses across a full day of fleet traffic — `.claude/scripts/trading-get-alerts.sh nukedev` shows no `AgentControllerResultNotFound`, and `kubectlnukedev -n dev logs agent-task-controller-openclaw-0 --since=24h | grep -c "task file not found ... skipping"` returns 0.
  - `deploy_check:` controller image on nuke dev carries the agent lib release containing this fix (mirrored-semver path: release → hand-build/upload image → bump pins in `nuke/agent/` → `BRANCH=dev KUBECONFIG=~/.kube/nuke-dev make apply`)

## Non-goals

- Changing `ShouldProcessResult` or the controller routing guard (works correctly when the message carries `target_vault`).
- Controller-side fallback that reads the file's stamped `target_vault` when the message lacks it — not viable for cross-vault: the non-owning controller's git-rest cannot read the other vault's files (that is exactly why the file miss happens).
- Changing the alert expression or threshold to hide the symptom.

## Failure modes the fix MUST cover

- A stub published while `originalContent` is empty or has no `target_vault` (legacy task / direct CLI run) — no panic, no bogus key.
- A full echo that already carries `target_vault` — must not be clobbered by a stale `originalContent` value.
- Duplicate keys — `TaskFrontmatter` is a map; stamping must not create `target_vault` twice.

## Workaround

None (alert is noise, not data loss; owning controller writes every result regardless).

## Open Questions

- None.
