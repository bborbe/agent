---
status: completed
summary: AgentStep now strips a leading markdown section heading from the extracted payload before writing it as the output section body, so marshalling no longer duplicates the heading and the next phase can reach the fenced JSON; covered by round-trip tests that fail against the unfixed code.
execution_id: agent-strip-heading-exec-220-claude-step-strip-payload-heading
dark-factory-version: v0.196.0
created: "2026-09-21T21:05:19Z"
queued: "2026-09-21T21:05:19Z"
started: "2026-09-21T21:05:31Z"
completed: "2026-09-21T21:11:03Z"
---

# Strip a payload's own heading before writing it as the output section body

<summary>
- A phase step no longer emits the section heading twice when the agent's payload already carries one
- The section on disk stays parseable by the next phase
- A payload that does not begin with the heading is written unchanged
- The non-envelope fallback path is untouched
- A test now marshals the task document and parses it back, so a serialisation-only defect cannot pass
- The new test uses a payload that begins with the section heading, matching what the real prompt emits
</summary>

<objective>
Make the Claude phase step stop writing the output section's heading twice when the agent's declared payload already begins with it. `AgentStep.Run` writes the payload into the section body, but marshalling emits the section's `Heading` followed by its `Body` — so a payload whose first line is the heading produces the heading twice, the next phase bounds the section at the second one, finds no fenced block, and fails with `json block missing in plan section`. Observed on dev 2026-09-21, Job `trading-hypothesis-agent-b9111443-20260921204337`.
</objective>

<context>
Read CLAUDE.md for project conventions.

Read these coding-plugin guides before starting: `go-error-wrapping-guide.md` and `go-testing-guide.md` in the coding plugin `docs/`.

Read `claude/agent-step.go` — find the `Run` method and the `body := result.Result` / `if parsedOK && parsed.Output != ""` block that selects the body, immediately above the `md.ReplaceSection` call.

Read `agent_markdown.go` (repo root) — find `Markdown.ReplaceSection` and the marshalling that emits each section's `Heading` followed by its `Body`. This is why a body beginning with the heading duplicates it.

Read `claude/types.go` — find `AgentResult.Output` (the field `Run` actually reads via `parsed.Output`), documented as the payload the agent declares for the configured output section, typically a heading plus a fenced JSON block. That documentation is the contract: the payload legitimately carries its own heading. Do not cite `AgentResultInfo.Output` in `agent_status.go` — that one is a rendered section body from `BuildResultSection` on the single-shot task-runner path (its comment there reads only `// body content (typically heading + fenced JSON)`), while this one is the agent's input. The "must not be conflated" note lives on `AgentResult.Output` in `claude/types.go`.

Read `docs/task-flow-and-failure-semantics.md` § "AgentStep output-section idempotency (spec 051)" — the documented contract for what the output section holds on success.

Read `claude/agent-step_test.go` — the `Describe("Run")` block, context "when the runner returns an envelope carrying an output payload". Note its assertion compares `section.Body` to the payload, i.e. it inspects the in-memory struct and never marshals. That is why the defect is uncovered.
</context>

<requirements>
1. In `claude/agent-step.go`, in `Run`, before writing the body, strip a leading markdown section heading from the extracted payload. The payload is written as a section body, so any leading level-1/level-2 heading (`# ` or `## ` — the same boundary rule `agent_markdown.go`'s `isMarkdownSectionHeading` applies) starts a new section when marshalled and truncates the output section to an empty body, whether or not that heading equals `s.cfg.OutputSection`. Compare the first line with surrounding whitespace trimmed; when it is such a heading, drop that first line and any immediately following blank line, so the body begins with the payload's remaining content (typically a fenced JSON block). A leading `### ` or deeper sub-heading does not start a section and must be kept.

2. Leave the body unchanged when the payload does not begin with a markdown section heading and on the non-envelope fallback path (`body = result.Result`). Only the extracted-payload case is affected. The extracted-payload path is already guarded by `parsed.Output != ""`, so an empty payload cannot reach the strip site — do not add a test case for it.

3. Document the reason at the strip site: marshalling emits `Heading` then `Body`, so a payload supplying its own heading would otherwise be written twice and the section would bound to an empty region.

4. Add a test to `claude/agent-step_test.go` inside the `Describe("Run")` block, in the success context, that exercises the **round trip**, not just the write. Drive an envelope whose `output` begins with a markdown section heading, run the step, then marshal the resulting `Markdown` and assert on the serialised text that exactly one occurrence of the section heading is present (`strings.Count(marshalled, step.OutputSection) == 1`; add the `strings` import). The enclosing context's `BeforeEach` returns a payload beginning with `## Plan` while `OutputSection` is `## Analysis`; override the mock in the new test so the payload's first line equals `OutputSection`, otherwise the heading-appears-once assertion also passes against the unfixed code and the test proves nothing.

5. In the same test, parse the marshalled document back with the in-repo section-bounding reader and assert the section body still contains the fenced ```` ```json ```` block. Use `lib.ParseMarkdown(ctx, marshalled)` into a new variable (e.g. `roundTripped`) then `roundTripped.FindSection(step.OutputSection)` — do not reuse the name `md`, which already holds the pre-marshal document in the same `It`. The `lib` package is already imported in the test file as `lib "github.com/bborbe/agent"`. Assert the section exists and its `Body` contains ```` ```json ````. `ExtractPlan` lives in a different repository and must not be imported or searched for. `ParseMarkdown` + `FindSection` are the repo's section-bounding reader and bound the section the same way the next phase's reader must; `ExtractPlan` is a separate implementation in another repository, so this is the closest available proxy rather than a proof of that reader's behaviour. Do not substitute a substring count on the marshalled string — that asserts the shape of the text, not that the reader can reach the fence.

6. Update the existing context at `claude/agent-step_test.go:274-315` ("when the runner returns an envelope carrying an output payload"): its payload's first line is `## Plan`, which is now stripped, so the assertion at line 302 (`Expect(section.Body).To(Equal(planPayload))`) must be changed to assert the stripped body (the fenced JSON block with no leading heading). Add the same marshal → `ParseMarkdown` → `FindSection` round trip as requirements 4-5 to this context: before the fix, `## Analysis` bounds to an empty body here too, which is why this fixture hid the defect. Keep the non-envelope fallback test unchanged.

7. Update `docs/task-flow-and-failure-semantics.md` § "AgentStep output-section idempotency (spec 051)" (~line 266): the output section now holds the payload's content with a leading heading stripped, not "a heading plus fenced JSON". In the same section, record the `ShouldRun` coupling this changes: with the duplicate heading gone the section no longer bounds to empty, so `failureMarked`'s `parseAgentResultBody` now parses a non-empty body on re-dispatch. The outcome is unchanged — the payload's JSON carries no `status`, so it is still not a failure marker and the step still skips — but the parse path is now live rather than vacuous.

8. Add a `## Unreleased` entry to `CHANGELOG.md` describing the output-section heading-strip fix.

9. Before you finish, re-run `<verification>` and confirm it passes; walk each requirement above against the change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Use `github.com/bborbe/errors` for error wrapping, matching the surrounding code
- Ginkgo v2 / Gomega, external test package `claude_test`, matching the existing file
- Repo-relative paths only
- Out of scope: `claude/task-runner.go` (writes no body section) and `pi/pi-step.go` (does write one at `md.ReplaceSection`, but its body is `extractLastJSONObject` output — fenced or bare JSON, never a heading — so it cannot duplicate one). Also out of scope: `agent_parser.go` `ParseStep.Run` — it writes via `MarshalSectionTyped`, whose heading comes from the constructor and whose body is generated fenced JSON, so it has no agent-supplied heading to duplicate. None of these can produce the duplicate-heading defect; do not change any of them.
</constraints>

<verification>
Run `make precommit` -- must pass.
</verification>
