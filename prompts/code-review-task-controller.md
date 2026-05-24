---
status: draft
created: "2026-05-24T09:26:08Z"
---

<summary>
- Service reviewed using full automated code review with all specialist agents
- Fix prompts generated for each Critical or Important finding
- Each fix prompt is independently verifiable and scoped to one concern
- No code changes made — review-only prompt that produces fix prompts
- Clean services produce no fix prompts
</summary>

<objective>
Run a full code review of task/controller and generate a fix prompt for each Critical or Important finding.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done criteria.

Read 3 recent completed prompts from the prompts completed directory (highest-numbered) to understand prompt style and XML tag structure.

Service directory: `task/controller/`
</context>

<requirements>

## 1. Read Config

Read `.dark-factory.yaml` to find `prompts.inboxDir` (default: `prompts`). Use this as the output directory for fix prompts.

## 2. Run Code Review

Run `/coding:code-review full task/controller` to get a comprehensive review with all specialist agents.

Collect the consolidated findings categorized as:
- **Must Fix (Critical)** — will generate fix prompts
- **Should Fix (Important)** — will generate fix prompts
- **Nice to Have** — skip, do NOT generate prompts

## 3. Generate Fix Prompts

For each Critical or Important finding (or group of related findings in the same file/package), write a prompt file to the prompts inbox directory.

**Filename:** `review-task-controller-<fix-description>.md`

Each fix prompt must follow this exact structure. HTML comments below are instructions for the generator — replace with concrete content; do NOT copy the instruction text into the output.

```
---
status: draft
created: "<current UTC timestamp in ISO8601>"
---

<summary>
<!-- 5-10 plain-language bullets. No file paths, struct names, or function signatures. -->
<!-- Example: - Adds context cancellation check to long-running fetch loop to allow graceful shutdown -->
</summary>

<objective>
<!-- What to fix and why (1-3 sentences). End state, not steps. -->
<!-- Example: The HTTP handler swallows context cancellation. After this change, the handler returns ctx.Err() and callers see a clean shutdown. -->
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes (read ALL first):
<!-- List 1-5 specific files. Repo-relative paths. Optional ~line N hint. -->
<!-- Example: - task/controller/pkg/session/runner.go (~line 142, function Run) -->
</context>

<requirements>
<!-- Numbered, specific, unambiguous steps. Anchor by function/type name (~line N as hint only). Include function signatures where helpful. -->
<!-- If new constants pass through a library validator (Validate/Parse), include a contract test calling the validator on each value. -->
<!-- If editing a function with sibling implementations in the same package, address all siblings or extract a shared helper. -->
<!-- Coverage targets: ≥80% on new code; cover all paths for modified code. -->
</requirements>

<constraints>
- Only change files in `task/controller/`
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Follow project conventions in `CLAUDE.md` and `docs/` — error wrapping with `github.com/bborbe/errors` (never `fmt.Errorf` or bare `return err`), context propagation, factory pattern, time injection
</constraints>

<verification>
cd task/controller && make precommit
</verification>
```

**Grouping rules:**
- One concern per prompt (e.g., "fix error wrapping in package X")
- Group coupled findings that must change together
- Split unrelated findings into separate prompts
- If order matters, prefix filenames with `1-`, `2-`, `3-` (single-digit only)

## 4. Summary

Print a summary of findings and generated prompt files.

</requirements>

<constraints>
- Do NOT modify any source code — this is a review-only prompt
- Only write files to the prompts inbox directory
- Never write to `in-progress/` or `completed/` subdirectories
- Never prefix prompt filenames with dark-factory's global 3-digit number (`NNN-`). Single-digit ordering prefixes (`1-`, `2-`, `3-`) for multi-prompt batches are allowed
- Repo-relative paths only in generated prompts (no absolute, no `~/`)
- If no findings at Critical/Important level → report clean bill of health, generate no prompts
</constraints>

<verification>
# This prompt only generates markdown files — no code changes, no build needed.
ls prompts/review-task-controller-*.md 2>/dev/null || echo "No findings — clean review"
</verification>
