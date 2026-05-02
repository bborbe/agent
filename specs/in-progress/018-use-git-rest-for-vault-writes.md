---
status: verifying
approved: "2026-05-02T19:04:42Z"
generating: "2026-05-02T19:04:42Z"
prompted: "2026-05-02T19:27:11Z"
verifying: "2026-05-02T20:23:34Z"
branch: dark-factory/use-git-rest-for-vault-writes
---

## Summary

- `task/controller` calls `git-rest` over HTTP for every vault file operation (read / write / delete / list) instead of embedding its own git client.
- Removes `pkg/gitclient/` plus all stash / pull / rebase / conflict-resolution code from the controller.
- Controller stops maintaining a local clone of the vault. Its `datadir` PVC (mounted at `/data`) stays — it still backs BoltDB at `/data/bolt`. The `/data/vault` directory is no longer read or written; existing dirs become inert and can be cleaned up later.
- Target git-rest instance is already deployed: `vault-obsidian-openclaw` (StatefulSet in `dev` 2026-05-02; running git-rest v0.16.0 against `git@github.com:bborbe/obsidian-openclaw.git` — the same remote the controller currently clones independently). prod deploy of `vault-obsidian-openclaw` is a prerequisite for prod migration.
- Migration goes through a feature flag (`USE_GIT_REST`) with a dual-path stage so the cutover is reversible per environment.

## Prerequisites

- `vault-obsidian-openclaw` git-rest StatefulSet must be running in the target namespace before the controller's `USE_GIT_REST=true` flip in that environment. Already deployed in `dev` as of 2026-05-02; prod deploy of the same manifest set is required before prod cutover. Tracked outside this spec.

## Problem

The controller currently reimplements the same git plumbing that git-rest already implements. They have drifted, and the drift causes outages.

Concrete failure observed 2026-05-02 in dev: an `UpdateFrontmatterCommand` from the watcher landed on the controller, the controller modified `tasks/<id>.md` in its working tree, then `git pull` failed with `cannot pull with rebase: You have unstaged changes` and stayed stuck for hours. New Kafka commands could not be materialised. Separately, the force-push handler appears to be wiping the `## Review` section instead of preserving prior review content alongside an `Outdated by force-push <sha>` marker — a second write-path bug in the same package.

git-rest has already solved the equivalent problems: auto-commit on every write, readiness probe reflects pending-push state, periodic pull, no manual stash/conflict logic exposed to callers. There is no value in fixing the controller's bugs one by one when the same logic exists, tested and deployed, in a sibling service.

## Goal

After this work:

- The controller process holds no git working tree on local disk for vault content.
- Every vault read/write the controller performs flows through git-rest's HTTP API.
- Restarting the controller does not require any git state recovery — it is stateless for vault content.
- Adding a new vault writer to the cluster (a future watcher, a manual operator tool) means writing an HTTP client, not embedding git plumbing.

## Non-goals

- Replacing the Kafka-command pattern with synchronous HTTP from watcher to controller. Separate, larger decision.
- Multi-vault support in git-rest. Today one git-rest = one repo.
- Sharing git plumbing as a library between git-rest and other services. Superseded by this approach.
- Fixing the `## Review`-on-force-push wipe in `pkg/gitclient/` before migrating. We migrate first; if the symptom persists post-migration, it is a controller-logic bug separate from the git layer and gets its own spec.
- Changing the Kafka command schema or adding new command kinds.

## Desired Behavior

1. Controller starts up with no PVC mount, no SSH key on disk, no `git clone` in its container.
2. Controller reads `GIT_REST_URL` from env (default `http://vault-obsidian-openclaw:9090` — same value in dev and prod since controller and git-rest live in the same namespace per environment) and probes `/readiness` at startup.
3. On every Kafka command:
   - Controller `GET /api/v1/files/tasks/<id>.md` to fetch current state.
   - Controller mutates in memory.
   - Controller `POST /api/v1/files/tasks/<id>.md` with new bytes; git-rest commits and pushes.
4. If git-rest's `/readiness` returns 503 (push stuck, conflict, etc.), controller pauses Kafka consumption — Kafka offsets stay put — and resumes when readiness flips back to 200.
5. After successful migration, `tasks/<id>.md` files in the vault repo are byte-identical (modulo commit message) to what the pre-migration controller produced for the same Kafka command sequence. Frontmatter transitions, `## Review` preservation across force-push, and trigger-count semantics are preserved.
6. The git history shows one commit per controller-side write, authored by git-rest's git identity.
7. Controller exposes a metric `controller_gitrest_calls_total{op,status}` and an alert fires when `controller_kafka_consume_paused_total` is > 0 for > 5 min.

## Constraints

These contracts must be preserved by the migration. Pre-migration behaviour is the source of truth.

- **Task file frontmatter schema** — fields, types, transitions documented in `docs/task-flow-and-failure-semantics.md` (`## Status Taxonomy`) must not change.
- **`## Review` section preservation** — when the watcher publishes a force-push reset, the controller must keep prior `## Review` content (typically by appending an `## Outdated by force-push <sha>` marker) rather than discarding it. This is mandatory for re-review-on-synchronize to work.
- **Per-task ordering** — Kafka partitioning by `task_id` (verified in `docs/kafka-schema-design.md`) gives the controller serial delivery per task. Read-modify-write via git-rest is correct only as long as this holds.
- **Kafka offset semantics** — on git-rest unavailability or write failure, the offset must NOT advance. A retried message must produce the same end state as a single successful delivery.
- **Atomic frontmatter operations** — `IncrementFrontmatterExecutor` and `UpdateFrontmatterExecutor` (documented in `docs/controller-design.md` `## Atomic Frontmatter Commands`) preserve other fields; behaviour must survive the migration.
- **No conflict with spec 017** — `specs/in-progress/017-create-task-command.md` is in flight; coordinate sequencing.
- **BoltDB Kafka offset cache** — stays at `/data/bolt` on the existing `datadir` PVC. The PVC itself is preserved; only the `/data/vault` directory content stops being used.
- **Same upstream remote** — git-rest's `GIT_REMOTE_URL=git@github.com:bborbe/obsidian-openclaw.git` matches the controller's current `GIT_URL`. Both clone the same repo today; post-migration there is one clone (git-rest's).

## Failure Modes

| Trigger | Expected behaviour | Recovery |
|---|---|---|
| `git-rest` pod down | Controller pauses Kafka consume; offsets stay put. Pod readiness flips false. | git-rest restarts → controller readiness recovers → consume resumes from last committed offset. |
| `git-rest` returns 5xx on `POST` | Controller retries with exponential backoff (max 5 attempts); on final failure pauses Kafka consume and alerts. | Operator inspects git-rest logs; once unblocked, controller resumes. |
| Remote `git push` conflict (concurrent writer) | git-rest's pull-before-commit resolves trivially; on real conflict git-rest returns 5xx. Controller treats as above. | Operator resolves in the remote repo; git-rest pull picks it up; controller retry succeeds. |
| Controller restart mid-command | Kafka offset not yet advanced → message redelivered → idempotent write produces same end state. | Automatic. |
| `git-rest` `POST` succeeds, controller crashes before committing Kafka offset | Message redelivered → second `POST` writes identical bytes. Whether git-rest creates a no-op commit or skips, the on-repo end state is identical. Empty commits are accepted as noise; no controller-side short-circuit needed. | Automatic. |
| HTTP body size > 10 MiB | git-rest returns 413; controller alerts. | Indicates a bug or pathological input — does not happen in practice (task files < 10 KB). |

## Acceptance Criteria

- [ ] `pkg/gitrestclient/` exists in `task/controller/` with `Get`, `Post`, `Delete`, `List` methods, mocked via Counterfeiter, unit-tested.
- [ ] Controller has a `GIT_REST_URL` flag/env; absence is a startup error.
- [ ] Feature flag `USE_GIT_REST` toggles handlers between `pkg/gitclient` and `pkg/gitrestclient` paths. Default ships as `false`; flipped to `true` per environment after dev burn-in.
- [ ] Controller readiness probe reports unready when `git-rest` `/readiness` is 503; Kafka consume pauses correspondingly.
- [ ] A scenario `scenarios/use-git-rest-for-vault-writes.md` exercises the full sequence: `CreateTaskCommand` → `UpdateFrontmatterCommand` → `WriteResultCommand` → `force-push reset` → `WriteResultCommand`. End state of `tasks/<id>.md` is byte-equivalent (modulo commit metadata) to pre-migration baseline.
- [ ] **`## Review` survives force-push** — explicit assertion in the scenario: after the reset and second result, the file contains `## Outdated by force-push <sha>` AND the prior `## Review` content (renamed/marked-outdated, not deleted).
- [ ] Controller manifest in dev: SSH key secret mount removed; `datadir` PVC stays (BoltDB at `/data/bolt`); StatefulSet kind preserved. Controller code no longer reads or writes anything under `/data/vault`.
- [ ] Metrics: `controller_gitrest_calls_total{op,status}`, `controller_kafka_consume_paused_total`, exposed and labeled correctly.
- [ ] `docs/controller-design.md` updated: `## Git Operation Serialization`, `## Push Retry with Rebase`, `## LLM Conflict Resolution` sections rewritten or removed; new `## Vault Writes via git-rest` section added.
- [ ] `pkg/gitclient/` deleted from `task/controller/`; no remaining imports.
- [ ] `make precommit` passes in `task/controller/`.

## Verification

- `cd task/controller && make precommit` — passes.
- Scenario `scenarios/use-git-rest-for-vault-writes.md` — passes the dark-factory scenario runner.
- Dev burn-in: feature flag flipped to `true` in `task/controller/dev.env`. Real PR opened on `bborbe/code-reviewer`. Verified end-to-end: vault file created, frontmatter transitions correct, `## Review` populated, force-push (re-push of head SHA) preserves prior review and adds the `Outdated by` marker.
- Pre-flip baseline: `git log --oneline -- tasks/` on the agent vault repo dumped to a file. Post-flip, same command run again — diff shows only expected new commits. No tampering with historical commits.
- Controller restart test: kill pod mid-burn-in run, confirm Kafka offset replay produces identical task file bytes.

## Do-Nothing Option

Cost of leaving the controller's git plumbing in place:

- Continued silent corruption — today's PR #2 task already shows missing `## Review` and a stuck `git pull`. Each new symptom costs a manual `kubectl exec + git stash/checkout` intervention.
- Schema drift between `lib/v0.5x` versions across watcher and controller has the same failure shape (silent skip on git ops). Centralising on git-rest reduces the controller's lib surface.
- Future watchers (Bitbucket, Jira) cannot be added without each one growing its own git client OR relying on the controller's broken one.

The do-nothing option is to keep patching `pkg/gitclient/` for each symptom. We have already paid that cost on 2026-04-30 (push retry with rebase), 2026-05-02 (this incident), and there is no end visible.

## Security / Abuse Cases

- **Auth model** — git-rest holds the SSH key; controller authenticates to git-rest over HTTP. Today the controller holds the SSH key directly. Net change: secret-bearing surface goes from one (controller pod) to one (git-rest pod). No expansion.
- **In-cluster network access** — git-rest's HTTP API is reachable by anything in the namespace by default. NetworkPolicy must restrict ingress to `task/controller` pods only. AC: a NetworkPolicy is shipped alongside the manifest changes.
- **Caller forensics** — git-rest's commit message is `git-rest: create|update|delete <path>`, no caller identity. Acceptable for now (only one caller); revisit if a second writer is added. Out of scope.
- **No new external attack surface** — git-rest stays cluster-internal, no Ingress, no NodePort.
- **Compromised controller pod** — a hostile controller could write arbitrary bytes to any path under the vault repo via git-rest. Same blast radius as today (it already has SSH push access). No regression.

## Resolved questions (previously open)

- **Glob recursion** — Audited controller code: list ops use only single-level globs (matched against `tasks/*.md`). git-rest's `filepath.Match` is sufficient. No API change to git-rest needed.
- **Per-task serialisation via Kafka partitioning** — `docs/kafka-schema-design.md` documents partitioning by `task_id`. Read-modify-write under git-rest is correct.
- **One commit per write** — Confirmed: each Kafka command produces ≤ 1 file write today. No batching to preserve.
- **git-rest conflict behaviour** — git-rest pulls before commit; on conflict returns 5xx to the caller. Controller treats this as transient (retry + backoff + pause-on-final-fail). Documented in `## Failure Modes`.

## Out of scope

- See `## Non-goals` above.
- Renaming or restructuring git-rest's API. The current API surface (`GET`/`POST`/`DELETE`/`?glob=` on `/api/v1/files/{path}`) covers all needs.
- Reducing git-rest's pull interval below 30 s.

## Decomposition (suggested for the daemon)

1. **`gitrestclient` package** — HTTP client + Counterfeiter mocks + unit tests in `task/controller/pkg/gitrestclient/`.
2. **Handler dual-path + feature flag** — wire `USE_GIT_REST` flag; existing handlers branch on it; both paths covered by existing handler tests.
3. **Manifest changes** — drop the SSH key secret mount from the controller StatefulSet (kind unchanged; `datadir` PVC stays for BoltDB), add `GIT_REST_URL` env, NetworkPolicy restricting `vault-obsidian-openclaw:9090` ingress to `agent-task-controller`.
4. **Metrics + readiness coupling** — `controller_gitrest_calls_total`, `controller_kafka_consume_paused_total`; readiness reflects git-rest health.
5. **Scenario** — `scenarios/use-git-rest-for-vault-writes.md` covering the AC sequence.
6. **`pkg/gitclient` removal + doc update** — final cutover prompt, runs after dev burn-in succeeds.
(Prod prereq — deploying `vault-obsidian-openclaw` to the prod namespace — is tracked outside this repo; see `## Prerequisites`.)

## Related

- `git-rest` HTTP API — endpoints used: `GET`, `POST`, `DELETE` on `/api/v1/files/{path}`; `?glob=` on `GET` for list. Auto-commit + push on `POST`/`DELETE`. `/healthz`, `/readiness`, `/metrics`. Body limit 10 MiB. Single-level glob (`filepath.Match`). Repo: `github.com/bborbe/git-rest`.
- `task/controller/pkg/gitclient/` — code to be removed.
- `docs/controller-design.md` — must be updated post-migration.
- `docs/task-flow-and-failure-semantics.md` — frozen-contract source for status taxonomy and result routing.
- `docs/kafka-schema-design.md` — partitioning guarantee referenced in Constraints.
- `specs/in-progress/017-create-task-command.md` — coordinate sequencing.
