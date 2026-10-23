---
status: draft
spec: [048-agent-egress-proxy]
created: "2026-10-23T10:40:00Z"
---

<summary>
- The Claude runner stops dropping proxy settings before the agent's tools start
- A pod that carries `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` now hands them to the Claude CLI child process, so `git`, `wget` and every other Bash-tool command can reach the egress proxy
- The runner's environment filter stays a filter: only the six proxy names are added, and an unrelated parent variable is still kept out
- Nothing changes when the proxy variables are unset — existing agents behave exactly as before
- A unit test locks both halves in place: the proxy names pass through, an arbitrary parent secret does not
</summary>

<objective>
Extend the Claude runner's child-process environment allowlist in `claude/claude-runner.go` to admit `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and their lowercase forms, with a unit test proving both that the proxy names pass through and that the allowlist is still not a pass-through for arbitrary parent variables. After this prompt an agent pod carrying proxy settings actually egresses through the egress proxy instead of failing closed.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these project files in full before editing:
- `claude/claude-runner.go` — `buildSubprocessEnv` (around line 383) is the function to change; its doc comment (around line 360) states the three-layer precedence and the trust-boundary rationale that your edit must keep true.
- `claude/claude-runner_test.go` — the Ginkgo suite. It is an **external** test package (`package claude_test`), so it cannot call `buildSubprocessEnv` directly; every existing test drives the public runner against a fake `claude` shim script written to a temp dir and prepended to `PATH` (see `writeShim` in the first `Describe`, and `writeEnvShim` at ~line 174 in the `CLAUDE_CONFIG_DIR env propagation` Describe — the closest precedent, since it echoes an env var and emits a valid result event). Follow that pattern.
- `docs/agent-network-security.md` — why the agent needs a proxy at all; the spec's background document.

Key facts (verified against the repo):
- `buildSubprocessEnv` builds the child environment in three layers: (1) an allowlist pass-through of `HOME, PATH, USER, TZ, ZONEINFO, TMPDIR, LANG, LC_ALL`; (2) `CLAUDE_CONFIG_DIR`; (3) `ClaudeRunnerConfig.Env`, which overrides everything above.
- Layer 3 is where `bborbe/agent-claude` injects its own settings (that repo parses a comma-separated `CLAUDE_ENV`), but it cannot carry these three: the parser splits on commas (`envparse.KeyValuePairs`, `envparse/envparse.go` in this repo) and `NO_PROXY`'s value is itself a comma-separated host list, so the trio cannot ride that field. Layer 1 is therefore the only correct place.
- Layer 1 exists as a trust boundary: the executor's own process carries Kafka credentials and secrets that must not reach the agent. Widening it by six well-known proxy names keeps that property; turning it into "pass everything" would destroy it.
- `claude-session.go` also calls `buildSubprocessEnv`, so one change covers both the one-shot runner and the long-lived session — do not add a second copy of the list.
- Out of scope, deliberately: `pi/pi-runner.go` has its own `buildSubprocessEnv` with the same eight-name allowlist and would drop the proxy vars the same way. Spec 048 permits exactly one code change (the Claude runner), so leave `pi/` untouched and note the gap in your report rather than fixing it here.
- This prompt changes Go code, so `make precommit` must pass. No chart file, values key or chart version changes — but note `make precommit` runs `test` → `test-helm` → `hack/test-helm-render.sh` unconditionally, so a chart render failure still fails the gate.
</context>

<requirements>

1. **Extend the layer-1 allowlist in `claude/claude-runner.go`.** Replace the single hard-coded slice in `buildSubprocessEnv` with a slice that also contains the six proxy names, keeping the existing iteration and `os.LookupEnv` guard unchanged:

   ```go
   // Layer 1: allowlist pass-through.
   for _, k := range []string{
       "HOME", "PATH", "USER", "TZ", "ZONEINFO", "TMPDIR", "LANG", "LC_ALL",
       // Proxy settings (spec 048): the agent's Bash tool reaches the internet
       // only through the egress proxy, so the pod's proxy configuration has to
       // survive this boundary. Without them every outbound call fails closed.
       "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
       "http_proxy", "https_proxy", "no_proxy",
   } {
       if v, ok := os.LookupEnv(k); ok {
           env[k] = v
       }
   }
   ```

2. **Update the `buildSubprocessEnv` doc comment.** Layer 1's description currently reads "only well-known, non-sensitive vars pass through automatically". Add one sentence after it naming the proxy variables and why they belong there (a pod-level proxy setting is useless if the child process cannot see it), and keep the existing warning that the allowlist is a trust boundary, not a pass-through.

3. **Add a regression test to `claude/claude-runner_test.go`.** New `Describe`/`It` following the existing shim pattern:
   - Set all six names in the parent environment via `os.Setenv` — `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `http_proxy`, `https_proxy`, `no_proxy` — restoring each with `DeferCleanup` (restore-or-unset, the `LookupEnv` form already used in this file). Assert all six, not just the uppercase three: a typo in a lowercase entry is otherwise untested.
   - Also set a control variable that must **not** cross the boundary, e.g. `AWS_SECRET_ACCESS_KEY=control-value`, and restore it with `DeferCleanup` too.
   - The shim script writes the six expanded values plus `$AWS_SECRET_ACCESS_KEY` into a temp file whose path is interpolated into the script body — **not** passed through an env var, which the allowlist would drop — then emits a valid stream-json result event (`echo '{"type":"result","result":"ok"}'`) and exits 0. See `writeEnvShim` (around line 174); a shim that exits 0 with no result event makes `Run` return `no result event found in claude CLI output`, so `Expect(err).NotTo(HaveOccurred())` would fail.
   - Assert each of the six appears with its set value and the control value appears nowhere in the captured output.

   Do not weaken or delete any existing test. Do not assert on the exact ordering of the environment slice — it is built from a map.

4. **Add a CHANGELOG entry.** Under `## Unreleased` (create the section directly under the `# Changelog` intro block if it is absent — it is absent today; the first version heading is `## v0.120.0`):

   ```
   - feat(claude): pass `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` (and their lowercase forms) through the Claude runner's child-env allowlist, so an agent pod's egress-proxy settings reach the Bash tool instead of being dropped (spec 048).
   ```

   No chart version bump — this prompt touches no chart file.

5. **Do NOT make any of these changes:**
   - Do NOT replace the allowlist with a pass-through of the whole parent environment, and do NOT add any name beyond the six proxy variables.
   - Do NOT touch `ClaudeRunnerConfig.Env` semantics (layer 3 still wins over the allowlist).
   - Do NOT add the proxy names to `agent-claude`'s `buildClaudeEnv` or to `CLAUDE_ENV` — that path cannot carry a comma-separated `NO_PROXY` value.
   - Do NOT change any Helm template, values key, or chart version.
   - Do NOT commit — dark-factory handles git.

</requirements>

<constraints>
- The allowlist stays an allowlist: exactly six names are added, nothing else.
- Layer 3 (`ClaudeRunnerConfig.Env`) keeps its highest-precedence position.
- No behaviour change when the proxy variables are unset — `os.LookupEnv`'s `ok` guard already gives this; do not add defaults.
- One list, not two: `claude-session.go` and `claude-runner.go` must keep sharing `buildSubprocessEnv`.
- No new dependency, no vendoring.
- Existing tests must keep passing unchanged.
</constraints>

<verification>
Run the package tests and the full precommit gate:

```bash
set -o pipefail
go test ./claude/... 2>&1 | tail -20     # must pass, including the new test
go build ./... 2>&1 | tail -5            # must be clean
make precommit 2>&1 | tail -20           # must exit 0 (format + generate + test + lint + license)
```

Then confirm the change is where it belongs and nowhere else:

```bash
grep -c '"HTTP_PROXY"' claude/claude-runner.go        # >=1 (quoted form — a prose mention in the doc comment does not count)
grep -c '"NO_PROXY"' claude/claude-runner.go          # >=1
grep -c 'HTTP_PROXY' claude/claude-session.go         # ==0 (no second list)
grep -c 'HTTP_PROXY' claude/claude-runner_test.go     # >=1 (the regression test exists)
grep -c 'Unreleased' CHANGELOG.md                     # >=1
```

Every check must return the annotated value or the prompt is not done. The harness also runs `make precommit` as its `validationCommand` — let it pass.
</verification>
