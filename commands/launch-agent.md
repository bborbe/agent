---
description: Interview-driven scaffolding for a new bborbe agent — clones a template repo, generates Config CRD + vault page + first scenario; `--config-only` skips the repo and emits a Config + ConfigMap prompt for the stock agent-claude image
argument-hint: "[<role-or-repo-name>] [--shape claude|code|gemini|pi] [--config-only]"
allowed-tools: [Skill, Task, Read, Write, Edit, Bash, AskUserQuestion, mcp__semantic-search__search_related]
---

<objective>
Scaffold a new bborbe agent end-to-end via interview + template clone. Delegate the heavy interview + scaffolding logic to the `launch-agent` skill; this command is a thin dispatcher that handles argument parsing and skill invocation.
</objective>

<process>
1. **Parse arguments** from `$ARGUMENTS`:
   - First positional token (if any): proposed role or repo-name seed. The skill suggests `<slug>-agent` and lets the user overwrite with any valid repo name — no forced `agent-` prefix
   - `--shape <shape>`: explicit shape pick (skip the shape-picker subagent)
   - `--config-only`: lightweight mode — no repo, no image, no CI. Its semantics live in `skills/launch-agent/references/config-only.md`; this command only parses and forwards the flag
   - No args: the skill runs the full interview from scratch

2. **Invoke the `launch-agent` skill** via the `Skill` tool, passing the parsed arguments through. The skill owns the whole pipeline — interview, shape pick, artifact generation, deploy checklist — so this command restates none of it. If the skill is unavailable, stop and say so; never inline its workflow here.
</process>

<success_criteria>
- The `launch-agent` skill was invoked with the parsed arguments
- The skill reported its own success criteria met and printed its deploy checklist
</success_criteria>

<notes>
- This command does NOT deploy the new agent — applying the Config CRD is the operator's decision after review
- The default mode's agent CI runs on its own repo and the template already configures it. `--config-only` creates no repo, so it configures no CI and releases nothing
- `allowed-tools` pre-approves the delegated skill's work; keep it in sync with `skills/launch-agent/SKILL.md`
</notes>
