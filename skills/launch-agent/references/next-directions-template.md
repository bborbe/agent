# NEXT-DIRECTIONS Template — `NEXT-DIRECTIONS.md`

Renders in the new agent repo's root. Captures everything the interview surfaced as "not in v0, but worth doing later" — keeping the deferred work named (not cut) and adjacent to the code rather than scattered across vault tasks.

Mirrors Anthropic's `launch-your-agent` NEXT-DIRECTIONS pattern: every deferral has a `what` + `why deferred` + `how to do it later`.

Placeholders: `<ANGLE_BRACKETED>` — replace during scaffolding; remove unused tiers if no deferrals at that level.

---

```markdown
# NEXT-DIRECTIONS — agent-<NAME>

Deferred-not-cut work captured during `/launch-agent` interview on <YYYY-MM-DD>. Each entry has a clear mechanism so future-you (or a sub-agent) can pick it up without re-deriving the context.

## v0 (shipped — initial scaffold)

- Scaffolded from `bborbe/agent-<SHAPE>` template
- Config CRD targets <STAGE> namespace
- Phases: <LIST>
- Smoke test: scenario 001 (happy path)
- Pinned to `github.com/bborbe/agent v<VERSION>`

## v1 (next iteration — surfaced during interview)

<!-- Examples: domain logic for the actual work; the v0 scaffold is just the runtime shell -->
- **<WHAT_1>**
  - **Why deferred**: <REASON — e.g. "scope-limited v0 to runtime + scenario, not domain logic">
  - **How**: <CONCRETE_MECHANISM — e.g. "implement `pkg/factory/factory.go` per [[<Name> Agent]] phase table; add prompts in `pkg/prompts/<phase>.md`; expand scenario 001 to cover real input">
  - **Effort**: <ROUGH_HOURS_OR_DAYS>

- **<WHAT_2>**
  - **Why deferred**: <REASON>
  - **How**: <MECHANISM>
  - **Effort**: <ESTIMATE>

## v2 (medium-term — quality / robustness improvements)

<!-- Examples: rubric tightening, more scenarios, observability metrics, etc. -->
- **Add `## Rubric` body section + ai_review wiring**
  - **Why deferred**: agent ships with phase prompts hard-coded; rubric-as-data lets operators tune without code release
  - **How**: see [[Claude Managed Agents - Ideas for bborbe Framework#6 Outcome rubric as separate primitive from agent config]]
  - **Effort**: half-day per phase

- **Add Prometheus metrics for token burn + per-task cost**
  - **Why deferred**: cost estimate from interview was rough; production metrics needed to validate
  - **How**: implement `pkg/metrics/metrics.go` (counterfeiter mock for tests) + Prometheus scrape via `agent-task-executor`'s shared `/metrics` endpoint
  - **Effort**: half-day

- **More scenarios**
  - **Why deferred**: scenario 001 is happy path only; edge cases pending
  - **How**: write `scenarios/002-<edge-case>.md`, `003-<failure-mode>.md`, ... per real failures seen in dev
  - **Effort**: half-day per scenario

## v3 (long-term — platform-level upgrades that benefit this agent)

<!-- Examples: persistent memory, mock connector, multi-agent coordination -->
- **Add persistent memory store** (if applicable)
  - **Why deferred**: requires platform change ([[Claude Managed Agents - Ideas for bborbe Framework#4]]); not agent-specific
  - **How**: when the platform ships `spec.memoryClaim` on Config CRD, add it to this agent's Config and use for <SPECIFIC_USE_CASE>
  - **Effort**: half-day once platform is ready

## Notes

- Updating this file: as v1 work lands, move entries to v0 with the shipped commit SHA; as new ideas surface, add to v2/v3
- Cross-link to vault tasks where the work is tracked: `[[Implement <Name> Agent Memory Store]]`
- This file is the source of truth for "what's next for this agent" — vault tasks reference back here, not the other way
```
