---
spec: ["041"]
status: pending
created: "2026-05-25T21:50:00Z"
---

<summary>
- Updates `docs/controller-design.md` to document the human_review guard behavior with `spawn_notification`
- Adds explicit note to the "Assignee-Clear on Escalation" section noting the guard fires regardless of `spawn_notification` state
- Documents AC#8 of spec 041
</summary>

<objective>
Update the "Assignee-Clear on Escalation" section of `docs/controller-design.md` to explicitly document that the `human_review` guard fires regardless of `spawn_notification` state on the merged frontmatter.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read `docs/controller-design.md` — specifically the "Assignee-Clear on Escalation (spec 021, refined by spec 039)" section (lines 59-71). This section needs the explicit `spawn_notification` note per AC#8 of the spec.

**Predecessor spec:** `specs/in-progress/041-spawn-notification-early-return-skips-human-review-guard.md` — AC#8 requires the note.
**Precedent prompt 075:** `prompts/completed/075-hotfix-apply-retry-counter-trigger-cap-before-spawn-notification.md` — doc already updated.
</context>

<requirements>

1. **Update `docs/controller-design.md`**

   Read the file. Locate the "Assignee-Clear on Escalation" section. Find the row for "Agent emits `Result.NextPhase: human_review` (legitimate handoff)" — this row documents the assignee-clear behavior from spec 039. Add an inline note to that row (or to the table's trailing paragraph if a dedicated row is absent):

   ```
   Note: the `assignee`-clear guard fires regardless of `spawn_notification` state on the merged frontmatter (spec 039 predecessor; spec 041 patch removed the unreachable-path gap when both signals are present simultaneously).
   ```

   The key text "regardless of spawn_notification state" MUST appear in the doc near the Assignee-Clear table after this change. Use the exact phrasing or equivalent in context.

   If the table lacks a dedicated row for the human_review handoff path, add a note to the section paragraph that covers it.

2. **Verify with grep**

   ```bash
   grep -n 'spawn_notification' /workspace/docs/controller-design.md
   ```

   Must return at least one match in or adjacent to the "Assignee-Clear on Escalation" table.

3. **Verify doc structure unchanged**

   ```bash
   grep -c 'Assignee-Clear\|###\|---\|| |' /workspace/docs/controller-design.md
   ```

   Must be > 0 (doc still has table structure).
</requirements>

<constraints>
- Only edit `docs/controller-design.md`.
- Do NOT add new table columns.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
grep -n 'spawn_notification' /workspace/docs/controller-design.md
grep -n 'Assignee-Clear' /workspace/docs/controller-design.md
```
The first grep must return a line near the Assignee-Clear section; the second must locate the section header.
