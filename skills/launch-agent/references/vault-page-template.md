# Vault Page Template — `50 Knowledge Base/<Name> Agent.md`

Renders the per-agent knowledge page in the Personal vault. Mirrors the [[Claude Agent]] / [[Code Agent]] / [[Gemini Agent]] / [[Pi Agent]] style so the catalog stays uniform.

Placeholders: `<ANGLE_BRACKETED>` — replace during scaffolding from interview answers.

---

```markdown
---
tags:
  - knowledge-base
  - agent
date: <YYYY-MM-DD>
---
Tags: [[AI Agent]] [[Agent Page Writing Guide]] [[Autonomous Agent Workflow]]

---

<ONE_PARAGRAPH_PURPOSE>  <!-- from interview Part 2.3 — 1-2 sentences -->

**Task types** (per Config CRD `spec.taskTypes`): `<NAME>`, `healthcheck`. The executor dispatches by `task_type`; `healthcheck` routes to the shared liveness handler.

## Purpose

<!-- from interview Part 1 (Motivation) — what this agent achieves -->
- <GOAL_BULLET_1>
- <GOAL_BULLET_2>
- <GOAL_BULLET_3>

## Trigger

- **Mechanism**: <CRON | GITHUB_EVENT | AGENT_CHAIN | MANUAL>  <!-- from interview Part 3.1 -->
- **Producer**: <PRODUCER_NAME>  <!-- from interview Part 3.4 -->
- **Cadence**: <SCHEDULE_OR_EVENT_DETAIL>  <!-- from interview Part 6.1 -->

## Phases

<!-- from interview Part 4 — supported phases + per-phase work -->
| Phase | Step list | Tool scope | Model |
|---|---|---|---|
| `planning` | <STEPS> | <SCOPE> | <MODEL> |
| `in_progress` | <STEPS> | <SCOPE> | <MODEL> |
| `ai_review` | <STEPS> | <SCOPE> | <MODEL> |

## Data Contract

- **Inputs**: <INPUTS>  <!-- from interview Part 5.1 -->
- **Outputs**: <OUTPUTS>  <!-- from interview Part 5.2 -->
- **Idempotency key**: `<KEY>`  <!-- from interview Part 5.3 -->
- **Concurrency**: <MODE>  <!-- from interview Part 5.4 -->

## Operations

- **Resources**: `<CPU>` / `<MEM>`  <!-- from interview Part 6.2 -->
- **Cost estimate**: <COST>  <!-- from interview Part 6.3 -->
- **Observability**: <METRICS>  <!-- from interview Part 6.4 -->
- **Kill switch**: `kubectlquant -n <NAMESPACE> delete config.agent.benjamin-borbe.de <NAME>`

## Safety

- **Consent gates**: <GATE_MODE>  <!-- from interview Part 7.1 -->
- **Error handling**: <SUMMARY>  <!-- from interview Part 7.2 -->
- **Secrets**: teamvault keys `<KEY_LIST>` — never raw secrets  <!-- from interview Part 7.3 -->
- **Data privacy**: <PRIVACY_NOTES>  <!-- from interview Part 7.5 -->

## Code

- **Repo**: [bborbe/<NAME>](https://github.com/bborbe/<NAME>)
- **Source path**: `~/Documents/workspaces/<NAME>/`
- **Template**: scaffolded from `bborbe/agent-<SHAPE>` via `/launch-agent` on <YYYY-MM-DD>
- **Deploy**: `BRANCH=<dev|prod> make buca` from a clean deployment worktree

## Tracked goal

- [[Build <NAME> Agent]] — the SMART goal tracking this agent's rollout

## Non-goals

<!-- from interview Part 4.5 — explicit out-of-scope -->
- <NON_GOAL_1>
- <NON_GOAL_2>
- <NON_GOAL_3>

## NEXT-DIRECTIONS

See `~/Documents/workspaces/<NAME>/NEXT-DIRECTIONS.md` for v1/v2/v3 deferred upgrades captured during the interview.

## Related

- [[Agent Hub]] — catalog of all agents
- [[Agent Design Guide]] — 45-Q checklist
- [[<SHAPE> Agent]] — reference template this agent is based on
- [[Quick-Launch New Agents]] — the goal that enabled this scaffolding flow
```
