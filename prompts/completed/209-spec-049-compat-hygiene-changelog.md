---
status: completed
spec: [049-claude-runner-persists-partial-output]
summary: 'Recorded spec 049''s Partial field in a new top ## Unreleased CHANGELOG section and proved the change fully additive: all three ClaudeRunner callers compile with unmodified specs passing, make generate leaves every counterfeiter-generated mock byte-identical (MOCKS UNCHANGED), and make precommit passes with exit 0 (ROOTDIR=/workspace supplied because the Makefile derives ROOTDIR from git, which the hideGit mask breaks)'
execution_id: agent-pr-reviewer-salvage-exec-209-spec-049-compat-hygiene-changelog
dark-factory-version: dev
created: "2026-08-21T10:20:00Z"
queued: "2026-08-21T11:03:51Z"
started: "2026-08-21T11:16:11Z"
completed: "2026-08-21T11:19:29Z"
branch: dark-factory/claude-runner-persists-partial-output
---

<summary>
- The new partial-output behavior from prompt 1 of this spec is recorded in the changelog under a new top `## Unreleased` section with a `feat:` bullet
- All existing callers of the claude runner (agent step, task runner, healthcheck step) still compile and their Ginkgo specs pass unmodified
- The generated `ClaudeRunner` mock is byte-identical after `make generate` — the interface was untouched, so no mock churn
- The change stays confined to the `claude` package plus `CHANGELOG.md`; no other package is edited
- The full precommit gate passes, proving the additive surface did not regress anything else in the repository
</summary>

<objective>
Confirm the spec 049 change (shipped by the sibling prompt of this spec) is fully additive: prove every existing caller still compiles and behaves identically, prove `make generate` leaves the generated mock untouched, record the change in `CHANGELOG.md` under a new top `## Unreleased` section, and run the final precommit gate. Implements spec 049 Desired Behavior 5 and Acceptance Criteria 5-7. This is prompt 2 of 2 — it MUST run after prompt 1 (the mechanism) has landed.
</objective>

<context>
Read `/workspace/CLAUDE.md` for project conventions (single global `CHANGELOG.md` at repo root, tag policy, no commits from YOLO).

Read these coding-plugin docs:
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — entry format (`- <prefix>: <what> [context]`, prefix required), verb style, `## Unreleased` rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — why the sibling suites must pass unmodified.
- `/workspace/docs/dod.md` — this repo's Definition of Done (CHANGELOG entry under `## Unreleased` is part of Done).

Read these files IN FULL before editing:
- `/workspace/CHANGELOG.md` — top ~40 lines establish the current layout (SemVer preamble, then `## v0.81.3`). The change to make is small: insert a new `## Unreleased` section between the preamble and `## v0.81.3`.
- `/workspace/mocks/claude-claude-runner.go` (117 lines) — the generated mock; its `runReturns`/`RunReturns` types reference `*claude.ClaudeResult` by pointer only and its `Run` signature is unchanged, so regenerating must not change it.
- `/workspace/claude/agent-step.go` (line 88), `/workspace/claude/task-runner.go` (line 59), `/workspace/healthcheck/healthcheck-claude-step.go` (line 32) — the three in-repo call sites of `ClaudeRunner.Run`; each ignores the result when `err != nil` (verified in prompt 1 of this spec). Read them to confirm they still compile and their specs still pass.

Precondition (prompt 1 of this spec must have landed on this branch): `/workspace/claude/claude-result.go` MUST already declare `Partial string` with the JSON tag `partial,omitempty`. Verify with:
```bash
grep -n 'Partial' /workspace/claude/claude-result.go
```
If it returns zero lines, STOP and report a precondition failure — do NOT re-implement the mechanism; prompt 1 owns it.
</context>

<requirements>

## 1. Add the `## Unreleased` CHANGELOG entry

In `/workspace/CHANGELOG.md`, insert a new `## Unreleased` section immediately after the SemVer preamble (`* PATCH version when you make backwards-compatible bug fixes.`) and immediately before the existing `## v0.81.3` heading. There is currently NO `## Unreleased` section in this file — if (unexpectedly) one already exists from another in-flight prompt, append the bullet to it instead of adding a second section.

```markdown
## Unreleased

- feat: claude: `ClaudeResult` now carries `Partial`, the bounded streamed assistant text captured up to the moment a run terminates (killed, cancelled, or missing result event), surfaced alongside the error so partial output can be salvaged instead of lost

```

Notes:
- Do NOT rename any version heading. The release-step rename of `## Unreleased` → `## v0.82.0` and the paired `vX.Y.Z` + `lib/vX.Y.Z` tags are an operator step of the spec, NOT part of this prompt.
- The bullet must keep the literal phrase "partial output" (case-insensitive) so the spec's `grep -n -i 'partial output' CHANGELOG.md` acceptance check passes.
- Follow the changelog-guide verb style — the prefix is `feat:` (additive public API, semver MINOR bump).

## 2. Confirm all existing callers compile and their specs pass unmodified

```bash
cd /workspace && go test -mod=mod -race ./claude/... ./healthcheck/...
```

Must report `ok` / PASS. This runs the full `claude` suite (including the new `claudeRunner partial capture` specs from prompt 1), the `claudeRunner stdout tail` / `usage capture` / `CLAUDE_CONFIG_DIR` / `AllowedTools` suites (which must pass unmodified), and the `healthcheck` suite (whose `healthcheck-claude-step_test.go` drives the mock runner through `RunReturns`). If a pre-existing spec fails for a reason unrelated to this change, do NOT silently "fix" it by editing behavior — report it in the completion report.

## 3. Prove `make generate` leaves the generated mock untouched

The `ClaudeRunner` interface is unchanged by spec 049, so regeneration must be byte-identical. Snapshot the mock tree before and after `make generate` and diff (no git needed):

```bash
cd /workspace
find mocks -type f -name '*.go' -exec sha256sum {} + | sort > /tmp/mocks.before
make generate
find mocks -type f -name '*.go' -exec sha256sum {} + | sort > /tmp/mocks.after
diff /tmp/mocks.before /tmp/mocks.after && echo "MOCKS UNCHANGED"
```

The diff must be empty and the script must print `MOCKS UNCHANGED`. If the diff is non-empty, inspect `/workspace/mocks/claude-claude-runner.go` — if it now references a field or method that does not exist, that indicates the interface was accidentally changed by prompt 1; STOP and report it (do not hand-edit the generated file to hide the diff).

## 4. Keep the change scope contained

Edit ONLY `/workspace/CHANGELOG.md` in this prompt. If inspection or test failures reveal a bug in the prompt-1 code under `/workspace/claude/`, fix it there — that is in-scope. Do NOT touch any file outside `/workspace/claude/` and `/workspace/CHANGELOG.md`. In particular do NOT touch `/workspace/pi/`, any other package, the helm chart, or any other repo file. The git-based scope check (`git diff --name-only origin/master...HEAD` must list only `claude/`, `CHANGELOG.md`, and `specs/`) is performed by the operator/manager after the PR is assembled — you cannot run it here (git is masked), so the hard constraint above is your side of that check.

## 5. Run the full precommit gate

```bash
cd /workspace && make precommit
```

Must exit 0. `make precommit` runs `ensure format generate test check addlicense` — its `test` step compiles and tests every package in the repository, so this is the whole-repo compatibility proof (all callers of the widened `ClaudeResult`, including any this prompt did not read directly, must still compile). If a target fails, fix the issue, then re-run ONLY the failing target (`make lint`, `make test`, ...) until it passes before re-running `make precommit` once more.
</requirements>

<constraints>
- The `Run(ctx, prompt) (*ClaudeResult, error)` signature and the `ClaudeRunner` interface are frozen. No new method, no new argument. The generated mock at `mocks/claude-claude-runner.go` MUST NOT change — `make generate` exits 0 with zero diff.
- Do NOT modify `ClaudeResult`, `scanOutput`, `Run`, or any code shipped by prompt 1 of this spec unless a test failure proves a genuine bug. This prompt is verification + changelog, not re-implementation.
- Do NOT add any new config field, env var, or opt-out for the partial capture (spec Non-goal — hard veto).
- Do NOT touch the tail ring-buffer contract (5 lines × 512 bytes, ` | ` joiner, `no stdout captured`) or the CLI invocation flag set.
- Do NOT rename CHANGELOG version headings and do NOT create or push any tag — release tagging is the spec's operator step, gated on the merged PR.
- Do NOT add a scenario (spec Non-goal).
- Change scope: only `/workspace/claude/` and `/workspace/CHANGELOG.md`.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# AC 6 (non-git half): ## Unreleased exists as the top section, above v0.81.3.
cd /workspace
grep -n '^## Unreleased' CHANGELOG.md
# Must return a line number strictly less than the ## v0.81.3 line (## Unreleased sits
# directly above the first released heading; the exact number depends on preamble length).
sed -n '1,/^## /p' CHANGELOG.md
# Must show "# Changelog" then "## Unreleased" (top section, no ## between them).
```

```bash
# AC 6 (bullet): the entry mentions partial output.
grep -n -i 'partial output' CHANGELOG.md
# Must return at least 1 line.
```

```bash
# Existing callers and their specs pass unmodified.
cd /workspace && go test -mod=mod -race ./claude/... ./healthcheck/...
# Must report ok / PASS.
```

```bash
# Mock untouched by regeneration (non-git proof).
cd /workspace
find mocks -type f -name '*.go' -exec sha256sum {} + | sort > /tmp/mocks.before
make generate
find mocks -type f -name '*.go' -exec sha256sum {} + | sort > /tmp/mocks.after
diff /tmp/mocks.before /tmp/mocks.after && echo "MOCKS UNCHANGED"
# Must print "MOCKS UNCHANGED" with an empty diff.
```

```bash
# Final full validation at the repository root.
cd /workspace && make precommit
# Must exit 0.
```
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **Git-based acceptance criteria are operator-side.** Spec AC 6's changelog-fold guard (`git diff origin/master -- CHANGELOG.md | grep -E '^[-+]## '` must show only `+## Unreleased`) and AC 7's scope containment (`git diff --name-only origin/master...HEAD` lists only `claude/`, `CHANGELOG.md`, `specs/`) cannot run in the prompt container — the daemon runs with `hideGit=true` (confirmed in `.dark-factory.log`; `/workspace/.git` is masked as a char device), so a bare `git` there would fail closed and false-pass. The operator/manager must run these two checks when assembling the PR. The executor-side proxies are requirement 4 (scope constraint) and requirement 3 (mock checksum diff).
- **Release step is not a prompt.** Renaming `## Unreleased` → `## v0.82.0`, cutting paired `vX.Y.Z` + `lib/vX.Y.Z` tags at the merged commit, and pushing are the spec's operator step (requires host git access and tag approval), deliberately outside both prompts. It MUST complete before the companion repo's dependency-bump prompt can pass.
