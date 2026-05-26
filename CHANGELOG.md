# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

## v0.63.30

- test(task/executor): add PublishIncrementTriggerCount coverage tests for happy path, error path, and empty task identifier edge case
- test(task/executor): add PublishRaw test covering base.ParseEvent error path when invalid JSON is passed

## v0.63.29

- fix(task/controller): reject titles containing path separators in resolveCreateTaskPath as defense-in-depth against traversal
- fix(task/controller): check context cancellation before first HTTP attempt in Post and Delete retry loops
- fix(task/controller): limit HTTP response body reads to 10 MiB in Get and List methods

## v0.63.28

- test(task/controller): add coverage tests for NewGitRestVaultScanner, exponentialBackoff, extractFrontmatter CRLF, and processFile YAML unmarshal failure path

## v0.63.27

- feat(task/controller): add Metrics interface to pkg/metrics/metrics.go enabling Counterfeiter mock injection for testability

## v0.63.26

- chore(task/controller): add tools.go declaring build tool dependencies
- chore(task/controller): add consolidated .PHONY declaration to Makefile

## v0.63.25

- test(lib/claude): add test coverage for NewAgentStep, NewNoopResultDeliverer, deliver error path, stepString with escaped chars, buildCommand with AllowedTools/Model/WorkingDirectory flags — overall coverage 76.6% → 85.1%

## v0.63.24

- refactor(agent/gemini): remove `CreateSyncProducer` and `CreateGeminiParser` factory functions — moved error-producing logic to call sites in `main.go` and `cmd/run-task/main.go`

## v0.63.23

- test(agent/gemini/pkg/steps): add Ginkgo v2 test suite covering ExecuteStep, VerifyStep, compute, and needsInput — 95.7% statement coverage

## v0.63.22

- test(agent/code/pkg/steps): add Ginkgo v2 test suite with 26 tests covering PlanStep, ExecuteStep, VerifyStep, and the compute helper — 95.2% statement coverage

## v0.63.21

- refactor(agent/code): simplify `CreateSyncProducer` factory to pure pass-through — accept agentName parameter, remove internal error wrapping, move error propagation to caller

## v0.63.20

- fix(agent/claude): use display:"password" for AnthropicAuthToken to fully mask credentials in process listings
- fix(agent/claude): add display:"length" to SentryProxy to mask embedded credentials in proxy URLs
- fix(agent/claude): add GoDoc comment to agentName constant
- fix(agent/claude): add package documentation to prompts package
- fix(agent/claude/cmd/run-task): use errors.Wrap with string concatenation instead of Wrapf with literal %s

## v0.63.19

- test(agent/claude): add tests for BuildInstructions, CreateKafkaResultDeliverer, CreateFileResultDeliverer, and CreateAgent with ≥80% coverage
- test(agent/claude): fix factory_suite_test.go GinkgoConfiguration with 60s timeout
- test(agent/claude): add //go:generate counterfeiter directive to main_test.go files

## v0.63.18

- refactor(agent/claude): simplify `CreateSyncProducer` factory to pure pass-through — removed internal error wrapping; error propagation now handled by caller in `main.go`

## v0.63.17

- fix(task/executor): handle JSON encode errors in `AgentsHandler.ServeHTTP` — return HTTP 500 when client disconnects mid-write instead of silently returning HTTP 200 with partial JSON

## v0.63.16

- feat(task/executor): add `ImagePullSecret` field to `AgentConfiguration` — allows the image pull secret name to be configured via the Config CR instead of being hardcoded to `docker`

## v0.63.15

- fix(task/controller): partial-update executor now enforces `phase: human_review` → `assignee: ""` doctrine via shared helper `result.ClearAssigneeIfHumanReview`. Closes the sixth `human_review` write site missed by spec 039 (predecessor); fixes the 2026-05-25 prod incident where pr-reviewer-agent emitted `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` on PR #3 and the task landed with `assignee: pr-reviewer-agent` still set, bypassing the operator inbox filter.

## v0.63.14

- feat(task/controller): add `ClearAssigneeIfHumanReview` shared helper in `result_writer.go` (spec 042) — centralizes the spec-039 doctrine (`phase: human_review` → `assignee: ""`) in a single exported function; routes through `clearAssignee` which captures prior assignee into `previous_assignee` if non-empty
- feat(task/controller): wire `ClearAssigneeIfHumanReview` into `buildUpdateModifyFn` in partial-update executor (spec 042) — enforces the human_review assignee-clear doctrine inside the same atomic write that performs the frontmatter merge; no-op when the merge does not produce `phase: human_review`
- feat(task/controller): replace inline `phase == "human_review"` guard in `applyRetryCounter` with `ClearAssigneeIfHumanReview` call — observable behavior unchanged; both the result writer and the partial-update executor now share the same chokepoint
- test(task/controller): add four spec-042 Ginkgo tests covering: phase-flip to human_review clears assignee, non-phase updates preserve assignee, idempotent re-clear on already-parked tasks, combined frontmatter+body verdict path (live 2026-05-25 prod reproducer)

## v0.63.13

- fix(controller): `resultWriter.applyRetryCounter` now runs the `phase == "human_review"` assignee-clear guard BEFORE the `spawn_notification` early return, so the spec 039 guard fires on the pr-reviewer agent's first post-spawn handoff. Previously the inherited `spawn_notification: true` on the merged frontmatter short-circuited the function before the guard ran, leaving `assignee: <agent>` on a task at `phase: human_review` and hiding it from the operator inbox filter. Live prod incident 2026-05-25 (~8h after the spec 039 deploy); second instance of the same bug class (precedent: 2026-04-24 `applyTriggerCap` reorder, prompt 075).

## v0.63.12

- fix(task/executor): pass context.Background() to NewHealthcheckTriggerHandler instead of caller's ctx; prevents premature context cancellation when HTTP server is still listening

## v0.63.11

- docs(task/executor): add GoDoc comments to jobSpawner, k8sConnector, and resultPublisher exported structs

## v0.63.10

- fix(task/controller): add suiteConfig.Timeout to metrics/result/command Ginkgo suites; add missing suite setup (time.Local, format.TruncatedDiff, GinkgoConfiguration) to gitrestclient and main test suites; replace time.Now() with libtimetest.ParseDateTime in task_result_executor_test.go

## v0.63.9

- refactor(task/controller): change `vault_scanner_test.go` to `package scanner_test` (external test package); add `RunCycle` to `VaultScanner` interface; export `InjectTaskIdentifier` and `DeduplicateFrontmatter` for test access
- fix(task/controller): reorder `resultWriter.applyRetryCounter` to run `phase == "human_review"` guard BEFORE the `spawn_notification` early return; fixes live-observed regression where merged frontmatter with `spawn_notification: true` skipped the assignee-clear guard and left `assignee: pr-reviewer-agent` on a `human_review` task (spec 039 regression; 2026-05-25 prod incident; task bborbe-agent #3)

## v0.63.8

- fix(task/controller): add context cancellation checks to `scanFiles` and `collectDeleted` loops in vault scanner

## v0.63.7

- fix(task/controller): rename metric `agent_task_controller_frontmatter_commands_total` to `agent_controller_frontmatter_commands_total`
- fix(task/controller): rename metric `controller_gitrest_calls_total` to `agent_controller_git_rest_calls_total`

## v0.63.6

- refactor(task/controller): remove `Fake` prefix from all Counterfeiter `--fake-name` directives — mocks now named `SyncLoop`, `GitRestClient`, `GitClient`, `VaultScanner`, `TaskPublisher`, `ResultWriter`

## v0.63.5

- fix(task/controller): pass context to `injectTaskIdentifier` in vault scanner instead of using `context.Background()`

## v0.63.4

- fix(lib): replace `fmt.Errorf` with `errors.Wrapf` in `PrintResult` and add context parameter
- fix(lib/command/task): wrap bare `return err` in title validation closures with context
- fix(lib/claude): wrap bare `return err` in `EnsureInstalled` and `ensureOne` with context

## v0.63.3

- fix(lib): add non-blocking `ctx.Done()` select checks to long-running loops in `StepRunner.Run` and `EnsureInstalled` to enable graceful shutdown on context cancellation

## v0.63.2

- fix(agent/gemini): pass context.Context to `parser.New` instead of using `context.Background()` internally; context is now propagated to the Gemini client and error wrapping, enabling proper cancellation and deadline handling

## v0.63.1

- fix(agent/code): replace `context.Background()` with `signal.NotifyContext` in `main.go` and `cmd/run-task/main.go` entry points to enable graceful shutdown on SIGTERM/SIGINT signals

## v0.63.0

- feat(lib): `agentlib.Agent.Run` now loops over phases in one process — when a step publishes `Done + NextPhase` and that phase exists on the same Agent, the loop runs it in-process instead of returning. The pod only exits on terminal status, terminal NextPhase (`"done"`/`"human_review"`/empty/unknown-to-this-agent), or ctx cancel. Consequence: one pod boot per agent on the happy path; the executor's 300s respawn grace window now only fires on genuine crashes and cross-agent hops.

## v0.62.29

- fix(controller): stop writing `phase: human_review` on `trigger_count` cap-exhaustion path in `task_increment_frontmatter_executor`; phase now reflects the lifecycle stage and only `assignee` is cleared (completes spec-021 escalation doctrine; spec-021 `needs_input` row superseded)
- fix(lib/delivery): stop writing `phase: human_review` on `AgentStatusNeedsInput` and `AgentStatusFailed`/default branches in `result-deliverer` and `content-generator`; phase now reflects the lifecycle stage and only `assignee` is cleared (completes spec-021 escalation doctrine)

## v0.62.28

- docs: update `docs/task-flow-and-failure-semantics.md` to reflect spec-039 doctrine: `phase: human_review` is reserved for agent-emitted `Result.NextPhase` handoffs; controller-side failure paths leave phase unchanged and clear assignee instead

## v0.62.27

- fix(lib/delivery): `applyStatusFrontmatter` no longer writes `phase: human_review` on `AgentStatusNeedsInput` or `AgentStatusFailed`; clears `assignee` and preserves existing phase instead

## v0.62.26

- fix(task/controller): `IncrementFrontmatterExecutor` cap-escalation now clears `assignee` and preserves `phase` instead of setting `phase=human_review`

## v0.62.25

- chore(task/executor): bump spawned-Job `TTLSecondsAfterFinished` 600s → 1800s; completed pods + logs stay queryable for 30 min instead of 10, giving operators headroom for live debug

## v0.62.24

- test(task/controller): update test inputs and assertions from legacy status `"todo"` → `"next"` and phase `"in_progress"` → `"execution"` in scanner, command, publisher, sync, and result packages
- test(task/controller): add alias roundtrip tests proving `NormalizeTaskPhase("in_progress")` → `TaskPhaseExecution` and `NormalizeTaskStatus("todo")` → `TaskStatusNext` in scanner and command packages

## v0.62.23

- chore(deps): bump `github.com/bborbe/vault-cli` to v0.64.3 across lib, task/controller, task/executor, agent/claude, agent/gemini, agent/code — exposes `TaskStatusNext` and `TaskPhaseExecution` constants
- refactor(lib): `TaskFrontmatter.Phase()` and `Status()` now call `NormalizeTaskPhase` / `NormalizeTaskStatus` so legacy phase `"in_progress"` and status `"todo"` transparently resolve to new canonical values on read
- refactor(lib/delivery): `resolveNextPhase` now uses `NormalizeTaskPhase` so legacy `NextPhase="in_progress"` normalizes to `"execution"` instead of failing validation and falling back to `"done"`
- refactor(task/executor): `defaultTriggerPhases` and `knownPhases` updated to reference `domain.TaskPhaseExecution` instead of `domain.TaskPhaseInProgress`
- refactor(agent/claude): Phase flag default changed from `"in_progress"` to `"execution"`; usage string updated to `planning | execution | ai_review`

## v0.62.22

- test(agent/code,agent/gemini): add compile-only smoke test `cmd/run-task/main_test.go` to mirror the existing claude variant. Closes the gap where `agent/claude/cmd/run-task/` had a Ginkgo `TestSuite` but the code and gemini siblings had none.

## v0.62.21

- feat(agent/claude): route claude CLI to Anthropic-compatible alt-provider via dedicated `AnthropicBaseURL`/`AnthropicAuthToken`/`AnthropicModel` fields on the application struct (mapped to `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_MODEL` env vars). The renamed `AnthropicModel` field drives both the `--model` CLI flag and the `ANTHROPIC_MODEL` env var on the claude subprocess — single source of truth replaces the prior `MODEL`/`ANTHROPIC_MODEL` two-knob configuration. Applied to both Kafka entry point (`agent/claude/main.go`) and local CLI entry point (`agent/claude/cmd/run-task/main.go`).
- feat(agent/claude): `k8s/agent-claude.yaml` adds `ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic` + `ANTHROPIC_MODEL=MiniMax-M2.7-highspeed` to `spec.env`; `k8s/agent-claude-secret.yaml` adds `ANTHROPIC_AUTH_TOKEN` sourced from teamvault `MOPmQL`. Enables MiniMax routing for dev canary as part of `[[Switch Agent API Provider]]` work.
- fix(go.mod): vulnerability fix
- chore(go.mod): bump dependencies (multiple cycles)
- chore(prompts/specs): update generated artifacts

## v0.62.20

- fix(task/executor): add deferred-respawn reconciliation loop — when `checkActiveCurrentJob` suppresses respawn inside the grace window, the task is queued for re-evaluation after `defaultRespawnGracePeriod`; `RunDeferredRespawnLoop` polls every 30s and calls `spawnIfNeeded` once grace elapses; emits `event=respawn_after_grace_window` log and `respawn_after_grace_window` metric; eliminates the "stuck forever" failure mode from 2026-05-17 (task `cbe79223`, PR #128 not reviewed for >2h)
- fix(task/executor): terminal-phase Kafka events now remove any pending deferred-respawn entry for the same task, preventing a stale spawn after the task has transitioned to `human_review` or `done`

## v0.62.19

- fix(task/executor): add grace-period gate in `spawnIfNeeded` — when `current_job` is set and the K8s Job is inactive, respawn is suppressed for 300s from `job_started_at` to allow the agent's terminal-phase write to propagate; emits `event=respawn_grace_window` log + metric; closes the duplicate-spawn race from 2026-05-16T20:25Z prod incident
- fix(lib): add `JobStartedAt() (time.Time, error)` accessor to `TaskFrontmatter` to parse the `job_started_at` frontmatter field written by `PublishSpawnNotification`

## v0.62.18

- fix(task/executor): add explicit terminal-phase gate in `parseAndFilter` — tasks with `phase ∈ {human_review, done}` are suppressed before the trigger-phase allowlist, emitting `event=spawn_suppressed` log and `spawn_suppressed_terminal_phase` metric; unknown phases emit `event=unknown_phase`; closes the 2026-05-16 incident where pod 2 dismissed pod 1's GitHub review on task 22fda7e7

## v0.62.17

- fix(lib/delivery): `ParseMarkdownFrontmatter` now returns `map[string]any` preserving native YAML types (int, float64, bool, list, map) — eliminates git merge conflicts caused by one writer serializing `trigger_count: 0` (int) while another serialized `trigger_count: "0"` (quoted string)

## v0.62.16

- refactor(agent/code): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider (healthcheck-only binary); boot-time deliverer construction moved to main.go Run

## v0.62.15

- refactor(agent/gemini): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider (healthcheck-only binary); boot-time deliverer construction moved to main.go Run

## v0.62.14

- refactor(agent/claude): factory.go is pure plumbing — `CreateAgentForTaskType` and `CreateDeliverer` removed; new `CreateAgentProvider` returns lib.AgentProvider; boot-time deliverer construction moved to main.go Run per go-factory-pattern.md

## v0.62.13

- feat(lib): add `AgentProvider` interface for task_type → *Agent dispatch — map-based provider with sorted-accepted-types error message; consumed by per-binary factory refactors that drop `CreateAgentForTaskType` switch statements (factory pattern compliance)

## v0.62.12

- feat(task/executor): probe runner publishes per-stage vault files and task identifiers; `stage:` frontmatter field matches executor branch (spec 033)
- docs: operator cleanup step — after deploy, delete stale `tasks/probe-<agent>.md` files (no stage suffix) from the OpenClaw vault host clone: `git rm tasks/probe-*.md && git commit -m "remove stale shared probe files" && git push`

## v0.62.11

- BREAKING(task/executor): rename oauth-probe probe pipeline to healthcheck — HTTP route `/oauth-probe-trigger` → `/healthcheck-trigger` (404 on old path after deploy); env var `OAUTH_PROBE_CRON_EXPRESSION` → `HEALTHCHECK_CRON_EXPRESSION` (default `0 0 8 * * 1` unchanged); factory `CreateOAuthProbeRunner`/`CreateOAuthProbeCron` → `CreateHealthcheckRunner`/`CreateHealthcheckCron`; interface `OAuthProbeRunner` → `HealthcheckRunner`; published task_type changes from `oauth-probe` to `healthcheck`; in-flight probe tasks with stale frontmatter self-heal on next cron tick via same UUIDv5 task identifier
- chore(lib): `TaskTypeOAuthProbe` constant intentionally retained in `lib/agent_task-type.go` for trading/maintainer consumers — removal deferred until their dispatch specs ship

## v0.62.10

- feat(agent/{claude,gemini,code}): per-task-type dispatch via factory.CreateAgentForTaskType — healthcheck task type routes to a dedicated liveness agent; unknown task_type fails fast with an accepted-types error (spec 031)

## v0.62.9

- feat(agent/claude): add `CreateAgentForTaskType` dispatch function — routes `healthcheck`/`oauth-probe` to liveness agent, `claude` to 3-phase domain agent; update `main.go` to use it (spec 031)

## v0.62.8

- feat(lib/healthcheck): shared liveness handler package — Claude/Gemini/Nop step flavors + NewAgent wrapper (spec 031)
- feat(lib): add TaskTypeHealthcheck constant; update TaskTypeOAuthProbe GoDoc (drop "once introduced" qualifier) (spec 031)

## v0.62.7

- feat(task/executor): inject TASK_TYPE env into every spawned Job from task frontmatter task_type field (spec 030)

## v0.62.6

- feat(agent/claude): add `healthcheck` to `taskTypes` list alongside `claude` + `oauth-probe` — prepares for healthcheck dispatch (rename of `oauth-probe`); no behavior change yet (executor still routes both)
- feat(lib): add TaskType named type with validation, well-known constants, and TaskFrontmatter.TaskType() accessor (spec 030)

## v0.62.5

- feat(agent/{claude,code,gemini}): wire `JobMetrics` into each binary's `Run()` — constructs a fresh registry + pusher at startup, defers `PushContext` for end-of-run metric delivery, records run outcome and duration at every return path; adds `PUSHGATEWAY_URL` (default `http://pushgateway:9090`) and `TASK_TYPE` (default `unknown`) env fields

## v0.62.4

- feat(lib/metrics): per-agent Prometheus PushGateway metrics package — `JobMetrics` interface with `agent_job_run_total` (CounterVec{status}), `agent_job_last_run_timestamp_seconds` (GaugeVec{status}), `agent_job_duration_seconds` (Histogram). Counter pre-initialized for `done`/`failed`/`needs_input`. Counterfeiter mock at `lib/metrics/mocks/job-metrics.go`.

## v0.62.3

- fix(lib/claude): `CLAUDE_CONFIG_DIR` is now always passed to the Claude subprocess, defaulting to `~/.claude` when the consumer has not configured a value. Previously the env var was only set when explicitly configured, which made Claude write `.claude.json` to the agent's ephemeral `$HOME` rather than the persistent `~/.claude/` PVC mount — refresh tokens were silently lost across Job restarts, eventually causing 401 errors. **Behavioral regression**: agents deployed against existing PVCs (which still have `.claude.json` at the old ephemeral path) will fail with "config file not found" on the next Job start. Re-run `claude login` per PVC via [[Agent - Refresh Claude OAuth Login]] after bumping `lib/claude`. A failure to resolve `$HOME` in the pod (rare) now manifests as a hard `Run` error rather than silent ephemeral fallback.

## v0.62.2

- feat(task/executor): add pre-spawn task-type filter — executor computes effective type set (`taskType` ∪ `taskTypes`) from the Config CR and publishes a synthetic failure (phase=ai_review, assignee="" cleared) when a task's `task_type` is absent or mismatched; no Job is spawned and trigger_count/retry_count are not bumped; **NOTE:** tasks without a `task_type` frontmatter field will now be rejected on first event delivery when the agent has `taskType`/`taskTypes` configured — operators must add `task_type` to legacy task templates before deploying this change

## v0.62.1

- feat(task/controller): write `previous_assignee` frontmatter field on every assignee-clear path (trigger cap, retry cap, needs_input) — captures the pre-clear agent name so operator-inbox queries can group parked tasks by parked-by-agent without parsing body content; persists across operator re-delegation

## v0.62.0

- feat: Config CRD gains optional `spec.taskTypes []string` field; `ConfigSpec.Validate` accepts either `taskType` or `taskTypes` (at-least-one-of); `ConfigSpec.Equal` detects `taskTypes` slice diffs; OpenAPIV3Schema gains `taskTypes` array property with item pattern + maxLength and CEL at-least-one-of rule; `taskType` field is marked deprecated in doc comments; generated deepcopy and applyconfiguration updated

## v0.61.6

- feat(task/executor): add POST `/oauth-probe/trigger` HTTP endpoint — fires the OAuth probe loop on demand with fire-and-forget and single-flight semantics; the runner instance is shared with the existing weekly cron so behavior is identical regardless of invocation path

## v0.61.5

- fix(task/executor): OAuth probe task identifiers are now deterministic UUIDv5s per agent (previously `probe-<agent>` literal strings, which the vault scanner silently rewrote with random UUIDs on each scan — producing merge conflicts and breaking `update-frontmatter` re-triggers). Probe vault files remain at the human-readable path `tasks/probe-<agent>.md` (driven by Title, not by task_identifier).

## v0.61.4

- feat(task/executor): add weekly OAuth probe cron (`OAUTH_PROBE_CRON_EXPRESSION`, default `0 0 8 * * 1`) — publishes `create-task` + `update-frontmatter` commands per Config CR on each tick to keep agent PVC OAuth credentials warm; failed probes escalate via existing `human_review` route; new agents auto-enrolled at next tick

## v0.61.3

- chore: bump direct dependencies across `lib/`, `task/controller`, `task/executor`, `agent/claude`, `agent/code`, `agent/gemini`. Notable: `bborbe/time v1.25.10 → v1.27.0`, `bborbe/vault-cli v0.58.1 → v0.64.0`, `bborbe/kafka v1.22.12 → v1.22.15`, `bborbe/errors v1.5.11 → v1.5.13`. Indirect bumps in `IBM/sarama`, `getsentry/sentry-go`, and various `bborbe/*` transitives.

## v0.61.2

- fix(lib/delivery): wrap the failure-section `Reason:` body in a fenced code block. Previously rendered as a single inline bullet, which produced unreadable output in Obsidian / GitHub / generic CommonMark viewers when `Result.Message` was long or contained markdown-confusing characters (asterisks, brackets, braces — common in JSON tails from `lib/v0.61.1`). The fence preserves monospace formatting, prevents stray markdown interpretation, and gives operators a one-click select-and-copy block. Empty-reason fallback keeps its inline form.

## v0.61.1

- fix(lib/claude): surface bounded stdout tail from failed `claude` CLI subprocess runs — ring buffer captures last 5 non-empty stdout lines (512 bytes/line max), joined with ` | `, so the `## Failure` body section on the task page contains the actual CLI diagnostic output (auth failures, rate-limit events, API errors) instead of the empty `claude CLI failed: : exit status 1` rendering caused by stream-json's always-empty stderr

## v0.61.0

- feat: Config CRD gains required `spec.taskType` string field; `ConfigSpec.Validate` rejects empty, non-`^[a-z0-9-]+$`, and >63-char values; `ConfigSpec.Equal` detects `TaskType` diff; OpenAPIV3Schema updated with pattern and maxLength; applyconfiguration regenerated with `WithTaskType` builder; `agent/claude` manifest migrated to `taskType: claude`
- fix(lib/delivery): `passthroughContentGenerator` now writes a `## Failure` body section on `AgentStatusFailed` and `AgentStatusNeedsInput`, mirroring the existing behavior of `fallbackContentGenerator` and `sectionContentGenerator`. Previously, agents using the passthrough generator (e.g. pr-reviewer) lost the failure reason whenever `result.Output` was empty — operators had to dig through TTL-cleaned pod logs to diagnose. Live incident: pr-reviewer task `712b7974-cfbf-5999-a1fc-6946207e21c3` on 2026-05-12 — Claude API 401 → empty task body. Adds table-driven regression test covering every generator × non-success status.

## v0.60.0

- feat: reset `trigger_count` and `retry_count` to 0 when vault scanner detects `assignee` transition from empty to named (operator re-delegation refills spawn budget automatically)
- docs: update `task-flow-and-failure-semantics.md` and `controller-design.md` to document `assignee: ""` as single inbox signal and new escalation shape

## v0.59.0

- feat: clear `assignee` to empty on all escalation paths (trigger cap, retry cap, needs_input) so parked tasks surface in operator inbox by assignee filter
- feat: preserve lifecycle phase on cap escalations — trigger-count and retry-count cap no longer overwrite phase to `human_review`; phase stays at the stage where the cap fired

## v0.58.1

- chore: bump Go toolchain 1.26.2 → 1.26.3 across all modules (stdlib CVE fixes GO-2026-4918, GO-2026-4971)

## v0.58.0

- chore(release): align lib + root tag numbers — paired tag bump to resync `lib/vX.Y.Z` with `vX.Y.Z` at the same commit (latest published `lib/v0.57.0` was stale; this unblocks downstream consumers)
- refactor(lib): move `CreateTaskCommand` (→ `task.CreateCommand`), `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand`, and `BodySection` to `lib/command/task` sub-package; remove flat `agent_task-commands.go`
- refactor(task/controller): migrate command executors to `lib/command/task` types
- refactor(task/executor): migrate `ResultPublisher` to `lib/command/task` types

## v0.54.20

- feat(lib): add `lib/command/task` package with `CreateCommand`, `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand` types, `Validate` methods, and typed command senders

## v0.54.19

- feat(task/controller): create-task executor now writes vault task files at `tasks/{title}.md`; re-validates `Title` on receive with WARN + UUID-path fallback on failure or path collision

## v0.54.18

- feat(lib): add `Title` field to `CreateTaskCommand` with cross-platform-safe validation rules enforced by a new `Validate(ctx)` method
- feat(lib): add `CreateTaskCommandSender` interface and `NewCreateTaskCommandSender` factory with validate-before-send invariant

## v0.54.17

- fix(ci): point `actions/setup-go` at `lib/go.mod` instead of nonexistent root `go.mod`. Multi-module repo has go.mod files only in subdirs (lib, agent/*, task/*); CI was failing immediately at `Set up Go` step.

## v0.54.16

- fix(task/executor): include YAML frontmatter when rendering `TASK_CONTENT` for spawned Jobs. Previously only the body was emitted, causing pr-reviewer (and any agent that reads frontmatter fields like `clone_url`, `ref`, `base_ref`) to fail with `clone_url is missing from task frontmatter`. The executor now emits `---\n<yaml>\n---\n<body>` matching the controller's result writer; round-trips through `lib.ParseMarkdown` cleanly.
- chore(task/controller): drop `agent-task-controller-netpol.yaml` — the K3s+Flannel cluster does not enforce NetworkPolicies; gateway-secret auth on git-rest is the operative defense. Goal [[Enable NetworkPolicy enforcement on K3s cluster]] tracks reintroducing real enforcement.

## v0.54.15

- feat(task/controller): gitrestclient sends `X-Gateway-Secret` + `X-Gateway-Initator` headers on `/api/v1/*` calls when `GATEWAY_SECRET` is set; matches git-rest spec 004 auth contract
- feat(task/controller): add `GATEWAY_SECRET` env / `--gateway-secret` flag (sourced from `OBSIDIAN_OPENCLAW_GATEWAY_SECRET` teamvault key in dev/prod manifests)

## v0.54.14

- feat(task/controller): delete `pkg/gitclient/` and `pkg/conflict/` — all vault I/O now flows through git-rest HTTP API
- feat(task/controller): remove `GIT_URL`, `GIT_BRANCH`, `GEMINI_API_KEY` flags and manifests — git-rest holds the SSH key
- docs: update `docs/controller-design.md` — rewrite vault write sections to reflect git-rest architecture

## v0.54.13

- feat(task/controller): remove SSH key volume from StatefulSet manifest; add `GIT_REST_URL` and `USE_GIT_REST=true` env vars
- feat(task/controller): add `NetworkPolicy` restricting git-rest ingress to agent-task-controller pods only
- docs: add `scenarios/use-git-rest-for-vault-writes.md` — full E2E acceptance criteria for spec-018

## v0.54.12

- feat(task/controller): adapt vault scanner and `FindTaskFilePath` to use `gitclient.GitClient` interface methods instead of `os.DirFS` — enables git-rest HTTP mode
- feat(task/controller): add `USE_GIT_REST` and `GIT_REST_URL` flags to `main.go`; feature flag switches all vault I/O to git-rest HTTP API when enabled
- feat(task/controller): controller `/readiness` reflects git-rest health when `USE_GIT_REST=true`

## v0.54.11

- feat(task/controller): extend `gitclient.GitClient` interface with `ListFiles`, `ReadFile`, `WriteFile` for filesystem-agnostic vault access
- feat(task/controller): add `gitRestGitClientAdapter` in `pkg/gitrestclient` — drop-in `GitClient` implementation backed by git-rest HTTP API

## v0.54.10

- feat(task/controller): add `pkg/gitrestclient` — HTTP client for git-rest API with Get/Post/Delete/List/IsReady, retry with exponential backoff, and Counterfeiter mock
- feat(task/controller): add `controller_gitrest_calls_total` and `controller_kafka_consume_paused_total` Prometheus metrics

## v0.54.9

- feat(lib/claude): add `Resolve()` method to `ClaudeConfigDir` and `AgentDir` that expands a leading `~/` to the user's home directory. `claude-runner.go` now calls `Resolve()` at the env-var emission and working-directory boundaries, so consumers can declare `default:"~/.claude"` (or pass `~/.claude` via env) and have the path correctly expand. Backwards-compatible — existing `.String()` callers see no change.

## v0.54.8

- chore(task/executor): migrate from tools.go to tools.env + Makefile @version pattern; drop obsolete replace directives; bump bborbe/metrics to v0.5.2

## v0.54.7

- chore(task/controller): migrate from tools.go to tools.env + Makefile @version pattern; bump bborbe deps (errors v1.5.11, boltkv v1.12.5, cqrs v0.5.1, http v1.26.11, kafka v1.22.12, kv v1.19.6, log v1.6.12); add GODEBUG=gotypesalias=1 to errcheck for Go 1.24+ generic type alias compatibility

## v0.54.6

- chore(agent/code): migrate from tools.go to tools.env + Makefile @version pattern; drop obsolete replace directives (cellbuf, go-header, go-diskfs, ginkgolinter); bump bborbe deps (errors v1.5.11, cqrs v0.5.1, kafka v1.22.12, sentry v1.9.16, service v1.9.10, time v1.25.10, vault-cli v0.58.1)

## v0.54.5

- chore(agent/claude): migrate from tools.go to tools.env + Makefile @version pattern; drop obsolete replace directives; bump bborbe deps (errors v1.5.11, cqrs v0.5.1, kafka v1.22.12, sentry v1.9.16, service v1.9.10, time v1.25.10, vault-cli v0.58.1)

## v0.54.4

- chore(agent/gemini): migrate from tools.go to tools.env + Makefile @version pattern; drop obsolete replace directives; bump bborbe deps (errors v1.5.11, cqrs v0.5.1, kafka v1.22.12, sentry v1.9.16, service v1.9.10, time v1.25.10, vault-cli v0.58.1)

## v0.54.3

- feat(lib/claude): add `PluginInstaller` + `PluginCommander` + `PluginSpec` — reusable Claude plugin install/update helper, ported from `code-reviewer/agent/pr-reviewer/pkg/plugins` (Phase 2 promotion). Install path runs `marketplace add` + `plugin install`; update path runs `marketplace update` + `plugin update` as soft failures (warn, don't fail). Same fast-path semantics as the local impl. Available to any agent wrapping `claude` CLI.

## v0.54.2

- feat(task/controller): add `CreateTaskCommand` executor — controller now materializes vault task files on Kafka command; idempotent (no-op if file already exists), validates required frontmatter fields (assignee, status)

## v0.54.1

- feat(lib): add `CreateTaskCommand` and `CreateTaskCommandOperation = "create-task"` so producers can request vault task creation via Kafka without embedding vault git logic

## v0.54.0

- refactor(lib): move `AgentStatus`, `AgentResultInfo`, `ResultDeliverer` from `lib/delivery` to `lib` root — removes potential import cycle for new framework primitives; `lib/delivery` still hosts impls (Noop / File / Kafka deliverers, ContentGenerator)
- feat(lib): add agent framework primitives — `Markdown`/`Section` types with `ParseMarkdown`/`Marshal`/`AddSection`/`ReplaceSection`/`InsertSection` mutations; `Step` interface + `Result`; `StepRunner`; `Phase` + `NewPhase`; `Agent` + `NewAgent` dispatcher with unsupported-phase fail-loud sentinel; generic `ExtractSection[T]` / `MarshalSectionTyped` helpers for typed JSON in body sections. Step is the single architectural primitive — code-heavy and AI-heavy agents share the same interface; AI-heavy steps wrap LLM calls, code-heavy steps are pure Go. Multi-step phases enable mid-phase crash resume via guard-based skip-or-run on saved task state.
- feat(lib): add AI step kinds — `AIParser` interface + generic `ParseStep[T]` in lib root for fuzzy markdown → typed Go struct boundary translation (planning-phase pattern); `claude.AgentStepConfig` + `claude.NewAgentStep` in `lib/claude` wraps a Claude CLI invocation as an `agentlib.Step` (single-Claude-call agent pattern, e.g. trade-analysis / pr-reviewer). Concrete `AIParser` impls (Gemini structured output, Claude JSON mode) live in their respective sub-packages.
- feat(lib): add `delivery.NewPassthroughContentGenerator` — returns `result.Output` verbatim with status/phase frontmatter applied on top. Used by the new agent framework: `StepRunner` produces the full marshaled task in `result.Output`; the deliverer must publish it as-is rather than splice into a `## Result` section (which is what `FallbackContentGenerator` does for the legacy single-shot pattern).
- refactor(agent/claude): migrate to the new agent framework. Single `claude.NewAgentStep` per phase, three phases (planning, in_progress, ai_review) sharing the same step preserves CRD trigger compatibility and existing per-invocation behavior (run Claude, mark done). Both Kafka mode (main.go) and file mode (cmd/run-task/main.go) updated. Factory replaces `CreateTaskRunner` with `CreateClaudeRunner`; uses `PassthroughContentGenerator` for both deliverer kinds. Existing tests pass unchanged. Becomes the canonical AI-heavy agent reference — future agents (trade-analysis, pr-reviewer) follow this shape.
- feat(agent/code): canonical pure-code agent reference. Three phases × 1 pure-Go step each (PlanStep / ExecuteStep / VerifyStep), no LLM dependency. Math agent (operation + a + b → result + verify) with typed JSON section handoffs. Demonstrates that the framework works without any AI deps — useful template for orchestration agents, data agents, validation agents. Verified end-to-end via `cmd/run-task` against a frontmatter-only task file.
- feat(agent/gemini): canonical boundary-translator agent reference. Three phases — planning uses generic `lib.NewParseStep[Plan]` wrapping a Gemini-backed `AIParser` (ported from `trading/agent/backtest/pkg/task-content-parser.go`, structured output via `google.golang.org/genai` with reflective schema derivation); in_progress + ai_review are pure-Go ExecuteStep + VerifyStep. Same math domain as agent/code but takes fuzzy human input (e.g. "Compute 3 plus 5"). Demonstrates the canonical AI usage pattern: LLM only at the boundary, deterministic code everywhere else. Concrete `Parser` lives in `agent/gemini/pkg/parser` per Rule of Three — promote to `lib/gemini/` when a 2nd consumer emerges.
- refactor(lib + agents): main.go slim-down. (1) `agentlib.PrintResult` in lib root replaces 6 duplicated `printResult` helpers across the 3 reference agents × 2 entry points. (2) `claude.ParseKeyValuePairs` in lib/claude replaces duplicated parser used by claude main + cmd/run-task. (3) `claude/pkg/factory.CreateAgent` collapses the runner + step + 3-phase agent assembly into one call; `CreateDeliverer` wraps the Kafka-or-Noop deliverer pattern with cleanup. (4) `application.TaskID` switches from `string` to `agentlib.TaskIdentifier` (argumentv2 unmarshals the named string type directly — drops the inline `agentlib.TaskIdentifier(...)` cast). (5) `application.Phase` becomes a typed `domain.TaskPhase` field with `arg/env/default` tags — drops the `os.Getenv("PHASE")` + manual default block in 3 main.go files. (6) `agentlib.Agent.Run` takes `domain.TaskPhase` instead of string; `Phase.Name` and `NewPhase` parameter are also `domain.TaskPhase`. Net: agent/claude main.go drops from 167 → 96 lines.
- feat(lib): `TaskFrontmatter.Int(key)` and `TaskFrontmatter.String(key)` generic accessors — same `int|float64` switch pattern as the existing typed methods (RetryCount/MaxRetries/etc), but for ad-hoc fields without dedicated typed getters. Used by agent/code's PlanStep to read frontmatter operands.
- refactor(agent/code + agent/gemini): align factory shape with agent/claude. Both now expose `CreateAgent(...)` (assembles 3-phase agent) + `CreateDeliverer(...)` (Kafka-or-Noop with cleanup). main.go and cmd/run-task/main.go drop their inline assembly + createDeliverer methods. Gemini's `CreateAgent` takes the `agentlib.AIParser` so the parser stays application-controlled (lifecycle / config). Net: code main.go 111→76 lines, gemini main.go 124→85 lines.

## v0.53.5

- feat(lib): add NewSectionContentGenerator(heading) to lib/delivery for phase-aware agents writing custom section headings (## Plan, ## Review, etc.) — same status-frontmatter + failure-section semantics as FallbackContentGenerator

## v0.53.4

- feat(lib): add AgentStatusInProgress for step-level in-place saves; preserves phase frontmatter, ignores NextPhase. Enables multi-step phase handlers to commit intermediate state without triggering phase advance.

## v0.53.3

- fix(lib): `kafkaResultDeliverer` now keeps `status: in_progress` when an agent returns `status: done` with a `NextPhase` that requests a non-terminal phase (planning/in_progress/ai_review/human_review); only `NextPhase: done` or empty sets `status: completed` — unblocks multi-phase agents from the post-phase-1 stall (live dev bug observed on hypothesis agent task `cde7365b` 2026-04-24)

## v0.53.2

- feat(lib): Agents can request a phase transition via new `NextPhase` field on `AgentResultInfo` and `AgentResultLike` — `kafkaResultDeliverer` writes the requested phase on `status: done`; failure/needs_input paths continue to escalate to `human_review` (074/077 rules win).
- BREAKING: `AgentResultLike` interface gains a `GetNextPhase() string` method — downstream consumers of `lib/claude` (pr-reviewer, backtest-agent, trade-analysis, hypothesis) must add this method to their concrete `AgentResult` types when bumping to this lib version.

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

## v0.53.1

- fix(lib): agent-returned `status: failed` now routes to `phase: human_review` (was: `ai_review`) and writes a dedicated `## Failure` body section with the failure reason — symmetric with `PublishFailure` behavior for K8s Job crashes
- fix(lib): `kafkaResultDeliverer.DeliverResult` no longer emits `phase: ai_review` on failure; `needs_input` and `failed` both route to `human_review` (retries are the controller's job via `trigger_count`)

## v0.53.0

- feat: Inject BUILD_GIT_VERSION (from `git describe --tags --always --dirty`) into all service images and surface it in startup logs of task/controller and task/executor.

## v0.52.7

- fix: reorder `resultWriter.applyRetryCounter` to run `trigger_count` cap escalation BEFORE the `spawn_notification` early return; fixes a live-observed regression of the 072 hotfix where agent result writes that inherited `spawn_notification: true` via `mergeFrontmatter` skipped the cap check and reverted `phase: human_review` to `ai_review` (task `ba1bad61-5ad4-48e7-ad05-e15ba8dfbfb9` on dev, controller v0.52.4); adds a regression-guard unit test

## v0.52.6

- fix(executor): `PublishFailure` now escalates K8s Job failures to `phase: human_review` (was: `ai_review`) and records the failure reason in a `## Failure` body section with timestamp and job name
- feat(lib): `UpdateFrontmatterCommand` gains an optional `Body` field (`*BodySection`); controller's executor applies `ReplaceOrAppendSection` when set — backward-compatible, nil Body preserves current frontmatter-only behavior

## v0.52.5

- feat(executor): inject `PHASE` env var into spawned agent Jobs, sourced from task frontmatter `phase` field (empty string when absent); enables per-phase dispatch in phase-aware agents without parsing `TASK_CONTENT` frontmatter

## v0.52.4

- fix: enforce `trigger_count >= max_triggers` escalation server-side in `resultWriter.applyRetryCounter` so `phase: human_review` stays sticky across stale-payload result writes; adds `## Trigger Cap Escalation` section with dedup; adds dedup to the existing `## Retry Escalation` path; unit-tested for the live dev clobber scenario

## v0.52.3

- test: add controller integration tests proving UpdateFrontmatterCommand partial-merge semantics preserve trigger_count across spawn-notification and failure sequences

## v0.52.2

- fix: migrate executor PublishSpawnNotification and PublishFailure from full-frontmatter rewrite to UpdateFrontmatterCommand (partial keys only), eliminating clobber of trigger_count; delete PublishRetryCountBump from ResultPublisher interface and implementation (spec 016, builds on spec 015 atomic primitives)

## v0.52.1

- fix: rename CommandOperation strings `increment_frontmatter` → `increment-frontmatter` and `update_frontmatter` → `update-frontmatter` so they pass cqrs regex `^[a-z][a-z-]*$`; unblocks trigger_count increment publish; adds regression test enumerating all lib CommandOperation constants against base.CommandOperation.Validate

## v0.52.0

- feat: trigger_count / max_triggers frontmatter fields bound executor spawn loops; atomic IncrementFrontmatterCommand makes counter non-idempotent; retry_count silently deprecated (executor no longer bumps it)

## v0.51.0

- feat: add AtomicReadModifyWriteAndCommitPush to GitClient interface and implementation for read-modify-write under a single mutex
- feat: add IncrementFrontmatterExecutor command handler that atomically increments a named frontmatter field and escalates phase to human_review when trigger_count reaches max_triggers
- feat: add UpdateFrontmatterExecutor command handler for atomic partial-key frontmatter updates without clobbering concurrent writes
- feat: wire both new executors into CreateCommandConsumer factory with gitClient and taskDir parameters
- feat: export FindTaskFilePath, ExtractFrontmatter, ExtractBody helpers from result package for cross-package reuse
- feat: add FrontmatterCommandsTotal Prometheus counter for increment_frontmatter and update_frontmatter operations

## v0.50.0

- feat: add TriggerCount/MaxTriggers frontmatter accessors and IncrementFrontmatterCommand/UpdateFrontmatterCommand Kafka command types to lib

## v0.49.0

- feat: task-event handler consults per-Config trigger phases/statuses with default fallback; removes hardcoded allowedPhases list

## v0.48.0

- feat: Config CRD gains optional spec.trigger with phases/statuses lists; ConfigSpec.Equal and Validate updated; AgentConfiguration.Trigger wired from config resolver; deepcopy regenerated

## v0.47.0

- feat: priorityClassName field on Config CRD enables K8s-native concurrency cap via ResourceQuota; executor stamps value onto spawned Job PodTemplates; agent-claude bundle includes PriorityClass and per-env ResourceQuota manifests

## v0.46.0

- feat: add `priorityClassName` field to `ConfigSpec`, `AgentConfiguration`, and CRD OpenAPIV3Schema to enable K8s-native concurrency control via PriorityClass + ResourceQuota

## v0.45.2

- fix: fallbackContentGenerator.Generate now trusts AgentResultInfo.Output verbatim when non-empty, eliminating the double `## Result` heading and duplicated `**Message:**` line observed in 2026-04-20b smoke writebacks
- refactor: split fallbackContentGenerator internals into applyStatusFrontmatter + buildMinimalResultSection helpers; public ContentGenerator interface unchanged

## v0.45.1

- fix: ReplaceOrAppendSection now coalesces multiple existing `## Result` sections into exactly one, fixing duplicate sections observed in 2026-04-20 smoke writebacks
- refactor: split markdown section helpers into HasSection, AppendSection, ReplaceSection (ReplaceOrAppendSection now composes them); public API unchanged

## v0.45.0

- Generalize ResultDeliverer and TaskRunner interfaces with AgentResultLike type parameter
- Add AgentResultLike constraint interface with GetStatus/GetMessage/GetFiles/RenderResultSection
- Add getter methods to AgentResult to satisfy AgentResultLike
- Wire agent/claude to use generic claude.TaskRunner[claude.AgentResult] and claude.ResultDeliverer[claude.AgentResult]
- Update golang.org/x/* dependencies (crypto, net, sys, tools, vuln, etc.)
- Bump counterfeiter to v6.12.2

## v0.44.1

- fix: controller result writer no longer increments retry_count — counter is maintained by executor at spawn time, preventing inflation from kubectl job deletions (spec 011)
- refactor: remove spec 010's phase==human_review guard from result writer — dead code after spawn-time accounting

## v0.44.0

- feat: executor publishes retry_count bump to agent-task-v1-request before spawning K8s Job (spawn-time accounting, spec 011)

## v0.43.2

- fix: executor `IsJobActive` now queries the same `agent.benjamin-borbe.de/task-id` label that `SpawnJob` writes onto the Job metadata, fixing the respawn loop where the controller repeatedly spawned duplicate jobs because it could not detect the existing one
- test: add integration test verifying `SpawnJob` + `IsJobActive` share the same label contract
- chore: add go.mod replace directives to work around osv-scanner compile error in containerd@v1.7.30

## v0.43.1

- docs: update agent/claude workflow.md to distinguish `needs_input` (semantically impossible/underspecified task) from `failed` (infrastructure error eligible for retry)

## v0.43.0

- feat: distinguish `needs_input` (task-level, human_review immediately, no retry) from `failed` (infra-level, retry up to max_retries)
- fix: prose-wrapped Claude output no longer synthesises an infra failure; result parser extracts the last balanced JSON object from any surrounding text
- fix: controller result writer skips retry counter when incoming result already has `phase: human_review`

## v0.42.1

- chore: remove unused duplicate `lib/claude.TaskContent` type (use `lib.TaskContent`)
- refactor: replace `errors.Wrapf` with `errors.Wrap` in lib validation helpers (no format verbs)
- refactor: inject `CurrentDateTimeGetter` into `CreateKafkaResultDeliverer` factory for testability
- fix: use `glog.V(2).Infof` consistently inside the V(2)-guarded block in `lib/claude/log-tool-use.go`
- chore: reorder `ClaudeModel` type above its constants

## v0.42.0

- feat: executor watches batch/v1 Jobs via shared informer and publishes synthetic failure results for OOMKilled, evicted, and backoffLimit-exceeded Jobs; feeds controller's retry counter identically to agent-published failures
- feat: executor deletes terminal Jobs after publishing synthetic failure result, preventing stale Job accumulation
- fix: executor taskStore is cleaned up on completed task events so job informer does not emit spurious synthetic failures after agent success

## v0.41.0

- feat: executor adds `agent.benjamin-borbe.de/task-id` label to spawned K8s Jobs for informer lookup
- feat: `SpawnJob` returns `(string, error)` with the spawned job name for spawn-notification publishing
- feat: executor publishes spawn notification to `agent-task-v1-request` after spawning, writing `current_job` and `job_started_at` to task file without incrementing retry counter
- feat: thread-safe `TaskStore` stores original task on spawn for informer failure publishing
- feat: executor checks `current_job` frontmatter field for idempotent spawn detection alongside K8s `IsJobActive`
- refactor: extract `parseAndFilter` and `spawnIfNeeded` helpers in `ConsumeMessage` to satisfy funlen limit

## v0.40.0

- feat: add `SpawnNotification()` and `CurrentJob()` accessors to `TaskFrontmatter` for executor job-spawn tracking
- feat: controller skips retry counter increment when result carries `spawn_notification: true`

## v0.39.0

- BREAKING: `agent.benjamin-borbe.de/v1` `AgentResources` now has nested `requests` and `limits` sub-objects instead of flat `cpu`/`memory`/`ephemeral-storage`. Update existing `Config` manifests before re-applying. Apply the updated CRD first, then re-apply any `Config` resources.
- feat: Propagate `Resources` from `Config` CRD (cpu/memory/ephemeral-storage, requests and limits independent) to spawned agent Job container; fixes OOMKill of Claude-Code-based agents that inherited the namespace LimitRange default of 50Mi.

## v0.38.0

- feat: Implement retry counter in `task/controller` `ResultWriter` — increments `retry_count` on each non-completed result write and escalates to `phase: human_review` with `## Retry Escalation` section when `retry_count >= max_retries` (default 3)

## v0.37.0

- feat: Add `RetryCount()` and `MaxRetries()` typed accessors to `lib.TaskFrontmatter` with int/float64 dual-source handling (YAML and Kafka paths)
- fix: `FallbackContentGenerator` now sets `phase: ai_review` on failure instead of `phase: human_review`, aligning file-based delivery with Kafka delivery and enabling controller retry counter

## v0.36.0

- feat: Add `agent-claude` service — headless Claude Code CLI runner for task execution; spawns `claude --print --output-format stream-json` with configurable model, allowed tools, env, working directory; publishes results via Kafka (when TASK_ID is set) or falls back to noop
- feat: Add `lib/delivery` package — generic `ResultDeliverer` (noop/file/kafka) and `ContentGenerator` with markdown frontmatter helpers; agents in other repos can depend on it for Kafka task-update publishing
- feat: Add `lib/claude` package — generic Claude CLI runtime (`ClaudeRunner`, `TaskRunner`, `BuildPrompt`, `Instructions` XML rendering, `AgentResult` types) moved out of `agent-claude/pkg/` so multiple agent services can share it
- feat: Add agents config handler in task/controller
- fix: Task file write via Kafka pipeline
- docs: Move agent-crd-specification and related docs to `specs/`
- docs: Task-retry design idea

## v0.35.0

- feat!: Rename AgentConfig CRD to Config and move the API group from `agents.bborbe.dev` to `agent.benjamin-borbe.de` to match the bborbe convention (`alerts.monitoring.benjamin-borbe.de`, `schemas.cdb.benjamin-borbe.de`, …); CRD is now `configs.agent.benjamin-borbe.de` with short name `cfg`; no cluster migration needed because the old CRD was never applied
- feat: Example Config CR `agent-claude` under `task/executor/k8s/`; trading-specific CRs (backtest-agent, trade-analysis) ship from the trading repo

## v0.34.0

- feat: Replace hardcoded `agentConfigs` slice in `task/executor/main.go` with a live in-memory store fed by a Kubernetes informer on `Config` resources; introduce `ConfigResolver` for per-lookup conversion with branch tagging; wire `K8sConnector.Listen` via `SharedInformerFactory`; executor binary has no compiled-in agent catalog
- feat: Example Config CRs under `task/executor/k8s/` (agent-claude); trading-specific CRs (agent-backtest-agent, agent-trade-analysis) moved to the trading repo
- feat: RBAC extended to grant executor ServiceAccount cluster-scoped write on `customresourcedefinitions` (self-install) and namespace-scoped `get/list/watch` on `configs.agent.benjamin-borbe.de`

## v0.33.0

- feat: Introduce AgentConfig CRD (`agents.bborbe.dev/v1`) with Go types under `task/executor/k8s/apis/agents.bborbe.dev/v1/`, typed clientset/informers/listers/applyconfigurations generated via `k8s.io/code-generator`, and `K8sConnector` with `SetupCustomResourceDefinition` for CRD self-install (create or update) on startup

## v0.33.0

- docs: Promote `spec.env`, `spec.secretName`, `spec.volumeClaim`, `spec.volumeMountPath` from "Future Extensions" to first-class AgentConfig CRD fields in agent-crd-specification.md; update trade-analysis example to reflect real PVC/secret wiring; align Who-Uses-the-CRD table with job-creator field usage

## v0.32.0

- feat: Add SecretName field to AgentConfiguration; SpawnJob injects per-agent K8s secret as envFrom on the container when SecretName is set; backtest-agent and trade-analysis-agent configured with their respective secrets

## v0.31.0

- feat: Validate task_identifier in vault scanner — non-UUID and duplicate identifiers are automatically replaced with generated UUIDs; valid unique UUIDs are preserved unchanged

## v0.30.0

- feat: Add optional PVC volume mount to AgentConfiguration (VolumeClaim, VolumeMountPath fields); SpawnJob mounts the PVC into agent containers when configured, returns error if VolumeMountPath is missing

## v0.29.0

- refactor: Remove `ANTHROPIC_API_KEY` plumbing from task/executor; trade-analysis-agent now authenticates via `claude /login` instead of API key env var (k8s secret entry, env var, main.go field, and PLACEHOLDER references in dev.env/prod.env all removed)

## v0.28.0

- feat: Add `agent_build_info` Prometheus gauge (`lib.BuildInfoMetrics`) and wire `BUILD_GIT_COMMIT` / `BUILD_DATE` into task/controller + task/executor so Prometheus can report the running commit per service

## v0.27.0

- feat: Add per-agent AgentConfiguration type to task/executor so each agent receives only its required API keys (backtest-agent gets GEMINI_API_KEY, trade-analysis-agent gets ANTHROPIC_API_KEY) instead of sharing a single key across all agents

## v0.26.0

- feat: Add stage filter to task/executor so each executor (dev/prod) only spawns jobs for tasks whose frontmatter `stage` matches its branch; tasks without `stage` default to `prod`

## v0.25.0

- feat: Add Prometheus counters to task/controller (scan cycles, tasks published, results written, git push retries, conflict resolutions) and task/executor (task events consumed, jobs spawned) for pipeline observability

## v0.24.2

- docs: Fix TASK_CONTENT example in agent-job-interface.md to show body-only (no frontmatter)
- docs: Add frontmatter merge, git serialization, push retry, and LLM conflict resolution to controller-design.md
- complete spec-006 (result-writer-conflict-resolution)
- add Prometheus metrics prompt for controller and executor

## v0.24.1

- fix: Merge existing task file frontmatter with agent-provided frontmatter in ResultWriter so keys like assignee, tags, and custom fields are preserved on writeback

## v0.24.0

- feat: Add Gemini LLM conflict resolver to task/controller so rebase merge conflicts are automatically resolved via the Gemini API (gemini-2.5-flash) before retrying push

## v0.23.1

- refactor: Replace in-memory DuplicateTracker with K8s Job label lookup (IsJobActive) in task/executor so deduplication survives restarts and completed tasks can be retriggered

## v0.23.0

- feat: Add push-retry with fetch+rebase in task/controller gitClient so concurrent pushes recover automatically; conflict markers abort rebase and return an error

## v0.22.5

- fix: Serialize concurrent git operations in task/controller with sync.Mutex and AtomicWriteAndCommitPush to prevent dirty commits when scanner and result writer run simultaneously

## v0.22.4

- fix: Enable CQRS result sending in task result executor so command senders receive processing confirmation

## v0.22.3

- fix: Add diagnostic logging to task result executor and result writer for debugging e2e pipeline failures

## v0.22.2

- refactor: Replace hand-built batchv1.Job struct in JobSpawner with bborbe/k8s fluent builders, adding TTL auto-cleanup (600s), pod template labels, and builder validation

## v0.22.1

- fix: Tolerate duplicate YAML frontmatter keys in VaultScanner by deduplicating before unmarshal (last value wins)

## v0.22.0

- feat: Change K8s Job naming in task executor from `agent-{taskID[:8]}` to `{assignee}-{YYYYMMDDHHMMSS}` to eliminate retrigger collisions; inject time via `CurrentDateTimeGetter`

## v0.21.1

- fix: Remove Object from Task.Validate to unblock agent result writeback
- fix: Use teamvaultPassword (not teamvaultUrl) for GEMINI_API_KEY secret
- fix: Rename GEMINI_API_KEY to GEMINI_API_KEY_KEY env var for teamvault resolution

## v0.21.0

- feat: Pass GEMINI_API_KEY from K8s Secret through executor Deployment to spawned agent Jobs

## v0.20.15

- fix: Add imagePullSecrets to spawned K8s Jobs for private registry auth

## v0.20.14

- feat: Add backtest-agent to task/executor assignee→image map
- fix: Derive agent image tag from BRANCH env var at runtime (supports dev/prod)
- fix: Update scenarios to use OpenClaw vault paths (tasks/ not 24 Tasks/)

## v0.20.13

- feat: Add backtest-agent to task/executor assignee→image map (hardcoded tag, superseded by v0.20.14)
## v0.20.12

- fix: Rename command operation from `update-result` to `update` to match CQRS convention
- docs: Update controller-design, job-creator-design, kafka-schema-design, agent-job-lifecycle to reflect current architecture (remove prompt layer, fix result flow)

## v0.20.11

- fix: Rename command operation from PascalCase `UpdateResult` to kebab-case `update` to comply with CQRS naming convention

## v0.20.10

- fix: Sanitize agent result content to escape bare `---` lines that would corrupt task file YAML frontmatter

## v0.20.9

- fix: Inject CurrentDateTimeGetter into taskPublisher to eliminate time.Now() in production code
- fix: Remove time.Local and format.TruncatedDiff from main_test.go to eliminate data race with gexec.Build

## v0.20.8

- Fix git pull with --rebase for diverged branches (controller commits locally)

## v0.20.7

- Fix git pull strategy error by adding --ff-only flag

## v0.20.6

- refactor: Rename TaskFile to Task, introduce TaskContent named type with non-empty validation

## v0.20.5

- Improve trivy ignorefile resolution with local→root→none wildcard fallback
- Add dark-factory prompt for TaskFile→Task rename

## v0.20.4

- Use ROOTDIR for trivy ignorefile, remove per-subdir .trivyignore copies
- Upgrade go-git to v5.17.1 in task/executor (CVE fix)

## v0.20.3

- refactor: Update task/executor handler and job spawner to consume lib.TaskFile from Kafka, reading status/phase/assignee via frontmatter accessors and passing content/UUID as TASK_CONTENT/TASK_ID env vars to K8s Jobs

## v0.20.2

- refactor: Update task/controller scanner, publisher, and sync loop to use lib.TaskFile; parse frontmatter as generic map, extract markdown body via extractBody helper, pass unknown status values through as strings

## v0.20.1

- refactor: Merge Task and TaskFile into single TaskFile type with base.Object[base.Identifier] embed and stable TaskIdentifier business key; remove TaskContent, TaskName, and old Task types; change Phase() accessor to return *domain.TaskPhase

## v0.20.0

- feat: Wire CQRS command consumer in task/controller to consume agent-task-v1-request and write results to vault via ResultWriter
- feat: Add DataDir and NoSync CLI flags to task/controller for BoltDB Kafka offset persistence

## v0.19.0

- feat: Add TaskFrontmatter (typed map with Status/Phase/Assignee accessors) and TaskFile types to lib/
- feat: Add ResultWriter to task/controller that writes agent results back to vault task files

## v0.18.0

- feat: Pass TASK_ID env var to K8s Jobs spawned by task/executor so agents can reference their task on result publish

## v0.17.2

- refactor: Remove prompt layer (prompt/controller, prompt/executor, Prompt types from lib/) — replaced by task/executor

## v0.17.1

- fix: Pin opencontainers/runtime-spec v1.2.0 to resolve osv-scanner compilation error
- docs: Rewrite agent-result-capture spec for agent-publishes-result architecture
- docs: Update agent-job-interface.md with CQRS result publishing and detailed Pattern B contract

## v0.17.0

- refactor: Remove prompt layer (prompt/controller, prompt/executor, Prompt types from lib/) — replaced by task/executor
- fix: Update moby/buildkit to v0.28.1 and containerd to v1.7.30 to resolve OSV vulnerabilities
- docs: Add agent-job-interface.md with three agent patterns (git-native, persistent service, ephemeral Job)

## v0.16.0

- feat: Add K8s manifests for task/executor (Deployment, Service, Secret, ServiceAccount, Role, RoleBinding)

## v0.15.0

- feat: Implement task/executor pipeline with TaskEventHandler (status/phase/assignee filters, dedup), JobSpawner (K8s batch/v1), and factory wiring

## v0.14.0

- feat: Add task/executor service skeleton with standalone go.mod, Makefile, Dockerfile, and bare HTTP server

## v0.13.0

- feat: Add phase filter to TaskEventHandler in prompt/controller to only process tasks in planning, in_progress, or ai_review phases

## v0.12.1

- fix: pass run.NewTrigger() instead of nil to Kafka consumer to prevent nil pointer panic

## v0.12.0

- feat: Add K8s deployment manifests for prompt/controller (Deployment, Service, Secret)
- fix: Add missing Makefile.env and common.env includes to prompt/controller Makefile

## v0.11.1

- Inject PromptIdentifierGenerator into TaskEventHandler for deterministic testing

## v0.11.0

- feat: Add Kafka task event consumer to prompt/controller that converts in-progress tasks into prompt events
- feat: Add kafka-brokers and branch CLI flags to prompt/controller

## v0.10.0

- feat: give prompt/controller its own go.mod as a standalone Go module

## v0.9.2

- bump bborbe/http v1.26.8, bborbe/run v1.9.12
- bump moby/buildkit v0.28.1, containerd/cgroups v3.1.2
- bump opencontainers/runtime-spec v1.3.0
- remove grpc-gateway/v2 indirect dep
- clean osv-scanner ignores after buildkit upgrade

## v0.9.1

- refactor: eliminate `frontmatterID` struct and `parseTask` method from vault_scanner; parse `domain.Task` once in `processFile` removing redundant file read and double-parsing

## v0.9.0

- feat: Inject stable UUIDv4 task_identifier into vault task frontmatter and use UUID as TaskIdentifier on Kafka events

## v0.8.0

- feat: add CommitAndPush to GitClient interface and implement it with git add/commit/push subprocess calls

## v0.7.2

- refactor: wrap bare return err statements in task/controller with errors.Wrapf for operation context

## v0.7.1

- refactor: move trigger channel ownership into SyncLoop; expose Trigger() method on SyncLoop interface; remove raw channel from factory and main.go

## v0.7.0

- feat: add /trigger HTTP endpoint for on-demand vault scan cycles
- feat: add trigger channel to VaultScanner for external scan triggering
- docs: add dark-factory prompts for trigger endpoint and UUID task identifier spec

## v0.6.2

- fix: add separate BRANCH env var for Kafka topic prefix (was using GIT_BRANCH 'main' instead of 'dev'→'develop')

## v0.6.1

- fix: change TASK_DIR from '24 Tasks' to 'tasks' matching OpenClaw vault structure
- fix: return publish errors instead of logging warnings (fail fast via CancelOnFirstErrorWait)
- docs: add deployment guide with buca workflow and useful links

## v0.6.0

- refactor: replace go func() with run.CancelOnFirstErrorWait in sync_loop
- refactor: change VaultScanner interface to caller-owned channel (Run(ctx, chan<- ScanResult))
- fix: reduce cognitive complexity by extracting processResult method
- feat: add /setloglevel endpoint with 5-minute auto-reset
- fix: align glog V-levels (V2=heartbeat, V3=per-item, V4=trace)
- docs: add README with service description and dev/prod setloglevel links

## v0.5.0

- feat: switch git auth from token to SSH key mounted as K8s secret volume
- feat: migrate to per-service go.mod with replace directives for shared lib (matching trading monorepo pattern)
- feat: decouple GIT_BRANCH from BRANCH env var for independent vault repo branch control
- fix: update .gitignore to match trading pattern (vendor without prefix for per-service dirs)
- fix: osv-scanner scans current dir instead of ROOTDIR to avoid vendor false positives

## v0.4.0

- feat: refactor TaskPublisher to use CQRS EventObjectSender stack (SyncProducer → JSONSender → EventObjectSender) matching trading best practices
- feat: add K8s deployment manifests for task/controller (StatefulSet with PVC, Service, Secret with teamvault)
- feat: add shared K8s infra (Makefile.k8s, Makefile.env, env files) for make apply workflow
- chore: align test suites with GinkgoConfiguration + 60s timeout, add gexec compile test to main_test.go

## v0.3.1

- chore: verify all tests pass, linting succeeds, and precommit checks are green

## v0.3.0

- feat: wire VaultScanner to TaskPublisher via SyncLoop in task/controller, publishing changed and deleted task events to Kafka; integrate sync loop with HTTP server in main.go for concurrent operation with graceful shutdown

## v0.2.0

- feat: add VaultScanner service in task/controller that polls git, detects file changes via sha256 content hashing, parses YAML frontmatter, and emits ScanResult events with changed and deleted task identifiers

## v0.1.0

### Added
- Initial project structure
- [Module] github.com/bborbe/agent
- feat: add GitClient interface and implementation in task/controller for git clone/validate via os/exec subprocess
- feat: add CLI flags (git-url, git-token, kafka-brokers, git-branch, poll-interval, task-dir) to task/controller application struct
- fix: update osv-scanner in Makefile.precommit to use ROOTDIR so subdirectory make precommit can find go.mod
- chore: suppress pre-existing moby/buildkit vulnerability in .osv-scanner.toml
