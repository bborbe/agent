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
- Coexists indefinitely with the existing `TaskResultExecutor` whole-file-rewrite path. No forced migration; agents opt in.
- Eliminates "nothing to commit, working tree clean" idempotent-write failures for body updates the same way spec 015 did for frontmatter counters.

## Problem

Spec 015 addressed idempotent-commit failures and frontmatter races by introducing atomic, field-scoped command handlers. Body edits are still stuck on the old path: `TaskResultExecutor` rewrites the whole file (frontmatter + body) every cycle. This is:

- **Coarse-grained.** An agent that only wants to update its "Result" section must reconstruct the entire body.
- **Idempotent-prone.** If the agent's output is byte-identical to last cycle, the write produces no diff and `git commit` fails — the same failure mode that motivated spec 015, still present on the body path.
- **Race-prone.** A whole-body write collides with concurrent frontmatter updates. Spec 015 serialized frontmatter writes behind the gitclient mutex, but a body rewrite that includes frontmatter-parse-and-reserialize is still at risk of clobbering a concurrent frontmatter update that landed between its read and its write.

Agents that want to publish structured partial results (e.g. just the "Result" section) have no way to do so without taking the full-file-rewrite risk.

## Goal

Agents and orchestration tools can update specific body regions of a task file — the whole body, a named `##` section, or an appended section — without reading or rewriting the rest of the file. Each such update is atomic end-to-end: read → modify → write → commit → push happens under the gitclient mutex, concurrent with any frontmatter command from spec 015, with no risk of clobber and no idempotent-commit failure when the new content differs from the old.

## Non-goals

- NOT changing or deprecating any frontmatter command from spec 015.
- NOT adding a `DeleteSection` command — can follow later if demand emerges.
- NOT supporting nested-heading (`###`+) sections.
- NOT deduplicating on `AppendSection` — duplicate section titles are legal and the caller's responsibility.
- NOT migrating existing agents. `TaskResultExecutor` stays. New commands are opt-in.
- NOT adding a `SetFrontmatter(key, value)` convenience wrapper — spec 015's `UpdateFrontmatter(map)` with a single-key map is sufficient.
- NOT changing the Kafka topic schema in a breaking way. New command kinds piggyback on the existing command-envelope pattern, matching specs 011 and 015.

## Desired Behavior

1. A `ReplaceBody(taskID, newBody)` command replaces everything after the closing `---` of the frontmatter block with `newBody`. Frontmatter bytes are byte-for-byte preserved.
2. A `ReplaceSection(taskID, sectionTitle, newContent)` command locates the unique `## <sectionTitle>` line (case-sensitive, exact match) and replaces it plus everything up to (but not including) the next `## ` heading (or end-of-file) with `## <sectionTitle>\n<newContent>`.
3. A `ReplaceSection` on a task whose body has no such heading fails with a not-found error and produces no write, no commit, and no partial state change.
4. A `ReplaceSection` on a task whose body has two or more headings with identical text fails with an ambiguous error and produces no write.
5. An `AppendSection(taskID, sectionTitle, content)` command appends `\n## <sectionTitle>\n<content>\n` to the end of the body, ensuring exactly one blank line precedes the new heading regardless of whether the existing body ends with a trailing newline.
6. An `AppendSection` on a task whose body already contains a section with that title still succeeds and creates a duplicate. Deduplication is explicitly not the command's responsibility.
7. All three commands execute atomically under the gitclient mutex via spec 015's `AtomicReadModifyWriteAndCommitPush` primitive. A concurrent `UpdateFrontmatter` or `IncrementFrontmatter` command for the same task serializes correctly: both land, neither clobbers the other.
8. When a command's computed new file content differs from the old, `git commit` always produces a diff — no idempotent-commit failure.
9. When a command's computed new file content equals the old (e.g. `ReplaceSection` with identical content), the controller detects the no-op before attempting the commit and returns success without a commit.
10. Each command emits a metric tagged with its operation kind and outcome (success, not_found, ambiguous, error), distinct from the frontmatter-command metric introduced in spec 015.

## Constraints

- The gitclient mutex and the `AtomicReadModifyWriteAndCommitPush` primitive from spec 015 are reused unchanged. This spec adds callers, not infrastructure.
- The `FindTaskFilePath`, `ExtractFrontmatter`, and `ExtractBody` helpers exported by spec 015's `result` package are reused unchanged.
- Existing `TaskResultExecutor` behavior does not change. Agents that publish whole-body results continue to work bit-for-bit identically.
- Command-envelope wire format on the Kafka request topic matches the pattern established by specs 011 and 015. New `base.CommandOperation` constants are additive.
- Heading-level semantics: only `## ` (h2) lines are section boundaries. `### `+ headings are body content inside the enclosing `##` section and must be preserved byte-for-byte by `ReplaceSection` of any ancestor section and never treated as a boundary.
- Section-title matching is byte-exact, case-sensitive. Trimming whitespace, collapsing spaces, or unicode normalization are explicitly out of scope.
- Frontmatter bytes are never parsed, reserialized, or touched by any of the three commands. A `ReplaceBody` on a task with malformed frontmatter still works and preserves the malformed frontmatter verbatim.

## Design Decisions (resolved)

1. **No `SetFrontmatter(key, value)` command.** Rationale: `UpdateFrontmatter({key: value})` from spec 015 is already a single-key-capable command. Adding a convenience wrapper doubles the command-operation surface and the executor-registration surface for zero expressive gain.

2. **Section identification: exact `## <title>`, case-sensitive, `##` only.** Rationale:
   - Exact match avoids accidental collisions on partial-match rules (e.g. "Result" vs "Result Summary").
   - Case-sensitive matches Obsidian's and most markdown tooling's heading semantics.
   - `##`-only scoping means `###` subsections stay welded to their parent, which is how humans naturally read the file. Deeper-nesting support can come later if a real use case emerges.
   - Missing-section and duplicate-section both raise errors (not silent no-ops) so callers learn about drift instead of silently losing writes.

3. **Primary and secondary callers.** Rationale:
   - **Primary: agents publishing structured partial results.** An agent that wants to say "here's my new Result section" sends `ReplaceSection("Result", ...)` instead of rebuilding the full body. This yields cleaner git history (diffs scoped to the actual change) and structurally eliminates the idempotent-commit failure mode for byte-identical outputs.
   - **Secondary: orchestration tools and supervisors.** Appending retry logs, updating status sections, stamping supervisor notes — all natural fits for `AppendSection` or `ReplaceSection`.
   - **Explicitly not: the executor itself.** Per spec 015, the executor only touches frontmatter. It does not gain body-edit responsibilities here.

4. **Coexistence with `TaskResultExecutor`.** Rationale:
   - Existing agents' full-body-rewrite path keeps working. No forced migration, no deprecation timeline.
   - New agents are recommended (in `docs/task-flow-and-failure-semantics.md`) to prefer `ReplaceSection("Result", ...)` for result writes, but can use the full-body path if they have reason to.
   - The two paths coexist indefinitely. There is no transition plan because none is needed — both reach the same on-disk state through different code paths.

5. **New metric, not an extension of spec 015's.** Rationale: spec 015 introduced `FrontmatterCommandsTotal{operation, outcome}`. Body-edit commands live in a different domain. Mixing them under one metric confuses dashboards and alerting. A sibling `ContentCommandsTotal{operation, outcome}` keeps the two domains independently observable and leaves spec 015's metric shape untouched.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| `ReplaceSection` targets a heading that does not exist in the body | Command fails with not-found error; no write, no commit, no partial state; metric outcome = `not_found` | Caller inspects the task file, corrects the section title or uses `AppendSection` instead |
| `ReplaceSection` targets a heading that appears two or more times in the body | Command fails with ambiguous error; no write; metric outcome = `ambiguous` | Caller disambiguates manually (edit the file to make titles unique) or rewrites via `ReplaceBody` |
| `AppendSection` with a title that already exists | Succeeds, creates a duplicate section. Explicit non-goal: dedup | Caller uses `ReplaceSection` instead if replacement was intended |
| Body ends without a trailing newline | `AppendSection` inserts a leading newline before the new `## ` heading, producing well-formed markdown | None needed |
| Empty body (frontmatter-only task, no bytes after closing `---`) | `AppendSection` succeeds and writes `\n## <title>\n<content>\n`; `ReplaceSection` fails with not-found | Use `AppendSection` or `ReplaceBody` for the first body write |
| Concurrent `ReplaceSection` + `UpdateFrontmatter` on the same task | Both serialize behind the gitclient mutex. Both apply. No clobber, no lost write | None needed |
| Computed new file content equals old (no-op) | Controller detects the no-op before calling `git commit`; returns success; no commit created; metric outcome = `noop` | None needed — this is the designed idempotent-safe path |
| Task file not found at resolve time | Command fails with not-found error; metric outcome = `task_not_found` | Caller validates the task id before publishing |

## Security / Abuse Cases

- **Attacker-controlled section title.** If a malicious command publisher sends a crafted title containing a newline, the body could be corrupted. The command executor must reject any section title containing newline, carriage return, or a leading/embedded `#` other than the implied heading prefix.
- **Attacker-controlled content payload.** `newContent` and `content` are written verbatim. If they contain `## ` lines, they effectively inject new sections — which is allowed and documented. No trust boundary is crossed because the writer already has write permission on the vault.
- **Path traversal via task id.** Task id → file path resolution is delegated to spec 015's `FindTaskFilePath` helper, which already handles validation. No new attack surface.
- **Unbounded `AppendSection` growth.** A misbehaving agent could append thousands of sections and balloon the file. This is a rate-limiting concern, not an abuse concern — the command publisher is trusted. Out of scope here; covered by the existing spawn/trigger caps.
- **No retry-forever or hang cases.** The command is a single read-modify-write-commit-push under a timeout inherited from the gitclient. No new loops, no new unbounded waits.

## Acceptance Criteria

- [ ] Three new command kinds (`ReplaceBody`, `ReplaceSection`, `AppendSection`) registered in the command factory and reachable from the Kafka request topic via the spec-011 envelope.
- [ ] Each command's executor calls `AtomicReadModifyWriteAndCommitPush` from spec 015 for its read-modify-write phase.
- [ ] `ReplaceSection` errors cleanly (no write, no commit) on missing and duplicate section titles, with distinct metric outcomes.
- [ ] `AppendSection` correctly handles bodies with and without trailing newlines, and frontmatter-only (empty-body) files.
- [ ] `ReplaceSection` preserves the exact bytes of the next `## ` heading and everything after it — does not consume the boundary.
- [ ] `ReplaceSection` preserves `### `+ subheadings inside the target section as body content (they follow the rewrite just like paragraph text).
- [ ] Concurrent-write test: one goroutine issues `ReplaceSection`, another issues `UpdateFrontmatter`, both targeting the same task; both land, neither clobbers the other.
- [ ] No-op detection: sending `ReplaceSection` with content identical to current produces no git commit and metric outcome = `noop`.
- [ ] `ContentCommandsTotal{operation, outcome}` metric is emitted for every command invocation with correct labels.
- [ ] `TaskResultExecutor` behavior unchanged — existing tests pass without modification.
- [ ] `docs/controller-design.md` has a section describing the three new commands, the `##`-only section rule, and the error cases.
- [ ] `docs/task-flow-and-failure-semantics.md` recommends `ReplaceSection("Result", ...)` for new agents publishing partial results and documents coexistence with the full-body path.

## Verification

```
cd task/controller && make precommit
```

Expected: all tests pass including the new executor unit tests, the concurrent-write test, and the no-op-detection test. No new lint findings.

Manual smoke:

1. Publish `AppendSection("<taskID>", "Notes", "first line")` — observe single commit with the new section appended.
2. Publish the same command again — observe a second commit creating a duplicate `## Notes` section.
3. Publish `ReplaceSection("<taskID>", "Notes", "replaced")` — observe an ambiguous-error metric increment and no commit.
4. Manually edit the file to remove one of the duplicates, re-publish `ReplaceSection` — observe a single commit replacing only that section, with frontmatter and other sections bit-for-bit preserved.
5. Publish `ReplaceSection` with identical content — observe metric outcome = `noop` and no commit.

## Do-Nothing Option

Keep `TaskResultExecutor` as the only body-edit path. Agents that want partial-section writes can't have them; idempotent-commit failures on byte-identical agent output continue to surface as controller-log noise; concurrent frontmatter/body races remain a latent risk. Acceptable if the operational noise stays low and no agent emerges that needs structured partial writes. Reconsider if (a) a new agent wants to publish a "Result" section without rebuilding the whole body, or (b) the byte-identical-output idempotent-commit failures show up on the body path in production the way they did on the frontmatter path before spec 015.

## References

- Spec 015 (`specs/in-progress/015-atomic-frontmatter-increment-and-trigger-cap.md`) — reuses its `AtomicReadModifyWriteAndCommitPush` primitive, `FindTaskFilePath` / `ExtractFrontmatter` / `ExtractBody` helpers, and `cdb.CommandObjectExecutorTx` pattern.
- Spec 011 — command-envelope wire format on `agent-task-v1-request`.
- Existing full-body path: `task/controller/pkg/command/task_result_executor.go`.
- `docs/controller-design.md`, `docs/task-flow-and-failure-semantics.md` — docs to update.
