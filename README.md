# agent

[![Go Reference](https://pkg.go.dev/badge/github.com/bborbe/agent.svg)](https://pkg.go.dev/github.com/bborbe/agent)
[![CI](https://github.com/bborbe/agent/actions/workflows/ci.yml/badge.svg)](https://github.com/bborbe/agent/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/bborbe/agent)](https://goreportcard.com/report/github.com/bborbe/agent)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/bborbe/agent)

**SDK + Claude Code plugin for the bborbe agent platform.**

- **Go SDK** — shared types, schemas, and runtime helpers consumed by every agent + task-system service in the ecosystem
- **Claude Code plugin** — `/launch-agent` slash command for interview-driven scaffolding of new agents

## Quick start

**Install the plugin** (in Claude Code):

```
claude plugin marketplace add bborbe/agent
claude plugin install agent
```

**Scaffold a new agent**:

```
/launch-agent <name>
```

Walks you through the [[Agent Design Guide]] interview, recommends a reference shape (claude/code/gemini/pi), clones the matching template repo via `gh repo create --template`, customizes the clone, writes vault artifacts (knowledge page, goal, scenario, NEXT-DIRECTIONS), and prints a deploy checklist. See `commands/launch-agent.md` for the workflow.

## What's in here

Single Go module at `github.com/bborbe/agent` with these subpackages:

| Package | Role |
|---|---|
| `agent` (root) | `Agent`, `Phase`, `Step`, `Status`, `Task`, `TaskFrontmatter`, parser/markdown helpers — the runtime contract every agent honors |
| `claude/` | Claude Code runner helpers (used by agent-claude template) |
| `pi/` | MiniMax `pi` runner helpers (used by agent-pi template) |
| `command/` | CQRS command shapes (`task.CreateCommand`, `task.UpdateFrontmatterCommand`, `task.IncrementFrontmatterCommand`) + `ErrTaskAlreadyExists` sentinel |
| `delivery/` | `ResultDeliverer` interface (Kafka + file deliverers) |
| `envparse/` | Env-var parsing helpers for agent main.go bootstraps |
| `healthcheck/` | Generic agent liveness handler |
| `metrics/` | Prometheus metrics for agent + executor runtime |
| `mocks/` | counterfeiter-generated test doubles |

## Consumers

Each agent + task service lives in its own repo and imports this SDK:

| Repo | Role |
|---|---|
| [agent-claude](https://github.com/bborbe/agent-claude) | AI-heavy reference template (Claude Code) — `is_template: true` |
| [agent-code](https://github.com/bborbe/agent-code) | Pure-Go reference template (deterministic phases) |
| [agent-gemini](https://github.com/bborbe/agent-gemini) | Boundary-translator reference (Gemini at planning edge) |
| [agent-pi](https://github.com/bborbe/agent-pi) | Tier-D LLM reference (MiniMax Pi) |
| [agent-task-controller](https://github.com/bborbe/agent-task-controller) | Single git writer for the vault (Kafka → git via [git-rest](https://github.com/bborbe/git-rest)) |
| [agent-task-executor](https://github.com/bborbe/agent-task-executor) | Kafka event consumer + per-task K8s Job spawner |

## Producers (emit task commands)

- [recurring-task-creator](https://github.com/bborbe/recurring-task-creator) — Schedule CR → cron tick → `CreateCommand`
- [maintainer](https://github.com/bborbe/maintainer) — `watcher/github-pr` / `github-build` / `github-release` → `CreateCommand`
- manual — `/vault-cli:create-task` via [vault-cli](https://github.com/bborbe/vault-cli)

## Use as a Go module

```bash
go get github.com/bborbe/agent
```

```go
import (
    "github.com/bborbe/agent"
    "github.com/bborbe/agent/delivery"
    "github.com/bborbe/agent/command/task"
)
```

## History

This repo was a monorepo through 2026-06-24, hosting `task/controller/`, `task/executor/`, and 4 reference agents under `agent/{claude,code,gemini,pi}/` as Go sub-modules + the SDK under `lib/`. On 2026-06-25 each service and reference agent was extracted to its own repo (see Consumers table above), the SDK was promoted from `lib/` to the repo root, and the module identity collapsed from `github.com/bborbe/agent/lib` to `github.com/bborbe/agent`. See [CHANGELOG.md](CHANGELOG.md) `## v0.70.0` for the migration guide.

Older import path `github.com/bborbe/agent/lib/...` continues to resolve via historical tags (latest `v0.69.0`) for any consumer not yet migrated; new development happens at the flat path.

## Plugin layout

`.claude-plugin/`, `commands/`, `agents/`, `skills/`, `scenarios/` at repo root — same convention as [bborbe/coding](https://github.com/bborbe/coding) and [bborbe/dark-factory](https://github.com/bborbe/dark-factory).

| Path | Role |
|---|---|
| `.claude-plugin/plugin.json` + `marketplace.json` | Plugin metadata |
| `commands/launch-agent.md` | The `/launch-agent` slash command (thin dispatcher to the skill) |
| `agents/agent-shape-picker.md` | Sonnet subagent: use case → shape recommendation with reasoning |
| `skills/launch-agent/SKILL.md` | 8-phase orchestrator (interview → shape → clone → customize → render templates → commit → checklist) |
| `skills/launch-agent/references/` | shapes.md + interview.md + 4 output templates (config-crd, vault-page, goal, scenario) + next-directions |
| `scenarios/001-launch-agent-happy-path.md` | End-to-end smoke test of the scaffolding flow |

## Architecture references

- `docs/kafka-schema-design.md` — Kafka topic + command schema design
- `docs/task-flow-and-failure-semantics.md` — task lifecycle + failure modes
- `docs/agent-job-interface.md` — what every agent main.go must implement
- `docs/agent-job-lifecycle.md` — phase + step lifecycle
- `docs/dod.md` — definition of done
- `docs/deployment.md` — platform deploy notes
- [Agent Hub](https://github.com/bborbe/obsidian-personal/blob/master/50%20Knowledge%20Base/Agent%20Hub.md) — full architecture catalog (in personal vault)
- [Quick-Launch New Agents](https://github.com/bborbe/obsidian-personal/blob/master/23%20Goals/Quick-Launch%20New%20Agents.md) — the goal this split landed under

## License

BSD-2-Clause.
