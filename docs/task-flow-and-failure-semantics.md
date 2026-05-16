# Task Flow and Failure Semantics

End-to-end flow of a task through the pipeline, with explicit failure taxonomy and retry behaviour.

For the agent-side contract see [agent-job-interface.md](agent-job-interface.md). For Job-level lifecycle see [agent-job-lifecycle.md](agent-job-lifecycle.md). For the controller's writer mechanics see [controller-design.md](controller-design.md).

Related specs:

- `specs/completed/008-task-retry-protection.md` — retry counter
- `specs/completed/009-executor-job-failure-detection.md` — synthetic failure on K8s Job terminal states
- `specs/in-progress/010-failure-vs-needs-input-semantics.md` — `failed` vs `needs_input` split
- `specs/in-progress/011-retry-counter-spawn-time-semantics.md` — retry_count moved to spawn time
- `specs/in-progress/015-executor-trigger-cap.md` — trigger_count / max_triggers cap (replaces retry_count bump)
- `specs/in-progress/016-partial-frontmatter-publishers.md` — migrate executor publishers to UpdateFrontmatterCommand; delete PublishRetryCountBump

## Terminology

| Term | Meaning |
|---|---|
| **Task** | A markdown file in the Obsidian vault with frontmatter (`status`, `phase`, `assignee`, `task_identifier`, …) |
| **AgentStatus** | What the agent reports: `done`, `failed`, or `needs_input` |
| **Phase** | Task lifecycle step: `planning` → `in_progress` → (`ai_review` | `done` | `human_review`) |
| **Trigger counter** | `trigger_count` frontmatter field, incremented atomically by the controller on every spawn-trigger event via `IncrementFrontmatterCommand`. Counts spawn-trigger attempts independent of job outcome. |
| **Max triggers** | `max_triggers` frontmatter field (default 3). When `trigger_count >= max_triggers`, the executor skips further spawns and the controller clears `assignee` to `""` — the lifecycle phase is left at the stage where the cap fired (spec 021). |
| **Retry counter** | `retry_count` frontmatter field. Removed as of spec 016 — `PublishRetryCountBump` deleted from executor; `retry_count` still readable in existing task files but is no longer written. |
| **Escalation** | Controller clears `assignee` to `""` on every escalation path (trigger cap, retry cap, `needs_input`) so the task surfaces in operator inbox. For `needs_input` the controller also sets `phase: human_review`. For cap escalations the lifecycle phase is left at the stage where the cap fired (`planning`, `in_progress`, or `ai_review`). Reference: spec 021. The controller also writes `previous_assignee: <name>` with the pre-clear agent name on every assignee-clear event, enabling operator queries by parked-by-agent without body parsing. The field persists across operator re-delegation. Reference: spec 027. |

## Inbox Signal (spec 021)

`assignee == ""` is the single canonical signal for "task needs attention". Operator boards and tooling that surface unclaimed work should filter on `assignee`, not on `phase`.

- `phase: human_review` means a human must do the actual work (agent emitted `needs_input`).
- `phase: ai_review` / `in_progress` / `planning` with `assignee: ""` means an agent hit a cap; fix the underlying issue and re-delegate by setting `assignee` to an agent name.

## Status Taxonomy

Two fundamentally different failure kinds. Treating them the same is wrong (spec 010).

| Kind | Example | AgentStatus | Retry on next run? |
|---|---|---|---|
| **Infra failure** | Claude CLI crashed, parse error, network blip, OOM, node eviction | `failed` | Yes — next run might succeed |
| **Task-wrong** | Window has zero trades; strategy name unknown; missing required parameter | `needs_input` | No — same input will yield same answer |
| **Success** | Work completed | `done` | — |

Agents choose. The controller routes. The executor never re-spawns once phase leaves the allowlist.

## Full Flow

```
Vault (Obsidian)
    │  user writes task with phase: planning or in_progress
    ▼
task/controller  [git watcher]
    │  publishes agent-task-v1-event on Kafka
    ▼
task/executor    [consumer + K8s Job spawner]
    │  filter: task_type ∈ agent's effective set AND
    │           status=in_progress AND phase ∈ {planning, in_progress, ai_review}
    │           AND matching assignee AND matching stage
    │  spawn K8s Job with TASK_CONTENT, TASK_ID, KAFKA_BROKERS, BRANCH, STAGE
    │  watch Job terminal state (informer)
    ▼
Agent (Pattern B Job)
    │  do work → emit AgentStatus (done | failed | needs_input)
    │  publish agent-task-v1-request on Kafka
    │  exit 0
    ▼
task/executor    [job informer]
    │  Succeeded → trust agent's published result
    │  Failed    → synthesise a `failed` result (spec 009)
    ▼
task/controller  [consumer + result writer]
    │  merge frontmatter + apply retry counter
    │  route per AgentStatus (see below)
    │  git commit + push
    ▼
Vault (Obsidian)   [task file updated]
```

## Result Routing (spec 010)

The controller's result writer translates the incoming result:

```
switch AgentStatus {
case done:
    status = completed
    phase  = done
    retry_count: unchanged
case needs_input:
    status = in_progress
    phase  = human_review       ← terminal, no retry
    retry_count: unchanged
default (failed):
    status = in_progress
    phase  = ai_review           ← re-enters executor allowlist
    retry_count: unchanged        ← executor already bumped it at spawn time (spec 011)
    if retry_count >= max_retries:
        assignee = ""            ← escalated; phase stays at ai_review (spec 021)
}
```

**Why `needs_input` skips the retry counter:** the agent already did the work; re-running it cannot change the outcome (zero trades is zero trades). Retrying wastes compute and appends duplicate `## Result` sections to the task, which poisons the next invocation's context.

**Why `failed` counts:** could be transient (network, rate limit, OOM). The executor bumps `retry_count` before each spawn attempt so the counter equals invocations attempted, not failure events observed.

## Failure Scenarios

### Happy path

1. Task `phase: in_progress`, agent emits `done` → `phase: done`, `status: completed`. Terminal.

### Agent emits `needs_input` (spec 010)

1. Task `phase: in_progress`, agent emits `needs_input` (e.g. zero trades in window).
2. Controller writes `phase: human_review`, `retry_count: 0`, single `## Result` block.
3. Executor does not re-spawn (phase out of allowlist).
4. Human edits task content, flips `phase: planning` or `in_progress` → cycle resumes with new params.

### Agent emits `failed` (infra, below max)

1. Agent crashes or returns parse error.
2. Controller writes `phase: ai_review`, `retry_count: 1`, Result section appended.
3. Executor re-spawns on next cycle (phase in allowlist).
4. Second run succeeds → `phase: done`. Terminal.

### Agent emits `failed` (infra, exceeds max)

1. Runs 1..N all emit `failed`.
2. On run N, `retry_count >= max_retries` → controller clears `assignee: ""`, leaves phase at `ai_review`, appends `## Retry Escalation` section (spec 021).
3. Executor stops re-spawning (no assignee match).
4. Human investigates the infra/prompt, then re-delegates by setting `assignee`.

### Type mismatch (spec 028)

1. Task `phase: planning`, `task_type: oauth-probe`, routed to `agent-pr-reviewer` whose effective set is `[pr-review]`.
2. Executor resolves Config CR for `agent-pr-reviewer`, computes effective set `[pr-review]`.
3. Type filter detects mismatch — no Job is spawned.
4. Executor synthesises a failure: `phase: ai_review`, `assignee: ""`, `current_job: ""`. Failure body names the mismatch.
5. Task surfaces in operator inbox (assignee-empty signal). `trigger_count` is NOT incremented.
6. Operator either edits the task's `task_type` to `pr-review`, or adds `oauth-probe` to the agent's `taskTypes` CRD field, then re-delegates by setting `assignee`.

### Silent infra failure (spec 009)

1. Agent process is SIGKILL'd (OOM, evict, backoffLimit) — never publishes.
2. Executor's Job informer sees `Failed` terminal state.
3. Executor synthesises a `failed` result and publishes to Kafka.
4. Flows through the normal `failed` path (ai_review). `trigger_count` was already incremented when the Job was spawned; the synthesised failure does not bump it again.

### Trigger cap reached (spec 015)

1. Task has been spawned `max_triggers` times (default 3); `trigger_count >= max_triggers`.
2. Executor receives a matching task event (status=in_progress, phase in allowlist).
3. Executor skips the spawn entirely — no `IncrementFrontmatterCommand` published, no K8s Job created.
4. Human investigates or raises `max_triggers` in the task frontmatter to allow more attempts.

**Over-count tolerance**: if `PublishIncrementTriggerCount` succeeds but the subsequent `SpawnJob` call fails, `trigger_count` has been incremented by 1 while no Job ran. This is expected and bounded — `max_triggers` absorbs at most one over-count per attempt. No rollback or compensation is attempted.

**Byte-identical output protection**: because `trigger_count` is incremented via an atomic `IncrementFrontmatterCommand` (never idempotent at the controller level), even identical task files produce a distinct write, preventing the executor from looping indefinitely on byte-identical agent output.

### Spawn collision (idempotency)

1. Two events for the same task arrive quickly.
2. Executor finds `current_job` label on an active Job → logs warning, does not spawn a duplicate.

## Parser Tolerance (spec 010)

Claude occasionally emits narrative prose around its final JSON. The result parser extracts the last balanced `{…}` object before `json.Unmarshal`. Only if no JSON object is present at all is the result treated as `failed` with the raw output in the message.

## When to Emit Which Status (for agent authors)

- **Transient error, retry might help** → `failed` (network timeout, rate limit, database deadlock, OOM before you caught it).
- **Task content is wrong or impossible** → `needs_input` (required param missing, strategy unknown, zero trades where trades were expected, contradictory dates).
- **Work completed, even if the answer is "no results"** → `done` *only if* "no results" is a valid answer given the task. If the task implicitly required results, prefer `needs_input` with a question for the human.

## Executor Publisher Command Kinds

Each `ResultPublisher` method publishes a specific command kind on `agent-task-v1-request`.
Only the listed frontmatter keys are written; all other keys — including `trigger_count` — are never touched by executor-originated publishes.

| Publisher method | Command kind | Frontmatter keys written |
|---|---|---|
| `PublishIncrementTriggerCount` | `increment-frontmatter` | `trigger_count` (delta +1) |
| `PublishSpawnNotification` | `update-frontmatter` | `current_job`, `job_started_at`, `spawn_notification` |
| `PublishFailure` | `update-frontmatter` | `status`, `phase`, `current_job` |
| `PublishTypeMismatchFailure` | `update-frontmatter` | `status`, `phase` (`ai_review`), `assignee` (`""`), `current_job` |

## Create-Task Path Resolution (spec-019)

When the controller processes a `create-task` command it resolves the vault path as follows:

1. **Title valid + title path unoccupied** → write `tasks/{title}.md`
2. **Title valid + title path occupied by the same `task_identifier`** → no-op (idempotent)
3. **Title valid + title path occupied by a different `task_identifier`** → WARN + fall back to `tasks/{task_identifier}.md`
4. **Title fails validation (any rule)** → WARN + fall back to `tasks/{task_identifier}.md`

In cases 3 and 4 the task is always materialized under its UUID path — the system never drops the task. The WARN log surfaces the anomaly to operators.

**UUID fallback is permanent contract, not a migration affordance.** Producers that bypass the sender's `Validate`-before-publish (e.g. anyone with Kafka write access publishing a raw command) will trigger the fallback; the WARN log is the alerting mechanism. The existing file at `tasks/{task_identifier}.md` (if any) is the idempotency record.

## Executor respawn gates

The executor applies two sequential gates before spawning a K8s Job for a task:

**Gate 1 — Terminal-phase gate (spec 035):** runs in `parseAndFilter`. Tasks whose `phase ∈ {human_review, done}` are suppressed before the trigger-phase allowlist. Emits `event=spawn_suppressed phase=<phase>` log and `spawn_suppressed_terminal_phase` metric. This gate fires when the agent's terminal-phase write has already arrived at the executor.

**Gate 2 — Grace-period gate (spec 036):** runs in `spawnIfNeeded`, inside the `current_job != "" && !active` branch. When the K8s Job has exited but no terminal phase has been observed yet, the executor suppresses respawn for `defaultRespawnGracePeriod` (300 seconds) from `job_started_at`. Emits `event=respawn_grace_window task=<id> current_job=<job> elapsed=<seconds>` log and `respawn_grace_window` metric. After the grace period, a genuinely crashed agent (no terminal write, no field cleared) is retried normally.

**Why two gates:** Gate 1 fires when the write has propagated; Gate 2 fires during the propagation window. Together they close the duplicate-spawn race: a clean-exit agent triggers Gate 2 during the window, then Gate 1 once the write lands. A crashed agent triggers neither gate (it never writes a terminal phase) and is retried after Gate 2's grace period expires.

**Diagnostic commands:**
```bash
# Suppressed by grace window (within 300s of spawn)
kubectl logs <executor-pod> | grep respawn_grace_window
# Suppressed by terminal gate (write already propagated)
kubectl logs <executor-pod> | grep spawn_suppressed_terminal_phase
```

## References

- `lib/delivery/status.go` — `AgentStatus` enum
- `lib/delivery/content-generator.go` — status → phase mapping
- `lib/claude/task-runner.go` — JSON parser
- `task/controller/pkg/result/result_writer.go` — retry counter + escalation
- `task/executor/pkg/handler/task_event_handler.go` — `allowedPhases`
- `task/executor/pkg/job_watcher.go` — Job terminal-state handler
