---
status: approved
spec: [053-agent-result-interaction-count]
created: "2026-09-14T16:09:52Z"
queued: "2026-09-14T17:54:09Z"
branch: dark-factory/agent-result-interaction-count
---

<summary>
- Every result an agent publishes for a Claude-backed run now carries two extra frontmatter entries: the session's turn total and the run's human interaction count
- Both entries are written before the status is applied, so they ride every published path — done with a next phase, done in place, in progress, needs input, and failed
- A number the run did not observe is simply not written: the key stays absent instead of being published as a zero
- A count already recorded on the task the run received is never lowered — when the run's own evidence is smaller, the recorded value stands
- Everything else in the published payload is unchanged: the change is purely additive, so existing consumers see exactly today's keys plus these two
- The local file-delivery path is untouched: the published message is the contract, and a local run's file output keeps its current shape
- The values are written as plain integers, never as strings, so nothing can smuggle structure into the frontmatter map
- The Prometheus counters are untouched: neither key name appears anywhere under the metrics package
- No config, flag, or opt-out: recording is unconditional
</summary>

<objective>
Publish the run's two observed counts — the session's turn total and the evidenced human interaction count — into the task-update frontmatter that the Kafka result deliverer sends, ahead of the status switch so they ride every path, with absence preserved and a recorded count never lowered. Implements spec 053 Desired Behaviors 1, 6, 7 and 9 (DB 4's publish-side half — the evidenced value is carried into the payload, never derived here; the capture half shipped in the sibling prompt).
</objective>

<context>
Read `CLAUDE.md` for project conventions (single-module repo at the repository root, `module github.com/bborbe/agent`; `make test` / `make precommit` run from the repository root).

Coding-plugin docs (read before editing, paths as they exist INSIDE the YOLO container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega suites, external test packages (`*_test`), counterfeiter mocks, coverage >= 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments for the new helper.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — funlen 80, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules.

Files to read IN FULL before editing (all paths repo-relative):
- `delivery/result-deliverer.go` — THE production file this prompt changes. `kafkaResultDeliverer.DeliverResult` builds the published `agentlib.Task.Frontmatter` from `ParseMarkdownFrontmatter(generated)` and then calls `d.applyResultFrontmatter(frontmatter, result)` (the status switch) and `d.stampTargetVault(frontmatter)`. `applyResultFrontmatter` is the in-package precedent for a helper that mutates the frontmatter map in place; `stampTargetVault` is the precedent for reading a value out of `d.originalContent`'s frontmatter.
- `delivery/result-deliverer_test.go` — THE test file this prompt changes. `Describe("KafkaResultDeliverer")` has a shared `BeforeEach` (mocks `sender` / `clock` / `generator`, sets `originalContent` and `taskID`) and a `JustBeforeEach` that builds `deliverer = delivery.NewKafkaResultDelivererWithSender(sender, taskID, originalContent, generator, clock)`. Published frontmatter is asserted via `_, cmdObj := sender.SendCommandObjectArgsForCall(0)` then `fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})`.
- `delivery/content-generator.go` — the content generators (`NewFallbackContentGenerator`, `NewPassthroughContentGenerator`, `NewSectionContentGenerator`) and `fileResultDeliverer`'s sibling in `result-deliverer.go`. Read to confirm they stay untouched: the entries belong to the PUBLISHED payload only, never to a local file write.
- `delivery/markdown.go` — `ParseMarkdownFrontmatter(content string) (map[string]any, string)`; returns an empty map when there is no frontmatter or the YAML does not parse.
- `agent_status.go` (repository root) — `AgentResultInfo`, which the sibling prompt has just widened with `AgentTurns *int64` and `InteractionCount *int64`.
- `agent_task-frontmatter.go` (repository root) — `TaskFrontmatter` (`map[string]interface{}`) and its typed accessor `Int(key string) (int, bool)`, which accepts both `int` (YAML-decoded) and `float64` (JSON-decoded) values. Reuse it — do NOT invent a new accessor.
- `specs/in-progress/053-agent-result-interaction-count.md` — the spec this prompt implements (Goal, Desired Behavior, Constraints, Failure Modes, Security).

**Precondition (the sibling prompt must have landed on this branch):** `agent_status.go` must already declare `AgentTurns *int64` and `InteractionCount *int64` on `AgentResultInfo`, and `agent_runner.go` must already forward them from `Result`. If either is missing, STOP and report a precondition failure — do NOT add those fields yourself, and do NOT reach into the `claude` package.

Load-bearing facts, verified against this repo:

1. The two frozen key names are `metrics_agent_turns` (this spec's new key) and `metrics_interaction_count` (the shipped key whose coverage this change extends). Lowercase, snake_case, no nesting, no version suffix, no alias. Do NOT rename, redefine, or replace `metrics_interaction_count`.
2. `DeliverResult` reaches `base.ParseEvent(ctx, task)` and then `d.commandObjectSender.SendCommandObject(ctx, commandObject)`. In tests, the published frontmatter map is the JSON-decoded `map[string]interface{}` — so a number that was written as an `int64` reads back as a `float64`. Assert numerically (`BeNumerically("==", 7)`), never with a `BeAssignableToTypeOf(int64(0))` matcher, which would fail on the decoded map.
3. Both entries are optional per key: the turn count can be present while the interaction count is absent (the run's evidence was unavailable), and vice versa. They are independent — never tie one to the other.
4. `d.originalContent` is the task markdown the run received; `stampTargetVault` already reads it via `ParseMarkdownFrontmatter(d.originalContent)`. The never-lower guard reads the recorded count from the same place.
5. Neither key may be added to the controller's `controllerOwnedFields` guard in `MergeFrontmatter` — that guard lives in another repository and is not touched by this prompt. Both keys are ordinary incoming keys here and their values are applied on write-back.
</context>

<requirements>

## 1. Add the frozen key constants and the two helpers to `delivery/result-deliverer.go`

Add the constants near the top of the file (after the imports, before the first exported constructor):

```go
// metricsAgentTurnsKey and metricsInteractionCountKey are the frozen frontmatter keys
// this deliverer publishes for a Claude-backed run (spec 053). Lowercase, snake_case,
// no nesting, no version suffix, no alias.
const (
	metricsAgentTurnsKey       = "metrics_agent_turns"
	metricsInteractionCountKey = "metrics_interaction_count"
)
```

Add the two unexported functions after the `applyResultFrontmatter` method's closing brace, next to `stampTargetVault`:

```go
// applyResultMetrics writes the run's observed counts into the published frontmatter.
// It runs BEFORE applyResultFrontmatter so the entries ride every status path the
// deliverer publishes.
//
// Both values are optional: a nil field means the run produced no measurement (the
// turn summary reported nothing positive, or the transcript evidence was
// unavailable) and its key is omitted entirely — absence is never substituted with a
// zero. Values are written as plain integers; nothing else about the payload changes.
//
// A count already recorded on the task the run received is never lowered: when the
// recorded value is higher than the run's own evidence, the run's value is discarded
// and the recorded one stands. This is a guard on this deliverer's own output, not a
// merge — no read-modify-write of any task file, no second writer.
func (d *kafkaResultDeliverer) applyResultMetrics(
	frontmatter agentlib.TaskFrontmatter,
	result agentlib.AgentResultInfo,
) {
	if result.AgentTurns != nil {
		frontmatter[metricsAgentTurnsKey] = *result.AgentTurns
	}
	if result.InteractionCount == nil {
		return
	}
	if recorded, ok := recordedInteractionCount(d.originalContent); ok &&
		*result.InteractionCount < recorded {
		glog.V(2).Infof(
			"task %s: keeping recorded %s=%d, run observed %d",
			d.taskID,
			metricsInteractionCountKey,
			recorded,
			*result.InteractionCount,
		)
		return
	}
	frontmatter[metricsInteractionCountKey] = *result.InteractionCount
}

// recordedInteractionCount reads the interaction count already recorded on the task
// content the run received. ok is false when the key is absent, unparseable, or does
// not hold a non-negative integer.
func recordedInteractionCount(originalContent string) (int64, bool) {
	fm, _ := ParseMarkdownFrontmatter(originalContent)
	frontmatter := agentlib.TaskFrontmatter{}
	for k, v := range fm {
		frontmatter[k] = v
	}
	recorded, ok := frontmatter.Int(metricsInteractionCountKey)
	if !ok || recorded < 0 {
		return 0, false
	}
	return int64(recorded), true
}
```

Behavior contract (do not deviate):

- `AgentTurns` non-nil → the key is set to that integer, whatever the status. Nil → the key is not written by this code at all (a value already present in the incoming frontmatter is left as it is).
- `InteractionCount` nil → nothing is written and no guard runs; the key is not invented.
- `InteractionCount` non-nil and the task content the run received records no count (or a non-integer one) → the run's value is written.
- `InteractionCount` non-nil and the recorded value is strictly greater → the run's value is NOT written; the recorded value that came in with the incoming frontmatter stands. Nothing is lowered, and nothing is deleted.
- `InteractionCount` non-nil and the recorded value is equal or smaller → the run's value is written (so a run that observed more than the recorded count publishes its larger observation).
- Neither helper returns an error: absence and a suppressed value are ordinary outcomes, not failures.

## 2. Call it ahead of the status switch in `DeliverResult`

In `kafkaResultDeliverer.DeliverResult`, insert exactly one call line immediately BEFORE the existing `d.applyResultFrontmatter(frontmatter, result)` line:

```go
	d.applyResultMetrics(frontmatter, result)

	d.applyResultFrontmatter(frontmatter, result)
```

Nothing else in `DeliverResult` changes. `d.stampTargetVault(frontmatter)` stays exactly where it is. No new imports are needed (`ParseMarkdownFrontmatter` is same-package, `agentlib` and `glog` are already imported). `DeliverResult` and the new helpers must each stay under the funlen limit of 80 lines.

## 3. Tests — `delivery/result-deliverer_test.go`

Add a new top-level `Context("metrics frontmatter (spec 053)", ...)` block inside `Describe("KafkaResultDeliverer")`, inserted before that block's closing `})` (after the last existing `Context`). Reuse the shared `BeforeEach` / `JustBeforeEach` fixtures and the mock `libmocks.AgentContentGenerator`; a small local helper closure that returns the published frontmatter map keeps the rows short:

```go
	publishedFrontmatter := func() map[string]interface{} {
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		return fm
	}
```

Use `collection.Ptr(int64(...))` (`github.com/bborbe/collection`, already a dependency) for the pointer fields — do not hand-write pointer helpers.

Rows (names as given — the spec's acceptance criteria are the evidence these rows must produce):

1. **AC1 — every payload path carries the turn count.** Five `It` rows, one per status path, each delivering `agentlib.AgentResultInfo{Status: ..., NextPhase: ..., AgentTurns: collection.Ptr(int64(7)), InteractionCount: collection.Ptr(int64(0))}` and asserting `fm["metrics_agent_turns"]` is `BeNumerically("==", 7)`:
   - `AgentStatusDone` with `NextPhase: "done"`;
   - `AgentStatusDone` with an empty `NextPhase` (in-place save);
   - `AgentStatusInProgress`;
   - `AgentStatusNeedsInput`;
   - `AgentStatusFailed`.
2. **AC3 — no reported turn count means no turn entry.** `AgentTurns: nil`; assert the key is absent from the published map by lookup (`_, ok := fm["metrics_agent_turns"]; Expect(ok).To(BeFalse())`) — not by a string comparison. The zero/negative cases are handled at the source and covered by the sibling prompt's rows.
3. **AC4 — the interaction count is evidenced, not assumed.** Two rows: `InteractionCount: collection.Ptr(int64(0))` → published value `BeNumerically("==", 0)`; `InteractionCount: collection.Ptr(int64(2))` → `BeNumerically("==", 2)`. A hardcoded `0` fails the second row.
4. **AC5 — absent is not zero.** `AgentTurns: collection.Ptr(int64(7))` with `InteractionCount: nil`; assert the interaction key is absent (`_, ok := fm["metrics_interaction_count"]; Expect(ok).To(BeFalse())`) while the turn key is present and equal to 7.
5. **AC6a — the run never lowers a recorded count.** Set `originalContent` to a task whose frontmatter records `metrics_interaction_count: 101`, have the generator return content that also carries `metrics_interaction_count: 101`, deliver with `InteractionCount: collection.Ptr(int64(0))`; assert the published value is `BeNumerically("==", 101)` and `NotTo(BeNumerically("==", 0))`.
6. **AC6b — a larger observation is published.** Same recorded `101` on both the original content and the generated content, deliver with `InteractionCount: collection.Ptr(int64(103))`; assert the published value is `BeNumerically("==", 103)`. This row is required: a guard that merely suppressed the key whenever it is already present would pass AC6a while violating the rule, which is conditional on the run's value being smaller.
7. **AC7 — both values are carried as numbers, never as strings.** Deliver with both fields set; assert each published value is `BeNumerically("==", N)` and `NotTo(BeAssignableToTypeOf(""))`. (The cqrs JSON round-trip decodes JSON numbers as `float64`, so assert numerically here; the concrete `int64` type is asserted at the source in the sibling prompt's `AgentStep` rows.)
8. **AC8 — both entries are additive.** Deliver the same result twice through two deliverers built on the same `originalContent` and generated content — once with both fields nil (baseline) and once with both set — capture each published map's key set as a `map[string]struct{}`, and assert the symmetric difference's sorted keys equal exactly `[]string{"metrics_agent_turns", "metrics_interaction_count"}`. No other key may appear or disappear.

## 4. Scope containment

Edit ONLY these two files:
- `delivery/result-deliverer.go`
- `delivery/result-deliverer_test.go`

Do NOT touch: `delivery/content-generator.go` (the local file-delivery path and the content generators must not carry either entry — the published payload is the contract, a local run's file output is unchanged), `delivery/markdown.go`, `fileResultDeliverer` in the same file (its behavior is unchanged: it writes the generated content verbatim), `noopResultDeliverer`, `metrics/`, `claude/`, `agent_status.go`, `agent_task-frontmatter.go` (reuse `Int(key)`, add no accessor), `mocks/`, or `CHANGELOG.md` (a sibling prompt of this spec owns the changelog entry — do not add one here).

## 5. Self-check before finishing

Re-run the `<verification>` commands and confirm each passes; then walk spec 053's Desired Behaviors 1, 6, 7, 9 and the Failure Modes rows "A run's evidenced count is lower than a count already recorded on the task" and "The agent reports `failed` after a completed session" against the change, and confirm each of these explicitly:

- Every status path published by `applyResultFrontmatter` carries the turn count when the run reported one.
- No code path writes a zero it did not observe, and no code path writes a key whose value is absent.
- The recorded count is never lowered, and a larger observation is still published.
- The published key set is exactly today's keys plus the two new ones — nothing else moved.
- Neither key name appears anywhere under `metrics/`.
</requirements>

<constraints>
- Two frozen frontmatter keys: `metrics_agent_turns` (this change's new key) and `metrics_interaction_count` (the shipped key, whose coverage this change extends). Lowercase, snake_case, no nesting, no version suffix, no alias.
- **Evidence, not assertion.** `metrics_interaction_count` is published only from the value the run observed; `0` is published only when the run's transcript was read and contained no human-authored entry. No code path may publish a zero it did not observe.
- **Absent is a first-class outcome.** Missing evidence yields an absent key, which the vault's definition reads as indeterminate. Do not substitute zero for absence anywhere in the emitter, and do not treat a missing entry as an error.
- **Never lower a recorded count.** When the task content the run received already records an interaction count and the run's evidence is smaller, the entry is omitted and the recorded value stands. This is a guard on the emitter's own output, not a merge: no read-modify-write of any task file, no second writer.
- **The human side is not touched.** vault-cli's counting rule and lifecycle are unchanged; the agent's key is the same key by name only, and the two counting rules are never reconciled, summed, or inferred from one another.
- **Not the Prometheus metric.** Neither entry may be wired into `agent_job_turns_total`, derived from it, or used to rename it or add labels to it. Neither key name may appear under `metrics/`.
- **The controller's write-back guard.** Neither key is on the controller's `controllerOwnedFields` list and neither may be added to it (that list lives in another repository and is out of scope here).
- **Additive only.** The task schema identifier is unchanged; every key and value the payload carries today is unchanged; existing tests that assert today's payload shape keep passing unmodified.
- Values are carried as integers, never as strings — a string could smuggle YAML or JSON structure into the frontmatter map the controller writes back to a file.
- No configuration surface: no env var, flag, or opt-out. Recording is unconditional.
- Claude provider only: a provider that produces no counts (the pi provider, and any run whose evidence was unavailable) publishes neither entry — that falls out of the nil fields; do NOT add a provider check.
- Error handling uses `github.com/bborbe/errors` where an error is returned; the new helpers return no error by design and must not log at warning level for an absent or suppressed value.
- Ginkgo v2 / Gomega in the external `delivery_test` package; counterfeiter mocks only (`libmocks.AgentContentGenerator`, `cqrsmocks.CDBCommandObjectSender`) — never hand-write mocks.
- Coverage for the new code (`applyResultMetrics`, `recordedInteractionCount`) must be >= 80%; the rows above exercise the turn-present/turn-absent, interaction-present/absent, recorded-higher, recorded-lower, and additivity branches.
- Line length limit is 100 characters (golines runs in `make format`); funlen limit is 80 lines.
- Do NOT commit — dark-factory handles git.
- Do NOT touch `CHANGELOG.md` — a sibling prompt of this spec owns the changelog entry.
</constraints>

<verification>
Run from the repository root (single-module repo; there is no `delivery/Makefile`).

```bash
# 1. Targeted delivery package tests (fast iteration — run after each edit):
go test -mod=mod ./delivery/ -v > /tmp/delivery-test.log 2>&1
# Must exit 0. The new Ginkgo rows must appear in the output:
grep -E "metrics frontmatter \(spec 053\)|AC1|AC3|AC4|AC5|AC6a|AC6b|AC7|AC8" /tmp/delivery-test.log
# Each pattern must match at least one line.

# 2. Root module still green (spec Verification "make test"):
make test
# Must exit 0.

# 3. AC9 — the Prometheus path is untouched:
! grep -rn 'metrics_agent_turns\|metrics_interaction_count' /workspace/metrics/
# Must return zero lines. Plain grep exits 1 when nothing matched; the leading `!` inverts
# that to success (exit 0), and fails the step when a line is found.

# 4. Both frozen keys exist exactly once as constants and are used by the deliverer:
grep -rn 'metrics_agent_turns\|metrics_interaction_count' /workspace/delivery/result-deliverer.go
# Must show the two constant declarations plus their use sites — nothing else.

# 5. The local file-delivery path carries neither entry (no metrics write in the generators):
! grep -rn 'metrics_agent_turns\|metrics_interaction_count' /workspace/delivery/content-generator.go
# Must return zero lines.

# 6. Full pipeline — must exit 0 (runs ensure + format + generate + test + lint + license):
make precommit
# The Makefile derives ROOTDIR from `git rev-parse --show-toplevel`; this repo runs with
# workflow: direct and hideGit unset, so .git is present and ROOTDIR resolves. If the
# derivation ever returns empty, re-run as: ROOTDIR=/workspace make precommit
```
</verification>
