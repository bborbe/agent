# Task Flow and Failure Semantics

End-to-end flow of a task through the pipeline, with explicit failure taxonomy and retry behaviour.

For the agent-side contract see [agent-job-interface.md](agent-job-interface.md). For Job-level lifecycle see [agent-job-lifecycle.md](agent-job-lifecycle.md). For the controller's writer mechanics see [controller-design.md](controller-design.md).

Related specs:

- `specs/completed/008-task-retry-protection.md` — retry counter
- `specs/completed/009-executor-job-failure-detection.md` — synthetic failure on K8s Job terminal states
- `specs/in-progress/010-failure-vs-needs-input-semantics.md` — `failed` vs `needs_input` split

## Terminology

| Term | Meaning |
|---|---|
| **Task** | A markdown file in the Obsidian vault with frontmatter (`status`, `phase`, `assignee`, `task_identifier`, …) |
| **AgentStatus** | What the agent reports: `done`, `failed`, or `needs_input` |
| **Phase** | Task lifecycle step: `planning` → `in_progress` → (`ai_review` | `done` | `human_review`) |
| **Retry counter** | `retry_count` frontmatter field, incremented by controller on each `failed`-like result |
| **Escalation** | Controller flips phase to `human_review` once `retry_count >= max_retries` |

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
    │  filter: status=in_progress AND phase ∈ {planning, in_progress, ai_review}
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
    retry_count++
    if retry_count >= max_retries:
        phase = human_review     ← escalated
}
```

**Why `needs_input` skips the retry counter:** the agent already did the work; re-running it cannot change the outcome (zero trades is zero trades). Retrying wastes compute and appends duplicate `## Result` sections to the task, which poisons the next invocation's context.

**Why `failed` counts:** could be transient (network, rate limit, OOM). Next attempt might succeed.

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
2. On run N, `retry_count >= max_retries` → controller flips to `phase: human_review` + appends `## Retry Escalation` section.
3. Executor stops re-spawning.
4. Human investigates the infra/prompt.

### Silent infra failure (spec 009)

1. Agent process is SIGKILL'd (OOM, evict, backoffLimit) — never publishes.
2. Executor's Job informer sees `Failed` terminal state.
3. Executor synthesises a `failed` result and publishes to Kafka.
4. Flows through the normal `failed` path (ai_review + retry).

### Spawn collision (idempotency)

1. Two events for the same task arrive quickly.
2. Executor finds `current_job` label on an active Job → logs warning, does not spawn a duplicate.

## Parser Tolerance (spec 010)

Claude occasionally emits narrative prose around its final JSON. The result parser extracts the last balanced `{…}` object before `json.Unmarshal`. Only if no JSON object is present at all is the result treated as `failed` with the raw output in the message.

## When to Emit Which Status (for agent authors)

- **Transient error, retry might help** → `failed` (network timeout, rate limit, database deadlock, OOM before you caught it).
- **Task content is wrong or impossible** → `needs_input` (required param missing, strategy unknown, zero trades where trades were expected, contradictory dates).
- **Work completed, even if the answer is "no results"** → `done` *only if* "no results" is a valid answer given the task. If the task implicitly required results, prefer `needs_input` with a question for the human.

## References

- `lib/delivery/status.go` — `AgentStatus` enum
- `lib/delivery/content-generator.go` — status → phase mapping
- `lib/claude/task-runner.go` — JSON parser
- `task/controller/pkg/result/result_writer.go` — retry counter + escalation
- `task/executor/pkg/handler/task_event_handler.go` — `allowedPhases`
- `task/executor/pkg/job_watcher.go` — Job terminal-state handler
