---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-14T12:30:11Z"
generating: "2026-05-14T12:39:03Z"
prompted: "2026-05-14T12:45:09Z"
branch: dark-factory/rename-oauth-probe-to-healthcheck
---

## Summary

- Rename the executor's `oauth-probe` concept to `healthcheck` across code, HTTP routes, env vars, K8s manifests, and published task frontmatter.
- The probe verifies liveness (binary boots, CLI present, auth valid, plugins loaded), not OAuth specifically — the current name misleads and blocks the move off OAuth.
- Hard cut at deploy time: no HTTP redirect, no env alias, no acceptance of the legacy `task_type: oauth-probe` value by the renamed publisher.
- In-flight vault probe tasks self-heal: the UUIDv5 task identifier is preserved, so the next probe tick overwrites stale frontmatter.
- Defers removal of the `TaskTypeOAuthProbe` constant in `lib/` — other repos (trading, maintainer) still consume that value until their own dispatch specs ship.

## Problem

The executor's probe pipeline publishes `task_type: "oauth-probe"` and exposes an HTTP trigger at `/oauth-probe-trigger`. The name describes a mechanism (OAuth credential check) rather than intent (the binary is alive and ready to accept work). The probe already checks more than OAuth: CLI presence, MCP plugin load, agent boot. As the agent fleet moves to alternative API providers, the OAuth-specific name becomes actively misleading — yet the same liveness check is still required. The name must describe what the probe verifies, not which credential type happens to be checked today.

## Goal

After this work:

- The executor publishes probe tasks with `task_type: healthcheck`.
- The executor exposes the trigger at `/healthcheck-trigger`.
- The executor's deployment env is `HEALTHCHECK_CRON_EXPRESSION`.
- The internal probe package, factory, handler, runner interface, and mock all use the `Healthcheck` name.
- No occurrence of `oauth-probe`, `OAuthProbe`, or `OAUTH_PROBE` remains anywhere under `task/executor/`.
- The UUIDv5 task identifier is unchanged so in-flight probe tasks self-heal.

## Non-goals

- Removing the `TaskTypeOAuthProbe` constant from `lib/`. Trading and maintainer agent binaries still consume `oauth-probe` until their own dispatch specs ship. Deferred to a follow-up.
- Removing `oauth-probe` from `taskTypes` lists in Config CRs across agent + trading + maintainer repos. Handled as a direct edit after this ships.
- Updating the runbook that calls `/oauth-probe-trigger`. Operator concern, post-deploy.
- Editing existing vault probe task frontmatter manually. Self-healing on next tick.
- Backward-compatibility shims: no 308 redirect, no legacy env alias, no acceptance of `task_type: oauth-probe` by the renamed publisher.

## Desired Behavior

1. The probe publisher publishes tasks with `task_type: healthcheck` (sourced from the `lib.TaskTypeHealthcheck` constant introduced by the agent-repo task-type dispatch spec).
2. The HTTP trigger route is `/healthcheck-trigger`. The legacy `/oauth-probe-trigger` route is removed and returns 404.
3. The deployment env var is `HEALTHCHECK_CRON_EXPRESSION` with the existing default cron expression `0 0 8 * * 1` unchanged.
4. The CLI flag is `healthcheck-cron-expression`.
5. The executor probe package, runner interface, struct, constructor, factory functions, HTTP handler, and counterfeiter mock all use the `Healthcheck` name.
6. The UUIDv5 namespace constant used to derive the probe task identifier is unchanged.
7. In-flight vault probe tasks with `task_type: oauth-probe` frontmatter fail the executor's task-type filter once after deploy, then are overwritten by the renamed publisher on the next probe tick (same UUIDv5 → same vault path → frontmatter rewrite).
8. `make precommit` passes in `task/executor/` and `lib/`.
9. CHANGELOG records the rename as a BREAKING change with the operator-facing impacts enumerated.

## Constraints

- **Dependency: agent-repo task-type dispatch spec must merge first.** It introduces `lib.TaskTypeHealthcheck` and the shared `lib/healthcheck/` agent that consumes the new `task_type` value. Without it, the renamed publisher produces tasks no agent binary knows how to handle.
- **Do not remove the `TaskTypeOAuthProbe` constant from `lib/agent_task-type.go`.** Trading and maintainer agents still accept `oauth-probe` until their own dispatch specs ship. Removal is a separate follow-up spec.
- **UUIDv5 namespace constant is frozen.** The value used to derive probe task identifiers (`00000000-0000-0000-0000-000000000024`) stays unchanged so existing vault probe tasks self-heal rather than being orphaned.
- **Default cron expression is frozen.** `0 0 8 * * 1` is preserved when renaming the env var.
- See `docs/task-flow-and-failure-semantics.md` for the executor's task-type filter behavior referenced in the self-healing failure mode.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| External caller hits `/oauth-probe-trigger` after deploy | HTTP 404 | Caller updates to `/healthcheck-trigger` |
| Deployment YAML still sets `OAUTH_PROBE_CRON_EXPRESSION` | Env ignored; default cron `0 0 8 * * 1` applies | Operator updates YAML to `HEALTHCHECK_CRON_EXPRESSION` |
| In-flight vault task has `task_type: oauth-probe` frontmatter | Executor task-type filter rejects on first event delivery; synthetic failure recorded | Next cron tick republishes with `task_type: healthcheck` under same UUIDv5; frontmatter overwritten |
| Trading or maintainer Config CRs still list `oauth-probe` in `taskTypes` | No effect — the value is no longer published by the executor | Cleaned up out-of-band when the relevant dispatch specs ship |
| Agent-repo dispatch spec not yet merged at deploy time | Renamed publisher publishes `healthcheck`, but no agent binary dispatches it | Hold deploy until dispatch spec ships (verified via constant existence) |

## Security / Abuse Cases

- The HTTP trigger handler is the only externally reachable surface affected. Authentication, rate-limiting, and input validation are unchanged by this rename. Renaming a route does not relax any existing constraint.
- No new user input, file paths, or trust boundaries are introduced.

## Acceptance Criteria

- [ ] `grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/` returns zero matches (excluding CHANGELOG and any generated mock headers from before regeneration).
- [ ] `grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/k8s/` returns zero matches.
- [ ] HTTP route `/healthcheck-trigger` responds successfully; `/oauth-probe-trigger` returns 404.
- [ ] The probe publisher's emitted frontmatter contains `task_type: healthcheck`.
- [ ] The UUIDv5 namespace constant for probe task identifiers is unchanged from its current value.
- [ ] The runner interface, struct, constructor, factory functions, HTTP handler constructor, and counterfeiter mock filenames all use `Healthcheck` (no `OAuthProbe` remnants).
- [ ] The CLI flag is `healthcheck-cron-expression`; the env var is `HEALTHCHECK_CRON_EXPRESSION`.
- [ ] The default cron expression `0 0 8 * * 1` is preserved.
- [ ] The `lib.TaskTypeOAuthProbe` constant is **still present** in `lib/agent_task-type.go` after this spec ships (removal deferred).
- [ ] The renamed publisher sources its task-type literal from `lib.TaskTypeHealthcheck.String()` (not a string literal).
- [ ] **Spec 031 dependency check**: `grep -n 'TaskTypeHealthcheck' lib/agent_task-type.go` returns at least one match (i.e. the constant exists at the SHA this spec's PR is rebased onto). If absent, this spec must not be merged.
- [ ] `make precommit` passes in `task/executor/` and `lib/`.
- [ ] CHANGELOG contains a BREAKING entry naming: HTTP route change, env var rename, factory + handler + interface renames, and the self-healing behavior for in-flight tasks.
- [ ] A second CHANGELOG entry confirms the `TaskTypeOAuthProbe` constant is intentionally retained for trading/maintainer consumers.

**Scenario coverage**: No new scenario. The behavior is fully reachable from unit tests (handler routing, env parsing, factory wiring, published frontmatter content) and integration tests (probe publish → executor filter → vault file content). The self-healing failure mode is observable from existing probe integration coverage once the publisher is renamed; no E2E run against a real cluster is needed to assert the rename.

## Verification

```
cd task/executor && make precommit
cd lib && make precommit
grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/ task/executor/k8s/ --exclude=CHANGELOG.md
grep -n 'TaskTypeHealthcheck' lib/agent_task-type.go
```

Expected: precommit passes both modules; grep returns zero matches.

## Do-Nothing Option

If we keep the name: the probe pipeline continues working today. But every future agent that moves off OAuth (per the broader provider-switch effort) inherits a misleading name, every new contributor has to be told "oauth-probe doesn't only check OAuth," and the CRD `taskTypes` lists across three repos accumulate a stale value that nobody dares remove because the contract is unclear. The longer this is deferred, the more call sites accumulate.
