---
status: completed
summary: claude.AgentStep.Run now writes the agent's declared payload (the result envelope's `output` field) into the configured output section instead of the whole envelope, reusing the single envelope parse, with docs and tests covering the write→read→failure-detection seam
execution_id: agent-step-output-exec-219-claude-step-write-extracted-output
dark-factory-version: v0.196.0
created: "2026-09-21T17:34:32Z"
queued: "2026-09-21T17:34:32Z"
started: "2026-09-21T17:34:45Z"
completed: "2026-09-21T17:40:57Z"
---

# Write the extracted output, not the raw result envelope, into the output section

<summary>
- A phase step writes the agent's declared payload into its output section, not the whole result envelope
- Downstream phases can parse that section without unwrapping an envelope first
- The envelope keeps carrying status, message and next-phase signalling unchanged
- Runs whose result is not an envelope still write their raw text, so existing prompts keep working
- A failed or needs-input run still writes no output section, preserving current failure semantics
- The Claude step now writes an extracted payload instead of raw runner text, like the sibling step, but extracts one level deeper — the envelope's `output` field rather than the envelope itself
- Covered by a test driving a real envelope that carries a plan payload through the step
</summary>

<objective>
Make the Claude phase step write the payload the agent declares — the `output` field of its result envelope — into the configured output section, instead of writing the entire envelope. Today the planning phase emits a valid plan but the section ends up holding the envelope, so the next phase cannot parse it and the pipeline never advances. The contract already exists elsewhere in this module; this change makes the Claude step honour it.

Observed 2026-09-21 on dev, task `hypothesis-smoke-test-bbr-h5e-v9-hypo2-fix`, Job `trading-hypothesis-agent-d6bc6789-20260921162237`: the planning phase succeeded and published `{"status":"done","next_phase":"in_progress","message":"plan extracted","output":"## Plan\n\n```json\n{...}\n```\n"}`, and the section then held that whole envelope. The next phase reported `plan section invalid: json block missing in plan section`, then `plan json malformed`. Same failure class as spec 051's live-verified defect.
</objective>

<context>
Read CLAUDE.md for project conventions.

Read `pi/pi-step.go` — the sibling step implementation. It writes an extracted JSON object into the output section rather than the raw assistant text (see the comment above its `md.ReplaceSection` call). Note precisely what it extracts: `jsonBlob` is the whole result envelope, not a payload inside it. So it is a structural pattern reference for "extract, then write the extracted value" — not the target behaviour. This change extracts one level deeper than pi does, taking the envelope's `output` field.

Read `claude/agent-step.go` — find the `Run` method, the `parseAgentResultBody` helper, and the `md.ReplaceSection` call that writes the output section.

Read `claude/types.go` — find the `AgentResult` struct.

Read `agent_status.go` — find `AgentResultInfo`. Its `Output` field is documented as the body content (typically heading plus fenced JSON). That documentation is the contract this change restores.

Read `docs/agent-job-interface.md` § "Stdout (optional, debug only)" — it documents the result envelope as carrying `status`, `output`, `message` and `links`, so the `output` field is an established part of the envelope shape. Its own description of `output` ("human-readable summary") is looser than the payload contract this change implements, which comes from `AgentResultInfo.Output` and the calling prompt.

Read `docs/task-flow-and-failure-semantics.md` § "AgentStep output-section idempotency (spec 051)" — the durable description of what the output section holds and how `ShouldRun` detects a failed body. This change alters that content, so that section needs correcting too.

Read `claude/agent-step_test.go` — the `Describe("Run")` block. Note that the existing "when runner succeeds" context drives an envelope carrying no `output` field, which is why this defect is currently uncovered.

Read these coding-plugin guides before starting: `go-error-wrapping-guide.md` and `go-testing-guide.md` in the coding plugin `docs/`.
</context>

<requirements>
1. In `claude/types.go`, add an `Output` field to the `AgentResult` struct, with JSON tag `output` and `omitempty`, consistent with the sibling `Message` and `Files` fields. Document it as the payload the agent declares for the configured output section — distinct from `AgentResultInfo.Output`, which carries a fully rendered section body including its heading — and say so explicitly so the two `Output` fields are not conflated.

2. In `claude/agent-step.go`, in `Run`, make the value passed to `md.ReplaceSection` the extracted `Output` when the runner's result parses as an envelope carrying a non-empty `Output`. When the result does not parse as an envelope, or parses but carries an empty `Output`, keep writing the runner's raw result text exactly as today, so prompts that do not use the envelope are unaffected.

3. Reuse the envelope already parsed for the status check rather than parsing twice. The `parseAgentResultBody` call in `Run` already parses that same envelope to decide the needs-input / failed path; widen the scope of that parsed value so the success path can read `Output` from it. Do not add a second parse of the same text.

4. Leave the needs-input and failed path unchanged: those still return their status without writing the output section, so the deliverer writes the failure marker as it does today.

5. Check this change against `ShouldRun`'s failure detection, whose input it alters. `failureMarked` parses the output-section body with `parseAgentResultBody` and forces a re-run when that body unmarshals to a `needs_input` / `failed` status — the guard that prevents a failed run from permanently poisoning re-dispatch (spec 051). With the extracted payload now in the section instead of the envelope, confirm the payload's fenced JSON cannot carry a `status` field of `needs_input` or `failed`, which would force a re-run on every dispatch. Record the conclusion in a comment on the `md.ReplaceSection` call.

6. Update `docs/task-flow-and-failure-semantics.md` § "AgentStep output-section idempotency (spec 051)". It currently says the failure marker is "an output-section body that parses to a `needs_input`/`failed` AgentResult", which stops describing what the section holds once this lands. Correct that wording to say what the section actually contains: on success the output section holds the agent's extracted payload (the envelope's `output` value — a heading plus fenced JSON), not the envelope itself; failure-marker detection stays a best-effort parse of whatever body is present, and the `## Failure` section remains the primary marker.

7. Add a test to `claude/agent-step_test.go` inside the `Describe("Run")` block, in the success context, driving a real envelope through the step. The envelope must carry a payload matching what a planning phase actually emits: a `done` status, a `next_phase`, a `message`, and an `output` whose value is a markdown block containing a fenced JSON block, with newlines escaped as they are on the wire. Assert the output section body equals the extracted payload and is not the raw envelope. Also assert the produced section round-trips through the reader that consumes it downstream: call `ShouldRun` on the mutated `Markdown` and assert it returns `false` (the extracted payload is not a failure marker), so the write → read → failure-detection seam the defect lives on is exercised rather than only the write. This is the executable counterpart to requirement 5's analysis.

8. Add a second test in the same block pinning the fallback: a runner result that does not parse as an envelope still writes the runner's raw text into the section. The envelope-without-`output` case is already pinned by the existing "when runner succeeds" context — leave that test as it is.

9. Before you finish, re-run `<verification>` and confirm it passes; walk each requirement above against the change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must still pass, including the existing "when runner succeeds" context
- Use `github.com/bborbe/errors` for error wrapping, matching the surrounding code
- Ginkgo v2 / Gomega, external test package `claude_test`, matching the existing file
- Repo-relative paths only
- Out of scope: `claude/task-runner.go` (`taskRunner.Run`) parses the same envelope for the single-shot path but writes no body section — the deliverer renders `## Result` from status, message and files via `BuildResultSection`. Leave it unchanged, and do not add `Output` to `BuildResultSection` or `AgentResultLike` in this prompt.
</constraints>

<verification>
Run `make precommit` -- must pass.
</verification>
