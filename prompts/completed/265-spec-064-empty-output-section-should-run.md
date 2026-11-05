---
status: completed
spec: [064-bug-empty-output-section-is-not-complete]
summary: agentStep.ShouldRun now returns true for an empty or whitespace-only output section, with four new tests, updated doc comments, and an Unreleased changelog entry; make precommit passed.
execution_id: 6400dec7-3edd-5452-847b-b0bc3e6d319a-exec-265-spec-064-empty-output-section-should-run
dark-factory-version: dev
created: "2026-11-05T17:40:43Z"
queued: "2026-11-05T17:57:22Z"
started: "2026-11-05T17:57:24Z"
completed: "2026-11-05T18:16:17Z"
branch: dark-factory/bug-empty-output-section-is-not-complete
---

# fix(claude): an empty output section is not completed work

<summary>
- A task whose body already carries a bare output heading (e.g. `## Result` with nothing under it) is now re-dispatched instead of skipped
- A section whose body is only spaces, tabs or newlines counts as empty too, so a formatting quirk cannot poison a task
- A section that already carries real content still skips, exactly as before
- The `## Failure` marker still forces a re-run, exactly as before
- A missing output section still forces a run, exactly as before
- A dispatch that used to fail in tens of milliseconds with `agent run returned no result` now reaches claude and writes a result
- No new logging, no new exported API, and no change to how the runner or the deliverer work
- New tests cover empty, whitespace-only and non-empty bodies, plus a document round-trip that reproduces the reported shape
- A changelog entry records the behaviour change
</summary>

<objective>
Make `agentStep.ShouldRun` return `true` when the configured output section exists but its body is empty or whitespace-only, so re-dispatch re-invokes the step instead of treating the bare heading as completed work. A section with content keeps today's skip, and the `## Failure` marker keeps forcing a re-run (spec 051).
</objective>

<context>
Read `CLAUDE.md` for project conventions and `docs/dod.md` for the repo's definition of done (it requires a `## Unreleased` CHANGELOG entry).

Read these files fully before editing:

- `claude/agent-step.go` — the whole file. The change is in `agentStep.ShouldRun` (around lines 87-96) and the doc comments immediately above `ShouldRun` and `failureMarked`. `failureMarked`, `parseAgentResultBody` and `Run` are not changed.
- `claude/agent-step_test.go` — the whole `Describe("ShouldRun", ...)` block (around lines 66-210). Every existing `It` in it must stay green and unmodified; the new rows go inside the existing `Context("when section already exists", ...)`.
- `agent_markdown.go` — `FindSection` returns the parsed `Section` with `Body` as the text between this heading and the next; `splitMarkdownSections` trims only one trailing newline, so a heading followed by blank lines can yield a body of `"\n"` or `"   "`. That is why the check is `strings.TrimSpace(...) == ""`, not `== ""`.
- `CHANGELOG.md` — the top heading is `## v0.139.4`; there is no `## Unreleased` section today.

Coding guidelines (these live in the YOLO container at the paths shown, not in this repo):

- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega style, external test package (`package claude_test`).
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — `## Unreleased` entry format and prefix rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — changed-behaviour coverage.

## Why the change is needed

`ShouldRun` currently returns `false` for any existing output section unless `failureMarked` is true. `failureMarked` recognises a `## Failure` section or a body that best-effort-parses to a `needs_input`/`failed` `AgentResult`. An empty body is neither: `parseAgentResultBody("")` returns `ok == false`, so the step is skipped, `Run` never calls claude, the caller gets no result, and the Job fails with `agent run returned no result`. Spec 051 fixed the neighbouring case (a failed run leaving a populated section) but deliberately left an empty body on the skip side. An empty section is the absence of completion, not evidence of it.

## Open question for the human reviewer (do not act on it in this prompt)

`docs/task-flow-and-failure-semantics.md` line 265 enumerates the reasons re-dispatch re-runs a step (failure marker, missing section) and predates this fix. Updating it would keep the authoritative doc in step with the code, but the spec's Suggested Decomposition scopes this change to `claude/agent-step.go` plus its test. It is left untouched here; consider a one-sentence follow-up if the doc is treated as normative.
</context>

<requirements>
1. **Add the failing tests first, before changing the implementation.** In `claude/agent-step_test.go`, inside the existing `Context("when section already exists", func() { ... })` (starts around line 90), insert these four `It` rows immediately after the existing `It("success body → false", ...)` block (after its closing `})`, before the Context's closing `})`). Do not move, rename or edit any existing row.

   ```go
   		It("empty body → returns true", func() {
   			md := &lib.Markdown{
   				Sections: []lib.Section{
   					{Heading: "## Analysis", Body: ""},
   				},
   			}
   			shouldRun, err := agentStep.ShouldRun(ctx, md)
   			Expect(err).NotTo(HaveOccurred())
   			Expect(shouldRun).To(BeTrue())
   		})

   		It("whitespace-only body → returns true", func() {
   			md := &lib.Markdown{
   				Sections: []lib.Section{
   					{Heading: "## Analysis", Body: "  \n\t\n"},
   				},
   			}
   			shouldRun, err := agentStep.ShouldRun(ctx, md)
   			Expect(err).NotTo(HaveOccurred())
   			Expect(shouldRun).To(BeTrue())
   		})

   		It("non-empty prose body → returns false", func() {
   			md := &lib.Markdown{
   				Sections: []lib.Section{
   					{Heading: "## Analysis", Body: "already done"},
   				},
   			}
   			shouldRun, err := agentStep.ShouldRun(ctx, md)
   			Expect(err).NotTo(HaveOccurred())
   			Expect(shouldRun).To(BeFalse())
   		})

   		It("parsed bare heading with no body → returns true", func() {
   			// The reported shape: a task body that ends with a bare heading.
   			// Go through ParseMarkdown so the assertion covers the parser
   			// boundary the bug actually crossed, not just a hand-built struct.
   			md, err := lib.ParseMarkdown(ctx, "Question: why?\n\n## Analysis\n")
   			Expect(err).NotTo(HaveOccurred())
   			shouldRun, err := agentStep.ShouldRun(ctx, md)
   			Expect(err).NotTo(HaveOccurred())
   			Expect(shouldRun).To(BeTrue())
   		})
   ```

   `lib` is already imported as `lib "github.com/bborbe/agent"` and `ParseMarkdown` is already used elsewhere in this test file, so no import change is needed.

2. **Confirm the new tests fail against the current code.** Run `go test ./claude/ -run TestClaude -count=1` BEFORE applying requirement 3. The three `returns true` rows must fail with Gomega's `Expected <bool>: false to be true`; the `non-empty prose body` row must already pass. Record that in your final report. Do not proceed to requirement 3 until you have seen this failure — it is what makes the regression evidence non-vacuous.

3. **Fix `agentStep.ShouldRun` in `claude/agent-step.go`.** The current function is:

   ```go
   func (s *agentStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
   	_, exists := md.FindSection(s.cfg.OutputSection)
   	if !exists {
   		return true, nil
   	}
   	if s.failureMarked(md) {
   		return true, nil
   	}
   	return false, nil
   }
   ```

   Replace it with:

   ```go
   func (s *agentStep) ShouldRun(_ context.Context, md *agentlib.Markdown) (bool, error) {
   	section, exists := md.FindSection(s.cfg.OutputSection)
   	if !exists {
   		return true, nil
   	}
   	if s.failureMarked(md) {
   		return true, nil
   	}
   	if strings.TrimSpace(section.Body) == "" {
   		return true, nil
   	}
   	return false, nil
   }
   ```

   `strings` is already imported in this file. The `## Failure` check runs first and is unchanged, so a failure marker still wins regardless of the output section's body.

4. **Update the two doc comments so the prose matches the code.** Do not change any other comment in the file.

   a. Replace the whole doc comment above `ShouldRun` (currently the block that begins `// ShouldRun returns false only when ...` and ends `// is not completed work, and re-dispatch must re-invoke claude (spec 051).`) with:

   ```go
   // ShouldRun returns false only when the output section exists and represents a
   // genuine success: a non-empty body that is not a needs_input/failed
   // AgentResult, with no ## Failure section present.
   //
   // Single-step idempotency check: if the LLM already wrote its section in a
   // prior Job that crashed before phase advance, skip the re-invocation.
   // (For multi-step phases, decompose the work — don't rely on a single
   // AgentStep to be partially-resumable.)
   //
   // A failure marker — a ## Failure section, or an output-section body that
   // parses to a needs_input/failed AgentResult — forces a re-run: a failed run
   // is not completed work, and re-dispatch must re-invoke claude (spec 051).
   //
   // An empty or whitespace-only body is likewise not completed work: the
   // heading may be a pre-seeded template or the residue of a partial edit, so
   // skipping it would leave the task permanently undispatchable — the dispatch
   // returns no result and the Job fails without ever invoking claude (spec 064).
   ```

   b. In the `failureMarked` doc comment, replace only its final sentence — `// Bodies that are not a failure marker (done JSON, unparseable prose, unknown` / `// status) are treated as a genuine success section (spec 051).` — with:

   ```go
   // Bodies that are not a failure marker (done JSON, unparseable prose, unknown
   // status) are not failures (spec 051). An empty body is not a failure marker
   // either; ShouldRun treats it as "not completed" separately (spec 064).
   ```

5. **Add a `## Unreleased` section to `CHANGELOG.md`.** The file's top version heading is `## v0.139.4`; insert directly above it, with a blank line after the new heading and before the bullet:

   ```markdown
   ## Unreleased

   - fix(claude): an empty or whitespace-only output section is no longer treated as completed work, so a task whose body carries a bare `## Result` heading is re-dispatched and claude runs instead of the Job failing with `agent run returned no result`; a section with content still skips, preserving spec 051's single-step idempotency

   ```

   Leave every historical entry untouched.

6. **Re-run the tests after the fix.** `go test ./claude/ -run TestClaude -count=1` must be green, including every pre-existing `ShouldRun` row and the four new rows. Do not weaken an existing assertion to make the suite pass.

7. **Self-check before finishing.** Confirm the only behavioural change is the one `if strings.TrimSpace(section.Body) == ""` branch in `ShouldRun`; `failureMarked`, `parseAgentResultBody`, `Run`, the `Step` interface and `StepRunner` are byte-for-byte unchanged. Confirm no new log line was added on the empty-body path. Confirm no new exported symbol exists.
</requirements>

<constraints>
- **Spec 051's rows must stay green and unmodified.** The failure-marker behaviour and the non-empty-prose skip are contracts other agents depend on. Do not rename or edit `It("returns false")`, `It("success body → false")`, the `Context("when a ## Failure section is present")` row, or the prose / unknown-status / invalid-JSON rows.
- **`parseAgentResultBody` is unchanged.** The empty check is a separate condition in `ShouldRun`, not a change to JSON parsing.
- **No new exported API.** The change is internal to `claude/agent-step.go`.
- **The `Step` interface and `StepRunner` are untouched.** Do not change `agent_agent.go`, `agent_runner.go`, or any executor code.
- **No new log line on the empty-body path.** The re-run is already visible through the existing `spawning claude CLI` marker; adding logging here would be noise.
- **Do NOT change what counts as a success body.** A non-empty body that is not a failure marker still skips. A fenced code block that renders empty still has text, so it is non-empty and still skips (spec Failure Modes).
- **Whitespace-only bodies re-run.** A body of only newlines or spaces counts as empty because the check is after `strings.TrimSpace` (spec Desired Behavior 5).
- **Do NOT commit** — dark-factory handles git.
- **Scope:** `claude/agent-step.go`, `claude/agent-step_test.go`, `CHANGELOG.md`. Nothing else.
- **Do not attempt the post-deploy reproduction or the `agent-claude` image release in this prompt.** Releasing `bborbe/agent`, bumping the `bborbe/agent-claude` pin, publishing the image and deploying it to the `service-analysis-agent` Config are operator-executable steps that run after merge; they are outside the container's reach and outside this prompt's scope.
</constraints>

<verification>
ROOTDIR=/workspace make precommit
go test ./claude/ -count=1 -v
test "$(go test ./claude/ -run TestClaude -v 2>&1 | grep -c 'empty body → returns true')" = "1"
</verification>
