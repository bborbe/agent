---
description: Interview-driven scaffolding for a new bborbe agent — clones a template repo, generates Config CRD + vault page + first scenario
argument-hint: "[agent name] [--shape claude|code|gemini|pi]"
allowed-tools: [Task, Read, Write, Edit, Bash, AskUserQuestion, mcp__semantic-search__search_related, mcp__semantic-search__check_duplicates]
---

<objective>
Scaffold a new bborbe agent end-to-end via interview + template clone. Delegate the heavy interview + scaffolding logic to the `launch-agent` skill; this command is a thin dispatcher that handles argument parsing and skill invocation.
</objective>

<process>
1. **Parse arguments** from `$ARGUMENTS`:
   - First positional token (if any): proposed agent name (will be normalized by the skill)
   - `--shape <shape>`: explicit shape pick (skip the shape-picker subagent)
   - No args: skill runs the full interview from scratch

2. **Invoke the `launch-agent` skill** via the `Skill` tool:
   - Pass parsed arguments through
   - The skill orchestrates: interview → shape pick (subagent if not explicit) → `gh repo create --template` → rename/customize → generate Config CRD + vault page + goal + scenario → print deploy checklist

3. **Skill workflow** (executed by `skills/launch-agent/SKILL.md`):
   - Read references (shapes.md, interview.md, templates)
   - Run [[Agent Design Guide]] 45-Q interview (conversational, AskUserQuestion for enumerable choices)
   - Recommend shape via `agent-shape-picker` subagent if `--shape` not provided
   - `gh repo create bborbe/agent-<name> --public --template bborbe/agent-<shape>`
   - Clone, rename Go module path + package names + Config kind across files
   - Generate `Config.yaml` (CRD instance), `agent/<name>/.claude/CLAUDE.md`, `README.md` adapted
   - Write vault artifacts: `50 Knowledge Base/<Name> Agent.md`, `23 Goals/Build <Name> Agent.md`, `24 Tasks/Bootstrap <Name> Agent.md`, scenario stub
   - Print deploy checklist (kubectl apply, build image, run scenario, etc.)
</process>

<success_criteria>
- Skill invoked successfully
- New repo created on GitHub via `--template` flag
- Local clone exists at `~/Documents/workspaces/agent-<name>/`
- Vault artifacts created in the configured vault (Personal)
- Deploy checklist printed
</success_criteria>

<notes>
- This command does NOT deploy the new agent — `kubectl apply` of the Config CRD is operator decision after review
- The agent's CI runs on its own repo; this command does not configure CI (template already has it)
- See [[Quick-Launch New Agents]] for the parent goal that landed this plugin
</notes>
