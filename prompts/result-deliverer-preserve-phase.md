---
status: draft
created: "2026-10-28T23:20:00Z"
---

# Do not publish a phase a result did not advance

<summary>
- A finished agent run can no longer overwrite the phase a task is currently in
- A late result therefore stops undoing a phase change that happened while the run was in flight
- Results that DO advance the phase behave exactly as before
- Both delivery paths are covered: the one production uses and the local command-line one
- The failure path deliberately keeps publishing its phase, because a downstream gate reads it
- Every test that does not pin the old behaviour still passes unchanged
</summary>

<objective>
Stop both result deliverers from republishing a phase a run did not advance, so that a delayed
result cannot silently revert a phase another component changed in the meantime. A task that
was parked for a human must stay parked.
</objective>

<context>
Read `CLAUDE.md` for project conventions and `docs/dod.md` for the repo's definition of done
(it requires a `## Unreleased` CHANGELOG entry). Read `docs/task-flow-and-failure-semantics.md`
for the phase contract — line 112: "(`done` with empty `NextPhase` is an in-place save: `status:
in_progress`, phase unchanged — used between steps of a multi-phase phase.)"

Read `delivery/result-deliverer.go`. It holds two deliverers, and `agent_result-deliverer.go`
(repo root, not the `delivery/` package) documents the split at line 14: `FileResultDeliverer`
is the local command-line path and `KafkaResultDeliverer` is the production Kubernetes path.
Both need the same guarantee, and they need it expressed differently because one writes a whole
file and the other publishes a frontmatter update that a downstream service merges.

The same defect class is already described in
`specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` at line 166 — "the agent's
own result publish … revives `phase: ai_review` from the agent's stale in-memory snapshot".

## Why this change is needed

A deliverer is constructed per Job with that Job's `originalContent`, and
`Generate(ctx, d.originalContent, result)` rebuilds the document from it — and the passthrough
generator rebuilds from `result.Output`, the agent's in-memory snapshot, which the in-process
phase walk never advances. Either way the generated frontmatter carries the Job-start phase.

`kafkaResultDeliverer.applyResultFrontmatter` already says, in the `AgentStatusDone` branch with
an empty `NextPhase`: "phase not modified here — preserves the incoming phase". The intent is
right and the mechanism is wrong: "not modified" keeps the phase the generator produced, which
is the Job-start phase, and publishes it. The controller then writes that stale phase over the
live one.

Measured in production on 2026-10-28: a job started at 19:44 with `phase: planning`. An executor
correctly wrote `phase: human_review` at 20:08 and again at 20:22. At 20:51:19 a late result
publish wrote `phase: planning` back, in the same commit that set
`job_started_at: 2026-10-28T19:44:00Z`. The task became eligible again and re-ran on a second
cluster.

## Why deleting the key is a preserve, not a delete

The published update is merged downstream, not written wholesale. `agent-task-controller`'s
`resultWriter.WriteResult` calls `MergeFrontmatter(currentOnDisk, req.Frontmatter)`
(`pkg/result/result_writer.go`), which copies the **existing on-disk values first** and then
overlays the incoming ones. A key absent from the payload therefore keeps its on-disk value.
`lib.Task.Validate` requires only `TaskIdentifier` and `Content`, so an absent `phase` is not
rejected. This is the mechanism the whole change rests on — if you find evidence contradicting
it, stop and say so rather than shipping.
</context>

<requirements>
1. **Kafka deliverer — stop publishing a phase the result did not advance.** In
   `kafkaResultDeliverer.applyResultFrontmatter`, the `agentlib.AgentStatusDone` branch with an
   empty `NextPhase` currently leaves `frontmatter["phase"]` as generated. Change it to remove
   the key instead:

   ```go
   delete(frontmatter, "phase")
   ```

   Replace the existing comment ("phase not modified here — preserves the incoming phase…")
   with one that states the real guarantee and names the mechanism: an in-place save publishes
   no phase at all, and the controller merges on-disk-first, so the task keeps whatever phase it
   currently holds — including one written by a park while the run was still going. A completed
   run does not own the phase.

2. Apply the same rule to the other non-advancing branches of `applyResultFrontmatter` —
   `in_progress`, `needs_input` and the `default` (unknown-status) branch. None of them advances
   the phase, so none of them may publish one. Read the function before editing.

   **Do NOT change the `failed` branch in this prompt.** The controller's planning-retry gate
   reads `phase` from the published payload, not from disk (`agent-task-controller`,
   `pkg/command/planning_retry.go`, `matchesRetryGate`), so removing the key there would
   silently disable planning retries for pr-review tasks. That branch is a follow-up change in
   `agent-task-controller` (move the gate's phase check onto the `existingFrontmatter` it already
   fetches), after which the `failed` branch can drop the key too. Leave it publishing its phase.

   State in your completion report which branches you changed and why each qualifies.

3. Do not change the advancing path: when `result.Status` is done and `result.NextPhase` is
   non-empty, `applyResultFrontmatter` resolves and writes the phase. Leave that exactly as it
   is.

   **Do remove the re-add tail of `kafkaResultDeliverer.carryAdvancedPhase`** — the
   `if d.advancedPhase != "" { frontmatter["phase"] = d.advancedPhase }` at the end of the
   method. Once a phase-preserving result publishes no `phase`, the controller's merge already
   keeps the on-disk phase, so re-adding `advancedPhase` can only restore a snapshot that has
   gone stale: a task parked at `human_review` while this Job was running would be dragged back
   to the Job's earlier advance. With the tail gone, `advancedPhase`, the code that records it
   and the `sync.Mutex` that protects it have no remaining purpose on `kafkaResultDeliverer` —
   remove them too, together with the now-pointless `d.mu.Lock()` / `defer d.mu.Unlock()` pair
   and the "Caller must hold d.mu" comment in its `DeliverResult`. With the tail and the
   recording gone, `kafkaResultDeliverer.carryAdvancedPhase` has an empty body: delete the
   method and its call in `DeliverResult`. Leave `fileResultDeliverer` alone — it keeps its own
   `advancedPhase`, `mu` and `carryAdvancedPhase`, which requirement 4 builds on. If you find
   that something else genuinely needs any of this, keep the minimum and say why in your report.

4. **File deliverer — preserve the phase currently on disk.** In `fileResultDeliverer`, add a
   method with the signature:

   ```go
   func (d *fileResultDeliverer) preserveDiskPhase(
       generated string,
       original string,
       result agentlib.AgentResultInfo,
   ) string
   ```

   Call it from `DeliverResult` between the existing `carryAdvancedPhase` call and
   `os.WriteFile`. `original` is already read at the top of `DeliverResult` as `[]byte` — pass
   `string(original)` and do not read the file again:

   ```go
   generated = d.preserveDiskPhase(generated, string(original), result)
   ```

   Behaviour, in order:

   a. If `result.Status == agentlib.AgentStatusDone && result.NextPhase != ""`, return
      `generated` unchanged — the same guard `carryAdvancedPhase` uses. `NextPhase` on any
      other status advances nothing.

   b. Otherwise parse `original` with `ParseMarkdownFrontmatter`. Take its `phase` value as a
      string. If it is empty, return `generated` unchanged.

   c. Otherwise return `SetFrontmatterField(generated, "phase", phase)` using the value from
      `original`. A non-empty value is the phase on disk and is preserved whatever it is,
      including `"done"` — the `!= "done"` test in `carryAdvancedPhase` guards a different
      decision (never recording `done` as an advanced phase) and does not apply here.

   This path writes a whole file, so it cannot omit the key; it must carry the current value
   forward instead. Never return an error and never fail the delivery — fall back to the
   generated content.

5. Write comments in the same voice as the surrounding code: explain WHY, not what.

6. Update the existing specs in `delivery/result-deliverer_test.go` and add the missing ones,
   following the surrounding Ginkgo/Gomega style.

   Existing assertions that pin the old behaviour must be **rewritten**, not preserved. These
   assert a `phase` value on branches that will no longer publish one (there is no prior
   in-process advance in these specs, so nothing re-adds it): ~421, 446, 798, 819
   (needs_input), ~474, 498 (done + empty NextPhase), ~729, 753 (in_progress), ~777
   (needs_input — listed out of order here, still in this list), ~1327 (5e), ~1348 (5f), ~1389
   (5g — its second publish is an in_progress save). Rewrite each to assert the branch's real
   contract — that the key is absent, e.g. `Expect(fm).NotTo(HaveKey("phase"))`, alongside the
   status/assignee assertions they already make — and keep their scenario text: the phase is
   still preserved, now by the controller's merge rather than by the payload. In 5g, the FIRST
   assertion (~1388, a done result with `NextPhase: execution`) still expects
   `phase: execution` — leave it as it is.

   Do NOT touch the `failed`-branch assertions — ~267, 292, 345, 380, 840, 862. Requirement 2
   keeps that branch publishing its phase, so they stay green unchanged.

   The specs that pin `carryAdvancedPhase`'s re-add tail (~1257, 1274, 1293, 1314, 1348) must be
   updated for requirement 3. In 5a/5b/5c/5e/5f (1257, 1274, 1293, 1327, 1348) the published
   frontmatter must now have **no** `phase` key. 5d (~1314) is the exception: its second result
   is a `failed` one, which requirement 2 keeps publishing, so it must assert
   `phase: planning` — the generated phase — and not key absence.

   Add, at minimum:

   a. `KafkaResultDeliverer` — a done result with an empty `NextPhase`, whose `originalContent`
      carries `phase: planning`, publishes a frontmatter with **no** `phase` key.
   b. Keep the existing spec at ~503 ("a done result with `NextPhase: execution` publishes
      `phase: execution`") as the control. Do not add a second copy; it is the proof the suite
      cannot pass by never publishing a phase.
   c. `FileResultDeliverer` — a task file whose frontmatter carries `phase: human_review`, a
      done result with an empty `NextPhase` whose generated content would carry
      `phase: planning`, and the file still reads `phase: human_review` afterwards.
   d. `FileResultDeliverer` — a task file carrying `phase: done`, same delivery, and the file
      still reads `phase: done`.
   e. `FileResultDeliverer` — a done result with `NextPhase: execution` still writes
      `phase: execution`.

7. `CHANGELOG.md` has no `## Unreleased` section today — its top heading is `## v0.132.4` at
   line 11. Insert `## Unreleased` directly above it (blank line before and after, matching the
   file's heading spacing), then add one bullet under it describing the fix from the user's
   point of view, in the style of the surrounding bullets. `docs/dod.md` line 27 requires this
   entry.

8. Before finishing, re-run `<verification>` and confirm it passes; then walk each requirement
   above against the change and confirm it is satisfied. In particular confirm you did not
   change the `failed` branch, and that no remaining test asserts a phase on a branch that no
   longer publishes one.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Every existing test that does not pin the old phase behaviour must pass unchanged. The tests
  that DO pin it must be updated in this prompt, not preserved — requirement 6 lists them.
  `make precommit` runs `test`, so the suite must be green at the end.
- Do not change the generator implementations in `delivery/content-generator.go`, the
  `DeliverResult` signatures, or any error path.
- Do not change the `failed` branch of `applyResultFrontmatter` — see requirement 2.
- Do not change how `status` is written. It is derived from `result.Status` on every branch and
  does not share this defect.
- Do not add a new dependency; `ParseMarkdownFrontmatter`, `SetFrontmatterField` and the
  deliverer types already exist in this package.
- Keep the change to `delivery/result-deliverer.go`, `delivery/result-deliverer_test.go` and
  `CHANGELOG.md`. Nothing else.
</constraints>

<verification>
make precommit
</verification>
