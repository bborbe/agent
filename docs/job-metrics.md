# Job Metrics

`metrics.NewJobMetrics` registers five Prometheus collector families onto a caller-owned registry. Metrics are pushed to the PushGateway under the job name derived from `metrics.BuildJobMetricsName`, so the per-agent breakdown comes from the push job name (e.g. `claude-agent` → `agent_job_claude_agent`), not from a metric label.

## Metrics Reference

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `agent_job_run_total` | Counter | `status` | Total number of agent job runs by terminal status (`done`, `failed`, `needs_input`). |
| `agent_job_last_run_timestamp_seconds` | Gauge | `status` | Unix timestamp (seconds) of the last agent job run, by terminal status. |
| `agent_job_duration_seconds` | Histogram | — | Duration of agent job runs in seconds. |
| `agent_job_tokens_total` | Counter | `type` | Total LLM tokens consumed by agent jobs. Label values: `input` (fresh input), `output` (generated), `cache_read` (served from prompt cache), `cache_creation` (written to prompt cache). |
| `agent_job_turns_total` | Counter | — | Total number of conversation turns taken by agent jobs. |

## Pre-initialization

Every counter series is pre-initialized to zero at construction (via `.Add(0)`). This ensures `rate()` evaluates to zero rather than no-data for a process that has not yet run a job, so alerts built on these counters fire correctly from the start.

## Not Recorded

The CLI's cost field is deliberately not captured. Under a non-Anthropic base URL the CLI computes a cost estimate at Anthropic list pricing that does not reflect what the provider actually charges, and a wrong number in a cost dashboard is worse than no cost dashboard.
