---
status: approved
tags:
    - dark-factory
    - spec
approved: "2026-03-27T12:58:22Z"
branch: dark-factory/uuid-task-identifier
---

## Summary

- Task files in the Obsidian vault get a stable UUID-based `task_identifier` in their YAML frontmatter (snake_case matching existing fields like `page_type`, `planned_date`)
- The scanner detects missing `task_identifier`, generates a UUID, and writes it back to the file
- All frontmatter mutations within a scan cycle are batch-committed and pushed in one git operation
- Only tasks with a `task_identifier` are published to Kafka — tasks pending write-back are skipped until the next cycle
- TaskIdentifier becomes a UUID instead of a file path, giving tasks stable identity across renames and moves

## Problem

Tasks are currently identified by their file path (`tasks/some-file.md`). When a task file is renamed or moved in the vault, the system sees a deletion of the old path and creation of a new one — losing continuity. Downstream consumers (prompt controller, executor) cannot correlate old and new events for the same logical task.

## Goal

After this work, every task file has a persistent UUID in its frontmatter that survives renames and moves. The task controller uses this UUID as the business key for Kafka events.

## Non-goals

- Migrating existing downstream consumers to use UUID-based keys (they already use TaskIdentifier; the type stays the same, only the value changes)
- Deduplicating events if a file is renamed within a single scan cycle (rename = delete old path + changed new UUID — acceptable)
- Conflict resolution if two files share the same UUID (log warning, skip the duplicate)

## Desired Behavior

1. When the scanner encounters a `.md` file with valid frontmatter but no `task_identifier` field, it generates a UUIDv4 and inserts `taskIdentifier: <uuid>` into the frontmatter YAML block.
2. The modified file content is written back to disk immediately within the scan cycle.
3. At the end of each scan cycle, if any files were modified, all mutations are committed and pushed to the remote in a single atomic operation.
4. Tasks whose `task_identifier` was just generated in this cycle are NOT published to Kafka — they will be picked up on the next scan cycle after the commit is pulled, ensuring the published state matches what is in git.
5. Tasks that already have a `task_identifier` in their frontmatter use that value as-is. The scanner never overwrites an existing `task_identifier`.
6. The `TaskIdentifier` field on published Kafka events contains the UUID value (not the file path).
7. Delete events emitted in `ScanResult` use the UUID-based `TaskIdentifier` so downstream consumers can correlate deletions.

## Constraints

- The `GitClient` interface gains commit+push capability; existing `Pull` and `EnsureCloned` methods must not change behavior.
- The `VaultScanner` interface signature (`Run(ctx, chan<- ScanResult)`) must not change.
- The `lib.Task` struct and `lib.TaskIdentifier` type remain unchanged — only the values carried change from paths to UUIDs.
- The `TaskPublisher` interface is unchanged.
- Frontmatter write-back must preserve all existing fields and formatting (no field reordering, no stripping of unknown fields).
- The commit message should be machine-recognizable (e.g., prefix `[agent-task-controller]`) so it can be filtered in git log.
- Existing tests must still pass after changes.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Git push fails (conflict, auth) | Log warning, skip publish for this cycle. Do not retry push within the same cycle. Next cycle pulls first, re-detects missing identifiers, re-generates if needed. | Automatic on next cycle |
| File has malformed frontmatter | Skip file entirely (existing behavior). Do not attempt to inject taskIdentifier. | Manual fix in vault |
| Two files have the same taskIdentifier UUID | Log warning for the duplicate, publish only the first one encountered. | Manual fix — remove duplicate UUID from one file |
| Write-back fails for a single file | Log warning, continue with remaining files. Do not commit partial batch if any write fails — skip the entire commit for this cycle. | Automatic retry on next cycle |

## Security / Abuse Cases

- File paths come from a trusted git clone via `fs.WalkDir` — no user-controlled path injection.
- UUID generation uses `github.com/google/uuid` (crypto/rand backed) — no predictability concern.
- Frontmatter write-back writes to the local git clone only — bounded by disk space of the PV.

## Acceptance Criteria

- [ ] A task file without `task_identifier` gets a UUIDv4 written into its frontmatter after one scan cycle
- [ ] A task file with existing `task_identifier` is published using that value, unchanged
- [ ] Files modified in a scan cycle are committed and pushed in a single git operation
- [ ] Tasks with freshly-generated identifiers are not published until the following scan cycle
- [ ] Existing tests pass (`make test` in `task/controller/`)
- [ ] Delete events use the UUID-based TaskIdentifier, not the file path
- [ ] Frontmatter write-back preserves existing fields and does not reorder them

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

## Do-Nothing Option

Without this change, task identity is tied to file paths. Every rename or move in the Obsidian vault causes a delete+create event pair, breaking continuity for downstream consumers. Operators have no way to force immediate reconciliation. The system works but is fragile for any vault reorganization.
