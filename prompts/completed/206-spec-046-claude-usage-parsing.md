---
status: completed
spec: [046-job-usage-metrics]
summary: Added usage token counts (input, output, cache-creation, cache-read) and turn count to ClaudeResult via two-pass unmarshal with map-based field extraction for schema-drift tolerance
execution_id: agent-usage-metrics-exec-206-spec-046-claude-usage-parsing
dark-factory-version: v0.192.9
created: "2026-08-01T22:05:00Z"
queued: "2026-08-01T22:17:52Z"
started: "2026-08-01T22:17:58Z"
completed: "2026-08-01T22:31:54Z"
branch: dark-factory/job-usage-metrics
---

<summary>
- Every Claude Code session already ends with a summary of how many tokens it burned and how many conversation turns it took; today that summary is read off the wire and thrown away.
- After this change the parsed session result carries those five numbers (fresh input tokens, output tokens, cache-creation tokens, cache-read tokens, turn count) so any caller can read them.
- Sessions where the CLI reports no usage summary, or only some of the fields, still parse exactly as they do today — the missing numbers simply read as zero and nothing errors out.
- When a session emits more than one final event carrying a usage summary, the newest summary wins.
- That "newest wins" rule for the numbers is deliberately independent of the existing "newest non-empty text wins" rule for the result text, so a later event with numbers but no text cannot wipe out the text.
- The dollar-cost figure the CLI reports is deliberately NOT captured — under a non-Anthropic provider it is a fictional number and seeding it into a dashboard would be worse than having none.
- No behavior visible to existing callers changes: the same result text comes back, the same errors are raised, the Pi harness is untouched.
- This prompt covers only the parsing half of the spec; publishing the numbers as Prometheus counters is a sibling prompt, and wiring the two together happens in a different repository.
</summary>

<objective>
Capture the Claude CLI's end-of-session usage summary (four token counts plus the turn count) while scanning the stream-json output in the `claude` package, and expose all five values on the `ClaudeResult` returned by `ClaudeRunner.Run`. Absent or partial usage must never be an error. Implements spec 046 Desired Behaviors 1-4 and Acceptance Criteria 1-5.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (Ginkgo v2 / Gomega, counterfeiter, external test packages, `github.com/bborbe/errors`).

Read these coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test package, coverage >= 80% for changed code, error paths must be tested.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments start with the identifier name, full sentences, describe behavior.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — repo idioms.
- `/workspace/docs/dod.md` — definition of done for this repo.

Read these files IN FULL before editing:
- `/workspace/claude/claude-event.go` (25 lines) — the stream-json wire types.
- `/workspace/claude/claude-result.go` (10 lines) — the type to widen.
- `/workspace/claude/claude-runner.go` (235 lines) — `Run` at line 46 and `scanOutput` at line 141 are the two functions to change.
- `/workspace/claude/claude-runner_test.go` (380 lines) — the existing Ginkgo suite and the `writeShim` PATH-shim helper you must reuse.

Load-bearing snippets, verified verbatim against source.

`/workspace/claude/claude-event.go` — the full current file body:
```go
// claudeEvent represents a single event in the Claude CLI stream-json output.
type claudeEvent struct {
	Type    string    `json:"type"`
	Result  string    `json:"result"`
	Message claudeMsg `json:"message"`
}

type claudeMsg struct {
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
```

`/workspace/claude/claude-result.go` — the full current type:
```go
// ClaudeResult holds the parsed output from a Claude Code CLI session.
type ClaudeResult struct {
	Result string `json:"result"`
}
```

`/workspace/claude/claude-runner.go` lines 61 and 73-78 — the current call site and return:
```go
	resultText, tail := scanOutput(ctx, stdoutPipe)
...
	if resultText == "" {
		return nil, errors.New(ctx, "no result event found in claude CLI output")
	}

	return &ClaudeResult{Result: resultText}, nil
```

`/workspace/claude/claude-runner.go` lines 140-180 — the current `scanOutput` in full:
```go
// scanOutput reads stream-json lines from stdout, logs events, and returns the result text and a bounded tail of all non-empty lines.
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, []string) {
	var resultText string
	var tail []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return "", nil
		default:
		}

		line := scanner.Bytes()
		glog.V(4).Infof("[line] %s", line)

		tail = appendTail(tail, line)

		var event claudeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		if event.Type == "result" && event.Result != "" {
			resultText = event.Result
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
	return resultText, tail
}
```

`/workspace/claude/claude-runner_test.go` lines 29-40 — the `writeShim` helper you must reuse for the new tests (it is redeclared inside each `Describe`; follow that existing duplication pattern rather than hoisting it):
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

`scanOutput` has exactly one caller: `/workspace/claude/claude-runner.go:61`. Verified with `grep -rn "scanOutput" --include="*.go" /workspace` — the only other hit is `/workspace/pi/pi-runner.go`, which is a SEPARATE unexported function in `package pi`. Do NOT touch `/workspace/pi/`.

`ClaudeResult` is constructed in exactly one non-test place (`claude-runner.go:77`) and read in tests plus `/workspace/healthcheck/healthcheck-claude-step_test.go`. All existing construction sites use keyed literals (`&claude.ClaudeResult{Result: "..."}`), so adding fields does not break them.
</context>

<requirements>
1. **Add the usage wire types to `/workspace/claude/claude-event.go`.** Extend `claudeEvent` with the two new wire fields and add the `claudeUsage` type. The JSON keys are fixed by the CLI and must be exactly as written here:

   ```go
   // claudeEvent represents a single event in the Claude CLI stream-json output.
   type claudeEvent struct {
   	Type     string       `json:"type"`
   	Result   string       `json:"result"`
   	Message  claudeMsg    `json:"message"`
   	Usage    *claudeUsage `json:"usage"`
   	NumTurns json.Number  `json:"num_turns"`
   }

   // claudeUsage is the token accounting object the Claude CLI attaches to its
   // terminal result event. Absent or non-integer fields decode as 0.
   type claudeUsage struct {
   	InputTokens              json.Number `json:"input_tokens"`
   	OutputTokens             json.Number `json:"output_tokens"`
   	CacheCreationInputTokens json.Number `json:"cache_creation_input_tokens"`
   	CacheReadInputTokens     json.Number `json:"cache_read_input_tokens"`
   }
   ```

   `Usage` is a POINTER on purpose: a nil pointer is how "this event carried no usage object at all" is distinguished from "this event carried a usage object whose fields are all zero". That distinction is what makes requirement 3's capture gate work.

   🚨 **The token fields MUST be `json.Number`, not `int64`, and `NumTurns` MUST be `json.Number` too. This is load-bearing, not stylistic.** `scanOutput` discards the entire event on unmarshal error (`if err := json.Unmarshal(line, &event); err != nil { continue }`). With `int64` fields, a token count serialized as `100.0`, `"100"`, or `1.5` fails to decode **the whole result event** — including `Result` — so `resultText` stays empty and `Run` returns `no result event found in claude CLI output`. A successful session would be reported as a failed job. That directly violates this spec's "Absent usage is never an error and never aborts a run" and both schema-drift failure modes. `json.Number` accepts any JSON number or string-encoded number without failing the decode; convert with `.Int64()` and **on conversion error use 0** — never propagate the error. This repo routes the CLI through a non-Anthropic `ANTHROPIC_BASE_URL` shim, so divergent number formatting is a live risk, not a hypothetical.

   Add `"encoding/json"` usage accordingly — the file already imports it.

2. **Add the parse-side aggregate type `sessionUsage` to `/workspace/claude/claude-event.go`**, directly below `claudeUsage`. It carries all five captured values out of the scanner in one value:

   ```go
   // sessionUsage is the token and turn summary captured from the Claude CLI's
   // terminal result event. The zero value means no usage was reported and is a
   // valid, non-error outcome.
   type sessionUsage struct {
   	inputTokens         int64
   	outputTokens        int64
   	cacheCreationTokens int64
   	cacheReadTokens     int64
   	numTurns            int64
   }
   ```

3. **Capture usage in `scanOutput` under its own gate, separate from the result-text gate.** In `/workspace/claude/claude-runner.go`:
   - Change the signature to `func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) (string, sessionUsage, []string)`.
   - Declare `var usage sessionUsage` alongside `var resultText string`.
   - Change the context-cancellation early return from `return "", nil` to `return "", sessionUsage{}, nil`.
   - Change the final return to `return resultText, usage, tail`.
   - Leave the existing result-text capture EXACTLY as it is:
     ```go
     if event.Type == "result" && event.Result != "" {
     	resultText = event.Result
     }
     ```
   - Add a SEPARATE, adjacent block immediately after it — do not fold the two conditions together:
     ```go
     // Usage capture is deliberately gated on the presence of a usage object, NOT
     // on a non-empty result text: a later result event carrying fresh usage but an
     // empty result string must update the numbers while leaving the previously
     // captured text intact. Last usage object wins.
     if event.Type == "result" && event.Usage != nil {
     	usage = sessionUsage{
     		inputTokens:         numberToInt64(event.Usage.InputTokens),
     		outputTokens:        numberToInt64(event.Usage.OutputTokens),
     		cacheCreationTokens: numberToInt64(event.Usage.CacheCreationInputTokens),
     		cacheReadTokens:     numberToInt64(event.Usage.CacheReadInputTokens),
     		numTurns:            numberToInt64(event.NumTurns),
     	}
     }
     ```
     The turn count is read from the SAME event that carried the winning usage object — the whole five-value summary is replaced atomically, never field by field.
   - Add the conversion helper to `/workspace/claude/claude-event.go`, directly below `sessionUsage`. It is the single place where a malformed number degrades to 0 — **the error is deliberately swallowed, never returned or logged at a level that would spam**:
     ```go
     // numberToInt64 converts a JSON number to int64, yielding 0 when the value is
     // absent, non-integer, or otherwise unconvertible. Usage accounting is
     // best-effort telemetry: a malformed count must never fail the run.
     func numberToInt64(n json.Number) int64 {
     	if n == "" {
     		return 0
     	}
     	v, err := n.Int64()
     	if err != nil {
     		return 0
     	}
     	return v
     }
     ```
     Do NOT wrap this in `github.com/bborbe/errors` — it returns no error by design. This is the one sanctioned deviation from the repo's no-swallowed-errors convention, and the GoDoc above states why.
   - Update the `scanOutput` GoDoc line to mention that it also returns the captured usage summary, and keep the line under 100 characters (golines `--max-len=100` runs in `make format`). For example:
     ```go
     // scanOutput reads stream-json lines from stdout, logs events, and returns the result
     // text, the captured usage summary, and a bounded tail of all non-empty lines.
     ```

4. **Widen `ClaudeResult` in `/workspace/claude/claude-result.go`** with the five exported fields. Every new field carries a GoDoc comment. Use `omitempty` on the numeric fields so the marshalled shape of a usage-free result is byte-identical to today:

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
   	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
   	// NumTurns is the number of conversation turns the session took. Zero when the
   	// CLI reported no usage summary.
   	NumTurns int64 `json:"num_turns,omitempty"`
   }
   ```

5. **Wire the captured values through `Run` in `/workspace/claude/claude-runner.go`.** Replace line 61 and the final return:
   ```go
   	resultText, usage, tail := scanOutput(ctx, stdoutPipe)
   ```
   ```go
   	return &ClaudeResult{
   		Result:              resultText,
   		InputTokens:         usage.inputTokens,
   		OutputTokens:        usage.outputTokens,
   		CacheCreationTokens: usage.cacheCreationTokens,
   		CacheReadTokens:     usage.cacheReadTokens,
   		NumTurns:            usage.numTurns,
   	}, nil
   ```
   Everything else in `Run` is untouched: the `cmd.Wait()` error path with the tail message, and the `resultText == ""` -> `errors.New(ctx, "no result event found in claude CLI output")` guard both stay exactly as they are. Missing usage must NOT produce an error and must NOT change that guard.

6. **Add a `Describe("claudeRunner usage capture", ...)` block to `/workspace/claude/claude-runner_test.go`** (append at the end of the file, `package claude_test`). Redeclare the `writeShim` closure inside it exactly as quoted in `<context>` — that is the established pattern in this file and the only supported way to reach the unexported parser. Do NOT create an in-package `package claude` test file. Cover these six cases, one `Context` each:

   - **Malformed usage numbers must not kill the event (schema-drift guard).** Shim body:
     ```sh
     echo '{"type":"result","result":"kept-text","num_turns":"7","usage":{"input_tokens":100.0,"output_tokens":"bad","cache_read_input_tokens":50}}'
     exit 0
     ```
     Assert **no error occurred**, `result.Result == "kept-text"` (the result text survives), `CacheReadTokens == int64(50)` (the well-formed sibling still lands), and the unconvertible `output_tokens` reads `int64(0)`. `input_tokens: 100.0` and `num_turns: "7"` are both valid `json.Number` inputs — assert whatever `.Int64()` yields for each (`100.0` → conversion error → `0`; `"7"` → `7`). **This is the regression guard for the `int64`-vs-`json.Number` decision in requirement 1** — every other fixture here uses clean integers, so without this Context the failure mode is invisible and the whole suite passes while a live bug ships.

   - **Full usage (AC1).** Shim body:
     ```sh
     echo '{"type":"result","result":"task-output-text","num_turns":7,"usage":{"input_tokens":100,"output_tokens":200,"cache_creation_input_tokens":300,"cache_read_input_tokens":400}}'
     exit 0
     ```
     Assert no error, `result.Result == "task-output-text"`, and `InputTokens == 100`, `OutputTokens == 200`, `CacheCreationTokens == 300`, `CacheReadTokens == 400`, `NumTurns == 7` (use `int64` literals in the Gomega `Equal` matcher — `Equal(int64(100))`, since Gomega's `Equal` is type-strict).

   - **No usage object and no turn count (AC2).** Shim body:
     ```sh
     echo '{"type":"result","result":"plain-output"}'
     exit 0
     ```
     Assert `err` did NOT occur, `result.Result == "plain-output"`, and all five values equal `int64(0)`.

   - **Partial usage fields (AC3).** Shim body:
     ```sh
     echo '{"type":"result","result":"partial-output","usage":{"input_tokens":11,"cache_read_input_tokens":22}}'
     exit 0
     ```
     Assert `InputTokens == int64(11)`, `CacheReadTokens == int64(22)`, and `OutputTokens`, `CacheCreationTokens`, `NumTurns` all equal `int64(0)`.

   - **Two result events both carrying usage — last wins (AC4).** Shim body:
     ```sh
     echo '{"type":"result","result":"first-text","num_turns":1,"usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}'
     echo '{"type":"result","result":"second-text","num_turns":9,"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}'
     exit 0
     ```
     Assert `result.Result == "second-text"` and the five values are `10, 20, 30, 40, 9`.

   - **Usage last-wins is independent of result-text last-wins (AC5) — this is the regression guard for requirement 3.** Shim body:
     ```sh
     echo '{"type":"result","result":"kept-text","num_turns":2,"usage":{"input_tokens":5,"output_tokens":6,"cache_creation_input_tokens":7,"cache_read_input_tokens":8}}'
     echo '{"type":"result","result":"","num_turns":4,"usage":{"input_tokens":50,"output_tokens":60,"cache_creation_input_tokens":70,"cache_read_input_tokens":80}}'
     exit 0
     ```
     Assert `result.Result == "kept-text"` (the first event's text survives) AND the five values are `50, 60, 70, 80, 4` (the second event's numbers win).

   Invoke the runner the same way the existing tests do: `claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")` with `ctx := context.Background()` set in a `BeforeEach`.

7. **Do not let the CLI's cost field into the tree.** The CLI's terminal result event also carries a dollar-cost key. Do NOT add a struct field for it, do NOT put it in any test fixture, and do NOT name it in any comment or test description. The literal key name must not appear anywhere under `/workspace/claude/` — when you need to refer to it in prose, write "the CLI's cost field". The spec's acceptance check is `grep -rn 'total_cost_usd' claude/ metrics/` returning zero lines.

8. **Add a CHANGELOG entry.** In `/workspace/CHANGELOG.md`, insert an `## Unreleased` section immediately after the SemVer preamble and before the existing `## v0.79.0` heading (there is no `## Unreleased` section today — if a sibling prompt already created one, append to it instead of adding a second):
   ```markdown
   ## Unreleased

   - feat: claude: `ClaudeResult` now carries the CLI session's token counts (input, output, cache-creation, cache-read) and turn count, parsed from the terminal result event's usage summary; absent or partial usage parses as zeros without error
   ```

9. **Run `make generate` is NOT required for this prompt.** The `ClaudeRunner` interface signature is unchanged (`Run(ctx, prompt) (*ClaudeResult, error)`), so `/workspace/mocks/claude-claude-runner.go` stays valid. Note that `make precommit` runs `make generate` anyway, which wipes and regenerates `/workspace/mocks/` — that is expected and must leave no diff beyond regeneration noise.
</requirements>

<constraints>
- Do NOT parse, store, or record the CLI's cost figure. Under a non-Anthropic base URL the CLI computes it at Anthropic list pricing, so it is a counterfactual number, not money spent. (Spec Non-goal, invariant.)
- Do NOT touch `/workspace/pi/` — the Pi harness does not run Claude Code and emits no usage summary. Its identically-named unexported `scanOutput` is a different function in a different package. (Spec Non-goal.)
- Do NOT wire the new values into any consumer or metrics call site. The recording call lives in a sibling prompt and the call sites live in a separate repository. (Spec Non-goal.)
- Do NOT add a config flag, env var, or opt-out for usage capture. Capture is unconditional. (Spec Non-goal.)
- Absent usage is never an error and never aborts a run — the `resultText == ""` guard is the ONLY reason `Run` may fail on a zero-exit CLI, exactly as today. (Spec Desired Behavior 3.)
- Usage capture must NOT be folded into the `event.Result != ""` condition. Two separate `if` blocks. (Spec Desired Behavior 4 — this is called out explicitly because folding them is the natural-looking mistake.)
- A malformed JSON line is still skipped silently by the existing `json.Unmarshal` `continue` — do not change that. (Spec Failure Modes.)
- An unknown or renamed usage field must be ignored and read as 0 — that falls out of `encoding/json` defaults; do not add strict decoding (`DisallowUnknownFields`). (Spec Failure Modes.)
- Error handling stays on `github.com/bborbe/errors` with context wrapping. No `fmt.Errorf`, no bare `return err`. (Spec Constraint.)
- Every new exported type, field, method, and function carries a GoDoc comment. (Spec Constraint.)
- Tests are Ginkgo v2 / Gomega in the external `claude_test` package, reaching the unexported parser through the existing `writeShim` PATH shim. Do NOT add a `package claude` in-package test file. (Spec Constraint.)
- Coverage for the changed code (`scanOutput`, `Run`) must be >= 80%; the five new contexts exercise the full-usage, no-usage, partial-usage, duplicate-usage, and empty-text-second-event branches. Check with `go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out`.
- All existing tests must still pass unmodified — in particular the `successful CLI exit`, `CLAUDE_CONFIG_DIR env propagation`, and `AllowedTools buildCommand branch` blocks, none of which emit usage.
- Line length limit is 100 characters (golines runs in `make format`); funlen limit is 80 lines (`scanOutput` stays well under after the addition).
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# Package tests — AC1 through AC5.
cd /workspace && go test -mod=mod -race ./claude/... 2>&1 | tail -20
# Must report ok / PASS.
```

```bash
# Coverage for the changed package.
cd /workspace && go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out | grep -E 'scanOutput|claude-runner.go:.*Run'
# scanOutput must be >= 80%.
```

```bash
# The CLI's cost field must not have entered the tree.
! grep -rq 'total_cost_usd' /workspace/claude/
# Must return zero lines (exit 1).
```

```bash
# The two capture gates must be separate statements, not one folded condition.
grep -n 'event.Type == "result"' /workspace/claude/claude-runner.go
# Must return exactly 2 lines.
```

```bash
# Pi harness untouched.
grep -c 'usage\|num_turns' /workspace/pi/pi-runner.go
# Must return 0.
```

```bash
# Changelog entry present.
grep -n -A5 '## Unreleased' /workspace/CHANGELOG.md | grep -iE 'token|turn'
# Must return at least one line.
```

```bash
# Final full validation at the repository root.
cd /workspace && make precommit
# Must exit 0.
```
</verification>
