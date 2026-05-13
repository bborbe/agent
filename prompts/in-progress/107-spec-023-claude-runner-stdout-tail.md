---
status: committing
spec: [023-surface-claude-cli-failure-reason-in-task-body]
summary: Modified claudeRunner in lib/claude to surface a bounded stdout tail (last 5 lines, 512 bytes/line) on CLI subprocess failures, replacing the empty double-colon rendering with real diagnostic output; added six new Ginkgo tests covering all error paths and the success path; updated CHANGELOG.md.
container: agent-107-spec-023-claude-runner-stdout-tail
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T00:00:00Z"
queued: "2026-05-13T06:54:23Z"
started: "2026-05-13T06:54:24Z"
branch: dark-factory/surface-claude-cli-failure-reason-in-task-body
---

<summary>
- The claude CLI runner currently builds its failure error message from `cmd.Stderr`, which is always empty in `--output-format stream-json` mode — producing the content-free `claude CLI failed: : exit status 1` rendering operators see today
- A ring buffer (max 5 lines, max 512 bytes/line) is added to `scanOutput` to capture every non-empty stdout line the CLI emits, shape-agnostically, in parallel with the existing result-event filter
- When `cmd.Wait()` returns non-zero, the error message now contains the captured tail joined with ` | ` — surfacing auth-failure events, rate-limit events, and other CLI diagnostics directly on the task page's `## Failure` section
- When the CLI exits non-zero having emitted no stdout at all (early kill, OOM), the error explicitly states "no stdout captured" instead of rendering the double-colon empty field
- On a successful run the tail is allocated then discarded — `ClaudeResult` and the success path are entirely unchanged
- The stderr buffer capture (`var stderr bytes.Buffer; cmd.Stderr = &stderr`) is removed; it was the only source of the empty middle field and is now superseded
- Five new Ginkgo tests cover: diagnostic lines on failure, zero-stdout failure, >5-line truncation, >512-byte line truncation, and successful run with no leakage — all using a shell-script shim in `t.TempDir()`, no real `claude` binary needed
- Change confined to `lib/claude/claude-runner.go`, new `lib/claude/claude-runner_test.go`, and `CHANGELOG.md`
</summary>

<objective>
Modify `claudeRunner` in `lib/claude` so that when the `claude` CLI subprocess exits non-zero, the returned error carries a bounded tail of the actual stdout lines the CLI emitted (last 5, each ≤512 bytes, joined with ` | `). This makes the `## Failure` body section on the task page operator-readable — showing the real auth-failure / rate-limit / API-error events from the CLI — without depending on pod logs that expire before the next investigation. The `ClaudeRunner` interface and `ClaudeResult` struct are frozen; all diagnostic detail rides on the error value.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — `bborbe/errors`, never `fmt.Errorf`, never bare `context.Background()` in pkg/
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, external test packages, suite files, coverage ≥80%
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**

- `lib/claude/claude-runner.go` — the only file modified (full read; it is 176 lines)
- `lib/claude/claude_suite_test.go` — test suite wiring (external package `claude_test`, Ginkgo v2, 60 s timeout)
- `lib/claude/task-runner_test.go` — example of the Ginkgo/Gomega style used in this package (`Describe` / `BeforeEach` / `JustBeforeEach` / `Context` / `It`)

**Inline reference — current `Run` method (claude-runner.go:37-66):**
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

    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    if err := cmd.Start(); err != nil {
        return nil, errors.Wrap(ctx, err, "start claude CLI")
    }

    resultText := scanOutput(ctx, stdoutPipe)

    if err := cmd.Wait(); err != nil {
        return nil, errors.Wrapf(ctx, err, "claude CLI failed: %s", stderr.String())
    }

    if resultText == "" {
        return nil, errors.New(ctx, "no result event found in claude CLI output")
    }

    return &ClaudeResult{Result: resultText}, nil
}
```

**Inline reference — current `scanOutput` signature (claude-runner.go:120):**
```go
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) string {
```

**Root cause of `: :` rendering:**
`stderr.String()` is always `""` in `--output-format stream-json` mode (CLI writes everything to stdout). The `errors.Wrapf` format `"claude CLI failed: %s"` with an empty string produces `"claude CLI failed: "`, and when the outer error message joins the cause (`exit status 1`) it renders as `"claude CLI failed: : exit status 1"` — the double colon.

**Import block in claude-runner.go that needs updating (add `"strings"`, remove `"bytes"` if unused elsewhere):**
```go
import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "os"
    "os/exec"

    "github.com/bborbe/errors"
    "github.com/golang/glog"
)
```
After the change, `"bytes"` is still needed for `bytes.NewBufferString(prompt)` in `buildCommand` (line 98) — do NOT remove it.
</context>

<requirements>

## 1. Add ring-buffer constants to `lib/claude/claude-runner.go`

Immediately after the import block, before the `//counterfeiter:generate` comment, add:

```go
const (
    tailMaxLines = 5
    tailMaxBytes = 512
    tailJoiner   = " | "
)
```

These constants are intentionally NOT exported and NOT on `ClaudeRunnerConfig`. They are properties of operator log readability, not per-deployment knobs.

## 2. Change `scanOutput` to also return the stdout tail

Change the function signature from returning `string` to returning `(string, []string)`:

```go
func scanOutput(ctx context.Context, reader interface{ Read([]byte) (int, error) }) (string, []string) {
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

        if len(line) > 0 {
            captured := line
            if len(captured) > tailMaxBytes {
                captured = captured[:tailMaxBytes]
            }
            tail = append(tail, string(captured))
            if len(tail) > tailMaxLines {
                tail = tail[len(tail)-tailMaxLines:]
            }
        }

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

Key design points:
- The ring buffer capture (`if len(line) > 0 { ... }`) is entirely BEFORE the JSON parsing block. It is shape-agnostic: every non-empty line is captured, regardless of event type.
- Truncation to `tailMaxBytes` happens before appending to `tail`, so the `tail` slice never holds a string longer than 512 bytes.
- Sliding window: `if len(tail) > tailMaxLines { tail = tail[len(tail)-tailMaxLines:] }` keeps only the most recent `tailMaxLines` entries.
- On `ctx.Done()`: return `"", nil` as before (tail is discarded on cancellation).

## 3. Update `Run` to use the tail in the error path

Replace the `Run` method body. The changes are:
1. Remove `var stderr bytes.Buffer` and `cmd.Stderr = &stderr` (no longer needed; stream-json writes nothing to stderr).
2. Change `resultText := scanOutput(...)` to capture the tail: `resultText, tail := scanOutput(...)`.
3. Replace the error line with a tail-based message.
4. Add `"strings"` to the import block.

The updated method (lines 37–66 of the current file):

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

    resultText, tail := scanOutput(ctx, stdoutPipe)

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

    return &ClaudeResult{Result: resultText}, nil
}
```

Add `"strings"` to the import block. Verify `"bytes"` is still present (still needed by `buildCommand` at line 98 for `bytes.NewBufferString(prompt)`).

## 4. Add new test file `lib/claude/claude-runner_test.go`

Create the file from scratch. Package is `claude_test` (external, consistent with the suite and other test files in this directory).

The test uses a shell-script shim placed in a temp dir, prepended to `PATH`, to drive `claude.NewClaudeRunner.Run` without a real `claude` binary. The shim receives all the `--print --output-format stream-json ...` arguments but ignores them — it just emits canned output and exits.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
)

var _ = Describe("claudeRunner stdout tail", func() {
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
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed())
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
	}

	Context("non-zero exit after emitting diagnostic stdout lines", func() {
		BeforeEach(func() {
			writeShim(`echo '{"type":"error","message":"auth-failure: 401 Invalid authentication credentials"}'
exit 1`)
		})

		It("error contains the diagnostic text the CLI emitted", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("auth-failure: 401 Invalid authentication credentials"))
		})

		It("error does not contain the double-colon empty rendering", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring(": :"))
		})
	})

	Context("non-zero exit with no stdout output", func() {
		BeforeEach(func() {
			writeShim("exit 1")
		})

		It("error contains 'no stdout captured'", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no stdout captured"))
		})

		It("error does not contain the double-colon empty rendering", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring(": :"))
		})
	})

	Context("non-zero exit after emitting more than 5 stdout lines", func() {
		BeforeEach(func() {
			// Emits 7 lines; only the most recent 5 (lines 3–7) should appear in the error.
			writeShim(`echo 'DROPPED-line-one'
echo 'DROPPED-line-two'
echo 'retained-line-3'
echo 'retained-line-4'
echo 'retained-line-5'
echo 'retained-line-6'
echo 'retained-line-7'
exit 1`)
		})

		It("drops the two oldest lines", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("DROPPED-line-one"))
			Expect(err.Error()).NotTo(ContainSubstring("DROPPED-line-two"))
		})

		It("retains the 5 most recent lines", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("retained-line-3"))
			Expect(err.Error()).To(ContainSubstring("retained-line-7"))
		})
	})

	Context("non-zero exit after emitting a line exceeding 512 bytes", func() {
		BeforeEach(func() {
			// Emits 600 'A' characters as a single line, then exits 1.
			writeShim("head -c 600 /dev/zero | tr '\\0' 'A'\necho\nexit 1")
		})

		It("truncates the captured line to 512 bytes", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).To(HaveOccurred())
			// 512 consecutive 'A's is the truncated content — it must appear in the error
			Expect(err.Error()).To(ContainSubstring(strings.Repeat("A", 512)))
			// 513 consecutive 'A's cannot appear — the line was truncated at 512
			Expect(err.Error()).NotTo(ContainSubstring(strings.Repeat("A", 513)))
		})
	})

	Context("successful CLI exit", func() {
		BeforeEach(func() {
			// Emits one diagnostic line (noise) and one result event, then exits 0.
			writeShim(`echo '{"type":"system","subtype":"init","cwd":"/tmp","session_id":"abc","tools":[]}'
echo '{"type":"result","result":"task-output-text"}'
exit 0`)
		})

		It("returns no error", func() {
			_, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns the result from the result event", func() {
			result, _ := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
			Expect(result).NotTo(BeNil())
			Expect(result.Result).To(Equal("task-output-text"))
		})
	})
})
```

**Notes for implementation:**
- `GinkgoT().TempDir()` is available in Ginkgo v2; it creates a test-scoped temp dir cleaned up automatically.
- `DeferCleanup` restores the original `PATH` after each test so the shim dir does not bleed into other tests.
- `claude.NewClaudeRunner(claude.ClaudeRunnerConfig{})` zero-value config is intentional: `WorkingDirectory`, `AllowedTools`, `Model`, and `Env` are all optional. The runner will call `allowlistEnv()` which reads the current process's `PATH`, which we have prepended with the shim dir.
- The shim ignores all arguments (`--print`, `--output-format stream-json`, etc.) — it just echoes its canned lines and exits.

## 5. Update `CHANGELOG.md` at repo root

Check whether `## Unreleased` section exists:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If it does NOT exist (the current top section is `## v0.61.0`), insert a new `## Unreleased` section immediately above `## v0.61.0`:

```markdown
## Unreleased

- fix(lib/claude): surface bounded stdout tail from failed `claude` CLI subprocess runs — ring buffer captures last 5 non-empty stdout lines (512 bytes/line max), joined with ` | `, so the `## Failure` body section on the task page contains the actual CLI diagnostic output (auth failures, rate-limit events, API errors) instead of the empty `claude CLI failed: : exit status 1` rendering caused by stream-json's always-empty stderr

```

## 6. Run iterative tests

```bash
cd lib && make test
```

Fix any compile errors before proceeding. Common issues:
- Missing `"strings"` import in `claude-runner.go`
- `scanOutput` callers expecting a single return value (only `Run` calls `scanOutput`; update that call site)

## 7. Check test coverage for `lib/claude` package

```bash
cd lib && go test -coverprofile=/tmp/cover.out ./claude/... && go tool cover -func=/tmp/cover.out | grep -E "claude-runner|total"
```

Coverage for the changed code paths must be ≥80%. The five new tests cover:
- `Run` error path with non-empty tail ✓
- `Run` error path with empty tail ✓
- `scanOutput` ring buffer eviction (>5 lines) ✓
- `scanOutput` line truncation (>512 bytes) ✓
- `Run` success path with tail discarded ✓

## 8. Run final precommit

```bash
cd lib && make precommit
```

Must exit 0. If lint fails, run only the failing target (e.g., `make lint`) and fix before retrying `make precommit`.

</requirements>

<constraints>
- Change confined to `lib/claude/claude-runner.go`, new `lib/claude/claude-runner_test.go`, and `CHANGELOG.md`. No other file in `lib/` or any other module is touched.
- `ClaudeRunner` interface (`Run(ctx, prompt) (*ClaudeResult, error)`) is frozen. No new method, no new return value on `Run`, no new field on `ClaudeResult`.
- `ClaudeRunnerConfig` is NOT modified. The ring-buffer parameters (`tailMaxLines = 5`, `tailMaxBytes = 512`, `tailJoiner = " | "`) are package-level constants, not config fields.
- The ring buffer is shape-agnostic: `scanOutput` must NOT branch on Claude event-schema fields (type, subtype, is_error) when deciding what to retain. The `if len(line) > 0 { ... }` capture block is unconditional, placed BEFORE the JSON-parse block.
- `"bytes"` import must NOT be removed — it is still used by `buildCommand` for `bytes.NewBufferString(prompt)`.
- Error wrapping uses `github.com/bborbe/errors` — never `fmt.Errorf`.
- Tests must not require a real `claude` binary. Shell-script shim in `GinkgoT().TempDir()` is the only sanctioned approach.
- Test package is `claude_test` (external), consistent with the existing test files in this directory.
- Existing tests (`task-runner_test.go`, `claude-plugin-installer_test.go`, `result-deliverer_test.go`, etc.) must still pass unchanged.
- Do NOT commit — dark-factory handles git.
- `cd lib && make precommit` must exit 0.
</constraints>

<verification>

Verify constants are present:
```bash
grep -n "tailMaxLines\|tailMaxBytes\|tailJoiner" lib/claude/claude-runner.go
```
Expected: three lines — the `const` block.

Verify `scanOutput` returns two values:
```bash
grep -n "func scanOutput" lib/claude/claude-runner.go
```
Expected: `func scanOutput(...) (string, []string)`.

Verify `stderr` buffer is removed:
```bash
grep -n "stderr" lib/claude/claude-runner.go
```
Expected: zero matches (neither `var stderr bytes.Buffer` nor `stderr.String()` remain).

Verify `strings.Join` is used in the error path:
```bash
grep -n "strings.Join\|no stdout captured" lib/claude/claude-runner.go
```
Expected: both present in the `Run` method.

Verify test file exists with shim pattern:
```bash
grep -n "writeShim\|GinkgoT().TempDir\|DeferCleanup" lib/claude/claude-runner_test.go
```
Expected: multiple matches.

Verify all five test cases are present:
```bash
grep -n "no stdout captured\|DROPPED-line\|Repeat.*512\|task-output-text\|auth-failure" lib/claude/claude-runner_test.go
```
Expected: five distinct matches covering each scenario.

Verify CHANGELOG updated:
```bash
grep -n "stdout tail\|ring buffer\|exit status 1" CHANGELOG.md | head -5
```
Expected: at least one match.

Run tests:
```bash
cd lib && make test
```
Expected: exit 0, all specs pass including the new `claudeRunner stdout tail` suite.

Run coverage:
```bash
cd lib && go test -coverprofile=/tmp/cover.out ./claude/... && go tool cover -func=/tmp/cover.out | grep "total:"
```
Expected: ≥80% total coverage for the `claude` package.

Run precommit:
```bash
cd lib && make precommit
```
Expected: exit 0.

</verification>
