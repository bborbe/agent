# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

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
