---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `docs/controller-design.md` updated to reflect that `needs_input` no longer writes `phase: human_review`
- The Assignee-Clear table row for `needs_input` now shows `phase: unchanged` instead of `phase: human_review`
- The `increment-frontmatter` cap escalation pseudocode no longer shows `phase = "human_review"` being set
- The "On agent-task-v1-request" pseudocode for `needs_input` now shows `clear assignee: ""` instead of `set phase: human_review, clear assignee: ""`
</summary>

<objective>
Update `docs/controller-design.md` to document the new doctrine: controller-side and lib/delivery-side code must NOT write `phase: human_review` on failure or cap-exhaustion paths. Only the agent's own `Result.NextPhase` can produce that value via `resolveNextPhase`.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `docs/controller-design.md` — specifically the sections that need updating per the spec's Constraints section:
  - Line ~42: the `if agent emits needs_input` bullet in the request-flow pseudocode
  - Line ~67: the Assignee-Clear table row for `needs_input`
  - Line ~97: the increment-frontmatter cap escalation step

This spec completes the work from spec-021 (Clear Assignee on Escalation) by fixing the remaining write sites that 021 missed.
</context>

<requirements>

1. **Update the "On agent-task-v1-request" pseudocode section** (around line 42):
   - Change the `needs_input` handling from `set phase: human_review, clear assignee: ""` to `clear assignee: "" (phase unchanged)`

   **Old text (around line 42):**
   ```
   │     └── if agent emits needs_input (phase: human_review) → set phase: human_review, clear assignee: ""
   ```

   **New text:**
   ```
   │     └── if agent emits needs_input → clear assignee: "" (phase unchanged per spec-021/superseded-by-spec-039)
   ```

2. **Update the "Assignee-Clear on Escalation" table** (around line 63-68):
   - Change the row for "Agent emits `needs_input`" from `phase: human_review` to `unchanged`

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

3. **Update the "increment-frontmatter" flow pseudocode** (around line 96-98):
   - Change the cap escalation step from `set phase = "human_review" in the same write` to `clear assignee in the same write, leave phase unchanged`

   **Old text (around line 96-98):**
   ```
   │     │     ├── cap escalation: if Field == "trigger_count" AND newVal >= max_triggers
   │     │     │     └── set phase = "human_review" in the same write
   ```

   **New text:**
   ```
   │     │     ├── cap escalation: if Field == "trigger_count" AND newVal >= max_triggers
   │     │     │     └── clear assignee in the same write, phase unchanged (spec 039)
   ```

4. **Verify the grep for human_review in the doc**:
   ```bash
   grep -n 'human_review' docs/controller-design.md
   ```
   The only remaining references should be:
   - The supersession note for spec-039
   - Any legitimate `Result.NextPhase` handoff reference (if any)

   No write-side references to `human_review` should remain in the non-comment, non-unchanged context.

</requirements>

<constraints>
- Only update the three specified locations in `docs/controller-design.md`
- Add a note about spec-039 superseding the spec-021 needs_input row
- Do NOT change any other documentation in this file
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Table row updated
grep -A1 'Agent emits.*needs_input' docs/controller-design.md
# Expected: phase shown as "unchanged" not "human_review"

# AC2: Increment-frontmatter flow updated
grep -A1 'cap escalation' docs/controller-design.md
# Expected: clear assignee mentioned, not phase = human_review

# AC3: No write-side human_review references
grep -n 'human_review' docs/controller-design.md
# Expected: only comment/supersession references, no assignments
```
</verification>