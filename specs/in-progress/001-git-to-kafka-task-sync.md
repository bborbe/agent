---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-03-26T17:38:10Z"
prompted: "2026-03-26T17:42:11Z"
verifying: "2026-03-26T19:22:00Z"
branch: dark-factory/git-to-kafka-task-sync
---

## Summary

- task/controller gains a polling loop that git-pulls an Obsidian vault and detects changed task files
- Changed tasks are parsed from frontmatter and published as events to Kafka topic `agent-task-v1-event`
- Change detection uses in-memory content hashing; restart triggers full re-scan (acceptable)
- Only tasks with an assignee field are published; unassigned tasks are silently skipped
- This is the Git-to-Kafka direction only; Kafka-to-Git write-back is a separate spec

## Problem

The agent orchestration system needs task state to flow from the Obsidian vault (source of truth for humans) into Kafka (source of truth for agents). Today, task/controller is a skeleton HTTP server with no data pipeline. Without this sync loop, no downstream service (prompt/controller, prompt/executor) can react to task changes.

## Goal

After this work, task/controller continuously mirrors assigned task state from the vault git repo into Kafka events. Any change to a task file's frontmatter (status, assignee, phase) or content produces exactly one Kafka event within the next poll interval. The service runs alongside its existing HTTP server as a concurrent goroutine.

## Non-goals

- Kafka-to-Git write-back (consuming requests to modify task files)
- Prompt event handling
- K8s CRD or heartbeat mechanisms
- File watching (inotify/fsnotify) — polling is sufficient
- Multi-repo support — single vault repo only
- Git push or commit — this direction is read-only
- GitHub account creation or collaborator setup — operational concern, not service code

## Assumptions

- Kafka consumers are idempotent — re-publishing all tasks after restart is safe
- Vault git repo is pre-cloned and accessible at the configured path
- Git credentials (SSH key or token) are pre-configured in the environment via K8s secret mount — the service does not manage authentication itself
- Kafka topics are pre-created (or auto-created by broker)
- Single vault repo; multi-repo is out of scope
- Polling at 60s intervals is sufficient; sub-second latency is not required

## Desired Behavior

1. On startup, the service clones or validates the configured vault git repository path. If the path does not exist or is not a git repo, the service exits with a fatal error.
2. At a configurable interval (default 60s), the service runs `git pull` on the vault repo via subprocess.
3. After each pull, the service walks the task directory (`24 Tasks/`), reads all `.md` files, and computes a content hash for each file.
4. Files whose hash differs from the last-known hash (or are new) are considered changed. Files that disappear are considered deleted.
5. For each changed file, the service parses frontmatter to extract status, assignee, phase, name, and content. Files with invalid or missing frontmatter are skipped with a warning log.
6. Only tasks where the assignee field is non-empty produce a Kafka event. Unassigned tasks are skipped silently.
7. Each qualifying changed task produces one `agent-task-v1-event` message on the Kafka topic determined by the schema ID and branch. The task identifier is derived from the file path relative to the vault root.
8. Deleted tasks that were previously published produce a deletion event.

## Constraints

- Existing HTTP server (healthz, readiness, metrics endpoints) must continue working unchanged
- `lib.Task` struct, `lib.TaskV1SchemaID`, and `lib.TaskIdentifier` types are frozen — do not modify
- Use `vault-cli/pkg/domain` types for frontmatter parsing (TaskStatus, TaskPhase) — do not duplicate
- Git operations must use subprocess execution — no embedded git libraries
- Factory functions remain pure composition — no conditionals, no I/O, no context creation
- All new interfaces must have generated mocks; all tests use project test framework
- The sync loop must respect context cancellation for graceful shutdown
- No new CLI flags beyond what is needed: vault path, git branch, Kafka brokers, poll interval

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Git pull fails (network, auth, conflict) | Log error at warning level, skip this cycle | Retry automatically on next poll interval |
| Task file has unparseable frontmatter | Log warning with filename, skip file | File is re-evaluated on next cycle if changed |
| Task file has valid frontmatter but empty assignee | Skip silently (not an error) | Re-evaluated if file changes later |
| Kafka broker unreachable | Sync producer blocks/retries per libkafka defaults | Backpressure; events queued until broker recovers |
| Vault path does not exist on startup | Fatal error, service refuses to start | Operator fixes config and restarts |
| Vault path exists but is not a git repo | Fatal error, service refuses to start | Operator fixes config and restarts |
| Service restarts (hash map lost) | Full re-scan on first cycle, all assigned tasks re-published | Consumers must be idempotent (already required by CQRS pattern) |
| File deleted between pulls | Deletion event published for previously known task | Consumers handle deletion events |

## Security / Abuse Cases

- Vault path is operator-configured, not user-supplied — no path traversal risk
- Git subprocess inherits credentials from K8s secret mount (dedicated GitHub service account) — no credentials in code or env vars
- Kafka topic is derived from frozen schema ID, not user input — no injection risk
- Frontmatter parsing: repeated warnings for the same malformed file are deduplicated across cycles

## Acceptance Criteria

- [ ] task/controller starts with vault-path, kafka-brokers, and poll-interval flags
- [ ] Service fatals on startup if vault path is missing or not a git repo
- [ ] Git pull runs every poll-interval seconds
- [ ] Changed task files produce exactly one Kafka event per change
- [ ] Deleted task files produce a deletion event
- [ ] Tasks without assignee are not published to Kafka
- [ ] Tasks with invalid frontmatter are skipped with a warning log
- [ ] Existing healthz/readiness/metrics endpoints still work
- [ ] `make precommit` passes in `task/controller/`
- [ ] Graceful shutdown: context cancellation stops the poll loop and flushes pending events

## Verification

```
cd task/controller && make precommit
```

## Do-Nothing Option

Without this sync loop, the agent system has no way to observe task changes. All downstream services (prompt/controller, prompt/executor) remain idle because no task events flow through Kafka. The entire agent orchestration pipeline is blocked. Doing nothing is not acceptable if the agent system is to function.
