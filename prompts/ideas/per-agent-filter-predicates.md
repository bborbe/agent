---
status: idea
---

<summary>
- Replace hardcoded filter chain in `task/executor/pkg/handler/task_event_handler.go` with per-agent predicate lists
- Each registered agent (currently `claude`, `backtest-agent`) declares what task frontmatter requirements it has
- Executor iterates the predicates for the task's assignee; skip if any predicate fails
- Stage filter becomes one predicate among many — dev/prod still expressed as a required value
- Keeps current behaviour for existing agents while enabling agent-specific requirements (e.g. `trade-analysis-agent` may need `phase: in_progress` only, never `ai_review`)
- New agents register predicates alongside their image entry — no new `if` blocks in the handler
</summary>

<objective>
Make the executor filter chain data-driven so adding a new agent with new
requirements doesn't require changing the handler's filter code path.
</objective>

<context>
Current state (after executor-stage-filter ships):
- `lib.TaskFrontmatter` is `map[string]interface{}` with typed helpers
- Handler hardcodes the filter chain: status → phase → stage → assignee → image-map → active-job
- Assignee-to-image map lives in `task/executor/main.go`
- Each executor instance has one `base.Branch` (dev/prod)

Future design sketch (brainstorm — refine before implementation):
- Introduce `AgentSpec` struct: `{Image string, Predicates []Predicate}`
- Predicate interface: `Matches(task lib.Task, execCtx ExecutionContext) (ok bool, reason string)`
- `ExecutionContext` carries executor-level values the predicates can match against (branch, namespace, etc.)
- Replace `assigneeImages map[string]string` with `agentSpecs map[string]AgentSpec`
- Handler: look up spec → iterate predicates → skip with `reason` as metric label
</context>

<requirements>
Not ready for draft — open questions below.
</requirements>

<constraints>
- Idea only — do NOT schedule for execution without a spec.
- Depends on `executor-stage-filter` being merged first (that prompt adds the `Stage()` helper and stage semantic).
</constraints>

## Open questions

1. Predicate storage: Go code (compiled) vs config file (YAML at startup)?
2. Metric label granularity: one label per predicate reason, or keep `skipped_predicate` generic?
3. How does this interact with the assignee→image lookup — is image still part of `AgentSpec` or separate?
4. Do predicates need access to the full `lib.Task` or only `Frontmatter`?
5. Shared predicates (e.g. `RequirePhase(...TaskPhase)`) vs per-agent custom functions?
6. Does this replace the current status/phase filters entirely, or are those globally required baselines that every agent shares?
