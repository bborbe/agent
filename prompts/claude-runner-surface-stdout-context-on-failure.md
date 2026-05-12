<summary>
- `lib/claude/claude-runner.go:55` calls `scanOutput` which drops every event in the Claude CLI `--output-format stream-json` stream that isn't a `result` event. When `cmd.Wait()` returns non-zero (line 57-59), the error wrap is `errors.Wrapf(ctx, err, "claude CLI failed: %s", stderr.String())`. The `--output-format stream-json` mode writes diagnostic events to STDOUT, not stderr, so `stderr.String()` is always empty — the wrap renders as the literal string `claude CLI failed: : exit status 1` (double-colon from empty `%s`). Operators see a body section with no information about WHY claude failed (OAuth expired? rate limit? 401? network?).
- Live evidence on dev 2026-05-12: a real OAuth-broken pr-reviewer Job emitted `## Failure - **Reason:** pr-plan claude run failed: claude CLI failed: : exit status 1` — exit code present, but nothing about the actual auth issue. The diagnostic info was somewhere in the stream-json stdout; `scanOutput` discarded it.
- Fix shape: change `scanOutput` to also accumulate a ring buffer of the last N (= 5) non-empty stdout lines (raw bytes, capped at ~2KB total to bound memory). Return both `resultText` and the ring buffer. In `Run()`, when `cmd.Wait()` returns non-zero, include the ring buffer in the error wrap so the next layer (`Result.Message`) carries actual diagnostic content. Drop the empty `stderr.String()` — it has never carried useful info in stream-json mode.
- Defensive design: shape-agnostic. We don't depend on knowing the exact JSON of error events (which can shift across Claude CLI versions). The last few stdout lines are whatever the CLI emitted right before exit — they contain the failure information by construction.
- Bound memory + bound output: cap each captured line at ~512 bytes, cap total to 5 lines, total ~2KB. Long assistant content events that happen to be in the tail get truncated, not dropped.
- No public API change. No new exported types. No behavior change on success path. Only the failure-path error message becomes useful.
- Add unit tests in `lib/claude/claude-runner_test.go` using a fake `claude` command (shell script under `t.TempDir()` shimming `claude`) that emits multiple stream-json lines then exits 1, and asserts the resulting error's `.Error()` contains the last line's content.
- New lib tag: `lib/v0.62.0` (additive behavior change; not a breaking API change).
</summary>

<objective>
Make Claude CLI failure messages diagnose-without-logs. Today, operators get `claude CLI failed: : exit status 1` and must race the agent pod's TTL cleanup to grab `kubectl logs <pod>` for the actual reason. After this fix, the `Result.Message` (and therefore the `## Failure` body section written by the content generator) carries the tail of the stream-json output verbatim — which is where Claude CLI puts its error events in `--output-format stream-json` mode.

The downstream contract is already in place: the `passthrough/fallback/section` generators (lib/v0.61.0) all splice `Result.Message` into a `## Failure` body section. This fix improves the *content* of that message so the body section is actually useful.

Anti-pattern to avoid: trying to parse known Claude error-event JSON shapes (e.g. `is_error: true`, `subtype: error_during_execution`). The CLI's event schema is not API-stable across versions. A raw-tail approach is forward-compatible by construction — whatever the CLI emits right before exiting goes into the operator's view.
</objective>

<context>

Read `CLAUDE.md` at repo root for project conventions (Ginkgo/Gomega tests, multi-module mono-repo, `lib/` is its own submodule with `lib/vX.Y.Z` tags).

## Files to edit

### `lib/claude/claude-runner.go`

Two changes:

**(a) `scanOutput` signature + body (lines 119-153)**

Current signature:

```go
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) string {
```

New signature:

```go
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) (resultText string, recentLines []string) {
```

In the scanner loop body, after the existing logic, append the line to a ring-buffer slice capped at 5 entries. Each line truncated to 512 bytes (use `truncateString(string(line), 512)` — add the helper below). Drop empty lines.

Behavior:
- On success path (cmd exits 0), `recentLines` is ignored by the caller — no behavior change.
- On failure path, `recentLines` carries the tail of the stdout stream.

Pseudocode for the ring buffer (inside the `for scanner.Scan()` loop, after JSON parsing):

```go
trimmed := bytes.TrimSpace(scanner.Bytes())
if len(trimmed) > 0 {
    recentLines = append(recentLines, truncateString(string(trimmed), 512))
    if len(recentLines) > 5 {
        recentLines = recentLines[len(recentLines)-5:]
    }
}
```

Place this AFTER the existing `if err := json.Unmarshal(...)` block — the `continue` on unmarshal failure should still capture the line to recentLines (malformed JSON is exactly the kind of thing we want to surface in failure output).

Restructure: move the `continue` on unmarshal failure to a labeled break or inline the ring-buffer push above the `continue`. Either is fine — the point is "every non-empty stdout line, regardless of parse success, gets into recentLines".

**(b) `Run()` lines 55-59 — capture both return values, drop stderr, format error**

Current:

```go
resultText := scanOutput(ctx, stdoutPipe)

if err := cmd.Wait(); err != nil {
    return nil, errors.Wrapf(ctx, err, "claude CLI failed: %s", stderr.String())
}
```

After:

```go
resultText, recentLines := scanOutput(ctx, stdoutPipe)

if err := cmd.Wait(); err != nil {
    return nil, errors.Wrapf(ctx, err, "claude CLI exited: %s", formatRecentLines(recentLines))
}
```

Drop the `var stderr bytes.Buffer; cmd.Stderr = &stderr` lines (48-49) entirely — stream-json mode writes nothing to stderr. (If concerned about losing rare stderr output, leave the assignment in place but stop reading it for the error message. Recommendation: drop it; `cmd.Stderr` defaults to discard which is what we want.)

Add `formatRecentLines` helper (file-local):

```go
// formatRecentLines renders the tail of the claude CLI stdout for inclusion in
// error messages. Empty input returns a descriptive placeholder so the resulting
// error message doesn't have a trailing colon-space (cosmetic), and so operators
// see "no diagnostic output" rather than thinking the message was truncated.
func formatRecentLines(lines []string) string {
    if len(lines) == 0 {
        return "(no stdout output)"
    }
    return strings.Join(lines, " | ")
}
```

Choice of separator ` | `: chosen so the resulting error message remains a single line (suits glog one-line-per-event), but operators can visually parse the boundary. Newlines `\n` would also work but break some log aggregators that treat newlines as event separators.

Add `truncateString` helper (file-local; if a same-named helper already exists in `lib/claude/`, reuse it):

```go
func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen] + "...(truncated)"
}
```

### `lib/claude/claude-runner_test.go` (NEW file)

The existing test file is `lib/claude/task-runner_test.go` (task runner, not claude runner). There is no `claude-runner_test.go` today — create it.

Test strategy: fake the `claude` binary by writing a shell script to `t.TempDir()` (or Ginkgo `BeforeEach` equivalent) and prepending it to `PATH`. The script emits canned stream-json output then exits with a configurable code. This exercises the real `claudeRunner.Run` path without needing a real Claude CLI.

Minimum test cases:

1. **Failure path — last lines surfaced**
   - Shim script emits 3 lines, last is `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"OAuth token expired"}`, then `exit 1`
   - Call `Run(ctx, "...")` → expect error
   - `err.Error()` MUST contain the substring `OAuth token expired`
   - `err.Error()` MUST contain `exit status 1` (Wait()'s err is wrapped)
   - `err.Error()` MUST NOT contain `claude CLI failed: : ` (the old double-colon bug)

2. **Failure path with no output — fallback placeholder**
   - Shim script emits nothing, then `exit 1`
   - `err.Error()` MUST contain `(no stdout output)`
   - `err.Error()` MUST contain `exit status 1`

3. **Failure path with > 5 lines — only last 5 retained**
   - Shim script emits 7 lines `line-1`...`line-7`, then `exit 1`
   - `err.Error()` MUST contain `line-7` (last)
   - `err.Error()` MUST contain `line-3` (5th from the end)
   - `err.Error()` MUST NOT contain `line-1` or `line-2` (dropped by ring buffer)

4. **Failure path with very long line — truncated to 512 bytes**
   - Shim script emits one line of `strings.Repeat("X", 2000)`, then `exit 1`
   - `err.Error()` MUST contain `...(truncated)`
   - `err.Error()` length is bounded (assert < 1500 chars or similar guard)

5. **Success path unchanged**
   - Shim script emits a valid `{"type":"result","result":"hello"}`, then `exit 0`
   - `Run` returns `(&ClaudeResult{Result: "hello"}, nil)`
   - No error, no behavior change vs current

6. **Malformed JSON line gets captured too** (regression-guard for "we silently drop malformed JSON")
   - Shim script emits `not-json-at-all`, then `{"type":"result","subtype":"error","is_error":true,"result":"final"}`, then `exit 1`
   - `err.Error()` MUST contain `not-json-at-all`
   - `err.Error()` MUST contain `final`

Suite setup pattern (sketch — adapt to existing Ginkgo conventions in `task-runner_test.go`):

```go
var _ = Describe("ClaudeRunner", func() {
    var (
        ctx     context.Context
        tempDir string
        oldPath string
    )

    BeforeEach(func() {
        ctx = context.Background()
        tempDir = GinkgoT().TempDir()
        oldPath = os.Getenv("PATH")
        os.Setenv("PATH", tempDir+":"+oldPath)
    })

    AfterEach(func() {
        os.Setenv("PATH", oldPath)
    })

    // helper to install a fake claude binary
    installFakeClaude := func(script string) {
        path := tempDir + "/claude"
        Expect(os.WriteFile(path, []byte(script), 0o755)).To(Succeed())
    }

    Describe("Run", func() {
        Context("when the claude CLI exits non-zero with stream-json output", func() {
            It("surfaces the tail of stdout in the error message", func() {
                installFakeClaude(`#!/bin/sh
echo '{"type":"system"}'
echo '{"type":"assistant"}'
echo '{"type":"result","is_error":true,"result":"OAuth token expired"}'
exit 1
`)
                runner := NewClaudeRunner(ClaudeRunnerConfig{})
                _, err := runner.Run(ctx, "prompt")
                Expect(err).To(HaveOccurred())
                Expect(err.Error()).To(ContainSubstring("OAuth token expired"))
                Expect(err.Error()).To(ContainSubstring("exit status 1"))
                Expect(err.Error()).NotTo(ContainSubstring("claude CLI failed: :"))
            })
        })
        // ... remaining contexts
    })
})
```

Match the existing test-file style in `lib/claude/task-runner_test.go` for suite registration (look for `RegisterFailHandler` / `RunSpecs`). If `task-runner_test.go` uses a suite file (e.g. `claude_suite_test.go`), reuse it — don't create a parallel suite.

### `CHANGELOG.md` (repo root)

Add a new entry. The next lib release will be `lib/v0.62.0`. Find the existing `## v0.61.0` section, insert a `## Unreleased` section ABOVE it with:

```
## Unreleased

- fix(lib/claude): surface tail of stream-json stdout in `claudeRunner.Run` error message when CLI exits non-zero. Previously the error was always `claude CLI failed: : exit status N` because (a) `cmd.Stderr` was read for the format string but `--output-format stream-json` writes nothing to stderr, and (b) `scanOutput` only kept `result` events and discarded everything else. Now: last 5 non-empty stdout lines (each truncated to 512 bytes) accumulate in a ring buffer and ride along in the wrapped error; on failure they end up in `Result.Message` which the content generators splice into the task page's `## Failure` body section. Operators no longer need `kubectl logs` to discover OAuth / 401 / rate-limit failures.
```

If `## Unreleased` already exists, append the bullet there — do NOT create a second `## Unreleased`.

### Release (post-prompt — NOT done by this prompt)

After this prompt lands, the operator releases `lib/v0.62.0` separately via the project's standard release flow (`/coding:commit` or whatever the project uses). This prompt does NOT release; it stops at `## Unreleased`.

</context>

<constraints>

- Do NOT change `claudeEvent` struct (`lib/claude/claude-event.go`) — the fix is shape-agnostic and avoids depending on Claude CLI's event schema.
- Do NOT remove the `glog.V(4).Infof("[line] %s", line)` debug log on line 132 — keeps debugging easy when the body-section context isn't enough.
- Do NOT add new exported types or functions. `formatRecentLines` and `truncateString` are package-private helpers.
- `scanOutput` signature change is a private function (lowercase first letter) so no external API break.
- Public `ClaudeRunner` interface and `Run` method signature MUST remain identical — same arguments, same return types.
- Ring buffer size = 5 lines. Per-line truncation = 512 bytes. Joined with ` | `. These are tunable but pick the documented values and don't bikeshed.
- Errors must be wrapped with `github.com/bborbe/errors` (the existing `errors.Wrapf` is already correct).
- `make precommit` MUST exit 0 in `lib/`. Run from `lib/` since this is a submodule.
- Test must use Ginkgo/Gomega (project convention per CLAUDE.md). No `testing.T`-style tests.
- Test must use a shim script in `t.TempDir()` — do NOT depend on a real `claude` CLI being installed in the test environment.
- Test must NOT spawn a real `claude` process — fully self-contained.
- The fake `claude` script needs `chmod +x` (mode `0o755`) — verify the test does this.
- If a `task-runner_test.go` style helper or fake exists for shimming `claude`, reuse it rather than rewriting.
- The whole change is one logical commit: `claude-runner.go` change + tests + CHANGELOG. Single commit.

</constraints>

<failure_modes>

| Trigger | Expected behaviour | Recovery |
|---|---|---|
| `scanOutput` consumes too much memory for a flood of long lines | Per-line truncation at 512 bytes + 5-line cap = ~2.5KB max accumulator | Per spec; no recovery needed |
| Test shim script fails to execute (permission denied) | Test setup writes script with mode `0o755`; if test still fails, OS-level perms issue | Add `t.Logf` / `GinkgoWriter` debug output of `ls -la $tempDir` |
| Test PATH manipulation breaks other tests in suite | `BeforeEach` saves `oldPath`, `AfterEach` restores | If parallel tests break, mark Ginkgo specs `Serial` |
| `scanOutput` ring buffer order wrong (oldest at end, newest at start) | Tests check both first-included and last-included line; explicit assertion of order | Inspect with `Expect(recentLines).To(Equal([]string{...}))` |
| Existing test in `task-runner_test.go` uses a different shim pattern | Reuse that pattern rather than introducing a parallel one | If the existing pattern is incompatible, document why in a comment and proceed with the new pattern |
| `make precommit` lint fails on new file (funlen, lll, gocyclo) | New test file may exceed thresholds | Split into multiple `It` blocks, extract helpers, never disable lints |
| `strings.Join` adds trailing separator on single-line case | `strings.Join` does NOT add trailing separator — verify by reading docs | N/A |
| Empty stdout case returns `nil`/`[]string{}` ambiguity | Test explicitly checks `(no stdout output)` substring | N/A — `formatRecentLines` handles both nil and empty |
| `errors.Wrapf` with very long format result truncates anything | `bborbe/errors` doesn't truncate format strings | If observed, add explicit truncation in `formatRecentLines` |
| Cosmetic: `claude CLI exited:` has trailing colon when output is empty | `formatRecentLines` returns `(no stdout output)`, never empty | N/A — covered |

</failure_modes>

<acceptance_criteria>

- [ ] `lib/claude/claude-runner.go` `scanOutput` signature is `(resultText string, recentLines []string)` (verify with `grep 'func scanOutput' lib/claude/claude-runner.go`).
- [ ] `lib/claude/claude-runner.go` `Run` method captures both return values from `scanOutput` and uses `recentLines` in the `errors.Wrapf` on the `cmd.Wait()` non-zero path.
- [ ] `lib/claude/claude-runner.go` no longer references `stderr.String()` in the error wrap message (verify with `grep 'stderr.String' lib/claude/claude-runner.go` — expect no match).
- [ ] `lib/claude/claude-runner.go` has new package-private helpers `formatRecentLines` and `truncateString` (or reuses an existing `truncateString`).
- [ ] `lib/claude/claude-runner_test.go` exists as a new file (or the tests are added to an existing test file in `lib/claude/` if one is more appropriate — verify with `ls lib/claude/*_test.go`).
- [ ] Test file uses Ginkgo `Describe("ClaudeRunner", ...)` / `Describe("Run", ...)` BDD structure.
- [ ] Tests cover all 6 cases listed in `<context>` above.
- [ ] Each test installs a shim `claude` binary in `t.TempDir()` (or Ginkgo equivalent) and prepends it to `PATH`.
- [ ] `CHANGELOG.md` (repo root) has the new `fix(lib/claude):` bullet under `## Unreleased`.
- [ ] `cd lib && make precommit` exits 0.
- [ ] `git diff --name-only HEAD -- lib/ CHANGELOG.md` shows EXACTLY:
  - `lib/claude/claude-runner.go`
  - `lib/claude/claude-runner_test.go` (or whichever test file received the new tests)
  - `CHANGELOG.md`
- [ ] No new file outside `lib/claude/` (verify with `git status`).
- [ ] No change to `lib/claude/claude-event.go`, `lib/claude/types.go`, or any other lib/claude file (verify with `git diff --name-only HEAD lib/claude/`).
- [ ] `Run` method signature unchanged (verify with `grep 'func .* Run(ctx' lib/claude/claude-runner.go`).
- [ ] `ClaudeRunner` interface unchanged (verify with `grep -A2 'type ClaudeRunner interface' lib/claude/claude-runner.go`).

</acceptance_criteria>

<verification>

```bash
cd lib

# Signature changed
grep 'func scanOutput' claude/claude-runner.go
# expect: func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) (resultText string, recentLines []string) {

# stderr.String() no longer in error wrap
grep 'stderr.String' claude/claude-runner.go
# expect: no match (or only in dropped lines)

# Tests file present and uses Ginkgo
test -f claude/claude-runner_test.go && echo OK
grep -E 'Describe\(|It\(|Context\(' claude/claude-runner_test.go | head
# expect: Ginkgo BDD structure

# Helpers present
grep -E 'formatRecentLines|truncateString' claude/claude-runner.go
# expect: at least 2 matches

# Precommit clean
make precommit
# expect: exit 0

# CHANGELOG
grep -A1 '## Unreleased' ../CHANGELOG.md | head -5
# expect: the new fix(lib/claude) bullet

# Diff scope
git diff --name-only HEAD -- ../lib/ ../CHANGELOG.md
# expect exactly:
#   CHANGELOG.md
#   lib/claude/claude-runner.go
#   lib/claude/claude-runner_test.go
```

Expected end state:
- `scanOutput` returns recentLines
- `Run` includes recentLines in error wrap
- Tests prove all 6 cases work
- CHANGELOG has Unreleased entry ready for next release
- Precommit clean
- Zero collateral changes outside lib/claude/

</verification>

<do_nothing_option>
Leaving `claude-runner.go` as-is keeps the `claude CLI failed: : exit status 1` body section as the operator's only signal. Every Claude CLI failure on dev or prod — OAuth expiry, 401, rate limit, network blip, broken model name, missing MCP config, plugin install failure — produces the same uninformative message. The body-writing fix (lib/v0.61.0) is half-done: the body is present but its content is useless. Operators still race the pod's TTL cleanup window to grab `kubectl logs <pod>` for actual diagnosis.

This change is the smallest possible move that completes the body-writing fix's actual goal (operator-readable explanation in the task body). It's also forward-compatible: works regardless of what shape Claude's stream-json events take in any future CLI version. The cost is one private signature change to a package-private helper + new tests + one CHANGELOG line.
</do_nothing_option>
