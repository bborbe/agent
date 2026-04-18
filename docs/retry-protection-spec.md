# Retry Protection for Agent Tasks

## Problem

When an agent task fails, the controller writes the result but `status` stays `in_progress`. The executor respawns the job on the next scan cycle, creating an infinite loop. This burns resources (K8s jobs, Gemini API quota) without human intervention.

## Solution

Add a `retry_count` frontmatter field incremented by the controller on each non-completed result writeback. After `max_retries` (default 3), the controller escalates to `phase: human_review` and appends an error message. The executor already filters out `human_review` from allowed phases.

## Frontmatter Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `retry_count` | int | 0 | Incremented by controller on each non-completed result |
| `max_retries` | int | 3 | Per-task override; controller default if absent |

## Flow

```
Agent fails → publishes result (status: in_progress, phase: ai_review)
  │
  Controller result_writer.WriteResult():
  ├── read existing retry_count from frontmatter (default 0)
  ├── if incoming status != "completed":
  │     retry_count++
  │     if retry_count >= max_retries:
  │       override phase → "human_review"
  │       append to content: "**Error:** max retries (N) exceeded. Manual review required."
  │
  ├── write merged frontmatter (includes retry_count)
  └── git commit + push
  
Next scan cycle:
  Controller publishes event → Executor sees phase: human_review → skips
```

## Changes

### Controller: `task/controller/pkg/result/result_writer.go`

In `WriteResult()`, after `mergeFrontmatter`:

```go
if merged.Status() != "completed" {
    retryCount := merged.RetryCount() + 1
    merged["retry_count"] = retryCount
    maxRetries := merged.MaxRetries() // default 3 if absent
    if retryCount >= maxRetries {
        merged["phase"] = "human_review"
        req.Content += "\n\n**Error:** max retries (" + strconv.Itoa(maxRetries) + ") exceeded. Manual review required.\n"
    }
}
```

### Lib: `lib/agent_task-frontmatter.go`

Add accessors:

```go
func (f TaskFrontmatter) RetryCount() int    // parse retry_count, default 0
func (f TaskFrontmatter) MaxRetries() int    // parse max_retries, default 3
```

### No executor changes

Executor already filters `allowedPhases = {planning, in_progress, ai_review}`. Setting `phase: human_review` naturally stops spawning.

### No agent changes

Agents don't need to know about retries. They publish results normally.

## Reset

To retry a failed task after manual review:
- Set `retry_count: 0` (or remove field)
- Set `phase: ai_review`
- Set `status: in_progress`

The controller detects the change, publishes event, executor spawns.

## Test Cases

1. **First failure**: retry_count 0→1, phase stays ai_review, executor respawns
2. **Third failure**: retry_count 2→3, phase→human_review, error appended, executor stops
3. **Success after retry**: retry_count stays at previous value, status→completed, phase→done
4. **Custom max_retries**: task with `max_retries: 5` retries 5 times before escalation
5. **Manual reset**: setting retry_count=0 + phase=ai_review allows fresh retries

## Related

- [controller-design.md](controller-design.md) — result writeback flow
- [job-creator-design.md](job-creator-design.md) — executor phase filtering
- Root cause fix: `trading/lib/agent/result-deliverer.go` — explicit frontmatter status/phase
