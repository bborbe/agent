---
status: draft
created: "2026-10-28T23:05:00Z"
---

# Do not publish a phase a result did not advance

<summary>
- A finished agent run can no longer overwrite the phase a task is currently in
- A late result therefore stops undoing a phase change that happened while the run was in flight
- Results that DO advance the phase behave exactly as before
- Both delivery paths are covered: the one production uses and the local command-line one
- A delivery that cannot determine the current phase still succeeds, unchanged
- Existing tests continue to pass
</summary>

<objective>
Stop both result deliverers from republishing the phase a run started at, so that a delayed
result cannot silently revert a phase another component changed in the meantime. A task that
was parked for a human must stay parked.
</objective>

<context>
Read `CLAUDE.md` for project conventions and `docs/dod.md` for the repo's definition of done
(it requires a `## Unreleased` CHANGELOG entry). Read `docs/task-flow-and-failure-semantics.md`
for the phase contract — "`done` with empty `NextPhase` is an in-place save: `status:
in_progress`, phase unchanged".

Read `delivery/result-deliverer.go`. It holds two deliverers, and
`agent_result-deliverer.go` documents the split: `FileResultDeliverer` is the local
command-line path and `KafkaResultDeliverer` is the production Kubernetes path. Both need the
same guarantee, and they need it expressed differently because one writes a whole file and the
other publishes a frontmatter update.

The same defect class is already described in
`specs/completed/015-atomic-frontmatter-increment-and-trigger-cap.md` — "the agent's own
result publish … revives `phase: ai_review` from the agent's stale in-memory snapshot".

## Why this change is needed

A deliverer is constructed per Job with that Job's `originalContent`, and
`Generate(ctx, d.originalContent, result)` rebuilds the document from it. The phase carried in
that document is the phase the run STARTED at.

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
   with one that states the real guarantee: an in-place save publishes no phase at all, so the
   controller leaves whatever phase the task currently holds, including one written by a park
   while the run was still going. A completed run does not own the phase.

2. Apply the same rule to the other non-advancing branches of `applyResultFrontmatter` —
   `in_progress`, `needs_input` and `failed`. None of them advances the phase, so none of them
   may publish one. Read the function before editing; state in your own words, in the PR-less
   commit message, which branches you changed and why each qualifies.

3. Do not change the advancing path. When `result.Status` is done and `result.NextPhase` is
   non-empty, `applyResultFrontmatter` resolves the phase and `carryAdvancedPhase` publishes
   it. Leave that behaviour exactly as it is.

4. **File deliverer — preserve the phase currently on disk.** In `fileResultDeliverer`, add a
   method with the signature:

   ```go
   func (d *fileResultDeliverer) preserveDiskPhase(
       generated string,
       original string,
       result agentlib.AgentResultInfo,
   ) string
   ```

   Call it from `DeliverResult` immediately after the existing `carryAdvancedPhase` call and
   before `os.WriteFile`, reassigning `generated`. `original` is already read at the top of
   `DeliverResult` — do not read the file again.

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

6. Add specs to `delivery/result-deliverer_test.go`, in the existing describe blocks, following
   the surrounding style. At minimum:

   a. `KafkaResultDeliverer` — a done result with an empty `NextPhase`, whose `originalContent`
      carries `phase: planning`, publishes a frontmatter with **no** `phase` key.
   b. `KafkaResultDeliverer` — a done result with `NextPhase: execution` still publishes
      `phase: execution`. This is the control: it proves the suite cannot pass by never
      publishing a phase.
   c. `FileResultDeliverer` — a task file whose frontmatter carries `phase: human_review`, a
      done result with an empty `NextPhase` whose generated content would carry
      `phase: planning`, and the file still reads `phase: human_review` afterwards.
   d. `FileResultDeliverer` — a task file carrying `phase: done`, same delivery, and the file
      still reads `phase: done`.
   e. `FileResultDeliverer` — a done result with `NextPhase: execution` still writes
      `phase: execution`.

7. `CHANGELOG.md` has no `## Unreleased` section today — its top heading is `## v0.132.4`.
   Insert `## Unreleased` directly above `## v0.132.4` (blank line before and after, matching
   the file's heading spacing), then add one bullet under it describing the fix from the user's
   point of view, in the style of the surrounding bullets. `docs/dod.md` requires this entry.

8. Before finishing, re-run `<verification>` and confirm it passes; then walk each requirement
   above against the change and confirm it is satisfied.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Do not change the generator implementations in `delivery/content-generator.go`, the
  `DeliverResult` signatures, or any error path.
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
