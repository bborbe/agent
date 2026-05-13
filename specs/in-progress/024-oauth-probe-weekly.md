---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-13T16:54:47Z"
generating: "2026-05-13T16:54:57Z"
prompted: "2026-05-13T17:08:05Z"
branch: dark-factory/oauth-probe-weekly
---

## Summary

- Operators have manually refreshed Claude OAuth credentials on the dev cluster twice in four days (2026-05-10, 2026-05-13). The failure mode is silent: tokens age out of validity between agent runs, and the next real task triggered against the agent is the one that surfaces the breakage — as a `human_review` escalation with the freshly-improved `## Failure` body from spec 023.
- This spec adds a weekly heartbeat cron to `task/executor` that re-runs one stable probe task per known `Config` CR. Each probe is exercised on cadence by publishing two atomic frontmatter commands: `create-task` (idempotent bootstrap on first run, no-op thereafter) and `update-frontmatter` (resets `phase: planning`, `trigger_count: 0`, `retry_count: 0` to trigger a fresh agent spawn).
- The reset emits an `agent-task-v1-event` that the executor already consumes; a Job spawns, claude executes `"reply 'ok'"`, the PVC's `.credentials.json` is rotated by that successful run, and the task closes back to `phase: done` until the next tick.
- Detection rides entirely on the failure pipeline shipped today via `lib/v0.61.1`. A non-zero exit from the probe leaves the task at `phase: human_review` (or `assignee: ""` on cap escalation) with an operator-readable failure body. No new metric, no new alert, no new escalation surface.
- The probe loop auto-enrolls. Any new `Config` CR in the executor's namespace is picked up on the next cron tick — no code, config, or CRD change required to onboard new agents.

## Problem

Claude OAuth tokens live on a per-agent PVC at `~/.claude/.credentials.json`. They are rotated automatically by every successful `claude --print` invocation, but if an agent goes idle long enough between real tasks the token expires, and the next task fails on authentication. That failure is what operators see — there is no proactive signal that an agent's credentials are about to lapse. Two manual refresh waves in four days (2026-05-10, 2026-05-13) confirm the cadence is wrong: token lifetime is shorter than the natural inter-task gap for several agents.

The status quo wastes operator attention twice: once when a real task fails because of an expired token (the task is now an incident, not an outcome), and again when each agent must be refreshed by hand. The fix that aligns with the existing architecture is not "alert on token age" — there is no token-age signal exposed — but "exercise each agent on a schedule short enough to keep credentials warm." A weekly probe across all known `Config` CRs makes refresh a side effect of monitoring.

## Goal

After this spec, the executor publishes a pair of atomic frontmatter commands per `Config` CR on a configurable weekly cadence (default Mondays 08:00). Each tick re-triggers a stable per-agent probe task that flows through the existing reconcile → Job → vault writeback pipeline. Successful probes refresh the agent's PVC credentials in passing; failing probes escalate to `human_review` (or empty `assignee` on cap) with the operator-readable failure body delivered by `lib/v0.61.1`. New agents added via `Config` CR are auto-enrolled at the next tick. No new alert, metric, or CRD field is introduced.

## Non-goals

- No new Prometheus metric. There is no `agent_oauth_probe{result=...}` series. Probe outcomes are observable only through the same vault-task surface every other task uses.
- No new alert rule. Probe failures reuse the existing `human_review` route. If that route is sufficient for real-task failures, it is sufficient for probe failures.
- No direct `Job` spawn from the probe loop. Probes flow through the executor's existing reconcile path. The probe loop is a Kafka producer of `agent-task-v1-request` commands; it is not a Kubernetes Job client.
- No stdout parsing or regex classification of failure type. The probe relies entirely on `lib/v0.61.1`'s tail-capture to surface the CLI's own diagnostic output.
- No release. Cutting paired `vX.Y.Z` + `lib/vX.Y.Z` tags and bumping downstream consumers are separate sibling work.
- No direct vault writes from the probe loop. Probe tasks are written and updated exclusively by `task/controller` consuming the published commands. The probe loop never touches git-rest, the vault, or files directly.
- No `probeInterval` field on the `Config` CR. Per-agent cadence would have required a lib change, a CRD bump, and downstream go.mod bumps. The single cluster-wide cron expression is intentional for this rung.
- No `probe: true` short-circuit for agents that reject the minimal prompt. If an agent (e.g. `pr-reviewer` expecting a `clone_url`) cannot run `reply 'ok'` cleanly, that surfaces as a probe failure on the first tick and the operator decides whether to add a per-agent bypass in a follow-up spec.

## Alternatives Considered

Three structurally different placements of the cron were weighed. The chosen one is recorded last.

- **External k8s `CronJob` per cluster** — one manifest per agent per namespace, each publishing one command to Kafka. Splits ownership across files, requires per-Config bookkeeping in YAML, no auto-enrollment when a new `Config` CR appears. Rejected: extra surface for no behavioral gain.
- **Direct `Job` spawn from the probe loop, bypassing the vault** — the executor's probe loop calls the K8s API directly to spawn probe Jobs against each `Config`. Cleaner role-wise (executor stays K8s-side only) but successful probes leave no audit trail in the vault, and probe failures must synthesize a vault writeback via the existing spec 009 path anyway. Operators lose the weekly OK signal entirely. Rejected: trades observability for a thinner role boundary that costs more than it saves.
- **CHOSEN — executor cron publishes `agent-task-v1-request` commands** — the probe loop joins the same publisher role that agents themselves occupy (every agent already publishes `update` commands on this topic). The executor's role expansion is small: it gains a Kafka producer for `create-task` and `update-frontmatter`, both of which the controller's CommandObjectExecutors already process. Successful and failing probes are both visible in the vault as normal tasks. Auto-enrollment is free via the existing `Config` lister.

## Desired Behavior

1. On each tick of the configured cron expression, the executor lists every `Config` CR in its own namespace via the existing informer/lister.
2. For each Config, the probe loop publishes two `agent-task-v1-request` commands in order on the same topic via the existing `syncProducer`:
   - `create-task` with `task_identifier = probe-<agent-name>`, `title = probe-<agent-name>`, `task_type = oauth-probe`, frontmatter `{status: in_progress, phase: planning, assignee: <agent-name>}`, content `reply 'ok'`. First tick creates the vault file at `tasks/probe-<agent-name>.md`; subsequent ticks are no-ops per spec-019 idempotency (same `task_identifier`).
   - `update-frontmatter` with `task_identifier = probe-<agent-name>`, updates `{phase: planning, trigger_count: 0, retry_count: 0}`. This is the actual re-trigger: the controller's atomic write produces an `agent-task-v1-event` that the executor consumes and a Job is spawned. On the first tick (file just created) the reset is a no-op write.
3. The probe loop produces no output beyond the Kafka publishes and structured logs. It does not call the Kubernetes API directly, does not touch the vault, and does not depend on the controller being healthy at tick time — Kafka is the buffer.
4. The cron cadence is configurable via the new env field `OAuthProbeCronExpression` (arg `oauth-probe-cron-expression`, env `OAUTH_PROBE_CRON_EXPRESSION`, default `0 0 8 * * 1` — Quartz 6-field, Mondays 08:00).
5. The probe loop is wired into the executor's existing `service.Run(...)` call alongside the other `run.Func`s. It starts when the executor starts and stops when the executor stops. No separate process, no separate Deployment.
6. New `Config` CRs added between ticks are picked up on the next tick. There is no admission-time enrollment step and no per-Config bookkeeping. Deleted Configs simply disappear from the next tick's snapshot; in-flight probe Jobs for a just-deleted agent finish through the existing reconcile path.

## Constraints

- Change is confined to the `task/executor` module: its `main.go`, its factory package, and the new probe loop's package and tests. No file in `lib/*`, `task/controller/*`, `agent/*`, or `prompt/*` is modified, and no downstream `go.mod` is bumped.
- The `Config` CR schema is NOT changed. No new fields, no new printer columns.
- Task IDs are stable per agent (`probe-<agent-name>`) — not per-week, not per-run. The set of probe vault files is bounded by the number of `Config` CRs and does not accumulate over time.
- The probe loop publishes to the same `agent-task-v1-request` topic that the controller already consumes. No new topic, no new schema.
- The factory function for the probe cron follows the project's existing cron-factory shape (see `cron.NewExpressionCron` usage in trading services). Retry-on-error semantics are inherited from `cron.NewExpressionCron` as-is.
- The cron expression format is Quartz 6-field. The default `0 0 8 * * 1` MUST parse cleanly at startup; a parse failure is a fatal startup error, not a runtime warning.
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks per project convention. `make precommit` (license, lint, gosec, trivy, generate-drift, go test) must be clean.
- A bullet under `## Unreleased` in the root `CHANGELOG.md` is required. No per-module CHANGELOG.
- Project tag policy from `CLAUDE.md` still applies if a release is cut from the merge: paired `vX.Y.Z` + `lib/vX.Y.Z` tags. Cutting that release is NOT in scope.

## Assumptions

- The executor already runs a `Config` CR informer/lister in its own namespace; the probe loop reuses that lister.
- The executor already constructs a `syncProducer` for publishing to Kafka. Verification against `pkg/factory/factory.go` is part of the implementation prompt.
- `service.Run(...)` accepts arbitrary `run.Func` values; the probe loop is appended to the existing composition.
- The controller's `create-task` executor implements spec-019 idempotency on `task_identifier` (verified in `task-flow-and-failure-semantics.md` §Create-Task Path Resolution).
- The controller's `update-frontmatter` executor (spec 016) accepts arbitrary frontmatter updates and emits an `agent-task-v1-event` on write.
- Each agent image accepts the minimal prompt `reply 'ok'`. This is the assumption that may not hold for every existing agent — see Failure Modes.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `Config` lister returns zero items | Probe tick is a noop; no Kafka publishes; structured log records the zero-Config tick | None |
| Kafka publish fails for one or more Configs | Error is sentry-wrapped and returned from the inner `Run`; `cron.NewExpressionCron` handles retry per its existing semantics; partial successes within the same tick are NOT rolled back | Operator inspects sentry; next tick recovers |
| `create-task` succeeds but `update-frontmatter` fails for the same agent within a tick | Vault has the probe task file but it is not reset to `phase: planning`; the next successful tick resets it | None — worst case is one missed weekly probe for that agent |
| `Config` CR is added between ticks | Picked up at the next tick; the first tick after enrollment performs both create-task (bootstraps the file) and update-frontmatter (which is a no-op on the just-created defaults) | None — by design |
| `Config` CR is deleted between ticks | Deleted Config is absent from the next tick's lister snapshot; no probe is published; any in-flight Job for that agent finishes through the existing reconcile path; the orphan probe vault file is left in place | Operator deletes the orphan vault file manually if desired (out of scope here) |
| Agent image rejects `reply 'ok'` because it requires extra pre-prompt plumbing | Probe Job exits non-zero; failure body surfaces the rejection via `lib/v0.61.1`'s tail-capture; existing `human_review` route pages the operator | Operator reads the failure body and decides whether to file a follow-up spec for a `probe: true` short-circuit |
| Cron expression fails to parse at startup | Executor refuses to start; startup error message names the offending expression | Operator corrects `OAUTH_PROBE_CRON_EXPRESSION` and restarts |
| `make precommit` flags drift after `make generate` | Implementation regenerates and commits the drift | None — caught at the verification rung |

## Security / Abuse Cases

- The probe loop reads from a `Config` lister it already owns and publishes to a Kafka topic it already produces to. No new trust boundary is crossed and no new external endpoint is contacted.
- The probe prompt is the constant string `reply 'ok'`. There is no template expansion from `Config` fields into the prompt; a malicious `Config` cannot inject prompt content into another agent's run.
- Task ID construction concatenates the literal `probe-` prefix with the agent name (an admitted Kubernetes object name, validated by the API server). The result is bounded in length and character set.
- DoS: probe volume is bounded by `len(Configs) × ticks_per_week`. At the default weekly cadence the load is negligible relative to real-task traffic.
- Secret handling is unchanged. The probe loop does not read, write, or log credentials; the PVC's `.credentials.json` rotation is a side effect of the spawned Job, not an action taken by the probe loop itself.

## Acceptance Criteria

- [ ] `task/executor`'s `main.go` carries a new env field `OAuthProbeCronExpression string` with arg `oauth-probe-cron-expression`, env `OAUTH_PROBE_CRON_EXPRESSION`, default `0 0 8 * * 1`, usage `Cron expression for Claude OAuth health probes`.
- [ ] A new factory function `CreateOAuthProbeCron(...)` exists in the executor's factory package, wrapping the inner `Run` with `cron.NewExpressionCron` from `github.com/bborbe/cron`.
- [ ] On each tick, the inner `Run` lists every `Config` CR in the executor's namespace via the existing lister and, for each Config, publishes exactly one `create-task` command followed by one `update-frontmatter` command — both on the existing `agent-task-v1-request` topic via the existing `syncProducer`.
- [ ] The published `create-task` carries `task_identifier = probe-<agent-name>`, `task_type = oauth-probe`, `assignee = <agent-name>`, frontmatter `{status: in_progress, phase: planning}`, and content `reply 'ok'`. The published `update-frontmatter` carries `task_identifier = probe-<agent-name>` and updates `{phase: planning, trigger_count: 0, retry_count: 0}`.
- [ ] The probe `run.Func` is wired into the executor's existing `service.Run(...)` invocation in `main.go`.
- [ ] Unit tests cover: (a) given N fake `Config`s, exactly 2N commands are produced in the expected order with the expected payloads; (b) empty lister produces zero publishes and no error; (c) producer error in the first publish is propagated wrapped; (d) producer error in the second publish is propagated wrapped after the first succeeded (no rollback).
- [ ] A bullet describing the change is present under `## Unreleased` in the root `CHANGELOG.md`.
- [ ] `make precommit` is clean in `task/executor`. No drift after `make generate`. Lint, license, gosec, trivy, format, generate, test all green.
- [ ] No file outside `task/executor/` and `CHANGELOG.md` is modified.

## Scenario Coverage

**No new scenario.** The probe loop's behavior is fully reachable via unit tests against the factory and the inner `Run` using counterfeiter fakes for the `Config` lister and the Kafka producer. The downstream reconcile path is already covered by completed specs (017 `CreateTaskCommand`, 016 `update-frontmatter`, 019 create-task path resolution, 009 executor-job-failure-detection, 023 surface-claude-cli-failure-reason). End-to-end probe-to-vault-task validation will happen at operator-run rollout time on dev; codifying it as a scenario would require a deliberately-broken OAuth state in the cluster, which is not reproducible from CI.

## Verification

```
cd task/executor
make precommit
```

`make precommit` runs lint, license, gosec, trivy, generate-drift, and go test. Acceptance stands on this rung alone.

Negative-path manual check (gated on a future release that ships this change to dev):

```
# After dev deploy, set a near-future cron expression on dev to fast-forward:
#   kubectlquant -n dev set env statefulset/agent-task-executor OAUTH_PROBE_CRON_EXPRESSION="0 */2 * * * *"
# Wait one tick. Confirm:
#   - one vault file per Config CR at tasks/probe-<agent>.md
#   - each probe task transitions planning → done within ~10s on healthy OAuth
#   - PVC .credentials.json mtime advances on the agent host
#   - any failing probe shows the lib/v0.61.1 ## Failure body with an operator-readable Reason
# Reset cron expression after verification.
```

## Do-Nothing Option

Ship nothing. Operators continue to manually refresh Claude OAuth credentials on whatever cadence is forced by real-task failures — recently 2026-05-10 and 2026-05-13, four days apart on dev. Each refresh wave costs operator attention twice: once to triage the failed task as an OAuth incident rather than a real outcome, and once to perform the refresh. The cost compounds with each new agent added: every new `Config` CR is a new latent token to expire, and there is no proactive signal that any agent's credentials are about to lapse.

The cost of fixing is small (one new env field, one new factory function, two-publish loop, table-driven unit tests, one CHANGELOG bullet) and is confined to a single module. The probe also doubles as the refresh, so the fix is both monitoring and remediation in one mechanism. Leaving the gap shifts the cost from build-time to operator-time on every token expiration, indefinitely.
