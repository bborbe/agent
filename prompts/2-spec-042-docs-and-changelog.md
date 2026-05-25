---
status: draft
spec: [042-update-frontmatter-executor-enforces-human-review-doctrine]
created: "2026-05-26T00:00:00Z"
branch: dark-factory/update-frontmatter-executor-enforces-human-review-doctrine
---

<summary>
- Documents the partial-update primitive (`UpdateFrontmatterCommand`) as a doctrine-constrained write path in the Assignee-Clear table
- Names `ClearAssigneeIfHumanReview` as the single enforcement chokepoint in both controller docs
- Updates `docs/task-flow-and-failure-semantics.md` Executor Publisher table notes and adds doctrine narrative for the partial-update primitive
- Appends an operator-visible CHANGELOG entry under `## Unreleased` naming spec 039 as predecessor and spec 042 as closure of the sixth write site
- No code changes — documentation-only prompt that depends on prompt 1 having shipped the helper
</summary>

<objective>
After this prompt, a new contributor reading `docs/controller-design.md` or `docs/task-flow-and-failure-semantics.md` learns that the partial-update primitive is constrained by the same `phase: human_review` → `assignee: ""` doctrine as the result writer, and that `result.ClearAssigneeIfHumanReview` is the named enforcement chokepoint. The CHANGELOG records the doctrine completion under `## Unreleased`.
</objective>

<context>
Read `CLAUDE.md` for project conventions. Read these guides:
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`

Read these project files:
- `docs/controller-design.md` — the "Assignee-Clear on Escalation" table is at § line 59. The `"update-frontmatter"` executor description is at § line 105.
- `docs/task-flow-and-failure-semantics.md` — the Executor Publisher Command Kinds table is at § line 176. The partial-update primitive feeds this table via `PublishFailure`, `PublishSpawnNotification`, and `PublishTypeMismatchFailure`.
- `CHANGELOG.md` — top entry is `## v0.63.8`. **Spec 041 prompt 2 (`2-spec-041-doc-and-changelog.md`) may have already added a `## Unreleased` heading between `# Changelog` and `## v0.63.8`. Check the actual file state before editing — if `## Unreleased` already exists, APPEND under it; if not, CREATE it.**
- `specs/in-progress/042-update-frontmatter-executor-enforces-human-review-doctrine.md` — ACs #10, #11, #12 define the doc/changelog evidence.
- `specs/completed/039-controller-stop-setting-human-review-on-failure.md` — predecessor; the doctrine name to cite.
- `prompts/2-spec-041-doc-and-changelog.md` (inbox or in-progress, depending on daemon timing) — sibling spec-041 prompt that may have already created `## Unreleased`. Coordinate so both entries coexist under the same heading.
</context>

<requirements>

1. **Update the Assignee-Clear table in `docs/controller-design.md`.**

   The current table at § "Assignee-Clear on Escalation (spec 021, refined by spec 039)" (around line 59) has three rows:

   ```
   | Escalation trigger | `phase` written | `assignee` written |
   |---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
   | Agent emits `needs_input` | unchanged (lifecycle stage preserved) | `""` |
   ```

   Update the table to (a) rename the heading to mention spec 042, (b) add a fourth row for the partial-update primitive, (c) add an enforcement-point column:

   ```
   ## Assignee-Clear on Escalation (spec 021, refined by spec 039, completed by spec 042)

   Every escalation path writes `assignee: ""` so the task surfaces in operator inbox.
   All four rows route through the single chokepoint `result.ClearAssigneeIfHumanReview`
   (for `human_review` paths) or `result.clearAssignee` (for cap paths) in
   `task/controller/pkg/result/result_writer.go`:

   | Escalation trigger | `phase` written | `assignee` written | Enforcement point |
   |---|---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` | `applyTriggerCap` → `clearAssignee` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` | `applyRetryCap` → `clearAssignee` |
   | Agent emits `Result.NextPhase: human_review` | `human_review` | `""` | `applyRetryCounter` → `ClearAssigneeIfHumanReview` |
   | Agent emits `UpdateFrontmatterCommand` with merged `phase: human_review` (spec 042) | `human_review` | `""` | `buildUpdateModifyFn` → `ClearAssigneeIfHumanReview` |
   ```

   Preserve the existing paragraph immediately below the table about cap stickiness ("Once a task is parked …"). Do not change other sections.

2. **Update the `"update-frontmatter"` (UpdateFrontmatterExecutor) section in `docs/controller-design.md`.**

   At § line 105 the section currently documents the executor flow with bullet points. Add a `human_review` doctrine guard step to the `AtomicReadModifyWriteAndCommitPush` bullet block, mirroring the surrounding style. After this prompt the block must read (note the new line — the rest is unchanged):

   ```
   ├── AtomicReadModifyWriteAndCommitPush:
   │     ├── read current file bytes (under mutex)
   │     ├── parse existing frontmatter
   │     ├── merge only the keys in Updates (all other keys unchanged)
   │     ├── if Body section provided → append/replace section in body (spec 016)
   │     ├── if merged phase == "human_review" → result.ClearAssigneeIfHumanReview clears assignee in the same write (spec 042)
   │     ├── write updated file (under mutex)
   │     └── git commit + push (under mutex)
   ```

   The "if Body section provided" line may already be present in the file (verify by reading the current section); add it only if absent. The "if merged phase == human_review" line is the new spec-042 line and must be added.

3. **Update `docs/task-flow-and-failure-semantics.md` Executor Publisher Command Kinds.**

   The table at § line 176 lists the publisher methods. AFTER the table (after the four-row block ending with `PublishTypeMismatchFailure`) and BEFORE § "Create-Task Path Resolution (spec-019)", insert a new paragraph block:

   ```
   **Partial-update doctrine guard (spec 042):** Every `update-frontmatter` command — regardless of which publisher emits it OR whether the agent emits it directly via the SDK — flows through `buildUpdateModifyFn` in `task/controller/pkg/command/task_update_frontmatter_executor.go`. That function applies the merge, optionally appends a body section, then calls `result.ClearAssigneeIfHumanReview` on the merged frontmatter before marshaling. If the merge produces `phase: human_review`, assignee is cleared to `""` and `previous_assignee` captures the prior value — in the same atomic write that performs the merge. This closes the sixth write site identified after spec 039's prod deploy (the 2026-05-25 pr-reviewer-agent incident on PR #3): no non-test code path in `task/controller/pkg/` or `lib/delivery/` can persist `phase: human_review` while leaving a non-empty `assignee` in the same atomic write.
   ```

   Style: match the existing prose tone (spec-039-style paragraph blocks elsewhere in the file). Keep the table itself unchanged — the new paragraph follows it.

4. **Add a CHANGELOG entry under `## Unreleased`.**

   Read `CHANGELOG.md` first. Two cases:

   **Case A — `## Unreleased` already exists (spec 041 prompt 162 ran first):** APPEND the new entry as a bullet under the existing `## Unreleased` heading. Place the new bullet immediately AFTER any existing spec-041 entry, separated only by a newline (no blank line between bullets within the same `## Unreleased` block). Do NOT duplicate the `## Unreleased` heading.

   **Case B — `## Unreleased` does not yet exist:** Insert a new `## Unreleased` heading BETWEEN `# Changelog` (line 1) and the current top version section (`## v0.63.8` at line 3). Format: one blank line above the new heading, one blank line below, then the new entry bullet.

   The new entry text (identical in both cases):

   ```
   - fix(task/controller): partial-update executor now enforces `phase: human_review` → `assignee: ""` doctrine via shared helper `result.ClearAssigneeIfHumanReview`. Closes the sixth `human_review` write site missed by spec 039 (predecessor); fixes the 2026-05-25 prod incident where pr-reviewer-agent emitted `UpdateFrontmatterCommand{Updates: {"phase": "human_review"}}` on PR #3 and the task landed with `assignee: pr-reviewer-agent` still set, bypassing the operator inbox filter.
   ```

   Detection script you can run before editing:

   ```bash
   grep -n '^## ' CHANGELOG.md | head -3
   ```

   If line 3 is `## Unreleased`, use Case A. If line 3 is `## v0.63.8` (or any version tag), use Case B.

5. **Verify the doc edits with grep.**

   ```bash
   # spec 042 AC#10
   grep -n 'UpdateFrontmatterCommand\|partial.update\|partial-update' docs/controller-design.md

   # spec 042 AC#11
   grep -n 'partial' docs/task-flow-and-failure-semantics.md

   # spec 042 AC#12
   grep -n 'spec 042\|ClearAssigneeIfHumanReview' CHANGELOG.md
   ```

   Expected:
   - First grep returns at least one match in the Assignee-Clear table area AND at least one match in the `"update-frontmatter"` section. At least one matched line names `ClearAssigneeIfHumanReview`.
   - Second grep returns at least one match referring to the partial-update primitive and naming the shared helper.
   - Third grep returns at least one match — the new `## Unreleased` entry text contains both `spec 042` (or `spec 039` for cross-reference) and `ClearAssigneeIfHumanReview` (or the shorter `human_review`).

6. **Run the controller precommit** to confirm the docs changes don't break markdown link checks or other repo-wide validators:

   ```bash
   cd task/controller && make precommit
   ```

   Must exit 0.
</requirements>

<constraints>
- Do NOT modify code files — this is a documentation-only prompt. Prompt 1 owns all `.go` edits.
- Do NOT bump any version in `CHANGELOG.md` — the entry goes under `## Unreleased`; the release process owns version bumps.
- Do NOT duplicate the `## Unreleased` heading if spec 041's prompt 162 already created it (Case A above).
- Do NOT remove or weaken any existing row in the Assignee-Clear table — spec 042 ADDS a fourth row and reframes the heading; it does not delete prior rows.
- Do NOT touch the `"increment-frontmatter"` (IncrementFrontmatterExecutor) section — spec 042 only constrains the partial-update path; the increment-frontmatter path is out of scope (its trigger-count cap escalation already routes through `clearAssignee` independently, per spec 039).
- Do NOT commit — dark-factory handles git.
- If spec 041's prompt 162 has already updated the Assignee-Clear table heading or other shared doc areas, RECONCILE rather than overwrite: the heading mentioning "spec 042 completion" must coexist with whatever spec 041 wrote. Read first, then edit.
</constraints>

<verification>
```bash
# AC#10 — controller-design.md table + section
grep -n 'UpdateFrontmatterCommand\|partial.update\|partial-update' docs/controller-design.md
grep -n 'ClearAssigneeIfHumanReview' docs/controller-design.md

# AC#11 — task-flow-and-failure-semantics.md partial-update enumeration
grep -n 'partial' docs/task-flow-and-failure-semantics.md
grep -n 'ClearAssigneeIfHumanReview' docs/task-flow-and-failure-semantics.md

# AC#12 — CHANGELOG entry
grep -n '^## ' CHANGELOG.md | head -3
grep -A 5 '## Unreleased' CHANGELOG.md | head -20

# AC#13 — precommit clean (docs changes don't break anything)
cd task/controller && make precommit
```

Expected:
- All three doc files show the spec-042 additions.
- `## Unreleased` heading appears exactly once in `CHANGELOG.md` and contains the spec-042 entry text (and the spec-041 entry text too, if prompt 162 ran first).
- `make precommit` exits 0.
</verification>
