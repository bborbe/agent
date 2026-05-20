---
status: prompted
approved: "2026-05-20T16:37:02Z"
generating: "2026-05-20T16:40:37Z"
prompted: "2026-05-20T16:53:58Z"
branch: dark-factory/rename-task-status-phase-taxonomy
---

## Summary

- Align agent repo with vault-cli's renamed task taxonomy: status canonical `next` (was `todo`), phase canonical `execution` (was `in_progress`).
- Bump vault-cli dependency in every service module that imports `pkg/domain` so the new constants are available.
- Flip Phase-flag defaults that currently hardcode `"in_progress"` to use the new canonical `"execution"`.
- Update CRD field documentation and any literal status/phase strings in tests so they emit the new canonical, keeping at least one alias-round-trip test per dimension.
- Existing tasks/goals with old values (`status: todo`, `phase: in_progress`) keep working because vault-cli's `NormalizeTaskStatus` / `NormalizeTaskPhase` accept both forms as aliases.

## Problem

vault-cli flipped the canonical status to `next` and phase to `execution` to eliminate the `status: in_progress, phase: in_progress` naming collision. Agent imports vault-cli's domain types, so the new constants propagate at compile time, but several places hold literal strings that bypass the type system: CLI flag defaults (`default:"in_progress"`), k8s CRD field documentation, and assertion literals in unit tests. Without an explicit sweep these emit legacy values forever, defeating the rename and producing noisy grep results across the vault.

## Goal

After this work, the agent repo:

1. Compiles against the vault-cli version that exposes `TaskStatusNext` and `TaskPhaseExecution`.
2. Writes new canonical values (`status: next`, `phase: execution`) into every freshly published or transitioned task.
3. Continues to load existing vault frontmatter that carries legacy `todo` / `in_progress` values via vault-cli's normalize functions — no breaking change for in-flight tasks.

## Non-goals

- Do NOT migrate existing vault frontmatter files in `~/Documents/Obsidian/Personal/` or `~/Documents/Obsidian/Trading/` — that's handled by separate vault-side tasks.
- Do NOT change the agent's k8s CRD API version or break consumers of `agent.benjamin-borbe.de/v1`.
- Do NOT rename `ai_review` / `human_review` (deferred to a separate goal in the vault).
- Do NOT remove the legacy aliases from vault-cli — both old and new values must keep validating.

## Desired Behavior

1. Every `go.mod` under `agent/`, `task/`, `lib/` that lists `github.com/bborbe/vault-cli` is bumped to a published version whose `pkg/domain.AvailableTaskStatuses` contains `next` and `pkg/domain.AvailableTaskPhases` contains `execution`.
2. `agent/claude/main.go` and `agent/claude/cmd/run-task/main.go` declare the Phase flag with `default:"execution"` and `usage:"Agent phase: planning | execution | ai_review"`.
3. Any other `*/main.go` in the agent repo declaring a `domain.TaskPhase` or `domain.TaskStatus` flag emits new canonical defaults.
4. `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` field comments and any sample manifests refer to the new canonical values.
5. Test files asserting literal `"todo"` or `"in_progress"` for task status/phase use the new canonical (`"next"`, `"execution"`) except for at least one test per dimension that explicitly exercises the alias-acceptance path through `NormalizeTaskStatus` / `NormalizeTaskPhase`.
6. `make precommit` exits 0 in every service whose `go.mod` was bumped.

## Constraints

- Existing task files written with `status: todo` / `phase: in_progress` MUST continue to load — never reject those values as invalid.
- The k8s CRD schema (`zz_generated.deepcopy.go`, OpenAPI spec) MUST remain backward-compatible — old values stay accepted at the API layer.
- `errors.Errorf` / `errors.Wrapf` from `github.com/bborbe/errors` for all new error wrapping.
- Counterfeiter mocks regenerate cleanly via `go generate ./...` after the dep bump.
- Existing tests must pass except where they assert a literal value that intentionally changes — those tests are updated, not deleted.
- No `go mod vendor` calls during the prompt.
- Add imports before running `go mod tidy` to avoid transitive demotion.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| vault-cli version with new constants not yet published | `go get github.com/bborbe/vault-cli@vX.Y.Z` fails with "version not found"; spec verification refuses | Wait for vault-cli release; re-run after the new tag exists |
| Counterfeiter regeneration drifts after dep bump | `make precommit` fails in changed service with mock-mismatch diff | Run `go generate ./...` in the offending package; recommit |
| Phase default change breaks a downstream consumer that pinned `in_progress` literally | `make run-dummy-task` produces an unexpected phase value | The legacy alias still validates; consumer reads via normalize and sees `execution` — no action needed unless consumer asserts the raw frontmatter string, in which case update the consumer |
| Mixed values in a single CRD object (old + new) | CRD load succeeds; normalize converges both reads to new canonical | None — by design |
| Concurrent dep bumps land different vault-cli versions across modules (race between this spec and an unrelated `go mod tidy` PR) | `go list -m github.com/bborbe/vault-cli` returns different versions in different modules; compile may still succeed but type aliases drift | Pin every module to the same vault-cli version in one commit; CI's `go list -m all` snapshot test (if present) catches drift |

## Do-Nothing Option

Acceptable short-term. Vault-cli normalize already accepts both old and new values, so agent continues to function with legacy defaults. Cost: every new task the agent publishes keeps emitting old canonical, slowly multiplying grep noise across the vault and reinforcing the legacy values. Fix is small but never gets cheaper.

## Acceptance Criteria

- [ ] `grep -rn 'github.com/bborbe/vault-cli' agent/ task/ lib/ --include='go.mod'` lists every module on the chosen vault-cli version; `go list -m github.com/bborbe/vault-cli` in each module prints that version exactly once.
- [ ] `grep -n 'default:"in_progress"' agent/claude/main.go agent/claude/cmd/run-task/main.go` returns 0 lines; `grep -n 'default:"execution"' agent/claude/main.go agent/claude/cmd/run-task/main.go` returns ≥1 line each.
- [ ] `grep -rn 'default:"in_progress"' agent/ task/ --include='main.go' --exclude-dir=vendor` returns 0 lines (covers `agent/claude`, `agent/gemini`, `agent/code`, and any future sibling main package declaring a Phase flag).
- [ ] `grep -rn 'planning | in_progress | ai_review' agent/ --include='*.go' --exclude-dir=vendor` returns 0 lines; equivalent usage strings reference `planning | execution | ai_review` instead.
- [ ] `grep -rn 'default:"todo"' --include='*.go' --exclude-dir=vendor` returns 0 lines for `TaskStatus`-typed flags. The grep target excludes lines containing `// ` (comments) and `usage:"...todo..."` (informational usage strings) — use `grep -rn 'default:"todo"'` then visually confirm no surviving hit is a `TaskStatus` flag default.
- [ ] `grep -n 'execution' task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types.go` returns ≥1 line in a field documentation comment for `Phases`.
- [ ] Running `make precommit` in `lib`, `task/controller`, `task/executor`, `agent/claude`, `agent/gemini`, `agent/code` exits 0.
- [ ] At least one test under `task/controller/pkg/scanner/` AND one under `task/controller/pkg/command/` invokes `domain.NormalizeTaskPhase("in_progress")` and asserts the return value equals `domain.TaskPhaseExecution` (or the equivalent string `"execution"`). Verify with `grep -B2 -A4 'NormalizeTaskPhase("in_progress")' task/controller/pkg/scanner/*_test.go task/controller/pkg/command/*_test.go` returning a matching assertion block in each directory.
- [ ] At least one test invokes `domain.NormalizeTaskStatus("todo")` and asserts the return value equals `domain.TaskStatusNext`. Verify with `grep -rn 'NormalizeTaskStatus("todo")' --include='*_test.go' --exclude-dir=vendor` returning ≥1 line with a `TaskStatusNext` assertion within 4 lines.

## Verification

```bash
# Each module verified in its own subshell — independent of prior step success
(cd lib                && make precommit)
(cd task/controller   && make precommit)
(cd task/executor     && make precommit)
(cd agent/claude      && make precommit)
(cd agent/gemini      && make precommit)
(cd agent/code        && make precommit)
```

Then sanity-check that the binary emits new canonical:

```bash
# In agent/claude
go run ./ --help | grep -A1 'Agent phase'
# Expected: "planning | execution | ai_review" appears in the usage string
```
