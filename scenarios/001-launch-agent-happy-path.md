---
status: active
---

# /launch-agent — scaffold a throwaway agent end-to-end

Validates the `/launch-agent` slash command produces a working scaffolded agent: GitHub repo created, local clone customized, vault artifacts written, deploy checklist printed.

**Scope**: this scenario covers the SCAFFOLD only. It does NOT deploy the resulting agent (that's operator decision). The proof-of-life test that a scaffolded agent ACTUALLY RUNS is a separate scenario / vault task.

**Test target**: a throwaway agent name like `agent-test-launch-NNN` where NNN is the current Unix timestamp — avoids collisions and signals it's disposable.

## Setup

- Claude Code session with this plugin installed (`bborbe/agent` v0.71.0+ in marketplace)
- Working directory: any (the plugin sets up its own paths)
- `gh` CLI authenticated to bborbe org (needs repo-create perms)
- Personal Obsidian vault registered with vault-cli (`vault-cli config list` shows it)
- The 4 template repos exist + are flagged `is_template: true`:
  ```bash
  for r in agent-claude agent-code agent-gemini agent-pi; do
    gh api repos/bborbe/$r --jq '.is_template' | grep -q true || echo "MISSING: $r template flag"
  done
  ```

## Steps

1. **Invoke the slash command** in a fresh Claude Code session:

   ```
   /launch-agent test-launch-$(date +%s)
   ```

2. **Verify Phase 1 (interview) runs**: Claude asks the Part 1 motivation questions. Answer minimally:
   - Problem: "Smoke test for /launch-agent scaffolding flow"
   - Manual alternative: "Hand-write Config CRD + Go scaffold + vault page"
   - Do-nothing cost: "Multi-day setup per new agent"
   - Success measure: "Scaffold completes in <30 min"

3. **Verify Phase 1 continues through Parts 2-8**: confirm-gates fire between Parts. Accept minimal answers throughout (this is a smoke test, not a real agent).

4. **Verify Phase 2 (shape recommendation)**: `agent-shape-picker` subagent fires; recommends one shape with reasoning. Accept the recommendation.

5. **Verify Phase 3 (GitHub repo creation)**:
   ```bash
   gh repo view bborbe/agent-test-launch-NNN --json url,isTemplate,description
   ```
   - URL exists
   - `isTemplate: false` (services don't get the template flag)
   - description matches what was passed

6. **Verify Phase 4 (local clone + customize)**:
   ```bash
   ls ~/Documents/workspaces/agent-test-launch-NNN/
   grep -c "module github.com/bborbe/agent-test-launch-NNN" ~/Documents/workspaces/agent-test-launch-NNN/go.mod
   grep -rln "github.com/bborbe/agent-<source-shape>" ~/Documents/workspaces/agent-test-launch-NNN/ --include='*.go'
   ```
   - Local clone exists
   - go.mod module rewritten to new name
   - Zero source-shape references in `.go` files (sed swept clean)

7. **Verify Phase 5 (Config CRD)**:
   ```bash
   ls ~/Documents/workspaces/agent-test-launch-NNN/k8s/agent-test-launch-NNN-config.yaml
   ```
   - Config YAML exists, fields populated from interview

8. **Verify Phase 6 (vault artifacts)**:
   ```bash
   ls ~/Documents/Obsidian/Personal/50\ Knowledge\ Base/Test\ Launch*\ Agent.md
   ls ~/Documents/Obsidian/Personal/23\ Goals/Build\ Test\ Launch*\ Agent.md
   ls ~/Documents/workspaces/agent-test-launch-NNN/scenarios/001-*.md
   ls ~/Documents/workspaces/agent-test-launch-NNN/NEXT-DIRECTIONS.md
   ```
   - All 4 artifacts exist
   - Vault knowledge page + goal page conform to their respective writing guides (no placeholder `<ANGLE_BRACKETS>` left)

9. **Verify Phase 7 (initial commit + push)**:
   ```bash
   cd ~/Documents/workspaces/agent-test-launch-NNN
   git log --oneline -2
   git status --short
   ```
   - One commit (`scaffold via /launch-agent ...`) — the initial-from-template baseline + customizations
   - Working tree clean
   - Branch tracks origin/master, pushed

10. **Verify Phase 8 (deploy checklist printed)**:
    The session output ends with a numbered checklist starting with "Review the generated Config CRD" and including `kubectl apply` + `make buca` steps. No execution — just printed.

11. **Verify `make precommit` passes on the scaffold**:
    ```bash
    cd ~/Documents/workspaces/agent-test-launch-NNN
    make precommit
    ```
    Should PASS — confirms the customize phase didn't break the template's build.

## Pass criteria

- [ ] All 8 phases complete without errors
- [ ] GitHub repo created via `--template` flag (clean history, no fork relationship)
- [ ] Local clone has rewritten module path + zero source-shape import refs
- [ ] Config CRD YAML rendered with placeholders filled
- [ ] Vault artifacts (knowledge page, goal, scenario, NEXT-DIRECTIONS) all exist
- [ ] No `<ANGLE_BRACKETED>` placeholders in any rendered file
- [ ] Initial commit pushed; working tree clean
- [ ] `make precommit` passes on the scaffolded repo
- [ ] Deploy checklist printed (no commands executed automatically)

## Fail recovery

If scaffolding halts mid-phase, the SKILL.md `output_format` section prints a partial-state recovery hint. Common cases:

- **GitHub repo create failed**: `gh` auth issue or repo name collision → adjust + re-invoke `/launch-agent` (the new repo name avoids collision)
- **Customization sed failed**: source-shape name had unexpected chars → manual fixup in local clone, commit, push
- **Vault write failed**: notesmd-cli unavailable or vault-cli config missing → fix and re-run Phase 6 manually using the templates

## Cleanup

After scenario passes:

```bash
# Delete throwaway repo
gh repo delete bborbe/agent-test-launch-NNN --yes

# Remove local clone
rm -rf ~/Documents/workspaces/agent-test-launch-NNN

# Remove vault artifacts (or leave as regression baseline)
notesmd-cli rm --vault Personal "50 Knowledge Base/Test Launch NNN Agent.md"
notesmd-cli rm --vault Personal "23 Goals/Build Test Launch NNN Agent.md"
```

## Related

- [[Quick-Launch New Agents]] — parent goal
- `commands/launch-agent.md` — the slash command this scenario validates
- `skills/launch-agent/SKILL.md` — the 8-phase workflow under test
- [[Agent Pipeline Debug Guide]] — what to check if Phase 3+ k8s steps fail
