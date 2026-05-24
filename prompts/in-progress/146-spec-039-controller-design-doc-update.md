---
status: approved
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `docs/controller-design.md` updated to reflect that `needs_input` no longer writes `phase: human_review`
- The Assignee-Clear table row for `needs_input` now shows `phase: unchanged` instead of `phase: human_review`
- The `increment-frontmatter` cap escalation pseudocode no longer shows `phase = "human_review"` being set
- The "On agent-task-v1-request" pseudocode for `needs_input` now shows `clear assignee: ""` instead of `set phase: human_review, clear assignee: ""`
- The "Assignee-Clear on Escalation" section header is touched up to note the spec-039 refinement
</summary>

<objective>
Update `docs/controller-design.md` to document the new doctrine: controller-side and lib/delivery-side code must NOT write `phase: human_review` on failure or cap-exhaustion paths. Only the agent's own `Result.NextPhase` can produce that value via `resolveNextPhase`.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `docs/controller-design.md` — read the whole file end-to-end first so you understand what `human_review` references already exist and which are legitimate (e.g. `Result.NextPhase` handoff). The spec calls out three primary locations:
  - Line ~42: the `if agent emits needs_input` bullet in the request-flow pseudocode
  - Line ~63-68: the "Assignee-Clear on Escalation" table (including its section header around line ~62)
  - Line ~96-98: the increment-frontmatter cap escalation step in the pseudocode

This spec completes the work from spec-021 (Clear Assignee on Escalation) by fixing the remaining write sites that 021 missed.
</context>

<requirements>

1. **Update the "On agent-task-v1-request" pseudocode section** (around line 42):

   **Old text (around line 42):**
   ```
   │     └── if agent emits needs_input (phase: human_review) → set phase: human_review, clear assignee: ""
   ```

   **New text:**
   ```
   │     └── if agent emits needs_input → clear assignee: "" (phase unchanged; spec-039 supersedes spec-021 for this row)
   ```

2. **Touch up the "Assignee-Clear on Escalation" section header** (around line 62) to reflect the spec-039 refinement. If the header reads:

   **Old:**
   ```
   ## Assignee-Clear on Escalation (spec 021)
   ```

   **New:**
   ```
   ## Assignee-Clear on Escalation (spec 021, refined by spec 039)
   ```

   If the actual header text differs (e.g. capitalization or wording), preserve the existing style — just add the `, refined by spec 039` qualifier in parentheses alongside any existing `spec 021` reference.

3. **Update the "Assignee-Clear on Escalation" table** (around line 63-68):
   - Change the row for "Agent emits `needs_input`" from `phase: human_review` to `unchanged (lifecycle stage preserved)` so it matches the other two rows

   **Old table:**
   ```
   | Escalation trigger | `phase` written | `assignee` written |
   |---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
   | Agent emits `needs_input` | `human_review` | `""` |
   ```

   **New table:**
   ```
   | Escalation trigger | `phase` written | `assignee` written |
   |---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
   | Agent emits `needs_input` | unchanged (lifecycle stage preserved) | `""` |
   ```

4. **Update the "increment-frontmatter" flow pseudocode** (around line 96-98):

   **Old text (around line 96-98):**
   ```
   │     │     ├── cap escalation: if Field == "trigger_count" AND newVal >= max_triggers
   │     │     │     └── set phase = "human_review" in the same write
   ```

   **New text:**
   ```
   │     │     ├── cap escalation: if Field == "trigger_count" AND newVal >= max_triggers
   │     │     │     └── clear assignee in the same write (phase unchanged; spec-039 supersedes spec-021 for this row)
   ```

5. **Enumerate and verify remaining `human_review` references in the doc.** After the edits above, every remaining `human_review` line in `docs/controller-design.md` must be one of these legitimate categories:
   - (a) A reference to `Result.NextPhase = "human_review"` (the agent-emitted handoff — the only legal write of that phase)
   - (b) A supersession / doctrine note explaining what the controller does NOT do (e.g. "the controller never writes `phase: human_review` on failure paths")
   - (c) An explanatory note about the result writer's existing line-180 `human_review` guard (which fires only for the `Result.NextPhase` legitimate handoff)

   List each remaining match by line number in the verification output and ensure none of them is a write-side directive saying the controller or deliverer writes `human_review` on a failure or cap path.

</requirements>

<constraints>
- The three required content updates (request-flow pseudocode, table row, increment-frontmatter pseudocode) are mandatory
- Section-header touch-ups (e.g. adding `, refined by spec 039` qualifiers) are allowed and expected
- Do NOT add or remove sections wholesale; this is a refinement update, not a rewrite
- Do NOT change documentation in this file outside the touched-up sections
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Table row updated — needs_input now shows phase: unchanged
grep -E '\| Agent emits .?needs_input.? \| unchanged' docs/controller-design.md
# Expected: exit 0 (match found)

# AC2: Table row no longer shows human_review for needs_input
! grep -E '\| Agent emits .?needs_input.? \| .?human_review.? \|' docs/controller-design.md
# Expected: exit 0 (no match)

# AC3: Increment-frontmatter pseudocode updated
grep 'clear assignee in the same write' docs/controller-design.md
# Expected: exit 0

# AC4: Increment-frontmatter pseudocode no longer sets phase = human_review
! grep 'set phase = "human_review" in the same write' docs/controller-design.md
# Expected: exit 0

# AC5: Request-flow pseudocode updated
grep 'if agent emits needs_input → clear assignee' docs/controller-design.md
# Expected: exit 0

# AC6: Section header refinement
grep -E 'Assignee-Clear on Escalation \(spec 021, refined by spec 039\)' docs/controller-design.md
# Expected: exit 0 (or the equivalent if the original header phrasing differed — manual inspection)

# AC7: Enumerate every remaining human_review reference. Each line must be a legitimate
# category: (a) Result.NextPhase reference, (b) supersession/doctrine note explaining
# the controller does NOT write this phase, or (c) explanation of the result-writer's
# existing assignee-clear guard. Manual review required.
grep -n 'human_review' docs/controller-design.md
# Expected: every match maps to one of (a)/(b)/(c) above. No write-side directives remain.

# AC8: No write-side directive saying controller/deliverer sets phase to human_review on failure
! grep -Ei '(controller|deliverer).*sets? phase.*human_review|set phase.*human_review.*on (failure|cap|needs_input)' docs/controller-design.md
# Expected: exit 0
```
</verification>
