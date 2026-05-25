---
status: completed
spec: [041-spawn-notification-early-return-skips-human-review-guard]
summary: 'Updated docs/controller-design.md with spec-041 invariant and added fix(controller): entry to CHANGELOG.md under ## Unreleased'
container: agent-exec-167-spec-041-doc-and-changelog
dark-factory-version: v0.173.0
created: "2026-05-25T22:55:00Z"
queued: "2026-05-25T22:35:21Z"
started: "2026-05-25T22:41:04Z"
completed: "2026-05-25T22:48:52Z"
branch: dark-factory/spawn-notification-early-return-skips-human-review-guard
---

<summary>
- Annotates the `docs/controller-design.md` "Assignee-Clear on Escalation" section with the spec-041 invariant: the `human_review` guard fires regardless of `spawn_notification` state on the merged frontmatter
- Adds an `Unreleased`-section `fix(controller):` entry to `CHANGELOG.md` naming the bug, citing spec 039 as predecessor and the 2026-05-25 prod incident as trigger
- Documentation-only follow-up to the code reorder in prompt 1-spec-041
- No code changes, no test changes
</summary>

<objective>
Doctrine doc reflects the new invariant and the changelog records the fix. After this prompt, a new contributor reading `docs/controller-design.md` cannot mistakenly conclude that the `human_review` guard is gated by `spawn_notification`; the CHANGELOG entry under `## Unreleased` traces the fix to spec 039 and the 2026-05-25 prod incident.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — Keep a Changelog format; one entry per fix; reference the type/scope (`fix(controller):`); cite predecessor specs and incident dates.

Read these project files before editing:
- `docs/controller-design.md` — focus on the section `## Assignee-Clear on Escalation (spec 021, refined by spec 039)` (around line 59). This section was last updated by prompt 146 (spec-039 doctrine sync). The table and the trailing paragraph are the edit targets.
- `CHANGELOG.md` — the latest version section is at the top of the file (currently `## v0.63.8`, but anchor by the first `## v` heading rather than the literal version number — releases land continuously). Add a new `## Unreleased` section ABOVE the latest version section if no `## Unreleased` heading exists yet; otherwise prepend the new entry under the existing `## Unreleased`.

Read the spec for evidence shape and constraints:
- `specs/in-progress/041-spawn-notification-early-return-skips-human-review-guard.md` — Acceptance Criteria AC#8 (doc update) and AC#9 (CHANGELOG entry) define the exact evidence the verifier will grep for.

**Predecessor for style reference:**
- `prompts/completed/146-spec-039-controller-design-doc-update.md` — same `docs/controller-design.md` section; mirror the in-place markdown edit style (do not rewrite the section, just amend).
- `prompts/completed/149-spec-039-changelog-and-precommit.md` — the spec-039 changelog entry that this fix builds on.
</context>

<requirements>

1. **Update `docs/controller-design.md` § "Assignee-Clear on Escalation" (around line 59)** to record the spec-041 invariant.

   The current section (lines 59-72) reads (do NOT rely on these line numbers — anchor by the heading text and the table):
   ```
   ## Assignee-Clear on Escalation (spec 021, refined by spec 039)

   Every escalation path writes `assignee: ""` so the task surfaces in operator inbox:

   | Escalation trigger | `phase` written | `assignee` written |
   |---|---|---|
   | `trigger_count >= max_triggers` | unchanged (lifecycle stage preserved) | `""` |
   | `retry_count >= max_retries` | unchanged (lifecycle stage preserved) | `""` |
   | Agent emits `needs_input` | unchanged (lifecycle stage preserved) | `""` |

   Once a task is parked (escalation section present, `assignee: ""`), repeated stale agent
   result publishes are idempotent: the escalation section is not duplicated, the lifecycle
   phase is restored from the on-disk value, and assignee stays empty.
   ```

   Required edits:

   a. **Update the section header** from `## Assignee-Clear on Escalation (spec 021, refined by spec 039)` to `## Assignee-Clear on Escalation (spec 021, refined by specs 039 and 041)`.

   b. **Add a new table row** for the agent's legitimate `Result.NextPhase: human_review` handoff. The existing table covers `trigger_count` cap, `retry_count` cap, and `needs_input` — but NOT the `Result.NextPhase: human_review` handoff path which is the very path spec 041 fixes. Add the new row after the `needs_input` row:
   ```
   | Agent emits `Result.NextPhase: human_review` (legitimate handoff) | `human_review` (from `resolveNextPhase`) | `""` (guard fires regardless of `spawn_notification` state on merged frontmatter) |
   ```

   c. **Append a new paragraph** AFTER the existing "Once a task is parked..." paragraph, explaining the spec-041 invariant in prose:
   ```
   The `phase == "human_review"` assignee-clear guard in `resultWriter.applyRetryCounter`
   runs BEFORE the `spawn_notification` early return. This ordering is load-bearing: on
   a pr-reviewer agent's first post-spawn write, the merged frontmatter carries
   `spawn_notification: true` (inherited from the executor's spawn-time
   `UpdateFrontmatterCommand`) AND incoming `phase: human_review` (from
   `Result.NextPhase` via `resolveNextPhase`). The guard fires regardless of
   `spawn_notification` state — see spec 041 for the 2026-05-25 prod incident reproducer
   and prompt 075 for the same reorder pattern applied to `applyTriggerCap` on
   2026-04-24.
   ```

   d. **Do NOT delete or weaken** any existing row or paragraph. The edits are additive (one new row, one new paragraph) plus the one-line header change.

2. **Add a CHANGELOG entry to `CHANGELOG.md`** under an `## Unreleased` heading.

   Anchor the insertion point: find the first line matching `^## v[0-9]+\.[0-9]+\.[0-9]+` (the most recent versioned release section — at the time of writing this is `## v0.63.8`, but it may have advanced). If a `## Unreleased` heading does NOT yet exist between `# Changelog` and that first versioned section, insert one BETWEEN them (one blank line above and below the new heading). Then add a single entry below the `## Unreleased` heading:

   ```
   ## Unreleased

   - fix(controller): `resultWriter.applyRetryCounter` now runs the `phase == "human_review"` assignee-clear guard BEFORE the `spawn_notification` early return, so the spec 039 guard fires on the pr-reviewer agent's first post-spawn handoff. Previously the inherited `spawn_notification: true` on the merged frontmatter short-circuited the function before the guard ran, leaving `assignee: <agent>` on a task at `phase: human_review` and hiding it from the operator inbox filter. Live prod incident 2026-05-25 (~8h after the spec 039 deploy); second instance of the same bug class (precedent: 2026-04-24 `applyTriggerCap` reorder, prompt 075).
   ```

   Required substrings (for AC#9 verifier grep):
   - `fix(controller)`
   - `spec 039`
   - `spawn_notification`

3. **Run the documentation acceptance criteria checks:**

   AC#8 evidence:
   ```bash
   grep -n 'spawn_notification' docs/controller-design.md
   ```
   Expected: at least one line in the "Assignee-Clear on Escalation" section mentions that the guard fires irrespective of `spawn_notification`.

   AC#9 evidence:
   ```bash
   grep -A 3 'fix(controller)' CHANGELOG.md | head -20
   ```
   Expected: shows the new `Unreleased` entry; the entry text contains both `spec 039` and `spawn_notification`.

4. **Run the controller precommit** (this catches markdown formatting / changelog lint if hooks check it):
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

   Then from the repo root:
   ```bash
   cd /workspace && make precommit
   ```
   If a repo-root `make precommit` target exists and runs docs/changelog lint, it must also exit 0. If no such target exists, this is a no-op — skip.

</requirements>

<constraints>
- Do NOT touch `docs/task-flow-and-failure-semantics.md` — the spec explicitly states "already states the doctrine; no edit required" (Constraints → Relevant docs).
- Do NOT modify `task/controller/pkg/result/result_writer.go` or any other code file — this prompt is documentation-only. The code change is owned by prompt 1-spec-041.
- Do NOT modify any test file — this prompt is documentation-only.
- Do NOT create new doc files, ADRs, or runbooks. The spec's AC#12 ("No new scenario test") and the doctrine-update style established by spec 039 (prompt 146) both require in-place edits to the existing doc.
- Do NOT bump the version in `CHANGELOG.md`. The entry goes under `## Unreleased`; the release process owns version bumps.
- Do NOT remove any existing `## v<X.Y.Z>` versioned release section.
- Do NOT add Claude attribution (no "Generated with Claude", no "Co-Authored-By") anywhere.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
From the repo root (`/workspace`):

```bash
# AC#8: doctrine doc names the invariant
grep -n 'spawn_notification' docs/controller-design.md
```
Expected: at least one match inside the "Assignee-Clear on Escalation" section; the matched line mentions the guard fires regardless of (or irrespective of) `spawn_notification`.

```bash
# AC#9: CHANGELOG entry exists with required substrings
grep -A 3 'fix(controller)' CHANGELOG.md | head -20
```
Expected: shows the `Unreleased` entry; text contains `spec 039` and `spawn_notification`.

```bash
# AC#10: precommit
cd task/controller && make precommit
```
Expected: exit 0; final log line contains `ready to commit`.
</verification>
