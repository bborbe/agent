---
status: completed
summary: Added envparse.KeysOnly and switched both subprocess-env debug logs to it; RedactForLog now stars every value, with tests and changelog updated.
execution_id: agent-keys-only-env-log-exec-264-envparse-keys-only-env-log
dark-factory-version: v0.198.17
created: "2026-11-04T09:01:14Z"
queued: "2026-11-04T09:10:31Z"
started: "2026-11-04T09:10:35Z"
completed: "2026-11-04T09:20:26Z"
---

# Log subprocess environment key names only

<summary>
- The debug log that prints a child process's environment stops printing values entirely.
- The log keeps the variable names, so an operator can still see which variables were passed.
- A secret whose variable name matches no known marker can no longer reach the log, because nothing is logged but the name.
- Both places that log a subprocess environment switch to the name-only form.
- The existing redaction helper keeps working for any caller that wants names plus stars instead of bare names.
- That helper becomes fail-safe: it now stars every value, whatever the variable is called.
- The classifier that decides which names look sensitive is untouched, so any caller that still relies on it sees no change; the log path no longer depends on it at all.
- Tests cover the leak this fixes directly: a private-key-shaped value under an ordinary-looking variable name must not appear in the output.
- The changelog records the fix.
</summary>

<objective>
Make the subprocess-environment debug log leak-proof by construction instead of by denylist. Today the log helper prints a value unless its key name matches one of ten markers, so every new secret under an unlisted name leaks — exactly how the GitHub App private key (`GITHUB_APP_PEM`) reached every `agent-claude` pod log before `PEM` was added to the list. Logging key names only removes the class of bug: a name carries no secret, so there is nothing left to classify.
</objective>

<context>
Read `CLAUDE.md` for project conventions (Ginkgo/Gomega, external test packages, `make precommit`).
Read `docs/dod.md` — the definition of done applied after the change (exported symbols need doc comments; `CHANGELOG.md` needs an entry under `## Unreleased`).

Read the coding plugin guides for the two surfaces this touches (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 conventions for this codebase.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comment style for the two rewritten doc comments.

Read these source files before writing anything:

- `envparse/redact.go` — the file you are changing. It holds `sensitiveKeyMarkers`, `IsSensitiveKey`, and `RedactForLog`. `RedactForLog` is the structural exemplar for `KeysOnly`: `if env == nil { return nil }`, `out := make([]string, len(env))`, `strings.IndexByte(entry, '=')`, entries without `=` copied through, input never mutated.
- `envparse/redact_test.go` — the test-style exemplar. Ginkgo v2 + Gomega, external package `envparse_test`, dot-imported Ginkgo/Gomega, `Describe` / `It` / `DescribeTable` / `Entry`, a placeholder value declared once at block scope. The `DescribeTable("redacts every sensitive marker, ...")` block is the table shape to copy.
- `envparse/envparse_suite_test.go` — the suite bootstrap (`Envparse Suite`, 60s timeout); no change needed.
- `claude/claude-runner.go` — `buildCommand` (method on `*claudeRunner`) logs `cmd.Env` at the end, after `cmd.Env = env` and before `applyCredential`. One call site to switch.
- `pi/pi-runner.go` — the pi command builder logs `args`, `cmd.Dir` and `env` in one `glog.V(4).Infof` call. The second call site to switch.
- `prompts/completed/263-envparse-redact-pem-keys.md` — the predecessor change that added the `PEM` marker. It documents the leak this prompt closes structurally.

Both call-site files already import `"github.com/bborbe/agent/envparse"`, so switching the call needs no import change.
</context>

<requirements>
1. In `envparse/redact.go`, add a new exported function:

   ```
   func KeysOnly(env []string) []string
   ```

   Contract:
   - Each entry in `env` is `"KEY=VALUE"` (the shape `exec.Cmd.Env` uses). Return the key only — split at the FIRST `=` (`strings.IndexByte`), so a value containing `=` stays with the discarded value side.
   - An entry with no `=` passes through unchanged.
   - `nil` in → `nil` out.
   - `[]string{}` in → an empty, non-nil slice out.
   - Preserve order. Do not mutate the input slice; build a new slice with `make([]string, len(env))`, mirroring `RedactForLog`.

   Its GoDoc comment must state WHY, not what: the denylist approach fails open, so a secret under an unlisted key name reaches the log; key names carry no secret, so logging names only cannot leak by construction. Follow the existing comment style in the file (full sentences, wrapped).

2. Switch both logging call sites to `KeysOnly`, changing only the logged argument:

   - `claude/claude-runner.go`, inside `buildCommand`: the line that logs `cmd.Env` becomes
     `glog.V(2).Infof("cmd.Env = %+v", envparse.KeysOnly(cmd.Env))`.
   - `pi/pi-runner.go`, the `glog.V(4).Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", ...)` call: the last argument becomes `envparse.KeysOnly(env)`.

   Keep the format strings and the verbosity levels as they are. `cmd.Env` itself must still carry the full values — the change is to what is logged, never to what the child process receives. Do not reorder or remove any surrounding statement.

   (In `pi/pi-runner.go` the call is split across two lines in the file — `glog.V(4).` then `Infof(...)` on the next line. The format string and arguments are unchanged; only the last argument changes.)

   Pin that boundary with a test, because the failure is silent: if the log argument and the assignment are confused (`cmd.Env = envparse.KeysOnly(env)`), every grep below still passes and the child starts with a broken environment — `PATH`, `HOME` and `CLAUDE_CONFIG_DIR` all value-less. `claude/claude-runner_test.go` already installs a PATH shim (`writeShim`) and asserts on what the child saw; extend that harness: a shim that echoes its own environment, and an `It` asserting a value the parent set (a `ClaudeRunnerConfig.Env` override) still arrives at the child. Mirror the shape in `pi/pi-runner_test.go` if a shim is cheap there.

3. Change `RedactForLog` so it redacts EVERY value regardless of key name, keeping the `KEY=***` output shape. It no longer consults `IsSensitiveKey`. Everything else stays: `nil` → `nil`, `[]string{}` → empty slice, entries without `=` pass through unchanged, order preserved, input not mutated. Rewrite its GoDoc comment to say it stars every value and to state why it survives alongside `KeysOnly` (it is the shape for a caller that wants names plus stars rather than bare names; it is fail-safe because no key name is trusted). It is a pre-existing exported symbol in the published library (`github.com/bborbe/agent/envparse`), so the name stays and any out-of-repo caller keeps compiling; making it fail-safe means such a caller cannot leak a value under an unlisted key either. That is the point of the change — the denylist fails open, and this is the one remaining place where a caller could still reach it.

4. Do NOT change `sensitiveKeyMarkers` or `IsSensitiveKey`. The `Describe("IsSensitiveKey", ...)` block must keep passing untouched, and no marker may be removed, renamed, narrowed, or reordered. (The `Describe("RedactForLog", ...)` cases DO change — see requirement 6.)

5. In `envparse/redact_test.go`, add a `Describe("KeysOnly", ...)` block in the file's existing style, placed after the existing `Describe("RedactForLog", ...)` block, covering:

   a. A `DescribeTable` of entry → expected key, including the load-bearing case: a secret-shaped value under a key that matches NO marker. Declare the value once at block scope, reusing the file's existing placeholder variable shape (a block-scoped `:=` inside the `Describe` block, with the `// #nosec G101` comment on the same line):
      ```
      HARMLESS_LOOKING=-----BEGIN RSA PRIVATE KEY-----sentinel-----END RSA PRIVATE KEY-----
      ```
      → `HARMLESS_LOOKING`. The fixture line carries `// #nosec G101` plus a short comment saying it is a synthetic placeholder, not a real key — the same treatment the existing `pemPlaceholder` line in this file already uses. Add at least three more marker-free keys (e.g. a plain `PATH`, an identifier like `GITHUB_APP_ID`, and one with `=` inside the value) so the table proves classification no longer matters.
   b. A case for an entry with no `=` — passes through unchanged.
   c. `KeysOnly(nil)` returns nil; `KeysOnly([]string{})` returns an empty slice (`Equal([]string{})`, not `BeNil()`).
   d. Input slice not mutated.
   e. One realistic mixed-environment `It` — the same GitHub App / Anthropic variable set the existing `RedactForLog` mixed-environment test uses — asserting the output is exactly the key names in order, and that no value survives: `Expect(out).NotTo(ContainElement(ContainSubstring("sentinel")))` plus a marker-free value such as the installation id appearing nowhere in the joined output. This is the case that mirrors what production logs.

6. In the existing `Describe("RedactForLog", ...)` block, update the cases whose expectation was "non-sensitive passes through" — they are now wrong by design. Update, do not delete the coverage:

   - The `It("keeps non-sensitive entries verbatim and redacts sensitive values, preserving order")` case: every value is now `***`, including `PATH` and `HOME`. Rename it to match the new behaviour.
   - The `It("passes entries without '=' through unchanged")` case: `NOEQ` still passes through, but `FOO=bar` becomes `FOO=***`. Keep both entries and update the expectation.
   - The `DescribeTable("passes non-sensitive entries through unchanged", ...)` block: replace it with a table asserting a marker-free key's value is now redacted (`GITHUB_APP_ID=123456` → `GITHUB_APP_ID=***`, and the same for the installation id, the base URL, and the model).
   - The `It("redacts only the PEM and token in a realistic mixed environment, ...")` case: all five values become `***`; keep the `NotTo(ContainElement(ContainSubstring("sentinel")))` assertion and rename the case.

   Leave untouched: nil input, empty input, the proxy-URL case, the empty-sensitive-value case, the non-mutation case, and the `DescribeTable("redacts every sensitive marker, ...")` block — its expectations still hold. Rename that table's description, though: after this change every entry is starred regardless of marker, so it no longer tests the markers (that coverage now lives in `Describe("IsSensitiveKey", ...)`). E.g. `DescribeTable("stars every entry, including every former marker", ...)`.

7. Add a GoDoc-quality comment on `KeysOnly` only — `RedactForLog` keeps its existing comment updated per requirement 3. No other new exported symbols.

8. In `CHANGELOG.md`, insert a `## Unreleased` section directly above the current head section `## v0.139.3`, with a blank line before and after, containing one entry:
   ```
   - fix: log subprocess environment key names only, so a secret under an unmapped key name can no longer reach the pod log; `RedactForLog` now stars every value
   ```

9. Touch no other files. Before finishing, re-run every command in `<verification>`, confirm each passes, and walk requirements 1-8 against the diff.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass; `IsSensitiveKey` and `sensitiveKeyMarkers` must be behaviourally identical to before this change.
- The fix must not weaken anything: `RedactForLog` keeps redacting every key it redacted before, and additionally redacts every key it did not.
- Only these five files may change: `envparse/redact.go`, `envparse/redact_test.go`, `claude/claude-runner.go`, `pi/pi-runner.go`, `CHANGELOG.md`.
- Do NOT change what the child process receives — `cmd.Env` stays the full environment at both call sites. Only the log argument changes.
- No new dependency. No config knob, no env-var switch, no log-verbosity change, no new package.
- Do NOT include any real key material anywhere — not in tests, not in comments, not in the changelog. The synthetic `sentinel` placeholder is the only key-shaped string permitted, and it carries `// #nosec G101` with a comment saying it is a placeholder.
- Test framework is Ginkgo v2 + Gomega in the external package `envparse_test`; do not introduce `testing` table loops and do not create a new test file.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` at the repo root — must pass. (The Makefile derives `ROOTDIR` from `git rev-parse --show-toplevel`; supply it explicitly so the call works whether or not `.git` is available in the container.)

Run `go test -mod=mod ./envparse/... ./claude/... ./pi/... -count=1` — must pass.

Run `grep -q 'func KeysOnly(' envparse/redact.go` — must succeed.

Run `grep -q 'KeysOnly' claude/claude-runner.go` — must succeed.
Run `! grep -q 'RedactForLog' claude/claude-runner.go` — must succeed (the claude call site no longer uses it).
Run `grep -q 'KeysOnly' pi/pi-runner.go` — must succeed.
Run `! grep -q 'cmd.Env = envparse' claude/claude-runner.go` — must succeed (the child keeps the full environment).
Run `! grep -q 'cmd.Env = envparse' pi/pi-runner.go` — must succeed.
Run `! grep -q 'RedactForLog' pi/pi-runner.go` — must succeed.

Run `grep -q 'HARMLESS_LOOKING=' envparse/redact_test.go` — must succeed (the marker-free secret-shaped fixture exists).

Run `awk '/^## /{print; exit}' CHANGELOG.md` — must print exactly `## Unreleased` (the new section is the head).
Run `awk '/^## /{sec=$0} /key names only/{print "sits under: " sec}' CHANGELOG.md` — must print exactly `sits under: ## Unreleased`.
</verification>
