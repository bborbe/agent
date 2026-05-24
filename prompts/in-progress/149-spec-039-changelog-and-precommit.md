---
status: approved
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
queued: "2026-05-24T23:20:15Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `CHANGELOG.md` updated with an `## Unreleased` section documenting the spec 039 change
- Entry explains that controller no longer writes `phase: human_review` on `needs_input`/`failed`/cap paths
- Entry names spec 021 as the predecessor that established the escalation doctrine
- Both `make precommit` targets pass: `task/controller/` and `lib/`
- Final repo-wide write-side audit (spec AC #9) returns zero remaining controller-side or lib/delivery-side `"human_review"` writes
</summary>

<objective>
Add a `## Unreleased` section to `CHANGELOG.md` documenting the spec 039 change. The entry should explain that controller-side and lib/delivery-side code no longer writes `phase: human_review` on failure or cap-exhaustion paths, completing the spec-021 escalation doctrine. Then run both `make precommit` targets and the spec's AC #9 cross-package grep audit.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `CHANGELOG.md` — check if `## Unreleased` section already exists; observe the existing entry style (e.g. `fix(scope): description`, `chore(scope): ...`, `refactor(scope): ...`). Versions cycle frequently on this auto-committing repo, so the top version will drift between runs.

The spec's Constraints section notes: "CHANGELOG.md under `## Unreleased`: an operator-visible entry naming the doctrine narrowing." The entry should name spec 021 as the predecessor.

Existing CHANGELOG entry format (observed):
```
fix(scope): description
```
or
```
chore(scope): description
```
One bullet per scope; the scope identifies the affected package/module.
</context>

<requirements>

1. **Check if `## Unreleased` section exists** in `CHANGELOG.md`:
   ```bash
   grep -n '## Unreleased' CHANGELOG.md
   ```

   If it exists, append the new entries below any existing entries in that section.

2. **If `## Unreleased` does not exist**, insert it immediately above the first version header line matching the regex `^## v[0-9]+\.[0-9]+\.[0-9]+` (the top of the released section). Do NOT anchor on a specific version number — the top version drifts because the repo auto-commits.

   The new section should look like:
   ```markdown
   ## Unreleased

   ```

3. **Add the spec 039 entries** under `## Unreleased`. Use two separate bullets, one per scope (matches the existing CHANGELOG style of one scope per bullet):

   ```markdown
   - fix(controller): stop writing `phase: human_review` on `trigger_count` cap-exhaustion path in `task_increment_frontmatter_executor`; phase now reflects the lifecycle stage and only `assignee` is cleared (completes spec-021 escalation doctrine; spec-021 `needs_input` row superseded)
   - fix(lib/delivery): stop writing `phase: human_review` on `AgentStatusNeedsInput` and `AgentStatusFailed`/default branches in `result-deliverer` and `content-generator`; phase now reflects the lifecycle stage and only `assignee` is cleared (completes spec-021 escalation doctrine)
   ```

4. **Run `make precommit`** in both directories. Both must exit 0:
   ```bash
   cd task/controller && make precommit
   cd lib && make precommit
   ```

5. **Run the spec AC #9 cross-package write-side audit** as the final pre-release check:
   ```bash
   grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go
   ```

   List every remaining match and classify each as one of:
   - read-side: `phase == "human_review"`, `if phase == "human_review"`, switch case, etc.
   - comment-side: `// human_review …` documentation
   - supersession-note: `// see spec 039 …`, or `Result.NextPhase` literal references in `resolveNextPhase`

   Any match that LHS-assigns `"human_review"` to a `phase` map key, struct field, or frontmatter setter is a FAIL. Zero write-side matches allowed.

</requirements>

<constraints>
- Entry must use `fix:` prefix (this is a bug fix narrowing the meaning of `phase: human_review`)
- Entry must reference spec 021 as predecessor
- Do NOT use `feat:` prefix — this is a narrowing/bug fix, not a new feature
- Split into two bullets (`fix(controller): ...` and `fix(lib/delivery): ...`) for style consistency with existing CHANGELOG
- When inserting a new `## Unreleased` section, anchor by regex match on `^## v[0-9]+\.[0-9]+\.[0-9]+` — never hardcode a version number
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Unreleased section exists
grep -n '## Unreleased' CHANGELOG.md
# Expected: 1 match

# AC2: Both entries present with spec reference; scope to the Unreleased section
awk '/^## Unreleased/{flag=1; next} /^## v/{flag=0} flag' CHANGELOG.md
# Expected output contains both bullets — one fix(controller): ... and one fix(lib/delivery): ... — and at least one mentions spec-021

awk '/^## Unreleased/{flag=1; next} /^## v/{flag=0} flag' CHANGELOG.md | grep -E 'fix\(controller\):'
# Expected: 1 match
awk '/^## Unreleased/{flag=1; next} /^## v/{flag=0} flag' CHANGELOG.md | grep -E 'fix\(lib/delivery\):'
# Expected: 1 match
awk '/^## Unreleased/{flag=1; next} /^## v/{flag=0} flag' CHANGELOG.md | grep -E 'spec-021'
# Expected: at least 1 match

# AC3: task/controller precommit passes
cd task/controller && make precommit
# Expected: exit 0

# AC4: lib precommit passes
cd lib && make precommit
# Expected: exit 0

# AC5: Spec AC #9 cross-package audit — zero write-side matches remain
grep -rn '"human_review"' task/controller/pkg/ lib/delivery/ --include='*.go' | grep -v _test.go
# Inspect each match; classify as read-side / comment-side / supersession-note.
# Any LHS assignment to a phase field is a FAIL.
```
</verification>
