---
status: completed
summary: Added envparse.RedactForLog / IsSensitiveKey, wired both subprocess-env log sites (lib/claude, lib/pi) through it, added Ginkgo tests (100% coverage on envparse), updated CHANGELOG
container: agent-redact-env-exec-199-redact-env-logging
dark-factory-version: v0.174.4
created: "2026-06-03T06:00:00Z"
queued: "2026-06-03T06:21:18Z"
started: "2026-06-03T08:56:59Z"
completed: "2026-06-03T09:02:37Z"
---

<summary>
- Stops leaking `ANTHROPIC_AUTH_TOKEN`, `GH_TOKEN`, and similar secrets through subprocess-env log lines that currently end up in Loki / `kubectl logs`.
- Adds a small shared helper in the existing `lib/envparse` package that redacts only the values of sensitive keys — the key name itself stays visible so operators can still see which vars are passed.
- Sensitive matching is a case-insensitive substring check against a fixed marker list (`TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`, `CREDENTIAL`, `API_KEY`, `PRIVATE_KEY`, `ACCESS_KEY`).
- Redacted values become the literal three-character string `***` — never a length hint, never `<redacted>`.
- Routes the two known offending log sites (one in `lib/claude`, one in `lib/pi`) through the new helper.
- Adds Ginkgo tests in the same package that pin the redaction contract, including case-insensitivity, empty-value handling, non-`KEY=VALUE` passthrough, and non-mutation of the input slice.
- After this prompt, `glog.V(2)`/`glog.V(4)` env dumps from the pr-reviewer-agent and pi-runner paths show keys plus `***`, not secrets.
</summary>

<objective>
Add `envparse.RedactForLog` and `envparse.IsSensitiveKey`, route both subprocess-env log sites (`lib/claude/claude-runner.go` and `lib/pi/pi-runner.go`) through `RedactForLog`, and pin behavior with Ginkgo tests. After this prompt no secret value reaches stdout via these two glog lines.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Files to read before changing:

- `lib/envparse/envparse.go` — existing helper package; the new functions live alongside `KeyValuePairs`. Package doc comment is already `Package envparse provides simple parsers for KEY=VALUE-style CLI inputs.` — broaden it minimally if needed, or just leave it.
- `lib/envparse/envparse_test.go` — exemplar for the Ginkgo test style (external test package `envparse_test`, `Describe` block per function, `It` cases, `Expect(...).To(Equal(...))`). Match this style exactly.
- `lib/envparse/envparse_suite_test.go` — already wires the suite via `RunSpecs`; no changes needed there. The new `redact_test.go` file just adds another `var _ = Describe(...)`.
- `lib/claude/claude-runner.go` — first call site. The offending line (currently inside `buildCmd` near line 117 of the file): `glog.V(2).Infof("cmd.Env = %+v", cmd.Env)`. This file does NOT yet import `envparse`; add `"github.com/bborbe/agent/lib/envparse"` to the import block.
- `lib/pi/pi-runner.go` — second call site. The offending line (currently near line 117): `glog.V(4).Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", args, cmd.Dir, env)`. This file also does NOT yet import `envparse`; add the same import.
- `docs/dod.md` — project Definition of Done used by the validation step.

Coding plugin docs (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo conventions for this codebase.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-glog-guide.md` — glog levels and idioms; informative only, no behavioral changes here.

The module path is `github.com/bborbe/agent` (confirm via `head -1 go.mod` if needed). Use `github.com/bborbe/agent/lib/envparse` as the import path everywhere.
</context>

<requirements>

### 1. Add the redactor to `lib/envparse`

Create a NEW file `lib/envparse/redact.go` (do not stuff this into `envparse.go` — keep the existing file focused on parsers).

File contents must include the standard copyright header (copy from `envparse.go`) and:

```go
package envparse

import "strings"

// sensitiveKeyMarkers is the case-insensitive substring set used to classify
// an env key as sensitive. The list is intentionally small and conservative;
// the cost of a false positive is "operator sees ***" and the cost of a
// false negative is "secret in Loki", so we err on the side of redacting.
var sensitiveKeyMarkers = []string{
    "TOKEN",
    "SECRET",
    "PASSWORD",
    "PASSWD",
    "CREDENTIAL",
    "API_KEY",
    "PRIVATE_KEY",
    "ACCESS_KEY",
}

// IsSensitiveKey reports whether key looks like it carries a secret value.
// Matching is case-insensitive substring against a fixed marker list.
func IsSensitiveKey(key string) bool {
    upper := strings.ToUpper(key)
    for _, marker := range sensitiveKeyMarkers {
        if strings.Contains(upper, marker) {
            return true
        }
    }
    return false
}

// RedactForLog returns a copy of env entries (each in "KEY=VALUE" form, the
// same shape exec.Cmd.Env uses) where values for sensitive keys are replaced
// with the literal "***". Keys remain visible so operators can confirm which
// vars are passed without exposing the values. Entries without '=' pass
// through unchanged. The input slice is not mutated.
func RedactForLog(env []string) []string {
    if len(env) == 0 {
        return []string{}
    }
    out := make([]string, len(env))
    for i, entry := range env {
        idx := strings.IndexByte(entry, '=')
        if idx < 0 {
            out[i] = entry
            continue
        }
        key := entry[:idx]
        if IsSensitiveKey(key) {
            out[i] = key + "=***"
            continue
        }
        out[i] = entry
    }
    return out
}
```

Behavioral pins (must hold against the implementation above):

- Empty / nil input → returns an empty non-nil slice (`[]string{}`).
- The input slice is never mutated; a same-length copy is returned.
- For a sensitive key, the value is replaced with the literal three-character string `***` — NOT `<redacted>`, NOT `***[len=42]`, NOT a length hint of any kind.
- For a non-sensitive key, the entry passes through verbatim (including any `=` in the value).
- An entry with no `=` (e.g. `"NOEQ"`) passes through verbatim.
- An entry like `"GH_TOKEN="` (empty value) becomes `"GH_TOKEN=***"`.
- Key matching is case-insensitive substring (so `aws_credentials` is sensitive because uppercased it contains `CREDENTIAL`).

### 2. Add the tests

Create `lib/envparse/redact_test.go` in the external test package `envparse_test`, matching the style of `envparse_test.go` (copyright header, `Describe` per function, `It` cases, `Expect(...).To(...)`).

Required `Describe("IsSensitiveKey", ...)` cases — positives, each must `Expect(envparse.IsSensitiveKey("X")).To(BeTrue())`:

- `ANTHROPIC_AUTH_TOKEN`
- `GH_TOKEN`
- `GITHUB_TOKEN`
- `ANTHROPIC_API_KEY`
- `DB_PASSWORD`
- `MY_SECRET`
- `AWS_ACCESS_KEY_ID`
- `aws_credentials` (lowercase — verifies case-insensitivity)

Required `Describe("IsSensitiveKey", ...)` cases — negatives, each must `Expect(envparse.IsSensitiveKey("X")).To(BeFalse())`:

- `PATH`
- `HOME`
- `ANTHROPIC_BASE_URL`
- `ANTHROPIC_MODEL`
- `BOT_GITHUB_LOGIN`
- `ZONEINFO`

Required `Describe("RedactForLog", ...)` cases:

- `It("returns an empty slice for nil input", ...)` → `Expect(envparse.RedactForLog(nil)).To(Equal([]string{}))`.
- `It("returns an empty slice for empty input", ...)` → `Expect(envparse.RedactForLog([]string{})).To(Equal([]string{}))`.
- `It("keeps non-sensitive entries verbatim and redacts sensitive values, preserving order", ...)`:
  - Input: `[]string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=sk-ant-xxxxxxxx", "HOME=/home/agent", "GH_TOKEN=ghp_yyyyyyyy"}`
  - Expected: `[]string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=***", "HOME=/home/agent", "GH_TOKEN=***"}`
- `It("replaces an empty sensitive value with ***", ...)`:
  - Input: `[]string{"GH_TOKEN="}` → Expected: `[]string{"GH_TOKEN=***"}`.
- `It("passes entries without '=' through unchanged", ...)`:
  - Input: `[]string{"NOEQ", "FOO=bar"}` → Expected: `[]string{"NOEQ", "FOO=bar"}`.
- `It("does not mutate the input slice", ...)`:
  - Build `input := []string{"PATH=/usr/bin", "GH_TOKEN=ghp_secret"}`.
  - Call `envparse.RedactForLog(input)`.
  - `Expect(input).To(Equal([]string{"PATH=/usr/bin", "GH_TOKEN=ghp_secret"}))` (input unchanged).

The existing `envparse_suite_test.go` already runs `RunSpecs` — do NOT create a second suite file.

### 3. Wire the call sites

#### 3a. `lib/claude/claude-runner.go`

- Add `"github.com/bborbe/agent/lib/envparse"` to the import block (alphabetical position: it goes with the `github.com/bborbe/...` group, before `github.com/golang/glog`).
- Replace the single line currently reading `glog.V(2).Infof("cmd.Env = %+v", cmd.Env)` with:
  ```go
  glog.V(2).Infof("cmd.Env = %+v", envparse.RedactForLog(cmd.Env))
  ```
- Do NOT change the log level, do NOT change the format verb, do NOT touch any other line in this file.

#### 3b. `lib/pi/pi-runner.go`

- Add `"github.com/bborbe/agent/lib/envparse"` to the import block (same `github.com/bborbe/...` group, before `github.com/golang/glog`).
- Replace the single line currently reading `glog.V(4).Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", args, cmd.Dir, env)` with:
  ```go
  glog.V(4).Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", args, cmd.Dir, envparse.RedactForLog(env))
  ```
- Do NOT change the log level, do NOT change the format string, do NOT touch any other line.

### 4. Verify no other subprocess-env log sites slipped in

Run:

```bash
grep -rn 'cmd.Env\|cmd\.Env\|"env"\s*:' lib/ task/ cmd/ 2>/dev/null | grep -E 'glog\.|log\.' | grep -v _test.go
```

If this command returns any line that prints a subprocess `env` (i.e. a `[]string` of `KEY=VALUE`) that is NOT one of the two already updated in step 3, treat it as in-scope: route it through `envparse.RedactForLog` the same way. Stop and explain in the PR body if a hit is ambiguous (e.g. structured logger field). The two known sites are the only ones expected; the grep is a safety net, not a fishing trip.

### 5. Validate

Run `make precommit` from the repo root. Must pass. The test suite runs `lib/envparse/...` through the standard Ginkgo entrypoint already wired by `envparse_suite_test.go`, so no extra make target is needed.

If `make precommit` fails on an unrelated lint or formatting nit introduced by the import-block edit, fix the formatting (gofmt/goimports ordering) and re-run. Do NOT add `nolint` directives or skip checks.

</requirements>

<constraints>

- Redacted values MUST be the literal three-character string `***`. No length hints, no `<redacted>`, no per-key custom strings. This is load-bearing for the spec.
- Keys MUST remain visible. Do not redact the key name itself.
- `RedactForLog` MUST NOT mutate its input. Callers re-use `cmd.Env` for the actual subprocess; mutating it would change what the subprocess sees.
- Use the existing `lib/envparse` package — do NOT create a new package like `lib/redact` or `lib/logsafe`. The helper is small and belongs next to `KeyValuePairs`.
- Do NOT introduce new dependencies. `strings` from the stdlib is all that's needed.
- Do NOT change glog levels or format strings beyond wrapping the env argument.
- Do NOT commit. dark-factory handles the commit.
- Existing tests in `lib/envparse/envparse_test.go` must continue to pass unchanged.

</constraints>

<verification>

1. `make precommit` from the repo root passes.
2. New tests in `lib/envparse/redact_test.go` execute and all cases pass (Ginkgo will report them under the existing `Envparse Suite`).
3. Manual spot-check: `grep -n 'cmd.Env\|env:' lib/claude/claude-runner.go lib/pi/pi-runner.go | grep -i glog` — every hit must show an `envparse.RedactForLog(...)` wrapper around the env argument.
4. `grep -rn 'RedactForLog' lib/ | grep -v _test.go` — must show exactly two call sites (claude-runner.go and pi-runner.go) plus the definition in `lib/envparse/redact.go`.
5. Eyeball check on the new `redact.go`: the redaction sentinel is the literal string `"***"` — no length hint, no `<redacted>`.

</verification>
