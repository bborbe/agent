---
status: completed
tags:
    - dark-factory
    - spec
approved: "2026-09-14T15:55:50Z"
generating: "2026-09-14T16:03:15Z"
prompted: "2026-09-14T16:20:37Z"
verifying: "2026-09-14T19:00:01Z"
completed: "2026-09-15T07:09:49Z"
branch: dark-factory/agent-result-interaction-count
---

## Summary

- Every result an agent publishes already carries the task's frontmatter map as its payload; that map says nothing today about the run — neither how much work it did nor how much human input it consumed.
- This adds two entries to that map: `metrics_agent_turns` (the Claude session's turn total, from the summary the code already parses for the token counts) and `metrics_interaction_count` (the run's human interaction count, evidenced from the run's own session transcript).
- The human count is written **only when it is evidenced**: zero when the transcript records no human-authored entry — that recorded zero is the unattended-delivery claim — and the key is left **absent** when the evidence is unavailable. Absent is not zero.
- `metrics_interaction_count` is the same key the human side already writes; this extends its coverage to agent-executed tasks, it does not rename or replace it. The agent's own turn total stays in a separate key so the two quantities are never conflated.
- Evidence exists and is verified: the CLI reports its session id in the stream, writes its transcript under the config dir the Job already sets, and marks human-authored input with its own provenance field. A headless run carries no such marker; an interactive session always does.
- The counting rules on the two sides are not the same quantity and are not reconciled: the human side counts user-role turns (tool results included), the agent side counts human-authored turns. Recorded, never summed.
- Proof is end-to-end: two real cluster Job runs, one per stage, whose payloads carry different turn counts and an evidenced `metrics_interaction_count: 0`.

## Problem

A task file records what a run produced, but not what the run cost. The published task update — the only artifact that reaches the vault — carries status, phase, assignee and body. Meanwhile the vault's `metrics_interaction_count` measures only human-executed tasks: it is written by vault-cli's `work-on` / `complete` lifecycle, which the controller/executor path never touches, so agent-executed tasks record nothing. `23 Topics/Unattended Execution.md` rules a completed task with no recorded count *indeterminate — not a delivery*, so the fleet's deliveries are invisible to the unattended-delivery measurement, and the goal `[[Unattended Deliveries Are Countable]]` cannot meet its own SC2.

Recording a bare `0` for every agent run is not a fix — it would manufacture unattended deliveries from an unproven assumption, the mirror image of treating an unrecorded count as zero. The value has to be **observed**.

**What the run can observe (established).** Three facts, verified on 2026-09-14 — facts 1 and 3 against real sessions; fact 2 by reproducing the agent's exact invocation locally, because a real Job transcript lives in the container's config dir / the prod PVC and reading it needs exec or a pod. The in-Job config dir and encoded-cwd resolution are therefore confirmed only by the two Post-Deploy ACs, which require a real Job to produce the key at all:

1. The CLI reports its session id in the stream the Job already reads — present on every event of a headless run, including the init event and the terminal result event.
2. The CLI writes a transcript for that session under the config directory the Job sets for it, as `<config-dir>/projects/<encoded-cwd>/<session-id>.jsonl`. A headless run with the agent's exact flags (`--print --output-format stream-json --verbose --strict-mcp-config`, prompt on stdin) produced exactly such a file.
3. The CLI marks provenance on user-role entries. Human-typed input carries a human origin (`origin.kind: human`, typically with a prompt source of `typed` / `queued`); a machine-delivered prompt carries `promptSource: sdk` and no human origin; tool results carry neither. Across every headless/SDK-driven transcript sampled, human-marked entries numbered **zero**; across every interactive session sampled, they were present.

So an unattended run is *evidenced* by its own transcript containing no human-authored entry — an observation, not an assumption.

**What does not exist (established, and it is the central constraint).** The shipped human-side rule cannot be matched literally. vault-cli counts `type:"user"` entries in the task's recorded session logs, and in today's CLI format those entries are overwhelmingly tool results — verified against two completed tasks: one recorded `101` where the session held `102` user-role entries of which `95` were tool results, another recorded `35` against `36` user-role entries. Applied to a machine-driven run, that rule yields at least `1` (the prompt itself) and usually hundreds — never `0`. A literal match would therefore mark every agent-executed task as attended and break the very definition it is meant to serve. The agent side must count the human-authored subset instead, and the divergence is recorded, not reconciled.

**Caveats that stay in the record.** The transcript path and the provenance marker are CLI implementation details, not a published contract: if they drift, the agent writes nothing rather than a guessed zero. The marker is not perfectly complete — one of six human prompts in one sampled interactive session carried no marker at all — so an under-marked human intervention would be reported as unattended; that residual risk is named in Failure Modes rather than engineered away. And a recorded count is a **snapshot at write time**: one sampled task recorded `0` at completion while nine user entries landed in its session afterwards, so a zero means "nothing human at delivery", never "nothing human ever".

## Goal

After this work, every result payload an agent publishes for a Claude-backed run carries two entries: `metrics_agent_turns` — the session's turn total from the CLI's own summary, absent when no count was reported — and `metrics_interaction_count` — the run's human interaction count, evidenced from the run's own transcript, `0` when the transcript records no human-authored entry and absent when the evidence is unavailable. A count already recorded on the task is never lowered by the run. The counting rules on the human side, the agent side, and the Prometheus turn counter are documented as three distinct quantities. Two real Job runs on different stages show different turn counts and an evidenced zero interaction count.

## Non-goals

- Do NOT rename, redefine, or replace `metrics_interaction_count`. It is the shipped key and the human side keeps writing it under its own rule; this work extends its coverage to agent-executed tasks, written through the controller's normal frontmatter merge.
- Do NOT change vault-cli, its counting rule, or the human-side lifecycle. No second writer: the Job emits into its payload only and never writes frontmatter.
- Do NOT reconcile the two counting rules. The human side counts user-role turns (tool results included); the agent side counts human-authored turns. They are different quantities and neither may be inferred from the other's absence.
- Do NOT write a count for a run whose evidence is unavailable. Absence is the honest answer; a guessed zero is the failure this spec exists to prevent.
- Do NOT extend the local file-delivery path with either entry. The published payload is the contract; a local run's file output is unchanged.
- Do NOT add a weekly rollup, a per-task aggregate, or any cross-run summation. One payload carries one run's numbers.
- Do NOT add platform observability: no new Prometheus metric, no per-Job metric, no dashboard, no alert, and no change to the existing turn counter's meaning, name, labels, or data source.
- Do NOT emit either entry for the pi provider. It has no turn counter and no Claude session transcript.
- Do NOT add a config flag, env var, or opt-out to disable either entry. Recording is unconditional — an escape hatch on the goal is itself a regression.
- Do NOT bump `EXECUTOR_VERSION`. It pins the executor Deployment's own image and is not on the path that carries this change to a Job.

## Acceptance Criteria

- [ ] Every payload path carries the turn count: a delivered result whose session reported N turns publishes frontmatter holding `metrics_agent_turns` equal to N — for `done` with a next phase, `done` without one, `in_progress`, `needs_input`, and `failed` — evidence: Ginkgo rows in the existing `KafkaResultDeliverer` suite, one per path, each asserting the published value equals N; `make test` exits 0.
- [ ] The turn count is the session's own total, taken from the end-of-run summary: a step whose runner reports N turns delivers a result carrying N — evidence: Ginkgo row in the `AgentStep` `Run` suite with a stubbed runner returning a summary of N turns, asserting the delivered result info carries N; `make test` exits 0.
- [ ] No reported turn count means no turn entry: a session that reported no turn total (absent), zero, or a negative number publishes frontmatter with no `metrics_agent_turns` key at all — evidence: Ginkgo rows asserting the key is absent from the published map (lookup returns false); `make test` exits 0. Negative evidence: the key count in the published map is 0.
- [ ] The interaction count is evidenced, not assumed: against a transcript with K human-marked user entries the payload carries `metrics_interaction_count` equal to K — rows for K = 0 and K = 2 — and against a transcript whose user entries are all machine-marked (a `sdk`-sourced prompt plus tool results) the payload carries `0` — evidence: Ginkgo rows driving the scan with synthetic transcripts and asserting the published value; `make test` exits 0. A hardcoded `0` fails the K = 2 row.
- [ ] Absent is not zero: when the run's transcript cannot be located or read, or the session id was not captured, the payload carries **no** `metrics_interaction_count` key while `metrics_agent_turns` is still written — evidence: Ginkgo rows asserting the interaction key is absent from the published map and the turn key is present; `make test` exits 0. Negative evidence: the interaction key count in the published map is 0 for those rows.
- [ ] The run never lowers a recorded count: when the task content the run received already records `metrics_interaction_count: 101` and the run's own evidence is zero human entries, the payload does not carry a `metrics_interaction_count` lower than 101 — evidence: Ginkgo rows asserting (a) the published value is either absent or ≥ 101, never `0`, and (b) when the run's own evidence exceeds the recorded count — recorded `101`, observed `103` — the published value is `103`. Row (b) is required: without it a guard that merely suppresses the key whenever it is already present would pass while violating the rule, which is conditional on the run's value being smaller; `make test` exits 0.
- [ ] Both entries are additive: for the same result delivered with and without them, the published key sets differ by exactly the keys this change adds — evidence: Ginkgo row asserting the symmetric difference of the two key sets is exactly `{metrics_agent_turns}` and `{metrics_agent_turns, metrics_interaction_count}` in the two cases; `make test` exits 0.
- [ ] Both values are carried as integers, never as strings — evidence: Ginkgo row asserting each published value's concrete type is an integer type; `make test` exits 0.
- [ ] The Prometheus path is untouched: neither key name appears under the metrics package, and its tests pass unmodified — evidence: `grep -rn 'metrics_agent_turns\|metrics_interaction_count' metrics/` returns no lines (exit 1); `make test` exits 0.
- [ ] The divergence is recorded: `docs/interaction-count.md` defines the agent-side turn count (`metrics_agent_turns` — all turns, from the session summary), defines the human-side counter (`metrics_interaction_count` — user turns, deduplicated per session id, written by vault-cli), states that the agent side writes that key only from evidenced human-authored entries and leaves it absent otherwise, states that the two rules are not comparable and must not be summed or reconciled, and names the Prometheus counter `agent_job_turns_total` as a third, unrelated quantity — evidence: `grep -c 'metrics_agent_turns' docs/interaction-count.md` ≥1; `grep -c 'metrics_interaction_count' docs/interaction-count.md` ≥1; `grep -ci 'not comparable' docs/interaction-count.md` ≥1; `grep -c 'agent_job_turns_total' docs/interaction-count.md` ≥1.
- [ ] `CHANGELOG.md` has a bullet under a `## Unreleased` heading describing both new frontmatter entries — evidence: `grep -n -A5 '## Unreleased' CHANGELOG.md | grep -ci 'interaction\|turns'` returns ≥1.
- [ ] `make precommit` exits 0 at the repository root — evidence: exit code.
- [ ] **Post-Deploy (Rung-2):** a real dev Job run **on a task that records no `metrics_interaction_count` beforehand** publishes an agent result payload on `develop-agent-task-v1-request` — a message with `initiator: agent`, `operation: update` — whose `frontmatter` map carries `metrics_agent_turns` with a value ≥ 1 **and** `metrics_interaction_count` with the value `0` — evidence: the captured message excerpt from that topic showing both keys and their values inside the frontmatter map, taken after the run was triggered. The dispatched task must record no interaction count before the run, or the never-lower guard correctly omits the key and this row fails spuriously.
  - `deploy_check:` `kubectlnukedev -n dev get config.agent.benjamin-borbe.de claude-agent -o jsonpath='{.spec.image}' | grep -Eqv ':(v0\.2\.0|v0\.2\.4)$' && echo FRESH`
  - `deploy_target:` `FRESH`
- [ ] **Post-Deploy (Rung-3):** a real prod Job run **on a task that records no `metrics_interaction_count` beforehand** publishes an agent result payload on `master-agent-task-v1-request` whose `frontmatter` map carries `metrics_agent_turns` with a value ≥ 1 that differs from the dev value **and** `metrics_interaction_count` with the value `0` — evidence: the captured prod message excerpt showing a turn count different from the dev excerpt's and an interaction count of `0`. Two equal turn counts are not acceptable evidence: a hardcoded constant would pass that test.
  - `deploy_check:` `kubectlnukeprod -n prod get config.agent.benjamin-borbe.de claude-agent -o jsonpath='{.spec.image}' | grep -Eqv ':(v0\.2\.0|v0\.2\.4)$' && echo FRESH`
  - `deploy_target:` `FRESH`

**Scenario coverage — no new scenario.** Every behavior above is reachable by unit and integration tests in the implementation prompts, plus the two operator-side cluster observations. No dark-factory scenario harness can reach a real cluster Job, a real Kafka topic, and a real mirrored image; the two Post-Deploy ACs are the E2E layer for this change.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

```bash
make precommit                                                        # exit 0
make test                                                             # unit + integration suites green
grep -rn 'metrics_agent_turns\|metrics_interaction_count' delivery/ claude/ *.go
grep -rn 'metrics_agent_turns\|metrics_interaction_count' metrics/    # expect no lines (exit 1)
grep -c 'metrics_agent_turns' docs/interaction-count.md               # >=1
grep -c 'metrics_interaction_count' docs/interaction-count.md         # >=1
grep -ci 'not comparable' docs/interaction-count.md                   # >=1
grep -c 'agent_job_turns_total' docs/interaction-count.md             # >=1
```

### Operator-executable (runs on host after PR merge, spec verification ladder)

```bash
# 1. Release the library. The maintainer's release watcher cuts the tag pair
#    (.maintainer.yaml: release.autoRelease true; .dark-factory.yaml autoRelease false
#    governs prompt execution only, not releases).
cd ~/Documents/workspaces/agent && git fetch
git tag -l 'v*'     --sort=-v:refname | head -1     # vX.Y.Z
git tag -l 'lib/v*' --sort=-v:refname | head -1     # same number, same commit

# 2. Bump the library pin in the leaf agent whose image the Job runs (agent-claude,
#    today github.com/bborbe/agent v0.87.1), merge, release — then BUILD AND PUSH the
#    image: the release tag alone does not build it.
cd ~/Documents/workspaces/agent-claude && git checkout master && git pull
VERSION=vA.B.C make build upload
docker manifest inspect docker.io/bborbe/agent-claude:vA.B.C >/dev/null && echo OK

# 3. Bump ALL THREE pins in the chart — a Makefile-only bump silently deploys nothing,
#    because the values `tag` is what the Config CR reads:
cd ~/Documents/workspaces/nuke/agent
#    Makefile MIRROR_IMAGES            agent-claude:vA.B.C
#    values-dev.yaml   claude-agent    tag: vA.B.C
#    values-prod.yaml  claude-agent    tag: vA.B.C
grep -rn 'agent-claude:' Makefile values-dev.yaml values-prod.yaml

# 4. Apply per the `Deploy Mirrored Agent Service` runbook (the prod apply needs explicit
#    in-the-moment operator approval):
cd ~/Documents/workspaces/nuke/agent && git fetch && git merge origin/master
BRANCH=dev    KUBECONFIG=~/.kube/nuke-dev  make apply
BRANCH=master KUBECONFIG=~/.kube/nuke-prod make apply

# 5. Dispatch one real llm run per stage to the claude agent (different tasks, so the turn
#    counts differ), then read the result topic with a local Kafka reader against the nuke
#    brokers (VPN up; the topics have one partition each).
cd ~/Documents/workspaces/kafka-topic-reader && go run main.go -- --listen=:18080 \
  --kafka-brokers=192.168.178.41:32159,192.168.178.42:32159,192.168.178.43:32159 \
  --sentry-dsn=https://dummy@dummy.ingest.sentry.io/dummy &
curl -s "http://localhost:18080/read?topic=develop-agent-task-v1-request&partition=0&offset=-100&limit=100&filter=metrics_agent_turns"
curl -s "http://localhost:18080/read?topic=master-agent-task-v1-request&partition=0&offset=-100&limit=100&filter=metrics_agent_turns"
```

The deployed `strimzi-topic-reader` admin route serves the same query in a browser (admin gateway requires Google OAuth): `https://trading.<stage>.nuke.benjamin-borbe.de/admin/strimzi-topic-reader/read?topic=<topic>&partition=0&offset=-100&limit=100&filter=metrics_agent_turns`.

A local `run-task` invocation writes a file and publishes nothing — it is not evidence for the two Post-Deploy ACs.

## Desired Behavior

1. A result delivered from a step that ran a Claude session publishes `metrics_agent_turns` holding that session's turn total, on every status the deliverer publishes.
2. The turn total is the value from the CLI's end-of-run summary — the same summary that already supplies the token counts — for the single session whose result is being delivered. It is never summed across steps or runs.
3. When the summary reports no turn total, or reports zero, or reports a negative number, that entry is omitted entirely.
4. The same result publishes `metrics_interaction_count` holding the number of human-authored entries the run's own session transcript records — `0` when the transcript contains none, which is the unattended-delivery claim, and a positive number when a human did intervene in that session.
5. The interaction entry is written only when the evidence is decisive. A transcript that cannot be located, read, or parsed, or a run whose session id was not captured, produces **no** entry — never a zero. The run's turn count is still published in that case.
6. The run never lowers a count already recorded on the task it received: when the task content carries a recorded interaction count and the run's evidence would write a smaller value, the entry is omitted and the recorded value stands.
7. A provider that has no Claude session publishes neither entry.
8. A run that never delivers a result contributes nothing. A Job killed before delivery (OOM, eviction, timeout, cancellation) produces no agent payload at all, so there is nothing to attach numbers to — that omission is structural, not a policy choice, and it must not be papered over with synthesized values. An agent-reported `failed` run is different: it does deliver, and therefore it does carry both numbers.
9. The three quantities — agent turns, the human-side counter, and the Prometheus turn counter — stay distinct and their divergence is recorded in the repository. Nothing is changed to reconcile them, and everything the payload carries today is unchanged.

## Constraints

- Two frozen frontmatter keys: `metrics_agent_turns` (this change's new key) and `metrics_interaction_count` (the shipped key, whose coverage this change extends). Lowercase, snake_case, no nesting, no version suffix, no alias.
- **Evidence, not assertion.** `metrics_interaction_count` is written from the run's own session transcript, counting the user-role entries the CLI records as human-authored. `0` is written only when the transcript was read and contained no such entry. No code path may write a zero it did not observe — the definition's whole value is that the recorded zero *is* the claim.
- **Absent is a first-class outcome.** Missing evidence yields an absent key, which the vault's definition reads as indeterminate. Do not substitute zero for absence anywhere in the emitter.
- **The human side is not touched.** vault-cli's rule (user-role turns across the task's recorded session logs, deduplicated per session id) and its lifecycle are unchanged. The agent's key is the same key by name only; the counting rules differ and that divergence is recorded, never reconciled.
- **Never lower a recorded count.** The emitter sees the task content the run received; when that content already records an interaction count, the run must not publish a smaller one. This is a guard on the emitter's own output, not a merge: no read-modify-write of any task file, no second writer.
- **Not the Prometheus metric.** Neither entry may be wired into `agent_job_turns_total`, derived from it, or used to rename it or add labels to it.
- **The controller's write-back guard.** The controller merges incoming frontmatter under a field-ownership guard (`controllerOwnedFields` in `MergeFrontmatter`). That guard does not accumulate: for a listed key it keeps the on-disk value and discards the incoming one, and deletes the key when it is absent on disk. Neither key is on that list and neither may be added to it. Because both are written as ordinary incoming keys, their values are applied on write-back; the combining rule (what an incoming run value means next to an on-disk total) belongs to the controller-side task, not to the emitter.
- **Transcript handling is untrusted-input handling.** The transcript path is built from the config directory plus the session id the CLI reports; the session id is validated before it is used in a path, and no path outside that config directory is ever read. The transcript content is read for provenance markers only — never logged, never published, never stored.
- Claude provider only. Both entries are Claude-CLI concepts; the pi provider has no equivalent counter and no such transcript, so its payloads carry neither.
- Payload compatibility: the change is additive. The task schema identifier is unchanged, and every key and value the payload carries today is unchanged.
- No configuration surface: no env var, flag, or opt-out. Recording is unconditional.
- Existing tests that assert today's payload shape keep passing unmodified.
- Assumes: the CLI keeps reporting its session id in the stream and keeps writing a transcript under the config directory it was given; the CLI keeps marking human-authored input distinctly from machine-delivered input; the published frontmatter map is what the controller reads on write-back; the `claude-agent` entry is the Config CR whose image the Job runs on both stages (verified — dev and prod both read `bborbe/agent-claude:v0.2.0`); the result topics are `develop-agent-task-v1-request` (dev) and `master-agent-task-v1-request` (prod), one partition each.

## Failure Modes

| Trigger | Detection | Expected behavior | Recovery | Reversibility | Concurrency |
|---|---|---|---|---|---|
| The CLI stops writing a transcript, moves it, or renames the provenance marker | The interaction entry stops appearing in payloads while the turn entry keeps appearing | No interaction entry is written — absent, never zero. The turn entry is unaffected | Re-derive the location or marker from a live CLI run; the entry resumes | Reversible | n/a |
| The transcript exists but is unreadable or truncated | Same as above: entry absent | No entry written; no error surfaces to the task | None needed for that run; investigate permissions if persistent | Reversible | A transcript read happens after the session exits, so no partial-file race with the CLI |
| The CLI fails to mark a human intervention | A task counted unattended that the operator actually touched — found by hand-verification, not by the emitter | The run reports `0`; this is the named residual risk of trusting the CLI's provenance record | Record it; treat the marker as untrustworthy for that CLI version and stop trusting the zero until re-verified | Not reversible for the affected tasks (the claim was written) | n/a |
| Kafka unreachable when the result is delivered | Existing publish-error log line; no payload for that run | Nothing is published — both numbers are lost with the rest of the result | Re-dispatch the task; the numbers come from the new run | Reversible (re-run) | A second delivery for the same task supersedes the first on write-back — unchanged from today |
| A run's evidenced count is lower than a count already recorded on the task | The emitted entry is absent where a zero might be expected | The recorded value stands; nothing is lowered | None needed; the operator's own sessions remain the source of that value | Reversible | The guard reads the task content the run received, so a concurrent operator session cannot be clobbered by the run |
| Job killed before it delivers (OOM, eviction, timeout, cancellation) | No agent payload for that run; the executor's synthesized failure is the only task update | No numbers for that run — structural. Do not synthesize values | Re-dispatch the task | Irreversible for that run (the numbers are gone) | A mid-run kill leaves no partial entry |
| The agent reports `failed` after a completed session | Payload present with `failed` status | The payload carries both numbers, exactly like every other path | Existing retry / trigger-cap path | Reversible | As today |
| Provider throttles or refuses before the session ends | No session summary and no usable transcript | Both entries are omitted for that run | Re-dispatch after the limit resets | Reversible | n/a |
| A buggy or hostile session reports a negative or absurd turn total | The value never appears in the payload | A negative value is treated as no measurement (entry omitted); a large value is carried as-is within integer range | None needed; the value is untrusted input and is never anything but an integer | Reversible | n/a |
| Clock skew across nodes | Not observable — both values are counts, not timestamps | No effect; nothing orders on them | None | n/a | n/a |

## Security / Abuse Cases

- Both values cross a trust boundary: they originate in a subprocess and its on-disk output, and are republished in a Kafka payload that the controller writes into a vault markdown file. Treat them as untrusted input.
- They are carried as integers, never as strings. A string could smuggle YAML or JSON structure into the frontmatter map that the controller writes back to a file; an integer cannot. Only non-negative integers are written; everything else is treated as "no measurement".
- The transcript path is built from the config directory plus a session id that arrives from the subprocess. The id is validated (expected identifier shape, no separators, no traversal sequences) before any path is built, and the read is confined to the run's own config directory — a hostile id must not be able to make the agent read an arbitrary file.
- The transcript is read for provenance markers and entry kinds only. No transcript content is logged, published, or persisted; the emitted numbers reveal nothing about the prompt or the work.
- Neither value becomes a metric label, a file path component, a log format string, or a map key — no cardinality growth, no injection surface.
- Bounded by construction: two integers and one bounded file read per run. No new network call, no new user input, no unbounded retry or wait.
- Absence is a valid state. No consumer may treat a missing entry as an error, and none may substitute zero for it — that substitution is exactly the manufacturing of deliveries from silence the definition forbids.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Capture the session turn total from the end-of-run summary and the run's session id from the stream; scan the run's own transcript for human-authored entries; carry both into the delivered result info with the absence rules | 2, 3, 4, 5 | 2, 3, 4, 5 | — |
| 2 | Write both entries into the published payload frontmatter ahead of the status switch so they ride every path; keep the change additive, never lower a recorded count, never substitute zero for absence, never emit for a provider without a Claude session | 1, 6, 7, 9 | 1, 6, 7, 8, 9 | prompt 1 |
| 3 | Document both keys, their provenance, and the three-quantity divergence (human side, agent side, Prometheus); add the CHANGELOG bullet | 9 | 10, 11 | — |
| 4 | Operator release chain and per-stage evidence capture — NOT a prompt; runs outside dark-factory | 8 | 12, 13, 14 | prompts 1–3 merged, released, and deployed |

Rationale: prompt 1 establishes both values and their absence rules; prompt 2 cannot run first because it needs values to publish; prompt 3 is documentation-only and independent. The operator row is the release path that carries the merged library to a running Job, and it is the only place the two Post-Deploy ACs can fire — it depends on everything above it.

## Do-Nothing Option

Doing nothing keeps agent-executed tasks invisible to the unattended-delivery measurement: the fleet's deliveries carry no interaction count, the definition reads them as indeterminate, and the goal's SC2 stays unmeasurable for exactly the tasks it was written to count. The tempting shortcut — writing `0` for every agent run — is rejected outright: it would manufacture unattended deliveries from an unproven assumption and make the definition unfalsifiable in the direction that flatters the fleet. The evidence to do it honestly exists and is verified (session id in the stream, transcript on disk, human-authored marker in the transcript), and the cost is one bounded file read, two integers on an existing payload, and one document.

## Open Questions

- None.
