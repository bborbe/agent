# Interview — 45-Q Script

Conversational walk through the [[Agent Design Guide]]'s 8 parts. Use `AskUserQuestion` for enumerable choices (max 4 options per question). For open-ended questions, ask once and accept the user's answer verbatim — don't over-clarify.

**Cadence**: don't ask all 45 in a row. After each Part's questions, summarize what you captured and confirm before moving to the next Part. This is the interview-first design — the user shouldn't feel interrogated; they should feel guided.

## Part 1 — Motivation (4 Q)

1. **Problem statement** (open): What problem does this agent solve? One paragraph.
2. **Current manual alternative** (open): How is this done today, if at all? (Manual? Slack? Half-built script?)
3. **Do-nothing cost** (open): What happens if we don't build it? (Lost time? Missed alerts? Slow feedback?)
4. **Success measure** (open): How will you know this agent is working? One concrete signal (latency, throughput, alert count, accuracy %).

→ Confirm: "Captured: <problem in 1 sentence>. Proceed?"

## Part 2 — Identity (5 Q)

1. **Role** (open): What does the agent do? (short kebab-able label, e.g. `dark-factory`, `pr-review`)
2. **Repo name** (suggest-with-override, per SKILL.md § naming): normalize the role to a slug, strip any trailing `-agent`, **suggest** `<core>-agent`, and let the user overwrite with any valid repo name (e.g. `github-dark-factory-agent` — `github-` repo prefix is valid). The chosen value is `<name>` = `bborbe/<name>`. No forced `agent-` prefix, no double `-agent`.
3. **Purpose statement** (open): 1-2 sentence purpose for the README and Config CRD description.
4. **Runtime tier** (`AskUserQuestion`): Which provider/cost tier?
   - Anthropic Max subscription (Claude Code, included quota)
   - Sonnet API (pay-per-token)
   - Local Qwen (on-cluster, no external API)
   - MiniMax (cheap, Tier-D quality)
5. **Domain & repo** (silent): `bborbe/<name>` (the chosen basename); document where the new repo lives.

→ Confirm: "Captured: <name> on <runtime tier>. Proceed?"

## Part 3 — Integration (5 Q)

1. **Trigger mechanism** (`AskUserQuestion`):
   - Cron (recurring-task-creator emits on schedule)
   - GitHub event (PR opened, build failed, release cut, etc.)
   - Agent chain (another agent emits a follow-up task)
   - Manual (operator-created task)
2. **If cron**: schedule expression (`hourly`, `daily 08:00`, `*/15 * * * *`, etc.)
3. **If GitHub event**: which event? (`pull_request`, `push`, `workflow_run`, etc.)
4. **Task producer** (open): Which existing watcher/cron creates the task, OR does this agent need a new producer? (If new, note "needs new watcher" in NEXT-DIRECTIONS.)
5. **Upstream deps** (open): What systems must be reachable for this agent to work? (Kafka topic, HTTP API, vault path, k8s namespace, etc.)
6. **Downstream consumers** (open): Who reads this agent's output? (Vault task body? Another agent? Slack? Email?)

→ Confirm: "Trigger: <mechanism>; Producer: <name>; Reads from: <list>; Writes to: <list>. Proceed?"

## Part 4 — Behavior (8 Q — most important)

1. **Supported phases** (`AskUserQuestion` multi-select if allowed, else loop):
   - `planning` (parse task → plan output)
   - `in_progress` (execute the plan)
   - `ai_review` (fresh-context verification)
   - `human_review` (escalation only)
   - `done` (terminal, no work)
2. **Per supported phase, ask**:
   - **Step list** (open): 1-N steps within this phase. For single-step (~90% case): just describe the work. For multi-step: list each step's name + work + save trigger.
   - **Tool scope** (`AskUserQuestion`): which Claude/LLM tools allowed in this phase? (Read-only / Read+Write / Full / Custom list)
   - **Model** (silent default): `planning` = Haiku/Sonnet, `in_progress` = Sonnet, `ai_review` = Opus; override if user specifies
3. **State passed between phases** (open): what does each phase write (body sections, frontmatter fields) for the next phase to read?
4. **Non-goals** (open): list 3-7 things this agent explicitly does NOT do (scope-creep guard).

→ Confirm: "Phases: <list>; Tool scopes: <summary>; Non-goals captured. Proceed?"

## Part 5 — Data Contract (4 Q)

1. **Inputs** (open): what's in `TASK_CONTENT` env var? (Markdown body fields, fenced JSON blocks, frontmatter fields the agent reads)
2. **Outputs** (open): what body sections + frontmatter fields does the agent write?
3. **Idempotency key** (open): if the same task is replayed (Kafka redelivery, manual re-trigger), what makes the operation safe? (UUID5? Filename? "Skip if `## Result` already present"?)
4. **Concurrency** (`AskUserQuestion`): can multiple instances of this agent run concurrently?
   - Yes (stateless, no shared resource)
   - No (writes to shared resource; need lock)
   - Per-task (sharded by task ID; each instance owns its task)

→ Confirm: "Inputs / outputs captured; idempotent on <key>; concurrency: <mode>. Proceed?"

## Part 6 — Operations (5 Q)

1. **Schedule frequency** (open): how often does this agent run? (continuous / hourly / daily / per-event)
2. **k8s resources** (`AskUserQuestion`):
   - Small (250m CPU / 256Mi RAM — most agents)
   - Medium (500m / 512Mi)
   - Large (1 CPU / 1Gi — heavy LLM context or compute)
3. **Cost estimate** (open): rough $/month estimate? (LLM tokens × frequency)
4. **Observability** (open): what metrics matter? (Prometheus counters, log keywords, Sentry events)
5. **Kill switch** (silent default): `kubectlquant -n <ns> delete config.agent.benjamin-borbe.de <agent-name>` removes the Config CR; agent stops accepting new tasks. Document this in README.

→ Confirm: "Resources: <profile>; Cost: <estimate>; Kill via: kubectl delete config. Proceed?"

## Part 7 — Safety (5 Q)

1. **Consent gates** (`AskUserQuestion`): does the agent need explicit user approval before any action?
   - No (fully autonomous)
   - Yes, for writes only (read-only autonomous, write gated)
   - Yes, for all actions (advisory only, never executes)
2. **Error handling per class** (open): for each failure mode (transient infra, semantic error, rate limit, missing dependency), what's the response?
3. **Security boundaries** (open): what secrets does the agent need? (teamvault keys, OAuth tokens) — list the teamvault key names (NOT raw secrets).
4. **Assumptions** (open): what does the agent assume about its environment that must remain true? (Kafka topic exists, vault is mounted RW, git-rest is reachable, etc.)
5. **Data privacy** (open): does the agent read/write any PII or sensitive data? If yes, where does it land?

→ Confirm: "Consent: <gate>; Failure modes: <summary>; Secrets: <keys>. Proceed?"

## Part 8 — Acceptance (4 Q)

1. **Per-phase acceptance** (open): for each phase, what does "success" look like? (Output structure, state transition, no errors)
2. **Overall acceptance** (open): end-to-end, what proves the agent works? (Smoke test scenario in plain English — this becomes scenario 001)
3. **Verification procedure** (silent default): the smoke test runs via `dark-factory:run-scenario scenarios/001-*.md`; document the steps in scenario template.
4. **Rollback plan** (open): if this agent ships and misbehaves, how do we stop it cleanly without affecting other agents? (Delete Config CR? Pause via env flag? Revert image tag?)

→ Confirm: "Per-phase + overall acceptance captured; scenario 001 = <one-line summary>; rollback via <method>. Proceed to shape recommendation?"

---

## Total: ~40 questions across 8 parts

Some questions have follow-ups based on the answer; total interaction count varies from ~30 (simple agent, defaults accepted) to ~50 (complex agent, custom answers throughout). Aim for the lower end — accept defaults aggressively, ask only when the answer materially affects scaffolding.

## After the interview

You have enough to:
- Pick the shape (Phase 2 — invoke `agent-shape-picker` with the captured intent)
- Render all templates with the captured values
- Write a clear deploy checklist

Capture the answers in a structured working-memory format (per-Part dict) so the template-rendering phase can look up values by name.

## Reference

- [[Agent Design Guide]] — full 45-Q source of truth (this script is a conversational rendering)
- `references/shapes.md` — shape decision matrix (used in Phase 2)
- [[Interview-First Agent Design]] — design pattern (why this exists)
