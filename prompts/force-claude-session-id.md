---
status: draft
created: "2026-11-10T18:10:00Z"
---

# Force the Claude CLI to run under a caller-supplied session id

<summary>
- A caller can pin the session id of the CLI process this package spawns, and the process runs under exactly that id.
- The id is handed to the spawned CLI, so the transcript file, the reported session id, and the caller's own record are the same value by construction.
- Unset means unchanged: every existing caller keeps today's argv byte for byte, so no current behaviour moves.
- The value is checked against the package's existing session-id rule before it can reach the command line, so a caller-supplied string cannot inject a flag.
- Both ways the package starts a CLI process are covered: the long-lived interactive one and the one-shot one.
</summary>

<objective>
Let a caller pin the session id of the CLI process the package spawns: when one is supplied it is used, when none is supplied nothing changes. A remote agent run whose task file already records a session id is otherwise unanchorable — the consumer looks that id up in the vault, and the CLI would pick its own.
</objective>

<context>
Read `CLAUDE.md` for project conventions: single root module, `make test` and `make precommit` from the repository root, errors via `github.com/bborbe/errors`, `New*` returns the interface.

Read `docs/claude-streaming-protocol.md` § Launch — it enumerates the session's flags and currently asserts, at lines 52-54, that the session id is deliberately not passed. Requirement 7 rewrites that paragraph.

Read `docs/dod.md` for the project Definition of Done (including the `## Unreleased` CHANGELOG entry).

Read `claude/claude-runner-config.go` — the config struct. `RunAsUID` and `Env` are the two existing examples of a caller-supplied value that must not silently change behaviour when unset; follow their doc-comment style.

Read `claude/claude-session.go` — `sessionArgs` (line ~443) builds the long-lived process's argv and returns `[]string` with no `ctx`. Its doc comment states the argv deliberately does not depend on the session id; that sentence must be rewritten. `startProcess` (line ~489) is where the `ctx` lives and therefore where validation belongs.

Read `claude/claude-runner.go` — the one-shot runner builds its argv in `buildCommand` (line ~269) and spawns at line 299. It is the second place a `claude` process is created, and the flag must appear there too, or a caller using `ClaudeRunner` instead of `Session` would silently get an unforced id.

Read `claude/transcript.go` — `isValidSessionID` (line 33) defines the accepted shape: 1..128 characters from `[A-Za-z0-9-]`. Reuse it rather than adding a second rule. It guards the transcript file name, which is the same trust boundary.

Read `claude/claude-runner_test.go` and `claude/claude-session_test.go` — the existing argv assertions, which already use the `SHIM_DIR` argv shim. Extend them; do not add a test file.

Follow the coding plugin's `docs/go-doc-best-practices.md` for the exported type's doc comment and `docs/go-error-wrapping-guide.md` for the error shape; if those paths do not resolve in the container, follow the doc-comment style of `RunAsUID` and `Env` in `claude/claude-runner-config.go` instead of searching the filesystem.
</context>

<requirements>
1. Add `type SessionID string`, doc-commented, in a new file `claude/claude-session-id.go`, and a `SessionID SessionID` field to `ClaudeRunnerConfig` in `claude/claude-runner-config.go` (after `Model`). The doc comment states: the value is the CLI session id the spawned process must adopt; empty means the CLI picks its own, which is today's behaviour; a non-empty value that is not a valid session-id shape is rejected before the process starts. It also states what happens when the id already has a transcript under the config dir: the package does not detect the collision and passes the id through, and the CLI's own behaviour decides (see `findTranscriptFile` in `claude/transcript.go`). Name the one collision the package itself causes: a rate-limit re-invoke re-runs `buildCommand` and starts a second process under the same `--session-id` while the abandoned attempt's transcript exists. Do not add collision detection.
2. Validate `SessionID` with `isValidSessionID` in `startProcess` (`claude/claude-session.go`, line ~489 — it has the `ctx` the error needs) immediately before it calls `sessionArgs`, and in `buildCommand` (`claude/claude-runner.go`, line ~269) before the argv is built. `sessionArgs` keeps its `[]string` signature and only ever receives an already-validated id. An invalid non-empty value fails the run with a new `errors.Errorf` error whose message is exactly `invalid SessionID (length %d): must be 1..128 characters from [A-Za-z0-9-]` — the length, never the value — and it never reaches the argv.
3. In `sessionArgs` (`claude/claude-session.go`), append `"--session-id"` and the id as two separate argv elements when `config.SessionID` is non-empty. Rewrite the function's doc comment: the argv is now id-dependent, the id is validated before it arrives, and continuity still comes from the held process.
4. In `buildCommand` (`claude/claude-runner.go`, line ~269), append the same pair under the same condition.
5. Append the pair in both builders immediately before the existing `if config.Model != ""` block (`claude/claude-session.go` line ~477 and `claude/claude-runner.go` line ~293), so the two builders stay textually parallel.
6. Add a spec for each builder asserting: empty `SessionID` produces the exact argv slice — for the session, `["--print","--input-format","stream-json","--output-format","stream-json","--verbose","--strict-mcp-config"]` with a nil decider and an otherwise empty config; for the one-shot runner, `["--print","--output-format","stream-json","--verbose","--strict-mcp-config"]` — asserted as a whole slice, not a subset; a valid id produces `"--session-id"` followed by the id exactly once; an invalid id fails before any process is started — assert the shim's `starts.log` (`sessionShimBase`) was never written — and the error names the reason rather than the value. Use as invalid fixtures: `abc def` (space), `abc/def` (slash), `abc_def` (character outside the set), and a 129-character value. Do NOT use `--` as an invalid fixture — `-` is an allowed character, so `isValidSessionID` accepts it.
7. Update `docs/claude-streaming-protocol.md` § Launch: add `--session-id <id>` to the conditional flag list, change "Six further flags are conditional" to "Seven", and replace the paragraph at lines 52-54 beginning "The session id is deliberately **not** passed." The replacement says: the id is now passed when the caller supplies one; continuity still comes from the held process rather than from `--resume`; and the id is validated against the transcript-file-name rule before it reaches the argv, so it cannot inject a flag or shell syntax. Also correct the paragraph that follows: the flag set no longer lives in exactly one function, since `--session-id` is added in both `sessionArgs` and `buildCommand`.
8. `NewSessionFactory` (`claude/claude-session.go`, line ~96) is the package's second per-id entry point and its doc comment must stop asserting the opposite of the new behaviour. It currently says "The id is not part of the spawn: continuity comes from the held process, not from a CLI flag, so the argv is identical for every id and no id can reach the argument vector." Rewrite that sentence: the id passed to `Create` names the session record only; the CLI session id comes from `ClaudeRunnerConfig.SessionID`. Add that `SessionID` is a per-process value, so a factory whose base config sets it must serve exactly one id — `Create` discards its argument (`func (f *sessionFactory) Create(_ string)`), so two factory sessions under one base config would run as the same CLI session and share one transcript. Leave `Create` discarding the id; only the comment and the constraint are new.
9. Add a `## Unreleased` CHANGELOG bullet describing the new field in the project's existing style.
</requirements>

<constraints>
- Do NOT commit, push, tag, or release. Leave the working tree for the caller to review.
- Do NOT change the `sessionArgs` signature: it stays `func sessionArgs(config ClaudeRunnerConfig, decider PermissionDecider) []string`. Validation happens in `startProcess` and `buildCommand`, which have the `ctx` that the wrapped error requires.
- Do NOT add a second session-id rule. `isValidSessionID` in `claude/transcript.go` is the only validator; reuse it unchanged. In particular do NOT make it reject `-` or `--`: real UUIDs contain hyphens, and the same function guards the transcript file lookup every session depends on.
- Do NOT add a new test file. Extend `claude/claude-session_test.go` and `claude/claude-runner_test.go`.
- Errors via `github.com/bborbe/errors` only; never `fmt.Errorf`.
- Run `make test` and `make precommit` from the repository root; there is a single root module.
</constraints>

<verification>
- `make test` passes from the repository root.
- `make precommit` passes.
- `make test` covers the empty-`SessionID` case by asserting the full argv slice; that assertion is what fails if the emptiness guard is dropped.
- `grep -c '"--session-id"' claude/claude-session.go claude/claude-runner.go` prints `1` for each file.
- `grep -n 'deliberately \*\*not\*\* passed' docs/claude-streaming-protocol.md` returns nothing — the paragraph asserting the id is not passed is gone.
</verification>
