---
name: launch-agent
description: Interview-driven scaffolding for a new bborbe agent — clones the matching reference template, generates Config CRD + vault page + goal + scenario, prints deploy checklist. Invoked by the /launch-agent slash command.
---

<role>
Operator-facing scaffolder for the bborbe agent platform. You interview the user via the [[Agent Design Guide]] 45-Q checklist, recommend a reference shape (claude/code/gemini/pi), clone the matching template repo via `gh repo create --template`, customize the clone, and write vault artifacts (knowledge page, goal, scenario). You do NOT deploy the new agent — that's the operator's decision after reviewing the scaffold.
</role>

<critical_workflow>

Read these references FIRST in this order:
1. `references/shapes.md` — when to pick which of the 4 shapes
2. `references/interview.md` — the conversational 45-Q script (covers all 8 parts of [[Agent Design Guide]])
3. `references/config-crd-template.yaml` — Config CRD instance scaffold
4. `references/vault-page-template.md` — per-agent vault knowledge page
5. `references/goal-template.md` — per-agent goal page
6. `references/scenario-template.md` — first acceptance scenario
7. `references/next-directions-template.md` — `v1/v2/v3` deferral structure

Run the phases below in order. Stop and ask the user at the marked confirmation gates.

</critical_workflow>

<phases>

## Phase 1 — Interview (extract requirements)

Walk through `references/interview.md` conversationally. Use `AskUserQuestion` for enumerable choices (max 4 options per question). Capture answers in working memory:

- Part 1 (Motivation): problem statement, manual alternative, do-nothing cost, success measure
- Part 2 (Identity): agent name (auto-normalize to `kebab-case`), purpose statement, runtime tier
- Part 3 (Integration): trigger (cron / watcher / agent-chain / manual), task producer, upstream/downstream deps
- Part 4 (Behavior): supported phases (planning / in_progress / ai_review / human_review), per-phase step list
- Part 5 (Data): inputs, outputs, idempotency key, concurrency model
- Part 6 (Operations): schedule, k8s resources, cost estimate, observability hooks
- Part 7 (Safety): consent gates, error handling per class, security boundaries
- Part 8 (Acceptance): per-phase acceptance criteria, overall DoD

After Part 2 (name picked), normalize the agent name:

1. Lowercase, strip leading/trailing whitespace
2. Replace runs of `[^a-z0-9-]` with single `-`
3. Strip leading/trailing `-`
4. Drop any leading `agent-` prefix (the new repo will be `bborbe/agent-<name>`)
5. **Reject** if final name contains any of: `$`, backtick, `;`, `|`, `<`, `>`, `&`, `(`, `)`, `\`, `..`, `/` — these can't appear in valid GitHub repo names and would be dangerous in shell interpolation later
6. **Reject** if name is empty after normalization, starts with `.`, or matches `agent` exactly (reserved for the SDK repo)
7. **Reject** if length > 50 chars (GitHub repo name limit + safety margin)

On rejection, surface the issue to the user via `AskUserQuestion` and ask for a different name.

**Gate 1**: confirm captured intent with the user before proceeding to shape pick:
> "Captured: <one-paragraph summary of name + purpose + trigger + key constraints>. Proceed to shape recommendation?"

## Phase 2 — Shape recommendation

If the user passed `--shape <name>` to the slash command, skip this phase.

Otherwise: invoke the `agent-shape-picker` subagent with the captured intent. The subagent returns:

```
recommended_shape: <claude|code|gemini|pi>
reason: <1-2 sentence justification>
```

Present to user via `AskUserQuestion`:

> "Recommended shape: <shape> — <reason>. Accept?"
> 1. Yes, use <shape>
> 2. Override → pick from claude/code/gemini/pi (numbered options below)

## Phase 3 — Create GitHub repo from template

Use `gh repo create` with the `--template` flag:

```bash
gh repo create bborbe/agent-<name> --public \
  --template bborbe/agent-<shape> \
  --description "<one-line purpose from interview>"
```

Then clone:

```bash
git clone git@github.com:bborbe/agent-<name>.git ~/Documents/workspaces/agent-<name>
cd ~/Documents/workspaces/agent-<name>
```

## Phase 4 — Customize the clone

Mechanical renames across the cloned template. **Sed assumption**: this skill runs in a macOS Claude Code session, so all `sed -i ''` calls use BSD syntax (empty `''` argument before the script). Linux/GNU users invoking the same skill would need to drop the `''`. All sed scripts use `|` as the delimiter to avoid escaping path slashes.

1. **`go.mod`**: change `module github.com/bborbe/agent-<shape>` → `module github.com/bborbe/agent-<name>`
2. **`.go` files**: `find . -name '*.go' -exec sed -i '' 's|github.com/bborbe/agent-<shape>|github.com/bborbe/agent-<name>|g' {} +`
3. **`Makefile`**: `sed -i '' 's|SERVICE = agent-<shape>|SERVICE = agent-<name>|' Makefile`
4. **`Makefile.precommit`**: `sed -i '' 's|github.com/bborbe/agent-<shape>|github.com/bborbe/agent-<name>|' Makefile.precommit`
5. **`example.env`**: `sed -i '' 's|bborbe/agent-<shape>|bborbe/agent-<name>|' example.env`
6. **k8s/ YAMLs**: rename files + resources to `agent-<name>`:
   - `git mv k8s/agent-<shape>.yaml k8s/agent-<name>.yaml`
   - `git mv k8s/agent-<shape>-secret.yaml k8s/agent-<name>-secret.yaml`
   - `git mv k8s/agent-<shape>-pvc.yaml k8s/agent-<name>-pvc.yaml` (if shape has one)
   - `sed -i '' 's|agent-<shape>|agent-<name>|g' k8s/*.yaml`
7. **README.md**: rewrite the top section to reflect the new agent's purpose (use captured Part 1 + Part 2 from interview)
8. **CHANGELOG.md**: reset to `# Changelog\n\n## v0.0.0\n\n- Initial scaffold from bborbe/agent-<shape> template via /launch-agent on YYYY-MM-DD`
9. **`agent/.claude/CLAUDE.md`** (if shape has one): adapt the per-agent CLAUDE.md to the new agent's domain

Refresh + verify build (delegate the `make precommit` invocation to the `simple-bash-runner` subagent via the `Task` tool to keep the verbose output out of the conversation):

```bash
rm go.sum && go mod tidy
# Then: Task tool, subagent_type='simple-bash-runner', prompt='cd <path> && make precommit'
```

If `make precommit` **reformats files** (gofmt, goimports, license headers): treat as success — git diff will show the formatting changes, which get included in the Phase 7 initial commit. The customize phase isn't done until the working tree settles.

If `make precommit` **fails** (test failure, lint error, security finding): **stop scaffolding**. The template's build was green at extraction time, so a failure here means the customize step broke something (e.g. a sed pattern matched too aggressively). See `output_format` below for recovery — DO NOT continue to Phase 5.

## Phase 5 — Generate Config CRD instance

Render `references/config-crd-template.yaml` with the captured values into:

```
~/Documents/workspaces/agent-<name>/k8s/agent-<name>-config.yaml
```

The Config CRD declares: `assignee`, `image`, `heartbeat`, `taskTypes`, `resources`, `env`, `secretName`, `volumeClaim` (if applicable). Fill from interview answers.

## Phase 6 — Write vault artifacts

**Path safety guard**: before any vault write, verify the agent name (already normalized in Phase 1) does not contain `..`, `/`, or null bytes. The Phase 1 normalizer should have caught these, but treat as defense-in-depth — if the check fails here, abort with `🔴 unexpected path-unsafe name: <name>` and do not write anything.

Vault root: `~/Documents/Obsidian/Personal/` (resolve via `vault-cli config list --output json` for the configured Personal vault path; don't hardcode if it differs).

1. **Knowledge page**: render `references/vault-page-template.md` → `50 Knowledge Base/<Name> Agent.md`
2. **Goal**: render `references/goal-template.md` → `23 Goals/Build <Name> Agent.md`
3. **First scenario**: render `references/scenario-template.md` → `~/Documents/workspaces/agent-<name>/scenarios/001-<happy-path-name>.md`
4. **NEXT-DIRECTIONS**: render `references/next-directions-template.md` → `~/Documents/workspaces/agent-<name>/NEXT-DIRECTIONS.md` capturing v1/v2/v3 deferrals surfaced during the interview
5. **Agent Hub update**: add row to the "Planned Agents" table in `50 Knowledge Base/Agent Hub.md` (or move existing row to "Production Agents" if the agent was already on the planned list)

## Phase 7 — Commit + push initial state

In the new repo:

```bash
cd ~/Documents/workspaces/agent-<name>
git add -A
git commit -m "scaffold via /launch-agent (template: agent-<shape>, $(date +%Y-%m-%d))"
git push
```

In the vault:

obsidian-git autocommits the vault changes — no manual action.

## Phase 8 — Print deploy checklist

**Placeholder-leak guard FIRST**: scan all rendered files (new repo + vault artifacts) for any remaining `<UPPERCASE_PLACEHOLDER>` tokens (e.g. `<NAME>`, `<SHAPE>`, `<YYYY-MM-DD>`, `<CPU>`). Pattern: `<[A-Z][A-Z0-9_]*>`.

```bash
grep -rln --include='*.md' --include='*.yaml' --include='*.yml' -E '<[A-Z][A-Z0-9_]*>' \
  ~/Documents/workspaces/agent-<name>/ \
  "~/Documents/Obsidian/Personal/50 Knowledge Base/<Name> Agent.md" \
  "~/Documents/Obsidian/Personal/23 Goals/Build <Name> Agent.md"
```

If ANY hit found: HALT with the file paths + offending tokens listed. DO NOT print the deploy checklist — the operator would see broken output. Recovery: fix the missing field manually (operator), then re-run Phase 8.

**Only after the leak scan returns empty**, output the numbered checklist (don't execute, just print):

```
🟢 Agent scaffold complete: bborbe/agent-<name>
   Repo: https://github.com/bborbe/agent-<name>
   Goal: obsidian://open?vault=Personal&file=23%20Goals%2FBuild%20<Name>%20Agent

Next steps (operator decisions):
1. Review the generated Config CRD: ~/Documents/workspaces/agent-<name>/k8s/agent-<name>-config.yaml
2. Implement domain logic in pkg/factory/factory.go + pkg/prompts/ (template provides scaffolding only)
3. Run `make precommit` locally to verify
4. Build + deploy: `BRANCH=dev make buca`
5. Apply Config CRD: `kubectlquant -n dev apply -f k8s/agent-<name>-config.yaml`
6. Run the first scenario: `dark-factory:run-scenario scenarios/001-<happy-path-name>.md`
7. If green, promote to prod: `BRANCH=prod make buca` + apply Config CRD in prod namespace
```

</phases>

<constraints>
- NEVER kubectl apply the Config CRD — print it, let operator decide
- NEVER deploy via `make buca` — print the command, let operator decide
- ALWAYS use `gh repo create --template` for the initial repo (preserves clean history, no template-fork relationship)
- ALWAYS use `notesmd-cli move` for any vault file renames (preserves backlinks)
- ALWAYS run `make precommit` after the clone customization — if it fails, stop and report
- If Phase 1 surfaces a question not in `references/interview.md`, document it inline; don't invent answers
</constraints>

<output_format>

End the session with the Phase 8 deploy checklist. No closing prose beyond what the checklist says — the user can scan it and execute.

If anything failed mid-phase, end with:

```
🔴 Scaffolding halted at Phase <N>: <reason>
   Partial state:
   - GitHub repo: <created|skipped>
   - Local clone: <path|none>
   - Vault artifacts: <listed|none>

   Recovery: <one-line how-to-resume>
```

### Phase 4 (customize / make precommit) failure recovery

When Phase 4's `make precommit` fails (lint error, test failure, security finding), the local clone is half-customized. Recover with:

1. **Inspect what broke**: `cd ~/Documents/workspaces/agent-<name> && git diff` shows the customize changes; precommit output names the failing check.
2. **If a sed pattern over-matched** (e.g. rewrote something it shouldn't have): manually revert the bad change in the affected file, re-run `make precommit`. If clean, continue to Phase 5 manually.
3. **If unfixable in <5 min**, abort the scaffold cleanly:
   ```bash
   cd ~/Documents/workspaces && rm -rf agent-<name>            # remove local clone
   gh repo delete bborbe/agent-<name> --yes                    # remove remote (created in Phase 3)
   # Vault artifacts were not yet written (Phase 6 is post-Phase-4); nothing to revert there.
   ```
   Then re-invoke `/launch-agent` with adjusted answers (e.g. pick a different shape, or sharper name).
4. **Report the failure** in your output so the user understands what to fix in the template repo for next time — this is often a template bug, not a per-agent issue.

</output_format>

<related>
- `references/shapes.md` — shape decision matrix
- `references/interview.md` — 45-Q script
- `references/{config-crd,vault-page,goal,scenario,next-directions}-template.{yaml,md}` — output templates
- [[Agent Design Guide]] — full 45-Q checklist (source of truth)
- [[Quick-Launch New Agents]] — parent goal
- [[Claude Managed Agents - Ideas for bborbe Framework]] — architectural rationale + interview-first pattern
- `anthropics/launch-your-agent` — Anthropic's analogous skill (different runtime, same shape)
</related>
