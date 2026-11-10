---
status: draft
created: "2026-11-10T18:10:00Z"
---

# Force the Claude CLI to run under a caller-supplied session id

<summary>
- A caller can hand the `claude` package a session id, and the spawned CLI runs under exactly that id.
- The id reaches the CLI as `--session-id <uuid>`, so the transcript file, the stream's `session_id`, and the caller's own record are the same value by construction.
- Unset means unchanged: every existing caller keeps today's argv byte for byte, so no current behaviour moves.
- The value is validated as a session-id shape before it can reach the argument vector, so a caller-supplied string cannot inject a flag.
- Both argv builders are covered: the long-lived stream-json session and the one-shot runner.
</summary>

<objective>
Add a `SessionID` field to `ClaudeRunnerConfig` and, when it is non-empty, append `--session-id <id>` to the argv of both the long-lived session process and the one-shot runner, so a caller that has already recorded a session id can force the CLI to adopt it. A remote agent run whose task file carries `claude_session_id` is otherwise unanchorable: the census looks the id up in the vault, and the CLI would pick its own.
</objective>

<context>
Read `CLAUDE.md` for project conventions: single root module, `make test` and `make precommit` from the repository root, errors via `github.com/bborbe/errors`, `New*` returns the interface.

Read `claude/claude-runner-config.go` — the config struct. `RunAsUID` and `Env` are the two existing examples of a caller-supplied value that must not silently change behaviour when unset; follow their doc-comment style.

Read `claude/claude-session.go` — `sessionArgs` is the single place the long-lived process's argv is built. Its doc comment currently states the argv deliberately does not depend on the session id. That sentence must be rewritten: continuity still comes from the held process, while the *identity* now comes from the caller.

Read `claude/claude-runner.go` — the one-shot runner builds its argv inline around line 273 and spawns at line 299. It is the second place a `claude` process is created, and the flag must appear there too, or a caller using `ClaudeRunner` instead of `Session` would silently get an unforced id.

Read `claude/transcript.go` — `isValidSessionID` already defines the accepted shape (1..128 characters from `[A-Za-z0-9-]`). Reuse it rather than adding a second rule; it exists because the id becomes a file-name component, which is exactly the trust boundary here too.

Read `claude/claude-runner_test.go` and `claude/claude-session_test.go` — the existing argv assertions. Extend them; do not add a parallel test file.
</context>

<requirements>
1. Add `SessionID string` to `ClaudeRunnerConfig` with a doc comment stating: the value is the CLI session id the spawned process must adopt; empty means the CLI picks its own, which is today's behaviour; a non-empty value that is not a valid session-id shape is rejected rather than passed through.
2. Validate `SessionID` with `isValidSessionID` at the point of use. An invalid non-empty value must fail the run with a wrapped `github.com/bborbe/errors` error naming the reason (length or character set), never reach the argv, and never be logged in full — log only its length.
3. In `sessionArgs` (`claude/claude-session.go`), append `--session-id`, `<id>` when `config.SessionID` is non-empty. Rewrite the function's doc comment: the argv is now id-dependent, the id is validated so it cannot inject a flag or shell syntax, and continuity still comes from the held process.
4. In the one-shot runner's argv construction (`claude/claude-runner.go`, around line 273), append the same pair under the same condition.
5. Both append sites must place the flag before any `--model` argument, so a future positional argument cannot be swallowed as the id's value.
6. Add a spec for each builder asserting: empty `SessionID` produces an argv byte-identical to today's (assert on the whole slice, not a subset); a valid id produces the pair exactly once; an invalid id (a value containing a space, a `--`, or a `/`) fails before any process is started and the error names the offending shape rather than the value.
7. Add a `## Unreleased` CHANGELOG bullet describing the new field in the project's existing style.
</requirements>

<verification>
- `make test` passes from the repository root.
- `make precommit` passes.
- The empty-`SessionID` argv spec fails if the flag is appended unconditionally — confirm by temporarily removing the emptiness guard.
- `grep -n 'session-id' claude/claude-session.go claude/claude-runner.go` shows exactly the two append sites.
</verification>
