---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-05-07T16:03:38Z"
generating: "2026-05-07T16:04:09Z"
prompted: "2026-05-07T16:13:02Z"
branch: dark-factory/human-readable-vault-task-paths
---

## Summary

- Add a required, human-readable `Title` field to the create-task command so new vault task files land at `tasks/{title}.md` instead of `tasks/<uuid>.md`.
- Define strict, cross-platform-safe validation rules for `Title` (length, forbidden characters, path-traversal sequences, Windows reserved names, edge whitespace and dots) and `Body` (length ≤500 KiB; empty body is valid).
- Add a `Validate(ctx)` method on `CreateTaskCommand` and a sender helper that calls `Validate` before publishing — establishing the validation barrier for this one command.
- The agent task controller honors the new `Title` field, re-validates it on receive (defense-in-depth), and falls back to the UUID-named filename on validation failure.
- Existing UUID-named files in the vault are not auto-renamed.

## Problem

Vault task files today are named after the task UUID (e.g. `tasks/a6f38ef6-c979-5c0b-9843-70e540d4bf35.md`), which makes triage by humans browsing the vault practically impossible. The maintainer's spec-019 ships a `filename_hint` field on a watcher-side wrapper, but the alpha agent task controller has not honored that field, and the wrapper is the wrong place for the contract — validation belongs on the command itself, with the controller as the system-of-record for what a valid filename looks like.

## Goal

After this work, every newly created vault task file lands at a human-readable path derived from a required `Title` field on the create-task command. `Title` carries strict cross-platform-safe validation rules enforced both producer-side (sender's `Validate`-before-publish) and consumer-side (controller re-validates with UUID fallback on failure). The `Title` contract is established for the create-task command only; other commands are unchanged. Existing UUID-named files in the vault are not modified.

## Non-goals

- Restructuring `agent/lib` commands into per-command sub-packages following the trading lib template — separate spec at `specs/ideas/agent-lib-command-package-restructure.md`.
- Adding `Validate(ctx)` to `UpdateFrontmatterCommand` or `IncrementFrontmatterCommand` — bundled with the restructure spec above.
- Adding counterfeiter-mocked sender helpers for update or increment commands — bundled with the restructure spec above.
- Migrating maintainer's github-pr or github-build watchers to use the new sender or emit `Title` instead of `FilenameHint` — separate spec, ships after this one.
- Auto-renaming existing UUID-named vault task files.
- Changing the `tasks/` base directory or any cross-cutting vault layout.
- Touching the maintainer's watcher-side `WatcherCreateTaskCommand` wrapper.

## Desired Behavior

1. The `lib.CreateTaskCommand` struct gains a required `Title string` field with a `json:"title"` tag (no `omitempty` — it's required).
2. The `lib.CreateTaskCommand` struct exposes a `Validate(ctx context.Context) error` method that fully enforces its schema rules: `Title` length and character constraints plus `Body` length bounds.
3. A new sender helper for the create-task command exposes a counterfeiter-generated mock and a factory constructor; calling `SendCommand` with an invalid command returns the validation error before publishing.
4. When the controller receives a valid create-task command, the new vault task file is written at `tasks/{title}.md` rather than `tasks/<uuid>.md`.
5. When the controller receives a create-task command whose `Title` fails re-validation, the controller logs a warning and falls back to writing `tasks/{task_identifier}.md` so the task is still materialized.
6. Existing UUID-named files in the vault are not renamed or otherwise modified.
7. Update-frontmatter and increment-frontmatter commands keep using UUID-based file lookup via the existing `FindTaskFilePath` path — the readable filename is set once at create time and is not re-derived later.
8. Operation constant strings are unchanged: `create-task`, `update-frontmatter`, `increment-frontmatter` — wire compatibility with already-published events is preserved.

## Constraints

- The agent task controller is alpha; breaking the in-process Go API of `agent/lib` for `CreateTaskCommand` (adding a required field) is acceptable.
- Both `agent/lib/go.mod` and `agent/task/controller/go.mod` live in the same repository and must build cleanly together.
- Errors must be wrapped with `github.com/bborbe/errors`; `fmt.Errorf` is not used for error wrapping.
- Validation must be composed via `github.com/bborbe/validation` (per-field `validation.Name` composition under `validation.All`).
- Sender helper layout follows the trading template at `trading/lib/command/mail/mail_send-command-sender.go`: counterfeiter directive on the interface, factory constructor, private struct that calls `Validate` before publishing via `cdb.CommandObjectSender`.
- The new sender helper file lives in `agent/lib/` (alongside the existing flat `agent_task-commands.go`); the full per-command package restructure is out-of-scope and tracked separately.
- `Title` validation rules (cross-platform safe):
  - length: `1..200` characters inclusive
  - forbidden characters: `< > : " / \ | ? *` and any control character in `0x00-0x1F` or `0x7F`
  - forbidden sequence: `..` (path traversal)
  - forbidden edges: leading or trailing space, leading or trailing `.`
  - forbidden names (case-insensitive, with or without extension): `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`
  - allowed: letters, digits, spaces, `-`, `_`, `.` mid-name, unicode letters/digits
- `Body` validation rules: length `0..500*1024` bytes (500 KiB). Empty body is valid (a task may have only frontmatter).
- Coverage for the changed packages must be at least 80%.
- WARN + UUID fallback is the **permanent** controller-side contract for invalid `Title`, not a migration affordance — producer bugs surface as actionable WARN logs, the system never drops the task.
- Reference docs (do not duplicate rules here): `docs/kafka-schema-design.md` (Title is a schema addition; controllers MUST treat unknown fields as today), `docs/task-flow-and-failure-semantics.md` (UUID-fallback is a new failure semantic worth recording in this doc once the spec lands).

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Producer constructs a create-task command with empty or oversize `Title` | Sender's `SendCommand` returns a validation error before publishing; nothing reaches Kafka | Producer fixes the field and retries |
| Producer constructs a create-task command with forbidden character / reserved name / path-traversal in `Title` | Sender's `SendCommand` returns a validation error before publishing | Producer sanitizes upstream and retries |
| Controller receives a create-task command whose `Title` fails re-validation (e.g. malicious or buggy producer bypassed sender) | Controller logs WARN, falls back to `tasks/{task_identifier}.md`, and still materializes the task | Operator inspects the WARN log; the task file is still present under its UUID name |
| Controller receives a create-task command and a file already exists at the resolved path | Existing controller idempotency rules apply (no-op on conflict) | None required |
| Producer publishes a `CreateTaskCommand` without `Title` (legacy schema) | Controller's re-validation fails (`Title` empty); WARN + UUID fallback per the rule above | Producer migrates to set `Title` |
| Two create-task commands with different `task_identifier` resolve to the same `tasks/{title}.md` path (Title collision across distinct tasks) | Controller detects the path is already occupied by a different `task_identifier`; logs WARN; falls back to `tasks/{task_identifier}.md` for the colliding write | Producer makes `Title` unique per logical task (e.g. include a discriminator); the original file stays intact, the new task is materialized under its UUID |

## Security / Abuse Cases

- `Title` crosses a trust boundary: it originates in producer code (e.g. a watcher reading external GitHub data) and is later used to construct a filesystem path. Without validation, a hostile or buggy producer could write outside `tasks/` (`../../etc/passwd`), overwrite hidden files (`.git/config`), or create files Windows clients cannot delete (`CON.md`, names with trailing space).
- Validation must run **both** at the sender (cheap, early rejection) **and** at the controller (defense-in-depth — sender bypass is possible since anyone with Kafka write access can publish a raw command). Controller-side failure must not crash the executor; falling back to UUID-named filename keeps the task materialized while flagging the anomaly via WARN.
- `Body` upper bound (500 KiB) caps memory usage and prevents oversized payloads from stalling the executor.
- No retry loop on validation failure: validation errors are deterministic, retrying does not help, and Kafka redelivery would amplify any abuse signal.

## Acceptance Criteria

- [ ] `lib.CreateTaskCommand` has a `Title string` field with `json:"title"` tag (required, no `omitempty`).
- [ ] `lib.CreateTaskCommand` has a `Validate(ctx context.Context) error` method composed via `github.com/bborbe/validation`.
- [ ] `Validate` enforces all listed `Title` rules (length, forbidden chars, control chars, `..` sequence, edge space/dot, Windows reserved names) and the `Body` length bounds.
- [ ] A new sender helper for create-task exists in `agent/lib/`, with a counterfeiter-generated mock and a factory constructor.
- [ ] The sender's `SendCommand` calls `Validate` before publishing via `cdb.CommandObjectSender`; a validation error is returned without publishing.
- [ ] Operation constant strings are unchanged (`create-task`, `update-frontmatter`, `increment-frontmatter`).
- [ ] Controller's create-task executor reads `Title` and writes to `tasks/{title}.md`.
- [ ] Controller re-validates `Title` on receive and, on validation failure, logs a WARN and falls back to `tasks/{task_identifier}.md`.
- [ ] Existing UUID-named files in the vault are not renamed or otherwise modified by this change.
- [ ] Unit tests cover, at minimum: each forbidden character class, length boundaries (min/max for both `Title` and `Body`), every Windows reserved name (case variations and with/without extension), the `..` path-traversal sequence, leading/trailing space and dot, empty title, empty body.
- [ ] Sender tests cover the `Validate`-before-send invariant using counterfeiter mocks (publisher must not be called when validation fails; publisher must be called with the exact command when validation passes).
- [ ] Controller test (table-driven) covers honoring a valid `Title`, falling back on each invalid `Title` class, and not modifying existing files.
- [ ] Controller test covers the Title-collision case: a create-task command with a valid `Title` whose resolved path is already occupied by a file with a different `task_identifier` → WARN + UUID-fallback path is written; the existing file is unchanged.
- [ ] `docs/task-flow-and-failure-semantics.md` updated to record the WARN + UUID-fallback contract for invalid `Title` and for path collisions.
- [ ] `make precommit` is clean in both `agent/lib` and `agent/task/controller`.

**Scenario coverage** — no new scenario. Per the dark-factory `docs/scenario-writing.md` four-condition test (in `bborbe/dark-factory` repo), condition (1) fails: unit + integration tests can reach every behavior. The validator is in our code (fully unit-testable), the sender's `Validate`-before-publish invariant is unit-testable with counterfeiter, the controller's `Title` → path resolution and UUID-fallback branch are integration-testable against a test broker + tmp filesystem. There is no runtime-only failure mode here (contrast dark-factory spec 015's cqrs regex enforcement inside library internals, which struct-shape tests could not see — that bug class does not apply because we own the validator). The watch-flag "new Kafka schema field" is explicitly listed in dark-factory `docs/scenario-writing.md` as not sufficient on its own ("Don't reach for a scenario because 'this touches an integration seam'"). Reference NO example from the same doc: "a new config field whose handler is unit-tested and whose effect is also unit-tested" — same shape as this change.

## Verification

Run from each affected module root:

```
cd agent/lib && make precommit
cd agent/task/controller && make precommit
```

Expected: both commands exit 0, all tests pass, lint clean, coverage ≥80% for the changed packages.

## Do-Nothing Option

Leaving filenames as UUIDs keeps the alpha controller working but locks in indefinite human-unscannable vault triage and forces the maintainer's `filename_hint` workaround to live at the wrong layer (a watcher-side wrapper that the controller does not honor). Doing nothing means accepting that operators continue to grep frontmatter to find a specific PR's task. The full lib restructure (per-command sub-packages, sender helpers for all commands) can be deferred indefinitely without affecting the human-readable-filename win.
