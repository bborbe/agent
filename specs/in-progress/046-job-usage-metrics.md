---
status: verifying
tags:
    - dark-factory
    - spec
approved: "2026-08-01T21:52:23Z"
generating: "2026-08-01T21:52:24Z"
prompted: "2026-08-01T22:06:19Z"
verifying: "2026-08-01T22:31:54Z"
branch: dark-factory/job-usage-metrics
---

## Summary

- Capture the LLM token counts and turn count that the Claude CLI already reports at the end of every agent session, instead of discarding them.
- Surface those five numbers on the parsed session result so any caller can read them.
- Add one recording method to the shared job-metrics interface that feeds two new counters: tokens by kind, and conversation turns.
- Both counters are pre-initialized at zero so fleet dashboards and alerts work before the first job runs.
- Deliberately does NOT record the CLI's reported dollar cost — under a non-Anthropic provider that number is fiction.

## Problem

The subscription decision between two LLM plans (MiniMax, sold as requests per rolling 5-hour window; GLM, sold as prompts per 5-hour window with a 2–3x multiplier) is blocked on a single unknown: how much the agent fleet actually consumes. Nobody can compare the plans, size a quota, or predict a throttle, because the fleet's real burn rate has never been measured. The measurement data already arrives — the Claude CLI emits a token and turn summary at the end of every session — and the code throws it away while parsing. A Prometheus PushGateway is already wired up and already receives per-job run, status, and duration metrics from the same code path, so the only thing missing is keeping the numbers and pushing them.

## Goal

After every agent job that runs a Claude Code session, the tokens it consumed (split into fresh input, output, cache-read, and cache-creation) and the number of conversation turns it took are available as Prometheus counters on the existing per-job push job, alongside the run/status/duration metrics already published. Summing those counters across the fleet over a rolling window answers "how many requests and how many tokens per 5 hours" without any new infrastructure.

## Non-goals

- Do NOT record the CLI's `total_cost_usd`. The Claude CLI computes cost at Anthropic list pricing; pointed at a different provider's base URL it reports a counterfactual number, not money actually spent. Seeding a wrong number into a cost dashboard is worse than having no cost dashboard. Tokens and turns are provider-neutral; cost is not. Invariant — if a future consumer needs cost, it must come from the provider's own billing data, and that is a separate spec.
- Do NOT wire the new recording call into consumer binaries. The call sites live in a separate repository and are an explicit follow-up.
- Do NOT touch the Pi harness — it does not run Claude Code and emits no usage summary.
- Do NOT create Grafana dashboards, recording rules, or alerting rules.
- Do NOT provision or reconfigure any infrastructure. The PushGateway and the per-job push wiring already exist.
- Do NOT add an `agent` label to the new metrics. Per-agent breakdown already comes free from the PushGateway job name. Invariant — adding it would duplicate an existing dimension; if a future consumer needs a different breakdown, that's a separate spec.
- Do NOT add a config flag, env var, or opt-out to disable usage recording. Recording is unconditional — an escape hatch on the goal is a regression. If a future deployment genuinely must suppress it, that's a separate spec.
- Do NOT add histograms, summaries, or per-model/per-session labels for tokens. Counters only.

## Desired Behavior

1. While parsing the CLI's streamed session output, the terminal result event's usage summary is captured: fresh input tokens, output tokens, cache-creation input tokens, cache-read input tokens, and the session's turn count. All five are non-negative integers.
2. The parsed session result — the `ClaudeResult` type returned by the runner, today carrying only `Result string` — exposes those five values to its caller. Field naming and JSON tags on `ClaudeResult` are agent-decided at impl time, but the wire names read from the CLI output are fixed by the CLI: `usage.input_tokens`, `usage.output_tokens`, `usage.cache_creation_input_tokens`, `usage.cache_read_input_tokens`, and top-level `num_turns`. Widening the unexported `scanOutput` signature to carry the values out is expected.
3. A session whose terminal result event carries no usage object, or omits individual usage fields, still parses successfully: the missing values read as 0 and the session result text is returned exactly as today. Absent usage is never an error and never aborts a run.
4. When more than one result event carries a usage object, the last usage object wins. **This is independent of the result text's own last-wins rule.** Today `claude-runner.go` gates result-text capture on `event.Type == "result" && event.Result != ""`; usage capture must NOT be folded into that same condition. A second result event carrying a usage object but an empty `result` string updates the usage values while leaving the previously captured result text intact.
5. The shared job-metrics interface gains one method, named `RecordUsage`, that records a full usage summary for a finished job in a single call: the four token counts and the turn count. Callers never pass metric label strings; the label values are internal to the metrics package.
6. Constructing the job metrics registers two additional collectors on the caller-owned registry: a token counter named `agent_job_tokens_total` carrying exactly one label `type` with the four values `input`, `output`, `cache_read`, `cache_creation`, and an unlabeled turn counter named `agent_job_turns_total`. Each has its own non-empty, distinct help text. All four token label combinations and the turn counter are pre-initialized to zero at construction, before any job has run.
7. Recording a usage summary increases each token counter by its matching count and the turn counter by the turn count. A negative value is skipped for that counter — the process never panics and the other counters in the same call still record. Negative is the only unrepresentable case reachable from integer wire fields; do not add handling for NaN, overflow, or other float pathologies.
8. Everything already published stays byte-identical in name, label set, and semantics: the run counter, the last-run timestamp gauge, and the duration histogram are untouched, as is the PushGateway job-name helper.

## Constraints

- Error handling follows the repo convention: `github.com/bborbe/errors` with context wrapping. No `fmt.Errorf`, no bare `return err`.
- Tests follow the conventions already present in the touched packages: Ginkgo v2 / Gomega, Counterfeiter fakes, registry assertions via `registry.Gather()` on a fresh `prometheus.NewRegistry()`.
- The `claude` package is tested externally (`package claude_test`); the parser is unexported and is reached through the PATH shell shim already established by `writeShim` in `claude/claude-runner_test.go`. Use that existing shim pattern — do not add an in-package `package claude` test file to reach `scanOutput` directly.
- Every new exported type, field, method, and function carries a GoDoc comment.
- Both new counter names end in `_total`. Registration panics otherwise, and the panic happens at startup in every consumer.
- Counter pre-initialization with `.Add(0)` is mandatory, following the pattern already used for the run counter's terminal statuses. Without it `rate()` returns no-data rather than zero for unseen label values, and alerts built on it silently never fire — neither when healthy nor when broken. Reference: `docs/dod.md`, and the counter-pre-initialization and help-string-quality rules in the Go Prometheus metrics guide at `~/Documents/workspaces/coding/docs/go-prometheus-metrics-guide.md`.
- Adding a method to the job-metrics interface is a breaking change for external implementers. The Counterfeiter fake in the repo must be regenerated so it satisfies the widened interface.
- The registry is caller-owned and must not be swapped for a global/default registry. Registration failures continue to panic — they are startup-time programmer errors.
- Metric and label naming stays consistent with the existing family prefix `agent_job_`.

## Failure Modes

| Trigger | Detection | Expected behavior | Recovery | Reversibility | Concurrency |
|---|---|---|---|---|---|
| CLI emits a terminal result event with no usage object | Token counters stay flat while the run counter advances | Parse succeeds, all five values are 0, result text returned unchanged | None needed; investigate CLI version if persistent | Reversible (read-only parse) | n/a |
| CLI renames or drops a usage field (schema drift, e.g. a new CLI version) | The affected token series stops growing while sibling series keep growing | The unknown field is ignored, the affected value reads 0, the run still succeeds | Update the parser for the new field name | Reversible | n/a |
| A usage value arrives negative or non-numeric | The affected counter does not advance; other counters in the same call do | That single counter increment is skipped; no panic, no error returned | None needed; the sibling values remain trustworthy | Reversible | n/a |
| A stream line is malformed JSON | Existing behavior: the line is skipped during scanning | Unchanged from today — malformed lines never abort parsing | None needed | Reversible | n/a |
| Job crashes or is killed before the terminal result event | No usage series for that push job in the window | No usage is recorded for that run; the run/status metrics behave exactly as today | Rerun the job | Irreversible for that run's usage (the numbers are lost) | Mid-run crash records nothing rather than a partial summary |
| PushGateway unreachable when the finished job pushes | Existing push-error log line; the job's series is missing from the gateway | Unchanged from today — the push failure is non-fatal and the job still reports its result | Gateway restored; next run's push lands | Irreversible for that run's sample | Two jobs pushing under the same job name overwrite each other's grouping — unchanged from today, not introduced here |
| Provider rate-limits or refuses the session before any turn completes | No terminal result event | Same path as "crashes before the terminal result event": no usage recorded, run status still published | Rerun after the limit resets | Irreversible for that run's usage | n/a |
| Extremely large token count in a single session | Counter value visible in the gateway | Recorded as-is; a float64 counter represents integers exactly up to 2^53, far above any real session | None needed | n/a | n/a |
| Duplicate registration of the new collectors on one registry | Panic at construction, before any job work | Fail fast at startup, as today for the existing collectors | Fix the wiring that registered twice | Reversible | Construction is once-per-process |
| Clock skew across fleet nodes | Timestamps on pushed samples disagree | Counters are monotonic and skew-insensitive; only the existing last-run gauge is time-sensitive and it is untouched | None needed | n/a | n/a |

## Security / Abuse Cases

- The parsed usage numbers come from a subprocess's stdout — a trust boundary. They are consumed as integers only: they never become label values, file paths, log format strings, or map keys, so there is no injection or cardinality-explosion vector. The `type` label values are compile-time constants in the metrics package.
- Label cardinality is bounded at exactly four token series plus one turn series per push job, fixed at compile time. No user-controlled or session-controlled value ever becomes a label.
- A hostile or buggy subprocess can only report wrong numbers (too large, negative, zero). Negative values are skipped rather than passed to a counter, which would otherwise panic and take the job down — a denial-of-service path from untrusted stdout.
- No new network calls, no new files read or written, no new user input surface, no unbounded retry or wait: parsing remains a single forward pass over already-bounded scanner input, and recording is in-memory counter arithmetic.
- Token counts are not secrets and reveal no prompt content.

## Acceptance Criteria

- [ ] A stream-json fixture whose terminal result event carries `usage` with all four token fields plus `num_turns` parses into a session result exposing exactly those five values — evidence: Ginkgo test in the `claude` package asserting each of the five values equals its fixture value; `make test` exits 0.
- [ ] A terminal result event with no `usage` object and no `num_turns` parses with all five values 0 and the result text unchanged — evidence: Ginkgo test asserting result text equality and five zero values; test does not expect an error.
- [ ] A terminal result event with only some usage fields present parses with the present fields set and the absent fields 0 — evidence: Ginkgo test asserting the mixed expectation.
- [ ] When two result events carry usage, the last one's values are the ones exposed — evidence: Ginkgo test feeding two result lines and asserting the second event's numbers.
- [ ] Usage last-wins is independent of result-text last-wins: a second result event carrying a usage object but an empty `result` string updates the usage values while the result text stays the first event's non-empty text — evidence: Ginkgo test feeding a first result event with text + usage and a second with `"result": ""` + different usage, asserting the first event's text and the second event's usage numbers.
- [ ] A freshly constructed job-metrics registers both new families — evidence: test calling `registry.Gather()` and asserting the returned family names contain `agent_job_tokens_total` and `agent_job_turns_total`.
- [ ] Before any recording call, `agent_job_tokens_total` has exactly 4 series with `type` label values `input`, `output`, `cache_read`, `cache_creation`, each at value `0.0`, and `agent_job_turns_total` is present at `0.0` — evidence: test asserting `HaveLen(4)`, the label-value set, and every value equal to `0.0`.
- [ ] Recording a usage summary with distinct values per token kind advances each series by exactly its own value and the turn counter by the turn count — evidence: test recording a summary with four different token values and asserting each gathered series value, plus the turn counter value.
- [ ] Recording twice accumulates — evidence: test recording the same summary twice and asserting each series equals twice its value.
- [ ] Recording a summary containing a negative value does not panic, leaves that series unchanged, and still advances the other series in the same call — evidence: test asserting no panic and the per-series expectations.
- [ ] Both new metric families expose non-empty help text, and the two help strings differ from each other and from every pre-existing family's help text — evidence: test gathering all families and asserting help strings are non-empty and pairwise distinct.
- [ ] The pre-existing families are unchanged: `agent_job_run_total` still exposes 3 pre-initialized status series at `0.0`, `agent_job_last_run_timestamp_seconds` and `agent_job_duration_seconds` still register, and the push-job-name helper still returns `agent_job_claude_agent` for input `claude-agent` — evidence: the existing package tests pass unmodified; `make test` exits 0.
- [ ] The Counterfeiter fake for the job-metrics interface implements the new recording method — evidence: `grep -n 'RecordUsage' metrics/mocks/job-metrics.go` returns at least one line. (The counterfeiter directive is `-o mocks/job-metrics.go` relative to the `metrics` package, so the fake lives at `metrics/mocks/`, NOT the repo-root `mocks/`.)
- [ ] The CLI's cost field is never parsed or recorded — evidence: `grep -rn 'total_cost_usd' claude/ metrics/` returns no lines (exit 1). The literal string `total_cost_usd` must not appear anywhere under `claude/` or `metrics/`, **including comments and test names** — when referring to it in prose or GoDoc, write "the CLI's cost field" instead.
- [ ] `CHANGELOG.md` has a bullet under a `## Unreleased` heading describing the new usage metrics — evidence: `grep -n -A5 '## Unreleased' CHANGELOG.md | grep -iE 'token|turn'` returns at least one line.
- [ ] The two new metric families are documented — evidence: `docs/job-metrics.md` exists and `grep -c 'agent_job_' docs/job-metrics.md` returns at least 5 (the two new families plus the three pre-existing ones, which are also currently undocumented).
- [ ] `make precommit` exits 0 at the repository root — evidence: exit code.

**Scenario coverage — no new scenario.** Every behavior above is reachable by unit tests against an in-memory Prometheus registry and a string fixture of CLI output. Nothing here requires a real CLI, a real gateway, or a cluster, and no existing user journey changes.

## Verification

```
make precommit
grep -n 'RecordUsage' metrics/mocks/job-metrics.go
grep -n -A5 '## Unreleased' CHANGELOG.md | grep -iE 'token|turn'
grep -c 'agent_job_' docs/job-metrics.md
grep -rn 'total_cost_usd' claude/ metrics/          # expect no lines, exit 1
```

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Parse the terminal result event's usage summary and turn count in the `claude` package; surface the five values on `ClaudeResult`; tests for full / absent / partial / duplicate-event / empty-text-second-event cases | 1, 2, 3, 4 | 1–5, 14 (partial), 16 | — |
| 2 | Add `RecordUsage` and the two counters to the `metrics` package, pre-initialize them, regenerate the fake, write `docs/job-metrics.md`; tests for registration, pre-init, accumulation, negative input, help-string quality, no regression of existing families | 5, 6, 7, 8 | 6–13, 14 (partial), 15, 16 | — |

Rationale: the two prompts touch disjoint packages and share no symbol, so they are independent and can run in either order or in parallel. Splitting them keeps each prompt inside one package with one test suite. Both must land before the follow-up work in the consumer repository can call the new method, but that wiring is out of scope here.

## Do-Nothing Option

Doing nothing keeps the fleet's burn rate unmeasured, so the plan comparison stays a guess: MiniMax's requests-per-5h and GLM's prompts-per-5h cannot be converted into a common unit without knowing how many sessions and turns the fleet actually produces. The alternative to instrumenting is scraping historical CLI output or job logs after the fact, which is manual, retrospective, and gives no ongoing signal for quota alerting. The measurement data is already present in the parsed stream and the push path already exists, so the cost of capturing it is small and the cost of continuing to discard it is a subscription decision made blind.
