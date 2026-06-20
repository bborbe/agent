---
status: verifying
approved: "2026-06-20T14:57:22Z"
generating: "2026-06-20T14:57:54Z"
prompted: "2026-06-20T15:05:45Z"
verifying: "2026-06-20T15:17:03Z"
branch: dark-factory/bug-task-controller-filename-collision-idempotency
---

# Task controller filename-collision idempotency

## Summary

- `agent/task/controller` silently overwrites existing vault task files when `task.CreateCommand` is replayed for a filename that already exists.
- Root cause: `NewCreateTaskExecutor` uses `os.Stat(absPath)` and `os.ReadFile(titlePath)` to gate writes — but the git-rest adapter's `Path()` returns a logical base path that does NOT exist on local disk, so the existence checks always fail and the executor always writes.
- Effect: every hourly recurring-task-creator tick and every `/trigger?date=…` replay strips runtime-added frontmatter (`claude_session_id`, `phase`) and any in-progress body edits from already-materialized recurring task files.
- Fix: switch to filename-based existence check via `gitClient.ReadFile(ctx, titlePath)` (HTTP round-trip to git-rest); on collision, return a typed sentinel `task.ErrTaskAlreadyExists` so the CQRS framework emits a Failure on the result topic — publishers can classify and treat as benign.
- Out of scope: recurring-task-creator's result-classifier + benign prom counter — separate spec in that repo.

## Problem

The recurring-task-creator publishes one `task.CreateCommand` per (slug, period-token) every hourly tick. Per its README the pipeline is "safe to re-publish every tick" because the downstream controller is expected to dedupe by identifier. In practice the controller doesn't dedupe at all when running against git-rest — it writes every command. After the first successful create on day D, every subsequent tick rewrites the file from the schedule template, stripping any fields the operator or downstream agents added since (notably `claude_session_id` and `phase`, plus body checkbox progress). Reproduced in prod 2026-06-20: a manual `/trigger?date=2026-06-20` produced 12 `git-rest: update` commits (one per existing W25-sat task) each stripping `claude_session_id` + `phase`. Vault commit `7279ef31d` is one such diff.

## Goal

When `task.CreateCommand` arrives for a `<title>.md` that already exists in the vault, the controller writes nothing and emits one `Failure` on the result topic carrying the typed sentinel `task.ErrTaskAlreadyExists`. First-create commands for new filenames still succeed unchanged. The sentinel is exported from `lib/command/task` so other repos (recurring-task-creator) can match it via `errors.Is` and classify the failure as benign.

## Non-goals

- recurring-task-creator's result-classifier + `recurring_task_creator_already_exists_total` prom counter — separate spec in that repo (controller half ships first).
- Migrating already-corrupted task files in the vault. Operator restores manually via `git revert` of the offending update commits.
- Changes to `UpdateCommand` or `DeleteCommand` executors.
- Changing the title-validation defense or the UUID fallback for the *Title-fails-validation* case — keep that path intact; only the *collision* branch changes.
- Reworking the recurring-task-creator README's "safe to re-publish every tick" claim — still true after the fix; replays just produce benign Failures instead of corruption.

## Acceptance Criteria

- [ ] **AC1 — Sentinel exported with stable identity.** `grep -nE 'var ErrTaskAlreadyExists\s*=' lib/command/task/*.go` returns one match. `cd lib && go doc ./command/task ErrTaskAlreadyExists` exits 0 and prints a GoDoc line. Evidence shape: file content + go doc output.
- [ ] **AC2 — Replay of existing filename returns sentinel + writes nothing.** Ginkgo test in `task/controller/pkg/command/` runs the executor twice against a stub `GitClient` whose `ReadFile(ctx, titlePath)` returns valid file content on the second call. Assertions: second call returns an error satisfying `errors.Is(err, task.ErrTaskAlreadyExists)`, AND the stub's `AtomicWriteAndCommitPushCallCount()` after the second call equals 1 (only the first create wrote). Evidence shape: `cd task/controller && make test` exits 0 with the new test row name in output.
- [ ] **AC3 — First create for new filename still writes.** Ginkgo test: stub `ReadFile` returns a "not found" error → executor calls `AtomicWriteAndCommitPush` exactly once, returns nil. Evidence shape: same `make test` run includes the test row.
- [ ] **AC4 — Different `task_identifier` at same title path still returns sentinel.** Ginkgo test: stub `ReadFile` returns existing file content whose frontmatter `task_identifier` differs from `cmd.TaskIdentifier` → executor returns `errors.Is(err, task.ErrTaskAlreadyExists)`, no write. Filename owns the slot; the controller does not consult frontmatter for collision-detection — the comparison is removed (see DB7), collision is detected by filename presence alone regardless of identifier. Evidence shape: test row in `make test`.
- [ ] **AC5 — Transient git-rest read error propagates without writing.** Ginkgo test: stub `ReadFile` returns a non-"not found" error → executor returns that error wrapped, `AtomicWriteAndCommitPushCallCount()` stays 0. Evidence shape: test row in `make test`.
- [ ] **AC6 — `make precommit` clean in `task/controller`.** Evidence shape: `cd task/controller && make precommit` exits 0.
- [ ] **AC7 — Post-Deploy (Rung-2):** dev controller no longer overwrites existing tasks — evidence: with feature branch image deployed, an HTTP `POST /admin/recurring-task-creator/trigger?date=<today>` against dev followed by `cd ~/Documents/Obsidian/Personal && git fetch && git log --since="2 minutes ago" --oneline -- "24 Tasks/"` returns zero `git-rest: update` commits for already-existing recurring tasks.
  - `deploy_check:` `kubectlquant -n dev get deploy/agent-task-controller-personal -o jsonpath='{.spec.template.spec.containers[0].image}' | awk -F: '{print $NF}'`
  - `deploy_target:` `$(git rev-parse --short HEAD)`

## Verification

```bash
# unit + integration
cd lib && make precommit
cd task/controller && make precommit

# go doc on the sentinel
cd lib && go doc ./command/task ErrTaskAlreadyExists
```

Post-deploy (manual, after `make buca` to dev):
```bash
curl -s "https://dev.quant.benjamin-borbe.de/admin/recurring-task-creator/trigger?date=$(date -u +%F)"
sleep 60  # allow Kafka consumer + git-rest to drain
cd ~/Documents/Obsidian/Personal && git fetch && git log --since="2 minutes ago" --oneline -- "24 Tasks/"
# expect: zero "git-rest: update" commits for already-existing recurring tasks
```

## Desired Behavior

1. `gitClient.ReadFile(ctx, titlePath)` (HTTP GET to git-rest) replaces `os.ReadFile(titlePath)` in `resolveCreateTaskPath` and `os.Stat(absPath)` in the executor body.
2. On "file exists" response, `NewCreateTaskExecutor` returns `errors.Wrapf(ctx, task.ErrTaskAlreadyExists, "title path %s occupied", titlePath)` — no write call, no event.
3. On "not found" response, executor proceeds to `AtomicWriteAndCommitPush` as today.
4. On any other read error (network, 5xx, timeout), executor wraps and returns the error — no write call.
5. `task.ErrTaskAlreadyExists` is exported from `lib/command/task` (package-level `var`) so cross-repo callers match via `errors.Is`. Implements `error`; no extra fields needed.
6. The UUID-fallback branch in `resolveCreateTaskPath` survives only for the Title-fails-validation and Title-contains-path-separator cases. The collision branch (different `task_identifier` at title path) is removed — collision now returns the sentinel via the caller, not a rename.
7. The frontmatter re-parse + `task_identifier` comparison in `resolveCreateTaskPath` is removed — filename ownership is decided by filename, not identifier.

## Constraints

- `gitClient` interface (`task/controller/pkg/gitrestclient/git_client_adapter.go`) already exposes `ReadFile(ctx, path) ([]byte, error)` — do NOT add new methods to the interface.
- Sentinel must be matchable via `errors.Is` — declare with `stderrors.New` (Go stdlib `errors`), not `github.com/bborbe/errors`, since the latter is for wrapping.
- Existing title-validation defense (`cmd.Validate(ctx)` + `strings.ContainsAny(cmd.Title, "/\\")`) and its UUID-fallback are unchanged — keep both lines intact.
- No new exported symbols in `lib/command/task` other than `ErrTaskAlreadyExists`.
- No changes to `task.CreateCommand` wire shape — sentinel is server-side only.
- Mocks: regenerate via `make generate` if the `GitClient` interface or any executor dep is touched (it shouldn't be).
- Ginkgo v2 / Gomega; counterfeiter for mocks; external test packages (`*_test`).
- `github.com/bborbe/errors.Wrapf` for wrapping; never `fmt.Errorf`, never bare `return err`.

## Reproduction

**Repro on prod 2026-06-20:**

```bash
# precondition: 12 W25-sat recurring task files already exist in 24 Tasks/ on Personal vault,
# each with operator-added fields like `claude_session_id` and `phase: planning`.

curl -s "https://prod.quant.benjamin-borbe.de/admin/recurring-task-creator/trigger?date=2026-06-20"
# response: {"date":"2026-06-20","published":36,"errors":[]}

cd ~/Documents/Obsidian/Personal && git log --since="10 minutes ago" --oneline -- "24 Tasks/"
# observed: 12 commits "git-rest: update 24 Tasks/<title> - 2026W25-sat.md"

git show 7279ef31d
# observed diff for "Turn on hell - 2026W25-sat.md":
#  -claude_session_id: b8211574-dfee-417a-9b8d-5c4753ffe6d5
#  -phase: planning
# (template fields like assignee/priority/planned_date preserved; runtime fields stripped)
```

Recurring-task-creator publisher: `~/Documents/workspaces/recurring-task-creator/pkg/publisher/publisher.go` (verified — sends only `task.CreateCommand`, not Update).

## Expected vs Actual

**Expected** (per `~/Documents/workspaces/recurring-task-creator/README.md` line "safe to re-publish every tick, safe to manual `/trigger?date=YYYY-MM-DD` replay"):
Replayed `CreateCommand` for an existing `<title>.md` is a no-op. The vault file is byte-identical before and after `/trigger`.

**Actual:**
The controller writes a fresh template to the existing filename on every replay, stripping runtime-added frontmatter fields and any in-progress body edits. `git log` shows one `git-rest: update` commit per existing recurring task per trigger.

## Why this is a bug

The controller's executor in `task/controller/pkg/command/task_create_task_executor.go:71-77` and `resolveCreateTaskPath` lines 137–187 use `os.Stat` and `os.ReadFile` on a path derived from `gitClient.Path()`. That path is logical — the git-rest adapter at `task/controller/pkg/gitrestclient/git_client_adapter.go` line 64 documents this explicitly:

> Path returns the logical base path. Note: this path does NOT exist on disk when using the gitrest adapter — callers must use ReadFile/WriteFile/ListFiles instead of direct filesystem operations.

The executor violates this contract. `os.Stat` and `os.ReadFile` on a non-existent local path return "file not found" → the idempotency branch never fires → every command writes.

## Failure Modes

| Trigger | Detection | Expected behavior | Reversibility | Recovery |
|---|---|---|---|---|
| git-rest read times out / 5xx during collision check | Result topic Failure with wrapped error; controller logs at `glog.Warningf` | Executor returns wrapped error, no write, offset commits (CQRS framework converts err → Failure result, no kafka replay) | Reversible — next tick re-publishes the same `CreateCommand`; transient git-rest recovery → next attempt succeeds | None needed — pipeline self-heals on next tick |
| git-rest returns malformed response (not "found" / not "not-found") | Same as above — error propagates | Same — wrap + return | Reversible | Operator investigates git-rest health; recurring-task-creator retries hourly |
| Collision on title path with different `task_identifier` (extremely rare — would mean a UUID-named file moved to title path, or a human created a file with the recurring task's exact title) | Result topic Failure with sentinel; recurring-task-creator (post-spec-2) increments benign counter | Sentinel returned, no write, no overwrite of human file | Non-destructive — human file untouched | Operator manually renames or removes the colliding file |
| Stale CreateCommand replayed from kafka after fix deployed | Failure on result topic | Sentinel returned, no write | Non-destructive | None — replay was idempotent by design |

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Export `ErrTaskAlreadyExists` sentinel in `lib/command/task` + GoDoc | 5 | 1 | — |
| 2 | Swap `os.Stat`/`os.ReadFile` → `gitClient.ReadFile` in executor; remove UUID-fallback-on-collision + frontmatter re-parse; return wrapped sentinel | 1, 2, 3, 4, 6, 7 | 2, 3, 4, 5, 6 | prompt 1 |

Two prompts is enough — prompt 1 is the cross-repo contract (sentinel); prompt 2 is the executor change + tests. Prompt 1 ships first so prompt 2 can import the sentinel.

## Do-Nothing Option

Cost: every hourly recurring-task-creator tick continues to overwrite every existing recurring task file in the vault. Operator workflows depending on `phase`, `claude_session_id`, body checkbox progress survive at most ~1 hour. Manual `/trigger` replays remain destructive. Operator loses trust in the recurring pipeline and either disables the publisher (regression: stops fulfilling [[Migrate recurring Jira subtasks to vault task system]]) or works around it by avoiding any in-file edits to recurring tasks (regression: breaks the whole agent → vault workflow).

## Open Questions

- Should same-`task_identifier` replay be silently skipped (`ErrCommandObjectSkipped` — no result, no event) while different-id title-collision returns the sentinel (Failure)? Cleaner publisher behavior — no Failure noise on the normal hourly tick — but adds a frontmatter re-parse round-trip. **Recommend: no — filename-only check is simpler; recurring-task-creator's prom counter handles the Failure noise and gives explicit observability into replay rate.**
- Should the executor log at INFO or WARN on collision? **Recommend: INFO — collision is expected during the manual-trigger and stale-replay paths; WARN would be alert noise.**

## Verification Result

**Verified:** 2026-06-20T17:59:26Z (HEAD 19ef379, release v0.69.0)
**Binary:** installed dark-factory v0.182.0 (/Users/bborbe/Documents/workspaces/go/bin/dark-factory)
**Scenario:** Three dev `/trigger` replays + one prod `/trigger` against feature-merged controllers; observed file creation on first-tick, byte-preserving no-op on operator-edited replay, sentinel-only path on prod 36-task replay.
**Evidence:**
- AC1: `lib/command/task/errors.go:18: var ErrTaskAlreadyExists = stderrors.New("task file already exists at title path")`; `go doc ./command/task ErrTaskAlreadyExists` prints stable GoDoc.
- AC2-5: `cd task/controller && make test` → `52 Passed | 0 Failed`; explicit Ginkgo `It` rows present: `returns ErrTaskAlreadyExists and does not write (AC2)`, `writes exactly once and returns nil when ReadFile reports not-found (AC3)`, `returns ErrTaskAlreadyExists and does not write (AC4)`, `propagates the wrapped error and does not write (AC5)`.
- AC6: `make precommit` clean in both `task/controller` and `lib` (`ready to commit`).
- AC7 dev: image tag `dev` deployed on `sts/agent-task-controller-personal`; trigger #1 → vault commit `0e906ba7a` (1× `git-rest: create`); trigger #3 against operator-edited file (3fbf1cfb0, 467 bytes) → controller log `title path … already occupied (467 bytes), returning ErrTaskAlreadyExists`, ZERO `git-rest: update` commits.
- AC7 prod: image tag `prod` deployed; trigger published 36 → ZERO `git-rest: update` commits; controller log shows 36× `ErrTaskAlreadyExists`.
- Spec-text note: AC7 `deploy_check:` references `deploy/agent-task-controller-personal`, but actual workload is `sts/agent-task-controller-personal`; behavior verified via STS variant — spec text needs editing in a follow-up but does not block this completion.
**Verdict:** PASS
