# CLAUDE.md

Agent orchestration system — event-driven, Kafka-based, K8s-native.

## Dark Factory Workflow

**Never code directly.** All code changes go through the dark-factory pipeline.

### Complete Flow

**Spec-based (multi-prompt features):**

1. Create spec → `/dark-factory:create-spec`
2. Audit spec → `/dark-factory:audit-spec`
3. User confirms → `dark-factory spec approve <name>`
4. dark-factory auto-generates prompts from spec
5. Audit prompts → `/dark-factory:audit-prompt`
6. User confirms → `dark-factory prompt approve <name>`
7. Start daemon → `dark-factory daemon` (use Bash `run_in_background: true`)
8. dark-factory executes prompts automatically

**Standalone prompts (simple changes):**

1. Create prompt → `/dark-factory:create-prompt`
2. Audit prompt → `/dark-factory:audit-prompt`
3. User confirms → `dark-factory prompt approve <name>`
4. Start daemon → `dark-factory daemon` (use Bash `run_in_background: true`)
5. dark-factory executes prompt automatically

### Assess the change size

| Change | Action |
|--------|--------|
| Simple fix, config change, 1-2 files | Write a prompt → `/dark-factory:create-prompt` |
| Multi-prompt feature, unclear edges, shared interfaces | Write a spec first → `/dark-factory:create-spec` |

### Read the relevant guide before starting — every time, not from memory

- Writing a spec → read [[Dark Factory - Write Spec]] and [[Dark Factory Guide#Specs What Makes a Good Spec]]
- Writing prompts → read [[Dark Factory - Write Prompts]] and [[Dark Factory Guide#Prompts What Makes a Good Prompt]]
- Running prompts → read [[Dark Factory - Run Prompt]]

### Claude Code Commands

| Command | Purpose |
|---------|---------|
| `/dark-factory:create-spec` | Create a spec file interactively |
| `/dark-factory:create-prompt` | Create a prompt file from spec or task description |
| `/dark-factory:audit-spec` | Audit spec against preflight checklist |
| `/dark-factory:audit-prompt` | Audit prompt against Definition of Done |

### CLI Commands

| Command | Purpose |
|---------|---------|
| `dark-factory spec approve <name>` | Approve spec (inbox → queue, triggers prompt generation) |
| `dark-factory prompt approve <name>` | Approve prompt (inbox → queue) |
| `dark-factory daemon` | Start daemon (watches queue, executes prompts) |
| `dark-factory run` | One-shot mode (process all queued, then exit) |
| `dark-factory status` | Show combined status of prompts and specs |
| `dark-factory prompt list` | List all prompts with status |
| `dark-factory spec list` | List all specs with status |
| `dark-factory prompt retry` | Re-queue failed prompts for retry |

### Key rules

- Prompts go to **`prompts/`** (inbox) — never to `prompts/in-progress/` or `prompts/completed/`
- Specs go to **`specs/`** (inbox) — never to `specs/in-progress/` or `specs/completed/`
- Never number filenames — dark-factory assigns numbers on approve
- Never manually edit frontmatter status — use CLI commands above
- Always audit before approving (`/dark-factory:audit-prompt`, `/dark-factory:audit-spec`)
- **BLOCKING: Never run `dark-factory prompt approve`, `dark-factory spec approve`, or `dark-factory daemon` without explicit user confirmation.** Write the prompt/spec, then STOP and ask the user to approve. Do not assume approval from prior context or task momentum.
- **Before starting daemon** — run `dark-factory status` first to check if one is already running. Only start if not running.
- **Start daemon in background** — use Bash tool with `run_in_background: true` (not foreground, not detached with `&`)

## Development Standards

This project follows the [coding-guidelines](https://github.com/bborbe/coding-guidelines).

### Key Reference Guides

- **go-architecture-patterns.md** — Interface → Constructor → Struct → Method
- **go-testing-guide.md** — Ginkgo v2/Gomega testing
- **go-makefile-commands.md** — Build commands

### Build and test

- `make precommit` — format + generate + test + lint + license
- `make test` — tests only
- Run in service dir, never at root

### Deploy (`make buca`)

- Always use `/make-buca <service-dir> <branch>` slash command (delegates to simple-bash-runner, concise output). Never raw `make buca`.
- Only `dev` or `prod` are valid branches. Never `develop` / `master` / feature branches.
- Example: `/make-buca agent/claude dev`

### Versioning and tags

- Single global `CHANGELOG.md` at repo root. No per-module CHANGELOG.
- Every release pairs two tags at the same commit: `vX.Y.Z` (root module) and `lib/vX.Y.Z` (lib module). Always same number, always same commit. Required so other repos can `go get github.com/bborbe/agent/lib@vX.Y.Z`.

### Test conventions

- Ginkgo/Gomega test framework
- Counterfeiter for mocks (`mocks/` dir)
- External test packages (`*_test`)

### Dependencies

- `github.com/bborbe/errors` — error handling
- `github.com/onsi/ginkgo/v2` / `github.com/onsi/gomega` — testing
- `github.com/maxbrunsfeld/counterfeiter/v6` — mock generation

## Architecture

- `tools.go` — tool dependencies (build tools, linters, generators)
- `lib/` — shared types (agent-task-v1, agent-prompt-v1, Config CRD)
- `task/controller` — single git writer: pull+diff→events, consume requests→write+commit+push
- `prompt/controller` — consumes task events, produces prompt requests, heartbeat
- `prompt/executor` — consumes prompt requests, spawns K8s jobs, publishes results

## Task Enums (source of truth)

Defined in `github.com/bborbe/vault-cli/pkg/domain`.

**TaskStatus** (`task_status.go`): `todo`, `in_progress`, `completed`, `backlog`, `hold`, `aborted`
- Aliases normalized by `NormalizeTaskStatus`: `done`→`completed`, `current`→`in_progress`, `next`→`todo`, `deferred`→`hold`

**TaskPhase** (`task_phase.go`): `todo`, `planning`, `in_progress`, `ai_review`, `human_review`, `done`

Executor allowlist (spawns Job only if phase ∈): `planning`, `in_progress`, `ai_review`.
Terminal phases (no auto-respawn): `human_review`, `done`.

Never invent values (e.g. `pending`) — they fail silently in the executor filter.

## Key Design Decisions

- **Event-driven** — Kafka-based message passing between components
- **K8s-native** — controller pattern, CRDs for agent config
- **Multi-service mono-repo** — each subdir is a separate service with its own `make precommit`
- **Factory functions are pure composition** — no conditionals, no I/O, no `context.Background()`
- **Counterfeiter mocks** — generated into `mocks/`, regenerated on `make generate`
- **No vendor** — `go mod tidy` removes vendor dir
