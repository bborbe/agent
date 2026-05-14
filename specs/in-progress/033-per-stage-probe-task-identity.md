---
status: generating
tags:
    - dark-factory
    - spec
approved: "2026-05-14T13:00:07Z"
generating: "2026-05-14T13:00:08Z"
branch: dark-factory/per-stage-probe-task-identity
---

## Summary

- The executor's probe runner emits stage-less task files at a shared vault path, so dev and prod overwrite each other's probe.
- Probe tasks must be per-stage: per-stage filename, per-stage stable UUID, and `stage:` frontmatter matching the executor's branch.
- The probe runner must reset `status` and `phase` to `in_progress` on every cycle so a completed prior probe does not block re-spawn.
- No executor-side filter logic changes; the executor's existing stage and status filters already do the right thing once the publisher emits the correct fields.
- Cleanup of stale shared probe files in the OpenClaw vault and the `oauth-probe` → `healthcheck` rename are explicitly out of scope (handled by separate work).

## Problem

The OpenClaw vault is one logical filesystem with three clones (host Mac, dev cluster, prod cluster). The executor's probe runner today writes a single task file `tasks/probe-<agent>.md` with no `stage:` frontmatter and a UUIDv5 keyed only on the agent name. As a result, the dev and prod clusters publish to the same vault path with the same task identifier, race each other to write `status: completed`, and — because the executor's stage filter at `task_event_handler.go:150` silently skips tasks whose `stage` field does not match the executor's branch — both clusters drop the probe on the floor. Today's verified consequence is that the dev pushgateway has zero `agent_job_*` rows from probe runs. The probe is functionally dead in both clusters.

## Goal

After this work, each executor cluster (dev and prod) publishes its own probe task per agent that is independent of the other cluster's probe. The two probes share neither vault path, nor task identifier, nor frontmatter row. The executor's existing stage filter routes each cluster's probe only to that cluster, and each cluster's pushgateway receives `agent_job_*` rows for every probe cycle.

## Non-goals

- Cleaning up the existing stale `tasks/probe-<agent>.md` files in the OpenClaw vault. One-time operator action after deploy.
- Renaming `oauth-probe` to `healthcheck`. Tracked by spec 032 separately.
- Removing the `TaskTypeOAuthProbe` constant from `lib/`. Tracked under spec 032's follow-up.
- Changing the executor's stage filter, status filter, or task-type filter. The bug is the publisher, not the filter.
- Introducing new cron expressions, new CRDs, new HTTP routes, or any new configuration surface. The probe still runs on the existing cron and HTTP trigger.

## Desired Behavior

1. The probe runner writes a per-stage vault file. For agent `claude-agent` running in the `dev` executor, the file is `tasks/probe-claude-agent-dev.md`. For `prod`, `tasks/probe-claude-agent-prod.md`.
2. The task identifier is a UUIDv5 derived from the tuple `(agent_name, stage)`. The same `(agent, stage)` pair yields the same UUID across restarts; different pairs yield different UUIDs. The dev and prod probes for the same agent therefore never collide on the task-identifier index.
3. The published frontmatter includes `stage: <dev|prod>` whose value equals the executor's existing `--branch` / `BRANCH` argument.
4. On every probe cycle, the runner publishes `status: in_progress` and `phase: in_progress` regardless of the prior cycle's terminal state. A completed prior probe does not block the next cycle from being picked up by the executor.
5. The UUIDv5 namespace constant used to derive probe identifiers is documented in code with a comment stating it is frozen and must not be changed without a migration plan. Whether the existing namespace is reused or a new one is chosen is an implementation choice; the constraint is that the chosen value is stable across runs and across restarts.
6. The executor's task event handler is unchanged. The stage filter at `task_event_handler.go:150` still skips on stage mismatch — but now the publisher emits the correct stage, so each cluster's executor accepts only its own probe.

## Constraints

- **Executor's task event handler is frozen.** The stage filter, status filter, type filter, assignee filter, and phase filter logic must remain as-is. This spec changes only what the probe runner publishes.
- **The probe runner's UUIDv5 namespace constant, once chosen, is frozen** and documented with a do-not-change comment. Changing it later would orphan in-flight probe tasks.
- **The default cron expression and HTTP trigger route are unchanged.** This spec does not touch scheduling or routing.
- **No new env vars, no new CLI flags.** Stage is sourced from the executor's existing `Branch` argument.
- **Task type literal is unchanged by this spec.** It stays `oauth-probe` until spec 032 ships. If 032 ships first, the task type is `healthcheck` — the per-stage identity work is orthogonal to the rename and must not couple to it.
- See `docs/task-flow-and-failure-semantics.md` for the executor's filter semantics referenced in failure modes.
- The OpenClaw vault topology (one GitHub origin, three clones with shared paths) is documented in `[[OpenClaw Vault Sync Architecture]]` in the Personal Obsidian vault — that document is the authoritative source for why per-stage paths are required.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Executor is started without a `Branch` value | Existing required-arg validation rejects startup | Operator supplies `BRANCH=dev` or `BRANCH=prod` |
| `Branch` is a value other than `dev` or `prod` (e.g. `live`, `develop`) | The runner publishes that literal value as `stage:` and as the filename suffix; executor's stage filter then matches as it does today | None needed — `stage` is opaque to the runner; matching is the executor's existing concern |
| Stale `tasks/probe-<agent>.md` (no stage suffix) exists in the vault from before this spec ships | The executor's stage filter rejects it (empty `stage` ≠ branch); new per-stage files coexist alongside the stale one | Operator deletes stale files once after deploy: `git rm tasks/probe-*.md` on the host clone, then push |
| Prior probe cycle completed with `status: done` | New cycle overwrites `status: in_progress` and `phase: in_progress` in frontmatter under the same identifier; executor picks it up on next event | Automatic — the publisher resets both fields every cycle |

## Security / Abuse Cases

- The probe runner does not accept external input. Its inputs are the executor's own `Branch` arg and the cluster's own Config CRs.
- Filename construction concatenates `agent_name` and `stage`. The agent name comes from a Config CR (cluster-scoped, operator-controlled, alphanumeric in practice); the stage comes from the executor's required `Branch` arg. Neither is an attacker-controlled string. No path traversal surface is introduced.
- No new HTTP surface, no new env, no new trust boundary.

## Acceptance Criteria

- [ ] In a dev executor (`BRANCH=dev`), the probe runner publishes a task with filename ending `-dev.md` and frontmatter `stage: dev`.
- [ ] In a prod executor (`BRANCH=prod`), the probe runner publishes a task with filename ending `-prod.md` and frontmatter `stage: prod`.
- [ ] The task identifier emitted for `(claude-agent, dev)` differs from the identifier emitted for `(claude-agent, prod)`.
- [ ] The task identifier function is a pure function of `(agent, stage)` with no per-process state, package-level mutable cache, or randomness — i.e. two callers passing the same `(agent, stage)` always receive the same UUID, including across a process restart.
- [ ] Every probe cycle publishes `status: in_progress` and `phase: in_progress`, even when the prior cycle's vault file ends in `status: done`.
- [ ] The published frontmatter `stage` value equals the executor's `Branch` argument verbatim.
- [ ] The UUIDv5 namespace constant carries a comment stating it is frozen and must not be changed without a migration plan.
- [ ] No new CLI flag, env var, HTTP route, or cron expression is introduced.
- [ ] The executor's `task_event_handler.go` is not modified by this spec.
- [ ] `make precommit` passes in `task/executor/`.
- [ ] CHANGELOG records the publisher behavior change and notes the operator cleanup step for stale shared probe files.

**Scenario coverage**: No new scenario. The behavior is fully reachable from unit tests on the probe runner (filename, identifier, frontmatter contents per stage) and from existing executor integration coverage (the stage filter is already tested). No real cluster, no real vault, no real `gh` is required to assert any of these acceptance criteria.

## Verification

```
cd task/executor && make precommit
```

Manual post-deploy verification:

```
# After deploy to dev, on next cron tick or after hitting the HTTP trigger:
kubectlquant -n dev logs deploy/agent-task-executor | grep -i probe
# Expect: probe publishes for each agent; no "skipped_stage" metric increments for probe identifiers.

# Vault state (host clone, after sync):
ls ~/Documents/Obsidian/OpenClaw/tasks/probe-*.md
# Expect: probe-<agent>-dev.md and probe-<agent>-prod.md per agent, no bare probe-<agent>.md (after one-time cleanup).
```

## Do-Nothing Option

If we keep the shared probe file: dev and prod continue to fight over `tasks/probe-<agent>.md`, the executor's stage filter continues to drop both clusters' probes on the floor, and no `agent_job_*` rows reach either pushgateway. The probe is functionally a no-op in both clusters. The longer this is deferred, the longer we fly blind on agent liveness across the fleet, and the more new agents inherit a broken probe contract on first onboarding.
