---
status: completed
summary: Made ALLOWED_TOOLS an enforced boundary in the headless runner and decider-less streaming session via --permission-mode dontAsk plus a belt-and-braces --disallowedTools Edit Write, backed by an exact-match AllowedTools.Contains helper, tests, docs and changelog.
execution_id: agent-exec-246-claude-allowed-tools-enforcement
dark-factory-version: v0.198.7
created: "2026-11-03T20:41:14Z"
queued: "2026-11-03T20:41:14Z"
started: "2026-11-03T20:46:47Z"
completed: "2026-11-03T20:54:41Z"
---

# Enforce ALLOWED_TOOLS in the claude runner and streaming session

<summary>
- An agent's configured tool list becomes an enforced boundary instead of a pre-approval hint.
- A tool outside that list is refused rather than silently executed by the headless CLI.
- An agent described as read-only can no longer edit files or run arbitrary Bash.
- The write tools are denied explicitly as well, so the guarantee does not depend on the permission mode alone.
- Agents that configure no tool list keep their current behaviour exactly.
- A streaming session with an approval decider keeps asking it, exactly as before.
- A streaming session without a decider is restricted the same way as the headless one.
</summary>

<objective>
Make the runner's `ALLOWED_TOOLS` setting actually restrict what a headless Claude session can run, so an agent configured with a read-only tool list cannot edit files or execute arbitrary Bash.
</objective>

<context>
Read `CLAUDE.md` for project conventions — in particular its rule that tests live in external `*_test` packages.

Read these files in full before editing:
- `claude/claude-runner.go` — `buildCommand` constructs the one-shot CLI's argv. It currently appends `--allowedTools` when `len(r.config.AllowedTools) > 0`, and appends nothing permission-related otherwise.
- `claude/allowed-tools.go` — `AllowedTools` is a `[]string` with `ParseAllowedTools` and `String()`. There is no membership helper.
- `claude/claude-runner_test.go` — the `Describe("claudeRunner AllowedTools buildCommand branch")` block is the pattern to extend. Its `writeShim` helper installs a shell script on `PATH` that echoes `$@`, so a test asserts the real argv.
- `claude/claude-session.go` — the streaming session's spawn path. `sessionArgs` adds `--permission-mode manual` only when a `PermissionDecider` is wired; a decider-less session falls through to the CLI's auto mode, which is the same pre-approval-only hole this change closes for the one-shot runner.

Why this exists: `--allowedTools` only pre-approves. Under `--print`, a tool that is not listed still runs, because no permission mode refuses it. `--permission-mode dontAsk` turns "not pre-approved" into "denied".
</context>

<requirements>
1. In `claude/claude-runner.go`, extend the existing `if len(r.config.AllowedTools) > 0` branch inside `buildCommand` so the emitted argv also carries, in that same branch and adjacent to the `--allowedTools` append:
   - `--permission-mode` followed by `dontAsk`
   - `--disallowedTools` followed by `Edit` and `Write`, but only when the configured list contains neither `Edit` nor `Write`
2. Determine membership by exact comparison against the strings `Edit` and `Write`. Do not test membership with `strings.Contains(a.String(), …)`: a substring test matches any longer tool name that merely contains the word (e.g. `MultiEdit`, or an `mcp__<server>__Write` entry), so it would wrongly conclude the write tool is already allowed and skip the deny. Put the check in one place — add `func (a AllowedTools) Contains(tool string) bool` to `claude/allowed-tools.go` and call it from both `buildCommand` and `sessionArgs` — so the two argv builders cannot drift, and cover it with a table test in `claude/allowed-tools_test.go` enumerating `Edit`, `Write`, `Read`, and a longer name containing `Edit`.
3. When `len(r.config.AllowedTools) == 0`, emit neither flag — no `--permission-mode`, no `--disallowedTools`. That case is unrestricted today and stays that way; `dontAsk` with an empty allowlist would deny every tool.
4. In `claude/claude-session.go`'s `sessionArgs`, leave the `--permission-mode manual` append exactly where it is, inside the `decider != nil` branch. Inside the existing `if len(config.AllowedTools) > 0` branch, add `--permission-mode dontAsk`, plus `--disallowedTools Edit Write` under requirement 1's membership condition (the list contains neither `Edit` nor `Write`), both only when `decider == nil`: a decider-less session has no approval path, so without these flags it keeps today's pre-approval-only hole. With a decider wired the branch is unchanged. Add a case to `claude/claude-session_test.go` asserting that a nil-decider session with `AllowedTools: claude.ParseAllowedTools("Read,Grep,Glob")` emits `--permission-mode dontAsk` and `--disallowedTools Edit Write`, and the symmetric case — `AllowedTools: claude.ParseAllowedTools("Read,Edit")` with a nil decider still emits `--permission-mode dontAsk` but NOT `--disallowedTools`. Rename the existing "leaves the permission flags off when no decider is wired" case, which stays green only because it sets no `AllowedTools`, to "leaves the permission flags off when neither a decider nor AllowedTools is configured". Build that session with `claude.NewSession(claude.ClaudeRunnerConfig{AllowedTools: …, Env: map[string]string{"SHIM_DIR": shimDir}}, nil)` directly, as the existing "passes the configured tools and model to the process" test does — the `openSession` helper hard-codes its config and cannot carry `AllowedTools`. Then update `docs/claude-streaming-protocol.md` § Launch, whose flag list states the argv lives in `sessionArgs`; that list currently omits the existing `--permission-mode manual`, so add it alongside the two new flags, and change the sentence "Three further flags are conditional" to match the new count.
5. Extend the `Describe("claudeRunner AllowedTools buildCommand branch")` block in `claude/claude-runner_test.go` with three cases, each reusing the existing `writeShim` helper and asserting on the echoed argv:
   - list without the write tools, e.g. `Read,Grep,Glob` → argv contains the exact pairs `--permission-mode dontAsk` and `--disallowedTools Edit Write`, asserted as adjacent tokens — split the echoed `result.Result` on whitespace with `strings.Fields` and compare with `indexOf`, as the session tests do — not as separate substrings
   - list including `Edit`, e.g. `Read,Edit` → argv contains the adjacent pair `--permission-mode dontAsk`, and does NOT contain `--disallowedTools`; repeat with `Read,Write` to pin the symmetric case
   - empty list → argv contains none of `--allowedTools`, `--permission-mode` or `--disallowedTools`
6. Add a `fix:` bullet to `CHANGELOG.md` stating that `ALLOWED_TOOLS` now restricts headless sessions instead of only pre-approving, and that any tool outside the list — including every `mcp__*` tool and every `Bash` command not matched by a `Bash(...)` entry — now fails instead of running. At the HEAD this was written against, the file has no `## Unreleased` section (its newest heading is `## v0.139.1`). If `## Unreleased` is still absent when you run, create it directly above the newest `## vX.Y.Z` heading, as commit `8980d37` did; if one exists, append to it — never append the bullet to a released section.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Keep the file's existing Interface → Constructor → Struct → Method layout and its `errors.Wrap(ctx, err, …)` convention.
- Add no new dependency.
- Use the exact flag spellings `--permission-mode` with the value `dontAsk`, and `--disallowedTools` with the two values `Edit` and `Write` — no synonyms and no case variants. Do NOT try to confirm them against a real `claude` binary, and do NOT name a CLI version in the `## Unreleased` bullet: this repo does not record the pinned `@anthropic-ai/claude-code` version (spec 055 AC2 is still open), and `docs/claude-streaming-protocol.md` § Version confirmation states the confirmation is operator-executable against the host's real binary, not the YOLO container's.
- Keep `--disallowedTools Edit Write` even though `dontAsk` already denies unlisted tools. It is a deliberate belt-and-braces deny for the two write tools that matter most, and it is the pair the change is verified against; do not "simplify" it away.
- Do not reorder the existing `--model` append relative to the `--allowedTools` append.
- Before finishing, re-run the `<verification>` commands and confirm they pass, then walk each numbered requirement against the change.
</constraints>

<verification>
Run from the repo root:

```bash
make test
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --config .golangci.yml ./claude/...
grep -rn -- '--permission-mode' claude/claude-runner.go claude/allowed-tools.go
grep -rn -- '--disallowedTools' claude/claude-runner.go claude/allowed-tools.go
grep -n -- 'dontAsk' claude/claude-session.go
grep -n -- '"manual"' claude/claude-session.go
```

All four greps must print at least one line. The first two must resolve inside `buildCommand` (or in a shared constant in `allowed-tools.go` that `buildCommand` uses); the third must be inside `sessionArgs`; the fourth must still print its original line so the decider branch is provably unchanged.
</verification>
