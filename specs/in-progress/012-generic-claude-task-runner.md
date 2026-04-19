---
status: prompted
approved: "2026-04-19T17:44:05Z"
generating: "2026-04-19T17:44:05Z"
prompted: "2026-04-19T17:49:29Z"
branch: dark-factory/generic-claude-task-runner
---

## Summary

- Make `lib/claude.TaskRunner` generic over the result type `T`
- Delete `trade-analysis/pkg/task-runner.go` — a near-copy that silently missed spec 010's parser tolerance fix
- Every agent consumes the same single task-runner implementation; prose-stripping + Claude CLI wiring + deliver pattern live in one place
- Eliminates the root cause behind the smoke-test failure: bug fixes applied to `lib/claude/task-runner.go` did not propagate to trade-analysis because trade-analysis never called it

## Problem

Today `lib/claude/task-runner.go` and `trade-analysis/pkg/task-runner.go` are ~95% identical: build prompt, run Claude CLI, unmarshal JSON result, deliver. They diverge only in their `AgentResult` type — trade-analysis adds `Analyzed`, `Skipped`, `Total` fields.

The duplication is a bug factory:

- Spec 010 added `extractLastJSONObject` prose-stripping to `lib/claude/task-runner.go`. trade-analysis's copy was not updated and still does naive `json.Unmarshal(result.Result)`.
- Bumping `lib/v0.37.0` into trade-analysis's `go.mod` does nothing because trade-analysis never calls `lib/claude.NewTaskRunner` — it calls its own `pkg.NewTaskRunner`.
- Observed: smoke-test task `94884aa4` keeps failing with `parse claude result failed: <prose prefix>\n\n{valid JSON}` across v0.43.2 + lib v0.37.0 + trade-analysis rebuild. The error format matches `trade-analysis/pkg/task-runner.go:91`, not lib's format strings. The spec 010 code is in the binary; it is just never executed.

Every future agent we write will start by copying `trade-analysis/pkg/task-runner.go` and re-introducing the same parser gap. The only way to kill the pattern is to delete the duplicate.

## Goal

One task-runner for every agent that uses Claude CLI. Agent-specific result types plug in through Go generics. Spec 010's parser tolerance applies to every agent automatically. Adding a new agent never involves copying task-runner logic.

## Non-goals

- Not changing the Claude CLI invocation itself (`ClaudeRunner.Run`)
- Not changing the `AgentResult` JSON wire format
- Not merging `trade-analysis`'s extra fields (`Analyzed/Skipped/Total`) into `lib/claude.AgentResult` — those are trade-analysis-specific
- Not changing the Result-section markdown that trade-analysis writes today (custom table rendering with `Analyzed/Skipped/Total` must survive the migration, byte-identical)
- Not changing `delivery.AgentResultInfo` — the delivery channel stays identical
- Not touching the agent-prompt / agent-task Kafka topics

## Desired Behavior

1. `lib/claude.TaskRunner` becomes `TaskRunner[T AgentResultLike]`, where `AgentResultLike` is the interface below.
2. `lib/claude.AgentResultLike` interface:
   - `GetStatus() AgentStatus`
   - `GetMessage() string`
   - `GetFiles() []string`
   - `RenderResultSection() string` — returns the markdown `## Result\n\n…` block for this result. **Each concrete type renders itself**, so agents with extra fields (trade-analysis's `Analyzed/Skipped/Total`) keep their custom table output.
3. `lib/claude.ResultDeliverer` becomes `ResultDeliverer[T AgentResultLike]`. The adapter that wraps `delivery.ResultDeliverer` calls `result.RenderResultSection()` to build the `Output` field of `AgentResultInfo`.
4. `lib/claude.BuildResultSection` stays as a helper function that renders the default layout (`Status/Message/Files`). `lib/claude.AgentResult.RenderResultSection()` calls it. Other types may call it too, then append their own lines.
5. `lib/claude.AgentResult` implements `AgentResultLike` (pointer-receiver methods, including `RenderResultSection` that delegates to `BuildResultSection`). It remains the default for simple agents.
6. `trade-analysis/pkg.AgentResult` implements `AgentResultLike`. Its `RenderResultSection()` produces the **exact same markdown** as today's `trade-analysis/pkg/result-deliverer.go BuildResultSection` (the `Analyzed/Skipped/Total/Files` table). Golden-file test guards byte equality.
7. `trade-analysis/pkg/task-runner.go` + `task-runner_test.go` + `mocks/pkg-task-runner.go` are **deleted**.
8. `trade-analysis/pkg/factory/factory.go` is the real call site: switch from the local `pkg.NewTaskRunner` to `claude.NewTaskRunner[pkg.AgentResult]`, reusing the same env-context keys today's `BuildPrompt` call expects.
9. Existing consumer `agent/claude/main.go` wires `claude.NewTaskRunner[claude.AgentResult](...)` — same runtime behavior as today.
10. `extractLastJSONObject` remains the single parser path. Stays unexported inside `lib/claude`.

## Assumptions

- Project's Go toolchain is ≥ 1.18 (generics).
- Counterfeiter ≥ v6.8 is available (generic-interface mock generation). If the pinned version is older, upgrade it as part of this spec — no hand-written fakes.
- `lib/claude.TaskRunner` has no out-of-tree consumers beyond `agent/claude` and `trade-analysis` at the time this spec runs. If a new consumer has appeared, add it to the migration list.
- `trade-analysis` repo and `agent` repo can be released in close succession (lib tag → trade-analysis `go.mod` bump within the same merge window).

## Constraints

- `lib/claude.BuildResultSection` accesses `result.Status/Message/Files` today — migrate it behind the `AgentResultLike` interface so it stays generic-free and callable from anywhere.
- Kafka / delivery wire format stays identical: the adapter still hands `delivery.AgentResultInfo{Status, Output, Message}` to the inner deliverer. The generic parameter never reaches Kafka.
- `lib/` is a separate Go module with its own tags — any change here requires a new `lib/vX.Y.Z` tag before trade-analysis can consume it.
- `trade-analysis/pkg.FileResultDeliverer` and `tradeAnalysisContentGenerator` stay as structs but drop the local `pkg.ResultDeliverer` interface — they satisfy `claude.ResultDeliverer[pkg.AgentResult]` directly. `pkg.ResultDeliverer` interface is deleted.
- `trade-analysis/pkg/factory.CreateKafkaResultDeliverer` signature is internal to trade-analysis — only the return type changes (`pkg.ResultDeliverer` → `claude.ResultDeliverer[pkg.AgentResult]`). No new parameters.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Downstream agent forgets to implement `AgentResultLike` | Compile error at the `NewTaskRunner[T]` call site | Compile fails loudly — fix is add four method stubs |
| Counterfeiter pinned version doesn't support generic interfaces | Upgrade counterfeiter to ≥ v6.8 in `tools.go`, re-run `go generate` | No hand-written fakes — they drift |
| trade-analysis Result-section rendering regresses (lost fields) | Golden-file unit test on `pkg.AgentResult.RenderResultSection()` fails CI | Restore custom render code before merging |
| JSON result contains no `{…}` at all | `extractLastJSONObject` returns `ok=false`; runner emits `AgentStatusFailed` with `parse claude result failed (no JSON object found): <raw>` | Same as today in `lib/claude` — trade-analysis now inherits correct message |
| JSON blob parses but is missing `status` field | `json.Unmarshal` succeeds with zero-value `Status`; deliver step still runs; controller sees empty status | Caller's responsibility to treat empty status as failed; outside this spec |
| trade-analysis bumped to new `lib` but agent image not rebuilt | Smoke test still fails with old error format | Rebuild + redeploy (documented in Verification) |

## Do-Nothing Option

Cost of keeping the duplicated task-runner:

- Every `lib/claude/task-runner.go` fix needs a matching copy-paste into `trade-analysis/pkg/task-runner.go` and any future agent's equivalent file. Enforcement is manual — the smoke test failure proves it breaks.
- New agents (swing-scanner, strategy-auditor, etc.) will copy the broken-by-default pattern.
- The "correct" way to fix the current parse error becomes "export `ExtractLastJSONObject`" — which hides the defect instead of fixing it.

Cheap alternative considered: export `ExtractLastJSONObject` and call it in trade-analysis. Rejected — unblocks the smoke test but leaves the duplication and the pattern. Every new agent author must remember to call the helper; the language does not enforce it.

## Security / Abuse

No new attack surface. Generics are a compile-time transformation; the runtime behavior of `lib/claude.TaskRunner` is byte-identical for the non-generic default (`T = claude.AgentResult`).

## Acceptance Criteria

**lib/claude changes**

- [ ] `lib/claude.AgentResultLike` interface defined with `GetStatus()`, `GetMessage()`, `GetFiles()`, `RenderResultSection() string`
- [ ] `lib/claude.TaskRunner` is declared `TaskRunner[T AgentResultLike]`
- [ ] `lib/claude.ResultDeliverer` is declared `ResultDeliverer[T AgentResultLike]`; the `NewResultDelivererAdapter` uses `result.RenderResultSection()` for `AgentResultInfo.Output`
- [ ] `lib/claude.AgentResult` implements `AgentResultLike`; its `RenderResultSection()` delegates to `BuildResultSection`
- [ ] `lib/claude.BuildResultSection` accepts `*AgentResult` (or `AgentResultLike` via the three accessors) and renders `Status/Message/Files` — behavior identical to today
- [ ] `lib/claude/task-runner_test.go` instantiates `TaskRunner[claude.AgentResult]`; existing prose-prefix/suffix/nested-braces parser tests still pass
- [ ] `lib/claude/result-deliverer_test.go` updated to new generic signature
- [ ] `lib/mocks/claude-task-runner.go` and `lib/mocks/claude-result-deliverer.go` regenerated via counterfeiter for generic interfaces; upgrade counterfeiter to ≥ v6.8 in `lib/tools.go` if needed
- [ ] `lib/CHANGELOG.md` `## Unreleased` entry: "claude: generic TaskRunner[T AgentResultLike], adds RenderResultSection method for per-type result markdown"
- [ ] `lib/` submodule tagged `lib/vX.Y.Z` after merge (required before trade-analysis consumes it)

**agent/claude changes**

- [ ] `agent/claude/pkg/factory/factory.go` updated to return `claude.TaskRunner[claude.AgentResult]` and `claude.ResultDeliverer[claude.AgentResult]`
- [ ] `agent/claude/main.go` compiles; runtime behavior identical
- [ ] Root `agent/CHANGELOG.md` `## Unreleased` entry: "agent/claude: wire generic claude.TaskRunner[claude.AgentResult]"

**trade-analysis changes** (separate repo: `~/Documents/workspaces/trading/agent/trade-analysis`)

- [ ] `pkg/task-runner.go`, `pkg/task-runner_test.go`, `mocks/pkg-task-runner.go` are **deleted**
- [ ] `pkg/types.go` — `AgentResult` implements `AgentResultLike` including `RenderResultSection()` that renders the **exact same markdown** (byte-for-byte) as today's `pkg/result-deliverer.go BuildResultSection` for `Analyzed/Skipped/Total/Files`. Golden-file test added under `pkg/`.
- [ ] `pkg/result-deliverer.go` — local `pkg.ResultDeliverer` interface deleted; concrete structs (`FileResultDeliverer`, `tradeAnalysisContentGenerator`) implement `claude.ResultDeliverer[pkg.AgentResult]`. Local `BuildResultSection` function deleted (logic moved into `AgentResult.RenderResultSection`)
- [ ] `pkg/factory/factory.go` — real call site — replace `pkg.NewTaskRunner(runner, instructions, branch, stage, gitRestURL, deliverer)` with `claude.NewTaskRunner[pkg.AgentResult](runner, instructions, envContext, deliverer)` where `envContext = map[string]string{"Branch": branch.String(), "Stage": stage.String(), "GIT_REST_URL": gitRestURL.String()}`
- [ ] `pkg/factory/factory.go` — `CreateKafkaResultDeliverer` return type updated to `claude.ResultDeliverer[pkg.AgentResult]`
- [ ] `main.go` compiles; no other signature changes required
- [ ] `go.mod` bumped to the new `lib/vX.Y.Z` tag; `go mod tidy`
- [ ] `make precommit` passes
- [ ] `CHANGELOG.md` `## Unreleased` entry: "trade-analysis: migrate to lib/claude generic TaskRunner, delete duplicated parser"

**End-to-end verification** (post-deploy)

- [ ] Smoke-test task `94884aa4-…` on dev: runs exactly one K8s Job, completes `status: done`, Result section contains `Analyzed/Skipped/Total` table identical in layout to pre-migration output
- [ ] No `parse claude result failed` error with trade-analysis's old format string anywhere in the task's Result history
- [ ] At least one verification run during sign-off uses a non-empty trade window and completes with `analyzed > 0` (zero-trade `analyzed = 0` alone does NOT satisfy this criterion — the parse path must be exercised on real data)
- [ ] Spec 010 (`specs/in-progress/010-failure-vs-needs-input-semantics.md`) is re-verified together with this spec; both marked `spec complete` in the same sign-off window

## Verification

```bash
cd lib && make precommit
cd agent/claude && make precommit
# trade-analysis lives in the trading repo
cd ~/Documents/workspaces/trading/agent/trade-analysis && make precommit
```

Release + deploy sequence:

1. Merge `lib` changes on master, tag `lib/vX.Y.Z`
2. In trade-analysis: `go get github.com/bborbe/agent/lib@vX.Y.Z && go mod tidy`
3. `BRANCH=dev make buca` in `agent/trade-analysis`
4. Reset smoke-test task frontmatter: `retry_count: 0`, `phase: in_progress`, clear `current_job` / `job_started_at`
5. Wait one executor cycle; verify:
   - exactly one new K8s Job spawns for the task id
   - job completes `status=done`
   - task file Result section contains parsed structured data (Analyzed/Skipped/Total), no parse-error prose
6. `kubectlquant -n dev get jobs -l agent.benjamin-borbe.de/task-id=94884aa4-4c6c-4985-9416-4dbb0e347560` — exactly one job, `Completions 1/1`

## References

- `specs/in-progress/010-failure-vs-needs-input-semantics.md` — added `extractLastJSONObject` to `lib/claude/task-runner.go`; currently `verifying` but its smoke-test acceptance criteria cannot pass until this spec lands (trade-analysis runs its own parser). Verify 010 together with this spec, then `spec complete` both.
- `lib/claude/task-runner.go` — generic target
- `trade-analysis/pkg/task-runner.go` — to be deleted
- `trade-analysis/pkg/types.go` — `AgentResult` with extra fields, must implement `AgentResultLike`
- `agent/claude/main.go` — existing consumer, unchanged behavior
- Smoke-test task: `~/Documents/Obsidian/OpenClaw/tasks/smoke-test-trade-analysis-2026-04-19e.md`
