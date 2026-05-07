# agent

Event-driven, Kafka-based agent orchestration system. Generic task controller + pluggable Claude/AI runners spawned as Kubernetes Jobs.

## How it works

1. A markdown task file lands in the configured Obsidian vault (assignee, status, phase frontmatter).
2. **`task/controller`** — single git writer for the vault. Pulls, diffs, publishes `create-task` / `update-frontmatter` / `increment-frontmatter` commands to Kafka. Consumes results back, writes them to the task file, commits, pushes.
3. **`task/executor`** — consumes task events, filters by `assignee` + `phase`, spawns a per-task Kubernetes Job using one of the runner images. Reads the Job's stdout, publishes the result back via Kafka.
4. The Job (one of `agent/claude`, `agent/code`, `agent/gemini`) runs the configured AI CLI headlessly with the task body as input, prints a result, exits.

Wire format and command schemas live in **`lib/`**, which is published as `github.com/bborbe/agent/lib` for downstream producers (e.g. `bborbe/maintainer` watchers).

## Components

| Path | Description |
|---|---|
| `lib/` | Shared types: `task.CreateCommand`, `task.UpdateFrontmatterCommand`, `task.IncrementFrontmatterCommand` (in `lib/command/task/`), each with `Validate(ctx)` + counterfeiter-mocked sender; agent-task-v1 + agent-prompt-v1 schemas; Config CRD; markdown parser |
| `task/controller/` | Single git writer for the vault — pulls/diffs/publishes events, consumes commands, atomic write + commit + push (via `git-rest` HTTP API) |
| `task/executor/` | Consumes task events, filters by assignee + phase, spawns per-task K8s Jobs, publishes Job results back |
| `agent/claude/` | Claude Code CLI runner (default Job image — `Bash`, `Edit`, `Read`, `Write` tools) |
| `agent/code/` | OpenAI Codex CLI runner |
| `agent/gemini/` | Gemini CLI runner |

Multi-module layout: each subdir has its own `go.mod`. Six modules total.

## Hierarchy

```
Vault task file (assignee: claude-agent, phase: planning)
  → task/controller publishes CreateTaskCommand to Kafka
  → task/executor filters (assignee=claude-agent, phase ∈ {planning, in_progress, ai_review})
  → spawns K8s Job (agent/claude image)
  → Claude CLI runs, prints result JSON to stdout
  → executor reads stdout, publishes UpdateFrontmatterCommand back
  → controller writes result section to the task file, commits, pushes
```

## Dark-factory pipeline

This repo's code changes flow through [dark-factory](https://github.com/bborbe/dark-factory):

```
specs/    spec inbox (idea → draft → approved → prompted → verifying → completed)
prompts/  per-spec implementation prompts (draft → approved → executed → completed)
```

See `CLAUDE.md` for the workflow rules. Specs and prompts are ephemeral — they describe behavior changes, get executed by Claude Code in YOLO containers, then move to `completed/`.

## Build and deploy

Build commands run per-service, never at repo root:

```bash
cd task/controller && make precommit       # format + generate + test + lint + license
cd task/controller && make test            # tests only
```

Deploy uses `make buca` (build, upload, commit, apply). Always from a clean deployment worktree:

```bash
cd ~/Documents/workspaces/agent-dev   # NOT ~/Documents/workspaces/agent (dark-factory commits there)
git pull
git merge master
cd task/controller && BRANCH=dev make buca
cd task/executor  && BRANCH=dev make buca
```

Or `scripts/buca-all.sh` from the deployment worktree to rebuild every service.

## Versioning

Single `CHANGELOG.md` at repo root. Every release pairs two tags at the same commit: `vX.Y.Z` (root module, all binaries) and `lib/vX.Y.Z` (lib module, for downstream `go get`). Both tags MUST equal the latest `## vX.Y.Z` header in `CHANGELOG.md`.

## Architecture references

- `CLAUDE.md` (this repo) — operational rules, dark-factory workflow, deploy conventions
- `docs/kafka-schema-design.md` — Kafka topic + command schema design
- `docs/task-flow-and-failure-semantics.md` — task lifecycle + failure modes
- [Agent Task Controller Architecture](https://github.com/bborbe/obsidian-personal/blob/master/50%20Knowledge%20Base/Agent%20Task%20Controller%20Architecture.md) — full design (in personal vault)

## License

BSD-2-Clause.
