---
spec: ["041"]
status: pending
created: "2026-05-25T21:51:00Z"
---

<summary>
- Adds a `fix(controller):` entry under `## Unreleased` in CHANGELOG.md
- Documents the applyRetryCounter reorder fix with spec 039 reference and prod incident date
- Entry uses `fix(controller):` prefix (patch bump per changelog-guide conventions)
</summary>

<objective>
Append a changelog entry under `## Unreleased` in `CHANGELOG.md` naming the spec-041 fix, referencing spec 039 and the 2026-05-25 incident.
</objective>

<context>
Read `CHANGELOG.md` to locate `## Unreleased`. Read the changelog-guide at `~/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — entry format is `- <prefix>: <what> [context]`, `fix:` = patch bump.

**Precedents:**
- `fix(controller): stop writing phase: human_review on trigger_count cap-exhaustion path` (v0.62.29)
- `fix(task/controller): pass context to injectTaskIdentifier` (v0.63.5)

**Spec reference:** `specs/in-progress/041-spawn-notification-early-return-skips-human-review-guard.md` — AC#9.
</context>

<requirements>

1. **Append to `CHANGELOG.md` under `## Unreleased`**

   Read the top of the file. Append this entry as the first entry under `## Unreleased` (newest first):

   ```
   - fix(controller): reorder `applyRetryCounter` in result_writer to run the spec-039 `human_review` guard BEFORE the `spawn_notification` early return; fixes a live regression where `spawn_notification: true` inherited via `mergeFrontmatter` skipped the `assignee`-clear guard on `phase: human_review` handoffs (spec 039 predecessor; prod incident 2026-05-25, task `bborbe-agent #3`)
   ```

   The entry MUST use the `fix(controller):` prefix. It MUST reference `spawn_notification` and spec 039.

2. **Verify grep**

   ```bash
   grep -n 'fix(controller).*spawn_notification' /workspace/CHANGELOG.md
   ```

   Must return the new entry line number near the top of the file (within first 30 lines).
</requirements>

<constraints>
- Only edit the `## Unreleased` section of `CHANGELOG.md`.
- Do NOT move, delete, or alter the preamble or existing version sections.
- Do NOT commit — dark-factory handles git.
- Entry must use `fix(controller):` prefix (patch bump per changelog-guide).
</constraints>

<verification>
```bash
grep -n 'fix(controller).*spawn_notification' /workspace/CHANGELOG.md
head -30 /workspace/CHANGELOG.md | grep '## Unreleased'
```
The first must return the new entry; the second must confirm `## Unreleased` exists at top.
