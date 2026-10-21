---
status: draft
spec: [055-claude-streaming-session]
created: "2026-10-21T08:32:54Z"
branch: dark-factory/claude-streaming-session
---

<summary>
- A Claude conversation can stay alive across requests instead of being rebuilt for every prompt
- One long-running Claude process per session id serves every turn, so a second request reaches the same conversation
- The conversation lives in the held process, not in a transcript file re-read between turns
- When Claude wants to use a tool mid-turn, the request leaves the process and the turn waits for a real allow/deny answer instead of hanging or auto-approving
- If the held process has died, the next prompt fails visibly rather than silently starting a blank conversation
- The process is terminated when the session is closed and when an in-flight turn is cancelled, so a pod cannot accumulate orphaned Claude processes
- The spawn options reuse the configuration the existing Claude runner already carries, so there is no second config type and no new continuity flag pair
- The existing one-shot Claude runner is untouched: same interface, same callers, same behaviour
- The wire protocol this depends on — message shapes, the turn-boundary rule, and the permission-request shape — is written down in the repository
- No secret or probe word is baked into the tests; the real-CLI memory probe is operator-executed on the host after merge
</summary>

<objective>
Add a Claude session implementation to `github.com/bborbe/agent` that satisfies the shared session interface landed by spec 054 and holds ONE long-lived `claude` process per session, driven over the CLI's stream-json protocol on stdio: each turn is written to that process's stdin, events are read back until the turn's terminal event, mid-turn tool-permission requests are routed out to a decider seam and the turn waits on that answer, and the process is terminated on `Close` and on cancellation of an in-flight turn. This implements spec 055 Desired Behaviors 1-4 and Acceptance Criteria 1 and 3. Acceptance Criterion 2 is operator-executable only (the real `claude` binary is not present in the build container) and is recorded here, not run.
</objective>

<context>
Repository root inside the container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). All paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (read before editing; paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private and named after the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), the counterfeiter directive, the suite timeout, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; no `fmt.Errorf`, no bare `return err`, no `context.Background()` in business logic.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — `go-concurrency/no-raw-go-func` is a MUST rule; use `context.AfterFunc` or `github.com/bborbe/run`, never a raw `go func(){...}()` outside `main.go` / `cmd/**`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md` — counterfeiter mocks for every new interface, generated into `mocks/`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every new exported type, field, and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules for new code.

Files to read IN FULL before editing:
- `agent_session.go` — the shared session interface (spec 054). Verified in this tree: package `lib` (imported as `agentlib "github.com/bborbe/agent"`), `Session` has `Prompt(ctx context.Context, prompt string) (string, error)` and `Close(ctx context.Context) error`; `SessionFactory` has `Create(id string) Session`. Its doc comment states that `Create` is construction-only: it performs no I/O and cannot fail. Generated fakes are `mocks.Session` and `mocks.SessionFactory`.
- `interactive/service.go` — the shared HTTP service (spec 054) and its `NewService` constructor. It takes an `agentlib.SessionFactory` as its first parameter; read the current parameter list rather than assuming it (later specs added auth, card, idle-timeout and max-sessions parameters).
- `pi/session.go` — the sibling session implementation. It is the shape to mirror: `NewSession` returns `agentlib.Session`, the struct is private, and `Create` copies the base config without mutating it.
- `claude/claude-runner.go` — the existing one-shot runner. `Run` builds a command, spawns `claude --print`, scans stdout once, waits, returns. Read `buildCommand` (the argv pattern), `scanOutput` (the bounded scanner pattern), and `buildSubprocessEnv` / `resolveConfigDir`. Verified in this tree: both env helpers are ALREADY package-level functions with signatures `buildSubprocessEnv(ctx context.Context, config ClaudeRunnerConfig) ([]string, error)` and `resolveConfigDir(ctx context.Context, config ClaudeRunnerConfig) (string, error)`; the `claudeRunner` methods delegate to them. Do not re-extract them; call the package-level forms directly.
- `claude/claude-runner-config.go` — `ClaudeRunnerConfig`. Verified fields: `ClaudeConfigDir ClaudeConfigDir`, `AllowedTools AllowedTools`, `Model ClaudeModel`, `WorkingDirectory AgentDir`, `Env map[string]string`. There is no session-continuity field and none may be added.
- `claude/claude-event.go` — the stream-json wire types. Verified: `claudeEvent` carries `Type`, `Result`, `Message claudeMsg`, `Usage json.RawMessage`, `NumTurns json.Number`, `SessionID string`; `claudeMsg` carries `Content []claudeContent`; `claudeContent` carries `Type`, `Text`, `Name`, `Input json.RawMessage`. Reuse this vocabulary; do not fork a parallel one.
- `claude/claude-result.go` — `ClaudeResult`. The shared `Session.Prompt` returns `string`, so the session returns the result text; do not invent a new result type.
- `claude/claude-runner_test.go` and `claude/claude_suite_test.go` — the existing Ginkgo suite and its `//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2 -generate` directive. The suite runs with a 60 s timeout and `time.Local = time.UTC`.
- `claude/claude-session_test.go` — MAY ALREADY EXIST from a prior attempt at this spec. Read it before writing tests; see requirement 1.
- `claude/claude-session.go` — MAY ALREADY EXIST from a prior attempt at this spec. Read it before writing code; see requirement 1.
- `interactive/service_test.go` — the shared contract table and its helpers. Verified: `newTestServer(factory agentlib.SessionFactory, providerBaseURL string) *httptest.Server`, `postPrompt(serverURL, sessionID, body string) (int, error)`, `type contractBackend`, and `runContractTable(description string, newBackend func() *contractBackend)`. Reuse these rather than copying the table.
- `specs/in-progress/054-shared-interactive-agent-service.md` — the sibling spec that owns the shared session interface and the HTTP service.
- `specs/in-progress/055-claude-streaming-session.md` — the spec this prompt implements (Goal, Non-goals, Desired Behavior, Constraints, Failure Modes, Security / Abuse Cases).
- `docs/interaction-count.md` — background only. The shared `Session.Prompt` returns plain text, so this session does not carry usage counters; do not add a metrics path for it in this prompt.

Verified load-bearing facts for this tree. Do not re-derive these, do not contradict them:
1. The shared session interface HAS landed: `agent_session.go` defines `agentlib.Session` and `agentlib.SessionFactory`, and `mocks/session.go` / `mocks/session-factory.go` exist. Spec 055's hard dependency is satisfied.
2. `claude.ClaudeRunner` is `Run(ctx context.Context, prompt string) (*ClaudeResult, error)` and its generated fake is `mocks.ClaudeRunner`. It is consumed outside this repository and must not change.
3. `buildSubprocessEnv` and `resolveConfigDir` are already package-level functions (fact list above). The one-shot runner's env allowlist is `HOME`, `PATH`, `USER`, `TZ`, `ZONEINFO`, `TMPDIR`, `LANG`, `LC_ALL`, then `CLAUDE_CONFIG_DIR`, then `ClaudeRunnerConfig.Env` overrides.
4. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, args ...interface{}) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, args ...interface{}) error`, `Join`, `Is`, `As`, `Cause`. There is no `Newf`.
5. Counterfeiter directives in this repo use the explicit form with `-o` and `--fake-name`, e.g. `//counterfeiter:generate -o ../mocks/claude-permission-decider.go --fake-name ClaudePermissionDecider . PermissionDecider`. The bare short form is a silent no-op here.
6. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. The container works directly in `/workspace`. This prompt nevertheless uses plain `grep` rather than `git` commands in verification, because the daemon does not treat a failed verification command as a failure.
7. Lint limits that bind the new files: funlen 80 lines / 50 statements, nestif complexity 4, golines line length 100.
8. `docs/claude-streaming-protocol.md`, `claude/claude-session.go`, `claude/claude-session_test.go`, and `interactive/claude-session_test.go` MAY ALREADY EXIST. Treat them as a prior attempt, not as authority: the requirements below are the authority, and any existing code must be verified against them.
</context>

<requirements>

## 0. Precondition — the shared session interface

Spec 055 depends on spec 054's prompt 1. In this tree it has landed: read `agent_session.go` and capture the exact interface before writing code. The names and signatures above are verified, but read the file anyway so a later change cannot be missed.

If `agent_session.go` does not define `Session` and `SessionFactory`, STOP and report a blocker naming `specs/in-progress/054-shared-interactive-agent-service.md` as the unlanded dependency. Do NOT define a second copy of the seam: spec 055 lists the shared interface as a Non-goal, and a second copy is the exact divergence the two specs exist to prevent.

## 1. Baseline check — a prior attempt may exist

Before writing anything, check whether these files exist:
- `claude/claude-session.go`
- `claude/claude-session_test.go`
- `interactive/claude-session_test.go`
- `docs/claude-streaming-protocol.md`

If a file exists, read it in full and verify it against the requirements below. Keep what satisfies them; fix or complete what does not. Do NOT delete passing tests, do NOT rewrite working code for style alone, and do NOT remove a field or behaviour that a later spec added (for example an extra field on the permission request). If a file does not exist, create it. Report in the completion report which files were created, changed, or verified unchanged.

## 2. Protocol reference — `docs/claude-streaming-protocol.md`

Create or update this file so it describes the protocol the implementation actually speaks. It must contain these sections:

1. **Launch** — the argv the session spawns and why each flag is there. Base set: `claude --print --input-format stream-json --output-format stream-json --verbose --strict-mcp-config`. Conditional: `--permission-prompt-tool stdio` and `--permission-mode manual` when a decider is configured; `--allowedTools` when `AllowedTools` is non-empty; `--model` when `Model` is non-empty. State that the session id is deliberately not passed, so the argv is identical for every id. State that the flag set lives in exactly one function (`sessionArgs`), so a correction against a pinned CLI is a one-line change plus an edit to this section. Note that spec 055 Assumptions require the exact flag set to be confirmed against the pinned CLI version and recorded with AC2's result.
2. **Input turn shape** — one newline-terminated JSON object per line on stdin: `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}`. Note that a bare string `content` is also accepted, and that the implementation writes the content-block form.
3. **Output event vocabulary** — one JSON object per line on stdout. List `system` (subtype `init`, carries `session_id`), `assistant` (carries `message.content[]` of `text` and `tool_use`), `user` (tool results), `result` (carries `result`, `usage`, `num_turns`, `session_id`), and `control_request`. State the forward-compatibility rule: an unrecognised non-control event type is ignored; a `control_request` is never ignored, and an unrecognised control-request subtype fails the turn.
4. **Turn-boundary rule** — a turn begins when one input turn is written to stdin and ends when the `result` event is read. Nothing is read from stdout outside a turn, and stdout is never read concurrently with a stdin write outside a single turn. A permission request does not end the turn: the CLI is blocked waiting on the answer.
5. **Permission-request shape** — the `control_request` envelope carrying `request_id`, `request.subtype`, `request.tool_name`, `request.description`, `request.input`; and the `control_response` envelope written back with the same `request_id` and `behavior: "allow"` or `"deny"`. Record that `can_use_tool` is the primary subtype and `permission` the fallback. State that the tool-input preview is never logged.
6. **Version confirmation** — state that the vocabulary was implemented against the documented SDK shape (the build container has no `claude` binary), that the pinned `@anthropic-ai/claude-code` version and confirmed flag set are recorded with spec 055 AC2's result, and that a vocabulary change is expected to fail loudly at the parser rather than degrade silently.

## 3. The permission seam — `claude/claude-session.go` (or a sibling file in the same package)

Define these exported types and interface with GoDoc comments. If the existing file already carries additional fields on `PermissionRequest` (for example a request source added by a later spec), leave them in place; the list below is the minimum this spec requires, not a licence to delete later work.

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

Put this counterfeiter directive on the line immediately above the interface:

```go
//counterfeiter:generate -o ../mocks/claude-permission-decider.go --fake-name ClaudePermissionDecider . PermissionDecider
```

Run `make generate` (or the counterfeiter `go:generate` in `claude/claude_suite_test.go`) so `mocks/claude-permission-decider.go` exists with type `ClaudePermissionDecider`.

## 4. Constructors — reuse `ClaudeRunnerConfig`, no new config field

Implement in `claude/claude-session.go` (`package claude`, license header, no `init()`, no package-level mutable state):

```go
// NewSession returns a Session backed by one long-lived claude process.
func NewSession(config ClaudeRunnerConfig, decider PermissionDecider) agentlib.Session

// NewSessionFactory returns a SessionFactory that builds one claude-backed session
// per session id.
func NewSessionFactory(base ClaudeRunnerConfig, decider PermissionDecider) agentlib.SessionFactory
```

- The implementation struct is private and named `session`; `sessionFactory` is the factory struct. Both constructors return the interface type.
- The session's constructor performs NO I/O and cannot fail. `agentlib.SessionFactory.Create` is construction-only by contract, so the process spawn MUST be deferred to the first `Prompt` call. Do not spawn in `NewSession`, in `Create`, or in `NewSessionFactory`.
- `sessionFactory.Create(id string)` ignores `id` for the spawn: every id gets its own session and therefore its own process, and the id never reaches the argument vector. Continuity comes from the held process, not from `--session-id` or `--resume`.
- Do NOT add a field to `ClaudeRunnerConfig` for this. Spec 055 Constraints are explicit: pi's `PersistSession`/`SessionID` pair exists because pi is a subprocess that must be told to persist; the Claude session holds its own process, so there is no subprocess flag pair to mirror and no consumer for one. If you believe a field is genuinely required, name the consumer in the completion report instead of adding it.
- Do NOT change `claude.ClaudeRunner` or its generated mock.

## 5. Spawn — one process, argv only, no shell

Implement a single spawn function, `startProcess(ctx context.Context, config ClaudeRunnerConfig, decider PermissionDecider) (*exec.Cmd, io.WriteCloser, *bufio.Scanner, error)`, and a single argv builder, `sessionArgs(config ClaudeRunnerConfig, decider PermissionDecider) []string`.

- Build the argv with `exec.Command("claude", args...)` — never through a shell, never `sh -c`. `exec.Command` must appear EXACTLY ONCE in `claude/claude-session.go`, inside `startProcess`. Every other path can only reuse the process this one function started.
- Do NOT use `exec.CommandContext` for the session. The spawn context is the first request's context, and a process bound to it would die when that request returned. Termination is explicit: see requirement 8.
- `sessionArgs` base flags: `--print`, `--input-format stream-json`, `--output-format stream-json`, `--verbose`, `--strict-mcp-config`. Add `--permission-prompt-tool stdio` and `--permission-mode manual` only when `decider != nil`; add `--allowedTools <list>` only when `len(config.AllowedTools) > 0`; add `--model <name>` only when `config.Model != ""`. `manual` is required because the CLI's default permission mode approves a tool invocation without asking, which would leave a wired decider never consulted.
- Set `cmd.Dir` from `config.WorkingDirectory.Resolve(ctx)` only when `WorkingDirectory != ""`, and `cmd.Env` from `buildSubprocessEnv(ctx, config)` (the existing package-level function). Do not reimplement either.
- Take `cmd.StdinPipe()` and `cmd.StdoutPipe()`; hold the stdin `io.WriteCloser` and a `bufio.Scanner` on stdout in the `session` struct. Buffer the scanner exactly as the one-shot runner does: `scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)`.
- Log the spawn argv at `glog.V(2)` (it contains no prompt content). On `cmd.Start()` failure, close both pipes before returning, because `Start` does not close pipes created before it failed.
- The session id must not appear in the argv, in a shell string, or in any log line added by this package.

## 6. Turn loop — one write, read to the terminal event

Implement `Prompt(ctx context.Context, prompt string) (string, error)` so one call performs exactly one turn:

1. Obtain the held process from a `process(ctx)` helper that locks the session mutex, returns an error if the session is already closed, and starts the process on first use only. If `startProcess` fails, mark the session closed so a later call cannot retry silently.
2. Write ONE newline-terminated JSON turn to the held process's stdin in the shape of requirement 2 section 2. Write directly to the `io.WriteCloser`; an OS pipe does not buffer, so no explicit flush is needed. Marshal failure and write failure are both wrapped errors (`errors.Wrap(ctx, err, "write turn")`).
3. Read stdout line by line until the `result` event. Parse each line with the existing `claudeEvent` type extended in place for the control-request envelope (requirement 7); do not fork a parallel event vocabulary. Return `event.Result` on `type == "result"`.
4. Keep the loop single-threaded. Do NOT add a reader goroutine: a permission response is written from the same loop iteration that read the request, and no stdout is read outside a turn. This satisfies the process-hygiene constraint and avoids the no-raw-goroutine rule entirely.
5. Watch the turn's context with `context.AfterFunc(ctx, func() { _ = cmd.Process.Signal(syscall.SIGTERM) })` and `defer stop()`, so a blocked scanner read cannot outlive a cancelled turn. `context.AfterFunc` is not a raw `go func` and is the preferred mechanism.
6. If the turn ends without a `result` event: if `ctx.Err() != nil`, return `errors.Wrap(ctx, ctx.Err(), "turn cancelled")`; otherwise call `cmd.Wait()` and return `errors.Wrap(ctx, waitErr, "claude process ended mid-turn")`, using `errors.New(ctx, "claude process closed its output")` when `Wait` returns nil. Never report such a turn as successful.
7. Any turn error — cancellation, malformed event, process death, permission failure — calls one `abort(cmd)` helper that marks the session closed, kills the process if `cmd.Process != nil`, and calls `cmd.Wait()`. A later `Prompt` on that session must fail with a clear wrapped error (for example `errors.New(ctx, "claude session is closed")`) and must NEVER re-spawn or silently start a fresh conversation.

## 7. Permission routing

Extend the parsed event with the control-request envelope. The exact JSON shape is in requirement 2 section 5. For each event read in the turn loop:

- If `event.Type != "control_request"`, it is a conversation event: ignore it and continue reading. Do not log its content.
- If `event.Type == "control_request"` and the request is nil or its subtype is neither `can_use_tool` nor `permission`, fail the turn with `errors.Errorf(ctx, "unsupported control request from claude process")`. Never ignore a control request.
- If the decider is nil, fail the turn with `errors.New(ctx, "permission requested but no decider is configured")`. Do not auto-allow, auto-deny, or retry.
- Otherwise build a `PermissionRequest` from `tool_name`, `description`, and a bounded preview of `input` (cap at 512 bytes; the preview is never logged), then call `DecidePermission` with the turn's context. The turn BLOCKS here — the pause is the held process waiting on the answer, not a busy loop. Do not poll and do not sleep.
- If `DecidePermission` returns an error, fail the turn with `errors.Wrap(ctx, err, "decide permission")`. A permission request raised while the HTTP surface is unavailable must fail the turn rather than block indefinitely.
- On a decision, write ONE newline-terminated `control_response` JSON line back to the same process's stdin, carrying the same `request_id`, `response.subtype: "success"`, and `behavior: "allow"` or `"deny"` (plus the optional `message`). A write failure fails the turn with a wrapped error. Then continue reading stdout for the terminal event.

## 8. Close and cancellation — process hygiene

- `Close(ctx context.Context) error` closes the held stdin pipe first — the CLI's input stream ends and it exits on its own — waits with a bounded grace period (`const sessionCloseGrace = 2 * time.Second`), and kills the process only if it is still running. Implement the grace with `context.WithTimeout` plus `context.AfterFunc` calling `cmd.Process.Kill()`. Return nil after a successful reap.
- `Close` must be idempotent: a second call must not panic and must not block. Detach the process under the mutex and mark the session closed, so a concurrent `Prompt` cannot reuse it.
- `Close` after the process has already died must still succeed and must not panic.
- Cancellation of the context passed to an in-flight `Prompt` terminates the process (requirement 6 step 5) and leaves the session unusable. No goroutine may outlive `Close`. If you introduce no goroutine, say so in the completion report.
- One process per session, terminated on `Close` and on cancellation. A leaked process per session is a resource leak the pod cannot recover from without a restart.

## 9. Tests — AC1, failure modes, security: `claude/claude-session_test.go`

Create or extend this file in the external `package claude_test`. Use Ginkgo v2 / Gomega. Redeclare a `writeSessionShim(body string) string` helper inside the `Describe` (the established convention in `claude/claude-runner_test.go`): it writes `#!/bin/sh\n` + `body` to `$TMPDIR/claude`, makes it executable with mode `0755` (annotate `//nolint:gosec`), prepends the temp dir to `PATH`, registers a `DeferCleanup` restoring the original `PATH`, and returns the shim directory.

Mechanical detail that will otherwise waste a cycle: the session passes only the env allowlist plus `ClaudeRunnerConfig.Env`. A process spawned by name receives `argv[0]` as the bare name `claude`, so `dirname "$0"` is unreliable. Hand the shim its directory explicitly via `ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR": shimDir}}` and have the shim read `$SHIM_DIR`. Open sessions with `claude.NewSession(config, decider)` and `DeferCleanup` their `Close`.

Use this long-lived shim as the base:

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

Required `It` names (the verification greps these exact strings). Put the first four in `Describe("claudeSession held process")` and the rest in `Describe("claudeSession failure and permission")`:

1. **"starts exactly one process across two sends"** — send `turn-one` then `turn-two`; `starts.log` has exactly ONE line. A per-turn `claude --print --resume <id>` wrapper produces two and fails.
2. **"writes both turns' payloads to that one process's stdin"** — `stdin.log` has exactly TWO lines; line 1 contains `turn-one`, line 2 `turn-two`; the two sends returned `reply-1` and `reply-2`. Also unmarshal line 1 as JSON and assert `type == "user"`, `message.role == "user"`, and the single content block is `{"type":"text","text":"turn-one"}`. A count-only assertion is insufficient.
3. **"the process is still running when the second turn is written"** — after the second send, `turn-2.marker` exists, its recorded pid equals the single pid in `starts.log`, and `os.FindProcess(pid).Signal(syscall.Signal(0))` succeeds. `*exec.Cmd` identity survives process death and proves neither liveness nor that the same process consumed turn 2.
4. **"spawns with the stream-json protocol flags"** — `args.log` (one argument per line) contains `--print`, `--input-format` immediately followed by `stream-json`, `--output-format` immediately followed by `stream-json`, and `--verbose`.
5. **"fails the next turn when the held process died between turns"** — shim reads one line, emits one `result`, then exits. Turn 1 succeeds; turn 2 returns an error; `starts.log` still has exactly ONE line (no re-spawn, no silent fresh conversation).
6. **"fails the turn on an unparseable event without logging its content"** — shim emits a malformed line carrying a canary such as `LEAK-CANARY`. Assert the send errors, `err.Error()` does NOT contain the canary, and the error does carry the line's shape (byte length). Capture `glog` output for the duration of the send (reuse the `captureGlog` pattern: swap `os.Stderr` for a pipe, restore it, read the buffer) and assert no captured line contains the canary. The security rule is about log output, not only the returned error.
7. **"routes a permission request out and writes the verdict back"** — shim reads the turn, writes `{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","description":"run a command","input":{"command":"ls"}}}`, reads the response, records it, then writes `{"type":"result","result":"done-after-permission"}`. Use `mocks.ClaudePermissionDecider` with `DecidePermissionStub` returning `PermissionDecision{Allow: true}`. Assert the decider was called exactly once with `ToolName == "Bash"`, the recorded response parses with `request_id == "req-1"` and `behavior == "allow"`, and the send returned `done-after-permission`.
8. **"fails the turn when the permission decider errors"** — same shim; the decider stub returns an error. The send returns an error and no result, and does not block.
9. **"builds the same argv regardless of the session id"** — create two sessions from two factories with different `Create(id)` ids and the same config; `args.log` is byte-identical for both. This is the "id cannot inject a flag or shell syntax" rule made executable.
10. **"terminates the process on Close"** — send one turn, call `Close`, then assert `exited.log` appears within a bounded `Eventually` and a subsequent `Prompt` fails.
11. **"terminates the process when an in-flight turn's context is cancelled"** — shim records the turn then blocks (`sleep 30` with stdout redirected, or `trap 'exit 1' TERM INT` plus a background sleep and `wait`). Call `Prompt` with `context.WithTimeout(context.Background(), 2*time.Second)`. Assert the send errors, a follow-up send on the same session also errors, and `starts.log` still holds exactly ONE line. Also assert the process is actually gone: `exited.log` appears within a bounded `Eventually` and the pid no longer answers `Signal(syscall.Signal(0))`. Without the liveness half, a session that merely returns `ctx.Err()` after the shim's own sleep would pass.
12. **"writes a denial back when the decider denies"** — decider returns `PermissionDecision{Allow: false, Message: "no"}`; the recorded response carries `behavior == "deny"` and the message.
13. **"fails the turn when a permission is requested with no decider"** — `NewSession(config, nil)` plus the permission shim; the send errors and does not auto-allow.
14. **"fails the turn on an unsupported control request"** — shim sends a `control_request` with `subtype: "other"`; the send errors.

Add a structural guard: read `claude-session.go` in the test and assert `strings.Count(source, "exec.Command(") == 1`. This is what makes "one spawn path" true by construction.

Use counterfeiter mocks only (`mocks.ClaudePermissionDecider`, `mocks.SessionFactory`); never hand-write a mock. Coverage for the new code must be >= 80%.

## 10. AC3 — the implementation is substitutable in the shared service

Spec 055 AC3: the Claude session implementation must be substitutable for the pi implementation in the shared service, driven through the HTTP path with the CLI boundary faked.

- Add the test to the shared service's own test package (`interactive_test`), in `interactive/claude-session_test.go` if it does not already exist, or extend it if it does.
- Read `interactive/service_test.go` first. Reuse its existing helpers — `newTestServer`, `postPrompt`, `type contractBackend`, and `runContractTable` — and call `runContractTable` a second time with a backend built from `claude.NewSessionFactory(config, decider)`. Do NOT copy the contract table: the point of AC3 is that one table exercises both backends, so a row added once is exercised by both.
- If the table's shape cannot take a second backend without a copy, say so in the completion report and add the smallest Claude-flavoured variant that still drives every row; do not silently duplicate the table.
- Install the fake CLI through `PATH` (a `claude` shell script in a temp dir, with `SHIM_DIR` in `ClaudeRunnerConfig.Env`) exactly as in requirement 9. The shim must stay alive across turns and answer every turn with a fixed `result` so the contract rows' response-body assertions hold.
- Also add an `It("holds one process across two prompts on one session id")`: two `POST /prompt` calls with the same `X-Session-Id`; `starts.log` has one line and `stdin.log` has both payloads.
- The pi implementation and its tests must keep passing unchanged.

## 11. Repository hygiene

- **`README.md`** — add the new surface to the existing package table. Verified current row: the `claude/` row already mentions the `Session` implementation; if it does not, add one sentence describing the held-process session and the `PermissionDecider` seam, consistent with the surrounding style.
- **`CHANGELOG.md`** — add an entry under `## Unreleased`. This tree has no `## Unreleased` section today (the newest section is `## v0.110.0`), so create it at the top, below the preamble and above the newest released section. Follow `changelog-guide.md`: `- feat: <what> [context]`, naming the held process and the permission routing.
- **`docs/claude-streaming-protocol.md`** — the file from requirement 2.

## 12. Scope containment

Edit or create ONLY these files:
- `claude/claude-session.go` (new or existing)
- `claude/claude-session-permission.go` (optional, only if you split the seam out)
- `claude/claude-session_test.go` (new or existing)
- `interactive/claude-session_test.go` (new or existing)
- `mocks/claude-permission-decider.go` (generated)
- `docs/claude-streaming-protocol.md` (new or existing)
- `README.md`
- `CHANGELOG.md`

Do NOT touch `pi/`, `metrics/`, `delivery/`, `healthcheck/`, the repository-root `agent-step.go`, `claude/agent-step.go`, `claude/task-runner.go`, `claude/result-deliverer.go`, or `claude/transcript.go`. Do NOT change `claude.ClaudeRunner` or its generated mock. Do NOT add a field to `ClaudeRunnerConfig`. Do NOT change `claudeRunner.Run`'s behaviour or re-extract `buildSubprocessEnv` / `resolveConfigDir` (they are already package-level). Do NOT add a Prometheus metric for the session: the shared `Session.Prompt` returns plain text and spec 055 does not ask for one.

</requirements>

<constraints>
- **Dependency direction (hard precondition).** This prompt depends on spec 054's prompt 1, which lands the shared session interface and the HTTP service. Spec 055: "It cannot be implemented before that spec's prompt 1 has landed." The interface is verified present in this tree; if it is absent, stop and report a blocker — never define a second copy of the seam.
- **No Python SDK.** `ClaudeSDKClient` is a Python class from `claude-agent-sdk-python` and is NOT importable in this Go repository. Do not import, reference, vendor, or shell out to a Python SDK. This prompt targets the CLI protocol the SDK wraps: `claude -p --input-format stream-json --output-format stream-json --verbose`.
- **`ClaudeRunnerConfig` gains no flag pair (hard veto).** Spec 055 Constraints: pi's `PersistSession`/`SessionID` exists because pi is a subprocess that must be told to persist; the Claude session holds its own process, so there is no subprocess flag pair to mirror and no consumer for one. Add NO field to `ClaudeRunnerConfig` and NO opt-out flag anywhere.
- **`claude.ClaudeRunner` is frozen.** Its signature `Run(ctx context.Context, prompt string) (*ClaudeResult, error)`, its generated mock, and `claudeRunner.Run`'s observable behaviour all stay exactly as they are. The session is a sibling of the runner, not a replacement. Existing tests pass unmodified.
- **Process hygiene.** One process per session, terminated on `Close` and on cancellation of an in-flight turn. No background reader goroutine; no stdout read concurrent with a stdin write outside a single turn; no goroutine outliving `Close`. The spawn is deferred to the first turn because `SessionFactory.Create` is construction-only by contract.
- **No raw goroutines.** `go-concurrency/no-raw-go-func` is a MUST rule: no raw `go func(){...}()` outside `main.go` / `cmd/**`. Use `context.AfterFunc`; if a goroutine is genuinely unavoidable, use `github.com/bborbe/run`.
- **Error handling.** Errors are wrapped with `github.com/bborbe/errors` — `errors.Wrap(ctx, err, "…")`, `errors.Wrapf(ctx, err, "…")`, `errors.New(ctx, "…")`, `errors.Errorf(ctx, "…")`. Never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in business logic.
- **Code conventions** (per `docs/dod.md`). Exported types, functions and interfaces carry GoDoc comments. Interface → Constructor → Struct → Method, with the implementation struct private and named as the interface with a lowercased first letter. Factory functions are pure composition — no conditionals, no I/O, no `context.Background()`. No package-level mutable state and no `init()`.
- **Security.** Turn content is never logged — a malformed line's SHAPE (byte length, at most a short digest) may be recorded, its content may not. The tool-input preview is never logged. The process is spawned with an argument vector, never a shell string. The permission verdict is the only instruction accepted from outside the process, and it is an allow/deny only.
- **No probe word.** Spec 055 AC2 asks the session to remember a word the operator picks AT RUN TIME. Do not write any such word, or any test that depends on one, into this repository. The in-container tests prove continuity by asserting both turns reach one process's stdin (requirement 9 row 2); the memory contract itself is the operator's host-side probe.
- **Tests.** Ginkgo v2 / Gomega in external `*_test` packages (`claude_test`, `interactive_test`), counterfeiter mocks only (`mocks.ClaudePermissionDecider`, `mocks.SessionFactory`) — never hand-write a mock, never add an in-package `package claude` test file. Coverage for the new code must be >= 80%.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; nestif complexity 4.
- **No new module.** No new dependency is required. No `replace` or `exclude` directive is introduced.
- **Repository hygiene.** `README.md` is updated and `CHANGELOG.md` gets an entry under `## Unreleased`; `docs/claude-streaming-protocol.md` is written.
- Existing tests must still pass.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable.

```bash
# 1. The session file exists.
ls /workspace/claude/claude-session.go

# 2. Exactly ONE process spawn in the session file.
grep -cE 'exec\.Command\(' /workspace/claude/claude-session.go
# Must print exactly 1.

# 3. The streaming-input protocol flags are constructed.
grep -n -- '--input-format' /workspace/claude/claude-session.go
grep -n -- '--output-format' /workspace/claude/claude-session.go
# Each must match at least one line. Smoke check only — the shim test is the evidence.

# 4. No Python SDK reference anywhere in the Go tree.
! grep -rn 'claude_agent_sdk\|ClaudeSDKClient' --include='*.go' /workspace

# 5. No session-continuity field was added to the config (hard veto).
! grep -n 'SessionID\|PersistSession' /workspace/claude/claude-runner-config.go

# 6. The ClaudeRunner interface is unchanged.
grep -n 'Run(ctx context.Context, prompt string) (\*ClaudeResult, error)' /workspace/claude/claude-runner.go
# Must match exactly once.

# 7. The permission decider seam and its generated mock exist.
ls /workspace/mocks/claude-permission-decider.go
grep -n 'PermissionDecider' /workspace/claude/claude-session.go

# 8. The protocol reference is written.
ls /workspace/docs/claude-streaming-protocol.md
grep -c '^## ' /workspace/docs/claude-streaming-protocol.md
# Must be >= 5.

# 9. AC1's observable evidence — one process, both turns on its stdin, still alive at turn 2.
go test -mod=mod -race -v ./claude/ > /tmp/claude-session-test.log 2>&1
# Must exit 0. Each of these patterns must match at least one line:
grep -E "starts exactly one process across two sends|writes both turns' payloads to that one process's stdin|the process is still running when the second turn is written|spawns with the stream-json protocol flags" /tmp/claude-session-test.log

# 10. The failure-mode, permission and security rows.
grep -E "fails the next turn when the held process died between turns|fails the turn on an unparseable event without logging its content|routes a permission request out and writes the verdict back|fails the turn when the permission decider errors|builds the same argv regardless of the session id|terminates the process on Close|terminates the process when an in-flight turn's context is cancelled" /tmp/claude-session-test.log

# 11. AC3 — the Claude session is substitutable in the shared service.
go test -mod=mod -race -v ./interactive/ > /tmp/interactive-test.log 2>&1
# Must exit 0; the shared contract rows must appear driven by the Claude factory.
grep -E "holds one process across two prompts on one session id|claude backend" /tmp/interactive-test.log

# 12. Coverage for the new code.
go test -coverprofile=/tmp/cover.out -mod=mod ./claude/ && go tool cover -func=/tmp/cover.out | grep -E 'claude-session|total'
# The new session functions must each be >= 80%.

# 13. No findings.
go vet ./...

# 14. Full pipeline — format, generate, test, lint, license.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then run `make precommit` once more at the end.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **This spec's implementation already exists in the working tree.** `claude/claude-session.go`, `claude/claude-session_test.go`, `interactive/claude-session_test.go`, `docs/claude-streaming-protocol.md`, and `mocks/claude-permission-decider.go` are all present, and `prompts/completed/223-spec-055-claude-streaming-session.md` records a prior run. The prompt therefore includes a baseline-check requirement (requirement 1) so a re-run verifies and closes gaps instead of churning working code. The reviewer should confirm whether this generation is a deliberate re-run; if not, this prompt is redundant with the completed one.
- **AC2 is operator-only and is deliberately not a container check.** The real `claude` binary is not present in the build container, so the two-turn memory probe (operator-chosen word, both responses, the `ps` observation taken during turn 2, and the pinned `claude --version`) runs on the host after merge and is recorded in the PR description. No probe word is written into the repository.
- **The exact flag set is confirmed at AC2 time, not here.** Spec 055 Assumptions require the flag set to be confirmed against the pinned CLI version and recorded with AC2's result. `--permission-prompt-tool stdio` and `--permission-mode manual` are included because spec 055 Desired Behavior 3 requires permission requests to leave the process; if the pinned CLI rejects or renames them, the flag set lives in exactly one function (`sessionArgs`) so the correction is a one-line change plus a doc update.
- **The permission seam is defined here, not by spec 054.** Spec 054's service defines only `/readiness`, `/metrics` and `POST /prompt`, and its session interface models prompt/close — neither carries a permission hook. Spec 055 Desired Behavior 3 and its Security section nevertheless require the request to leave the process carrying a tool name, a description and an input preview and to accept only an allow/deny verdict back, which is the `PermissionDecider` shape in requirement 3. Binding it to the pod's HTTP surface is the `agent-claude` hand-off named in spec 054's Hand-offs table, not a prompt of either spec.
- **The `*exec.Cmd`-identity half of AC1 is satisfied structurally, not by a field accessor.** The repo tests in external `*_test` packages, so a test cannot read the session's private `cmd` field. Requirement 9 asserts the stronger observable: one pid in `starts.log`, the same pid consuming turn 2, that pid still alive, and exactly one `exec.Command` call site in the file (verification step 2). A literal `*exec.Cmd` equality assertion would require exporting a test accessor, widening the public API for a test.
- **Git-dependent checks are kept on the operator/manager side.** This prompt's verification uses plain `grep` rather than `git grep`, and carries no `git diff` scope check, because the daemon does not treat a failed verification command as a failure.
