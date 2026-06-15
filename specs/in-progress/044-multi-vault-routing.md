---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-06-14T21:06:26Z"
generating: "2026-06-14T21:13:00Z"
prompted: "2026-06-14T21:25:02Z"
verifying: "2026-06-15T15:20:33Z"
branch: dark-factory/multi-vault-routing
---

## Summary

- Add a top-level `targetVault` field to the `task.CreateCommand` Kafka payload so producers can declare which Obsidian vault a task belongs in.
- Teach the `CreateCommandSender` constructor to accept a per-producer default vault that fills in `targetVault` when the caller leaves it empty.
- Teach the task controller to read its own vault identity from a required env var and skip commands whose effective target vault is not its own.
- Preserve wire compatibility for legacy producers: empty `targetVault` on the wire keeps flowing — at the controller it falls back to the legacy default vault `openclaw`.
- Keep one controller deployment per vault; no per-vault topics, no per-vault URL maps inside a single controller.

## Problem

Today every `task.CreateCommand` published on `agent-task-v1-request` is consumed by every running task controller. We are about to run two controllers (one for the `openclaw` vault, one for the `personal` vault), each backed by its own `vault-obsidian-*` git-rest target. Without a routing key on the command, both controllers would materialize every create command to their vault, producing duplicate task files across vaults. Producers (recurring-task-creator, maintainer, the controller's own publishers) currently have no way to declare which vault a task belongs in.

## Goal

A producer can declare the target vault for a task — either explicitly per command or as a producer-wide default fixed at construction time — and a controller will only act on commands destined for the vault it owns. Legacy producers that emit no target vault continue to function: their commands are routed to the `openclaw` controller as today.

## Non-goals

- Do NOT introduce per-vault Kafka topics; routing stays on the existing `agent-task-v1-request` topic via the new payload field.
- Do NOT add frontmatter-based routing — `targetVault` is a top-level field on the command, not a frontmatter key.
- Do NOT make a single controller process multiple vaults; one controller, one vault target, one `GIT_REST_URL` — invariant; if a future deployment demands fan-out, that's a separate spec.
- Do NOT change `recurring-task-creator`, the `bborbe/maintainer` repo, or any k8s manifest in this spec — those are explicit follow-ups.
- Do NOT introduce a config flag to disable vault filtering — the filter is invariant; if a future consumer needs to disable it, that's a separate spec.
- Do NOT migrate existing producers to set `targetVault` explicitly — the controller-side legacy fallback covers them.

## Desired Behavior

1. `task.CreateCommand` carries an optional top-level string field `TargetVault` (JSON `targetVault`, `omitempty`). The zero value (`""`) is a valid wire form and round-trips through JSON unchanged.
2. When `TargetVault` is non-empty, `CreateCommand.Validate` requires it to match `^[a-z][a-z0-9-]*$` — a slug starting with a lowercase letter, followed by lowercase letters, digits, or hyphens. Any other non-empty value is a validation error and is rejected before publish.
3. `NewCreateCommandSender` takes a second argument `defaultVault string`. When the sender's `SendCommand` is called with a command whose `TargetVault` is empty AND `defaultVault` is non-empty, the sender substitutes `defaultVault` into the command before publishing. If both are empty, the command is published with `TargetVault` empty. If the command's `TargetVault` is already set, the sender publishes it as-is without overriding.
4. The substituted `TargetVault` is itself revalidated against the slug regex; an invalid `defaultVault` provided at construction surfaces as a validation error at `SendCommand` time (not at construction).
5. The task controller binary requires a new env var `MY_VAULT` (CLI flag `--my-vault`). It is a required configuration value; startup fails with a clear error if it is empty or fails the slug regex.
6. On each `CreateCommand` the controller consumes from `agent-task-v1-request`, it computes an effective target vault: if the command's `TargetVault` is empty, the effective target is the literal string `openclaw` (legacy fallback); otherwise the effective target is the command's `TargetVault` verbatim. The controller processes the command iff the effective target equals `MY_VAULT`; otherwise it is skipped without side effects (no git write, no result publish, no error returned to the consumer loop).
7. Skipped commands log a single structured line at V(2) naming the command's `TargetVault`, the effective target, and `MY_VAULT`, so an operator can confirm routing decisions in the controller logs.
8. All existing in-repo call sites of `NewCreateCommandSender` are updated to pass `""` as the second argument, preserving current behavior. No producer in this repo gains a non-empty default in this spec — that is left to follow-up specs for individual producers.

## Constraints

- Wire format MUST stay backward compatible: a command serialized without `targetVault` by an old producer must deserialize cleanly in the new controller, and vice versa. `omitempty` is the mechanism.
- The Kafka topic name, schema ID (`agent-task-v1`), and command operation (`create-task`) MUST NOT change.
- All existing tests in `lib/command/task/...` and `task/controller/...` must continue to pass after the changes (with their call sites updated to the new sender signature).
- Project DoD (`docs/dod.md`) applies: exported symbols documented, errors wrapped via `github.com/bborbe/errors`, Ginkgo v2 / Gomega / Counterfeiter for tests, factories pure composition, no `context.Background()` in factories.
- Reference: `docs/kafka-schema-design.md` (existing schema/topic conventions stay intact), `docs/dod.md`.

## Failure Modes

| Trigger | Detection | Expected behavior | Recovery | Reversibility |
|---|---|---|---|---|
| Producer emits `targetVault` with uppercase or whitespace | Sender's `Validate` returns wrapped validation error | `SendCommand` returns error; Kafka send not attempted | Producer fixes the value and retries | Reversible (no side effects) |
| Producer constructed with invalid `defaultVault` (e.g. `"My Vault"`) | First `SendCommand` call returns wrapped validation error | Error surfaces on first send, not at construction | Operator fixes deployment env / wiring code and redeploys | Reversible |
| Controller starts with empty `MY_VAULT` | Startup error log, non-zero exit | Process exits before consuming any command | Operator sets the env var and restarts | Reversible |
| Controller starts with invalid `MY_VAULT` (fails slug regex) | Startup error log, non-zero exit | Process exits before consuming any command | Operator fixes the env var and restarts | Reversible |
| Legacy command with empty `targetVault` reaches `personal` controller | V(2) log line "skipped: effective=openclaw my=personal" | Command skipped, no git write, consumer advances offset | None needed — by design | n/a |
| Two controllers (`openclaw` + `personal`) running simultaneously, legacy command arrives | Both controllers log routing decision; only `openclaw` processes | `openclaw` writes file and commits; `personal` skips | None needed — by design | n/a |
| Command targets a vault no controller serves (e.g. `targetVault: "obsolete"`) | All controllers log skip with effective=obsolete | Command is silently dropped by every consumer; offset still advances | Operator either deploys a controller for that vault OR producer corrects the value | Irreversible for the dropped command (operator can republish) |
| Mid-stream crash of one controller during processing | Existing CQRS consumer-loop semantics (BoltDB offset) apply | On restart, the consumer resumes from last committed offset; routing filter re-evaluates the same command identically | Standard controller restart | Same as today |
| Schema drift: new producer adds `targetVault`, old controller deployed | Old controller ignores the field (unknown JSON key) and routes every command as today | No regression on legacy deployments during rollout | Operator finishes rollout | Reversible |

## Security / Abuse Cases

- A producer with write access to the topic could set `targetVault` to any slug to redirect a task to a different vault's controller. This is acceptable: producers are already trusted to publish on the topic, and the routing field does not grant access to vaults — only a controller actually deployed for that vault would act on the command. No new trust boundary is created.
- The slug regex `^[a-z][a-z0-9-]*$` prevents path traversal, whitespace, control characters, and case-folding ambiguity in the routing key. The field never reaches `gitClient.AtomicWriteAndCommitPush`; it is only compared for equality, so injection into file paths is not a risk.
- No retry-forever path is introduced: skipped commands return `nil` to the consumer loop so the offset advances; they do not loop.

## Acceptance Criteria

- [ ] `CreateCommand` exposes a string field with JSON tag `targetVault,omitempty`. Evidence: `grep -n 'targetVault' lib/command/task/create-command.go` returns at least one matching line.
- [ ] A `CreateCommand` whose `TargetVault` is `""` round-trips through `json.Marshal` → `json.Unmarshal` with `TargetVault == ""` and produces JSON that does not contain the substring `targetVault`. Evidence: Ginkgo test in `lib/command/task/create-command_test.go` asserting both conditions.
- [ ] A `CreateCommand` whose `TargetVault` is `"personal"` round-trips through marshal/unmarshal preserving the value, and the marshaled JSON contains `"targetVault":"personal"`. Evidence: Ginkgo test assertion.
- [ ] `CreateCommand.Validate` returns nil when `TargetVault` is `""`, returns nil for valid slugs (`"openclaw"`, `"personal"`, `"vault-2"`), and returns a wrapped validation error for invalid values (`"Personal"`, `" personal"`, `"per sonal"`, `"1personal"`, `"-personal"`). Evidence: Ginkgo table test enumerating each case.
- [ ] `NewCreateCommandSender(commandObjectSender, "")` produces a sender whose `SendCommand` publishes commands with `TargetVault` unchanged from the input. Evidence: Ginkgo test capturing the `cdb.CommandObject` passed to the fake `CommandObjectSender` and asserting the embedded payload's `TargetVault` equals the input's.
- [ ] `NewCreateCommandSender(commandObjectSender, "personal")` produces a sender that publishes commands with `TargetVault == "personal"` when the input's `TargetVault` is `""`. Evidence: Ginkgo test capturing the published payload and asserting `TargetVault == "personal"`.
- [ ] `NewCreateCommandSender(commandObjectSender, "personal")` does NOT override an input whose `TargetVault` is `"openclaw"` — the published payload retains `"openclaw"`. Evidence: Ginkgo test assertion.
- [ ] `NewCreateCommandSender(commandObjectSender, "Bad Vault")` returns a sender whose first `SendCommand` call returns a wrapped validation error (construction itself does not error). Evidence: Ginkgo test.
- [ ] All existing call sites of `NewCreateCommandSender` in this repo compile and pass tests after being updated to the two-argument form. Evidence: `make precommit` exits 0 in both `lib/` and `task/controller/`.
- [ ] The task controller binary fails to start if `MY_VAULT` is unset or empty. Evidence: a Ginkgo test on the `application.Run` (or equivalent integration-level harness) showing a non-nil error whose message names `MY_VAULT`. (Acceptable alternative evidence: invoking the binary with the env var unset and observing non-zero exit and an error line mentioning `my-vault`.)
- [ ] The task controller binary fails to start if `MY_VAULT` does not match `^[a-z][a-z0-9-]*$`. Evidence: same harness as above with `MY_VAULT=Bad`, asserting non-nil error referencing the slug rule.
- [ ] The controller's command routing — exposed as a small, separately testable predicate (e.g. `func ShouldProcess(cmd task.CreateCommand, myVault string) bool` in a controller-internal package) — returns `true` for the matrix (cmd `TargetVault=""` , myVault `openclaw`), (cmd `TargetVault="openclaw"`, myVault `openclaw`), (cmd `TargetVault="personal"`, myVault `personal`), and `false` for (cmd `TargetVault=""` , myVault `personal`), (cmd `TargetVault="openclaw"`, myVault `personal`), (cmd `TargetVault="other"`, myVault `openclaw`). Evidence: Ginkgo table test asserting each case.
- [ ] The `CreateTaskExecutor` (or whichever component is wired into the CQRS consumer for `create-task`) consults the routing predicate and returns no error / no event / no state mutation when `ShouldProcess` is false. Evidence: Ginkgo test injecting a fake `GitClient`, invoking the executor with `MyVault="personal"` and a command with `TargetVault="openclaw"`, and asserting (a) zero calls on the git client's write methods and (b) executor returns `(nil, nil, nil)`.
- [ ] The same executor with `MyVault="openclaw"` and command `TargetVault="openclaw"` invokes the git client's write path exactly once. Evidence: Ginkgo test asserting `gitClient.AtomicWriteAndCommitPushCallCount() == 1`.
- [ ] When a command is skipped due to vault mismatch, the controller emits a structured log line containing the command's `TargetVault`, the effective target, and `MY_VAULT`. Evidence: a test capturing glog output (or a logger interface seam) and asserting all three values appear in the emitted line.
- [ ] `CHANGELOG.md` has an `## Unreleased` entry naming both the new `targetVault` field and the new `MY_VAULT` env var. Evidence: `grep -n 'targetVault\|MY_VAULT' CHANGELOG.md` returns at least two matching lines under `## Unreleased`.
- [ ] `README.md` (or `docs/controller-design.md`, whichever currently documents controller env vars) lists `MY_VAULT` as required. Evidence: `grep -n 'MY_VAULT' README.md docs/controller-design.md` returns at least one match.

Scenario coverage: NO new scenario. Unit + executor-level tests with fake `GitClient` and fake `CommandObjectSender` reach every routing decision and validation rule. The behavior is observable end-to-end via existing controller boot semantics; a scenario would add Docker / Kafka cost without exercising a code path the unit tests cannot.

## Verification

```
cd lib && make precommit
cd task/controller && make precommit
```

Both must exit 0. Manual smoke test (informational, not gating):

```
MY_VAULT=personal go run ./task/controller   # should refuse to consume openclaw-targeted commands
MY_VAULT=openclaw  go run ./task/controller   # should refuse to consume personal-targeted commands
```

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Add `TargetVault` field + slug validation + JSON round-trip tests in `lib/command/task` | 1, 2 | field shape, round-trip, validation matrix | — |
| 2 | Extend `NewCreateCommandSender` with `defaultVault` arg; update every in-repo call site to pass `""`; update sender tests | 3, 4, 8 | sender behavior matrix (4 cases), call-site compile | prompt 1 |
| 3 | Add `MY_VAULT` config to controller; introduce `ShouldProcess` predicate; wire predicate into `CreateTaskExecutor`; add skip-log line; update CHANGELOG and controller docs | 5, 6, 7 | controller startup validation, routing matrix, skip-log evidence, doc evidence | prompts 1, 2 |

Rationale: the field and its validation are the smallest self-contained unit and unblock the other two; the sender change is a strictly mechanical signature update that depends only on the field existing; the controller change is the largest surface (env, predicate, executor wiring, log, docs) and depends on both prior pieces being in place so its tests can construct realistic commands.

## Do-Nothing Option

If we don't do this, we cannot run more than one task controller without duplicating every created task across all vaults. The recurring-task-creator rollout for a second vault (`personal`) is blocked. Workarounds — running only one controller, or hand-partitioning topics per vault — either defeat the goal of independent vaults or fragment the schema-design contract documented in `docs/kafka-schema-design.md`. The do-nothing path is not viable.

## Follow-ups (out of this spec)

- Update `bborbe/maintainer` call sites of `NewCreateCommandSender` to the two-argument form (separate repo, separate PR; no behavior change — passes `""`).
- Update `recurring-task-creator` to pass a non-empty `defaultVault` matching its target deployment.
- Update k8s manifests / Helm values to set `MY_VAULT` on each controller deployment.
- Consider promoting the `openclaw` legacy fallback to a deprecation log once all producers explicitly set `targetVault`.
