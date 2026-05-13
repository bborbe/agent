---
status: completed
summary: Replaced allowlistEnv with buildSubprocessEnv that always passes CLAUDE_CONFIG_DIR defaulting to ~/.claude, added 5 boundary-case tests, and updated CHANGELOG.md with operator re-login note.
container: agent-115-fix-claude-runner-default-config-dir
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T21:30:00Z"
queued: "2026-05-13T21:14:48Z"
started: "2026-05-13T21:14:50Z"
completed: "2026-05-13T21:18:56Z"
---

<summary>
- The Claude CLI subprocess loses its OAuth refresh token between Job restarts because `.claude.json` writes go to the agent's ephemeral `$HOME` instead of the persistent `~/.claude/` PVC mount
- Root cause: `lib/claude/claude-runner.go` only passes `CLAUDE_CONFIG_DIR` to the subprocess when the agent explicitly sets it; the default is to leave it unset, which makes Claude write to `$HOME/.claude.json`
- Fix: always pass `CLAUDE_CONFIG_DIR` via a new map-based `buildSubprocessEnv` method with explicit three-layer precedence — consumer `r.config.Env` > explicit `r.config.ClaudeConfigDir` > parent process env > default `~/.claude`
- The map-based construction replaces the previous three-place env assembly (allowlistEnv call + conditional CLAUDE_CONFIG_DIR append + consumer Env loop) with a single linear pipeline; duplicate-key entries in `cmd.Env` are eliminated and precedence is readable top-to-bottom
- `allowlistEnv()` is deleted; its security rationale (only pass-through specific safe parent vars, never arbitrary parent env) is preserved as the doc comment on `buildSubprocessEnv` Layer 1
- New tests cover five boundary cases: default to expanded `~/.claude`, explicit absolute path passed through, explicit tilde-prefixed path expanded, parent process env used when config empty, and consumer `r.config.Env` override beats both config and env
- Behavioral regression for any deployed agent that does not yet have `$HOME/.claude/.claude.json` (its PVC has `.claude.json` at the old ephemeral path) — they will fail with "config file not found" on the next Job start until a one-time `claude login` populates the file at the new path. The CHANGELOG calls this out so downstream consumers know to re-login each PVC after bumping `lib/claude`
</summary>

<objective>
Make `lib/claude/claude-runner.go` always pass `CLAUDE_CONFIG_DIR` to the Claude subprocess, defaulting to `~/.claude` when the consumer has not explicitly configured a value. After this change, every consumer of `claude.NewClaudeRunner` gets persistent `.claude.json` storage by default — provided a PVC is mounted at `$HOME/.claude`.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `bborbe/errors`, never `fmt.Errorf`
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega, external test packages, **errcheck rule** for bare error-returning calls in `It` blocks (wrap with `Expect(...).To(Succeed())` / `.To(HaveOccurred())`)
- `~/.claude/plugins/marketplaces/dark-factory/docs/prompt-writing.md` — "Test the boundaries the new code crosses" — the boundary here is the subprocess env contract; the existing test pattern (shell-script shim that echoes env vars) exercises it directly

**Key files to read in full before editing:**

- `lib/claude/claude-runner.go` — the only file modified by this prompt. Read lines 78-127 (`buildCommand`) carefully; lines 110-120 are the block being rewritten
- `lib/claude/claude-config-dir.go` — `ClaudeConfigDir` type, `Resolve` semantics (tilde expansion via `expandTilde`)
- `lib/claude/claude-runner_test.go` — existing test patterns; the `writeShim` helper at the top of the suite is what new tests should reuse
- `lib/claude/claude-runner-config.go` (or wherever `ClaudeRunnerConfig` is defined) — the struct holding `ClaudeConfigDir`

**Inline reference — the block being changed (`claude-runner.go:111-120`):**

```go
cmd.Env = allowlistEnv()
if r.config.ClaudeConfigDir != "" {
    cfgDir, err := r.config.ClaudeConfigDir.Resolve(ctx)
    if err != nil {
        return nil, errors.Wrap(ctx, err, "resolve ClaudeConfigDir")
    }
    cmd.Env = append(
        cmd.Env,
        "CLAUDE_CONFIG_DIR="+cfgDir,
    )
}
```

**Inline reference — `ClaudeConfigDir` type (`claude-config-dir.go`):**

```go
type ClaudeConfigDir string

func (c ClaudeConfigDir) Resolve(ctx context.Context) (string, error) {
    return expandTilde(ctx, string(c))
}
```

`Resolve` expands a leading `~/` to `$HOME` (via `os.UserHomeDir` or equivalent). Empty input returns empty string. Absolute paths and paths without a tilde prefix are returned unchanged.

**Inline reference — `expandTilde` behavior** (verify the exact symbol before relying on it):

```bash
grep -n "func expandTilde" lib/claude/
```

It should be in the same package, reading `$HOME` from the parent process env.

**Inline reference — existing test pattern (`claude-runner_test.go:18-40`):**

```go
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

This helper writes a fake `claude` binary that the runner will spawn. Reuse this pattern. The shim can echo subprocess env vars as the `result` field of a JSON line:

```go
writeShim(`echo "{\"type\":\"result\",\"result\":\"CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR\"}"
exit 0`)
```

`NewClaudeRunner(config).Run(ctx, "test")` then returns `&ClaudeResult{Result: "CLAUDE_CONFIG_DIR=/some/path"}` which the test inspects.
</context>

<requirements>

## 1. Refactor subprocess env construction in `lib/claude/claude-runner.go`

The current code builds `cmd.Env` in three places (allowlist call, conditional CLAUDE_CONFIG_DIR append, consumer Env loop). Replace those three places with a single `buildSubprocessEnv` method that uses a `map[string]string` to make precedence explicit and eliminate duplicate-key ambiguity.

**a. Add a new private method `(r *claudeRunner) buildSubprocessEnv(ctx)`** below `buildCommand`:

```go
// buildSubprocessEnv constructs the env var slice for the Claude CLI subprocess.
// Precedence (later layers override earlier):
//   1. Allowlist: pass-through of safe parent-process vars (HOME, PATH, ...).
//   2. CLAUDE_CONFIG_DIR: explicit config > parent process env > default "~/.claude".
//   3. Consumer-provided r.config.Env: arbitrary overrides — highest precedence.
//
// Building via map[string]string makes precedence linear by assignment order and
// prevents duplicate-key entries in the resulting []string.
func (r *claudeRunner) buildSubprocessEnv(ctx context.Context) ([]string, error) {
    env := map[string]string{}

    // Layer 1: allowlist pass-through.
    for _, k := range []string{"HOME", "PATH", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL"} {
        if v, ok := os.LookupEnv(k); ok {
            env[k] = v
        }
    }

    // Layer 2: CLAUDE_CONFIG_DIR with precedence config > env > default.
    cfgDir := r.config.ClaudeConfigDir
    if cfgDir == "" {
        if envVal := os.Getenv("CLAUDE_CONFIG_DIR"); envVal != "" {
            cfgDir = ClaudeConfigDir(envVal)
        }
    }
    if cfgDir == "" {
        cfgDir = "~/.claude"
    }
    resolved, err := cfgDir.Resolve(ctx)
    if err != nil {
        return nil, errors.Wrap(ctx, err, "resolve ClaudeConfigDir")
    }
    env["CLAUDE_CONFIG_DIR"] = resolved

    // Layer 3: consumer-provided env overrides everything above.
    for k, v := range r.config.Env {
        env[k] = v
    }

    // Convert to []string for exec.Cmd.
    result := make([]string, 0, len(env))
    for k, v := range env {
        result = append(result, k+"="+v)
    }
    return result, nil
}
```

**b. Replace the three-block env construction in `buildCommand` (lines 110-123)** with a single call:

```go
// OLD (lines 110-123):
// cmd.Env = allowlistEnv()
// if r.config.ClaudeConfigDir != "" {
//     cfgDir, err := r.config.ClaudeConfigDir.Resolve(ctx)
//     if err != nil { ... }
//     cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+cfgDir)
// }
// for k, v := range r.config.Env {
//     cmd.Env = append(cmd.Env, k+"="+v)
// }

// NEW:
env, err := r.buildSubprocessEnv(ctx)
if err != nil {
    return nil, errors.Wrap(ctx, err, "build subprocess env")
}
cmd.Env = env
```

**c. Delete the now-unused `allowlistEnv()` function** (lines 187-207) and its doc comment. It is unexported and has no callers outside `buildCommand`. The doc-comment intent (security rationale for not passing arbitrary parent env) MUST be preserved — relocate it as the doc comment on the new `buildSubprocessEnv` method's "Layer 1" section, or keep it as a leading paragraph on `buildSubprocessEnv` describing the trust boundary.

**d. Update the doc comment in `claude-runner-config.go:18`.**

Exact find-and-replace:

```
OLD: AFTER the allowlist filter (see allowlistEnv in claude-runner.go). This is
NEW: AFTER the allowlist + CLAUDE_CONFIG_DIR layers (see buildSubprocessEnv in claude-runner.go). This is
```

This is the canonical reference the rest of the codebase has to `allowlistEnv` — there are no other consumers of that symbol name to update.

**e. Verify the `os` import is still needed** after the refactor (yes — both `os.LookupEnv` and `os.Getenv` are used). No new imports.

## 2. Add new test cases to `lib/claude/claude-runner_test.go`

Add a new `Describe` block at the end of the file (after the existing `Describe("claudeRunner stdout tail", ...)`):

```go
var _ = Describe("claudeRunner CLAUDE_CONFIG_DIR env propagation", func() {
    var ctx context.Context

    BeforeEach(func() {
        ctx = context.Background()
    })

    // writeEnvShim writes a fake "claude" binary that echoes the named env var
    // as the `result` field of a stream-json result event, then exits 0.
    writeEnvShim := func(envVar string) {
        shimDir := GinkgoT().TempDir()
        shimPath := filepath.Join(shimDir, "claude")
        script := fmt.Sprintf(`#!/bin/sh
echo "{\"type\":\"result\",\"result\":\"%s=$%s\"}"
exit 0
`, envVar, envVar)
        Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
        originalPath := os.Getenv("PATH")
        DeferCleanup(func() {
            Expect(os.Setenv("PATH", originalPath)).To(Succeed())
        })
        Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
    }

    Context("when config.ClaudeConfigDir is empty (default)", func() {
        BeforeEach(func() {
            writeEnvShim("CLAUDE_CONFIG_DIR")
        })

        It("passes CLAUDE_CONFIG_DIR=<expanded ~/.claude> to the subprocess", func() {
            home, err := os.UserHomeDir()
            Expect(err).NotTo(HaveOccurred())

            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude")))
        })
    })

    Context("when config.ClaudeConfigDir is an explicit absolute path", func() {
        BeforeEach(func() {
            writeEnvShim("CLAUDE_CONFIG_DIR")
        })

        It("passes that path through unchanged", func() {
            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
                ClaudeConfigDir: claude.ClaudeConfigDir("/custom/claude/path"),
            }).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/custom/claude/path"))
        })
    })

    Context("when config.ClaudeConfigDir is a tilde-prefixed path", func() {
        BeforeEach(func() {
            writeEnvShim("CLAUDE_CONFIG_DIR")
        })

        It("expands the tilde to the user's home directory", func() {
            home, err := os.UserHomeDir()
            Expect(err).NotTo(HaveOccurred())

            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
                ClaudeConfigDir: claude.ClaudeConfigDir("~/custom-claude"),
            }).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=" + filepath.Join(home, "custom-claude")))
        })
    })

    Context("when CLAUDE_CONFIG_DIR is set in the parent process env", func() {
        BeforeEach(func() {
            writeEnvShim("CLAUDE_CONFIG_DIR")
            originalEnv, hadOriginal := os.LookupEnv("CLAUDE_CONFIG_DIR")
            DeferCleanup(func() {
                if hadOriginal {
                    Expect(os.Setenv("CLAUDE_CONFIG_DIR", originalEnv)).To(Succeed())
                } else {
                    Expect(os.Unsetenv("CLAUDE_CONFIG_DIR")).To(Succeed())
                }
            })
            Expect(os.Setenv("CLAUDE_CONFIG_DIR", "/env-set/claude")).To(Succeed())
        })

        It("uses the parent env value when explicit config is empty", func() {
            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{}).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/env-set/claude"))
        })

        It("explicit config takes precedence over parent env", func() {
            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
                ClaudeConfigDir: claude.ClaudeConfigDir("/explicit/claude"),
            }).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/explicit/claude"))
        })
    })

    Context("when r.config.Env explicitly sets CLAUDE_CONFIG_DIR", func() {
        BeforeEach(func() {
            writeEnvShim("CLAUDE_CONFIG_DIR")
        })

        It("the consumer-provided value wins over everything (highest precedence)", func() {
            result, err := claude.NewClaudeRunner(claude.ClaudeRunnerConfig{
                ClaudeConfigDir: claude.ClaudeConfigDir("/explicit/claude"),
                Env: map[string]string{
                    "CLAUDE_CONFIG_DIR": "/consumer-env/claude",
                },
            }).Run(ctx, "test")
            Expect(err).NotTo(HaveOccurred())
            Expect(result.Result).To(Equal("CLAUDE_CONFIG_DIR=/consumer-env/claude"))
        })
    })
})
```

Add `"fmt"` to the test file's import block if not already present.

## 3. Update `CHANGELOG.md` at repo root

The current `CHANGELOG.md` has NO `## Unreleased` section — it goes from `# Changelog` (line 1) directly to `## v0.62.2` (line 3). **Create a new `## Unreleased` heading above `## v0.62.2`** so autoRelease renames it to the next version tag at commit time. Exact insertion shape (keep `## v0.62.2` and everything after it unchanged):

```markdown
# Changelog

## Unreleased

- fix(lib/claude): `CLAUDE_CONFIG_DIR` is now always passed to the Claude subprocess, defaulting to `~/.claude` when the consumer has not configured a value. Previously the env var was only set when explicitly configured, which made Claude write `.claude.json` to the agent's ephemeral `$HOME` rather than the persistent `~/.claude/` PVC mount — refresh tokens were silently lost across Job restarts, eventually causing 401 errors. **Behavioral regression**: agents deployed against existing PVCs (which still have `.claude.json` at the old ephemeral path) will fail with "config file not found" on the next Job start. Re-run `claude login` per PVC via [[Agent - Refresh Claude OAuth Login]] after bumping `lib/claude`. A failure to resolve `$HOME` in the pod (rare) now manifests as a hard `Run` error rather than silent ephemeral fallback.

## v0.62.2
```

Do NOT modify the existing `## v0.62.2` bullet or any prior version section. The new bullet lives ONLY in the new `## Unreleased` block.

## 4. Run tests

```bash
cd lib/claude && go test ./... -count=1
```

Verify the new specs pass and existing specs are still green.

## 5. Run final precommit

```bash
make precommit
```

Must exit 0 at the repo root. Trivy, gosec, golangci-lint, generate-drift, license-check, go test all green.

</requirements>

<constraints>
- Change is confined to `lib/claude/claude-runner.go`, `lib/claude/claude-runner_test.go`, and root `CHANGELOG.md`. No other files modified. No new files created.
- The `ClaudeRunner` interface (`Run(ctx, prompt) (*ClaudeResult, error)`) is frozen. No signature change.
- The `ClaudeRunnerConfig` struct shape is frozen. No new fields. Default value remains the zero value (empty `ClaudeConfigDir`); the new logic interprets empty as `"~/.claude"` at use-time, NOT at struct-construction time.
- The default value `~/.claude` is the literal string `~/.claude` (with the tilde) — NOT a pre-expanded path. `Resolve` does the expansion at the trust boundary, consistent with the existing pattern.
- Existing tests in `claude-runner_test.go` must remain green. They construct `ClaudeRunnerConfig{}` (empty) and expect specific error messages — none of those assertions inspect the env, so they are unaffected by this change.
- `r.config.Env` loop order is preserved: CLAUDE_CONFIG_DIR is appended FIRST, then consumer env. A consumer that explicitly sets CLAUDE_CONFIG_DIR in `r.config.Env` would override the lib's default — leave that escape hatch intact.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`. Never bare `context.Background()` in pkg/ code (tests are exempt for `BeforeEach`).
- Tests use Ginkgo v2 + Gomega per project convention. External test package `claude_test`.
- Per `go-testing-guide.md` "Critical Rules": never call an error-returning function bare in an `It` block. Wrap with `Expect(...).To(Succeed())` or capture the error and assert separately. The new tests above already follow this.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required, with explicit operator-facing note about re-login.
- Project tag policy: paired `vX.Y.Z` + `lib/vX.Y.Z` tags will be cut by `autoRelease` at the next opportunity. Both must be at the same commit.
- Do NOT bump downstream consumers' `go.mod` files (agent, maintainer, trading) in this prompt. Those are separate sibling work after the lib release.
- Do NOT commit — dark-factory handles git.
- `make precommit` must exit 0.
</constraints>

<verification>

Verify `allowlistEnv` was deleted:
```bash
grep -n 'allowlistEnv' lib/claude/claude-runner.go
```
Expected: no matches.

Verify the new `buildSubprocessEnv` method exists:
```bash
grep -n 'func (r \*claudeRunner) buildSubprocessEnv' lib/claude/claude-runner.go
```
Expected: one match.

Verify `buildCommand` now uses `buildSubprocessEnv`:
```bash
grep -n 'r.buildSubprocessEnv(ctx)' lib/claude/claude-runner.go
```
Expected: one match (inside `buildCommand`).

Verify the default literal is present in the new method:
```bash
grep -n '"~/.claude"' lib/claude/claude-runner.go
```
Expected: exactly one match (Layer 2 default fallback).

Verify the doc-comment reference in `claude-runner-config.go` was updated:
```bash
grep -n 'allowlistEnv\|buildSubprocessEnv' lib/claude/claude-runner-config.go
```
Expected: at least one match for `buildSubprocessEnv`, no matches for `allowlistEnv`.

Verify the new test `Describe` block exists:
```bash
grep -n 'claudeRunner CLAUDE_CONFIG_DIR env propagation' lib/claude/claude-runner_test.go
```
Expected: one match.

Verify all five boundary cases are present:
```bash
grep -cE 'Context\(' lib/claude/claude-runner_test.go
```
Expected: at least 5 new Contexts in the env-propagation block (empty / explicit absolute / explicit tilde / parent env / consumer override).

Verify CHANGELOG entry is present and warns about re-login:
```bash
grep -n "CLAUDE_CONFIG_DIR" CHANGELOG.md
grep -i "re-login\|Refresh Claude OAuth Login" CHANGELOG.md
```
Expected: at least one match each, both under `## Unreleased`.

Run tests:
```bash
cd lib/claude && go test ./... -count=1
```
Expected: exit 0, all new specs pass.

Run precommit:
```bash
make precommit
```
Expected: exit 0.

</verification>
