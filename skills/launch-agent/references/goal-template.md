# Goal Template — `23 Goals/Build <Name> Agent.md`

Renders the per-agent rollout goal in the Personal vault. Follows the [[Goal Writing Guide]] outcome-vs-mechanism rule for both title and summary first sentence.

Placeholders: `<ANGLE_BRACKETED>` — replace during scaffolding.

---

```markdown
---
status: next
page_type: goal
priority: 3
category: agent/infrastructure
timeline: <YYYY-MM-DD> to <YYYY-MM-DD+28>
themes:
  - "[[Leverage Autonomous Agents]]"
---
Tags: [[Goal]] [[Agent Hub]]

---

<OUTCOME_FIRST_SENTENCE>  <!-- e.g. "Every Sentry issue gets a root-cause + fix-spec written to its task body within 10 minutes of arrival, with low-confidence cases escalated to human review." DO NOT lead with "Build X" / "Refactor Y" — state the world after the agent is running -->

# Impact

**Approach**: scaffold `bborbe/agent-<NAME>` from the `<SHAPE>` template via `/launch-agent` (interview captured <YYYY-MM-DD>), implement <SHAPE>-specific domain logic in `pkg/factory/factory.go` + `pkg/prompts/`, deploy to dev → smoke → prod.

- **Strategic**: <FROM_INTERVIEW_PART_1_PROBLEM>  <!-- why this agent matters -->
- **Theme alignment**: Direct enabler of [[Leverage Autonomous Agents]] — agent #N in the production catalog.
- **Cost of not doing**: <FROM_INTERVIEW_PART_1.3>  <!-- do-nothing cost -->

# Status Summary

**Progress**: 0/4 tasks complete
**Current**: Scaffold landed via `/launch-agent` (template: `bborbe/agent-<SHAPE>`, commit `<INITIAL_SHA>`). Domain logic not yet implemented.
**Next**: [[Implement <Name> Agent Domain Logic]]
**Blockers**: none

# Success Criteria

- [ ] `bborbe/agent-<NAME>` exists, builds green on CI
- [ ] Domain logic implemented in `pkg/factory/factory.go` + `pkg/prompts/<phase>.md`
- [ ] Deployed to dev; first task processed end-to-end (scenario 001 green)
- [ ] Deployed to prod; <SUCCESS_MEASURE_FROM_INTERVIEW_PART_1.4> verified for 1 week

# Non-goals

<!-- from interview Part 4.5 — explicit out-of-scope -->
- <NON_GOAL_1>
- <NON_GOAL_2>
- <NON_GOAL_3>
- **Building a watcher for this agent** — if a new producer is needed, separate goal (`/launch-watcher` is future work)

# Tasks

- [ ] [[Implement <Name> Agent Domain Logic]] — factory + prompts + tests
- [ ] [[Deploy <Name> Agent to Dev]] — `BRANCH=dev make buca` + apply Config CRD + verify scenario 001
- [ ] [[Smoke Test <Name> Agent in Dev]] — observe for 1 week; tune resource limits / retry budget
- [ ] [[Promote <Name> Agent to Prod]] — `BRANCH=prod make buca` + apply Config CRD; monitor <SUCCESS_MEASURE> for 1 week

# Related

**Themes**: [[Leverage Autonomous Agents]]
**Documentation**: [[<NAME> Agent]] (knowledge page) · [[Agent Hub]] (catalog) · [[Agent Design Guide]] (45-Q checklist used to interview this agent)
**Template**: [[<SHAPE> Agent]] — base template
**Parent enablement**: [[Quick-Launch New Agents]] — the goal that built the `/launch-agent` plugin

# Risk Management

**Risk**: Template scaffolding has gaps the interview didn't capture — agent fails on first real task.
- **Mitigation**: Scenario 001 (smoke test) runs against real input before deploy; surfaces gaps early.

**Risk**: Cost overrun — interview's cost estimate (<COST>) was rough; production usage may diverge.
- **Mitigation**: Prometheus token-burn metrics + per-task cost logging from day 1; revisit budget after first week in prod.
```
