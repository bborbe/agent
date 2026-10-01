---
status: approved
spec: [055-claude-streaming-session]
created: "2026-10-01T06:05:14Z"
queued: "2026-10-01T07:36:06Z"
---

<summary>
- A Claude conversation can now stay alive across requests instead of being thrown away and rebuilt for every prompt
- One long-running Claude process per session id serves every turn; the conversation lives in that process, not in a file re-read between turns
- A second prompt on the same session reaches the same process, so whatever the first turn was told is still in front of the model
- When Claude wants to use a tool mid-turn, the request leaves the process and the turn pauses on a real answer instead of hanging or auto-approving
- If the held process has already died, the next prompt fails visibly rather than quietly starting a blank conversation behind the caller's back
- The process is cleaned up when the session is closed and when an in-flight turn is cancelled, so a pod cannot accumulate orphaned Claude processes
- The spawn options reuse the configuration the existing Claude runner already carries — there is no second config to keep in sync and no new flag pair
- The existing one-shot Claude runner is untouched: same interface, same callers, same behaviour
- The wire protocol this depends on — the message shapes, how a turn is known to be finished, and what a permission request looks like — is written down in the repository so it does not live only inside the implementation
- Nothing secret is baked into the tests: no probe word, no credential, and the real end-to-end check against the actual CLI binary is run by the operator on the host, not in the build container
</summary>

<objective>
Add a Claude session implementation to `github.com/bborbe/agent` that satisfies the shared session interface landed by spec 054 and holds ONE long-lived `claude` process per session, driven over the CLI's stream-json protocol on stdio: each turn is written to that process's stdin, events are read back until the turn's terminal event, tool-permission requests raised mid-turn are routed out to a decider seam and the turn waits on that answer, and the process is terminated on `Close` and on cancellation of an in-flight turn. Implements spec 055 Desired Behaviors 1-4 and Acceptance Criteria 1 and 3. Acceptance Criterion 2 is operator-executable only (the real `claude` binary is not present in the build container) and is recorded, not run, here.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (read before editing, paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named as the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), the `//go:generate` counterfeiter directive, the mandatory suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; `no-fmt-errorf`, `no-bare-return-err`, `no-context-background-in-business-logic`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — the `no-raw-go-func` rule: raw `go func(){...}()` outside `main.go` / `cmd/**` is a MUST-violation; use `github.com/bborbe/run` if a goroutine is genuinely needed.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md` — counterfeiter mocks for every new interface, generated into `mocks/`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every new exported type, field, and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing (repo-relative):
- `claude/claude-runner.go` — the existing one-shot runner. `Run` builds the command, spawns `claude --print`, scans stdout once, waits, returns. `buildCommand` builds the argv. `buildSubprocessEnv` and `resolveConfigDir` are methods on the unexported `claudeRunner`. This file is the pattern source for argv building, env building, and the bounded stdout scanner — but its spawn shape (one process per call, `bytes.NewBufferString(prompt)` on stdin, no `--input-format`) is exactly what this spec replaces for the session path.
- `claude/claude-runner-config.go` — `ClaudeRunnerConfig`, the configuration the session must reuse (Desired Behavior 4).
- `claude/claude-event.go` — the stream-json wire types (`claudeEvent`, `resultHolder`, `claudeMsg`, `claudeContent`, `sessionUsage`) and `numberToInt64`. Reuse these for parsing the session's stdout; do not fork a parallel event vocabulary.
- `claude/claude-result.go` — `ClaudeResult`, the parsed result shape.
- `claude/claude-runner_test.go` — the existing Ginkgo suite (`package claude_test`). It reaches the CLI boundary through the `writeShim` closure (a shell script named `claude` written into a temp dir prepended to `PATH`), redeclared inside each `Describe`. Follow that exact pattern; do not hoist it to a shared helper.
- `claude/claude_suite_test.go` — suite wiring; carries the `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate` directive that makes the per-interface `//counterfeiter:generate` directives take effect.
- `specs/in-progress/054-shared-interactive-agent-service.md` — the sibling spec that owns the shared session interface and the HTTP service. Read it in full.
- `specs/in-progress/055-claude-streaming-session.md` — the spec this prompt implements (Goal, Non-goals, Desired Behavior, Constraints, Failure Modes, Security / Abuse Cases).
- `docs/interaction-count.md` — the per-turn metric contract the Claude session feeds.

Load-bearing facts, verified against this working tree at `origin/master` (`0f2795f`). Do not re-derive these, do not contradict them:

1. `claude/claude-runner.go`'s `buildCommand` today produces `claude --print --output-format stream-json --verbose --strict-mcp-config` (plus conditional `--allowedTools` and `--model`), sets `cmd.Stdin = bytes.NewBufferString(prompt)`, starts the process, scans stdout once, and exits. It never passes `--input-format`. Verified in `buildCommand`.
2. `ClaudeResult.SessionID` is captured by `scanOutput` and is used only to locate that session's transcript on disk (`countHumanInteractions`). It is never used to resume a conversation. Verified in `claude/claude-runner.go` and `claude/claude-result.go`.
3. `ClaudeRunnerConfig` (`claude/claude-runner-config.go`) has exactly five fields — `ClaudeConfigDir`, `AllowedTools`, `Model`, `WorkingDirectory`, `Env` — and no session-continuity field. Verified in the file.
4. `claude.ClaudeRunner` is `Run(ctx context.Context, prompt string) (*ClaudeResult, error)`. It is consumed by other repositories and its counterfeiter mock is generated from that exact signature; spec 055 lists changing it as a Non-goal. It must not change.
5. `buildSubprocessEnv` and `resolveConfigDir` are methods on the unexported `claudeRunner`, and every test in `claude/` lives in the external `package claude_test` — so no test calls them directly. Extracting them into package-level functions parameterised by `ClaudeRunnerConfig` breaks no test; the behaviour must stay byte-identical (the `claudeRunner CLAUDE_CONFIG_DIR env propagation` and `AllowedTools` describes in `claude/claude-runner_test.go` must pass unmodified).
6. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, args ...interface{}) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, args ...interface{}) error`, `Join`, `Is`, `As`, `Cause`. There is no `Newf`.
7. `github.com/bborbe/run` is at `v1.10.2` and is currently an INDIRECT requirement in `go.mod`. Importing it is allowed (`go mod tidy` promotes it to direct) but is not required — see requirement 3, which specifies a design that needs no goroutine at all.
8. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`; the daemon log records `hideGit=false`. The container therefore works directly in the repo tree at `/workspace`.
9. **The shared session interface does not exist in this tree yet.** `grep -rn 'SessionFactory\|OpenSession\|Session interface' --include='*.go' /workspace` returns nothing; `specs/in-progress/054-shared-interactive-agent-service.md` is at `status: prompted` and its prompt 1 has not landed. This prompt's hard precondition is that it has landed. See requirement 0.
10. counterfeiter directives in this repo use the explicit long form on the interface line, e.g. `//counterfeiter:generate -o ../mocks/claude-claude-runner.go --fake-name ClaudeRunner . ClaudeRunner`. The short form (`. Foo`) is a silent no-op here.
11. Lint limits that bind the new files: funlen 80 lines / 50 statements, nestif complexity 4, golines line length 100.
12. `docs/claude-streaming-protocol.md` does not exist yet; it is a deliverable of this prompt (spec 055 Repository hygiene).
</context>

<requirements>

## 0. Precondition — locate and read the shared session interface (do this FIRST)

This prompt implements an interface it does not own. Before writing any code:

- Run `grep -rn 'SessionFactory\|OpenSession\|Session interface' --include='*.go' /workspace` and read the package that defines the shared session interface and its factory, landed by spec 054 prompt 1. The names in that grep are illustrative only — spec 054 owns the actual names — so an empty result is not by itself proof of absence: read spec 054's landed package before concluding the precondition is unmet.
- Read that interface in full and capture, verbatim: the interface name, its package import path, every method name, every parameter type, and every return type. Every method name and signature in requirement 2 must match it exactly — the names `Session`, `SessionFactory`, `OpenSession`, `Send`, `Close` are used descriptively in spec 055 and are 054's to define, not this prompt's.
- Also read the shared service package's contract test (spec 054 AC1/AC2) so requirement 9 can wire the Claude implementation into it.

**If the grep returns nothing, STOP and report a blocker** naming `specs/in-progress/054-shared-interactive-agent-service.md` as the unlanded dependency. Do NOT define the shared session interface in this prompt — spec 055 lists it as a Non-goal, and defining a second copy of the seam is the exact divergence this pair of specs exists to prevent. Do NOT invent a plausible interface shape and code against it.

## 1. Write the protocol reference — new file `docs/claude-streaming-protocol.md`

Spec 055 Repository hygiene requires the stream-json turn-loop protocol to be written down so the knowledge does not die with the implementation. Create `docs/claude-streaming-protocol.md` with exactly these sections:

1. **Launch** — the argv the session spawns, and why each flag is there. State the base set as `claude --print --input-format stream-json --output-format stream-json --verbose --strict-mcp-config`, plus `--permission-prompt-tool stdio` when a permission decider is configured, plus the conditional `--allowedTools` and `--model`. Note explicitly that spec 055 Assumptions require the exact flag set to be confirmed against the pinned CLI version and recorded with AC2's result, and that the flag set lives in exactly one function so a correction is a one-line change.
2. **Input turn shape** — one JSON object per line on stdin, newline-terminated, of the form `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}`. Note that a bare string `content` is also accepted by the CLI, and that the implementation writes the content-block form.
3. **Output event vocabulary** — one JSON object per line on stdout. List the events the implementation recognises: `system` (subtype `init`, carries `session_id`), `assistant` (carries `message.content[]` of `text` and `tool_use` items), `user` (tool results), `result` (carries `result`, `usage`, `num_turns`, `session_id`), and the control-request event described in section 5.
4. **Turn-boundary rule** — a turn begins when the implementation writes one input turn to stdin and ends when the `result` event for that turn is read from stdout. The implementation reads no stdout outside a turn. State that a permission request raised mid-turn does not end the turn; the CLI is blocked waiting on the answer and the turn resumes after the response is written.
5. **Permission-request shape** — the request event the CLI emits when it needs a tool decision, carrying a request id, a tool name, and the tool input; and the response event the implementation writes back on stdin, carrying the same request id and an allow/deny verdict. Record both the `control_request` / `control_response` envelope names and the `can_use_tool` and `permission` request-subtype spellings, and state which one the implementation accepts as its primary and which as its fallback.
6. **Version confirmation** — a short paragraph stating that this vocabulary was implemented against the documented SDK shape, that the pinned `@anthropic-ai/claude-code` version and the confirmed flag set are recorded with spec 055 AC2's result, and that a vocabulary change is expected to fail loudly at the parser rather than degrade silently.

The document must describe the protocol the implementation actually speaks. Write it after requirement 3 so the two agree; if you change the parser while writing it, update the code, not the prose.

## 2. The session implementation — new file `claude/claude-session.go`

Create `claude/claude-session.go` (`package claude`, license header, no `init()`, no package-level mutable state). It contains:

**The permission seam.** Define the interface and its value types in this file (or a sibling `claude/claude-session-permission.go` if the file approaches the funlen limit):

```go
// PermissionRequest describes a tool invocation the CLI raised mid-turn and is
// waiting on. It carries no conversation content: only the tool's name, a
// human-readable description, and a preview of the tool input.
type PermissionRequest struct {
	// ToolName is the name of the tool the CLI wants to invoke.
	ToolName string
	// Description is the CLI's human-readable description of the invocation.
	Description string
	// InputPreview is a preview of the tool input. It is not logged.
	InputPreview string
}

// PermissionDecision is the verdict returned for a PermissionRequest.
type PermissionDecision struct {
	// Allow reports whether the tool invocation may proceed.
	Allow bool
	// Message is an optional explanation returned to the CLI on denial.
	Message string
}

// PermissionDecider decides whether a tool invocation the CLI raised mid-turn is
// allowed. It is the one place this package accepts an instruction from outside
// the process. The pod's HTTP surface implements it; a nil decider is invalid.
type PermissionDecider interface {
	DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)
}
```

Add the counterfeiter directive on the interface line in the repo's explicit long form, targeting `../mocks/claude-permission-decider.go` with `--fake-name ClaudePermissionDecider`.

**The session struct and constructor.** Implement the shared session interface from requirement 0. The struct is private and named after the interface with a lowercased first letter; the constructor follows the `New<InterfaceName>` convention and returns the shared interface type. The constructor takes exactly two dependencies: a `ClaudeRunnerConfig` (Desired Behavior 4 — reuse the existing config, do not introduce a parallel config type) and a `PermissionDecider`.

Do NOT add any field to `ClaudeRunnerConfig` for this. Spec 055 Constraints are explicit: pi's `PersistSession`/`SessionID` pair exists because pi is a subprocess that must be told to persist, and the Claude session holds its own process — there is no subprocess flag pair to mirror and no consumer for one. If you believe a field is genuinely required, name the consumer in your final report instead of adding it.

**Spawn.** The session starts the process once, when the session is opened, and holds it for the session's life:

- Build the argv with `exec.Command` / `exec.CommandContext` — never through a shell, never `sh -c`. Spec 055 Security: the process is spawned with an argument vector, not a shell command.
- The argv is the flag set from requirement 1. `--allowedTools` is added only when `len(config.AllowedTools) > 0` and `--model` only when `config.Model != ""`, exactly as `buildCommand` does today.
- Do NOT pass the session id as an argument and do not pass `--session-id`. Continuity comes from the held process (spec 055 Non-goal: resume-from-transcript). The spawn argv is therefore identical for every session id — see requirement 8 for the test that asserts this.
- Reuse `ClaudeRunnerConfig.WorkingDirectory` for `cmd.Dir` and the existing env construction for `cmd.Env`. To do that, extract `buildSubprocessEnv` and `resolveConfigDir` from methods on `claudeRunner` into package-level functions parameterised by `ClaudeRunnerConfig`, and have `claudeRunner`'s methods delegate to them. The observable behaviour must stay byte-identical — the existing `CLAUDE_CONFIG_DIR env propagation` and `AllowedTools` describes in `claude/claude-runner_test.go` must pass unmodified.
- Hold the stdin pipe (`io.WriteCloser`) and the stdout pipe in the struct. Use `bufio.Scanner` on stdout with the same bounded buffer the runner uses (`scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)`).
- `exec.Command` is called in exactly ONE function in this file. `Send` must never spawn. See requirement 7's structural check.

**Do not tie the process lifetime to the context passed to the open call.** The open call's context is request-scoped (spec 054 builds a session lazily on the first request and caches it), so a process bound to it would die when the first request returned. Spawn with `exec.Command` (no context), or with a context the session owns, and terminate explicitly per requirement 5.

## 3. The turn loop

Implement the send method of the shared interface so that one call performs exactly one turn:

1. Write ONE JSON turn to the held process's stdin — a single line, newline-terminated, in the input shape of requirement 1's doc section 2. Write it directly to the `io.WriteCloser`; an OS pipe does not buffer, so no explicit flush is needed. Never write to stdin and read stdout concurrently outside this single turn.
2. Read stdout line by line until the turn's terminal event. The terminal event is the `result` event, parsed with the existing `claudeEvent` type from `claude/claude-event.go`. Do not fork a parallel event vocabulary.
3. Return the turn's result in the type the shared interface requires. Where you may choose that type, reuse the existing `ClaudeResult` rather than inventing a parallel result type: `docs/interaction-count.md` depends on the per-turn turn count (`num_turns`) and token counts that `ClaudeResult` already carries, and dropping them would silently break the `metrics_agent_turns` path.

Design the loop single-threaded. A reader goroutine is not needed and must not be added: the permission response of requirement 4 is written from the same loop iteration that read the request, and no stdout is read outside a turn. This satisfies spec 055's process-hygiene constraint ("must not read from the process's stdout concurrently with a write to its stdin outside a single turn") and avoids the `go-concurrency/no-raw-go-func` MUST rule entirely. If you conclude a goroutine is genuinely unavoidable, use `github.com/bborbe/run`, never a raw `go func`.

## 4. Permission routing

When a line read in step 2 is a permission request rather than a conversation event:

1. Parse the request id, the tool name, and the tool input. Build a `PermissionRequest` carrying the tool name, the CLI's description, and a preview of the tool input. Never log the preview (spec 055 Security).
2. Call `PermissionDecider.DecidePermission` with the turn's context. The turn BLOCKS here — the pause is the held process waiting on the answer, not a busy loop. Do not poll, do not sleep-loop.
3. Write the verdict back to the same process's stdin as one newline-terminated JSON line carrying the same request id and the allow/deny verdict, in the shape of requirement 1's doc section 5.
4. Continue reading stdout for the turn's terminal event.

**If `DecidePermission` returns an error, the turn fails.** Spec 055 Failure Modes: a permission request raised while the HTTP surface is unavailable must fail the turn rather than blocking indefinitely. Wrap the error with `errors.Wrap(ctx, err, "decide permission")` and return it from the send method. Do not auto-allow, do not auto-deny, and do not retry.

## 5. Close and cancellation — process hygiene

Spec 055 Constraints: the process is terminated on `Close` and on context cancellation; a leaked process per session is a resource leak the pod cannot recover from without a restart.

- `Close` closes the held stdin pipe first — the CLI's input stream ends and it exits on its own — waits for the process with a bounded grace period, and only then kills it. Make `Close` idempotent: a second call must not panic and must not block.
- Cancellation of the context passed to an in-flight send terminates the process. A turn that ends because its context was cancelled must return a wrapped error (`errors.Wrap(ctx, err, "turn cancelled")` or equivalent) and must leave the session unusable rather than reusable. **Name the mechanism you use.** A blocked `bufio.Scanner.Scan()` cannot observe context cancellation by itself, so something must watch the send's context: a session-owned context cancelled when the in-flight send's context is cancelled, a read deadline on the stdout pipe, or a watcher goroutine created with `github.com/bborbe/run`. Requirement 3 forbids a reader goroutine, so if you choose a watcher, bound it to the session and state it in your report.
- A turn that ends abnormally for any reason — context cancellation, a malformed event, or the process dying — must not be followed by a silent re-spawn. Mark the session closed; every later send on it fails with a clear error.
- `Close` after the process has already died must still succeed (return nil, or a wrapped error you can justify) and must not panic.
- No goroutine outlives `Close`. If you introduced none (requirement 3), state that in your report.

## 6. Dead process, malformed event, and idle timeout

Implement the spec 055 Failure Modes rows that are reachable in-container:

- **The held process dies between turns.** The next send fails with an error wrapped via `errors.Wrap` naming the process exit, and does NOT start a fresh conversation. Detect the death by the stdout scan ending and the process having exited, and/or by `cmd.Wait()` having already returned.
- **The process emits an unparseable event.** The turn fails, and the error records the offending line's SHAPE — its byte length, and at most a short digest — never its content. Spec 055 Security: turn content is not logged. Close the process; a CLI that emitted a malformed event may be mid-state.
- **The CLI version changes its stream-json vocabulary.** The parser fails loudly on the first unrecognised shape rather than skipping it. A line that is valid JSON but carries an unknown `type` must not be silently dropped if it is a control request; an unknown event type that is not a control request may be ignored, but state that choice in `docs/claude-streaming-protocol.md`.
- **The CLI has an idle timeout and closes the process.** Surface it as a per-turn failure (the dead-process row above). Do NOT add a flag or a config field to disable it; the spec permits either surfacing the loss or disabling the timeout, and surfacing is the branch that needs no new knob. Record in `docs/claude-streaming-protocol.md` whether the pinned CLI has such a timeout, if you can determine it.

## 7. Tests — `claude/claude-session_test.go` (AC1)

Create `claude/claude-session_test.go` in the external `package claude_test`. Redeclare the `writeShim`-style helper inside your `Describe` (the established convention in this file family) but have it return the shim directory so the test can pass it to the session through `ClaudeRunnerConfig.Env`.

**A critical mechanical detail:** the session spawns the CLI through the inherited env construction, which passes only the allowlist (`HOME`, `PATH`, `USER`, `TZ`, `ZONEINFO`, `TMPDIR`, `LANG`, `LC_ALL`), `CLAUDE_CONFIG_DIR`, and `ClaudeRunnerConfig.Env`. Your shim therefore cannot discover its own directory — `exec.Command("claude")` passes `argv[0]` as the bare name `claude`, not the resolved path, so `dirname "$0"` is unreliable. Pass the shim directory explicitly as `ClaudeRunnerConfig.Env["SHIM_DIR"]` and have the shim read `$SHIM_DIR`.

Use this shim body (a long-lived fake that stays alive across turns):

```sh
#!/bin/sh
trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
echo "$$" >> "$SHIM_DIR/starts.log"
printf '%s\n' "$@" > "$SHIM_DIR/args.log"
n=0
while IFS= read -r line; do
  n=$((n+1))
  printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
  printf '%s\n' "$$" > "$SHIM_DIR/turn-$n.marker"
  printf '{"type":"result","result":"reply-%s","session_id":"sess-fixed","num_turns":1}\n' "$n"
done
exit 0
```

The `Describe("claudeSession held process")` block must contain an `It` for each of these, named as given:

1. **"starts exactly one process across two sends"** — open a session, send `turn-one`, then send `turn-two`. Assert `starts.log` has exactly ONE line. A per-turn `claude --print --resume <id>` wrapper produces two lines and fails this.
2. **"writes both turns' payloads to that one process's stdin"** — assert `stdin.log` has exactly TWO lines, that line 1 contains `turn-one` and line 2 contains `turn-two`. This is the half a count-only assertion misses: a stub that starts one process and never writes a turn to it passes a start-count check but fails this one. A substring check alone would pass for a malformed literal, so also unmarshal `stdin.log` line 1 as JSON and assert `type == "user"` with the content block carrying `turn-one`. Also assert the two send calls returned `reply-1` and `reply-2` respectively.
3. **"the process is still running when the second turn is written"** — assert `turn-2.marker` exists after the second send returns (the process wrote it while consuming turn 2), that its recorded pid equals the single pid in `starts.log`, and that the process is still alive (`os.FindProcess` + `Signal(syscall.Signal(0))`). A `*exec.Cmd` identity assertion alone does not prove liveness — the identity survives process death — which is why this row asserts the pid that consumed turn 2 is the pid that started, and is still running.
4. **"spawns with the stream-json protocol flags"** — assert `args.log` contains `--input-format` followed by `stream-json`, and `--output-format` followed by `stream-json`, and that `--print` is present. `printf '%s\n' "$@"` writes one argument per line, so grep each token on its own line.

Add a structural guard as a fifth `It` or as a verification command: `exec.Command` appears exactly once in `claude/claude-session.go`, in the spawn function, and the send path never calls it. This is what makes "the same `*exec.Cmd` across two sends" true by construction.

## 8. Tests — failure modes, permission routing, security

Add a `Describe("claudeSession failure and permission")` block with one `It` per row below. Use a counterfeiter fake of `PermissionDecider` (`mocks.ClaudePermissionDecider`) — never hand-write a mock.

1. **"fails the next turn when the held process died between turns"** — shim that answers turn 1 and then exits:
   ```sh
   #!/bin/sh
   trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
   echo "$$" >> "$SHIM_DIR/starts.log"
   read -r line
   printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
   printf '{"type":"result","result":"reply-1"}\n'
   exit 0
   ```
   Assert turn 1 succeeds, turn 2 returns an error, and `starts.log` still has exactly ONE line — no re-spawn, no silent fresh conversation (spec 055 Failure Modes row 1, Desired Behavior 2).
2. **"fails the turn on an unparseable event without logging its content"** — shim that emits one deliberately malformed line carrying a canary, e.g. `{"type":"assistant","text":"LEAK-CANARY not valid json`. Assert the send returns an error AND that `err.Error()` does NOT contain `LEAK-CANARY`, while the error does carry the line's shape (byte length). This is the spec 055 Security row "turn content is not logged" made executable. **Also capture `glog` output for the duration of the send and assert no captured line contains `LEAK-CANARY`** — the security row is about log output, not only the returned error, and the existing `scanOutput` pattern logs every line at `glog.V(4)`, so a parser that copied that line would otherwise pass this row while violating the constraint. Reuse spec 054's glog-capture pattern.
3. **"routes a permission request out and writes the verdict back"** — shim that raises a request and waits for the answer:
   ```sh
   #!/bin/sh
   trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
   read -r line
   printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
   printf '{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}\n'
   read -r resp
   printf '%s\n' "$resp" >> "$SHIM_DIR/stdin.log"
   printf '{"type":"result","result":"done-after-permission"}\n'
   ```
   Stub the decider to return `PermissionDecision{Allow: true}`. Assert: the decider was called exactly once with `ToolName == "Bash"`; the shim's `stdin.log` second line parses as a response carrying `request_id` `req-1` and an allow verdict; and the send returned `done-after-permission`.
4. **"fails the turn when the permission decider errors"** — same shim, decider stub returns an error. Assert the send returns an error and does NOT return a result — the turn must not block and must not auto-allow (spec 055 Failure Modes row 3).
5. **"builds the same argv regardless of the session id"** — open two sessions with different ids and the same config, and assert `args.log` is byte-identical for both. This is the spec 055 Security row "the session id is not interpolated into a shell string" made executable: the argv does not depend on the id at all, so no id can inject a flag or shell syntax.
6. **"terminates the process on Close"** — send one turn, call `Close`, then assert `exited.log` appears within a bounded `Eventually` and that a subsequent send fails.
7. **"terminates the process when an in-flight turn's context is cancelled"** — shim that sleeps before answering (`sleep 30` after recording the turn); call send with a `context.WithTimeout(context.Background(), 2*time.Second)`; assert the send returns an error and that a follow-up send on the same session also fails, with `starts.log` still holding exactly ONE line. **Also assert the process is actually gone** — `exited.log` appears within a bounded `Eventually`, and the pid recorded in `starts.log` no longer answers `Signal(syscall.Signal(0))`. Without that half the row passes on a session that merely returns `ctx.Err()` after the shim's own 30 s sleep, since the shim exits by itself either way.

For row 7, if the shim is flaky on a slow container, raise the timeout (the suite timeout is 60 s, so 5 s is still safe) — do not shorten the shim's sleep.

## 9. Test — the implementation is substitutable in the shared service (AC3)

Spec 055 AC3: the implementation must be substitutable for the pi implementation in the shared service. Add a test that constructs the shared service from requirement 0 with the Claude session implementation wired in place of the pi one, and drives the shared service's contract table through the HTTP path (`httptest`), with the CLI boundary faked by the `SHIM_DIR` shim of requirement 7.

- Put this test where the shared service's own contract test lives (the package landed by spec 054 prompt 1), or in a sibling test file in that package — whichever keeps the shared contract table single-sourced rather than copied.
- Do not copy the contract table. Reuse the shared service's own table-driven test and parameterise it with the Claude session factory if its shape allows; if it does not, say so in your report and add the smallest Claude-flavoured variant that still drives all of the service's rows.
- The pi implementation and its tests must keep passing unchanged.

## 10. Repository hygiene

- **`README.md`** — add the new package surface to the existing package table / usage notes. Keep the edit minimal and consistent with the surrounding style.
- **`CHANGELOG.md`** — add an entry under `## Unreleased` describing the Claude session: one long-lived process per session driven over the CLI's stream-json protocol, with permission requests routed out. Follow `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`. If `## Unreleased` does not exist, create it at the top, below the preamble and above the newest released `## vX.Y.Z` section.
- **`docs/claude-streaming-protocol.md`** — the file from requirement 1.

## 11. Scope containment

Edit or create ONLY these files:

- `claude/claude-session.go` (new)
- `claude/claude-session-permission.go` (new, optional — only if you split the seam out)
- `claude/claude-session_test.go` (new)
- `claude/claude-runner.go` (only the `buildSubprocessEnv` / `resolveConfigDir` extraction of requirement 2 — no behaviour change)
- `claude/claude-runner-config.go` (only if a doc comment needs adjusting — do NOT add a field)
- `mocks/claude-permission-decider.go` (generated)
- the shared service package's test file(s) for requirement 9
- `docs/claude-streaming-protocol.md` (new)
- `README.md`
- `CHANGELOG.md`

Do NOT touch `pi/`, `metrics/`, `delivery/`, `healthcheck/`, `agent-step.go` (repository root), `claude/agent-step.go`, `claude/task-runner.go`, `claude/result-deliverer.go`, or `claude/transcript.go`. Do NOT change the `claude.ClaudeRunner` interface or its generated mock. Do NOT add a field to `ClaudeRunnerConfig`. Do NOT change `claudeRunner.Run`'s behaviour.

</requirements>

<constraints>
- **Dependency direction (hard precondition).** This prompt depends on spec 054's prompt 1, which lands the shared session interface and the HTTP service. Spec 055: "It cannot be implemented before that spec's prompt 1 has landed." If the interface is absent from the tree, stop and report a blocker — never define a second copy of the seam.
- **No Python SDK.** `ClaudeSDKClient` is a Python class from `claude-agent-sdk-python` and is NOT importable in this Go repository. Do not import, reference, vendor, or shell out to a Python SDK. This prompt targets the CLI protocol the SDK wraps: `claude -p --input-format stream-json --output-format stream-json --verbose`.
- **`ClaudeRunnerConfig` gains no flag pair (hard veto).** Spec 055 Constraints: pi's `PersistSession`/`SessionID` exists because pi is a subprocess that must be told to persist; the Claude session holds its own process, so there is no subprocess flag pair to mirror and no consumer for one. Add NO field to `ClaudeRunnerConfig` and NO opt-out flag anywhere.
- **`claude.ClaudeRunner` is frozen.** Its signature `Run(ctx context.Context, prompt string) (*ClaudeResult, error)`, its generated mock, and `claudeRunner.Run`'s observable behaviour all stay exactly as they are. The session is a sibling of the runner, not a replacement. Existing tests pass unmodified.
- **`claudeRunner`'s observable behaviour is frozen.** The only permitted edit to `claude/claude-runner.go` is extracting `buildSubprocessEnv` and `resolveConfigDir` into package-level functions parameterised by `ClaudeRunnerConfig`, with the methods delegating. The `CLAUDE_CONFIG_DIR env propagation`, `AllowedTools`, `usage capture`, and `stdout tail` describes in `claude/claude-runner_test.go` must pass byte-identically.
- **Process hygiene.** One process per session, terminated on `Close` and on cancellation of an in-flight turn. No background reader goroutine; no stdout read concurrent with a stdin write outside a single turn; no goroutine outliving `Close`.
- **No raw goroutines.** `go-concurrency/no-raw-go-func` is a MUST rule: no raw `go func(){...}()` outside `main.go` / `cmd/**`. The design in requirement 3 needs no goroutine at all; if one becomes unavoidable, use `github.com/bborbe/run`.
- **Error handling.** Errors are wrapped with `github.com/bborbe/errors` — `errors.Wrap(ctx, err, "…")` / `errors.Wrapf(ctx, err, "…")`, `errors.New(ctx, "…")`, `errors.Errorf(ctx, "…")`. Never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in business logic.
- **Code conventions** (per `docs/dod.md`). Exported types, functions and interfaces carry GoDoc comments. Interface → Constructor → Struct → Method, with the implementation struct private and named as the interface with a lowercased first letter. Factory functions are pure composition — no conditionals, no I/O, no `context.Background()`. No package-level mutable state and no `init()`.
- **Security.** Turn content is never logged — a malformed line's SHAPE (byte length, at most a short digest) may be recorded, its content may not. The tool-input preview is never logged. The process is spawned with an argument vector, never a shell string. Nothing outside the process is trusted: the permission verdict is the only instruction accepted from outside, and it is an allow/deny only.
- **No probe word.** Spec 055 AC2 asks the session to remember a word the operator picks AT RUN TIME. Do not write any such word, or any test that depends on one, into this repository. The in-container tests prove continuity by asserting both turns reach one process's stdin (requirement 7 row 2); the memory contract itself is the operator's host-side probe.
- **Tests.** Ginkgo v2 / Gomega in external `*_test` packages (`claude_test`), counterfeiter mocks only (`mocks.ClaudePermissionDecider`) — never hand-write a mock, never add an in-package `package claude` test file. Coverage for the new code must be >= 80%.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; nestif complexity 4.
- **No new module.** `github.com/bborbe/run` may move from indirect to direct in `go.mod` if you import it; no other dependency is added and no `replace` or `exclude` directive is introduced.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable.

```bash
# 1. The session file exists and holds the held-process design.
ls /workspace/claude/claude-session.go

# 2. Exactly ONE process spawn in the session file — this is what makes
#    "the same *exec.Cmd across two sends" true by construction.
grep -cE ':= *exec\.Command(Context)?\(' /workspace/claude/claude-session.go
# Must print exactly 1 (the spawn call site). A prose mention of exec.Command in a
# doc comment inflates this count — keep the call site and any prose distinguishable.

# 3. The streaming-input protocol flags are constructed.
grep -n -- '--input-format' /workspace/claude/claude-session.go
grep -n -- '--output-format' /workspace/claude/claude-session.go
# Each must match at least one line. NOTE: this is a smoke check only, NOT evidence —
# a comment containing the literal satisfies it. Requirement 7's shim test is the evidence.

# 4. No Python SDK reference anywhere in the Go tree.
! grep -rn 'claude_agent_sdk\|ClaudeSDKClient' --include='*.go' /workspace
# Must print nothing. This is a guard, NOT AC1 evidence — spec 055 rejects a symbol grep
# as proof that a long-lived process exists.

# 5. No session-continuity field was added to the config (hard veto).
! grep -n 'SessionID\|PersistSession\|Session ' /workspace/claude/claude-runner-config.go
# Must print nothing.

# 6. The ClaudeRunner interface is unchanged.
grep -n 'Run(ctx context.Context, prompt string) (\*ClaudeResult, error)' /workspace/claude/claude-runner.go
# Must match exactly once.

# 7. The permission decider seam and its generated mock exist.
ls /workspace/mocks/claude-permission-decider.go
grep -n 'PermissionDecider' /workspace/claude/claude-session.go
# Must match.

# 8. The protocol reference is written.
ls /workspace/docs/claude-streaming-protocol.md
grep -c '^## ' /workspace/docs/claude-streaming-protocol.md
# Must be >= 5 (launch, input turn, output vocabulary, turn boundary, permission shape).

# 9. AC1's observable evidence — one process, both turns on its stdin, still alive at turn 2.
go test -mod=mod -race -v ./claude/ > /tmp/claude-session-test.log 2>&1
# Must exit 0. Each of these patterns must match at least one line:
grep -E "starts exactly one process across two sends|writes both turns' payloads to that one process's stdin|the process is still running when the second turn is written|spawns with the stream-json protocol flags" /tmp/claude-session-test.log

# 10. The failure-mode and permission rows.
grep -E "fails the next turn when the held process died between turns|fails the turn on an unparseable event without logging its content|routes a permission request out and writes the verdict back|fails the turn when the permission decider errors|builds the same argv regardless of the session id|terminates the process on Close|terminates the process when an in-flight turn's context is cancelled" /tmp/claude-session-test.log

# 11. AC3 — the Claude session is substitutable in the shared service.
go test -mod=mod -race -v ./... > /tmp/all-test.log 2>&1
# Must exit 0; the shared service's contract rows must appear driven by the Claude factory.

# 12. Coverage for the new code.
go test -coverprofile=/tmp/cover.out -mod=mod ./claude/... && go tool cover -func=/tmp/cover.out | grep -E 'claude-session|total'
# The new session functions must each be >= 80%.

# 13. No findings.
go vet ./...

# 14. Full pipeline — format, generate, test, lint, license.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **AC2 is operator-only and is deliberately not a container check.** The real `claude` binary is not present in the build container, so the two-turn memory probe (operator-chosen word, both responses, the `ps` observation taken during turn 2, and the pinned `claude --version`) runs on the host after merge and is recorded in the PR description. The prompt states this rather than pretending a container test reaches it. No probe word is written into the repository.
- **The exact flag set is confirmed at AC2 time, not here.** Spec 055 Assumptions require the flag set to be confirmed against the pinned CLI version and recorded with AC2's result. `--permission-prompt-tool stdio` is included because spec 055 Desired Behavior 3 requires permission requests to leave the process, and that flag is what enables the CLI's permission control channel; if the pinned CLI rejects or renames it, the flag set lives in exactly one function (requirement 1's doc section 1) so the correction is a one-line change plus a doc update.
- **The permission seam is defined here, not by spec 054.** Spec 054's service defines only `/readiness`, `/metrics` and `POST /prompt`, and its session interface models open/send/close — neither carries a permission hook. Spec 055 Desired Behavior 3 and its Security section nevertheless require the request to leave the process carrying "a tool name, a description and an input preview" and to accept "only an allow/deny verdict back", which is exactly the `PermissionDecider` shape in requirement 2. Binding it to the pod's HTTP surface is the `agent-claude` hand-off named in spec 054's Hand-offs table, not a prompt of either spec.
- **The `*exec.Cmd`-identity half of AC1 is satisfied structurally, not by a field accessor.** The repo tests in external `*_test` packages, so a test cannot read the session's private `cmd` field. Requirement 7 therefore asserts the stronger observable: one pid in `starts.log`, the same pid consuming turn 2, that pid still alive, and exactly one `exec.Command` call site in the file (verification step 2). If the auditor prefers a literal `*exec.Cmd` equality assertion, that requires an exported test accessor, which would widen the public API for a test — flagged rather than done.
- **Git-dependent checks moved to the operator/manager side.** This prompt's verification uses plain `grep` rather than `git grep`, and carries no `git diff` scope check; the manager runs `git diff --name-only origin/master...HEAD` against the scope list in requirement 11 after the merge. `.dark-factory.yaml` sets `workflow: direct` with no `hideGit`, but a bare `git` command in the container is avoided regardless, because the daemon does not check verification exit codes and a failed `git` would read as a pass.
