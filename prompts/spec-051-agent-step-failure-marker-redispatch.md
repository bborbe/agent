---
status: draft
spec: [051-bug-agent-step-shouldrun-idempotency-skip-poisons-redispatch]
created: "2026-09-06T15:31:45Z"
branch: dark-factory/bug-agent-step-shouldrun-idempotency-skip-poisons-redispatch
---

<summary>
- REVIEWER NOTE (generation-time state): the working tree this prompt was generated against ALREADY contains this fix — shipped as v0.84.1 via completed prompt `213-spec-051-agent-step-failure-marker-redispatch.md` (the `failureSectionHeading` constant, rewritten `ShouldRun`, failure-status branch in `Run`, all new Ginkgo rows, the `docs/task-flow-and-failure-semantics.md` section, and the CHANGELOG entry are all present; the spec-051 changelog bullet sits under `## v0.84.1`). Confirm intent before approving: if this is an intentional re-generation (rollback pending), run normally; if the tree is already green, this prompt executes as a verified no-op and must still pass `make precommit`. The operator-side AC6 (prod re-dispatch smoke) is NOT a prompt — it is post-deploy.
- Fixes a silent permanent outage: a failed `AgentStep` run (e.g. the sentry-collector daily fan-out hitting `needs_input`) leaves its output section behind, and the old `ShouldRun` treated any existing section as completed work, so every re-dispatch skipped the step forever with no error, no log, no escalation
- `ShouldRun` now re-runs a step whenever the task carries a failure marker — either a `## Failure` section, or an output-section body that parses to a `needs_input`/`failed` AgentResult
- `ShouldRun` still skips only when the output section represents a genuine success: a body that parses to `status: done`, or an unparseable prose body, with no `## Failure` section — so the single-step idempotency guard is preserved and prose agents (trade-analysis, pr-reviewer) are unchanged
- `Run` no longer writes a success-looking output section for a `needs_input`/`failed` runner body — it returns that status instead, and the existing deliverer writes the repo-wide `## Failure` marker as today
- The `## Failure` marker is pinned to the existing repo convention via a new package constant in `claude/` — no new section-name vocabulary
- A failed run's re-dispatch now re-invokes claude and overwrites the section on success, ending the `trigger_count` churn / `deadline_exceeded` poison loop described in the spec
- Covers all spec failure modes and acceptance criteria with new automated tests, an updated semantics doc, and a changelog entry
- No change to the `Step` interface, `StepRunner`, `agent-task-executor`, the collector prompt, or `scripts/sentry-create-tasks.sh`; the executor spawn cap / `deadline_exceeded` mapping stay out of scope
</summary>

<objective>
Make a failed `AgentStep` run unable to poison re-dispatch: `ShouldRun` must skip only on a genuine success section and re-run when a failure marker is present, and `Run` must never leave a success-looking output section for a `needs_input`/`failed` runner body. The fix lands entirely inside `claude/agent-step.go` (the lib all `NewAgentStep` consumers use) plus its tests, the durable semantics doc, and the CHANGELOG. This is a single-layer change per the spec's Suggested Decomposition — one prompt, no split.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single-module repo; `make precommit` / `make test` run from `/workspace`, never from `/workspace/claude`).

Coding-plugin docs (read before editing):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suites, counterfeiter mocks, external test packages (`*_test`), coverage rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors.Wrapf(ctx, err, ...)`; never `fmt.Errorf`, never bare `return err`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments for the new helpers.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — `## Unreleased` entry format (`- <prefix>: <what>`), prefix `fix:` for a bug fix, "if `## Unreleased` already exists, append to it (don't replace)".

Files to read IN FULL before editing (paths relative to repo root):
- `claude/agent-step.go` — the only production file this prompt changes. `AgentStepConfig`, `NewAgentStep`, `agentStep`, `ShouldRun`, `Run`.
- `claude/agent-step_test.go` — the only test file this prompt changes. The `Describe("ShouldRun")` and `Describe("Run")` blocks whose fixtures and rows are modified/added.
- `claude/claude-result.go` — `ClaudeResult` (the runner return value `Run` inspects).
- `claude/task-runner.go` — `extractLastJSONObject(s string) (string, bool)` (same package, unexported — callable from `agent-step.go`) and the prose-wrapping tolerance it provides.
- `claude/types.go` — `AgentResult` struct (`Status AgentStatus` with `json:"status"`, `Message string` with `json:"message,omitempty"`), and the local `AgentStatus`/`AgentStatusNeedsInput`/`AgentStatusFailed` aliases.
- `agent_markdown.go` — `Markdown.FindSection(heading string) (*Section, bool)` (exact-heading match) and `ReplaceSection(section Section)` (idempotent replace-or-append).
- `agent_status.go` — `AgentStatusDone = "done"`, `AgentStatusFailed = "failed"`, `AgentStatusNeedsInput = "needs_input"` in the root `lib` package.
- `agent_step.go` — the `Step` interface (`ShouldRun(ctx context.Context, md *Markdown) (bool, error)`, `Run(ctx context.Context, md *Markdown) (*Result, error)`; "Guards must be cheap — no expensive I/O") and the `Result` struct. Do NOT change this interface.
- `docs/task-flow-and-failure-semantics.md` — the durable doc that must gain the changed idempotency semantic; the `## Failure` convention is already described (line ~116: "renders `## Result` block (no `## Failure` since needs_input is not a crash)"). The new section goes immediately before the `## References` heading.
- `delivery/content-generator.go` — NOT to be edited, but read to confirm the repo `## Failure` convention (on `AgentStatusFailed`/`AgentStatusNeedsInput` the deliverer splices a `## Failure` section into the published task body via `ReplaceOrAppendSection(updated, "## Failure", ...)`). This is the marker `ShouldRun` keys on.
- `CHANGELOG.md` — read the top sections to find where to add the `fix:` bullet (see requirement 8; the spec-051 entry may already exist under a released version — do not duplicate it).

Pre-existing primitives to pin behavior to (do NOT invent new ones):
- `extractLastJSONObject(s string) (string, bool)` in `claude/task-runner.go` — extracts the last balanced top-level JSON object, tolerant of narrative prose. Reuse it for best-effort body parsing.
- `md.FindSection(heading string) (*Section, bool)` in `agent_markdown.go` — exact-heading section lookup; the heading strings must be exactly `s.cfg.OutputSection` and the `failureSectionHeading` constant.
- `md.ReplaceSection(section Section)` — idempotent replace-or-append, already the write path in `Run`.
- The mock `libmocks.ClaudeRunner` (`mocks/claude-claude-runner.go`, import path `github.com/bborbe/agent/mocks`) with `RunReturns(result1 *claude.ClaudeResult, result2 error)` — already used by `agent-step_test.go` (which is `package claude_test`). Reuse it; do not hand-write mocks.

Failure-marker semantics (from the spec — resolve before writing code):
- A failure marker is: (a) a `## Failure` section present anywhere in the document, OR (b) the output-section body parsing to an `AgentResult` whose `Status` is `AgentStatusNeedsInput` or `AgentStatusFailed`.
- Any other case — output section absent, body parses to `done`, body unparseable prose, or body parsing to an unknown/other status — is NOT a failure marker.
- The new package constant `failureSectionHeading = "## Failure"` pins the heading string once so `ShouldRun`'s detection cannot drift from the repo convention in `delivery/content-generator.go`. It is unexported (only used inside the `claude` package); the `delivery/` package keeps its own literals — refactoring `delivery/` is out of scope.
</context>

<requirements>

## 1. Add the `## Failure` heading constant in `claude/agent-step.go`

Add immediately after the `type agentStep struct { cfg AgentStepConfig }` declaration (before `Name`):

```go
// failureSectionHeading is the repo-wide failure-marker heading written by
// delivery/content-generator.go (and agent-task-executor's result publisher)
// on AgentStatusFailed / AgentStatusNeedsInput. Its presence means the prior
// run failed, so ShouldRun forces a re-run instead of treating the output
// section as completed work (spec 051).
const failureSectionHeading = "## Failure"
```

Do NOT change `AgentStepConfig`, `NewAgentStep`, or the `agentStep` struct shape.

## 2. Rewrite `ShouldRun` and add the failure-marker helpers

Replace the current `ShouldRun` method body with:

```go
// ShouldRun returns false only when the output section exists and represents a
// genuine success: a body that is not a needs_input/failed AgentResult, with no
// ## Failure section present.
//
// Single-step idempotency check: if the LLM already wrote its section in a
// prior Job that crashed before phase advance, skip the re-invocation.
// (For multi-step phases, decompose the work — don't rely on a single
// AgentStep to be partially-resumable.)
//
// A failure marker — a ## Failure section, or an output-section body that
// parses to a needs_input/failed AgentResult — forces a re-run: a failed run
// is not completed work, and re-dispatch must re-invoke claude (spec 051).
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

// failureMarked reports whether the task carries a failure marker that must
// force a re-run despite an existing output section: a ## Failure section, or
// an output-section body that parses to a needs_input/failed AgentResult.
// Bodies that are not a failure marker (done JSON, unparseable prose, unknown
// status) are treated as a genuine success section (spec 051).
func (s *agentStep) failureMarked(md *agentlib.Markdown) bool {
	if _, exists := md.FindSection(failureSectionHeading); exists {
		return true
	}
	section, exists := md.FindSection(s.cfg.OutputSection)
	if !exists {
		return false
	}
	result, ok := parseAgentResultBody(section.Body)
	if !ok {
		return false
	}
	return result.Status == agentlib.AgentStatusNeedsInput ||
		result.Status == agentlib.AgentStatusFailed
}

// parseAgentResultBody extracts the last balanced JSON object from body and
// unmarshals it as an AgentResult. ok is false when no JSON object is present
// or unmarshal fails — callers treat an unparseable body as "not a failure
// marker" (best-effort parsing, spec 051). Unknown status values are not a
// failure marker and fall through to the success/skip path.
func parseAgentResultBody(body string) (AgentResult, bool) {
	blob, ok := extractLastJSONObject(body)
	if !ok {
		return AgentResult{}, false
	}
	var result AgentResult
	if err := json.Unmarshal([]byte(blob), &result); err != nil {
		return AgentResult{}, false
	}
	return result, true
}
```

Behavior contract (this is the spec's Desired Behavior — do not deviate):

- Output section absent → `true` (unchanged).
- Output section present + `## Failure` section present anywhere → `true` (spec DB3).
- Output section present + body parses to `needs_input` → `true` (spec DB2).
- Output section present + body parses to `failed` → `true`.
- Output section present + body parses to `done`, OR unparseable prose, OR parses to an unknown status (e.g. `in_progress`) → `false` (spec DB1, DB6, DB7 — prose agents keep today's idempotency).
- Must remain cheap: in-memory markdown inspection + one bounded JSON scan of the output-section body only — no I/O (the `Step.ShouldRun` interface doc says "Guards must be cheap").

`extractLastJSONObject` is the existing same-package primitive in `claude/task-runner.go` — call it directly; do NOT reimplement JSON-object scanning. An unparseable/prose body with no JSON object returns `ok == false` and falls through to skip, exactly as the spec's "Parsing the output-section body as AgentResult is best-effort: unparseable bodies fall through to the success/skip path" constraint requires.

## 3. Modify `Run` to propagate `needs_input`/`failed` runner bodies without writing the output section

Keep the method signature `func (s *agentStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error)` and keep the runner-error branch byte-for-byte unchanged (spec AC3 — runner error returns `AgentStatusFailed`, no section written; that is today's behavior).

Update `Run`'s doc comment ("writes the LLM's output under the configured section heading") to note the new failure path: on a `needs_input`/`failed` runner body the step returns that status WITHOUT writing the output section (the deliverer writes `## Failure`); the output section is written only for genuine success. One to two sentences, per go-doc-best-practices.

Insert a new failure-status branch immediately after the `glog.Infof(... "claude runner returned %d bytes in %s" ...)` block and BEFORE the `md.ReplaceSection(agentlib.Section{...})` block:

```go
	// A needs_input/failed body is a failed run, not completed work — return
	// that status and let the deliverer write the ## Failure marker. Never
	// write a success-looking output section for a failed run: that section
	// would make ShouldRun skip every subsequent re-dispatch (spec 051).
	if parsed, ok := parseAgentResultBody(result.Result); ok &&
		(parsed.Status == agentlib.AgentStatusNeedsInput ||
			parsed.Status == agentlib.AgentStatusFailed) {
		msg := parsed.Message
		if msg == "" {
			msg = fmt.Sprintf("%s claude run returned status %s", s.cfg.Name, parsed.Status)
		}
		return &agentlib.Result{
			Status:  parsed.Status,
			Message: msg,
		}, nil
	}

	md.ReplaceSection(agentlib.Section{
		Heading: s.cfg.OutputSection,
		Body:    result.Result,
	})

	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: s.cfg.NextPhase,
	}, nil
```

The final `return &agentlib.Result{Status: agentlib.AgentStatusDone, ...}` block stays exactly as today.

Behavior contract (spec Desired Behavior 4-5, AC4):

- Runner error → `AgentStatusFailed`, no section write (unchanged).
- Runner success with a body that parses to `needs_input` → return `&agentlib.Result{Status: AgentStatusNeedsInput, Message: <parsed message or fallback>}` and do NOT write the output section.
- Runner success with a body that parses to `failed` → same, with `AgentStatusFailed`.
- Runner success with a `done` body, a prose body, or an unknown-status body → write the output section and return `AgentStatusDone` (unchanged — prose agents are unaffected).
- The returned `parsed.Status` is of type `agentlib.AgentStatus` (via the `claude.AgentStatus` alias) and is directly assignable to `Result.Status`; `parsed.Message` is the `AgentResult.Message` field. Do not construct the fallback message with `fmt.Errorf` — this is a value, not an error, so `fmt.Sprintf` is correct.

## 4. Imports in `claude/agent-step.go`

Add `"encoding/json"` to the import block (needed by `parseAgentResultBody`'s `json.Unmarshal`). The existing imports `context`, `fmt`, `time`, `github.com/bborbe/errors`, `github.com/golang/glog`, `agentlib "github.com/bborbe/agent"` all stay. Run `goimports` via the Makefile rather than hand-editing — `make precommit` runs the reviser and will reconcile the block; verify after step 8 that no import is unused.

## 5. Update `Describe("ShouldRun")` in `claude/agent-step_test.go`

The file is an external test package (`package claude_test`) importing `lib "github.com/bborbe/agent"` and `"github.com/bborbe/agent/claude"` — keep that.

5a. **Update the existing "when section already exists" context** to a genuine success body, keeping the existing row name `returns false` and the `BeFalse()` assertion, and add AC2's named row `success body → false` as a second `It` in the same context:

```go
Context("when section already exists", func() {
    It("returns false", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"done","message":"analysis complete"}`},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeFalse())
    })

    It("success body → false", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"done","message":"analysis complete","next_phase":"done"}`},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeFalse())
    })
})
```

(Spec AC2 evidence: "existing Ginkgo row 'when section already exists → returns false' stays green (fixture updated to a done-body body while keeping the same row name); new row 'success body → false'". The two rows use two different done-body fixtures so they are distinct specs; both must return `false`.)

5b. **Add the following new `Context` blocks** immediately after the "when section already exists" context, still inside `Describe("ShouldRun")` (the shared `BeforeEach` provides `step` with `OutputSection: "## Analysis"` and `agentStep`):

```go
Context("when a ## Failure section is present", func() {
    It("returns true", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"done","message":"analysis complete"}`},
                {Heading: "## Failure", Body: "- **Reason:** job failed"},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeTrue())
    })
})

Context("when the output section body is a needs_input AgentResult", func() {
    It("returns true", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"needs_input","message":"permission denied"}`},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeTrue())
    })
})

Context("when the output section body is a failed AgentResult", func() {
    It("returns true", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"failed","message":"claude CLI crashed"}`},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeTrue())
    })
})

Context("when the output section body is unparseable prose", func() {
    It("returns false", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: "already done"},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeFalse())
    })
})

Context("when the output section body has an unknown status", func() {
    It("returns false", func() {
        md := &lib.Markdown{
            Sections: []lib.Section{
                {Heading: "## Analysis", Body: `{"status":"in_progress","message":"working"}`},
            },
        }
        shouldRun, err := agentStep.ShouldRun(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(shouldRun).To(BeFalse())
    })
})
```

The `## Failure`-present row deliberately pairs a genuine success body in `## Analysis` with a `## Failure` section — this proves the failure marker overrides an otherwise-skippable success section (spec DB3). The needs_input row is spec AC1's first named row; the `## Failure` row is AC1's second. The prose row is spec DB7/DB1 backward-compat; the unknown-status row is spec DB6.

## 6. Update `Describe("Run")` in `claude/agent-step_test.go`

6a. **Keep the existing "when runner returns error" context unchanged** — spec AC3 requires the existing row "when runner returns error → returns Result with Failed status" to pass unedited.

6b. **Add a new row inside that same error context** (the no-section-write assertion must be a new row, NOT an edit to the existing one — spec AC3 evidence):

```go
It("does not write the output section", func() {
    md := &lib.Markdown{}
    result, err := agentStep.Run(ctx, md)
    Expect(err).NotTo(HaveOccurred())
    Expect(result).NotTo(BeNil())
    _, exists := md.FindSection("## Analysis")
    Expect(exists).To(BeFalse())
})
```

6c. **Keep the existing "when runner succeeds" context unchanged** — its mock returns `{"status":"done","message":"analysis complete"}`, which is a genuine success body: section written, `AgentStatusDone` returned. This stays green under the new `Run` logic.

6d. **Add the following new contexts** after "when runner succeeds", still inside `Describe("Run")`:

```go
Context("when runner returns a needs_input body", func() {
    BeforeEach(func() {
        mockRunner.RunReturns(&claude.ClaudeResult{
            Result: `{"status":"needs_input","message":"permission denied"}`,
        }, nil)
    })

    It("returns Result with NeedsInput status and does not write the section", func() {
        md := &lib.Markdown{}
        result, err := agentStep.Run(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(result).NotTo(BeNil())
        Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
        Expect(result.Message).To(ContainSubstring("permission denied"))
        _, exists := md.FindSection("## Analysis")
        Expect(exists).To(BeFalse())
    })
})

Context("when runner returns a failed body", func() {
    BeforeEach(func() {
        mockRunner.RunReturns(&claude.ClaudeResult{
            Result: `{"status":"failed","message":"claude CLI crashed"}`,
        }, nil)
    })

    It("returns Result with Failed status and does not write the section", func() {
        md := &lib.Markdown{}
        result, err := agentStep.Run(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(result).NotTo(BeNil())
        Expect(result.Status).To(Equal(lib.AgentStatusFailed))
        Expect(result.Message).To(ContainSubstring("claude CLI crashed"))
        _, exists := md.FindSection("## Analysis")
        Expect(exists).To(BeFalse())
    })
})
```

These are spec AC4's named rows (the needs_input row is AC4's exact evidence shape: mock runner returning `{"status":"needs_input","message":"permission denied"}` → result status `NeedsInput`, `md.FindSection("## Analysis")` not found). The `md.FindSection("## Analysis")` after `Run` uses the exact heading string `"## Analysis"` matching `OutputSection` in the shared `BeforeEach`.

6e. **Add one row covering the fallback-message branch** — a `needs_input` body with NO `message` field must still return `NeedsInput` status and a non-empty fallback message (the `msg == ""` → `fmt.Sprintf` branch in requirement 3), and must not write the section:

```go
Context("when runner returns a needs_input body without a message", func() {
    BeforeEach(func() {
        mockRunner.RunReturns(&claude.ClaudeResult{
            Result: `{"status":"needs_input"}`,
        }, nil)
    })

    It("returns NeedsInput status with a fallback message and does not write the section", func() {
        md := &lib.Markdown{}
        result, err := agentStep.Run(ctx, md)
        Expect(err).NotTo(HaveOccurred())
        Expect(result).NotTo(BeNil())
        Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
        Expect(result.Message).NotTo(BeEmpty())
        _, exists := md.FindSection("## Analysis")
        Expect(exists).To(BeFalse())
    })
})
```

## 7. Document the changed idempotency semantic in `docs/task-flow-and-failure-semantics.md`

Insert a new `## AgentStep output-section idempotency (spec 051)` section immediately before the `## References` heading. Content (adapt wording freely, keep the substance):

```markdown
## AgentStep output-section idempotency (spec 051)

`claude/agentStep.ShouldRun` skips a step only when its output section (e.g. `## Analysis`) exists AND represents a genuine success. Re-dispatch re-runs the step when the task carries a failure marker — a `## Failure` section, or an output-section body that parses to a `needs_input`/`failed` AgentResult — so a failed run can never permanently poison re-dispatch. Absence of a success section forces a run. Unparseable prose bodies with no `## Failure` marker still skip (prose agents unchanged).

`claude/agentStep.Run` never writes a success-looking output section for a `needs_input`/`failed` runner body: it returns that status and the deliverer writes the `## Failure` marker as today (`delivery/content-generator.go`). Failure-marker detection is in-memory markdown inspection only (the `Step.ShouldRun` "guards must be cheap" contract).
```

Do NOT change any existing content in the doc. This is the spec constraint "Specs die after implementation; the rule must survive in the doc".

## 8. Add the CHANGELOG entry

Per `changelog-guide.md`: the entry format is `- <prefix>: <what>` with prefix `fix:` for a bug fix, and if `## Unreleased` already exists at the top of `CHANGELOG.md`, append to it (do not add a second `## Unreleased` heading; do not rename any version heading). If no `## Unreleased` exists yet, create it at the top and add the bullet under it.

```markdown
- fix(claude): `agentStep.ShouldRun` re-runs a step whose previous run failed — a `## Failure` section or a `needs_input`/`failed` output-section body now forces re-dispatch instead of being skipped as if completed; `agentStep.Run` no longer writes a success-looking output section for a `needs_input`/`failed` runner body (spec 051)
```

REVIEWER/EXECUTOR NOTE: if this bullet (or an equivalent `spec 051` fix entry) is ALREADY present in the changelog — e.g. under a released version header like `## v0.84.1` — the fix is already shipped and the changelog step is a no-op: do NOT create a `## Unreleased` section or duplicate the bullet. Leave the changelog as-is and record that fact in the completion report.

## 9. Scope containment

Edit ONLY these four files:
- `claude/agent-step.go`
- `claude/agent-step_test.go`
- `docs/task-flow-and-failure-semantics.md`
- `CHANGELOG.md`

Do NOT touch: `agent_step.go` (the `Step` interface), `agent_parser.go` (`ParseStep` keeps its own skip-only `ShouldRun` — it is a different step type and out of scope), `delivery/content-generator.go`, `claude/task-runner.go`, `claude/types.go`, `mocks/`, `agent-task-executor` behavior, the collector prompt, or `scripts/sentry-create-tasks.sh`. The `## Failure` heading constant lives only in `claude/agent-step.go`; do not propagate it into `delivery/`.

## 10. Failure-mode self-check (walk the spec table before finishing)

Each spec Failure Modes row must be satisfied by the code you wrote and the tests you added:
- Unparseable prose + no `## Failure` → `ShouldRun` false (5b prose row) — spec DB1/DB7.
- `needs_input` body + no `## Failure` → `ShouldRun` true (5b needs_input row) — spec DB2.
- `## Failure` on job-level `deadline_exceeded` → `ShouldRun` true (5b `## Failure` row) — spec DB3.
- Runner error before any write → `Run` returns `AgentStatusFailed`, no section (6b no-write row) — spec DB4.
- Two concurrent re-dispatches both pass `ShouldRun` → last write wins via `ReplaceSection` (idempotent), controller dedups per-alert files — inherent, no code change (spec DB5).
- Unknown status value → success/skip fall-through (5b unknown-status row) — spec DB6.
- Prose agents unchanged on re-dispatch (prose body → skip; `Run` writes prose body as a section) — spec DB7.

</requirements>

<constraints>
- Fix lands in `claude/agent-step.go` (the lib that all `NewAgentStep` consumers use); no change to the `Step` interface or `StepRunner` semantics.
- The single-step idempotency intent is preserved: a genuine success section still skips. Do NOT remove idempotency — the bug is that failure looks like success, not that idempotency exists.
- Failure-marker detection must be cheap (in-memory markdown inspection, no I/O) — consistent with the `ShouldRun` interface doc "Guards must be cheap".
- Reuse the existing `## Failure` heading string via the new `failureSectionHeading` constant in `claude/agent-step.go`; do not introduce a new section-name convention. The `delivery/` package keeps its own literals.
- Parsing the output-section body as `AgentResult` is best-effort: unparseable bodies fall through to the success/skip path (prose agents unchanged).
- Do NOT change `agentTaskExecutor`/`agent-task-executor` behavior — the executor spawn cap and `deadline_exceeded` mapping are out of scope (executor stays v0.6.4).
- No change to the collector's prompt or `scripts/sentry-create-tasks.sh` — this spec fixes the re-dispatch poison, not the transient.
- Ginkgo v2 / Gomega; counterfeiter mocks (`mocks/`); external test packages (`*_test`). `claude/agent-step_test.go` is already `package claude_test` and uses the generated `libmocks.ClaudeRunner` — reuse it, do not hand-write mocks.
- Error wrapping uses `github.com/bborbe/errors.Wrapf(ctx, err, ...)`; never `fmt.Errorf`, never bare `return err`. (The only new `fmt` use is `fmt.Sprintf` for the failure-result message — a value, not an error.)
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass. Existing rows named in the spec ("when runner returns error → returns Result with Failed status", "when runner succeeds", "when section already exists → returns false") stay green with unchanged names.
- Test coverage: all new code paths get a Ginkgo row (the boundary crossed is `ShouldRun`/`Run` → `parseAgentResultBody` → `extractLastJSONObject` + `json.Unmarshal`, exercised directly with real JSON bodies).
</constraints>

<verification>
Run from `/workspace` (single-module repo; the Makefile and go.mod are at root, there is no `claude/Makefile`):

```bash
cd /workspace
# 1. Targeted claude package tests (fast iteration — run after each edit):
go test -mod=mod ./claude/ -v 2>&1 | tee /tmp/claude-test.log
# Must exit 0. The new Ginkgo rows must appear in the output (grep each It description):
grep -E "needs_input AgentResult|failed AgentResult|## Failure section is present|unparseable prose|unknown status|success body|does not write the output section|NeedsInput status|Failed status and does not write" /tmp/claude-test.log
# Each pattern must match at least one line.

# 2. Root module still green (spec Verification "make test"):
cd /workspace && make test
# Must exit 0.

# 3. Full pipeline — AC5 (must exit 0; this runs ensure + format + generate + test + lint + license):
cd /workspace && make precommit
# If the Makefile's ROOTDIR derivation (`git rev-parse --show-toplevel`) fails because .git is
# masked in the container, re-run as: ROOTDIR=/workspace make precommit

# 4. Scope check — only the four intended files carry changes:
git status --porcelain   # if .git is masked, skip this and instead confirm manually that the
                         # four files above were the only ones edited
```
</verification>
