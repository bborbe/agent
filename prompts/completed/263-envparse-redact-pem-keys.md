---
status: completed
summary: Added the PEM marker to the shared env-redaction classifier so GITHUB_APP_PEM values are redacted, extended the Ginkgo tests to cover all markers plus pass-through keys and a realistic mixed environment, and recorded the fix under a new Unreleased changelog section.
execution_id: agent-redact-pem-exec-263-envparse-redact-pem-keys
dark-factory-version: v0.198.7
created: "2026-11-03T17:40:38Z"
queued: "2026-11-03T17:53:18Z"
started: "2026-11-03T17:53:30Z"
completed: "2026-11-03T18:01:04Z"
---

# Redact env values for PEM keys in the log helper

<summary>
- The shared environment-redaction helper stops printing private-key material.
- Any environment key whose name contains "PEM" has its value replaced with stars before it is logged.
- The GitHub App private key injected into agent containers is named `GITHUB_APP_PEM`, so it no longer appears verbatim in pod logs.
- Ordinary identifiers — the GitHub App id, the installation id, the model name, and the base URL — still appear in full.
- Every key type that was already redacted stays redacted exactly as before, with no marker removed or weakened.
- Redacted output keeps its key names and its ordering, so an operator can still see which variables were passed.
- Both logging call sites inherit the fix without being touched, because the classification lives in one shared list.
- New tests cover the private-key case, the pass-through identifiers, every existing marker, and a realistic mixed environment.
- The changelog records the fix.
</summary>

<objective>
Stop the subprocess environment debug log from writing a GitHub App private key into pod logs. The key arrives as the environment variable `GITHUB_APP_PEM`, whose name matches none of the classifier's markers, so the value passes through the redaction helper unchanged and is logged in full at the default verbosity of every agent container. Close the leak at the classifier — one shared marker list — so both logging call sites and every future caller inherit the fix, without weakening redaction for any key that is already covered.
</objective>

<context>
Read `CLAUDE.md` for project conventions (dark-factory flow, Ginkgo/Gomega, `make precommit`).
Read `envparse/redact.go` — `sensitiveKeyMarkers` (the marker list and its "err on the side of redacting" doc comment), `IsSensitiveKey` (case-insensitive substring match against the list), and `RedactForLog` (splits each `KEY=VALUE` entry at the first `=`, replaces the value with `***` for a sensitive key, preserves order, does not mutate its input, returns nil for nil input).
Read `envparse/redact_test.go` — the existing `Describe("IsSensitiveKey", ...)` and `Describe("RedactForLog", ...)` blocks. This file is the style exemplar: Ginkgo v2 + Gomega, external test package `envparse_test`, import `"github.com/bborbe/agent/envparse"`, dot-imported Ginkgo/Gomega.
Read `envparse/envparse_suite_test.go` — the suite bootstrap; the suite is `Envparse Suite` and runs via `go test ./envparse/...`.
Read `agent_task-type_test.go` — the in-repo `DescribeTable` / `Entry` exemplar (its `Describe("Validate", ...)` block) for table-driven tests in this codebase.
Read the top of `CHANGELOG.md` — the newest section is `## v0.139.0`; there is currently no `## Unreleased` section.
Read `docs/dod.md` — the definition of done applied after the change.

Background for the fix, so the requirement is not over- or under-scoped:
- The production call sites are `claude/claude-runner.go` (`glog.V(2).Infof("cmd.Env = %+v", envparse.RedactForLog(cmd.Env))` inside the command builder) and `pi/pi-runner.go` (`glog.V(4).Infof("spawning pi: pi %v\n  cwd: %s\n  env: %v", args, cmd.Dir, envparse.RedactForLog(env))`). Both live in their respective `buildCommand` methods and already call the helper — neither file changes.
- `GITHUB_APP_PEM` is not defined in this repository; the sibling `agent-claude` image injects it into the child environment. Do not add it anywhere here.
- The verbosity is on by default in that image, so the value reaches every agent pod's log, not just a debug run.
</context>

<requirements>
1. In `envparse/redact.go`, add the marker `"PEM"` to `sensitiveKeyMarkers` as a new final entry immediately after the existing `"PROXY"` entry (the explanatory comment block above `"PROXY"` stays where it is). Keep the list's existing style: one quoted uppercase string per line, trailing comma, tab indentation as the file already has. The list already contains nine markers (`TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`, `CREDENTIAL`, `API_KEY`, `PRIVATE_KEY`, `ACCESS_KEY`, `PROXY`) — `PEM` makes ten. Do not reorder the existing nine.

2. Keep the list's doc comment accurate. Leave the existing rationale sentences (the false-positive / false-negative cost argument and the "err on the side of redacting" conclusion) verbatim, and add one sentence explaining the new entry — that `PEM` is included because private keys are delivered as PEM text, e.g. the GitHub App key injected as `GITHUB_APP_PEM`.

3. Do NOT change `IsSensitiveKey` or `RedactForLog`. Matching stays a case-insensitive substring test against the marker list; the `***` replacement, the first-`=` split, the order preservation, the nil handling, and the non-mutation guarantee all stay as they are. No new function, no new parameter, no new exported symbol.

4. Do NOT touch the logging call sites (`claude/claude-runner.go`, `pi/pi-runner.go`) or any log verbosity. The fix belongs in the classifier, so every caller inherits it.

5. Extend `envparse/redact_test.go` in the file's existing Ginkgo style, using `DescribeTable` / `Entry` (see the `agent_task-type_test.go` exemplar). Define the placeholder value once at the top of the relevant block and reuse it:
   ```
   -----BEGIN RSA PRIVATE KEY-----sentinel-----END RSA PRIVATE KEY-----
   ```
   This placeholder is the only key-shaped string allowed anywhere in the change. Do NOT add a real key, a real base64 body, or a truncated copy of one.

6. Add a `DescribeTable` to the existing `Describe("RedactForLog", ...)` block that takes an entry and its expected redacted entry, and covers at minimum these pairs (input → expected):
   - `GITHUB_APP_PEM=<placeholder>` → `GITHUB_APP_PEM=***`
   - `ANTHROPIC_AUTH_TOKEN=sk-ant-xxxxxxxx` → `ANTHROPIC_AUTH_TOKEN=***` (TOKEN)
   - `MY_SECRET=value` → `MY_SECRET=***` (SECRET)
   - `DB_PASSWORD=hunter2` → `DB_PASSWORD=***` (PASSWORD)
   - `DB_PASSWD=hunter2` → `DB_PASSWD=***` (PASSWD)
   - `AWS_CREDENTIAL=value` → `AWS_CREDENTIAL=***` (CREDENTIAL)
   - `ANTHROPIC_API_KEY=sk-ant-xxxxxxxx` → `ANTHROPIC_API_KEY=***` (API_KEY)
   - `SSH_PRIVATE_KEY=value` → `SSH_PRIVATE_KEY=***` (PRIVATE_KEY)
   - `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE` → `AWS_ACCESS_KEY_ID=***` (ACCESS_KEY)
   - `HTTPS_PROXY=http://user:pass@proxy:8888` → `HTTPS_PROXY=***` (PROXY)
   The table asserts whole-entry equality against a one-element slice, so it also proves the key name survives and the value is replaced with exactly `***`.

7. Add a second `DescribeTable` (in the same `RedactForLog` block) for keys that must pass through unchanged — expected output equals the input — covering at minimum: `GITHUB_APP_ID=123456`, `GITHUB_APP_INSTALLATION_ID=98765432`, `ANTHROPIC_BASE_URL=http://127.0.0.1:8788`, `ANTHROPIC_MODEL=claude-sonnet-4`.

8. Add one `It` in the `RedactForLog` block that redacts a realistic mixed environment slice — `GITHUB_APP_ID`, `GITHUB_APP_PEM` (the placeholder), `GITHUB_APP_INSTALLATION_ID`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` — and asserts: order is preserved, only the PEM and token values become `***`, the two GitHub identifiers and the base URL are unchanged, and the placeholder body does not survive anywhere in the output (e.g. `Expect(redacted).NotTo(ContainElement(ContainSubstring("sentinel")))`). This is the case that mirrors what production actually logs.

9. Add a `DescribeTable` to the existing `Describe("IsSensitiveKey", ...)` block covering the classification directly: `GITHUB_APP_PEM` is sensitive; each of `GITHUB_APP_ID` and `GITHUB_APP_INSTALLATION_ID` is not; `ANTHROPIC_BASE_URL` and `ANTHROPIC_MODEL` are not. Keep the existing `It` cases in that block untouched.

10. In `CHANGELOG.md`, insert a new `## Unreleased` section directly above `## v0.139.0` (the current head section), with a blank line before and after, containing exactly this entry:
    ```
    - fix: redact env values whose key contains `PEM`, so `GITHUB_APP_PEM` (the GitHub App private key) is no longer printed verbatim by the subprocess env debug log
    ```

11. Touch no other files. Before finishing, re-run every command in `<verification>`, confirm each passes, and walk requirements 1-10 against the diff.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass, and the existing `It` cases in `envparse/redact_test.go` must remain unmodified.
- Do NOT include any real key material anywhere — not in the test, not in the changelog entry, not in a comment. The placeholder from requirement 5 is the only permitted key-shaped string.
- The fix must not weaken redaction of any existing marker: do not remove, rename, reorder away, or narrow `TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`, `CREDENTIAL`, `API_KEY`, `PRIVATE_KEY`, `ACCESS_KEY`, `PROXY`, and do not change the matching from case-insensitive substring to anything narrower.
- `PROXY` already exists in the list and must stay — it redacts proxy URLs that embed credentials. Do not add any marker beyond `PEM`; the change is scoped to closing this one leak.
- No new dependency, no new exported function, no config knob, no logging change, no change to the call sites.
- Only `envparse/redact.go`, `envparse/redact_test.go`, and `CHANGELOG.md` may change.
- Test framework is Ginkgo v2 + Gomega in the external package `envparse_test`; do not introduce `testing` table loops or a new test file.
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` at the repo root — must pass. (The Makefile derives `ROOTDIR` from `git rev-parse --show-toplevel`; supply it explicitly so the call works whether or not `.git` is available in the container.)

Run `go test -mod=mod ./envparse/... -count=1` — must pass.

Run `grep -q '"PEM",' envparse/redact.go` — must succeed.

Run this loop — must print nothing and exit 0 (every marker still present):
```
for m in TOKEN SECRET PASSWORD PASSWD CREDENTIAL API_KEY PRIVATE_KEY ACCESS_KEY PROXY PEM; do grep -q "\"$m\"," envparse/redact.go || { echo "missing marker: $m"; exit 1; }; done
```

Run `grep -q 'sentinel' envparse/redact_test.go` — must succeed (the placeholder is used).

Run `awk '/^## /{sec=$0} /GITHUB_APP_PEM/{print "sits under: " sec}' CHANGELOG.md` — must print exactly `sits under: ## Unreleased`.

Run `awk '/^## /{print; exit}' CHANGELOG.md` — must print exactly `## Unreleased` (proves the new section is the head, not merely that one exists somewhere).
</verification>
