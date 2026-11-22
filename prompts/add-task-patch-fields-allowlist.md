---
status: draft
created: "2026-11-22T18:10:00Z"
---

# Add the task PATCH field rule to the shared task schema

<summary>
- One canonical rule for "may a task PATCH name this frontmatter key?" now lives in this library, beside the task payload both consumers already import.
- The rule is a predicate, not only a list: it carries the enumerated keys and the one open namespace, so a caller cannot implement half of it and drift.
- The typed subset and the agent-pipeline subset stay distinguishable, because one consumer attributes every pipeline key to the producer that writes it.
- The enumerated data is served as read-only copies, so a caller cannot mutate the canonical set.
- A contract spec ties the rule to what this library's own result writer actually publishes, so a new producer key cannot silently fall outside it.
</summary>

<objective>
Make this library hold the one rule for which task-frontmatter keys a task PATCH may name, exported so the API that enforces it and the client that writes through it call the same predicate instead of each keeping its own list. Adoption by those two consumers is a separate change in their own repositories. Today the API refuses the entire request on the first key it does not recognise and the client's write is dropped whole — so a single unknown key destroys a completed run's result, and a client-side list that is one key behind is just as destructive in the other direction.
</objective>

<context>
Read `README.md` — its "What's in here" and "History" sections describe the post-split layout: root package `lib`, single root module, `make test` and `make precommit` from the repository root. ⚠️ `CLAUDE.md`'s Architecture and build sections still describe the pre-split monorepo (`lib/`, `task/controller`) and say "run in service dir, never at root" — they are stale; prefer the README.

Read `docs/dod.md` — the project Definition of Done, including the `## Unreleased` CHANGELOG entry.

Read `agent_task-frontmatter.go` — `TaskFrontmatter` is a `map[string]interface{}` carried on `Task` and on the wire. This file is the closest existing example of the package's file naming, license header and doc-comment style; follow both. `MaxRetries()` (~line 83) reads the `max_retries` key that requirement 2 adds.

Read `agent_task.go` — `Task` is the payload the result writer merges and the vault API stores. The new rule describes the frontmatter of exactly this type, which is why it belongs here rather than in either consumer.

Read `delivery/result-deliverer.go` — `applyResultMetrics` (~line 411) and `applyTokenMetrics` (~line 441) write the six flat `metrics_*` keys declared at lines 31-39 into the frontmatter that becomes the PATCH payload. They are why the rule is a predicate: the vault API admits that whole namespace by prefix, so a closed list on this side would drop the next counter.

Read `docs/interaction-count.md` — documents `metrics_agent_turns` and `metrics_interaction_count`, the two keys that motivated the namespace.

Read `delivery/result-deliverer_test.go` — each `Context` block declares its own local `publishedFrontmatter` closure (~lines 900 and 1102); it is not a package-level helper. The real call path is `delivery.NewKafkaResultDelivererWithSender(...)` then `DeliverResult` — `kafkaResultDeliverer` is unexported and cannot be named from the external test package. Requirement 6 extends this file rather than adding a new one.

Read `agent_task-frontmatter_test.go` — the package's test style: Ginkgo v2 / Gomega, external test package `lib_test`, one `Describe` per behaviour. Follow the coding plugin's `go-testing-guide.md` and `go-doc-best-practices.md` where they resolve in the container; otherwise follow the existing test files and doc comments.
</context>

<requirements>
1. Add `agent_task-patch-fields.go` in package `lib`, with the standard license header, holding two **unexported** enumerated sets (`taskTypedFields`, `taskPipelineFields`), their union (`taskPatchFields`, built once from the two at package scope — do not restate the keys a third time), the open-namespace constant, and the predicate.
2. The two source sets are exactly these, and no key may be added, dropped or renamed:

   ```go
   // taskTypedFields are the task-frontmatter keys the vault API validates and
   // reasons about — the ones whose values it checks against a transition graph,
   // a normalised vocabulary or a rule. It is not the whole of what a write may
   // name: the agent pipeline's own keys are admitted too, through
   // taskPipelineFields below, and the two are kept apart deliberately.
   var taskTypedFields = map[string]bool{
       "status": true, "phase": true, "assignee": true, "task_type": true,
       "stage": true, "cluster": true, "flag": true, "goals": true,
       "priority": true, "category": true, "role": true, "mode": true,
       "defer_date": true, "due_date": true, "target_date": true,
       "planned_date": true, "start_date": true, "blocked_by": true,
       "themes": true, "page_type": true,
   }

   // taskPipelineFields are the task-frontmatter keys the agent pipeline writes
   // through the vault API. A key here is one the API stores and carries without
   // interpreting it; a key in taskTypedFields is one it validates. Merging the
   // two would hide that difference from the next reader.
   //
   // max_retries is carried rather than written by this repository: a result
   // PATCH is a read-modify-write, so an operator-set max_retries rides along in
   // the payload, and the vault API refuses the whole request because the key is
   // neither enumerated there nor metrics_-prefixed. It is the one key in this
   // list the API did not already accept.
   var taskPipelineFields = map[string]bool{
       "trigger_scope": true, "trigger_count": true, "max_triggers": true,
       "current_job": true, "claude_session_id": true, "job_started_at": true,
       "spawn_notification": true, "queued_at": true, "aborted_reason": true,
       "gate_successor": true, "target_vault": true, "previous_assignee": true,
       "retry_count": true, "last_result_status": true, "completed_date": true,
       "processed_at": true, "superseded_by": true, "base_ref": true,
       "clone_url": true, "ref": true, "repo": true, "current_version": true,
       "override_reason": true, "title": true, "created_by": true,
       "auto_abort_prior": true, "date": true, "launcher": true,
       "branch": true, "pr_number": true, "current_go": true, "latest_go": true,
       "episode_sha": true, "vuln_count": true, "vulns": true,
       "short_id": true, "project": true, "issue_url": true,
       "new_version": true, "previous_version": true, "release_kind": true,
       "release_notes_url": true, "recovery_sha": true, "run_outcome": true,
       "failure_reason": true, "run_id": true,
       "max_retries": true,
   }
   ```

   The enumerated keys are the vault API's own PATCH allowlist (`github.com/bborbe/vault-api`, `pkg/handler/task_content.go`, `typedTaskFields` ∪ `agentPipelineFields` at `origin/master` on 2026-11-22, v0.31.0) plus `max_retries`. `metrics_runs` is deliberately **not** enumerated even though the vault API lists it: the prefix admits it, and enumerating it would contradict requirement 3 and the `metrics_*` verification check.
3. Add the open namespace and the predicate. The vault API admits every `metrics_*` key by prefix rather than enumerating them, because that key list belongs to the executor and the controller — a new counter there must not require an API release before a verdict can be published. Mirror that rule exactly, in one place:

   ```go
   // metricsFieldPrefix is the one open namespace: the run-record keys belong to
   // the executor and the controller, so a new counter must be admitted without a
   // change here. The enumerated sets stay closed; every other unknown key is
   // still refused.
   const metricsFieldPrefix = "metrics_"

   // TaskPatchFieldAdmitted reports whether a task PATCH may name this frontmatter
   // key. ...
   func TaskPatchFieldAdmitted(key string) bool {
       return taskPatchFields[key] || strings.HasPrefix(key, metricsFieldPrefix)
   }
   ```

   `strings` is the only import this file needs.
4. Export four accessors: `TaskTypedFields()`, `TaskPipelineFields()` and `TaskPatchFields()` each returning a **fresh copy** of the corresponding set, plus `TaskPatchFieldAdmitted(key string) bool`.

   The three set doc comments state: what the set is, that this package is its single definition, and the consequence of a key being absent — the vault API refuses a request naming a key the predicate rejects, and a client must drop such a key rather than send it, because one rejected key refuses the whole request and loses every field in it. The `TaskPatchFields` comment names its two intended consumers (the vault API's request validation and the task controller's result writer) and states that this package is the single rule they should call rather than copy. Do not claim they already call it — they do not yet. The predicate's comment says plainly that it is the rule to call instead of testing a set directly, and names the `metrics_` prefix as the one open namespace.
5. Add `agent_task-patch-fields_test.go` (external package `lib_test`, Ginkgo v2 / Gomega) asserting:
   - the union equals the two sets combined, and the two sets are disjoint — assert by membership over every key, not by comparing sizes alone;
   - every enumerated key is non-empty and matches `^[a-z][a-z0-9_]*$`;
   - the exact sizes are 20 typed, 47 pipeline and 67 patch, so an accidental deletion fails the build instead of silently shrinking the allowlist;
   - each set accessor returns an independent copy: mutate the returned map, call the accessor again, and assert the mutation is gone;
   - the predicate admits every enumerated key, admits a synthetic future key `metrics_not_yet_invented`, and rejects `run_claim` and `not_a_task_field`;
   - `run_claim` is **not** a member of `TaskPatchFields()`. Use the description `It("run_claim is a spawn-gate claim marker written at execution start, whose only expiry is its TTL, so a result write must never be able to set it")` so a failure names the reason.
6. Add a contract spec to `delivery/result-deliverer_test.go` (external package `delivery_test`; declare a `publishedFrontmatter` closure the same way the existing `Context` blocks do). Drive `delivery.NewKafkaResultDelivererWithSender(...)` through `DeliverResult` for a run whose original content carries `max_retries` and whose result carries agent turns, interaction count, all four token counts and a run-history entry, then assert `max_retries` is present in the published frontmatter (the fake content generator must echo the original frontmatter, or the key never reaches the published map) and that **every key in the published frontmatter satisfies `agentlib.TaskPatchFieldAdmitted`**. Name the spec so its failure reads "the result writer published a key the PATCH field rule does not admit". This is the only place in this repo where the producer and the rule meet.
7. Add a `## Unreleased` bullet to `CHANGELOG.md` in the project's existing style, naming the new exported surface and why the rule lives here. ⚠️ The file has no `## Unreleased` section today — its head is `## v0.147.3`. Create the section directly above the newest `## vX.Y.Z` header; do not rename or edit a released section.
8. Add one line to `README.md`'s "What's in here" section naming the new exported surface, since the package's exported API changed.
</requirements>

<constraints>
- Do NOT commit, push, tag or release. Leave the working tree for the caller to review.
- Do NOT change or remove any existing exported symbol. This change is purely additive.
- Do NOT enumerate any `metrics_*` key. They are admitted by the prefix; listing them re-closes the namespace the API deliberately opened, and a new counter would then be dropped on this side.
- Do NOT restate the key list a third time for the union — build it from the two source sets.
- Do NOT add a per-key history comment for every entry. The group comments in requirement 2 are the documentation; per-key provenance lives in the vault API repository's own source history.
- The only import in the new file is `strings`.
- Tests use Ginkgo v2 / Gomega in external `*_test` packages, matching the existing test files.
- Do NOT widen the enumerated sets speculatively. A key belongs there only if the vault API enumerates it or, like `max_retries`, a producer carries it and the API would refuse it. Requirement 6 is the guard that keeps that true.
</constraints>

<verification>
- `ROOTDIR=/workspace make precommit` passes.
- `grep -c 'func TaskPatchFieldAdmitted' agent_task-patch-fields.go` prints `1`.
- `grep -c 'func TaskPatchFields' agent_task-patch-fields.go` prints `1`.
- `grep -c 'const metricsFieldPrefix' agent_task-patch-fields.go` prints `1`.
- `test -f agent_task-patch-fields.go && awk '/^var TaskPatchFields/{n++} END{print n+0}' agent_task-patch-fields.go` prints `0` — the file exists and the canonical data is unexported.
- `test -f agent_task-patch-fields.go && awk '/"metrics_[a-z_]+": true/{n++} END{print n+0}' agent_task-patch-fields.go` prints `0` — the file exists and no `metrics_*` key is enumerated.
- `grep -c 'run_claim' agent_task-patch-fields_test.go` prints a count of at least `1`.
- `grep -c 'TaskPatchFieldAdmitted' delivery/result-deliverer_test.go` prints a count of at least `1` — the producer-side contract spec exists.
- `awk '/^## /{print; exit}' CHANGELOG.md` prints exactly `## Unreleased`.
- `awk '/^## /{sec=$0} /TaskPatchFieldAdmitted/{print "sits under: " sec}' CHANGELOG.md` prints exactly `sits under: ## Unreleased`.
</verification>
