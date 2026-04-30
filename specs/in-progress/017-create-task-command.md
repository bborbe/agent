---
status: verifying
approved: "2026-04-27T20:10:03Z"
generating: "2026-04-27T20:10:20Z"
prompted: "2026-04-27T20:14:21Z"
verifying: "2026-04-27T20:35:11Z"
branch: dark-factory/create-task-command
---

## Summary

- A new Kafka command `CreateTaskCommand` in `lib/` lets external producers (watchers, cron jobs, future agents) request that the controller create a vault task on their behalf
- The command carries a deterministic `task_identifier` so the controller can no-op duplicates idempotently — producers don't need to query vault state to dedup
- The controller's executor materializes a vault task file, commits, and pushes via its existing git infrastructure — single writer, no race with humans' obsidian-git
- Producers never need vault git access, never need to know the task directory layout, never duplicate task-creation logic
- First consumer is the upcoming `pr-watcher` service in `bborbe/code-reviewer`; future watchers (jira-watcher, calendar-watcher, etc.) reuse the same command

## Problem

Today the OpenClaw vault is mutated by two paths: humans (via the obsidian-git plugin) and the controller (via Kafka commands like `UpdateFrontmatterCommand`). Task creation is solely a human path — there is no way for a non-human producer to request a new task without each producer cloning the vault, writing the file, committing, and pushing. That duplicates infrastructure (git auth, conflict handling, file-layout knowledge) into every producer pod and races with humans' obsidian-git pushes. The architecture is inconsistent: mutations go through the controller; creations bypass it.

The first concrete need is the `pr-watcher` service ([GitHub PR Reviewer](../../../Obsidian/Personal/23%20Goals/GitHub%20PR%20Reviewer.md) Task B1) that polls GitHub and needs to create a vault task per new PR. Without `CreateTaskCommand`, pr-watcher would have to embed a vault git client.

## Goal

After completion, any service in the cluster (pr-watcher, future watchers, cron jobs) can publish a `CreateTaskCommand` to Kafka and the controller writes the corresponding task into the vault. Calling the command twice with the same `task_identifier` produces the same end state (idempotent — second call is a no-op). The vault remains the single source of truth, with the controller as the single writer.

## Non-goals

- Replacing the human/obsidian-git path — humans continue creating tasks via Obsidian; this command supplements, not replaces
- Templates / schema validation per task type — `CreateTaskCommand` accepts arbitrary frontmatter + body; schema enforcement is the producer's job
- Updating an existing task — `UpdateFrontmatterCommand` already covers that
- Deleting tasks — out of scope; not needed for the MVP consumers
- Server-side authorization / per-producer ACLs — Kafka topic ACLs are sufficient for now; revisit when there are multiple distrust-domains

## Desired Behavior

1. The `lib/` package exposes `CreateTaskCommand` and a `CreateTaskCommandOperation` constant matching the cqrs operation-name regex `^[a-z][a-z-]*$`
2. The command payload includes: a deterministic `task_identifier` chosen by the producer, the initial frontmatter as a typed map (mirroring `UpdateFrontmatterCommand.Updates`), and an optional initial body string
3. The controller's executor for this operation materializes a task file at the vault's standard location for that `task_identifier`, writes the frontmatter + body, commits, and pushes
4. If a task with the same `task_identifier` already exists, the executor logs at info level and returns success without modifying the existing task — strict idempotency
5. The executor runs in the controller's existing single-writer event loop — no new concurrency surface
6. Validation: `CreateTaskCommandOperation` is added to the cqrs validator's known-operations table so unrecognized variants are rejected at publish time

## Constraints

- New file `lib/agent_task-commands.go` extension — alongside existing `IncrementFrontmatterCommand`, `UpdateFrontmatterCommand`. Same `lib` package
- Operation constant MUST match cqrs regex `^[a-z][a-z-]*$` (lowercase + hyphens) — recommended: `CreateTaskCommandOperation = "create-task"`
- Operation constant MUST be added to the validate-all test table in `lib/agent_task-commands_test.go` (per the existing IMPORTANT note in that file)
- Executor lives in `task/controller/pkg/` — follow existing executor structure used by `IncrementFrontmatterCommand` and `UpdateFrontmatterCommand`
- Idempotency: existing-`task_identifier` is detected by checking for an existing file at the materialized path; if present, executor returns nil with info-level log
- The vault write follows the same path-derivation logic the controller already uses for other operations (controller owns the path layout)
- Errors wrapped via `github.com/bborbe/errors`; structured logging via `glog`
- Tests: Ginkgo/Gomega + Counterfeiter for any external dependencies
- The new command's lib version must be tagged before pr-watcher can depend on it — coordinate version bump
- The `task_identifier` is a typed `lib.TaskIdentifier` (existing type) — no new ID type
- Initial frontmatter MUST include at minimum a recognizable `assignee` and `status`; if absent, executor returns wrapped validation error

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `task_identifier` already exists in vault | No-op, info log, return nil (strict idempotency) | n/a |
| `task_identifier` empty / malformed | Wrapped validation error from executor; cqrs returns failure to producer | Producer fixes payload |
| Frontmatter missing required fields (`assignee`, `status`) | Wrapped validation error | Producer fixes payload |
| Vault git commit fails (corrupt repo, disk) | Wrapped error; cqrs ack-fails so producer can retry | Operator investigates controller pod |
| Vault git push fails (network, auth) | Wrapped error; cqrs ack-fails so producer can retry | Operator investigates; producer retries on next cycle |
| Operation name not registered in cqrs validator | Publish fails at producer side with regex / unknown-op error | Add operation to validator + redeploy |
| Concurrent CreateTask + UpdateFrontmatter for same identifier | Serialized by controller's single-writer loop; one wins, the other operates on the post-commit state | n/a |
| Two concurrent CreateTask publishes for the same identifier | Serialized by single-writer loop; first creates the file, second is a no-op per idempotency rule | n/a |
| Body section that conflicts with frontmatter parser (delimiter in body) | Validation rejects or escapes; body is rendered as a fenced or escaped block | Producer should sanitize before publish |

## Security / Abuse Cases

- The Kafka topic publishing this command is internal-cluster-only; no external producers
- Future producers should declare what task assignees they're authorized to create for (out of scope here, but document the assumption)
- Body content is producer-controlled; downstream agents that read the task body inherit any prompt-injection risk — this command does not sanitize text-content
- File path materialized by the controller MUST be derived from validated `task_identifier` only — no producer-controlled string flows into the path

## Acceptance Criteria

- [ ] The `lib/` package exposes the new command type alongside the existing task commands, with operation constant matching the cqrs regex
- [ ] Initial body payload follows the same shape as the existing `UpdateFrontmatterCommand` body (reuse the existing typed body section if structurally suitable; introduce a parallel one only if the semantics differ)
- [ ] `lib/agent_task-commands_test.go` Validate-all table updated to include the new operation
- [ ] Controller has an executor that handles the operation, materializes the file, commits, pushes
- [ ] Idempotency verified by a unit test: calling the executor twice with the same identifier produces one file, second call is no-op
- [ ] Validation tests cover: empty identifier, malformed identifier, missing required frontmatter
- [ ] All tests + precommit green in `bborbe/agent`
- [ ] `lib/v0.55.0` (or next minor) tag pushed
- [ ] Controller image tagged + deployed to dev
- [ ] **E2E scenario** (new Kafka command kind + new controller executor + vault filesystem write — required per spec-writing.md "integration seam" rule): publish a real `CreateTaskCommand` to the dev cluster's Kafka topic; observe the controller logs accept the command; verify the task file appears in the vault repo with the correct frontmatter + body; publish the same command again and verify it's a no-op (no second commit, info-log only); publish with a malformed identifier and verify rejection — confirms the operation is registered in cqrs validator (the regex-mismatch class of bug that hit `IncrementFrontmatterCommand`), the executor runs, idempotency holds, and validation fires

## Verification

```sh
make precommit
```

Manual verification in dev (after deploy):

```sh
# Publish a test command (use the dev Kafka topic + a throwaway task_identifier)
# Inspect controller logs for the executor accepting + applying the command
kubectlquant -n dev logs deploy/agent-task-controller --tail=50

# Verify task file in vault
ls ~/Documents/Obsidian/OpenClaw/tasks/ | grep <test-task-id>

# Re-publish the same command — confirm no-op (no new commit on the OpenClaw repo)
```

## Do-Nothing Option

Each future producer (pr-watcher, jira-watcher, etc.) embeds its own vault git client: clone the repo, write the file, commit, push, handle conflicts with obsidian-git. Costs: SSH key per pod, race conditions, duplicated logic across services, no central audit trail of who created which task. The architecture stays mixed (humans + controller + multiple ad-hoc producers all writing). Acceptable only if there's never going to be a second non-human producer; we already know there will be (jira-watcher, calendar-watcher are in [PR Reviewer Ideas](../../../Obsidian/Personal/23%20Goals/PR%20Reviewer%20Ideas.md) and obvious next steps).

A weaker alternative — a dedicated "task-creator" microservice that producers POST to via HTTP — adds a synchronous request/response boundary that the existing Kafka-based controller doesn't need. Reusing the same Kafka path keeps the architecture consistent.

## Dependent Specs

- `bborbe/code-reviewer` `specs/pr-watcher.md` — the first consumer; depends on this spec shipping first
