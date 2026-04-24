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
  ├── git pull
  ├── walk task directory, sha256-hash each file
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
  │     └── if retry_count >= max_retries → set phase: human_review, append ## Retry Escalation
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

## Git Operation Serialization

All git operations (pull, write, commit, push) are serialized via `sync.Mutex` in the GitClient. Two methods hold the lock for the entire sequence:

- `AtomicWriteAndCommitPush(absPath, content, message)` — writes `content` directly then commits.
- `AtomicReadModifyWriteAndCommitPush(absPath, modify, message)` — reads the current file, calls `modify(current []byte) ([]byte, error)`, writes the result, then commits. Unlike `AtomicWriteAndCommitPush`, the read is also inside the lock, ensuring no other git operation can interleave between read and write.

## Push Retry with Rebase

When `git push` fails (remote has new commits), the controller:

1. Fetches latest changes
2. Rebases local commits on top
3. If rebase is clean → retry push
4. If rebase has conflicts → invoke LLM conflict resolver

## LLM Conflict Resolution

Git merge conflicts are resolved via Gemini API (`gemini-2.5-flash`). The resolver:

- Receives the conflicted file content with `<<<<<<<`/`=======`/`>>>>>>>` markers
- Returns clean merged markdown
- Is generic (no task/domain knowledge) — works for any markdown file
- Requires `GEMINI_API_KEY` env var (controller won't start without it)

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
