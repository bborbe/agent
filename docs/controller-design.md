# Controller Design (task/controller)

The controller is the single writer to the vault git repo. It has two responsibilities: detecting task changes in git and publishing them to Kafka, and consuming commands from Kafka and writing results back to git. It has no K8s API access.

## Inputs / Outputs

| Direction | Topic | Purpose |
|-----------|-------|---------|
| Produces | `agent-task-v1-event` | Task created or status changed in git |
| Consumes | `agent-task-v1-request` | Update task commands (from agents) |
| Produces | `agent-task-v1-result` | Command processing confirmation (CQRS auto) |

## Core Logic

### 1. Change Detection (git → Kafka)

```
Poll loop:
  │
  ├── Pull() — no-op (git-rest handles pulls internally)
  ├── gitClient.ListFiles(taskDir/*.md) → enumerate task files via HTTP
  ├── sha256-hash each file's content
  ├── compare with previous hashes
  │
  ├── changed file → parse frontmatter + body → publish agent-task-v1-event
  └── deleted file → publish agent-task-v1-event (deleted)
```

### 2. Command Processing (Kafka → git)

```
On agent-task-v1-request (operation: "update"):
  │
  ├── deserialize lib.Task from command payload
  ├── validate: TaskIdentifier and Content must be non-empty
  │
  ├── walk task directory, find file matching task_identifier in frontmatter
  ├── merge frontmatter + apply escalation check (counter set by executor, not incremented here)
  │     ├── read retry_count from merged frontmatter (set by executor at spawn time, spec 011)
  │     ├── if trigger_count >= max_triggers → clear assignee: "", preserve lifecycle phase, append ## Trigger Cap Escalation (once)
  │     ├── if retry_count >= max_retries   → clear assignee: "", preserve lifecycle phase, append ## Retry Escalation (once)
  │     └── if agent emits needs_input (phase: human_review) → set phase: human_review, clear assignee: ""
  ├── sanitize content (escape bare --- lines to prevent YAML corruption)
  ├── write frontmatter + content to file
  ├── git add + commit + push
  └── CQRS framework publishes success/failure result to agent-task-v1-result
```

## Frontmatter Merge

When writing a result back, the ResultWriter merges frontmatter from the existing task file with frontmatter provided by the agent. Existing keys are preserved, agent keys override on conflict. This ensures fields like `assignee`, `tags`, and `task_identifier` survive result writeback even though agents don't receive frontmatter.

```
Existing file:  {assignee: backtest-agent, tags: [agent-task], task_identifier: xyz}
Agent provides: {status: completed, phase: done}
Merged result:  {assignee: backtest-agent, tags: [agent-task], task_identifier: xyz, status: completed, phase: done}
```

## Assignee-Clear on Escalation (spec 021)

Every escalation path writes `assignee: ""` so the task surfaces in operator inbox:

| Escalation trigger | `phase` written | `assignee` written |
|---|---|---|
| `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
| `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
| Agent emits `needs_input` | `human_review` | `""` |

Once a task is parked (escalation section present, `assignee: ""`), repeated stale agent
result publishes are idempotent: the escalation section is not duplicated, the lifecycle
phase is restored from the on-disk value, and assignee stays empty.

## Empty-to-Named Reset (spec 021)

When the vault scanner observes a task file whose `assignee` transitions from empty (or absent) to a non-empty agent name, it writes `trigger_count: 0` and `retry_count: 0` back to the file atomically and queues a git commit. This refills the per-attempt budgets for the re-delegated agent without requiring manual counter edits. The reset fires exactly once per empty-to-named transition (named→named and named→empty transitions do not trigger a reset).

## Atomic Frontmatter Commands

In addition to the `"update"` operation (full result write), the controller handles two atomic frontmatter operations on `agent-task-v1-request`:

### `"increment-frontmatter"` (IncrementFrontmatterExecutor)

Payload: `lib.IncrementFrontmatterCommand{TaskIdentifier, Field, Delta}`

```
On agent-task-v1-request (operation: "increment-frontmatter"):
  │
  ├── deserialize IncrementFrontmatterCommand
  ├── find task file by task_identifier (WalkDir)
  ├── if not found → log warning, return nil (no error)
  ├── AtomicReadModifyWriteAndCommitPush:
  │     ├── read current file bytes (under mutex)
  │     ├── parse frontmatter, read Field value (default 0 if absent)
  │     ├── newVal = currentVal + Delta
  │     ├── set Field = newVal
  │     ├── cap escalation: if Field == "trigger_count" AND newVal >= max_triggers
  │     │     └── set phase = "human_review" in the same write
  │     ├── write updated file (under mutex)
  │     └── git commit + push (under mutex)
  └── increment FrontmatterCommandsTotal{operation, outcome}
```

Delta may be negative (decrement). Cap escalation only fires for `trigger_count` reaching `max_triggers`.

### `"update-frontmatter"` (UpdateFrontmatterExecutor)

Payload: `lib.UpdateFrontmatterCommand{TaskIdentifier, Updates map[string]any}`

```
On agent-task-v1-request (operation: "update-frontmatter"):
  │
  ├── deserialize UpdateFrontmatterCommand
  ├── if Updates is empty → return nil (no-op, no write)
  ├── find task file by task_identifier (WalkDir)
  ├── if not found → log warning, return nil
  ├── AtomicReadModifyWriteAndCommitPush:
  │     ├── read current file bytes (under mutex)
  │     ├── parse existing frontmatter
  │     ├── merge only the keys in Updates (all other keys unchanged)
  │     ├── write updated file (under mutex)
  │     └── git commit + push (under mutex)
  └── increment FrontmatterCommandsTotal{operation, outcome}
```

## Vault Writes via git-rest

The controller holds no local git clone. All vault file operations flow through the
`vault-obsidian-openclaw` git-rest StatefulSet via HTTP:

| Operation | HTTP call | Who commits |
|-----------|-----------|-------------|
| Read file | `GET /api/v1/files/{relPath}` | N/A |
| Write file | `POST /api/v1/files/{relPath}` | git-rest (auto-commit) |
| Delete file | `DELETE /api/v1/files/{relPath}` | git-rest (auto-commit) |
| List files | `GET /api/v1/files/?glob={pattern}` | N/A |

git-rest ensures one commit per write. The controller's `/readiness` endpoint reflects
git-rest readiness: if git-rest returns 503 (push stuck), the controller reports 503
and the Kafka consumer goroutine blocks inside the write retry loop until git-rest
recovers. Kafka offsets are not advanced during this block.

BoltDB (at `/data/bolt` on the `datadir` PVC) continues to track Kafka consumer
offsets — unchanged from the pre-migration architecture.

## Content Sanitization

Agent output may contain bare `---` lines that would corrupt YAML frontmatter boundaries. The ResultWriter escapes these to `\-\-\-` before writing.

## HTTP Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/healthz` | Liveness probe |
| `/readiness` | Readiness probe |
| `/metrics` | Prometheus metrics |
| `/setloglevel` | Temporary log level change (5-min auto-reset) |
| `/trigger` | On-demand vault scan cycle |

## What the Controller Does NOT Do

- No K8s API calls (task/executor handles job spawning)
- No domain logic (doesn't know what a backtest is)
- No job management (doesn't know about pods)
- No prompt conversion (removed in v0.17.0)
