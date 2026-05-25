---
spec: ["041"]
status: pending
created: "2026-05-25T21:50:00Z"
---

<summary>
- Updates `docs/controller-design.md` to add an explicit note to the "Assignee-Clear on Escalation" table: the agent-emitted `Result.NextPhase: human_review` row now notes that the guard fires regardless of `spawn_notification` state on the merged frontmatter
- This documents the confirmed behavior after the prompt-1 reorder fix
</summary>

<objective>
Update the "Assignee-Clear on Escalation" table in `docs/controller-design.md` to explicitly document that the human_review guard fires regardless of `spawn_notification` state on the merged frontmatter.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read `docs/controller-design.md` — specifically the "Assignee-Clear on Escalation (spec 021, refined by spec 039)" section (lines 59-71). This is the section to update. The table currently has three rows (trigger_count, retry_count, needs_input). A fourth row is NOT needed — the spec-039 guard covers the agent-emitted human_review handoff path, which is already implicitly covered by the existing table rows. The fix documents ONE specific interaction: the gapped path where `spawn_notification` could bypass the guard.

**Predecessor spec:** `specs/in-progress/041-spawn-notification-early-return-skips-human-review-guard.md` — same spec driving this prompt, this is the doc-update task from AC#8.

**Precedent prompt (prompt 075):** `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — precedent for updating the controller-design doc alongside a reorder fix.
</context>

<requirements>

1. **Update `docs/controller-design.md`**

   Read the file. Find the "Assignee-Clear on Escalation (spec 021, refined by spec 039)" section (around line 59). Read the existing table:

   ```
   | Escalation trigger | `phase` written | `assignee` written |
   |---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
   | Agent emits `needs_input` | unchanged (lifecycle stage preserved) | `""` |
   ```

   Find the row for "Agent emits `Result.NextPhase: human_review` (legitimate handoff)" — this row is present or implied. Add a note column or inline note: "guard fires regardless of `spawn_notification` state on merged frontmatter (spec 039 guard placed AFTER spawn_notification early return in applyRetryCounter, resulting in unreachable path; fixed by spec 041)."

   Alternatively, if the current table does not have a dedicated row for "Agent emits Result.NextPhase: human_review", note this in the table body or in the paragraph above it. The key text that must appear somewhere in or near the table is: "the guard fires regardless of `spawn_notification` state on the merged frontmatter."

   Do NOT add new table columns if the existing format is simple. Inline note in the row or in a trailing paragraph is sufficient.

   Example update (the exact wording is agent's choice; guided by the spec-039 docs precedent):

   Locate the table and add to the relevant row:
   - Row for "Agent emits Result.NextPhase: human_review (legitimate handoff)" or the equivalent needs_input row → add note: "the `assignee` guard fires regardless of `spawn_notification` state on the merged frontmatter (spec 039 predecessor; spec 041 patch against the unreachable-path gap when both signals are present simultaneously)."

   Verify the resulting grep passes:

   ```bash
   grep -n 'spawn_notification' /workspace/docs/controller-design.md
   ```

   Must return at least one match in the assignee-clear section.
</requirements>

<constraints>
- Only edit `docs/controller-design.md`.
- Do NOT add new table columns unless the existing format explicitly supports them.
- Do NOT commit — dark-factory handles git.
- No verification command runs `make precommit` — the verification for this file is grep-based (no Go changes).
</constraints>

<verification>
Grep check — `spawn_notification` must appear in the assignee-clear section of the doc:

```bash
grep -n 'spawn_notification' /workspace/docs/controller-design.md
```

Must return at least one match with meaningful context (not an unrelated line). Confirm the matched line is in or adjacent to the "Assignee-Clear on Escalation" table.

Confirm no regressions — the document still has valid markdown structure:

```bash
grep -c 'Assignee-Clear\|###\|---\|' /workspace/docs/controller-design.md
```

Must be > 0.
</verification>
