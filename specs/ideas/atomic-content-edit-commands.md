---
tags:
  - dark-factory
  - spec
status: idea
---

## Summary

- Extend the atomic-command model from spec 015 (frontmatter) to task **body** edits.
- Introduce three new commands — `ReplaceBody`, `ReplaceSection`, `AppendSection` — each executed under the gitclient mutex using the same `AtomicReadModifyWriteAndCommitPush` primitive as spec 015.
- Section identification: exact `## <title>` match, case-sensitive, `##` only (not `###`+). Missing or duplicate sections are errors.
- Coexists with the existing `TaskResultExecutor` whole-file-rewrite path. No forced migration; agents opt in.
- Eliminates the "nothing to commit, working tree clean" idempotent-write failure for body updates the same way spec 015 did for frontmatter counters.

## Problem

Spec 015 solved idempotent-commit failures and race conditions for frontmatter updates by introducing atomic, field-scoped command handlers. Body edits are still stuck on the old path: `TaskResultExecutor` rewrites the whole file (frontmatter + body) every cycle. This is:

- **Coarse-grained.** An agent that only wants to update its "Result" section must reconstruct the entire body.
- **Idempotent-prone.** If the agent's output is byte-identical to the prior cycle, the write produces an empty diff and the controller sees the same `nothing to commit, working tree clean` error spec 015 removed from the frontmatter path.
- **Race-prone.** A partial-body update can clobber or be clobbered by a concurrent frontmatter update. Spec 015 closed the frontmatter–frontmatter race; the body–frontmatter race remains.

The underlying fix — serialize writes through the gitclient mutex and scope the change to the minimum necessary bytes — applies equally to body content.

## Goal

After this work:

1. Agents can update a named `##` section of a task body without reconstructing the rest of the file.
2. Agents can append a new `##` section without scanning the body.
3. Agents can replace the entire body (frontmatter preserved) as a coarse fallback.
4. All three operations run atomically under the gitclient mutex, serialized with frontmatter commands from spec 015, and produce either a real commit or a typed error — never an idempotent-commit failure that masks a real bug.
5. The existing full-file `TaskResultExecutor` path continues to work unchanged for agents that have not migrated.

## Non-goals

- NOT modifying the frontmatter commands from spec 015.
- NOT adding a delete-section command. If a concrete need appears later, it is a separate spec.
- NOT supporting nested-heading sections (`###` or deeper) as addressable units — they remain inside their parent `##` section.
- NOT deduplicating `AppendSection` — callers are responsible for their own dedup logic.
- NOT migrating existing agents. `TaskResultExecutor` coexists indefinitely.
- NOT changing the gitclient mutex or introducing a new lock.

## Desired Behavior

1. **ReplaceBody(taskID, newBody).** The entire body (everything after the closing frontmatter `---`) is replaced with `newBody`. Frontmatter bytes are preserved exactly. Trailing-newline handling is well-defined (documented: body ends with exactly one `\n`).

2. **ReplaceSection(taskID, sectionTitle, newContent).** The controller locates the single `## <sectionTitle>` heading in the body. Everything from that heading through (but not including) the next `## ` heading — or end of file if none — is replaced with `## <sectionTitle>\n<newContent>`, preserving the section boundary of whatever follows.

3. **AppendSection(taskID, sectionTitle, content).** The controller appends `\n## <sectionTitle>\n<content>\n` to the end of the body, ensuring the body had a trailing newline first (inserting one if needed). No check for existing sections with the same title.

4. **Section identification rule (frozen).** Exact string match on `## <title>`, case-sensitive. Heading level `##` only. Content under deeper headings (`###`+) belongs to the enclosing `##` section. A missing section on `ReplaceSection` is an error. Multiple matching `## <title>` headings is an error.

5. **Concurrency.** `ReplaceBody`, `ReplaceSection`, `AppendSection`, `UpdateFrontmatter`, and `IncrementFrontmatter` for the same task serialize through the gitclient mutex. Interleaved applications all land; none are lost.

6. **Observability.** A new metric `ContentCommandsTotal{operation, outcome}` counts the three content operations separately from spec 015's `FrontmatterCommandsTotal`. Outcomes include `ok`, `not_found`, `ambiguous`, and `error`.

7. **Recommendation (documentation, not enforcement).** New agents prefer `ReplaceSection("Result", ...)` for result writes. Legacy agents may keep the full-body path.

## Design Decisions

1. **No `SetFrontmatter(key, value)` convenience command.** Spec 015's `UpdateFrontmatter(updates map)` called with a single-key map covers this exact need. Adding a one-field wrapper doubles the command surface for no semantic gain. Rationale: keep the command vocabulary minimal; composition over variants.

2. **Section identification rule is exact `## <title>`, case-sensitive, `##` only.** Rationale:
   - Case-sensitive avoids silent matches on capitalization drift between agents (`## result` vs `## Result`).
   - `##` only avoids ambiguity when an agent writes `### Result` as a sub-heading under `## Status`.
   - Exact match avoids fuzzy matching in a system where the caller always knows the exact title it wrote.
   - Missing and duplicate sections are errors (not silent no-ops) so callers learn about drift immediately.

3. **Publishers of these commands.** Primary: agents writing structured partial results — e.g. `ReplaceSection("Result", ...)` instead of rewriting the whole body. This yields a cleaner git history and eliminates the idempotent-commit failure on unchanged result text (the diff is scoped to the section, and even an unchanged section produces the same commit result as today's full-file write — but more importantly, a section-scoped write makes "did the Result change?" obvious in `git log`). Secondary: orchestration tools / supervisors updating status sections, appending retry logs. Not: the executor, which per spec 015 operates on frontmatter only.

4. **Coexistence with `TaskResultExecutor`.** The full-body-rewrite executor stays. The new commands are opt-in. No migration is forced. Documentation recommends new agents prefer section-scoped writes, but legacy behaviour is supported indefinitely. Rationale: the two paths do not conflict (both go through the gitclient mutex), the cost of keeping both is low, and forcing migration of every existing agent would be a bigger change than the spec itself.

## Constraints

- MUST reuse spec 015's `AtomicReadModifyWriteAndCommitPush` primitive. No new locking primitive.
- MUST reuse `FindTaskFilePath`, `ExtractFrontmatter`, `ExtractBody` helpers exported by spec 015's `result` package.
- Frontmatter bytes MUST be byte-identical before and after any of the three content commands (verifiable by a diff test).
- All existing atomic-write guarantees from spec 011 and spec 015 are preserved.
- The existing `TaskResultExecutor` remains functional and unchanged.
- `cd task/controller && make precommit` MUST pass.

## Assumptions

- Task body markdown uses `## ` for primary sections, consistent with existing task templates. If a task uses only `# ` or no headings, `ReplaceSection` returns `not_found` — which is the correct behaviour.
- The gitclient mutex from spec 015 is sufficient for body-scoped writes (it already serializes file-level writes; section-scoped writes are a subset).
- Domain docs `docs/controller-design.md` and `docs/task-flow-and-failure-semantics.md` are the right homes for the new commands and the agent-guidance note. Referenced in Verification.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `ReplaceSection` on a task with no matching `## <title>` heading | Return typed error; metric `ContentCommandsTotal{operation=replace_section, outcome=not_found}` increments; no write, no commit | Caller logs and decides whether to retry with `AppendSection` or fail the task |
| `ReplaceSection` on a task with two or more matching headings | Return typed error; metric `outcome=ambiguous`; no write | Caller (or operator) de-duplicates the task body manually |
| `AppendSection` with a title that already exists | Silently appends a second section with that title | Explicit non-goal: dedup. Documented. Callers that need dedup use `ReplaceSection` first and fall back to `AppendSection` on `not_found` |
| `AppendSection` on a body that does not end with `\n` | Controller inserts a trailing newline before the new heading | Transparent to caller |
| `AppendSection` on an empty body (frontmatter-only task) | Writes `\n## <title>\n<content>\n`; body is now non-empty | Transparent to caller |
| `ReplaceSection` on an empty body | Returns `not_found` | Same as any other missing-section case |
| Concurrent `ReplaceSection` + `UpdateFrontmatter` for the same task | Both serialize through the gitclient mutex; both land in separate commits | No caller action |
| Concurrent `ReplaceSection` + `ReplaceSection` on the same task (same or different sections) | Serialize through the gitclient mutex; both land; second read sees the first write | No caller action |
| Agent writes byte-identical section content as prior cycle | Either still produces a commit (scope is smaller but semantics unchanged), or the diff is empty and we get the same "nothing to commit" benign error as today. This spec does NOT claim to fix idempotent writes at the body level — scope is structural, not content-diff. | Documented limitation; follow-up spec if operationally painful |
| Malformed frontmatter delimiter (no closing `---`) | Helper returns error from `ExtractBody`; command fails `error` outcome | Operator fixes the task file |

## Security / Abuse Cases

- **Input provenance.** The `sectionTitle` and content parameters originate from agent-controller messages (Kafka commands). Agents are already trusted to write task files; this spec does not widen trust.
- **Path traversal.** None. `taskID` is resolved via the existing `FindTaskFilePath` helper (same as spec 015); no caller-supplied paths.
- **Denial of service.** An agent could call `AppendSection` in a tight loop, growing the task file unbounded. Mitigation: same as any other agent misbehaviour — covered by retry-counter / trigger-cap semantics from spec 011 + spec 015. No new vector introduced.
- **Section-title injection.** A malicious title like `Foo\n## Bar` could break the resulting markdown structure. Mitigation: reject titles containing newlines or the `#` character at handler entry; return typed error.
- **Race with manual vault edits.** The gitclient mutex serializes controller writes but does not prevent an operator's manual edit racing a controller commit. Same limitation as every other controller write path; no new exposure.

## Acceptance Criteria

- [ ] Unit tests for `ReplaceBody`: happy path; frontmatter bytes identical before/after; trailing-newline normalization verified.
- [ ] Unit tests for `ReplaceSection`: happy path; `not_found` case; `ambiguous` (duplicate heading) case; boundary-preservation (next `## ` heading not consumed); last-section case (extends to EOF).
- [ ] Unit tests for `AppendSection`: happy path; empty-body case; body-without-trailing-newline case; duplicate-title case (confirms it appends without error).
- [ ] Concurrent test: `ReplaceSection` + `UpdateFrontmatter` on the same task serialize correctly; both results visible in final file.
- [ ] Concurrent test: two `ReplaceSection` calls on the same task serialize; final state reflects both writes in order.
- [ ] Metric `ContentCommandsTotal{operation, outcome}` registered and incremented on each outcome class (`ok`, `not_found`, `ambiguous`, `error`).
- [ ] Section-title validation rejects titles containing `\n` or `#`.
- [ ] `docs/controller-design.md` updated: atomic content commands documented alongside atomic frontmatter commands.
- [ ] `docs/task-flow-and-failure-semantics.md` updated: guidance for agents choosing between full-body write and section-level writes.
- [ ] `TaskResultExecutor` still passes its existing tests unchanged.
- [ ] `cd task/controller && make precommit` passes.

## Verification

```
cd ~/Documents/workspaces/agent/task/controller && make precommit
```

Manual check after implementation:

1. Create a test task with frontmatter and a `## Result` section.
2. Issue a `ReplaceSection("Result", "new text")` command.
3. Verify the commit diff touches only the `## Result` block; frontmatter bytes unchanged.
4. Issue an `UpdateFrontmatter({status: done})` in parallel.
5. Verify both commits land; final file has the new Result AND the new status.

## Do-Nothing Option

Keep using `TaskResultExecutor` for all body writes. Costs:

- Agents that only want to update one section keep paying the full-body-reconstruction cost.
- Idempotent body writes keep generating the same benign `nothing to commit` errors spec 015 eliminated for counters — harmless, but operationally noisy.
- Body–frontmatter races remain theoretically possible (practically rare; no known incident).

This is a reasonable do-nothing stance if no agent actually wants section-scoped writes. The spec is worth doing once two or more agents need structured partial-result updates, or when the `nothing to commit` body-write noise becomes operationally painful.

## References

- Spec 015: `specs/in-progress/015-atomic-frontmatter-increment-and-trigger-cap.md` — primitives (`AtomicReadModifyWriteAndCommitPush`), helpers (`FindTaskFilePath`, `ExtractFrontmatter`, `ExtractBody`), metric template (`FrontmatterCommandsTotal`), and the `cdb.CommandObjectExecutorTx` pattern.
- Spec 011: `specs/completed/011-retry-counter-spawn-time-semantics.md` — original atomic-write guarantees preserved here.
- Existing full-body path: `task/controller/pkg/command/task_result_executor.go` — unchanged by this spec; coexists.
