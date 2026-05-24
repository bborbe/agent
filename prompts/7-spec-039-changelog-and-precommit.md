---
status: draft
spec: [039-controller-stop-setting-human-review-on-failure]
created: "2026-05-25T00:00:00Z"
branch: dark-factory/controller-stop-setting-human-review-on-failure
---

<summary>
- `CHANGELOG.md` updated with an `## Unreleased` section documenting the spec 039 change
- Entry explains that controller no longer writes `phase: human_review` on `needs_input`/`failed`/cap paths
- Entry names spec 021 as the predecessor that established the escalation doctrine
- Both `make precommit` targets pass: `task/controller/` and `lib/`
</summary>

<objective>
Add a `## Unreleased` section to `CHANGELOG.md` documenting the spec 039 change. The entry should explain that controller-side and lib/delivery-side code no longer writes `phase: human_review` on failure or cap-exhaustion paths, completing the spec-021 escalation doctrine.
</objective>

<context>
Read CLAUDE.md for project conventions.

**Files to read before implementing:**
- `CHANGELOG.md` — check if `## Unreleased` section already exists
- Read the changelog-guide.md if needed for entry format

The spec's Constraints section notes: "CHANGELOG.md under `## Unreleased`: an operator-visible entry naming the doctrine narrowing." The entry should name spec 021 as the predecessor.
</context>

<requirements>

1. **Check if `## Unreleased` section exists** in `CHANGELOG.md`:
   ```bash
   grep -n '## Unreleased' CHANGELOG.md
   ```
   
   If it exists, append the new entry below any existing entries.

2. **If `## Unreleased` does not exist**, insert it immediately above `## v0.62.25`:
   ```markdown
   ## Unreleased

   ```

3. **Add the spec 039 entry** under `## Unreleased`:
   ```markdown
   - fix(controller,lib/delivery): stop writing `phase: human_review` on `needs_input`/`failed`/cap-exhaustion paths; phase now strictly reflects the lifecycle stage on parked tasks (completes spec-021 escalation doctrine; spec-021 `needs_input` row is superseded)
   ```

4. **Run `make precommit`** in both directories:
   ```bash
   cd task/controller && make precommit
   cd lib && make precommit
   ```
   
   Both must exit 0.

</requirements>

<constraints>
- Entry must use `fix:` prefix since this is a bug fix (narrowing the meaning of `phase: human_review`)
- Entry must reference spec 021 as predecessor
- Do NOT use `feat:` prefix — this is a narrowing/bug fix, not a new feature
- Do NOT commit — dark-factory handles git
</constraints>

<verification>
```bash
# AC1: Unreleased section exists
grep -n '## Unreleased' CHANGELOG.md
# Expected: match found

# AC2: Entry present with spec reference
grep -A3 '## Unreleased' CHANGELOG.md | grep -E 'spec-021|spec-039|human_review'
# Expected: entry with spec-021 reference

# AC3: task/controller precommit passes
cd task/controller && make precommit
# Expected: exit 0

# AC4: lib precommit passes
cd lib && make precommit
# Expected: exit 0
```
</verification>