# Scenario Template — `scenarios/001-<happy-path-name>.md`

Renders the first acceptance scenario in the new agent repo. Becomes the canonical smoke test the operator runs before deploy and that CI runs to validate the agent end-to-end.

Placeholders: `<ANGLE_BRACKETED>` — replace during scaffolding.

---

```markdown
# Scenario 001: <HAPPY_PATH_NAME>

**Purpose**: prove the agent works end-to-end on a happy-path input. Run before any deploy; run again after any non-trivial change.

**Setup**:
- Agent `bborbe/<NAME>` deployed to <STAGE> (dev typically)
- Config CRD applied: `kubectlquant -n <STAGE> get config.agent.benjamin-borbe.de <NAME>` shows it
- Producer ready to emit a task (or operator creates one manually via vault edit)

**Acceptance**: <FROM_INTERVIEW_PART_8.2 — one-line success statement>

## Steps

1. **Create the task** (one of these paths):

   **Manual** (operator path):
   ```bash
   vault-cli task create "<TASK_TITLE>" \
     --vault Personal \
     --assignee <NAME> \
     --task-type <NAME>
   ```
   <!-- adjust fields per interview Part 5.1 (Inputs) -->

   **Automated via producer**: <FROM_INTERVIEW_PART_3>
   <!-- e.g. "Wait for next cron tick from recurring-task-creator (Schedule CR: <SCHEDULE_NAME>)" -->

2. **Observe controller pickup**:
   ```bash
   kubectlquant -n <STAGE> logs agent-task-controller-0 --tail=100 | grep "<TASK_ID>"
   ```
   Expected: `published CreateTaskCommand` for this task within 1 poll cycle (~5min).

3. **Observe executor spawn**:
   ```bash
   kubectlquant -n <STAGE> get pods | grep "<NAME>-<TASK_ID_PREFIX>"
   ```
   Expected: a Job pod appears within 30s of the controller publish.

4. **Watch the agent run**:
   ```bash
   kubectlquant -n <STAGE> logs <POD_NAME> --tail=200 -f
   ```
   Expected per interview Part 4 (Phases): planning → in_progress → ai_review → done.

5. **Verify task file updated**:
   ```bash
   cat ~/Documents/Obsidian/Personal/<TASK_PATH>
   ```
   Expected (from interview Part 5.2 — Outputs):
   - Frontmatter: `phase: done`, `status: completed`
   - Body has `## <EXPECTED_SECTION>` with `<EXPECTED_CONTENT_SHAPE>`

## Pass criteria

- [ ] Task transitions all configured phases without `## Failure` section
- [ ] Final phase = `done`, status = `completed`
- [ ] <DOMAIN_SPECIFIC_CHECK_1>  <!-- from interview Part 8.1 per-phase acceptance -->
- [ ] <DOMAIN_SPECIFIC_CHECK_2>
- [ ] No agent-pipeline alerts fired in the 10 min after task completion

## Fail recovery

If the task fails (phase: human_review with `## Failure`):
1. Read the `## Failure` JSON for the error class
2. Common classes: transient infra (retry should help), missing dependency (preflight gap — see [[Fail-Fast Preflight for Tool-Dependent LLM Agents]]), rate limit (wait for window reset), semantic error (task input is wrong shape)
3. Fix the root cause, then re-trigger the task via `vault-cli task set "<TITLE>" status next` + `vault-cli task set "<TITLE>" assignee <NAME>`

## Cleanup

After scenario passes:
- Leave the test task in the vault as a known-good reference (don't delete)
- If repeated runs accumulate test tasks, archive periodically: `vault-cli task complete --reason "scenario 001 regression baseline"`

## Related

- [[<NAME> Agent]] — agent knowledge page
- [[Agent Hub]] — catalog
- [[Agent Pipeline Debug Guide]] — what to check when a scenario fails for non-obvious reasons
```
