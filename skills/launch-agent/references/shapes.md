# Shapes — Reference Template Decision Matrix

Four reference templates exist on GitHub as `is_template: true`. Picking the right one shapes the agent's runtime cost, latency, and where AI lives in the work.

## Quick decision

| Question | Yes → Pick |
|---|---|
| Can the task be fully expressed as deterministic Go logic? | **code** |
| Is the INPUT free-form text but the work itself deterministic? | **gemini** |
| Does the WORK ITSELF need natural-language reasoning (planning, code edits, prose verdicts)? | next row |
| ↳ High-stakes / complex reasoning (PR review, code generation, multi-step plans) | **claude** |
| ↳ Low-stakes / monkey work (classification, normalization, bulk triage) | **pi** |

## The four shapes

### claude — AI-heavy reference

**Template repo**: `bborbe/agent-claude`

**Structure**: Same Claude Code step reused across all 3 phases (planning, execution, ai_review). The LLM does the work each time; phase = label + tool scope + prompt.

**Pick when**:
- Task requires natural-language reasoning, judgment, or code generation
- Tool surface is small (Bash, Read, Edit, Grep) — Claude orchestrates internally
- Output structure emerges from the prompt, not rigid Go code
- High stakes — Claude's reasoning quality matters

**Cost shape**: ~10-50k tokens per phase per task, ~$0.10-0.50 per task on Sonnet, ~$1-5 on Opus.

**Live examples**: pr-reviewer, github-releaser, trade-analysis, sentry-bug-analyser (planned)

**Don't pick when**: the work is deterministic Go (use `code`), or input is free-form but execution is deterministic (use `gemini`).

### code — Pure-Go reference

**Template repo**: `bborbe/agent-code`

**Structure**: 3 distinct deterministic Go steps (PlanStep → ExecuteStep → VerifyStep). No LLM in the loop.

**Pick when**:
- Task is fully expressible as deterministic Go logic
- Inputs/outputs are typed structs, not free-form text
- No natural-language interpretation needed
- Speed + cost matter (no LLM token cost)

**Cost shape**: ~zero LLM cost. CPU only.

**Live examples**: backtest (currently AI-heavy planning, migrating to code), CRD reconciliation loops, mechanical fix-up agents

**Don't pick when**: any phase needs natural-language reasoning, even just parsing free-form input.

### gemini — Boundary-translator reference

**Template repo**: `bborbe/agent-gemini`

**Structure**: Gemini at the planning edge (free-form input → structured plan). Pure-Go for execution + verify.

**Pick when**:
- Input is messy / unstructured (markdown task body, free-form description, etc.) BUT
- Once interpreted, the work is deterministic Go
- Cost matters (Gemini ~10× cheaper than Claude for one-shot interpretation)
- You want the LLM OUT of the execution loop (Go runs the actual work)

**Cost shape**: 1 Gemini call per task (~$0.001-0.01), zero LLM in execution.

**Live examples**: backtest plan parser (Gemini extracts strategy params from free-form description; Go runs the backtest)

**Don't pick when**: execution itself needs LLM (use `claude`), or input is already structured (use `code`).

### pi — Tier-D LLM reference

**Template repo**: `bborbe/agent-pi`

**Structure**: Same as `claude` but uses MiniMax `pi` model instead of Claude. Much cheaper, less capable.

**Pick when**:
- Monkey-work LLM calls (extraction, formatting, simple classification)
- Cost-sensitive workloads where Claude is overkill
- High-frequency, low-stakes runs
- Quality bar is "good enough" not "great"

**Cost shape**: ~$0.001-0.01 per task. Fits subscription pricing without rate-limit pressure.

**Live examples**: bulk task triage, simple format normalization

**Don't pick when**: any output is high-stakes or needs robust reasoning (use `claude`).

## Anti-patterns

- **Picking `claude` for everything** → cost balloons; many tasks are deterministic (use `code`) or Gemini-tier (use `gemini`/`pi`).
- **Picking `code` to avoid LLM cost** when the work fundamentally needs interpretation → leads to brittle regex/string parsing that breaks on edge cases.
- **Picking `gemini` when execution also needs LLM** → mixed concern; `claude` end-to-end is cleaner.
- **Picking `pi` for production-critical paths** → MiniMax quality bar is lower; reserve for non-critical bulk work.

## When the use case spans shapes

If the agent has phases that genuinely need different tiers (e.g. planning = Claude, execution = Go, review = Pi), see [[Mixed-Shape Pattern B Agents]] in the vault. The `claude` template's per-phase tool scoping accommodates this — swap the model per phase via env var rather than picking a different base template.

## Reference

- [[Agent Hub]] — full catalog with which agents use which shape
- [[Claude Agent]] / [[Code Agent]] / [[Gemini Agent]] / [[Pi Agent]] — per-shape vault pages
- [[Multi-Provider Agent Architecture]] — tier rationale (Anthropic Max / Sonnet API / local Qwen / MiniMax)
- `agent-shape-picker` subagent — automated classifier for use case → shape
