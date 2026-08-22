---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-08-21T07:45:20Z"
generating: "2026-08-21T08:45:02Z"
prompted: "2026-08-21T10:37:36Z"
verifying: "2026-08-22T15:11:26Z"
completed: "2026-08-22T15:29:03Z"
branch: dark-factory/claude-runner-persists-partial-output
---

## Summary

- When a claude CLI run is terminated early (K8s Job deadline SIGKILL, context cancellation, or a non-zero exit), the review markdown the model streamed before termination is currently thrown away — only a 5-line tail survives into the error message. Large multi-file PR reviews (`Seibert-Data/moss` PR #1, 51 files) ended in `human_review` with a blank `needs_input` and nothing usable.
- This spec changes the claude library in `github.com/bborbe/agent` so the streamed assistant text captured up to the moment of termination is returned to the caller alongside the error — a bounded, structured partial the caller can persist.
- The existing tail-line error context is preserved, and the change is additive: the `Run` call signature and the `ClaudeRunner` interface are unchanged; every existing caller compiles and behaves identically on the success path.
- The partial is bounded (keeps the most recent text, at least 16 KiB) so a runaway stream cannot exhaust memory.
- This is the library half of a two-repo salvage feature. The companion spec in `github-pr-review-agent` (`pr-reviewer-soft-time-budget-and-salvage.md`) consumes this capture to write a `## Salvage` section into the task file. This library change must land and release first.

## Problem

The pr-reviewer agent fails on large multi-file PRs because the LLM investigation phase is unbounded and burns the K8s Job deadline before reaching a verdict. When the claude subprocess is killed, the wrapper discards all streamed partial output: `scanOutput` collects the stream, but on `cmd.Wait()` error only a tail-line survives into the error message, and the run-cancellation branch of the scanner zeroes even that. The partial review content (`## Review` sections written so far, `## Findings` batches) is lost. Evidence: `Seibert-Data/moss` PR #1 (51 files) — the review cut off mid-investigation producing a blank `needs_input` escalation with nothing usable. Root cause verified by reproduction: the mechanical funnel is not the problem (74 rules over 51 files in 1.8s); the failure is unbounded LLM investigation plus discarded partial output on kill. The salvage fix requires this library to surface the streamed partial on termination — without it, the caller has nothing to persist.

## Goal

After this work, whenever a claude run does not complete normally, the caller of the runner receives the streamed assistant text captured up to the point of termination — bounded, verbatim, and clearly separated from the error message — so it can persist partial findings instead of losing them. Successful runs are byte-identical to today. Existing callers compile and run unchanged.

## Non-goals

- Do NOT change when or how the claude subprocess is terminated. The K8s deadline, executor context wiring, and the task-file salvage routing (`## Salvage`, `human_review`) are the companion repo's scope (`pr-reviewer-soft-time-budget-and-salvage.md`).
- Do NOT write into the task file from this library. The runner only surfaces the partial to the caller; persistence is the caller's decision.
- Do NOT add a config knob, env var, or opt-out for the capture. A cap that can be disabled is an escape hatch on the Goal; the cap is a frozen package constant (≥ 16 KiB).
- Do NOT parse or branch on the CLI's event schema beyond the existing shape-agnostic extraction. An unrecognized schema yields an empty partial, never a crash (spec 023 doctrine).
- Do NOT change the CLI invocation flag set (`--print --output-format stream-json --verbose --strict-mcp-config`).
- Do NOT change the tail ring-buffer error format (5 lines × 512 bytes, ` | ` joiner).
- Do NOT add a scenario. The behavior is fully reachable via unit tests with a shell-script `claude` shim (spec 023's established pattern).

## Acceptance Criteria

- [ ] A run terminated mid-stream surfaces the assistant text streamed before termination to the caller alongside a non-nil error — evidence: new Ginkgo specs in `claude/claude-runner_test.go` using a shell-script `claude` shim in a temp dir prepended to `PATH`:
  - (a) shim emits canned stream-json assistant-text lines then `exit 1` → assert the returned partial contains the canned text verbatim and the error is non-nil
  - (b) shim streams a line then blocks; the test cancels the run context → assert the returned partial equals the pre-cancellation text (non-empty) and the error is non-nil
- [ ] The partial is the assistant's plain streamed text, not the raw stream-json envelope — evidence: test (a) additionally asserts the partial does NOT contain the `{"type":` envelope opening of the emitted stream-json lines (negative row)
- [ ] The partial is bounded and keeps the most recent text, with a ≥ 16 KiB floor — evidence: Ginkgo spec where the shim streams more assistant text than the cap asserts partial length ≥ 16384 bytes, the last streamed line is present, and the first streamed line is absent (earliest bytes dropped)
- [ ] The tail-line error context is preserved on terminated runs — evidence: the existing tail-error Ginkgo specs (auth-failure diagnostic, 5-line retention, 512-byte truncation, no `: :`) pass unmodified; the new kill-path test asserts the error still contains the shim's diagnostic line, not only the partial
- [ ] Existing callers compile and the success path is unchanged; the generated mock is untouched — evidence: `make precommit` exits 0; `make generate` exits 0 and `git diff --name-only` shows no change under `mocks/`; existing runner/task-runner/agent-step Ginkgo specs pass unmodified
- [ ] `CHANGELOG.md` carries the change under a new top `## Unreleased` section — evidence: `grep -n '## Unreleased' CHANGELOG.md` returns line ≥ 1 AND it is the top section (above `## v0.81.3`; `sed -n '1,/^## /p' CHANGELOG.md` shows `# Changelog` then `## Unreleased`); `grep -n -i 'partial output' CHANGELOG.md` returns line ≥ 1; the changelog-fold guard passes (`git diff origin/master -- CHANGELOG.md | grep -E '^[-+]## '` shows only `+## Unreleased`)
- [ ] Scope containment — evidence (negative): `git diff --name-only origin/master...HEAD` lists only files under `claude/`, `CHANGELOG.md`, and `specs/`

Scenario coverage — **NO new scenario.** The behavior is fully reachable via unit tests against the runner using a shell-script CLI shim (the same pattern as spec 023). Nothing about the capture benefits from an E2E rung; the companion spec owns the operator-level replay of the moss PR #1 failure.

## Verification

## Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — fmt, generate, test, lint, vet, vuln, license clean (exit 0)
- `go test ./claude/...` — new kill-path (non-zero exit + context cancellation), envelope-negative, bounded-capture, and error-context-preserved specs green (exit 0)
- `grep -n 'Partial' claude/claude-result.go` — returns line ≥ 1 (the additive field exists)
- `grep -n '## Unreleased' CHANGELOG.md` — returns line ≥ 1 and it is the top section (above `## v0.81.3`); `grep -n -i 'partial output' CHANGELOG.md` — returns line ≥ 1
- `git diff --name-only origin/master...HEAD` — lists only files under `claude/`, `CHANGELOG.md`, and `specs/`

## Operator-executable (runs on the host after PR merge, verification ladder)

Release the library so the companion repo can bump its `go.mod`. From the merged commit, per the project's tag policy (`CLAUDE.md`): rename `## Unreleased` to the next-version header, then cut the paired tags at the same commit:

```bash
# in the merged worktree, after renaming ## Unreleased -> ## v0.82.0 in CHANGELOG.md
git commit -m "release v0.82.0"
git tag v0.82.0
git tag lib/v0.82.0
git push origin master v0.82.0 lib/v0.82.0
```

The version is a semver MINOR bump (v0.81.3 → v0.82.0): this change adds a public-API field to a library consumed by other repos and is backward compatible. This release MUST complete before the companion spec's dependency-bump prompt (`github-pr-review-agent`) can pass — cross-check with `git ls-remote --tags origin | grep -E 'v0.82.0'`.

## Desired Behavior

1. **Stream capture.** During every run the runner accumulates the streamed assistant text (the review markdown the model is writing) into a partial buffer, in the same linear pass that extracts the result text, the usage summary, and the tail ring buffer. Tool payloads, usage telemetry, and the stream-json envelope itself are excluded from the partial.
2. **Termination surfacing.** Whenever the run does not complete normally — context deadline, non-zero exit, or a missing result event — the runner returns the partial captured so far to the caller alongside the error. The run-cancellation branch that currently zeroes captured state instead returns what was captured up to that point. On normal success the partial is likewise populated with any streamed text, and callers that ignore it are unaffected.
3. **Bounded partial.** The partial is capped by a package-internal constant of at least 16384 bytes; on overflow the most recent text is kept and the earliest dropped. The cap is not configurable and cannot be disabled.
4. **Error context preserved.** The error returned by a terminated run still carries the existing bounded tail (5 lines × 512 bytes, ` | `-joined) and the existing empty-case message (`no stdout captured`), so the diagnostic value of the failure message is not reduced by the new partial.
5. **Additive surface.** `ClaudeResult` gains one field, `Partial string`, omitted when empty in JSON. The `Run(ctx, prompt) (*ClaudeResult, error)` signature and the `ClaudeRunner` interface are unchanged; the generated mock and every existing caller (agent steps, task runner, healthcheck step, downstream repos) compile and behave identically on the success path.

## Constraints

- The `Run(ctx, prompt) (*ClaudeResult, error)` signature and the `ClaudeRunner` interface are frozen. No new method, no new argument. The generated mock at `mocks/claude-claude-runner.go` MUST NOT change — `make generate` exits 0 with zero diff.
- `ClaudeResult` gains exactly one additive field, `Partial string` (JSON `partial,omitempty`). Every existing field and JSON tag is unchanged; the success-path `Result`, token, and turn semantics are unchanged.
- The tail ring-buffer contract is frozen: max 5 lines, max 512 bytes per line, ` | ` joiner, and the `no stdout captured` empty case. Existing tail-error tests pass unmodified.
- The run-cancellation branch of the scanner MUST return the captured partial and tail instead of empty values — discarding them is the bug this spec fixes.
- The capture cap is a package-internal constant ≥ 16384 bytes that keeps the most recent text. It is NOT a `ClaudeRunnerConfig` field and no env knob or opt-out exists for it.
- The CLI invocation flag set (`--print --output-format stream-json --verbose --strict-mcp-config`) is unchanged.
- The change is confined to `claude/` and `CHANGELOG.md` (plus this spec under `specs/`). No other package is touched.
- Project tag policy from `CLAUDE.md` applies at release: paired `vX.Y.Z` + `lib/vX.Y.Z` tags at the same commit matching the latest CHANGELOG header. This release is a semver MINOR (additive public API) and must land before the companion repo's dependency bump.
- The companion spec in `github-pr-review-agent` (`specs/pr-reviewer-soft-time-budget-and-salvage.md`, worktree `~/Documents/workspaces/github-pr-review-agent-salvage`) consumes this capture; this spec lands and releases first.
- Test conventions follow the repo defaults: Ginkgo v2 + Gomega, external test package (`claude_test`), fake `claude` binary as a shell shim in a temp dir prepended to `PATH` — no network, no real claude install.

## Assumptions

- The claude CLI streams the review markdown as assistant text content events in stream-json mode, which the runner can accumulate into the partial. This matches the companion spec's contract ("captures the bounded streamed assistant text during `scanOutput`").
- Job termination manifests to the runner as context cancellation and/or a non-zero exit from `cmd.Wait()`; both are exercised by tests.
- Persistence and routing of the partial (task-file `## Salvage`, `human_review` escalation) are the companion spec's scope, not this library's.
- The executor's default Job `ActiveDeadlineSeconds` remains 1800s (the companion spec's soft budget sits below it); this library change does not depend on the exact value.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Subprocess SIGKILLed by the K8s Job deadline mid-run (partial-progress crash) | Runner returns the partial captured up to the kill alongside the error — nothing is zeroed | Companion persists the partial to the task file and routes `human_review`; the content survives |
| Run context cancelled mid-stream (executor deadline) | Runner returns the partial captured before cancellation (today it returns empty) and an error naming the termination | None needed — the fix is the intended path |
| Huge streamed output (resource exhaustion) | Partial truncated to the bounded cap keeping the most recent text; earliest bytes dropped | Detection: `## Salvage` shorter than the streamed review (companion); raise the cap as an implementation constant (≥ 16 KiB floor), never an env knob |
| CLI event-schema drift changes how assistant text is streamed (schema drift) | Capture degrades to an empty partial without crashing; result-text, usage, and tail extraction keep their existing behavior (spec 023 shape-agnostic doctrine) | None — best-effort by design; existing paths unaffected |
| External unavailability (auth failure, network, API error) makes the CLI exit non-zero | Run returns the partial (possibly empty) plus the existing tail error naming the failure | Operator diagnoses from the tail error as today; partial available if the CLI streamed anything first |
| Rate limiting: claude API retry storm eats the whole budget | Run is terminated by the deadline; partial surfaced instead of nothing | Companion routes `human_review` with the partial; the bounded failure replaces a blank escalation |
| Clock skew | Not applicable — the runner performs no wall-clock comparisons; termination is driven by the context lifecycle and process exit, both monotonic | — |

## Security / Abuse Cases

- The partial is LLM-streamed text, shaped indirectly by the task content an attacker could influence (the prompt). It is returned to the caller (the agent process, same trust domain as today's result text) and, in the companion, persisted as plain markdown under a controlled heading — never executed or interpreted. No new trust boundary.
- Memory-exhaustion DoS is bounded: the partial cannot exceed the package cap (~16 KiB+), regardless of how much the CLI streams.
- No new exec, env allowlist change, file I/O, or network call. The capture reads only the existing stream-json stdout pipe.
- Secret leakage: if the CLI were to stream a secret into its text output, the partial would carry it back to the caller — the same exposure that already exists via pod logs and the result text; the partial adds no new exposure beyond the stream already observed.
- The success path and error path are unchanged for callers that do not read `Partial`; there is no way for the new field to alter result routing.

## Suggested Decomposition

Single code layer (`claude` package) — split into two prompts plus an operator release step.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Runner capture: accumulate bounded streamed assistant text during the scan; on any non-success termination (ctx cancel, non-zero exit, missing result) return it on `ClaudeResult.Partial` alongside the error; remove the zeroing run-cancellation branch; add the kill-path + bounded-capture + envelope-negative Ginkgo specs | 1, 2, 3, 4 | 1, 2, 3, 4 | — |
| 2 | Compatibility + hygiene: confirm all existing callers compile, success path unchanged, `make generate` clean (mock untouched); add the `## Unreleased` CHANGELOG bullet; verify scope containment | 5 | 5, 6, 7 | prompt 1 |
| 3 (operator, not a prompt) | Release: rename `## Unreleased` → `## v0.82.0`, cut paired `vX.Y.Z` + `lib/vX.Y.Z` tags at the merge commit, push | — | — | prompts 1, 2 (after merge) |

Rationale: prompt 1 is the whole mechanism and all its tests. Prompt 2 is verification and hygiene on top — it cannot pass until the mechanism exists. The release is an operator step gated on the merged PR and is the seam the companion repo (`github-pr-review-agent`) blocks on for its dependency bump; it is intentionally not a prompt because it requires host git access and tag approval.

## Do-Nothing Option

Doing nothing keeps the current state: large multi-file PRs with a deep investigation phase burn the Job deadline, the streamed partial review is discarded, and the task escalates to `human_review` with a blank `needs_input` and zero findings preserved — exactly the `Seibert-Data/moss` PR #1 outcome (two failed reviews, all 7 concerns missed). The companion salvage feature cannot land at all, because the caller has no partial to persist. Without this library change, every large PR review remains a coin flip that can consume two full job runs and still produce nothing reviewable.

## Verification Result

**Verified:** 2026-08-22T15:22:28Z (HEAD b1fb201)
**Binary:** /Users/bborbe/Documents/workspaces/go/bin/dark-factory (installed)
**Scenario:** No scenario (spec declares NO new scenario) — shim-based unit-test verification. One-shot `dark-factory run` swept prompted→verifying; `go test ./claude/...` (37.6s) + `make precommit` (exit 0) + targeted greps run fresh on merged HEAD.
**Evidence:**
- `go test ./claude/...` ok (37.592s): kill-path, context-cancellation, envelope-negative, bounded-capture, error-context-preserved Ginkgo specs all green
- `make precommit` exit 0 (gofmt/generate/test-race+cover/golangci/vet/vuln/trivy/addlicense); `make generate` exit 0; `mocks/claude-claude-runner.go` byte-identical (zero diff)
- `grep -n 'Partial' claude/claude-result.go` → line 12 `Partial string \`json:"partial,omitempty"\``; cap `partialMaxBytes = 16384` (claude-runner.go:32)
- `grep -n -i 'partial output' CHANGELOG.md` → line 17 under `## v0.82.0`; fold guard at merge showed only `+## Unreleased`
- PR bborbe/agent#48 merged (fce6e63), `v0.82.0` tag on release commit 35a8aae; companion `github-pr-review-agent/go.mod` already on `github.com/bborbe/agent v0.82.0`
**Verdict:** PASS
