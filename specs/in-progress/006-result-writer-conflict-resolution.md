---
status: prompted
approved: "2026-04-04T11:32:07Z"
generating: "2026-04-04T11:32:29Z"
prompted: "2026-04-04T11:38:12Z"
branch: dark-factory/result-writer-conflict-resolution
---

## Summary

- All git operations in the controller are serialized — no concurrent access to the working directory
- Result writes that push successfully complete with zero overhead
- Push failures trigger rebase; if rebase produces merge conflicts, an LLM resolves them
- Gemini API key is required — service refuses to start without it
- LLM error during conflict resolution fails the command — CQRS result reports failure

## Problem

The controller has three actors (vault scanner, result writer, trigger channel) that all read/write the same git working directory without coordination. Interleaving causes corrupted commits — one actor's file write gets swept into another actor's `git add`.

Additionally, when a human edits a task while an agent works on it, both sides push changes. The controller's push fails and the agent result is lost because there is no rebase or conflict resolution.

## Goal

After this change, the controller:

1. Serializes all git operations so concurrent actors never corrupt each other
2. Handles push failures by rebasing and retrying
3. Resolves rebase merge conflicts via LLM
4. Fails the command explicitly when LLM cannot resolve a conflict

## Non-goals

- Generating or enriching task content (agent's responsibility)
- Understanding task structure, frontmatter semantics, or backtest results
- Changing the CQRS command format or task schema (see `docs/kafka-schema-design.md`)

## Desired Behavior

1. Only one git operation (pull, commit, push, write+commit+push) runs at a time — scanner and result writer never interleave
2. File writes and their corresponding commit+push are atomic — no window where another actor can `git add` someone else's dirty file
3. When push succeeds on first attempt, no additional latency is added
4. When push fails due to remote changes, the controller rebases and retries
5. When rebase produces conflict markers, an LLM resolves them by merging both versions, preferring agent content for overlapping sections
6. When LLM fails (API error, malformed response), the command fails — CQRS result reports the error, rebase is aborted, working directory is left clean
7. LLM conflict resolution is a generic markdown merge — it has no knowledge of task structure
8. The service refuses to start without a Gemini API key

## Constraints

- CQRS command format and task schema must not change (see `docs/kafka-schema-design.md`)
- Controller actor model must not change (see `docs/controller-design.md`)
- Gemini API key is a required startup parameter — missing key prevents startup
- Fast path (push succeeds): zero additional overhead
- All existing tests must pass
- `make precommit` passes in task/controller

## Security

- File content (markdown task files) is sent to Gemini API for conflict resolution
- Only conflict-state files are sent — fast path (no conflict) never calls the API
- Task files may contain strategy names, backtest IDs, or other project metadata — acceptable risk since Gemini is already used by agent-backtest
- LLM prompt must not include raw conflict markers in a way that allows prompt injection — conflict content should be clearly delimited as data, not instructions
- Task files are small (< 10KB typically) — no size limit enforced, accepted risk

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| No Gemini API key on startup | Service refuses to start | Configure API key |
| Scanner and result writer run simultaneously | Serialized — second waits for first | Automatic |
| Push succeeds (fast path) | Done | N/A |
| Push fails, rebase clean | Push again | Automatic |
| Push fails, rebase conflict, LLM resolves | Resolved content pushed | Automatic |
| Push fails, rebase conflict, LLM error | Command fails, rebase aborted, error in CQRS result | Manual retry |
| LLM returns content still containing conflict markers | Detected, command fails, rebase aborted | Manual retry |
| File deleted between event and result | Skip write | None needed |

## Acceptance Criteria

- [ ] Service refuses to start without Gemini API key
- [ ] All git operations serialized — no concurrent working directory access
- [ ] File write + commit + push are atomic (no interleaving window)
- [ ] Push succeeds → done, no extra work
- [ ] Push fails + clean rebase → retry push succeeds
- [ ] Push fails + conflict + LLM available → LLM resolves, push succeeds
- [ ] Push fails + conflict + LLM error → command fails, rebase aborted, clean working directory
- [ ] Existing tests pass
- [ ] `make precommit` passes in task/controller

## Verification

```
cd task/controller && make precommit
```

## Assumptions

- Concurrent edits are rare — today only one agent type (backtest-agent) and one human editor exist, so push succeeds 90%+ of cases
- Gemini API latency (2-5s) is acceptable for the rare conflict path
- Agent sends complete content (original task + result) — controller just writes it

## Do-Nothing Option

Current behavior: concurrent git operations can corrupt commits, and push failures lose agent results. Today this is rare — one agent type, low edit frequency. Risk grows as more agents run simultaneously: two agents completing at the same time will corrupt commits. Can implement incrementally: serialization first (correctness fix), then push retry, then LLM merge.
