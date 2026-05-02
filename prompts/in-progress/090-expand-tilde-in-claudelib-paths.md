---
status: committing
summary: Added Resolve() method to ClaudeConfigDir and AgentDir for tilde expansion, created expandTilde helper, updated claude-runner.go to call Resolve() at subprocess boundaries, added unit tests, and updated CHANGELOG.md.
container: agent-090-expand-tilde-in-claudelib-paths
dark-factory-version: dev
created: "2026-05-02T15:50:00Z"
queued: "2026-05-02T15:48:05Z"
started: "2026-05-02T15:48:06Z"
---

<summary>
- Add a `Resolve()` method to `claudelib.ClaudeConfigDir` and `claudelib.AgentDir` that expands a leading `~/` to the user's home directory
- Update `claude-runner.go` to call `Resolve()` (instead of `.String()`) when emitting the path to the subprocess environment and when setting the working directory
- Empty input is preserved (returns empty string — caller decides whether empty means "don't set")
- Absolute paths and paths without a `~/` prefix are returned unchanged
- This lets consumers declare arg defaults like `default:"~/.claude"` and have the path correctly expand at the trust boundary, regardless of how the OS or downstream subprocess handles tildes
- Backwards-compatible: `.String()` continues to return the raw value; only callers that opt into `.Resolve()` get the expansion. `claude-runner.go` is the one consumer updated in this prompt
</summary>

<objective>
Centralize tilde-prefix expansion for path types in `agent/lib/claude` so consumers can declare `default:"~/.claude"` (or pass `~/.claude` via env) and have the path correctly resolve to `$HOME/.claude` when the agent uses it.
</objective>

<context>
Read CLAUDE.md for project conventions.
Read `go-error-wrapping-guide.md` from coding plugin (`~/.claude/plugins/marketplaces/coding/docs/`) — `errors.Wrapf`, never `fmt.Errorf`.
Read `go-testing-guide.md` from coding plugin — Ginkgo v2 + Gomega conventions used in this repo.
Read `go-architecture-patterns.md` from coding plugin — type wrappers + method placement conventions.

Files to read before changing anything:
- `lib/claude/claude-config-dir.go` — current `ClaudeConfigDir` type (`type ClaudeConfigDir string`, `String()` method)
- `lib/claude/agent-dir.go` — current `AgentDir` type (same shape)
- `lib/claude/claude-runner.go` — `claude-runner.go:97` emits `"CLAUDE_CONFIG_DIR="+r.config.ClaudeConfigDir.String()` — the call site that needs to use `Resolve()`
- `lib/claude/claude-runner-config.go` — `ClaudeRunnerConfig` struct that holds these types

Key facts (verified):
- Both `ClaudeConfigDir` and `AgentDir` are simple `type X string`
- The runner currently calls `.String()` on the path values directly (no normalization)
- Empty values are guarded today via `if r.config.ClaudeConfigDir != ""` — preserve this contract
- **Consumer audit (verified)**: the only ACTUAL consumer of these path values is `claude-runner.go` (which emits the env var and sets `cmd.Dir`). Every other site (`agent/claude/main.go`, `agent/claude/cmd/run-task/main.go`, `agent/claude/pkg/factory/factory.go`, `lib/claude/agent-step.go` comment) only PROPAGATES the value through to `ClaudeRunnerConfig` — no filesystem ops, no shell interpolation. Updating `claude-runner.go` therefore covers every effective consumer.
</context>

<requirements>

**Execute steps in this order. Run `make precommit` only in the final step.**

1. **Add `Resolve(ctx)` to `lib/claude/claude-config-dir.go`** — context is required (every existing helper in `lib/claude/` threads `ctx` through `errors.Wrapf`; mirror that style):

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package claude

   import "context"

   // ClaudeConfigDir is the path to the Claude Code configuration directory (~/.claude).
   type ClaudeConfigDir string

   // String returns the raw path string as configured.
   // Use Resolve when the path will cross into a subprocess or filesystem call —
   // String preserves the literal "~/" prefix that environment variables and
   // child processes generally do not expand.
   func (c ClaudeConfigDir) String() string { return string(c) }

   // Resolve expands a leading "~/" to the user's home directory. Empty input
   // returns empty string. Absolute paths and paths without a tilde prefix are
   // returned unchanged. Use at the trust boundary — when emitting the path
   // into a subprocess env var, opening a file, or constructing a filesystem
   // operation — so configuration like CLAUDE_CONFIG_DIR=~/.claude works on
   // every consumer regardless of whether the consumer expands tildes itself.
   func (c ClaudeConfigDir) Resolve(ctx context.Context) (string, error) {
       return expandTilde(ctx, string(c))
   }
   ```

   Note: `expandTilde` is a shared helper added in step 3 (file `lib/claude/expand-tilde.go` — kebab-case to match this package's filename convention).

2. **Add `Resolve(ctx)` to `lib/claude/agent-dir.go`** with the same shape (mandate ctx threading):

   ```go
   import "context"

   // AgentDir is the working directory for the agent process.
   type AgentDir string

   // String returns the raw path string as configured.
   func (d AgentDir) String() string { return string(d) }

   // Resolve expands a leading "~/" to the user's home directory.
   // See ClaudeConfigDir.Resolve for full semantics.
   func (d AgentDir) Resolve(ctx context.Context) (string, error) {
       return expandTilde(ctx, string(d))
   }
   ```

   (Keep the existing file content — only add the `Resolve(ctx)` method and the `context` import.)

3. **Create `lib/claude/expand-tilde.go`** (kebab-case, matching `claude-config-dir.go` / `agent-dir.go` convention) — the shared helper, with `ctx` threaded through (mirrors every existing `lib/claude/` helper's error-wrapping style):

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package claude

   import (
       "context"
       "os"
       "path/filepath"
       "strings"

       "github.com/bborbe/errors"
   )

   // expandTilde returns the argument with a leading "~/" replaced by the
   // current user's home directory (via os.UserHomeDir). Empty input returns
   // empty. Inputs that do not begin with "~/" are returned unchanged.
   //
   // The lone "~" (without trailing slash) is also expanded — same as shell
   // semantics. Any other "~"-prefixed form (e.g. "~user/...") is NOT expanded
   // and is returned unchanged; the caller is expected to use the literal.
   func expandTilde(ctx context.Context, path string) (string, error) {
       if path == "" {
           return "", nil
       }
       if path != "~" && !strings.HasPrefix(path, "~/") {
           return path, nil
       }
       home, err := os.UserHomeDir()
       if err != nil {
           return "", errors.Wrapf(ctx, err, "resolve user home directory for path %q", path)
       }
       if path == "~" {
           return home, nil
       }
       return filepath.Join(home, path[2:]), nil
   }
   ```

4. **Update `lib/claude/claude-runner.go`** at the existing call sites — pass `ctx` (already in scope in the runner's methods):

   - Find: `"CLAUDE_CONFIG_DIR="+r.config.ClaudeConfigDir.String()`
   - Replace with `Resolve(ctx)`-based form:

     ```go
     cfgDir, err := r.config.ClaudeConfigDir.Resolve(ctx)
     if err != nil {
         return errors.Wrapf(ctx, err, "resolve ClaudeConfigDir")
     }
     // Use cfgDir in the env entry instead of the previous .String() value.
     ```

   - Apply the same treatment at the `WorkingDirectory` (`AgentDir`) call site (`cmd.Dir = r.config.WorkingDirectory.String()` at ~line 88). Read the file to find every consumer; both ClaudeConfigDir and AgentDir uses must switch to `.Resolve(ctx)`.

   - **Out of scope (no change needed)**: per the consumer audit in `<context>`, the only actual consumer is `claude-runner.go`. Sites in `agent/claude/main.go`, `cmd/run-task/main.go`, `agent/claude/pkg/factory/factory.go`, and `lib/claude/agent-step.go` only PROPAGATE the value into `ClaudeRunnerConfig` — they perform no filesystem ops or shell interpolation. Updating `claude-runner.go` therefore covers every effective consumer.

5. **Add unit tests** in `lib/claude/expand-tilde_test.go` (use Ginkgo per project convention, external `_test` package):

   - Empty input → empty string, no error
   - `~/.claude` → `<home>/.claude`
   - `~` → `<home>`
   - `/abs/path` → unchanged
   - `relative/path` → unchanged
   - `~user/foo` (other-user form) → unchanged (NOT expanded; documented as not supported)

   Use `t.Setenv("HOME", "/test-home")` (auto-restores at test end — preferred over manual `os.Setenv` + `BeforeEach`/`AfterEach` plumbing). On macOS and Linux, `os.UserHomeDir()` reads `HOME`. Tests must pass `context.Background()` to `expandTilde`.

6. **Add unit tests** in `lib/claude/claude-config-dir_test.go` and `lib/claude/agent-dir_test.go` (or extend existing tests if files exist):

   - `String()` returns the raw value (no expansion)
   - `Resolve()` expands `~/` correctly
   - `Resolve()` on empty value → empty, no error

7. **Update CHANGELOG.md** under `## Unreleased` (or create the section if absent):

   ```markdown
   - feat(lib/claude): add `Resolve()` method to `ClaudeConfigDir` and `AgentDir` that expands a leading `~/` to the user's home directory. `claude-runner.go` now calls `Resolve()` at the env-var emission and working-directory boundaries, so consumers can declare `default:"~/.claude"` (or pass `~/.claude` via env) and have the path correctly expand. Backwards-compatible — existing `.String()` callers see no change.
   ```

8. **Run `make precommit`**:

   ```bash
   cd lib && make precommit
   ```

</requirements>

<constraints>
- Only edit files under `lib/claude/` and `CHANGELOG.md`
- Do NOT commit — dark-factory handles git
- Do NOT change `ClaudeConfigDir`/`AgentDir` from `type X string` to a struct — the shape stays
- `String()` semantics MUST NOT change (raw value, no expansion) — backwards-compat for existing callers
- Use `github.com/bborbe/errors` (`errors.Wrapf`, `errors.Errorf`); never `fmt.Errorf`
- Mirror the error-wrapping style of existing helpers in `lib/claude/` (with or without `context.Context`)
- Tests follow Ginkgo v2 + Gomega + external `_test` package
- `make precommit` runs from `lib/`, not from repo root
- Existing tests must keep passing
</constraints>

<verification>
cd lib && make precommit

# Confirm Resolve methods exist:
grep -nE "func.*ClaudeConfigDir.*Resolve|func.*AgentDir.*Resolve" lib/claude/*.go

# Confirm claude-runner uses Resolve:
grep -nE "ClaudeConfigDir\.Resolve|WorkingDirectory\.Resolve|AgentDir.*Resolve" lib/claude/claude-runner.go

# Confirm the helper file exists:
ls lib/claude/expand_tilde.go
</verification>
