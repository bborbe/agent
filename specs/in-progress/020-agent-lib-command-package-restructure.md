---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-07T17:38:42Z"
generating: "2026-05-07T17:38:43Z"
prompted: "2026-05-07T18:06:54Z"
verifying: "2026-05-07T18:33:37Z"
branch: dark-factory/agent-lib-command-package-restructure
---

## Summary

- Restructure `agent/lib` task commands into per-command sub-packages following the trading lib template (`/Users/bborbe/Documents/workspaces/trading/lib/command/mail/`).
- Each command lives in its own file: `agent/lib/command/task/{create,update-frontmatter,increment-frontmatter}-command.go` — struct + operation constant + `Validate(ctx)` per file.
- Each command has a counterfeiter-mocked sender helper: `agent/lib/command/task/{*}-command-sender.go` — `SendCommandObjectSender` interface, factory constructor, private impl that calls `Validate` before publishing.
- Add `Validate(ctx)` to `UpdateFrontmatterCommand` and `IncrementFrontmatterCommand`; the `CreateCommand` `Validate` shipped by spec 019 carries over unchanged.
- Migrate the create-task command + Validate + sender (introduced by spec 019 across `agent/lib/agent_task-commands.go`, `agent_create-task-command.go`, `agent_create-task-command-sender.go`, and the `lib-create-task-command-sender.go` mock) into the new sub-package; retire those files together with `agent/lib/agent_task-commands.go`.
- Operation strings on the wire are unchanged: `create-task`, `update-frontmatter`, `increment-frontmatter`.

## Problem

After spec 019 ships (verified state at time of writing), `agent/lib` has a half-modernized layout: the create-task command got a `Validate(ctx)` method (in `agent_create-task-command.go`) and a sender helper (in `agent_create-task-command-sender.go` with mock at `mocks/lib-create-task-command-sender.go`), but all three command structs (`CreateTaskCommand`, `UpdateFrontmatterCommand`, `IncrementFrontmatterCommand`) plus their operation constants still live in the flat `agent_task-commands.go`, and the other two commands have no `Validate` and no sender helper. Each remaining producer (e.g. the executor publishing frontmatter updates back to the controller) hand-rolls its marshal-and-send code with no shared validation barrier. The trading lib already established the per-command sub-package pattern; replicating it here makes the `Validate`-before-publish barrier the only sanctioned path for every command and removes the inconsistent half-flat layout left by spec 019.

## Goal

After this work, every agent task command lives in its own sub-package under `agent/lib/command/task/`, exposes a `Validate(ctx)` method, and has a counterfeiter-mocked sender helper that calls `Validate` before publishing via `cdb.CommandObjectSender`. The flat `agent_task-commands.go` file no longer exists. Wire-format compatibility is preserved — operation strings and JSON payload field names are unchanged. Internal callers (executor, controller-side senders) use the new sub-package types.

## Non-goals

- Adding new fields to any command — the `Title` field on `CreateCommand` is the only schema change in this thread, and it ships in spec 019, not here.
- Changing the controller's command-handling behavior — handlers continue to consume the same payloads with the same semantics.
- Migrating maintainer's github-pr or github-build watchers to the new sender helpers — separate spec, ships after this one.
- Changing wire-format JSON tags or operation strings.
- Introducing a new validation library or replacing `github.com/bborbe/validation`.

## Desired Behavior

1. `agent/lib/command/task/` package exists, with one file per command (`create-command.go`, `update-frontmatter-command.go`, `increment-frontmatter-command.go`) and one sender file per command (`*-command-sender.go`).
2. The flat `agent/lib/agent_task-commands.go` file no longer exists; all three command structs and operation constants live in their per-command files. The 019-introduced files in `agent/lib/` (`agent_create-task-command.go`, `agent_create-task-command-sender.go`, `agent_create-task-command-sender_test.go`, the create-task validate tests inside `agent_task-commands_test.go`, and `mocks/lib-create-task-command-sender.go`) are also removed — their content lives in the new sub-package.
3. Each of the three commands has a `Validate(ctx context.Context) error` method composed via `github.com/bborbe/validation`.
4. The `CreateCommand` `Validate` from spec 019 carries over with the same rules (Title and Body validation); `UpdateFrontmatterCommand.Validate` and `IncrementFrontmatterCommand.Validate` are new and enforce their existing schema (required fields, field-shape rules already implicit in the struct).
5. Each command has a counterfeiter-generated mock for its sender interface and a `New…CommandObjectSender` factory constructor.
6. Each sender's `SendCommand` calls `Validate` before publishing via `cdb.CommandObjectSender`; a validation error is returned without publishing.
7. Internal callers in the executor and controller are migrated to the new sub-package types and senders. Concretely (verified at time of spec writing): `task/controller/pkg/command/task_create_task_executor.go` (uses `lib.CreateTaskCommand`, `lib.CreateTaskCommandOperation`), `task/controller/pkg/command/task_update_frontmatter_executor.go` (uses `lib.UpdateFrontmatterCommand`, `lib.UpdateFrontmatterCommandOperation`), `task/controller/pkg/command/task_increment_frontmatter_executor.go` (uses `lib.IncrementFrontmatterCommand`, `lib.IncrementFrontmatterCommandOperation`), and `task/executor/pkg/result_publisher.go` (uses `lib.UpdateFrontmatterCommand`, `lib.IncrementFrontmatterCommand` plus their operations) — all imports updated to the new sub-package. The matching `*_test.go` files alongside each caller (and the lib `*_test.go` files for command structs and senders) are migrated in lockstep.
9. Go type rename: with the package itself named `task`, the existing `lib.CreateTaskCommand` becomes `task.CreateCommand`, `lib.UpdateFrontmatterCommand` becomes `task.UpdateFrontmatterCommand`, and `lib.IncrementFrontmatterCommand` becomes `task.IncrementFrontmatterCommand` (idiomatic Go drops the redundant package-name prefix on the create command; the other two retain their disambiguating suffix). On-the-wire JSON struct tags (`taskIdentifier`, `title`, `frontmatter`, `body`, `updates`, `field`, `delta`) and operation strings are unchanged.
8. Operation constant strings on the wire are unchanged: `create-task`, `update-frontmatter`, `increment-frontmatter`.

## Constraints

- **Sequencing**: spec 019 (`human-readable-vault-task-paths`) must ship first (status `verifying` at time of writing). It introduced `CreateCommand.Validate` and the create-task sender helper as separate files inside `agent/lib/` (alongside the pre-existing flat `agent_task-commands.go`); this spec relocates them into the new sub-package and replicates the pattern across the other two commands.
- The agent task controller is alpha; breaking the in-process Go API of `agent/lib` (renaming types, moving them to a sub-package) is acceptable as long as on-the-wire JSON payload field names and operation strings stay byte-identical.
- Both `agent/lib/go.mod` and `agent/task/controller/go.mod` live in this same repository and must build cleanly together after the migration.
- Errors must be wrapped with `github.com/bborbe/errors`; `fmt.Errorf` is not used for error wrapping.
- Validation must be composed via `github.com/bborbe/validation` (per-field `validation.Name` composition under `validation.All`), matching spec 019.
- Sender layout must follow the trading template at `trading/lib/command/mail/mail_send-command.go` and `mail_send-command-sender.go`: package per command-family, one struct + operation constant + `Validate` per file, one sender interface + factory + private struct per file, counterfeiter directive on the interface.
- Mocks live under `agent/lib/command/task/mocks/` (matching the trading lib's relative-path counterfeiter directive convention).
- Coverage for `agent/lib/command/task/` must be at least 80%.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Producer constructs an `UpdateFrontmatterCommand` with empty `TaskIdentifier` or empty `Updates` map | Sender's `SendCommand` returns a validation error before publishing | Producer fixes the field and retries |
| Producer constructs an `IncrementFrontmatterCommand` with empty `TaskIdentifier` or empty `Field` | Sender's `SendCommand` returns a validation error before publishing | Producer fixes the field and retries |
| Internal caller still imports the flat `lib.CreateTaskCommand` / `lib.UpdateFrontmatterCommand` / `lib.IncrementFrontmatterCommand` types after the migration | Compile error (the lib API moved) | Migrate the caller to the new sub-package types |
| External (already-published) Kafka events with the existing payload shape arrive at the controller after deploy | Controller decodes them identically — wire format is byte-identical for all three operations | None required — the schema is preserved |

## Acceptance Criteria

- [ ] `agent/lib/command/task/` package exists with `create-command.go`, `update-frontmatter-command.go`, `increment-frontmatter-command.go`, and matching `*-command-sender.go` files.
- [ ] Each of the three command types has a `Validate(ctx context.Context) error` method composed via `github.com/bborbe/validation`.
- [ ] `CreateCommand.Validate` enforces the same `Title` + `Body` rules established by spec 019 (no behavior change to that validator).
- [ ] `UpdateFrontmatterCommand.Validate` enforces: `TaskIdentifier` non-empty AND at least one of (`Updates` non-empty, `Body` non-nil) — a no-op update with both empty is rejected as a producer bug.
- [ ] `IncrementFrontmatterCommand.Validate` enforces: `TaskIdentifier` non-empty, `Field` non-empty. `Delta` is unconstrained (zero and negative are valid — the controller is the source of truth for "decrement allowed here").
- [ ] Each command has a counterfeiter-generated sender mock at `agent/lib/command/task/mocks/` and a `New…CommandObjectSender` factory constructor.
- [ ] Each sender's `SendCommand` calls `Validate` before publishing via `cdb.CommandObjectSender`; a validation error is returned without publishing.
- [ ] The flat `agent/lib/agent_task-commands.go` file is deleted from the repository.
- [ ] All internal callers in `agent/task/executor/` and `agent/task/controller/` that previously constructed `lib.{CreateTask,UpdateFrontmatter,IncrementFrontmatter}Command` are migrated to the new sub-package types — concretely: `task/controller/pkg/command/task_create_task_executor.go`, `task_update_frontmatter_executor.go`, `task_increment_frontmatter_executor.go`, and `task/executor/pkg/result_publisher.go`.
- [ ] The 019-introduced files in `agent/lib/` (`agent_create-task-command.go`, `agent_create-task-command-sender.go`, `agent_create-task-command-sender_test.go`, the create-task validate test cases inside `agent_task-commands_test.go`, and `mocks/lib-create-task-command-sender.go`) are removed; their content lives in the new sub-package (`agent/lib/command/task/`).
- [ ] Operation constant strings are unchanged on the wire (`create-task`, `update-frontmatter`, `increment-frontmatter`); a wire-format test asserts byte-identical JSON output for each command type before and after.
- [ ] Sender tests cover the `Validate`-before-send invariant for each command using counterfeiter mocks (publisher must not be called when validation fails; publisher must be called with the exact command when validation passes).
- [ ] Unit tests for each `Validate` cover the required fields, edge cases (empty maps, empty strings, zero values), and at least one happy-path call.
- [ ] Coverage for `agent/lib/command/task/` is ≥80%.
- [ ] `make precommit` is clean in `agent/lib`, `agent/task/controller`, and `agent/task/executor`.

**Scenario coverage** — no new scenario. Per dark-factory `docs/scenario-writing.md` four-condition test, condition (1) fails: this is a pure refactor with no behavior change. The same payloads flow over the same operations; the only externally observable difference is the location of the Go types. Wire-format byte-identity is asserted at the unit level (marshal-then-compare). The dark-factory NO example "A refactor that splits one function into two; behavior unchanged" applies directly.

## Verification

```
cd agent/lib && make precommit
cd agent/task/controller && make precommit
cd agent/task/executor && make precommit
```

Expected: all three exit 0, all tests pass, lint clean, coverage ≥80% for `agent/lib/command/task/`.

## Do-Nothing Option

Leaving the half-flat layout in place after spec 019 ships keeps `CreateCommand` enjoying `Validate` + sender while `UpdateFrontmatterCommand` and `IncrementFrontmatterCommand` are still raw structs that every producer hand-publishes. The cost: the validation barrier is enforced for one command and not the other two, so per-producer validation drift continues for the un-modernized commands; the lib's organization diverges from the trading lib pattern; mocking individual command senders requires extra wrapper code. Doing nothing is acceptable only if we're willing to live with that asymmetry indefinitely. Note: the `Title` field win is already delivered by spec 019 — this spec does not gate the user-visible win, it cleans up the architectural seam.
