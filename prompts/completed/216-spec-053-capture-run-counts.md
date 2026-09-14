---
status: completed
spec: [053-agent-result-interaction-count]
summary: Capture the Claude session id and human-authored transcript count in the runner, carry both through Result/AgentResultInfo with absence-never-zero rules
execution_id: agent-exec-216-spec-053-capture-run-counts
dark-factory-version: v0.193.0
created: "2026-09-14T16:09:52Z"
queued: "2026-09-14T17:54:09Z"
started: "2026-09-14T18:42:45Z"
completed: "2026-09-14T18:52:01Z"
branch: dark-factory/agent-result-interaction-count
---

<summary>
- Every Claude session already reports its turn total in the end-of-run summary the runner parses; that number now travels out of the runner instead of being discarded
- The runner also captures the session id the CLI reports in its stream and uses it to find that session's own transcript on disk
- The transcript is read only to count the entries the CLI marks as human-authored: zero when there are none (that recorded zero is the unattended-delivery claim), absent when the evidence cannot be read
- Absence is never turned into a zero: no session id, no transcript, or an unreadable or unparseable transcript produces no count at all
- Both numbers travel with the step's result and reach the deliverer on the result info the framework publishes, for every status a step can produce after a session ran
- A turn total that is zero or negative counts as no measurement and is omitted entirely
- The pi provider is untouched — it has no Claude session and therefore produces neither number
- No config field, no flag, no opt-out: the capture is unconditional
- Writing the numbers into the published task frontmatter is a sibling prompt; this one only produces and carries them
</summary>

<objective>
Make a Claude-backed step produce and carry the run's two observed counts — the session's turn total from the CLI's end-of-run summary and the run's human interaction count evidenced from the session's own transcript — so the result a step hands to the framework already holds both numbers with the absence rules applied. Implements spec 053 Desired Behaviors 2-5 and the carrier half of 8; publishing them into the task frontmatter is the sibling prompt.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single-module repo at the repository root, `module github.com/bborbe/agent`; `make test` / `make precommit` run from the repository root, never from a subdir).

Coding-plugin docs (read before editing, paths as they exist INSIDE the YOLO container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suites, external test packages (`*_test`), counterfeiter mocks, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments for every new exported field/function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-security-linting.md` — gosec expectations for file reads (`#nosec` with a reason).
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md` — the non-blocking context check inside a read loop.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — funlen 80, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (all paths repo-relative):
- `claude/claude-runner.go` — the file that runs the CLI. `Run` (starts at the `func (r *claudeRunner) Run` line) calls `scanOutput`, waits for the process, and returns the `ClaudeResult`. `scanOutput` reads the stream-json lines. `buildSubprocessEnv` resolves the `CLAUDE_CONFIG_DIR` value with the precedence config > parent env > `~/.claude`.
- `claude/claude-event.go` — the stream-json wire types (`claudeEvent`, `resultHolder`, `sessionUsage`) and `numberToInt64`.
- `claude/claude-result.go` — the `ClaudeResult` type returned by `ClaudeRunner.Run`.
- `claude/claude-runner_test.go` — the existing Ginkgo suite (`package claude_test`); it reaches the unexported scanner only through the PATH-shim helper `writeShim` (redeclared inside each `Describe` — follow that existing duplication pattern rather than hoisting it).
- `claude/agent-step.go` — `agentStep.Run` is the caller of `ClaudeRunner.Run` this prompt changes; it has three return paths after the runner call (done, agent-reported needs_input/failed body, runner error). The other two in-repo callers (`claude/task-runner.go`, `healthcheck/healthcheck-claude-step.go`) are out of scope: the capture lives in the runner and reaches all three, but neither other caller carries per-run counts.
- `claude/agent-step_test.go` — the `Describe("AgentStep")` suite with its `Describe("Run")` block; uses the counterfeiter mock `libmocks.ClaudeRunner`.
- `agent_step.go` (repository root) — the `Step` interface and the `Result` struct a step returns.
- `agent_status.go` (repository root) — `AgentStatus` constants and `AgentResultInfo`, the struct a deliverer receives.
- `agent_runner.go` (repository root) — `StepRunner.Run` builds the `AgentResultInfo` from the step's `Result`.
- `agent_runner_test.go` (repository root) — the `Describe("StepRunner")` suite; uses the counterfeiter mocks `mocks.AgentResultDeliverer` and `mocks.AgentStep`.
- `specs/in-progress/053-agent-result-interaction-count.md` — the spec this prompt implements (Desired Behavior, Constraints, Failure Modes, Security sections).

Load-bearing facts, verified against this repo and the spec (do not re-derive, do not contradict):

1. `claudeEvent` (in `claude/claude-event.go`) currently has no session-id field. The CLI reports its session id on every event of a headless run — including the init event and the terminal result event — under the JSON key `session_id` (spec 053, "What the run can observe", fact 1).
2. The CLI writes the session transcript under the config directory the runner already sets for it, as `<config-dir>/projects/<encoded-cwd>/<session-id>.jsonl` (spec fact 2). The `<encoded-cwd>` segment is a CLI implementation detail (it encodes the session's working directory); the spec pins the `<session-id>.jsonl` file name, not the encoding. This prompt therefore locates the transcript by matching the exact file name `<session-id>.jsonl` in any single directory directly under `<config-dir>/projects/` instead of computing the encoded working directory — the read stays confined to the config directory either way, and no unverified encoding rule is baked in. **Open question for the reviewer (audit time):** if you prefer computing `<encoded-cwd>` from the runner's working directory (replace `/` and `.` with `-`), say so at audit — the glob lookup is the deliberately chosen, more robust alternative.
3. The CLI marks provenance on user-role transcript entries: human-typed input carries a human origin (`origin.kind: human`, typically alongside a prompt source of `typed` / `queued`); a machine-delivered prompt carries a prompt source of `sdk` and no human origin; tool results carry neither (spec fact 3). The spec pins the marker value `human`, not the exact nesting, so the scan reads the marker from the entry's own `origin` object and, as a fallback, from the nested `message.origin` object.
4. `github.com/bborbe/collection` is already a dependency (`v1.20.26`) and provides `collection.Ptr[T any](value T) *T`. This repo's convention is to use it rather than hand-writing pointer helpers (see `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`, "Pointer utilities"). `github.com/bborbe/agent/agent_task-identifier.go` already imports the package unaliased.
5. `errcheck` in `.golangci.yml` excludes `(*os.File).Close`, so a plain `defer file.Close()` is correct here (the same pattern is used in `github.com/bborbe/kafka`).
6. `ClaudeResult` is constructed only in `claude/claude-runner.go` — three keyed literals: the `cmd.Wait()` error return, the `resultText == ""` guard, and the successful final return — plus test fixtures using keyed literals; adding fields breaks nothing.
7. The `ClaudeRunner` interface (`Run(ctx context.Context, prompt string) (*ClaudeResult, error)`) MUST NOT change: it is consumed by other repositories and its counterfeiter mock (`mocks/claude-claude-runner.go`) is generated from that exact signature.
8. `Result` (`agent_step.go`) and `AgentResultInfo` (`agent_status.go`) are structs; adding fields is additive and breaks no caller. They are constructed with keyed literals everywhere in this repo.
</context>

<requirements>

## 1. Capture the session id from the stream — `claude/claude-event.go` and `claude/claude-runner.go`

In `claude/claude-event.go`, add the session-id wire field to `claudeEvent`:

```go
// claudeEvent represents a single event in the Claude CLI stream-json output.
type claudeEvent struct {
	Type      string          `json:"type"`
	Result    string          `json:"result"`
	Message   claudeMsg       `json:"message"`
	Usage     json.RawMessage `json:"usage"`
	NumTurns  json.Number     `json:"num_turns"`
	SessionID string          `json:"session_id"`
}
```

The JSON key is fixed by the CLI and must be exactly `session_id`. Do NOT add the field to `resultHolder` and do NOT change `sessionUsage`.

In `claude/claude-runner.go`, `scanOutput` gains a fifth return value, the session id. Its signature becomes:

```go
func scanOutput(
	ctx context.Context,
	reader interface{ Read([]byte) (int, error) },
) (string, sessionUsage, string, []string, string)
```

returning `(resultText, usage, partial, tail, sessionID)` in that order. Update the GoDoc line accordingly and keep every line under 100 characters.

- Declare `var sessionID string` alongside `var resultText string`.
- Capture it next to the existing usage-capture block, inside the same loop, after the full `json.Unmarshal`:

```go
		if event.SessionID != "" {
			sessionID = event.SessionID
		}
```

  Last non-empty value wins, exactly like the usage summary. Do NOT fold this into the usage block and do NOT gate it on `event.Type == "result"` — the init event carries the id too.
- Both early returns inside `scanOutput` (the context-cancellation return and the final return) must return the values captured so far, including `sessionID`.
- Update the single caller in `Run`: `resultText, usage, partial, tail, sessionID := scanOutput(ctx, stdoutPipe)`.

## 2. Extract the config-dir resolution — `claude/claude-runner.go`

`buildSubprocessEnv` currently resolves the config directory inline (its "Layer 2"). Extract that resolution into a method so the transcript scan can use the exact same value, and call it from `buildSubprocessEnv`:

```go
// resolveConfigDir returns the Claude config directory the CLI subprocess is given,
// applying the same precedence buildSubprocessEnv applies: explicit config > parent
// process env > default "~/.claude", with a consumer-provided Env override winning.
func (r *claudeRunner) resolveConfigDir(ctx context.Context) (string, error) {
	cfgDir := r.config.ClaudeConfigDir
	if cfgDir == "" {
		if envVal := os.Getenv("CLAUDE_CONFIG_DIR"); envVal != "" {
			cfgDir = ClaudeConfigDir(envVal)
		}
	}
	if cfgDir == "" {
		cfgDir = "~/.claude"
	}
	// The highest-precedence layer of buildSubprocessEnv: a consumer-provided Env
	// override. The transcript is written where the CLI was told to write it, so the
	// scan follows the same value. (For an override containing "~" the subprocess gets
	// the literal string while this resolves it; the scan then finds no transcript and
	// the count stays absent — never wrong.)
	if override, ok := r.config.Env["CLAUDE_CONFIG_DIR"]; ok && override != "" {
		cfgDir = ClaudeConfigDir(override)
	}
	resolved, err := cfgDir.Resolve(ctx)
	if err != nil {
		return "", errors.Wrap(ctx, err, "resolve ClaudeConfigDir")
	}
	return resolved, nil
}
```

`buildSubprocessEnv` keeps its observable behavior byte for byte: the five existing `Context` blocks in the `claudeRunner CLAUDE_CONFIG_DIR env propagation` Describe in `claude/claude-runner_test.go` must pass unmodified. In `buildSubprocessEnv`, replace the inline "Layer 2" resolution block with a call to this method and keep the comment explaining the precedence.

## 3. Scan the session's own transcript — new file `claude/transcript.go`

Create `claude/transcript.go` (`package claude`, license header, Ginkgo-free production code) with exactly these unexported primitives.

Constants:

```go
// humanOriginKind is the provenance marker the Claude CLI writes on user-role
// transcript entries for human-typed input ("origin.kind": "human"). A
// machine-delivered prompt carries no such marker and tool results carry neither,
// so this marker is the whole discriminator between a human intervention and
// machine-driven traffic (spec 053).
const humanOriginKind = "human"

// maxSessionIDLength bounds the session id accepted as a file-name component.
const maxSessionIDLength = 128

// maxTranscriptLineBytes bounds a single transcript line the scanner will read.
const maxTranscriptLineBytes = 10 * 1024 * 1024
```

```go
// isValidSessionID reports whether sessionID is safe to use as a file-name component
// under the config directory: 1..maxSessionIDLength characters from [A-Za-z0-9-].
// Path separators, traversal sequences, glob metacharacters and whitespace are all
// rejected — the id arrives from a subprocess and is treated as untrusted input.
func isValidSessionID(sessionID string) bool
```

```go
// findTranscriptFile returns the path of the session's transcript under
// <configDir>/projects. The CLI writes it as
// <config-dir>/projects/<encoded-cwd>/<session-id>.jsonl; the encoded-cwd segment is
// a CLI implementation detail, so the lookup matches the exact file name
// <session-id>.jsonl in any single directory directly under projects/. The session id
// is validated first and the returned path is always inside <configDir>/projects — a
// rejected id, a missing projects directory, or anything other than exactly one match
// returns ok=false.
func findTranscriptFile(configDir, sessionID string) (string, bool)
```

Implement `findTranscriptFile` with `filepath.Glob(filepath.Join(configDir, "projects", "*", sessionID+".jsonl"))` after the `isValidSessionID` gate, and return `ok=false` when the glob errors or does not yield exactly one match (an ambiguous match is not decisive evidence). The `*` wildcard never crosses a path separator, so the read stays one level under `projects/`.

```go
// transcriptEntry is the subset of a transcript line the scan reads: the entry kind
// and the human-provenance marker. Nothing else is decoded, logged, published, or
// stored.
type transcriptEntry struct {
	Type    string            `json:"type"`
	Origin  *transcriptOrigin `json:"origin"`
	Message *transcriptMsg    `json:"message"`
}

// transcriptOrigin is the provenance object the CLI attaches to an entry.
type transcriptOrigin struct {
	Kind string `json:"kind"`
}

// transcriptMsg is the nested message object, which may carry its own provenance.
type transcriptMsg struct {
	Origin *transcriptOrigin `json:"origin"`
}
```

```go
// isHumanAuthoredEntry reports whether a transcript line is a user-role entry the CLI
// recorded as human-authored. The marker is read from the entry's own origin object
// and, as a fallback, from the nested message's origin object — the spec pins the
// marker value `human`, not its exact nesting. A line that does not parse returns an
// error: the caller treats an unparseable transcript as unavailable evidence, never
// as an observation.
func isHumanAuthoredEntry(line []byte) (bool, error)
```

Body contract: unmarshal into `transcriptEntry` (return the error on failure); return `false, nil` when `entry.Type != "user"`; return `true, nil` when either `entry.Origin != nil && entry.Origin.Kind == humanOriginKind` or `entry.Message != nil && entry.Message.Origin != nil && entry.Message.Origin.Kind == humanOriginKind`; otherwise `false, nil`.

```go
// countHumanAuthoredEntries counts the user-role entries the session's own transcript
// records as human-authored. ok is false whenever the evidence is not decisive — an
// invalid or missing session id, a transcript that cannot be located, opened, read to
// the end, or parsed. Callers must then write nothing at all: an unobserved zero would
// manufacture an unattended-delivery claim (spec 053). A (0, true) result is an
// observation: the transcript was read in full and held no human-authored entry.
func countHumanAuthoredEntries(
	ctx context.Context,
	configDir ClaudeConfigDir,
	sessionID string,
) (int64, bool)
```

Body contract, in order:

1. `path, ok := findTranscriptFile(configDir.String(), sessionID)`; `if !ok { return 0, false }`.
2. `file, err := os.Open(path)` — this is an untrusted-path read, so carry the repo's existing gosec idiom: `// #nosec G304 -- path confined to the config dir; session id validated` (mirrors `delivery/result-deliverer.go`). On error return `0, false`; `defer file.Close()` on success.
3. Scan with `bufio.NewScanner(file)` and `scanner.Buffer(make([]byte, 0, 1024*1024), maxTranscriptLineBytes)` — the runner uses the same bounded-buffer idiom for the CLI stream, and a transcript line can be a large tool result.
4. Inside the loop, first do the non-blocking context check (`if ctx.Err() != nil { return 0, false }`), then `line := strings.TrimSpace(scanner.Text())`; skip an empty line; call `isHumanAuthoredEntry([]byte(line))`; on error return `0, false`; otherwise increment the counter when it returns true.
5. After the loop, `if err := scanner.Err(); err != nil { return 0, false }`, then `return count, true`.

Never log, publish, or store transcript content: `claude/transcript.go` performs no logging at all (only the deliverer logs, and only whether a recorded value was kept). Do NOT wrap any of these "unavailable" returns in an error: absence is a first-class outcome, not a failure.

## 4. Run the scan after the session exits — `claude/claude-runner.go`

Add this method next to `resolveConfigDir`:

```go
// countHumanInteractions resolves the Claude config directory and counts the
// human-authored entries in the session's own transcript. It returns nil — never a
// zero — when the evidence is unavailable: no session id, an unresolvable config
// directory, or a transcript that cannot be located, read, or parsed.
func (r *claudeRunner) countHumanInteractions(ctx context.Context, sessionID string) *int64 {
	if sessionID == "" {
		return nil
	}
	configDir, err := r.resolveConfigDir(ctx)
	if err != nil {
		return nil
	}
	count, ok := countHumanAuthoredEntries(ctx, ClaudeConfigDir(configDir), sessionID)
	if !ok {
		return nil
	}
	return collection.Ptr(count)
}
```

Import `github.com/bborbe/collection` in `claude/claude-runner.go` (unaliased, same as `agent_task-identifier.go`).

Call it from the successful final return of `Run` (the `return &ClaudeResult{...}` that already sets `Result`, `Partial`, and the usage fields) — the session has exited by then (`cmd.Wait()` has returned), so there is no partial-file race:

```go
	return &ClaudeResult{
		Result:              resultText,
		Partial:             partial,
		InputTokens:         usage.inputTokens,
		OutputTokens:        usage.outputTokens,
		CacheCreationTokens: usage.cacheCreationTokens,
		CacheReadTokens:     usage.cacheReadTokens,
		NumTurns:            usage.numTurns,
		SessionID:           sessionID,
		InteractionCount:    r.countHumanInteractions(ctx, sessionID),
	}, nil
```

The two earlier returns of `Run` (the `cmd.Wait()` error path and the `resultText == ""` guard) keep returning no counts — a run whose session never produced a result has no numbers, and its numbers must stay absent, never zero. `Run` must stay under the funlen limit of 80 lines.

## 5. Widen `ClaudeResult` — `claude/claude-result.go`

Add the two fields, each with a GoDoc comment (append after `NumTurns`, keep gofmt alignment):

```go
	// SessionID is the CLI session identifier the stream reported. Empty when the CLI
	// reported no session id.
	SessionID string `json:"session_id,omitempty"`
	// InteractionCount is the number of human-authored entries the session's own
	// transcript recorded. Nil when the evidence was unavailable — no session id, no
	// transcript, or an unreadable or unparseable transcript. A non-nil zero is an
	// observation: the transcript was read and contained no human-authored entry.
	InteractionCount *int64 `json:"interaction_count,omitempty"`
```

`omitempty` on a pointer omits only nil — a pointer to zero is still marshalled, which is exactly the "recorded zero is a claim" contract.

## 6. Carry the counts on `Result` — `agent_step.go` (repository root)

Add two optional fields to `Result`, each with a GoDoc comment, after `ContinueToNext`:

```go
	// AgentTurns is the number of conversation turns the session that produced this
	// result took, from the CLI's own end-of-run summary. Nil means no measurement was
	// reported — a zero or negative summary is treated as no measurement, and the turn
	// entry is then omitted from the payload entirely.
	AgentTurns *int64

	// InteractionCount is the number of human-authored entries the run's own session
	// transcript recorded. Nil means the evidence was unavailable — the interaction
	// entry is omitted from the payload. A non-nil zero is an observation: the
	// transcript was read and contained no human-authored entry. Absence is never
	// substituted with a zero.
	InteractionCount *int64
```

Do NOT change any existing field, and do NOT make these required.

## 7. Carry the counts on `AgentResultInfo` — `agent_status.go` (repository root)

Add the same two fields with the same semantics to `AgentResultInfo` (the struct a deliverer receives), each with a GoDoc comment, after `ContinueToNext`. Keep the wording aligned with requirement 6; this struct is the one the publishing prompt consumes.

## 8. Propagate through the framework — `agent_runner.go` (repository root)

In `StepRunner.Run`, add the two fields to the `AgentResultInfo` literal it delivers — a straight pass-through, no interpretation:

```go
		if err := r.deliverer.DeliverResult(ctx, AgentResultInfo{
			Status:           result.Status,
			Output:           newContent,
			Message:          result.Message,
			NextPhase:        result.NextPhase,
			ContinueToNext:   result.ContinueToNext,
			AgentTurns:       result.AgentTurns,
			InteractionCount: result.InteractionCount,
		}); err != nil {
```

Nothing else in `StepRunner.Run` changes.

## 9. Set the counts in the Claude step — `claude/agent-step.go`

Add this helper to `claude/agent-step.go` (import `github.com/bborbe/collection` if it is not already imported):

```go
// runCounts extracts the run's observed counts from the CLI result with the absence
// rules: a turn total that is not positive is no measurement (nil), and a nil
// InteractionCount (evidence unavailable) stays nil — absence is never turned into a
// zero (spec 053).
func runCounts(result *ClaudeResult) (agentTurns *int64, interactionCount *int64) {
	if result == nil {
		return nil, nil
	}
	if result.NumTurns > 0 {
		agentTurns = collection.Ptr(result.NumTurns)
	}
	return agentTurns, result.InteractionCount
}
```

In `agentStep.Run`, immediately after the successful runner call (i.e. after the `if runErr != nil { ... }` block that returns the failed result), add:

```go
	agentTurns, interactionCount := runCounts(result)
```

and set both fields on the two `&agentlib.Result{...}` literals that follow it:

- the agent-reported needs_input/failed body path (`return &agentlib.Result{Status: parsed.Status, Message: msg}`) — this is the "agent-reported failed run still carries both numbers" case (spec Desired Behavior 8);
- the success path (`return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: s.cfg.NextPhase}`).

The runner-error path (`if runErr != nil`) stays exactly as it is: no numbers, no zero.

Do NOT touch `claude/task-runner.go` or `claude/result-deliverer.go`. The single-shot `TaskRunner` path delivers through `ResultDeliverer[T AgentResultLike]`, whose interface carries no per-run numbers; extending it is out of scope for this spec (it would be a breaking interface change for other repositories).

## 10. Tests — `claude/claude-runner_test.go` (new `Describe` block)

Append a new `Describe("claudeRunner interaction count capture", ...)` block at the end of the file (`package claude_test`), redeclaring the `writeShim` closure exactly as the existing blocks do — that is the established pattern in this file and the only supported way to reach the unexported scanner.

Each row writes its own transcript into the config dir the runner was given, using the subprocess's own `$CLAUDE_CONFIG_DIR` (the runner sets it; verified in the existing `CLAUDE_CONFIG_DIR env propagation` block). The shim writes the transcript first, then emits the stream:

```go
		writeShim(`mkdir -p "$CLAUDE_CONFIG_DIR/projects/-tmp"
printf '%s\n' '{"type":"user","promptSource":"sdk","message":{"role":"user","content":"do the thing"}}' '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}' > "$CLAUDE_CONFIG_DIR/projects/-tmp/sess-abc.jsonl"
echo '{"type":"system","subtype":"init","session_id":"sess-abc"}'
echo '{"type":"result","result":"task-output-text","num_turns":7,"session_id":"sess-abc","usage":{"input_tokens":1,"output_tokens":2}}'
exit 0`)
```

Drive the runner as `claude.NewClaudeRunner(claude.ClaudeRunnerConfig{ClaudeConfigDir: claude.ClaudeConfigDir(<the temp dir>)}).Run(ctx, "test")` with `ctx := context.Background()` in a `BeforeEach`, and `<the temp dir>` from `GinkgoT().TempDir()`.

Rows (one `Context` each, `It` names as given — the spec's AC evidence names these behaviors):

1. **"captures the session id from the stream"** — the fixture above; assert `result.SessionID == "sess-abc"`.
2. **"counts zero for a transcript whose user entries are all machine-marked"** (spec AC, K = 0) — the fixture above (an `sdk`-sourced prompt plus a tool result, no human marker); assert `result.InteractionCount` is not nil and `*result.InteractionCount == int64(0)`. This is the unattended-delivery claim: an observed zero, not an absent key.
3. **"counts human-marked user entries"** (spec AC, K = 2) — a transcript holding two human-marked entries (`{"type":"user","origin":{"kind":"human"},"promptSource":"typed",...}` and `{"type":"user","origin":{"kind":"human"},"promptSource":"queued",...}`) plus a tool result; assert `*result.InteractionCount == int64(2)`. A hardcoded zero fails this row.
4. **"leaves the count absent when the transcript cannot be located"** — the shim emits the stream but writes no transcript file; assert `result.InteractionCount` is nil while `result.NumTurns == int64(7)` (the turn count is unaffected).
5. **"leaves the count absent when the transcript cannot be parsed"** — a transcript whose second line is not JSON; assert `result.InteractionCount` is nil.
6. **"leaves the count absent when the CLI reported no session id"** — a transcript file exists but the stream carries no `session_id`; assert `result.SessionID == ""` and `result.InteractionCount` is nil.
7. **"rejects a session id that is not a plain identifier"** — the stream reports `"session_id":"../../escape"`; assert `result.InteractionCount` is nil. The id is validated before any path is built, so nothing outside the config directory can be read; the fixture needs no file outside the config dir.

Gomega `Equal` is type-strict: use `int64` literals (`Equal(int64(7))`) and always dereference the pointer after asserting `NotTo(BeNil())`.

## 11. Tests — `claude/agent-step_test.go`

Extend the existing `Describe("AgentStep")` → `Describe("Run")` block with a new `Context` (use the existing `libmocks.ClaudeRunner`). The stub returns a `ClaudeResult` like this:

```go
mockRunner.RunReturns(&claude.ClaudeResult{
	Result:           `{"status":"done","message":"analysis complete"}`,
	NumTurns:         7,
	InteractionCount: collection.Ptr(int64(0)),
}, nil)
```

Rows (one `It` each, names as given):

1. **"carries the turn total and the evidenced interaction count on a done result"** — the stub above; assert `result.AgentTurns` is not nil with `*result.AgentTurns == int64(7)` and `*result.InteractionCount == int64(0)`. This is the spec's AC2 row ("a step whose runner reports N turns delivers a result carrying N") — the concrete-type assertion lives here, where the value enters the payload as an `int64`.
2. **"carries both counts on an agent-reported failed body"** — stub with the result body `{"status":"failed","message":"claude CLI crashed"}`, `NumTurns: 5`, `InteractionCount: collection.Ptr(int64(2))`; assert `result.Status == lib.AgentStatusFailed` and both counts are carried (spec Desired Behavior 8).
3. **"omits the turn count when the summary reported none"** — stub `NumTurns: 0, InteractionCount: nil`; assert `result.AgentTurns` is nil and `result.InteractionCount` is nil (absence, not zero).
4. **"treats a negative turn total as no measurement"** — stub `NumTurns: -3`; assert `result.AgentTurns` is nil.

The existing runner-error row in this block (`mockRunner.RunReturns(nil, errors.New("claude CLI crashed"))`) must keep passing unchanged — a run whose session never produced a result carries no counts.

## 12. Tests — `agent_runner_test.go` (repository root)

Add two rows to the existing `Describe("StepRunner")` → `Describe("Run")` block (use the existing `mocks.AgentStep` and `mocks.AgentResultDeliverer`):

1. **"forwards the run's observed counts to the deliverer"** — `step.RunReturns(&lib.Result{Status: lib.AgentStatusDone, AgentTurns: collection.Ptr(int64(7)), InteractionCount: collection.Ptr(int64(0))}, nil)`; assert `deliverer.DeliverResultCallCount() == 1` and, from `deliverer.DeliverResultArgsForCall(0)`, that `info.AgentTurns` is not nil with `*info.AgentTurns == int64(7)` and `*info.InteractionCount == int64(0)`.
2. **"forwards absence as absence"** — a `Result` with both fields nil; assert `info.AgentTurns` and `info.InteractionCount` are both nil (a nil pointer must never be turned into a zero on the way through the framework).

Import `github.com/bborbe/collection` in both test files (unaliased) for `collection.Ptr`.

## 13. Scope containment

Edit ONLY these files:
- `claude/claude-event.go`
- `claude/claude-result.go`
- `claude/claude-runner.go`
- `claude/transcript.go` (new)
- `claude/claude-runner_test.go`
- `claude/agent-step.go`
- `claude/agent-step_test.go`
- `agent_step.go`
- `agent_status.go`
- `agent_runner.go`
- `agent_runner_test.go`

Do NOT touch `claude/task-runner.go`, `claude/result-deliverer.go`, `pi/`, `metrics/`, `delivery/`, `healthcheck/`, `mocks/` (the `ClaudeRunner` interface signature is unchanged, so no mock regeneration is needed — `make generate` will still wipe and regenerate `mocks/` during `make precommit`, which is expected), or `CHANGELOG.md` (a sibling prompt owns the changelog entry for this spec — do not add one here).

## 14. Self-check before finishing

Re-run the `<verification>` commands and confirm each passes; then walk spec 053's Desired Behaviors 2-5 and the carrier half of 8 and Failure Modes rows against the change, and confirm each of these explicitly:

- A run whose transcript holds no human-authored entry yields an observed `0`; a run whose evidence is unavailable yields nil (absent), never `0`.
- A zero or negative turn total yields no turn count at all.
- No code path turns an unavailable evidence into a zero.
- No transcript content is logged, published, or stored; only the count is.
- The `ClaudeRunner` interface signature is unchanged.
</requirements>

<constraints>
- Two frozen frontmatter keys exist for this spec — `metrics_agent_turns` (new) and `metrics_interaction_count` (shipped) — but this prompt writes NEITHER into any frontmatter; it only produces and carries the numbers. Do not invent key names here.
- **Evidence, not assertion.** The interaction count is written from the run's own session transcript, counting the user-role entries the CLI records as human-authored. `0` is produced only when the transcript was read and contained no such entry. No code path may produce a zero it did not observe.
- **Absent is a first-class outcome.** Missing evidence yields no value at all — never a zero, never an error that fails the run.
- **Transcript handling is untrusted-input handling.** The session id arrives from a subprocess: validate it before it is used in a path, read only inside the run's own config directory, and never log, publish, or persist transcript content. Only non-negative integers leave the scan.
- **Claude provider only.** The pi provider has no turn counter and no Claude session transcript; it must keep producing neither number (its `Step` leaves the new fields nil).
- **No configuration surface.** No env var, flag, config field, or opt-out disables or tunes the capture — recording is unconditional.
- **Additive only.** The `ClaudeRunner` interface signature, `AgentStatus` values, `Markdown`, and every existing field of `Result` / `AgentResultInfo` / `ClaudeResult` stay exactly as they are. Existing tests that assert today's shapes keep passing unmodified.
- The Prometheus path is untouched: neither key name, nor either number, may appear under `metrics/` — `agent_job_turns_total` keeps its meaning, name, labels, and data source.
- Error handling uses `github.com/bborbe/errors` (`errors.Wrap` / `errors.Wrapf`) where an error is returned; the "unavailable evidence" returns are not errors by design and must not be wrapped or logged as failures.
- Ginkgo v2 / Gomega, external test packages (`claude_test`, `lib_test`), counterfeiter mocks only (`libmocks.ClaudeRunner`, `mocks.AgentStep`, `mocks.AgentResultDeliverer`) — never hand-write mocks. Do NOT add an in-package `package claude` test file.
- Coverage for the new code (`transcript.go`, the new `claudeRunner` methods, `runCounts`) must be >= 80%; the rows above exercise the human-marked, machine-marked, missing-file, unparseable, no-session-id and invalid-id branches.
- Line length limit is 100 characters (golines runs in `make format`); funlen limit is 80 lines.
- Do NOT commit — dark-factory handles git.
- Do NOT touch `CHANGELOG.md` — a sibling prompt of this spec owns the changelog entry.
</constraints>

<verification>
Run from the repository root (single-module repo; there is no `claude/Makefile`).

```bash
# 1. Targeted package tests (fast iteration — run after each edit):
go test -mod=mod ./claude/ -v > /tmp/claude-test.log 2>&1
# Must exit 0. The new Ginkgo rows must appear in the output:
grep -E "captures the session id from the stream|counts zero for a transcript whose user entries are all machine-marked|counts human-marked user entries|leaves the count absent when the transcript cannot be located|leaves the count absent when the transcript cannot be parsed|leaves the count absent when the CLI reported no session id|rejects a session id that is not a plain identifier|carries the turn total and the evidenced interaction count|carries both counts on an agent-reported failed body|omits the turn count when the summary reported none|treats a negative turn total as no measurement" /tmp/claude-test.log
# Each pattern must match at least one line.

# 2. Root-package tests (the StepRunner forwarding rows):
go test -mod=mod . -v > /tmp/root-test.log 2>&1
# Must exit 0.
grep -E "forwards the run's observed counts to the deliverer|forwards absence as absence" /tmp/root-test.log
# Each pattern must match at least one line.

# 3. Coverage for the changed package:
go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out | grep -E 'transcript.go|claude-runner.go'
# The new transcript.go functions and the new claudeRunner methods must be >= 80%.

# 4. The capture is unconditional — no config surface was added:
! grep -rn 'InteractionCount\|AgentTurns\|SessionID' /workspace/claude/claude-runner-config.go
# Must return zero lines. Plain grep exits 1 when nothing matched; the leading `!` inverts
# that to success (exit 0), and fails the step when a line is found.

# 5. Nothing was written into the metrics package:
! grep -rn 'metrics_agent_turns\|metrics_interaction_count' /workspace/metrics/
# Must return zero lines.

# 6. No transcript content is logged — the scan logs at most a count:
! grep -q 'glog' /workspace/claude/transcript.go
# Must print nothing — the negated grep exits 0 when the file has no logging.

# 7. Full pipeline (must exit 0; runs ensure + format + generate + test + lint + license):
make precommit
# The Makefile derives ROOTDIR from `git rev-parse --show-toplevel`; this repo runs with
# workflow: direct and hideGit unset, so .git is present and ROOTDIR resolves. If the
# derivation ever returns empty, re-run as: ROOTDIR=/workspace make precommit
```
</verification>
