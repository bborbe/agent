---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-05-12T23:38:41Z"
generating: "2026-05-12T23:38:41Z"
prompted: "2026-05-12T23:45:13Z"
verifying: "2026-05-13T07:00:55Z"
branch: dark-factory/surface-claude-cli-failure-reason-in-task-body
---

## Summary

- The 2026-05-12 dev incident on PR `bborbe/trading#122` confirmed `lib/v0.61.0` half-fixed the operator-readable failure story: the `## Failure` body section is now present, but its message is literally `claude CLI failed: : exit status 1` — content-free.
- Root cause is in the agent lib's claude runner: when `claude --output-format stream-json` exits non-zero, the runner wraps `cmd.Stderr` (empty in stream-json mode) and discards every non-`result` event from stdout, including the auth-failure, rate-limit, or runtime-error events that explain why claude died.
- This spec closes the gap. The runner must surface a short, bounded tail of the actual stdout the CLI emitted when it failed, so operators can diagnose without racing the agent pod's TTL cleanup window for `kubectl logs`.
- Approach is intentionally shape-agnostic: capture the last few raw lines via a small ring buffer and include them verbatim in the error message. No parsing of Claude's event schema (which is not API-stable) and no typed error classification.
- Scope is narrow: change in `lib/claude` only, plus tests, plus a CHANGELOG bullet. The release tag bump and the downstream `maintainer/agent/pr-reviewer` go.mod consumer bump are explicitly excluded from this spec.

## Problem

When a `claude --print --output-format stream-json` subprocess exits non-zero, the agent currently records the failure with the literal message `claude CLI failed: : exit status 1`. The double colon and empty middle field are not a quirk — they are the symptom: the runner wraps `cmd.Stderr`, but stream-json mode writes everything to stdout, so stderr is always empty on failure. Meanwhile, the runner's stdout scanner retains only events of type `result` and silently drops everything else, including the auth-failure event, the rate-limit event, and any runtime error event the CLI emitted on its way out.

The Definition of Done from the original "pr-reviewer agent writes failure body on any agent-failure path" task required the body section to contain an **operator-readable explanation**. `exit status 1` does not clear that bar. The Failure section is present (good — `lib/v0.61.0` shipped that), but operators still have to find and pull the agent pod's logs within the Kubernetes TTL window to learn whether claude died from an expired OAuth token, a 401, a rate limit, or something else entirely. By the time a failure is investigated, those logs are usually gone.

## Goal

After this spec, when the claude CLI subprocess exits non-zero, the error returned by the runner contains a bounded tail of the actual stdout lines the CLI emitted, joined into a single readable string. Operators inspecting the task page's `## Failure` body section see a message that includes the diagnostic information the CLI itself printed — auth failure events, API error events, rate-limit events — without depending on pod logs that may already be gone. The fix is forward-compatible with future CLI versions because it captures raw lines, not parsed event types.

## Non-goals

- No typed parsing of Claude CLI error events. Patterns like `{"type":"result","is_error":true,...}`, `subtype: error_during_execution`, and other event-schema details are intentionally NOT introspected. The CLI's stream-json event schema is not API-stable; building on it would create silent breakage on every CLI upgrade. Tail-the-raw-lines is the contract.
- No change to result-event handling. Successful runs still extract `resultText` from the `result` event as today; the only change to the success path is that the captured tail is allocated and then discarded when `cmd.Wait()` returns nil.
- No release. This spec lands a code + test + CHANGELOG-bullet change on a feature branch through the dark-factory pipeline. Cutting `vX.Y.Z` / `lib/vX.Y.Z` tags and bumping `maintainer/agent/pr-reviewer`'s go.mod are separate, sibling pieces of work and are NOT in scope here.
- No downstream consumer update. `task/controller`, `task/executor`, `agent/claude/cmd`, and any other in-repo consumer of `lib/claude.ClaudeRunner` keeps the same call site. The runner's signature does not change.
- No re-architecture of the runner's stdout scanner beyond what is needed to keep the tail. The scanner stays a single linear pass over stream-json lines; no new goroutine, no new channel.
- No structured/JSON failure payload on the error type. The tail is plain text glued into the error message.

## Desired Behavior

1. When the claude CLI subprocess exits non-zero, the error returned by `Run` carries the tail of the non-empty stdout lines the CLI emitted before exit, presented as a single operator-readable string. The `## Failure` body section on the resulting task page surfaces that string in its `Reason` field — no more `claude CLI failed: : exit status 1`.
2. When the CLI exits non-zero having emitted no non-empty stdout lines (e.g. early SIGKILL), the failure message conveys that condition explicitly rather than rendering an empty middle field. Operators can distinguish "CLI failed with no output" from "CLI failed and we couldn't capture output".
3. On a successful run, the captured tail is discarded and observable behavior is unchanged from today. The success path neither exposes the tail on `ClaudeResult` nor changes its existing extraction of the result-event text.
4. The failure-message construction does not depend on `cmd.Stderr`. Stream-json mode writes nothing to stderr, so the previous `cmd.Stderr` capture contributed only the empty middle field of the `claude CLI failed: : exit status 1` rendering; that input is no longer part of the failure-message path. Existing per-line debug logging at high verbosity is preserved as-is.

## Constraints

- The change is confined to `lib/claude`. Other modules (`task/*`, `agent/*`, `prompt/*`) MUST NOT be touched. Their go.mod files are not bumped here.
- `ClaudeRunner` interface (`Run(ctx, prompt) (*ClaudeResult, error)`) is frozen. No new method, no new field on `ClaudeResult`, no new arg on `Run`. The failure detail rides on the error returned, not on a new return value or struct field.
- `ClaudeResult` shape stays untouched. Adding a `FailureTail` field would imply consumers should inspect it, which contradicts the "error is the carrier" design.
- The `--output-format stream-json` argument and the surrounding `--print --verbose --strict-mcp-config` flag set are NOT changed. The fix is downstream of the CLI invocation, not in how the CLI is configured.
- Existing tests in `lib/claude` (Ginkgo suite at `claude_suite_test.go`, runner-adjacent tests like `task-runner_test.go`) must remain green. New tests are added; existing ones are not deleted or weakened.
- Ring buffer parameters are constants of the package: **max lines = 5**, **max bytes per line = 512**, **joiner = ` | ` (space-pipe-space)**. They are not configurable via `ClaudeRunnerConfig`. Reason: failure-message budget is a property of how operators read logs, not a per-deployment knob.
- The capture is shape-agnostic. The implementation MUST NOT branch on Claude event-schema fields (e.g. event type, is_error flag, subtype) when deciding what to retain in the ring buffer. Every non-empty stdout line is eligible. Result-text extraction continues to use its existing event-type filter; the ring buffer is parallel to it, not nested inside its switch.
- Tests run on a fake `claude` binary, not the real CLI. The pattern is a shell-script shim placed in `t.TempDir()` and prepended to `PATH` in the spawned subprocess's env. The shim emits canned stream-json lines on stdout, then `exit 1`. No network, no real claude install.
- Test conventions follow the repo defaults: Ginkgo v2 + Gomega, external test package (`claude_test`), counterfeiter mocks if needed (none expected for this change).
- The bump under `## Unreleased` (or the next-version header if no `## Unreleased` block exists at the time the change lands) in the single root `CHANGELOG.md` is required. No per-module CHANGELOG.
- Project tag policy from `CLAUDE.md` still applies if and when a release is cut from the merge: paired `vX.Y.Z` + `lib/vX.Y.Z` tags at the same commit, matching the latest CHANGELOG header. Cutting that release is NOT in scope of this spec.

## Assumptions

- The 2026-05-12 incident reproduces deterministically by replaying the `PR Review github - bborbe-go-skeleton - 10 - test-human-readable-filename-rung-2-verification.md` task with frontmatter reset to `phase: planning`, `status: in_progress`, `trigger_count: 0`, while the dev cluster's OAuth credentials remain broken. The OAuth-broken state is the natural source of a non-zero CLI exit on dev today.
- The CLI emits its diagnostic events (auth failure, API error, rate limit) on stdout in stream-json mode shortly before exiting non-zero. The last 5 lines are sufficient context, based on inspection of the live pr-reviewer pod stdout during the v0.61.0 verification. The `~2.5KB` upper bound on the entire tail is comfortable for log aggregators and for the YAML body the task page writes.
- The runner is the single chokepoint. There is no other place in `lib/claude` that constructs the wrapping error for a non-zero `cmd.Wait()` from the claude CLI, so this change does not need to propagate to sibling files.
- `task-flow-and-failure-semantics.md` (the failure-semantics doc) already documents that `failed` is for infra failures and that the agent body must carry an operator-readable explanation. This spec strengthens implementation against that contract; it does NOT change the contract itself.
- The existing failure-body content generator (shipped in `lib/v0.61.0`) consumes the error message on the task page. That code is unchanged here.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| CLI exits non-zero with diagnostic events on stdout (OAuth fail, 401, rate limit) | Error message contains the joined tail of those events; `## Failure` body section on task page is operator-readable | None — this is the happy path of the fix |
| CLI exits non-zero with zero stdout lines emitted (early SIGKILL, OOM before first line) | Error message states `no stdout captured` explicitly; no double-colon empty rendering | Operator falls back to pod logs / Job events; the empty case is correctly signalled |
| CLI emits very long single line (e.g. multi-MB stack trace) | Line is truncated at 512 bytes before insertion into ring buffer; total tail stays ≤ ~2.5KB | None — bounded by design |
| CLI emits >5 useful lines and the relevant one is early | Earlier line is dropped from ring buffer; tail contains only the most recent 5 | Operator reads pod logs for full context; acceptable trade-off vs unbounded memory |
| CLI succeeds (`cmd.Wait()` returns nil) but emitted noise on stdout | Tail is discarded; `ClaudeResult` carries only `resultText`; no leakage | None |
| Context cancellation mid-stream | Existing behavior preserved: `scanOutput` returns early on `ctx.Done()`; whatever was captured to that point is still surfaced if `cmd.Wait()` then returns non-zero | None |
| Future CLI version changes its event schema | Tail capture is unaffected — it does not parse events. Result extraction may break if Claude renames `type: result`, but that risk is unchanged by this spec | Out of scope; tracked elsewhere |

## Security / Abuse Cases

- The captured tail is content the claude CLI subprocess wrote to stdout. That stdout is shaped by the prompt the executor passed in, plus the CLI's own internal events. An attacker who controls the task content could influence the prompt and thus indirectly nudge what Claude prints; however, the failure path is reached only when the CLI itself exits non-zero, and the captured tail is the CLI's diagnostic output, not Claude-the-model's free-form generation. The blast radius is "the failure message on a task page contains some attacker-influenced bytes" — bounded by the 5×512-byte cap, written into a markdown body section that is rendered by Obsidian.
- Path traversal / shell injection: not applicable. The tail flows into an `errors.Wrapf` format string as a `%s` argument, not into a shell command.
- DoS via unbounded log: defended by the 5-line × 512-byte cap. The total maximum size of the captured tail is ~2.5KB regardless of how much the CLI prints.
- Secret leakage: the OAuth token or API key is in the env passed to the CLI; if the CLI itself were to print that token to stdout, the tail would surface it on the task page. This is a CLI-side hygiene issue (already a risk today via pod logs). The fix does not increase exposure relative to the pod-logs status quo, and the new exposure is gated by `cmd.Wait()` returning non-zero. Tasks live in a private vault. Acceptable.
- No new network calls, no new file I/O, no new exec calls. The change is in-memory string accumulation.

## Acceptance Criteria

- [ ] When `claude --print --output-format stream-json …` exits non-zero after emitting one or more non-empty stdout lines, the error returned by `Run` contains those lines (or the most recent 5 of them) joined with ` | `.
- [ ] The error string does not contain the substring `: :` (the double-colon symptom of the empty-middle-field rendering).
- [ ] When the CLI exits non-zero having emitted **zero** non-empty stdout lines, the error string contains a phrase signalling absent output (e.g. "no stdout captured" or equivalent), and does NOT contain `: :`.
- [ ] Each retained stdout line is capped at 512 bytes; the ring buffer holds at most 5 lines. Both bounds are exercised by tests that push more lines and longer lines than the cap.
- [ ] On a successful run (`cmd.Wait()` returns nil), `ClaudeResult` is unchanged from current behavior — `Result` holds the extracted result-event text, and no tail data leaks into it.
- [ ] `cmd.Stderr` capture is no longer used to construct the failure message. The failure-message path does not read from any stderr buffer.
- [ ] New Ginkgo specs in `lib/claude` cover, at minimum:
  - [ ] non-zero exit with diagnostic-shaped stream-json line → error string contains the canned diagnostic substring the shim emitted (verbatim, not paraphrased)
  - [ ] non-zero exit with zero stdout lines → explicit "no stdout captured" message and no `: :`
  - [ ] >5 emitted lines → only most-recent 5 retained
  - [ ] >512-byte line → truncated (assert message length is bounded)
  - [ ] successful exit → no tail leakage onto `ClaudeResult`
- [ ] The tests do not require a real `claude` binary on PATH. The test harness builds a shell-script shim in `t.TempDir()`, prepends that dir to `PATH` in the spawned process env, and asserts on the resulting error.
- [ ] `CHANGELOG.md` carries a bullet describing the change under `## Unreleased` (or the next version's header if `## Unreleased` is absent at merge time).
- [ ] `cd lib && make precommit` is clean. No drift after `make generate`. Lint, license, format, generate, test all green.
- [ ] No file outside `lib/claude/` and `CHANGELOG.md` is modified by the change.

Scenario coverage — **NO new scenario.** The behavior is fully reachable via unit tests against the runner using a shell-script CLI shim. Existing scenarios (`scenarios/001-…003-result-writeback-*.md`, `use-git-rest-for-vault-writes.md`) cover the result-writeback / vault-write paths the failure body flows through; nothing about the runner-side capture benefits from an E2E rung. The dev-replay verification is operator-run after release, not codified as a scenario test (rationale: it depends on a deliberately-broken OAuth state in the dev cluster, which is not reproducible from CI).

## Verification

```
cd lib
make precommit
```

`make precommit` already runs `go test`, lint, license, format, and generate-drift checks. The iteration-loop convenience of running `go test ./claude/...` directly is available but not part of acceptance.

Negative-path manual check (after a future release that includes this change is rolled to dev):

```
# Reset the dev verification task to re-trigger pr-reviewer with broken OAuth
# Vault path: PR Review github - bborbe-go-skeleton - 10 - test-human-readable-filename-rung-2-verification.md
# Set frontmatter: phase: planning, status: in_progress, trigger_count: 0
# Commit + push → controller spawns pr-reviewer Job → Job fails on OAuth
# Verify: task page ## Failure body Reason field now contains the real CLI error text
#         (e.g. an auth-failure event from stdout), not "claude CLI failed: : exit status 1"
```

The manual check is gated on a release tag bump + the maintainer go.mod bump, both of which are out of scope for this spec. The acceptance criteria above stand entirely on the `make precommit` + unit-test rung.

## Do-Nothing Option

Ship nothing further. `lib/v0.61.0` already added the `## Failure` body section, so the symptom is "the section is there but the message is content-free." Operators continue to race the agent pod's TTL cleanup window (default ~30 minutes on dev) to grab `kubectl logs <pod>` whenever pr-reviewer or any other claude-based agent fails. For low-frequency failures this is annoying but survivable. For a recurring failure mode (OAuth expiration, sustained rate-limit, image-registry hiccup), operators waste cycles on every incident chasing logs that the failure section should already contain. The original task's Definition of Done explicitly required an operator-readable explanation; leaving the message at `exit status 1` ships a half-fix and treats the DoD as advisory rather than binding.

The cost of fixing is small (one file in `lib/claude`, table-driven unit tests, one CHANGELOG bullet). The cost of not fixing accumulates on every failure investigation and is borne by whichever operator is paged.
