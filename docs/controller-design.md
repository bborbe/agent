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

## Git Operation Serialization

All git operations (pull, write, commit, push) are serialized via `sync.Mutex` in the GitClient. The `AtomicWriteAndCommitPush` method holds the lock for the entire write→commit→push sequence, preventing concurrent operations from interleaving.

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
