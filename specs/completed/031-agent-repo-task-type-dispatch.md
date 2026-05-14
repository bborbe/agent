---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-05-14T12:28:19Z"
generating: "2026-05-14T12:28:19Z"
prompted: "2026-05-14T12:39:03Z"
verifying: "2026-05-14T12:49:02Z"
completed: "2026-05-14T14:09:24Z"
branch: dark-factory/agent-repo-task-type-dispatch
---

## Summary

- Spec 030 made every spawned Job carry a real `TASK_TYPE` env reflecting the task's frontmatter `task_type` field. The three agent-repo binaries (`agent-claude`, `agent-gemini`, `agent-code`) read that env into `application.TaskType` (spec 029) and use it only as a Prometheus grouping label — they do NOT branch on it. Every Job still runs the binary's single hardcoded domain agent regardless of what `task_type` says.
- This spec adds **per-task-type dispatch** in all three agent-repo binaries. Each binary selects which `agentlib.Agent` to construct based on the task's declared `task_type`, returning a clear error when an unsupported value arrives.
- A new shared package, `lib/healthcheck/`, supplies the liveness handler that every dispatch table maps `healthcheck` to. It exposes three step flavors (Claude-CLI, Gemini-API, Nop) and a thin `NewAgent` wrapper that boxes any step into a one-phase `*agentlib.Agent`. The step that runs is the one appropriate to the binary's external dependencies.
- A `lib.TaskTypeHealthcheck` constant joins the existing well-known task types in `lib/agent_task-type.go`. The existing `TaskTypeOAuthProbe` GoDoc drops its "once introduced" qualifier and points at the new constant.
- Out of scope (separate specs): renaming the `oauth-probe` probe publisher, HTTP route, and cron env in `task/executor`; per-task-type dispatch in trading-repo and maintainer-repo binaries.

## Referenced Specs

- **Spec 029** — declared the `TaskType` field on each binary's application struct (default `"unknown"`). This spec is the first consumer that branches on it.
- **Spec 030** — established `lib.TaskType` (named type, validation, well-known constants), `TaskFrontmatter.TaskType()` accessor, and `TASK_TYPE` env injection from the executor's `buildJobEnvBuilder`. This spec adds the `TaskTypeHealthcheck` constant that 030 left as a `// Deprecated: use TaskTypeHealthcheck once introduced` placeholder.

## Problem

`TASK_TYPE` reaches every agent container correctly, but no binary acts on it. The `agent-claude` Config CR already declares `taskTypes: [claude, oauth-probe, healthcheck]` — meaning the executor will route `healthcheck` tasks to `agent-claude` — yet `agent-claude/main.go` constructs the same 3-phase Claude prompt agent regardless. Operators have no way to ship a fast liveness check, a smoke test, or any second behavior into an existing binary without forking it.

Worse, the agent framework is set up to support exactly this: `agentlib.NewAgent(...phases)` accepts any composition. The wiring gap is purely in `factory.CreateAgent` and the single call site in each `main.go`. Closing it once, in this repo, also fixes the pattern that will be copy-pasted into trading and maintainer agent binaries.

## Goal

After this spec ships, every agent-repo binary chooses its `agentlib.Agent` from a small switch keyed on `lib.TaskType`. Each binary supports exactly two values: its domain task type and `healthcheck`. Any other value produces a wrapped error at construction time (before any Step runs), surfaced as a `Status: failed` task result with a message naming the accepted types.

A shared `lib/healthcheck/` package provides the liveness step flavors and the one-line agent wrapper that all three binaries (and future agent binaries) consume. The package is the single source of truth for what "the binary is healthy" means; each binary picks the flavor whose external dependency matches its own.

## Dependencies

- **Spec 030 (executor inject TASK_TYPE env) has merged (v0.62.7).** The `lib.TaskType` named type, `TaskFrontmatter.TaskType()` accessor, and `TASK_TYPE` env injection are all in place.

## Non-goals

- **No rename of `oauth-probe` in the probe pipeline.** `task/executor/pkg/probe/probe.go` keeps publishing tasks with literal `task_type: "oauth-probe"`. Cron env name `OAUTH_PROBE_CRON_EXPRESSION` and HTTP route `/oauth-probe-trigger` are unchanged. The `agent-claude` Config CR already accepts both `oauth-probe` and `healthcheck` in `taskTypes`; switching the publisher to emit `healthcheck` is a follow-up spec after this dispatch lands.
- **No per-task-type dispatch outside this repo.** Trading-repo binaries (`pr-reviewer`, `trade-analysis`, `backtest`, `hypothesis`) and maintainer-repo binaries (`maintainer-agent-pr-reviewer`) get separate specs once the pattern is proven here.
- **No new CRD field, no new env, no new Kafka schema.** Dispatch is driven entirely by the existing `TASK_TYPE` env (already populated by the executor).
- **No change to `agentlib.Agent` shape.** It stays a struct produced by `agentlib.NewAgent(...Phase)`. The healthcheck wrapper composes existing primitives — no new interface, no `agentlib.Agent` becoming an interface.
- **No new task type beyond `healthcheck`.** This spec adds exactly one constant (`TaskTypeHealthcheck`). It does NOT introduce `TaskTypeGemini` or `TaskTypeCode`; see Assumptions below for how the gemini / code binaries identify their domain task type.
- **No deprecation or removal of the existing `CreateAgent` function in each factory.** It stays available (callable by local-CLI `cmd/run-task/main.go`, tests, and the new dispatch function) but no longer drives `main.go` directly. Renaming it (e.g. to `CreateDomainAgent`) is implementation-detail — auditor's call during prompt review.
- **No change to local-CLI entry points** in `agent/{claude,gemini,code}/cmd/run-task/main.go`. They keep calling the existing domain-agent factory directly. Per-task-type dispatch belongs to the Kafka entry point only.
- **No probe / healthcheck output assertions.** The healthcheck step returns `done` when the underlying dependency responds at all; what it says is logged but not validated against an expected string beyond non-emptiness.

## Assumptions

- **Domain task type for `agent-claude`** is `lib.TaskTypeClaude` (already a well-known constant, value `"claude"`).
- **Domain task type for `agent-gemini`** — there is currently no `TaskTypeGemini` constant and **no Config CR for `agent-gemini` in this repo**. The binary today is a reference implementation invoked only by `cmd/run-task/main.go` and tests. **Default**: healthcheck-only binary. The factory switch accepts exactly `TaskTypeHealthcheck`; any other value (including a future literal `"gemini"`) hits the default-error branch. Adding `TaskTypeGemini` is deferred to whenever a Config CR for `agent-gemini` is introduced.
- **Domain task type for `agent-code`** — same situation as gemini. **Default**: healthcheck-only binary. Factory switch accepts exactly `TaskTypeHealthcheck`; any other value hits the default-error branch.
- **The healthcheck step for `agent-gemini` uses `agentlib.AIParser`**, not a hypothetical `geminilib.Runner` — there is no Gemini CLI in this repo. The Gemini flavor calls the existing parser with a minimal "reply 'ok'" prompt and asserts a non-empty parsed result; failure (HTTP error, empty response, parse error) maps to `Status: failed` with the error captured in the body.
- **The healthcheck step for `agent-code` uses `NewNopStep`**. Reaching the step proves the binary booted, envconfig parsed, Kafka client opened, and the framework wired the phase — that is the only liveness signal a pure-Go binary can offer.
- **`oauth-probe` handling in `agent-claude` switch**: **Default**: include `TaskTypeOAuthProbe` in the accepted set, aliased to the same healthcheck-Claude agent (matches the Config CR's current `taskTypes: [claude, oauth-probe, healthcheck]` and prevents probe pipeline regression until the rename spec ships). Switch shape: 3 named cases (`TaskTypeClaude`, `TaskTypeHealthcheck`, `TaskTypeOAuthProbe`) + default-error. The deprecated `TaskTypeOAuthProbe` constant gets a fall-through `case` line; cleanup is the rename spec's concern.
- **`CreateAgent` is retained** (not renamed). `CreateAgentForTaskType` is added as a new function alongside it; the new function calls into the existing `CreateAgent` internally for the domain branch. This keeps `cmd/run-task/main.go` and tests unaffected.
- **Healthcheck phase wrapping**: **Default**: register the step under all three phase names (`planning`, `in_progress`, `ai_review`) — matches the existing `agent-claude` shape, makes the step phase-agnostic without depending on undocumented framework single-phase semantics.

## Desired Behavior

1. A new package `lib/healthcheck/` exists in the agent repo's `lib/` module. It exports exactly the public names listed in items 2–5 below. No other exported types or functions.
2. `healthcheck.NewClaudeStep(runner claudelib.ClaudeRunner) agentlib.Step` returns a Step that runs the configured Claude CLI with the prompt `"reply 'ok'"`. Step name is `"healthcheck-claude"`. On success (runner returns no error and a non-empty result text), the step returns `Status: done` with the trimmed result text as output. On runner error, the step returns `Status: failed` with the wrapped error message as output; the framework propagates this as a normal failed result.
3. `healthcheck.NewGeminiStep(parser agentlib.AIParser) agentlib.Step` returns a Step that calls the AIParser with a minimal "reply 'ok'" prompt parameterized over a tiny `Reply struct { OK string }` (or equivalent shape suitable for the generic parser). Step name is `"healthcheck-gemini"`. Behavior shape matches the Claude step: parser-error → `failed`, non-empty parsed reply → `done`.
4. `healthcheck.NewNopStep() agentlib.Step` returns a Step that immediately returns `Status: done` with output `"ok"`. Step name is `"healthcheck-nop"`. No external calls. Used by pure-Go binaries.
5. `healthcheck.NewAgent(step agentlib.Step) *agentlib.Agent` wraps any Step in a phase-agnostic `*agentlib.Agent`. Per Assumptions, the step is registered under all three phase names (`planning`, `in_progress`, `ai_review`) so the healthcheck task succeeds regardless of which `PHASE` env it was spawned with.
6. A new constant `lib.TaskTypeHealthcheck TaskType = "healthcheck"` exists in `lib/agent_task-type.go` immediately after `TaskTypeOAuthProbe`. Its GoDoc names it as the liveness task type and references this package (`lib/healthcheck`).
7. The GoDoc on `TaskTypeOAuthProbe` is updated from `// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.` to `// Deprecated: use TaskTypeHealthcheck.` — the "once introduced" qualifier is removed.
8. `agent/claude/pkg/factory/factory.go` exposes a new function `CreateAgentForTaskType(taskType lib.TaskType, <same arguments CreateAgent currently takes>) (*agentlib.Agent, error)`. It switches on `taskType`: `TaskTypeClaude` returns the existing domain agent (calls the existing `CreateAgent` internally), `TaskTypeHealthcheck` AND `TaskTypeOAuthProbe` (case fall-through) both return `healthcheck.NewAgent(healthcheck.NewClaudeStep(runner))` reusing the same `CreateClaudeRunner` factory the domain agent uses (single runner for all branches), any other value returns a wrapped error citing the accepted task types for this binary.
9. `agent/gemini/pkg/factory/factory.go` exposes the same shape as item 8. Per Assumptions, this is a healthcheck-only binary: accepts exactly `TaskTypeHealthcheck` (routing to `healthcheck.NewGeminiStep(geminiParser)`), every other value (including the literal `"gemini"`) hits the default-error branch.
10. `agent/code/pkg/factory/factory.go` exposes the same shape as item 8. Per Assumptions, this is a healthcheck-only binary: accepts exactly `TaskTypeHealthcheck` (routing to `healthcheck.NewNopStep()`), every other value hits the default-error branch.
11. Each binary's `main.go` replaces the existing direct call to `factory.CreateAgent(...)` with a call to `factory.CreateAgentForTaskType(lib.TaskType(a.TaskType), ...)`. The error return is recorded via `jobMetrics.RecordRun(agentlib.AgentStatusFailed)` and `jobMetrics.RecordDuration(time.Since(start))` before returning a wrapped error, mirroring the existing failure pattern around `factory.CreateDeliverer`.
12. When the task arrives with `task_type` empty/absent, the executor injects an empty `TASK_TYPE` env, envconfig then defaults `a.TaskType` to `"unknown"`. Dispatch on `lib.TaskType("unknown")` falls into the default branch — the binary fails fast with the accepted-types error rather than silently running the domain agent. This is a behavior change versus today; documented in Failure Modes below.
13. Unknown / unsupported `task_type` results in a wrapped error from `CreateAgentForTaskType` that contains the literal phrase `unknown task_type`, the offending value (quoted), the binary's agent name, and the accepted constants by value (e.g. `[claude healthcheck]`). The error is wrapped via `errors.Errorf(ctx, ...)` so it carries context for upstream logging.

## Constraints

- **Existing `factory.CreateAgent` signatures and return types stay reachable** from `cmd/run-task/main.go` and tests. Whether `CreateAgent` keeps its name or gets renamed and called by `CreateAgentForTaskType` internally is implementation-detail — but no caller outside this spec breaks.
- **No new direct dependency on `claudelib` from `agent/gemini` or `agent/code` factories.** Each binary's factory only imports the runner abstraction it actually uses. The healthcheck package, by contrast, may depend on all three (`claudelib`, `agentlib.AIParser`, none) — that's the point of centralizing it.
- **`lib/healthcheck/` cannot import `agent/claude`, `agent/gemini`, or `agent/code`** — it lives below them in the import graph and is consumed by all three.
- **`lib/healthcheck/` may depend on `lib/claude/` for the ClaudeRunner interface.** The `lib/` Go module already contains `lib/claude/`; same module, no cross-module concern.
- **Counterfeiter mocks for the runner / parser interfaces consumed by the healthcheck steps must exist.** `lib/claude/` already has `ClaudeRunner` mocks; the AIParser mock at `lib/mocks/` (or wherever generated) is already used by `lib/agent_parser_test.go`. The healthcheck tests reuse these — no new mock generation is mandatory unless the existing ones don't cover the call shape.
- **`make precommit` must pass in `lib/`, `agent/claude/`, `agent/gemini/`, and `agent/code/` service dirs.**
- **No semantic change to the framework's `Agent.Run` flow.** The healthcheck step is dispatched by the framework exactly like any other step.
- **Reference doc**: `docs/agent-job-lifecycle.md` describes how main.go consumes `TASK_TYPE` and reaches the factory. Update only if behavior diverges from what's already documented; otherwise unchanged.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Task arrives with `task_type` matching the binary's domain (e.g. `claude` for `agent-claude`) | Dispatch returns the existing domain agent; behavior identical to today | None — designed |
| Task arrives with `task_type: "healthcheck"` to `agent-claude` | Dispatch returns the healthcheck-Claude agent; Claude CLI runs the smoke prompt; non-empty reply → `done` | None — designed |
| Task arrives with `task_type: "healthcheck"` to `agent-gemini` | Dispatch returns the healthcheck-Gemini agent; AIParser called with smoke prompt; non-empty parsed reply → `done` | None — designed |
| Task arrives with `task_type: "healthcheck"` to `agent-code` | Dispatch returns the healthcheck-Nop agent; step immediately returns `done` | None — designed |
| Healthcheck Claude step: Claude CLI exits non-zero or stderr present | Step returns `Status: failed`, output contains the wrapped runner error | Operator inspects task result body for the captured error |
| Healthcheck Gemini step: AIParser returns error | Step returns `Status: failed`, output contains the wrapped parser error | Operator inspects task result body |
| Healthcheck Gemini step: parser returns empty / zero-value reply | Step returns `Status: failed`, output is `gemini healthcheck reply empty` (or equivalent literal) | Operator inspects task result body |
| Task arrives with `task_type: "oauth-probe"` to `agent-claude` | Per Assumptions: alias to the healthcheck-Claude agent (case fall-through with `TaskTypeHealthcheck`). Matches the Config CR's current `taskTypes: [claude, oauth-probe, healthcheck]` and prevents probe-pipeline regression until the rename spec ships. | None — designed |
| Task arrives with `task_type: ""` (executor injected empty after frontmatter absent) | envconfig defaults `a.TaskType` to `"unknown"`; dispatch hits default branch; binary returns wrapped error → job logs `failed` and pushes metric | Operator adds `task_type` to the task frontmatter; spec 028 filter already prevents this path in normal operation |
| Task arrives with `task_type` of an entirely unknown value (e.g. typo) | Dispatch hits default branch; binary returns wrapped error citing accepted types | Operator fixes frontmatter or AgentConfig CR |
| `factory.CreateAgentForTaskType` returns error | `main.go` records `AgentStatusFailed` on the metric, records duration, and returns a wrapped error — same pattern as the existing `CreateDeliverer` error path | None — designed |

### Interactions (not failures)

- **PHASE env value when dispatched to healthcheck**: the healthcheck step runs the same way regardless of `PHASE`. Implementation choice (item 5) decides whether all three phases register the step or framework single-phase semantics apply; either path yields the same observable result.
- **Probe publisher (out of scope, but interaction noted)**: `task/executor/pkg/probe/probe.go` still emits tasks with `task_type: "oauth-probe"`. The probe pipeline only routes to `agent-claude` (which the failure-modes row 9 decision must accept). Probes do not reach `agent-gemini` or `agent-code`.

## Security / Abuse Cases

`task_type` arrives at the binary already validated (CRD-side `validateTaskTypeValue` regex, executor-side filter from spec 028). The healthcheck steps issue a constant smoke prompt — no attacker-controlled data crosses the prompt boundary, no shell expansion, no path interpolation. Threat model:

- **Attacker controls**: nothing in the healthcheck path. The smoke prompt is a compile-time string literal.
- **Boundary crossed**: same as the existing domain-agent path (Claude CLI subprocess, Gemini HTTPS, Kafka result). No new boundaries.
- **Hang risk**: the underlying Claude CLI and Gemini HTTP client already respect the context deadline set by `service.Main`. Healthcheck inherits that deadline. No new infinite-loop surface.
- **Race**: none — dispatch is a single switch, evaluated before any goroutine launches.
- **DoS via unknown task_type**: a flood of unknown values causes a flood of fast-fail Jobs. Each Job still costs a pod startup. Mitigated upstream by spec 028 (executor filter) which only spawns tasks whose `task_type` is in the AgentConfig's accepted set — operators control that list.

No new validation needed inside the binary.

## Acceptance Criteria

- [ ] New package `lib/healthcheck/` exists with files following the agent repo's one-file-per-symbol convention (e.g. `healthcheck-claude-step.go`, `healthcheck-gemini-step.go`, `healthcheck-nop-step.go`, `healthcheck-agent.go`, plus the Ginkgo suite file and per-symbol `_test.go` files).
- [ ] `healthcheck.NewClaudeStep`, `healthcheck.NewGeminiStep`, `healthcheck.NewNopStep`, and `healthcheck.NewAgent` are exported with the signatures in Desired Behavior items 2–5.
- [ ] Unit tests cover: each step's success path (mock runner / parser returns non-empty result → `Status: done`), each step's failure path (mock returns error → `Status: failed`, output contains wrapped error), Nop step is trivially `done`, `NewAgent` wraps an arbitrary `Step` and `Agent.Run` invokes the wrapped step regardless of which phase value is passed.
- [ ] `lib/agent_task-type.go` exports `TaskTypeHealthcheck TaskType = "healthcheck"` with a GoDoc that describes it as the liveness task type and references `lib/healthcheck`.
- [ ] `lib/agent_task-type.go`'s `TaskTypeOAuthProbe` GoDoc is updated to `// Deprecated: use TaskTypeHealthcheck.` (the trailing "once introduced…" clause removed).
- [ ] `lib/agent_task-type_test.go` covers `TaskTypeHealthcheck` in the existing valid-constants table.
- [ ] `agent/claude/pkg/factory/factory.go` exports `CreateAgentForTaskType(taskType lib.TaskType, <existing CreateAgent args>) (*agentlib.Agent, error)`. The function switches on `taskType` with three branches: `TaskTypeClaude` → domain agent, `TaskTypeHealthcheck` → healthcheck-Claude agent, default → `errors.Errorf(ctx, "unknown task_type %q for agent-claude; accepted: %v", taskType, acceptedTypes)`. The probe-pipeline decision (Failure Modes row 9) is reflected by either including `TaskTypeOAuthProbe` in the accepted set with the same healthcheck-Claude mapping, or explicitly omitting it with a comment justifying the choice.
- [ ] Same shape applied to `agent/gemini/pkg/factory/factory.go` with the gemini parser and `healthcheck.NewGeminiStep`.
- [ ] Same shape applied to `agent/code/pkg/factory/factory.go` with `healthcheck.NewNopStep`.
- [ ] Each of the three binaries' factory has a `*_test.go` covering: domain task type (only `agent-claude`: `TaskTypeClaude`) returns a non-nil agent and nil error; `TaskTypeHealthcheck` returns a non-nil agent and nil error; `agent-claude` also asserts `TaskTypeOAuthProbe` returns the same as `TaskTypeHealthcheck`; an unsupported value (e.g. `"bogus"`) returns a nil agent and an error whose message contains the literal `unknown task_type` and the offending value.
- [ ] Each of `agent/claude/main.go`, `agent/gemini/main.go`, `agent/code/main.go` calls `factory.CreateAgentForTaskType(lib.TaskType(a.TaskType), ...)` instead of `factory.CreateAgent(...)` at the Kafka entry point. The error branch records `AgentStatusFailed` on `jobMetrics`, records duration, and returns a wrapped error — pattern matching the existing `CreateDeliverer` error block.
- [ ] `agent/{claude,gemini,code}/cmd/run-task/main.go` files are unchanged.
- [ ] `make precommit` passes in each of: `lib/`, `agent/claude/`, `agent/gemini/`, `agent/code/`.
- [ ] `CHANGELOG.md` has three new `Unreleased` bullets:
  - `feat(lib/healthcheck): shared liveness handler package — Claude/Gemini/Nop step flavors + NewAgent wrapper`
  - `feat(lib): add TaskTypeHealthcheck constant; update TaskTypeOAuthProbe GoDoc (drop "once introduced" qualifier)`
  - `feat(agent/{claude,gemini,code}): per-task-type dispatch via factory.CreateAgentForTaskType — healthcheck task type now routes to a dedicated liveness agent`
- [ ] **No new scenario.** Unit tests at the healthcheck-step, factory-dispatch, and constant layers fully cover the behavior. No E2E test is added — the regression risk is concrete but bounded by the unit-level checks above; the existing executor → main.go → factory seam is exercised in production by the probe pipeline once the rename spec ships.

## Verification

```
cd ~/Documents/workspaces/agent/lib && make precommit
cd ~/Documents/workspaces/agent/agent/claude && make precommit
cd ~/Documents/workspaces/agent/agent/gemini && make precommit
cd ~/Documents/workspaces/agent/agent/code && make precommit
```

All four must exit zero. Factory test output must include the three dispatch-branch assertions in each binary. Healthcheck package test output must include the step-success, step-failure, and agent-wrapper assertions.

## Suggested Prompt Split

Four prompts, with prompts 2–4 gated on prompt 1.

1. **`lib/healthcheck/` package + `TaskTypeHealthcheck` constant.** Adds the new package, the constant, the GoDoc update on `TaskTypeOAuthProbe`, and a single CHANGELOG bullet covering both. Self-contained; merges first.
2. **`agent/claude`: factory `CreateAgentForTaskType` + main.go dispatch + factory tests + CHANGELOG bullet.** Depends on prompt 1. Resolves the oauth-probe-handling decision from Failure Modes row 9 inside this prompt.
3. **`agent/gemini`: factory `CreateAgentForTaskType` + main.go dispatch + factory tests.** Depends on prompt 1. Independent of prompt 2. Resolves the gemini-domain-type decision from Assumptions inside this prompt.
4. **`agent/code`: factory `CreateAgentForTaskType` + main.go dispatch + factory tests + final CHANGELOG bullet.** Depends on prompt 1. Independent of prompts 2 and 3. Resolves the code-domain-type decision from Assumptions inside this prompt.

Prompts 2/3/4 are independent of each other and may be implemented in parallel after prompt 1 merges.

## Do-Nothing Option

If we skip this, every agent-repo binary keeps running its single hardcoded domain agent. The `agent-claude` Config CR's `taskTypes: [claude, oauth-probe, healthcheck]` declaration is a lie — both `oauth-probe` and `healthcheck` tasks reach the binary and silently run the full 3-phase domain prompt, burning Claude API minutes on a smoke check. Any future "I want a second behavior in this binary" feature is blocked on either forking the binary or doing this work then. The dispatch shape is also the gating prerequisite for the same work in trading-repo and maintainer-repo binaries (eight more binaries downstream). Not acceptable — the cost is one new small package, three two-line factory functions, three one-line main.go edits, and one new constant, against an indefinite tax on every future task-type-aware feature across all three repos.

## Verification Result

**Verified:** 2026-05-14T14:08:30Z (HEAD 78d2bda)
**Binary:** /Users/bborbe/Documents/workspaces/go/bin/dark-factory (v0.156.1-1-g04f3863-dirty)
**Scenario:** No runtime scenario per spec design ("No new scenario"); unit/structural verification per spec ## Verification block (make precommit × 4 dirs).
**Evidence:**
- `lib/healthcheck/` directory contains 9 files: agent + 3 step flavors + Ginkgo suite + 4 _test.go (per-symbol convention).
- `lib/agent_task-type.go:65` defines `TaskTypeHealthcheck TaskType = "healthcheck"`; line 61 GoDoc on `TaskTypeOAuthProbe` reads `// Deprecated: use TaskTypeHealthcheck.` (no "once introduced" qualifier).
- `agent/{claude,gemini,code}/pkg/factory/factory.go` each expose `CreateAgentForTaskType` with switch + default `errors.Errorf(ctx, "unknown task_type %q for agent-<name>; accepted: ...")`.
- Factory Ginkgo suites: claude 4/4, gemini 3/3, code 3/3 passed; healthcheck suite 19/19 passed.
- `make precommit` clean in lib/, agent/claude/, agent/gemini/, agent/code/ (all "ready to commit").
- main.go entry points call `factory.CreateAgentForTaskType(...)`; error branch records `agentlib.AgentStatusFailed` + duration (claude:114-128, gemini:110-115, code:98-103). `cmd/run-task/main.go` unchanged (still calls `factory.CreateAgent(...)`).
- CHANGELOG.md Unreleased entries on lines 19, 27, 28 (spec 031).
- Runtime corroboration: `claude-agent-a7032bbc-20260514133234` healthcheck Job completed status=done in 6.27s; `agent_job_duration_seconds_count{agent="claude-agent",task_type="healthcheck"} = 1`.
**Verdict:** PASS
