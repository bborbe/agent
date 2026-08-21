---
status: completed
spec: [049-claude-runner-persists-partial-output]
summary: 'Wired bounded partial capture into the claude runner: scanOutput now accumulates streamed assistant text into a frozen 16 KiB cap (partialMaxBytes + appendPartial/capturePartial) and Run returns ClaudeResult.Partial on every non-success termination (kill path, cancellation, missing-result) and on success; six new Ginkgo contexts cover all paths, coverage 89.6% (scanOutput 92.9%, Run 83.3%), make precommit exits 0. CHANGELOG entry deliberately deferred to prompt 2 per the prompt''s explicit constraint.'
execution_id: agent-pr-reviewer-salvage-exec-208-spec-049-runner-partial-capture
dark-factory-version: dev
created: "2026-08-21T10:20:00Z"
queued: "2026-08-21T11:03:51Z"
started: "2026-08-21T11:03:56Z"
completed: "2026-08-21T11:16:10Z"
branch: dark-factory/claude-runner-persists-partial-output
---

<summary>
- When a claude CLI run is terminated early (subprocess killed, run context cancelled, or the CLI exits without emitting a result event), the caller now receives the assistant text the model streamed up to that moment, delivered alongside the error instead of getting nothing back
- A new `Partial` field on the run result carries that text, kept fully separate from the error message, so a salvage caller can persist partial findings instead of losing them
- The partial is bounded to the most recent 16 KiB by a frozen package constant — a runaway stream cannot exhaust memory and the cap cannot be disabled or tuned
- The existing tail-line diagnostic error format is untouched: killed runs still carry the CLI's last stdout lines in the error message
- The run context-cancellation branch of the scanner no longer discards captured state — it returns what was captured before cancellation, which is the core bug this spec fixes
- Successful runs behave exactly as before, except the new field is populated when the CLI streamed any assistant text first; callers that ignore it are unaffected
- Six new Ginkgo specs drive the real runner through a shell-script `claude` shim in a temp dir prepended to `PATH`: kill path, cancellation, envelope-negative, bounded capture, success path, and missing-result path
- The `Run` signature, the `ClaudeRunner` interface, `ClaudeRunnerConfig`, and the generated mock are all unchanged — this is purely additive
</summary>

<objective>
Modify the `claude` package so that `scanOutput` accumulates the streamed assistant text (the review markdown the model is writing) into a bounded partial buffer during the same linear pass that extracts result text, usage, and the tail ring buffer, and so that `Run` returns that partial on `ClaudeResult.Partial` whenever the run does not complete normally (non-zero exit, context cancellation, or missing result event) alongside the error — while leaving the success path byte-identical except for the new populated field. Implements spec 049 Desired Behaviors 1-4 and Acceptance Criteria 1-4. This is prompt 1 of 2 (the mechanism and all its tests); prompt 2 owns the CHANGELOG entry, mock-unchanged verification, and scope containment.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (Ginkgo v2 / Gomega, external test packages, `github.com/bborbe/errors`, counterfeiter explicit directives).

Read these coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test package `claude_test`, coverage ≥ 80% for changed code, error paths must be tested.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `bborbe/errors` with context wrapping, never `fmt.Errorf` in pkg/.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — repo idioms (public interface, private struct, `New*` constructor).
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments start with the identifier name, full sentences, describe behavior.
- `/workspace/docs/dod.md` — this repo's Definition of Done.

Read these files IN FULL before editing:
- `/workspace/claude/claude-runner.go` (302 lines) — `Run` at line 46 and `scanOutput` at line 187 are the two functions to change; the `tail*` constants at lines 22-26 and `appendTail` at line 132 are the pattern the partial capture mirrors.
- `/workspace/claude/claude-result.go` (21 lines) — the type to widen.
- `/workspace/claude/claude-event.go` (68 lines) — the `claudeEvent` / `claudeContent` wire types.
- `/workspace/claude/claude-runner_test.go` (560 lines) — the existing Ginkgo suite; reuse the `writeShim` PATH-shim helper pattern (redeclared inside each `Describe`, per the existing convention).
- `/workspace/claude/claude_suite_test.go` — suite wiring (`package claude_test`, 60 s timeout).

Load-bearing snippets, verified verbatim against source.

Current constants in `/workspace/claude/claude-runner.go` (lines 22-26):
```go
const (
	tailMaxLines = 5
	tailMaxBytes = 512
	tailJoiner   = " | "
)
```

Current `Run` method in `/workspace/claude/claude-runner.go` (lines 46-85), in full:
```go
func (r *claudeRunner) Run(ctx context.Context, prompt string) (*ClaudeResult, error) {
	cmd, err := r.buildCommand(ctx, prompt)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "build command")
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create stdout pipe")
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrap(ctx, err, "start claude CLI")
	}

	resultText, usage, tail := scanOutput(ctx, stdoutPipe)

	if err := cmd.Wait(); err != nil {
		var tailMsg string
		if len(tail) > 0 {
			tailMsg = strings.Join(tail, tailJoiner)
		} else {
			tailMsg = "no stdout captured"
		}
		return nil, errors.Wrapf(ctx, err, "claude CLI failed: %s", tailMsg)
	}

	if resultText == "" {
		return nil, errors.New(ctx, "no result event found in claude CLI output")
	}

	return &ClaudeResult{
		Result:              resultText,
		InputTokens:         usage.inputTokens,
		OutputTokens:        usage.outputTokens,
		CacheCreationTokens: usage.cacheCreationTokens,
		CacheReadTokens:     usage.cacheReadTokens,
		NumTurns:            usage.numTurns,
	}, nil
}
```

Current `scanOutput` in `/workspace/claude/claude-runner.go` (lines 185-246), in full:
```go
// scanOutput reads stream-json lines from stdout, logs events, and returns the result
// text, the captured usage summary, and a bounded tail of all non-empty lines.
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, sessionUsage, []string) {
	var resultText string
	var usage sessionUsage
	var tail []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return "", sessionUsage{}, nil
		default:
		}

		line := scanner.Bytes()
		glog.V(4).Infof("[line] %s", line)

		tail = appendTail(tail, line)

		// Two-pass unmarshal: first extract type/result safely (never fails on schema issues),
		// then attempt full unmarshal for usage fields (may fail on e.g. json.Number receiving
		// a string). The first pass always succeeds for syntactically valid JSON, ensuring
		// resultText survives schema drift in the usage subtree.
		var holder resultHolder
		if err := json.Unmarshal(line, &holder); err != nil {
			continue
		}

		if holder.Type == "result" && holder.Result != "" {
			resultText = holder.Result
		}

		// Full unmarshal: may fail due to schema-level issues in usage fields.
		// On failure we still keep the resultText captured above.
		var event claudeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		// Usage capture is deliberately gated on the presence of a usage object, NOT
		// on a non-empty result text: a later result event carrying fresh usage but an
		// empty result string must update the numbers while leaving the previously
		// captured text intact. Last usage object wins.
		if event.Type == "result" && len(event.Usage) > 0 {
			usage = parseUsage(event.Usage, event.NumTurns)
		}

		for _, c := range event.Message.Content {
			switch c.Type {
			case "tool_use":
				logToolUse(c)
			default:
				glog.V(2).Infof("type(%s): %s", c.Type, c.Text)
			}
		}
	}
	return resultText, usage, tail
}
```

Current `ClaudeResult` in `/workspace/claude/claude-result.go`, in full:
```go
// ClaudeResult holds the parsed output from a Claude Code CLI session.
type ClaudeResult struct {
	Result string `json:"result"`
	// InputTokens is the count of fresh (non-cached) input tokens the session consumed.
	InputTokens int64 `json:"input_tokens,omitempty"`
	// OutputTokens is the count of output tokens the session produced.
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// CacheCreationTokens is the count of input tokens written into the prompt cache.
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// CacheReadTokens is the count of input tokens served from the prompt cache.
	CacheReadTokens int64 `json:"cache_read_input_tokens,omitempty"`
	// NumTurns is the number of conversation turns the session took. Zero when the
	// CLI reported no usage summary.
	NumTurns int64 `json:"num_turns,omitempty"`
}
```

The `writeShim` helper pattern from `/workspace/claude/claude-runner_test.go` (lines 29-40) — redeclare this closure inside the new `Describe` (the established convention in this file is one `writeShim` per `Describe`; do NOT hoist it):
```go
	// writeShim creates a temp dir, writes a "claude" shell script with the given body,
	// prepends the dir to PATH, and registers cleanup via DeferCleanup.
	writeShim := func(body string) {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := "#!/bin/sh\n" + body
		err := os.WriteFile(shimPath, []byte(script), 0755) //nolint:gosec
		Expect(err).NotTo(HaveOccurred())
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	}
```

Verified call-site facts:
- `scanOutput` has exactly ONE caller in this repo: `claude-runner.go:61`. The `pi` package's `/workspace/pi/pi-runner.go:58` has a SEPARATE unexported `scanOutput` in `package pi` — do NOT touch `/workspace/pi/` (spec Non-goal).
- `ClaudeResult` is constructed in exactly one non-test place (`claude-runner.go:77`); all test and healthcheck construction sites use keyed literals, so adding a field breaks nothing. The generated mock `/workspace/mocks/claude-claude-runner.go` references only `*claude.ClaudeResult` (by pointer) and the `Run(ctx, prompt)` signature — both unchanged, so no mock regeneration is needed for this prompt.
- Every existing `ClaudeRunner.Run` call site (`claude/agent-step.go:88`, `claude/task-runner.go:59`, `healthcheck/healthcheck-claude-step.go:32`) ignores the result value when `err != nil`, so returning a non-nil result alongside the error (required by this spec) changes no caller behavior.
- In Claude Code stream-json output, the model's streamed review markdown arrives as `{"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}` events. Gating the capture on `event.Type == "assistant"` plus content `type == "text"` selects exactly the plain streamed assistant text and excludes tool payloads (the `tool_use` content items carry their payload in `Input`, not `Text`), usage telemetry, and the JSON envelope.
</context>

<requirements>

## 1. Add the frozen capture-cap constant to `/workspace/claude/claude-runner.go`

Immediately after the existing `const ( tailMaxLines ... )` block (lines 22-26), add:

```go
// partialMaxBytes caps the partial captured from the claude CLI stream: at most this
// many of the most recent bytes of streamed assistant text are kept. It is a frozen
// package constant (spec 049): a cap that can be disabled is an escape hatch on the
// salvage feature, so it is deliberately NOT configurable.
const partialMaxBytes = 16384
```

This constant is intentionally NOT exported and NOT on `ClaudeRunnerConfig` (spec Non-goal: no config knob, no env var, no opt-out).

## 2. Add the bounded-append helper `appendPartial` to `/workspace/claude/claude-runner.go`

Directly below the existing `appendTail` function (line 145), add a sibling that mirrors `appendTail`'s structure:

```go
// appendPartial appends chunk to the bounded partial buffer, keeping at most
// partialMaxBytes of the most recent bytes and dropping the earliest bytes on
// overflow.
func appendPartial(partial []byte, chunk string) []byte {
	partial = append(partial, chunk...)
	if len(partial) > partialMaxBytes {
		partial = partial[len(partial)-partialMaxBytes:]
	}
	return partial
}
```

No new imports are needed — the existing import block already has `"strings"` (used by `Run`); `appendPartial` uses only the standard library builtins.

## 3. Change `scanOutput` to also return the bounded partial

In `/workspace/claude/claude-runner.go`, change `scanOutput` as follows.

3a. Update the GoDoc and signature — the third return value is the partial (`string`), the fourth is the existing tail:

```go
// scanOutput reads stream-json lines from stdout, logs events, and returns the result
// text, the captured usage summary, the bounded partial of streamed assistant text,
// and a bounded tail of all non-empty lines.
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, sessionUsage, string, []string) {
	var resultText string
	var usage sessionUsage
	var partial []byte
	var tail []string
```

3b. Change the context-cancellation early return — this is the bug fix: the branch that currently zeroes everything (`return "", sessionUsage{}, nil`) must instead return the state captured so far:

```go
		select {
		case <-ctx.Done():
			return resultText, usage, string(partial), tail
		default:
		}
```

3c. Add the assistant-text capture block immediately BEFORE the existing `for _, c := range event.Message.Content {` content loop (the block that calls `logToolUse`), keeping that existing loop byte-for-byte unchanged so the V(2) logging behavior does not regress:

```go
		if event.Type == "assistant" {
			for _, c := range event.Message.Content {
				if c.Type == "text" {
					partial = appendPartial(partial, c.Text)
				}
			}
		}
```

This selects only the plain streamed assistant text. Tool payloads (`tool_use` content items — their payload rides in `Input`, and `Text` is empty), usage telemetry (top-level `usage`), and the stream-json envelope itself are excluded. On CLI event-schema drift, this gate degrades to an empty partial without crashing — the required spec failure-mode behavior. Empty `c.Text` is a no-op inside `appendPartial`, so no empty-string gate is needed.

3d. Change the final return to:

```go
	return resultText, usage, string(partial), tail
```

## 4. Update `Run` to surface the partial on every non-success termination and populate it on success

In `/workspace/claude/claude-runner.go`, replace the `Run` method body after `stdoutPipe` setup. The changes relative to the current source (quoted in `<context>`):

4a. Change the `scanOutput` call to capture the partial:
```go
	resultText, usage, partial, tail := scanOutput(ctx, stdoutPipe)
```

4b. Replace the `cmd.Wait()` error return — the caller must now receive the partial alongside the error (spec Desired Behavior 2). The error message format is UNCHANGED (same tail join, same `no stdout captured` fallback):
```go
	if err := cmd.Wait(); err != nil {
		var tailMsg string
		if len(tail) > 0 {
			tailMsg = strings.Join(tail, tailJoiner)
		} else {
			tailMsg = "no stdout captured"
		}
		return &ClaudeResult{
			Partial: partial,
		}, errors.Wrapf(ctx, err, "claude CLI failed: %s", tailMsg)
	}
```

4c. Replace the missing-result-event return — same pattern, partial alongside the error (spec Desired Behavior 2: "or a missing result event"):
```go
	if resultText == "" {
		return &ClaudeResult{
			Partial: partial,
		}, errors.New(ctx, "no result event found in claude CLI output")
	}
```

4d. Add `Partial: partial` to the success return so the success path is also populated (spec Desired Behavior 2: "On normal success the partial is likewise populated with any streamed text"). The success path is otherwise byte-identical:
```go
	return &ClaudeResult{
		Result:              resultText,
		Partial:             partial,
		InputTokens:         usage.inputTokens,
		OutputTokens:        usage.outputTokens,
		CacheCreationTokens: usage.cacheCreationTokens,
		CacheReadTokens:     usage.cacheReadTokens,
		NumTurns:            usage.numTurns,
	}, nil
```

Note: on the two error paths the returned `*ClaudeResult` carries ONLY `Partial` (all other fields zero). This is deliberate and safe — every existing caller in this repo ignores the result when `err != nil`.

## 5. Add the `Partial` field to `ClaudeResult` in `/workspace/claude/claude-result.go`

Insert it directly below `Result` (position is not load-bearing for JSON but keeps the struct grouped by importance):

```go
// ClaudeResult holds the parsed output from a Claude Code CLI session.
type ClaudeResult struct {
	Result string `json:"result"`
	// Partial is the assistant text the CLI streamed before the run ended, bounded
	// to the most recent 16 KiB. Empty when the CLI streamed no assistant text.
	Partial string `json:"partial,omitempty"`
	// InputTokens is the count of fresh (non-cached) input tokens the session consumed.
	InputTokens int64 `json:"input_tokens,omitempty"`
	// OutputTokens is the count of output tokens the session produced.
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// CacheCreationTokens is the count of input tokens written into the prompt cache.
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
	// CacheReadTokens is the count of input tokens served from the prompt cache.
	CacheReadTokens int64 `json:"cache_read_input_tokens,omitempty"`
	// NumTurns is the number of conversation turns the session took. Zero when the
	// CLI reported no usage summary.
	NumTurns int64 `json:"num_turns,omitempty"`
}
```

`omitempty` keeps the marshalled shape of a run with no streamed assistant text byte-identical to today (spec Constraint: exactly one additive field, `Partial string`, `partial,omitempty`).

## 6. Add the `claudeRunner partial capture` Ginkgo block to `/workspace/claude/claude-runner_test.go`

Append a new top-level `Describe` at the end of the file (`package claude_test`). First add `"time"` to the file's import block (needed by the cancellation spec). Then add the block below, verbatim. Invoke the runner exactly as the existing tests do: `claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(...)`.

```go
var _ = Describe("claudeRunner partial capture", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// writeShim creates a temp dir, writes a "claude" shell script with the given body,
	// prepends the dir to PATH, and registers cleanup via DeferCleanup.
	writeShim := func(body string) {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := "#!/bin/sh\n" + body
		err := os.WriteFile(shimPath, []byte(script), 0755) //nolint:gosec
		Expect(err).NotTo(HaveOccurred())
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	}

	Context("non-zero exit after streaming assistant text (kill path)", func() {
		BeforeEach(func() {
			writeShim(
				`echo '{"type":"error","message":"auth-failure: 401 Invalid authentication credentials"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"## Review of the moss PR\n"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Findings batch one: all 7 concerns identified.\n"}]}}'
exit 1`,
			)
		})

		It("returns the streamed assistant text as the partial alongside the error", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Partial).To(ContainSubstring("## Review of the moss PR"))
			Expect(result.Partial).To(ContainSubstring("Findings batch one: all 7 concerns identified."))
		})

		It("keeps the tail diagnostic line in the error, not only the partial", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("auth-failure: 401 Invalid authentication credentials"))
		})

		It("partial is plain assistant text, not the stream-json envelope (negative)", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Partial).NotTo(ContainSubstring(`{"type":`))
		})
	})

	Context("context cancellation mid-stream", func() {
		BeforeEach(func() {
			// Emits one assistant-text line, then blocks so the run is still in
			// progress when the test cancels the context.
			writeShim(
				`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"pre-cancellation-partial-text\n"}]}}'
sleep 30`,
			)
		})

		It("returns the pre-cancellation partial alongside the error", func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(cancelCtx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Partial).NotTo(BeEmpty())
			Expect(result.Partial).To(ContainSubstring("pre-cancellation-partial-text"))
		})
	})

	Context("streaming more assistant text than the cap", func() {
		BeforeEach(func() {
			// Emits 1 + 300 + 1 assistant-text lines totalling well over the 16384-byte
			// cap (~19340 bytes of decoded text): the earliest bytes (the FIRST marker)
			// must be dropped, the most recent bytes (the LAST marker) must be kept.
			// Do not "simplify" this fixture to fewer/smaller lines — the total MUST
			// exceed the cap or the length assertion below cannot hold.
			writeShim(
				`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"FIRST-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}]}}'
i=0
while [ $i -lt 300 ]; do
  echo '{"type":"assistant","message":{"content":[{"type":"text","text":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}]}}'
  i=$((i+1))
done
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx-LAST"}]}}'
exit 1`,
			)
		})

		It("keeps at least 16384 bytes of the most recent text", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(len(result.Partial)).To(BeNumerically(">=", 16384))
		})

		It("keeps the last streamed line and drops the first streamed line", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Partial).To(ContainSubstring("-LAST"))
			Expect(result.Partial).NotTo(ContainSubstring("FIRST"))
		})
	})

	Context("successful exit with streamed assistant text", func() {
		BeforeEach(func() {
			writeShim(
				`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"streamed-review-draft\n"}]}}'
echo '{"type":"result","result":"final-task-output"}'
exit 0`,
			)
		})

		It("returns the streamed text as the partial on success too", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Result).To(Equal("final-task-output"))
			Expect(result.Partial).To(ContainSubstring("streamed-review-draft"))
		})
	})

	Context("zero exit with no result event after streaming", func() {
		BeforeEach(func() {
			writeShim(
				`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"partial-before-missing-result\n"}]}}'
exit 0`,
			)
		})

		It("returns the partial alongside the missing-result error", func() {
			result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no result event found"))
			Expect(result).NotTo(BeNil())
			Expect(result.Partial).To(ContainSubstring("partial-before-missing-result"))
		})
	})
})
```

Notes for implementation:
- The `writeShim` closure is redeclared inside this `Describe` — follow the existing per-`Describe` duplication convention in this file (do NOT hoist it to a shared helper).
- The kill-path and cancellation fixtures are the regression guards for the non-zero-exit and context-cancellation surfaces (spec Failure Modes rows 1-2); the bounded fixture is the guard for the resource-exhaustion row; the envelope-negative spec is the guard for "plain text, not the raw envelope" (Acceptance Criterion 2); the success and missing-result specs cover the remaining `Run` branches so the changed code reaches ≥80% coverage.
- The existing `claudeRunner stdout tail`, `usage capture`, `CLAUDE_CONFIG_DIR`, and `AllowedTools` describes must pass UNMODIFIED — none of them emit `assistant` events, so `Partial` stays empty for them and no assertion in them touches the new field.

## 7. Run the package tests iteratively

```bash
cd /workspace && go test -mod=mod -race ./claude/...
```

Must report `ok` / PASS. Fix compile or assertion errors before proceeding. Common issues to check:
- Missing `"time"` import in `claude-runner_test.go`.
- The `scanOutput` caller in `Run` must destructure four return values (`resultText, usage, partial, tail := ...`).
- If the cancellation spec is flaky on a slow container (2 s window too short for the first line to be read), raise the `context.WithTimeout` duration — the suite timeout is 60 s, so a 5 s window is still safe. Do not shorten the shim's `sleep 30`.

## 8. Check coverage for the changed code

```bash
cd /workspace && go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out | grep -E 'scanOutput|claude-runner.go:.*Run|total'
```

`scanOutput` and `Run` must each be ≥ 80% (the six new contexts cover every new branch: kill, cancellation, bounded overflow, success-with-partial, missing-result, plus the pre-existing paths).

## 9. Run the full precommit gate

```bash
cd /workspace && make precommit
```

Must exit 0. If a target fails, fix it, then re-run ONLY the failing target (`make format`, `make lint`, `make test`, ...) until it passes before re-running `make precommit` once more.
</requirements>

<constraints>
- `Run(ctx, prompt) (*ClaudeResult, error)` signature and the `ClaudeRunner` interface are frozen. No new method, no new argument. The generated mock at `mocks/claude-claude-runner.go` MUST NOT change — the interface is unchanged, so no `make generate` output should differ (prompt 2 verifies this explicitly).
- `ClaudeResult` gains exactly one additive field, `Partial string` (JSON `partial,omitempty`). Every existing field and JSON tag is unchanged; the success-path `Result`, token, and turn semantics are unchanged.
- The tail ring-buffer contract is frozen: max 5 lines, max 512 bytes per line, ` | ` joiner, and the `no stdout captured` empty case. Existing tail-error tests pass unmodified.
- The run-cancellation branch of `scanOutput` MUST return the captured partial and tail instead of empty values — discarding them is the bug this spec fixes.
- The capture cap is a package-internal constant of 16384 bytes that keeps the most recent text. It is NOT a `ClaudeRunnerConfig` field and NO env knob or opt-out exists for it. (Spec Non-goal — hard veto.)
- The CLI invocation flag set (`--print --output-format stream-json --verbose --strict-mcp-config`) is unchanged.
- Do NOT touch `/workspace/pi/` — its identically-named unexported `scanOutput` is a different function in a different package.
- Do NOT touch `CHANGELOG.md` in this prompt — the sibling prompt of this spec owns the changelog entry. Do NOT write any other package or file outside `claude/`.
- Do NOT add a scenario — the behavior is fully reachable via these unit tests with the shell-script shim (spec Non-goal).
- Error handling stays on `github.com/bborbe/errors` with context wrapping. No `fmt.Errorf`, no bare `return err`.
- Tests are Ginkgo v2 / Gomega in the external `claude_test` package, driving the real runner through the `writeShim` PATH shim — no network, no real claude install, no `package claude` in-package test file.
- Line length limit is 100 characters (golines runs in `make format`); funlen limit is 80 lines. Keep GoDoc lines under 100.
- Coverage for the changed code (`scanOutput`, `Run`, `appendPartial`) must be ≥ 80%.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# The additive field exists (spec container check).
grep -n 'Partial' /workspace/claude/claude-result.go
# Must return at least 1 line (the field declaration + GoDoc).
```

```bash
# The frozen cap constant exists and is not a config field.
grep -n 'partialMaxBytes' /workspace/claude/claude-runner.go
# Must return at least 2 lines (the const and appendPartial).
grep -n 'partialMaxBytes' /workspace/claude/claude-runner-config.go
# Must return ZERO lines (exit 1) — the cap is NOT configurable.
```

```bash
# The capture gate is on assistant events.
grep -n 'event.Type == "assistant"' /workspace/claude/claude-runner.go
# Must return exactly 1 line.
```

```bash
# scanOutput returns four values.
grep -n 'func scanOutput' /workspace/claude/claude-runner.go
# Must show: (string, sessionUsage, string, []string)
```

```bash
# The cancellation branch no longer zeroes captured state.
grep -n 'return resultText, usage, string(partial), tail' /workspace/claude/claude-runner.go
# Must return exactly 1 line.
```

```bash
# Package tests — AC1 through AC4.
cd /workspace && go test -mod=mod -race ./claude/...
# Must report ok / PASS, including the new "claudeRunner partial capture" suite.
```

```bash
# Coverage for the changed package.
cd /workspace && go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out | grep -E 'scanOutput|claude-runner.go:.*Run|total'
# scanOutput and Run must each be >= 80%.
```

```bash
# Final full validation at the repository root.
cd /workspace && make precommit
# Must exit 0.
```
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **Git-based acceptance criteria moved to the operator ladder.** Spec AC 6's changelog-fold guard (`git diff origin/master -- CHANGELOG.md | grep -E '^[-+]## '`) and AC 7's scope containment (`git diff --name-only origin/master...HEAD`) are git commands; the daemon runs with `hideGit=true` (confirmed in `.dark-factory.log`: `hideGit=true hideGitSource=arg`, and `/workspace/.git` is masked as a char device), so a bare `git` in the container would fail closed and produce a false-positive pass. Per dark-factory prompt rules these moved to the operator/manager side of the spec's Verification ladder (the manager commits and can run `git diff --name-only origin/master...HEAD` itself). The executor-side proxies are the hard constraint "only files under `claude/` may change" plus the grep-based checks above.
- **Error path now returns a non-nil `*ClaudeResult` alongside the error.** This is spec-mandated (AC 1a requires the partial be returned alongside a non-nil error), but it IS a behavioral change from the previous `nil, err` contract on the error path. Verified safe for all four in-repo call sites (`claude/agent-step.go`, `claude/task-runner.go`, `healthcheck/healthcheck-claude-step.go`, and the `pi` runner uses a different interface) — each ignores the result when `err != nil`. Downstream consumers (the companion `github-pr-review-agent` repo) should be told the error path now yields a non-nil result carrying `Partial`; this is the surface the companion's salvage prompt consumes.
- **Partial capture is assistant-event-gated.** The gate `event.Type == "assistant"` + content `type == "text"` matches the Claude Code stream-json assistant message shape. If a future CLI version streams review text under a different event/content type, the capture degrades to an empty partial without crashing (the spec's schema-drift failure mode) — but that means the salvage feature would silently produce nothing. The companion spec should include a real-run sanity check that `Partial` is non-empty on a terminated run; the fixture-driven unit tests here cannot catch real-world schema drift.
