---
status: completed
spec: [053-agent-result-interaction-count]
summary: 'Added docs/interaction-count.md documenting metrics_agent_turns, metrics_interaction_count, and agent_job_turns_total as three not-comparable quantities, plus the spec 053 CHANGELOG bullet under ## Unreleased'
execution_id: agent-exec-218-spec-053-interaction-count-docs
dark-factory-version: v0.193.0
created: "2026-09-14T16:09:52Z"
queued: "2026-09-14T17:54:09Z"
started: "2026-09-14T18:56:41Z"
completed: "2026-09-14T19:00:01Z"
branch: dark-factory/agent-result-interaction-count
---

<summary>
- A new repository document explains the two frontmatter keys an agent result now carries: the session's turn total and the run's human interaction count
- The document names who writes each key, what each one counts, and what an absent key means
- It states plainly that the agent-side rule and the human-side rule count different things and are not comparable — they must never be summed or reconciled
- It records that a recorded zero is a claim ("nothing human at delivery"), while an absent key is indeterminate
- It names the existing Prometheus turn counter as a third, unrelated quantity and links to the document that describes it
- The changelog gains one bullet under `## Unreleased` describing both new frontmatter entries
- No code changes: this prompt is documentation and changelog only
</summary>

<objective>
Record the divergence between the three turn/interaction quantities in the repository — the agent-side turn count, the shipped human-side interaction counter, and the Prometheus turn counter — so no future reader treats them as one number, and add the changelog entry for the two new frontmatter keys. Implements spec 053 Acceptance Criteria 10 and 11.
</objective>

<context>
Read `CLAUDE.md` for project conventions (single-module repo at the repository root).

Coding-plugin docs (read before writing, paths as they exist INSIDE the YOLO container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — `## Unreleased` placement, the required `prefix:` format, and the anti-patterns (missing or wrong prefix, vague descriptions; name the exact thing touched).
- `/home/node/.claude/plugins/marketplaces/coding/docs/git-workflow.md` — the dark-factory-mode CHANGELOG rules: add entries under `## Unreleased`, describe what changed, never prompt filenames.
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md` — §9 Style Rules for `docs/` pages (relative links between docs, no duplicated content, no stale docs).
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — the prompt-execution definition of done, including the CHANGELOG requirement.

Files to read IN FULL before writing (all paths repo-relative):
- `docs/job-metrics.md` — the house style for a short reference document (H1 title, a one-paragraph intro, a table, short sections) and the canonical description of the Prometheus collector families, including `agent_job_turns_total`. The new document must link to it rather than restate its table.
- `docs/task-flow-and-failure-semantics.md` — the other reference-document exemplar in this directory (prose sections, no marketing tone).
- `docs/interaction-count.md` — does NOT exist yet; this prompt creates it.
- `CHANGELOG.md` — read the top 20 lines. The highest released heading is `## v0.88.0` and there is currently NO `## Unreleased` section.
- `specs/in-progress/053-agent-result-interaction-count.md` — the spec this prompt implements. Its "Summary", "Problem", "Goal", "Constraints" and Acceptance Criteria 10/11 are the source of every statement the new document makes; do not invent rules beyond it.

Facts to state (all from the spec — copy the meaning, not necessarily the wording):

1. `metrics_agent_turns` — the agent-side turn count: every conversation turn the Claude session took, taken from the CLI's own end-of-run summary (the same summary that already supplies the token counts). One payload carries one run's number; it is never summed across steps, phases, or runs. A summary that reported no turn total — or a zero or negative one — publishes no key at all.
2. `metrics_interaction_count` — the shipped key, written by both sides under different rules:
   - human side (vault-cli's `work-on` / `complete` lifecycle): user-role turns in the task's recorded session logs, deduplicated per session id, tool results included;
   - agent side (this spec): the user-role entries in the run's own session transcript that the CLI records as human-authored.
3. The agent side writes `metrics_interaction_count` only from evidenced human-authored entries: a recorded `0` means the transcript was read and contained no human-authored entry — that zero is the unattended-delivery claim — and the key is left absent when the evidence is unavailable (no session id, transcript missing, unreadable, or unparseable). Absent is read as indeterminate, never as zero.
4. The run never lowers a count already recorded on the task it received: when the run's own evidence is smaller, the entry is omitted and the recorded value stands. A recorded count is a snapshot at write time — a zero means "nothing human at delivery", never "nothing human ever".
5. The two counting rules are not comparable and must not be summed or reconciled — they count different quantities (all turns vs. human-authored user turns; the human side includes tool results, the agent side does not).
6. `agent_job_turns_total` is a third, unrelated quantity: the process-lifetime Prometheus counter described in `docs/job-metrics.md`, pushed to the PushGateway. It is not per task, is not on the result payload, and is never derived from — or used to compute — either frontmatter key.
</context>

<requirements>

## 1. Create `docs/interaction-count.md`

Write the document with exactly these five sections, in this order. It must contain the literal key names `metrics_agent_turns` and `metrics_interaction_count`, the literal counter name `agent_job_turns_total`, and the literal phrase `not comparable`. Keep it factual and short (roughly 40-70 lines), in the style of `docs/job-metrics.md`: an H1 title, a short intro paragraph, then prose sections (a table is optional).

**`# Interaction Count`** — intro paragraph: two frontmatter keys on a published agent result record how much a run cost in turns and in human attention; they count different things, are written by different writers, and must never be summed or compared.

**`## metrics_agent_turns — the agent-side turn count`**
- What it counts: every conversation turn the Claude session took, from the CLI's end-of-run summary (the same summary that supplies the token counts).
- Who writes it: the agent, into the frontmatter of every result it publishes. One payload carries one run's number — never summed across steps, phases, or runs.
- Absence: a summary reporting no turn total, a zero, or a negative total publishes no key at all. Absent is not zero.
- Provider scope: Claude only. A provider without a Claude session publishes neither this key nor `metrics_interaction_count`.

**`## metrics_interaction_count — the human interaction count`**
- The human side: written by vault-cli's `work-on` / `complete` lifecycle as the number of user-role turns in the task's recorded session logs, deduplicated per session id, tool results included.
- The agent side: written into the published result from the run's own session transcript, counting the user-role entries the CLI records as human-authored. Same key by name only.
- Evidence, not assertion: a recorded `0` means the transcript was read and held no human-authored entry — that zero is the unattended-delivery claim; the key is absent (indeterminate, never zero) when the evidence is unavailable: no session id, or a transcript that cannot be located, read, or parsed.
- Never lowered: when the task content the run received already records a count and the run's own evidence is smaller, nothing is published for that key and the recorded value stands.
- Snapshot: a recorded count is a snapshot at write time — a zero means "nothing human at delivery", never "nothing human ever".

**`## The three quantities are not comparable`**
- State explicitly that the agent-side turn count, the human-side interaction count, and the Prometheus turn counter are three distinct quantities; that the two counting rules for `metrics_interaction_count` count different things (all turns vs. human-authored user turns, tool results included on the human side only); and that they must not be summed, averaged, reconciled, or inferred from one another's absence. Use the phrase `not comparable` verbatim in this section.

**`## agent_job_turns_total — a third, unrelated quantity`**
- Name the Prometheus counter `agent_job_turns_total` as a process-lifetime cumulative counter (not per task, not on any payload), link to `job-metrics.md` (repo-relative link) instead of restating its table, and state that it is never derived from either frontmatter key and neither key is ever derived from it.

Do NOT add: a weekly rollup, a per-task aggregate, any cross-run summation, a dashboard description, or a new metric proposal. Do NOT restate vault-cli's internals beyond the counting rule above, and do NOT claim any consumer behaviour the spec does not state.

## 2. Add the CHANGELOG entry

`CHANGELOG.md` currently has no `## Unreleased` section (the top released heading is `## v0.88.0`). Insert one directly above the highest `## vX.Y.Z` heading — immediately after the SemVer preamble block — with exactly one bullet. If a sibling prompt has already created a `## Unreleased` section, append this bullet to it instead of adding a second section:

```markdown
## Unreleased

- feat: agent results now publish `metrics_agent_turns` (the Claude session's turn total from the CLI's end-of-run summary) and `metrics_interaction_count` (the run's human-authored transcript entries — `0` when the transcript was read and held none, absent when the evidence is unavailable) into the task frontmatter; a count already recorded on the task is never lowered, and a provider without a Claude session publishes neither (spec 053)
```

One bullet, `feat:` prefix (minor bump), no version heading renamed, no other bullet added, and no bash comments or prompt filenames copied in.

## 3. Scope containment

Create/modify ONLY:
- `docs/interaction-count.md` (new)
- `CHANGELOG.md`

Do NOT change any Go file, any test, `README.md`, `docs/job-metrics.md`, or any other document. This prompt ships documentation only; the code changes shipped in the sibling prompts of this spec.

## 4. Self-check before finishing

Re-run the `<verification>` commands and confirm each passes; then re-read `docs/interaction-count.md` against spec 053 Acceptance Criterion 10 and confirm, line by line, that the document: defines the agent-side turn count from the session summary; defines the human-side counter as user turns deduplicated per session id written by vault-cli; states that the agent side writes the key only from evidenced human-authored entries and leaves it absent otherwise; states that the two rules are not comparable and must not be summed or reconciled; and names `agent_job_turns_total` as a third, unrelated quantity.
</requirements>

<constraints>
- Documentation only: no Go code, no test, no chart, no config change in this prompt.
- The two frozen key names are `metrics_agent_turns` and `metrics_interaction_count` — lowercase, snake_case, no nesting, no version suffix, no alias. Do not rename, redefine, or replace `metrics_interaction_count`.
- The document must not present the two counting rules as comparable, must not propose reconciling them, and must not suggest substituting a zero where a key is absent.
- Do not add a weekly rollup, a per-task aggregate, a cross-run summation, a dashboard, an alert, or a new metric — spec Non-goals.
- Do not describe the Prometheus counter as per-task or per-payload, and do not claim either frontmatter key feeds it.
- The changelog entry follows `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`: `## Unreleased` above the highest released heading, one `feat:` bullet describing what was implemented.
- `make precommit` must still exit 0 at the repository root (spec Acceptance Criterion 10).
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root.

```bash
# 1. Spec AC10 — the document defines all three quantities and the divergence:
grep -c 'metrics_agent_turns' docs/interaction-count.md          # must print >= 1
grep -c 'metrics_interaction_count' docs/interaction-count.md    # must print >= 1
grep -ci 'not comparable' docs/interaction-count.md              # must print >= 1
grep -c 'agent_job_turns_total' docs/interaction-count.md        # must print >= 1

# 2. The document links the Prometheus reference instead of restating it:
grep -c 'job-metrics.md' docs/interaction-count.md               # must print >= 1

# 3. Spec AC11 — the changelog bullet sits inside the Unreleased section:
grep -n -A5 '## Unreleased' CHANGELOG.md | grep -ci 'interaction\|turns'   # must print >= 1
grep -n '^## v0.88.0' CHANGELOG.md
# The '## Unreleased' line number must be strictly smaller than the '## v0.88.0' line number.

# 4. Documentation-only scope — no Go source outside the deliverer references the keys
#    (the sibling prompts own the code; this prompt adds no Go change):
grep -rln 'metrics_agent_turns\|metrics_interaction_count' --include='*.go' . | grep -v '^\./delivery/'
# Must print nothing.

# 5. The Prometheus package still carries neither key name:
! grep -rn 'metrics_agent_turns\|metrics_interaction_count' /workspace/metrics/
# Must return zero lines.

# 6. Repo gate (spec Acceptance Criterion 10):
make precommit
# Must exit 0. The Makefile derives ROOTDIR from `git rev-parse --show-toplevel`; this repo
# runs with workflow: direct and hideGit unset, so .git is present and ROOTDIR resolves.
# If the derivation ever returns empty, re-run as: ROOTDIR=/workspace make precommit
```
</verification>
