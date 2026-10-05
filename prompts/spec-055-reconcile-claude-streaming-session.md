---
status: draft
spec: [055-claude-streaming-session]
created: "2026-10-05T18:06:25Z"
branch: dark-factory/claude-streaming-session
---

<!-- OPEN QUESTION FOR THE HUMAN REVIEWER (audit time) — READ BEFORE APPROVING:
     Spec 055 (claude-streaming-session) appears to be ALREADY IMPLEMENTED AND RELEASED in this
     tree. The evidence is concrete and checked into the repo:
       * prompts/completed/223-spec-055-claude-streaming-session.md — a COMPLETED prompt whose
         frontmatter records `spec: [055-claude-streaming-session]` and `completed: "2026-10-01T08:28:48Z"`.
       * claude/claude-session.go — exports `NewSession` / `NewSessionFactory` and holds one long-lived
         `claude` process per session over the CLI stream-json protocol.
       * claude/claude-session_test.go — encodes AC1 and the spec's Failure Modes rows.
       * interactive/claude-session_test.go — wires the Claude session into the shared service's
         frozen-contract table (AC3).
       * docs/claude-streaming-protocol.md — the Repository-hygiene protocol reference.
       * CHANGELOG.md `## v0.92.0` — the released entry describing exactly this feature; v0.93.0 /
         v0.93.1 / v0.94.0 build on it.
     If regeneration of spec 055's prompts was NOT intended (for example, this spec was left in
     `specs/in-progress/` after its work shipped), REJECT this prompt rather than approving it —
     approving it will produce a verification-only run that changes nothing.
     If regeneration WAS intended, this prompt is the honest artifact: a reconciliation that proves
     the shipped state with named evidence, checks for regression from the follow-on releases, and
     repairs only a genuinely broken piece. It deliberately does NOT re-implement shipped code. -->

<summary>
- Spec 055's Claude streaming-session feature is already implemented and released (v0.92.0); this prompt does not re-implement it
- The prompt proves that claim with named, reproducible evidence instead of asserting it
- It re-runs the tests that encode the spec's in-container acceptance criteria and confirms they still pass
- It checks the spec's repository-hygiene deliverables — README, changelog entry, and the stream-json protocol doc — are still present
- It checks the spec's frozen surfaces are still frozen: the one-shot Claude runner and its config are unchanged
- It looks specifically for regressions introduced by the follow-on releases that touched the same package
- If — and only if — a genuine regression or missing deliverable is found, it is repaired with a minimal, targeted fix
- If nothing is wrong, the prompt makes no code change and records the reconciliation finding
- It flags an open question for the human reviewer: whether regenerating a prompt for an already-shipped spec was intended
- The real end-to-end probe against the actual `claude` binary stays a host-side operator step and is not a container check
</summary>

<objective>
Reconcile spec 055 (`specs/in-progress/055-claude-streaming-session.md`) against the current working tree. The spec's in-container acceptance criteria (AC1, AC3) and its repository-hygiene deliverables appear already satisfied by released work — v0.92.0, with v0.93.0 / v0.93.1 / v0.94.0 building on the same package. Confirm that with named, reproducible evidence; detect and repair any regression the follow-on releases introduced in spec 055's behaviour; and record the finding so a human can decide whether the spec should be closed. Do NOT re-implement, refactor, or cosmetically improve shipped code. AC2 is operator-executable only (the real `claude` binary is not in the build container) and is recorded, not run, here.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `docs/dod.md` for the Definition of Done.

Files to read IN FULL before deciding anything:
- `specs/in-progress/055-claude-streaming-session.md` — the spec being reconciled (Goal, Non-goals, Acceptance Criteria, Constraints, Failure Modes, Security / Abuse Cases).
- `claude/claude-session.go` — the shipped implementation: `NewSession`, `NewSessionFactory`, `session`, `PermissionDecider`, the turn loop, `sessionArgs`, `startProcess`.
- `claude/claude-session_test.go` — the AC1 tests and the spec's Failure Modes rows.
- `interactive/claude-session_test.go` — the AC3 test: the Claude session wired into the shared service's frozen-contract table.
- `docs/claude-streaming-protocol.md` — the Repository-hygiene protocol reference.
- `CHANGELOG.md` — the `## v0.92.0` entry and the follow-on `## v0.93.0` / `## v0.93.1` / `## v0.94.0` entries.
- `agent_session.go` — the shared `Session` / `SessionFactory` seam (package `lib`, module import path `github.com/bborbe/agent`).
- `README.md` — the package table.
- `prompts/completed/223-spec-055-claude-streaming-session.md` — the completed prompt that shipped this spec.

Coding-plugin docs (paths as they exist INSIDE the container), read before judging whether a finding is a real defect:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; private struct named as the interface with a lowercased first letter; `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external `*_test` packages, counterfeiter directives, coverage >= 80%.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage and verification rules.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape.

Load-bearing facts, verified against this working tree. Do NOT re-derive them, do NOT contradict them, and do NOT "correct" the code to match a different shape:
1. The shared seam is `lib.Session` with methods `Prompt(ctx context.Context, prompt string) (string, error)` and `Close(ctx context.Context) error`, and `lib.SessionFactory` with `Create(id string) Session`, defined in `agent_session.go` (package `lib`, imported elsewhere as `agentlib "github.com/bborbe/agent"`). Sibling spec 054 is at `status: verifying`; the dependency has landed.
2. `claude.NewSession(config ClaudeRunnerConfig, decider PermissionDecider) agentlib.Session` and `claude.NewSessionFactory(base ClaudeRunnerConfig, decider PermissionDecider) agentlib.SessionFactory` exist in `claude/claude-session.go`. `PermissionDecider` is `DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)` with the counterfeiter directive on the interface line.
3. `ClaudeRunnerConfig` (`claude/claude-runner-config.go`) has exactly five fields — `ClaudeConfigDir`, `AllowedTools`, `Model`, `WorkingDirectory`, `Env` — and no session-continuity field. This is a spec 055 hard veto.
4. `claude.ClaudeRunner.Run(ctx context.Context, prompt string) (*ClaudeResult, error)` is unchanged, and `buildSubprocessEnv` / `resolveConfigDir` were extracted to package-level functions in `claude/claude-runner.go` (called by the session's `startProcess`).
5. `claude/claude-session.go` contains exactly one `exec.Command(...)` call site, inside `startProcess`.
6. `docs/claude-streaming-protocol.md` exists and carries six `##` sections.
7. `CHANGELOG.md` carries a released `## v0.92.0` entry describing the Claude session.
8. `prompts/completed/223-spec-055-claude-streaming-session.md` is `status: completed` for `spec: [055-claude-streaming-session]`.
9. `.dark-factory.yaml` sets `workflow: direct` with no `hideGit`. This prompt nonetheless uses plain `grep`/`find`, never bare `git`, so verification cannot read as a false pass.

This is a reconciliation prompt. The default outcome is NO code change. Only a concrete, evidenced defect — a test that no longer passes, a spec Constraint that no longer holds, or a repository-hygiene deliverable that is missing — justifies an edit, and then only the minimal repair.
</context>

<requirements>

## 0. Reconcile AC1 (one held process serves the whole session)

Spec 055 AC1 requires a test that records every byte written to the fake process's stdin and asserts one process start across two sends, both turns' payloads on that one process's stdin, the process still alive at turn 2, and both protocol flags in the constructed command.

Confirm, with named evidence:
- Run the `claude` package suite (`go test -mod=mod -race -v ./claude/`, log to `/tmp/claude-session-test.log`) and confirm it exits 0.
- Confirm each of these `It` blocks is present in the log and passing:
  - `starts exactly one process across two sends`
  - `writes both turns' payloads to that one process's stdin`
  - `the process is still running when the second turn is written`
  - `spawns with the stream-json protocol flags`
  - `creates the process in exactly one place`
- Confirm the structural facts those tests rely on: `claude/claude-session.go` contains exactly ONE `exec.Command(...)` call site (in `startProcess`), and both `--input-format stream-json` and `--output-format stream-json` appear in `sessionArgs`.
- Confirm the Failure Modes rows are covered by tests: `fails the next turn when the held process died between turns` (dead process), `fails the turn on an unparseable event without logging its content` (malformed event + no content logging), `fails the turn when the permission decider errors` (HTTP surface unavailable), `fails the turn when the process exits without a result event` (idle timeout / vocabulary change).

If any of these fails or is missing, that is a real finding — go to requirement 5.

## 1. Reconcile AC3 (substitutable in the shared service)

Spec 055 AC3 requires the Claude session to be substitutable for the pi implementation in the shared service, driven through the HTTP path with the CLI boundary faked.

Confirm, with named evidence:
- Run `go test -mod=mod -race -v ./interactive/` (log to `/tmp/interactive-test.log`) and confirm it exits 0.
- Confirm the log shows the shared service's frozen-contract table being driven by the Claude backend — the `Describe("Service with the claude session backend")` block and the `frozen :9090 contract (claude backend)` table run, plus the `holds one process across two prompts on one session id` row.
- Confirm `interactive/claude-session_test.go` constructs the service with `claude.NewSessionFactory(...)` and does NOT copy the contract table (it reuses the shared `contractEntries` / `runContractTable`).

If the shared contract table is no longer driven by the Claude backend, that is a real finding — go to requirement 5.

## 2. Reconcile the Repository-hygiene deliverables

Spec 055 Constraints require: `README.md` updated, a `CHANGELOG.md` entry, and `docs/claude-streaming-protocol.md` written.

Confirm:
- `README.md` names the Claude session implementation (its package table row mentions the long-lived process / stream-json protocol and the `PermissionDecider`).
- `CHANGELOG.md` carries the released entry describing the feature. Note which released version carries it (`v0.92.0`) and record that in the report — the spec's "under `## Unreleased`" wording is satisfied by the entry having been released.
- `docs/claude-streaming-protocol.md` exists with at least five `##` sections covering launch, input turn shape, output event vocabulary, turn-boundary rule, and the permission-request shape.

A missing deliverable is a real finding — go to requirement 5.

## 3. Reconcile the spec's Constraints and frozen surfaces

Confirm each spec 055 Constraint still holds:
- **`ClaudeRunnerConfig` gains no flag pair (hard veto):** `claude/claude-runner-config.go` contains no `SessionID` / `PersistSession` / session-continuity field.
- **`claude.ClaudeRunner` is frozen:** the signature `Run(ctx context.Context, prompt string) (*ClaudeResult, error)` is unchanged, and `claudeRunner.Run`'s behaviour is unchanged.
- **No Python SDK:** no `ClaudeSDKClient` / `claude_agent_sdk` reference anywhere in the Go tree.
- **Process hygiene:** the session has no raw `go func(){...}()`; it uses `context.AfterFunc` watchers that are stopped before the turn returns and never outlive `Close`.
- **Error handling:** errors in `claude/claude-session.go` are wrapped with `github.com/bborbe/errors`, never `fmt.Errorf` and never a bare `return err`.

A violated Constraint is a real finding — go to requirement 5.

## 4. Regression check against the follow-on releases

Spec 055's package was touched after v0.92.0. Confirm the follow-on changes did not regress spec 055's behaviour:
- **v0.93.1** added `--permission-mode manual` to `sessionArgs` when a decider is wired. Confirm the flag pair is added and removed together: the tests `asks the CLI for a verdict when a decider is wired` and `leaves the permission flags off when no decider is wired` both pass, so a session built without a decider still spawns without `--permission-mode`.
- **v0.94.0** changed the `interactive` constructors to take an explicit `interactive.Auth` decision. Confirm `interactive/claude-session_test.go` still compiles and the Claude-backend contract table still passes — i.e. the constructor change did not leave the AC3 test behind.
- **v0.93.0** added the `interactive` permission endpoint. Confirm nothing in `claude/claude-session.go` was duplicated into `interactive/` or vice versa (the `PermissionDecider` seam stays in `claude/`; the HTTP surface stays in `interactive/`).

If a regression is present, that is a real finding — go to requirement 5.

## 5. Repair — only on a genuine, evidenced finding

Make a code change ONLY if requirements 0-4 surfaced a concrete defect. A "concrete defect" is one of: a named test that fails, a spec Constraint that no longer holds, a Repository-hygiene deliverable that is missing, or a regression from a follow-on release that breaks spec 055's behaviour.

- Repair the defect minimally, inside the scope of the files spec 055 owns: `claude/claude-session.go`, `claude/claude-session_test.go`, `interactive/claude-session_test.go`, `docs/claude-streaming-protocol.md`, `README.md`, `CHANGELOG.md`.
- Do NOT rewrite shipped code to look like fresh work. Do NOT refactor, rename, or "tidy" passing code.
- Do NOT change `claude.ClaudeRunner`, `claudeRunner.Run`, or `ClaudeRunnerConfig`'s field set.
- Do NOT add a field to `ClaudeRunnerConfig`.
- Do NOT define a second copy of the shared session seam — it belongs to spec 054.
- If a repair is made, add a `CHANGELOG.md` entry under `## Unreleased` (creating it above the newest released section if absent) describing the fix, per `changelog-guide.md`.

## 6. Record the reconciliation finding

If no repair was needed (the expected outcome), the deliverable is the record itself, written into the completion report and the `## Improvements` section — not a new file:
- For each of AC1 and AC3, state the named evidence found (the test names and that they pass) and the verdict.
- State that the spec's Repository-hygiene deliverables and Constraints were confirmed present.
- State that AC2 is operator-executable only and was therefore not run in-container, naming it as the one outstanding spec item.
- Record the finding that spec 055 appears fully shipped as v0.92.0 and that the completed prompt `223-spec-055-claude-streaming-session.md` is the shipping artifact, so a human can decide whether the spec should be closed.
- In `## Improvements`, raise the open question from the top-of-file comment: whether regenerating a prompt for an already-shipped spec was intended.

Do NOT write a report/summary/analysis `.md` file into the repository. The record lives in the completion report dark-factory appends.

## 7. Scope containment

When a repair is needed, edit ONLY the spec 055 files listed in requirement 5. When no repair is needed (the default), make NO code change at all. Do NOT touch `pi/`, `metrics/`, `delivery/`, `healthcheck/`, `agent-step.go` (repository root), `claude/agent-step.go`, `claude/task-runner.go`, `claude/result-deliverer.go`, or `claude/transcript.go`.

</requirements>

<constraints>
- **Reconciliation, not re-implementation.** The default outcome is NO code change. Every edit must be justified by a concrete, evidenced defect from requirements 0-4. Do NOT rewrite, refactor, rename, or cosmetically improve shipped code. Spec 055's in-container work is already released as v0.92.0.
- **Dependency direction.** This spec depends on `specs/in-progress/054-shared-interactive-agent-service.md` for the shared session interface, which has landed (`agent_session.go`, spec 054 at `status: verifying`). Do NOT define, duplicate, or move that interface — spec 055 lists it as a Non-goal.
- **No Python SDK.** `ClaudeSDKClient` is a Python class from `claude-agent-sdk-python` and is NOT importable in this Go repository. Do not import, reference, vendor, or shell out to a Python SDK.
- **`ClaudeRunnerConfig` gains no flag pair (hard veto).** Add NO field to `ClaudeRunnerConfig` and NO opt-out flag anywhere. pi's `PersistSession`/`SessionID` exists because pi is a subprocess that must be told to persist; the Claude session holds its own process, so there is no subprocess flag pair to mirror.
- **`claude.ClaudeRunner` is frozen.** Its signature `Run(ctx context.Context, prompt string) (*ClaudeResult, error)`, its generated mock, and `claudeRunner.Run`'s observable behaviour all stay exactly as they are. The session is a sibling of the runner, not a replacement.
- **Process hygiene.** One process per session, terminated on `Close` and on cancellation of an in-flight turn. No background reader goroutine; no stdout read concurrent with a stdin write outside a single turn; no goroutine outliving `Close`.
- **No raw goroutines.** `go-concurrency/no-raw-go-func` is a MUST rule: no raw `go func(){...}()` outside `main.go` / `cmd/**`.
- **Error handling.** Errors are wrapped with `github.com/bborbe/errors` — `errors.Wrap(ctx, err, "…")` / `errors.Wrapf(ctx, err, "…")`, `errors.New(ctx, "…")`, `errors.Errorf(ctx, "…")`. Never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in business logic.
- **Code conventions** (per `docs/dod.md`). Exported types, functions and interfaces carry GoDoc comments. Interface → Constructor → Struct → Method, with the implementation struct private and named as the interface with a lowercased first letter. No package-level mutable state and no `init()`.
- **Security.** Turn content is never logged — a malformed line's SHAPE (byte length, at most a short digest) may be recorded, its content may not. The tool-input preview is never logged. The process is spawned with an argument vector, never a shell string.
- **No probe word.** Spec 055 AC2 asks the session to remember a word the operator picks AT RUN TIME. Do not write any such word, or any test that depends on one, into this repository. The in-container tests prove continuity by asserting both turns reach one process's stdin.
- **Tests.** Ginkgo v2 / Gomega in external `*_test` packages (`claude_test`, `interactive_test`), counterfeiter mocks only — never hand-write a mock. Coverage for any repaired code must be >= 80%.
- **Limits.** golines line length 100; funlen 80 lines / 50 statements; nestif complexity 4.
- **No new module.** No dependency is added and no `replace` or `exclude` directive is introduced.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable; none requires Docker, a cluster, or the dark-factory CLI.

```bash
# 1. AC1 — the held-process tests exist and pass.
go test -mod=mod -race -v ./claude/ > /tmp/claude-session-test.log 2>&1
# Must exit 0. Each of these patterns must match at least one line:
grep -E "starts exactly one process across two sends|writes both turns' payloads to that one process's stdin|the process is still running when the second turn is written|spawns with the stream-json protocol flags|creates the process in exactly one place" /tmp/claude-session-test.log

# 2. AC1 structural — one spawn site, both protocol flags constructed.
grep -cE ':= *exec\.Command(Context)?\(' claude/claude-session.go   # must print exactly 1
grep -n -- '--input-format' claude/claude-session.go                 # must match at least one line
grep -n -- '--output-format' claude/claude-session.go                # must match at least one line

# 3. AC3 — the Claude backend drives the shared service's frozen-contract table.
go test -mod=mod -race -v ./interactive/ > /tmp/interactive-test.log 2>&1
# Must exit 0. Each of these patterns must match at least one line:
grep -E "claude backend|holds one process across two prompts on one session id" /tmp/interactive-test.log

# 4. Repository hygiene.
grep -n 'claude-session\|stream-json' README.md                      # must match at least one line
grep -n '## v0.92.0' CHANGELOG.md                                    # must match
grep -c '^## ' docs/claude-streaming-protocol.md                     # must print >= 5

# 5. Frozen surfaces (spec Constraints).
grep -n 'SessionID\|PersistSession' claude/claude-runner-config.go   # must print NOTHING
grep -c 'Run(ctx context.Context, prompt string) (\*ClaudeResult, error)' claude/claude-runner.go  # exactly 1

# 6. No Python SDK anywhere in the Go tree.
grep -rn 'claude_agent_sdk\|ClaudeSDKClient' --include='*.go' .      # must print NOTHING

# 7. Full pipeline — format, generate, test, lint, license.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `make precommit` once more. If step 5 shows a `SessionID`/`PersistSession` field in `claude-runner-config.go`, or step 6 prints a Python-SDK reference, that is a spec 055 Constraint violation and must be repaired before the report is written.

Note on the reconciliation outcome: if the expected case holds — all steps pass, no Constraint is violated, no deliverable is missing — the run makes NO code change. The verification above still runs and its results are the evidence recorded in the completion report. Report `status: success` only when `make precommit` exits 0.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **This spec appears already shipped.** `prompts/completed/223-spec-055-claude-streaming-session.md` is a completed prompt for `spec: [055-claude-streaming-session]`, `claude/claude-session.go` is implemented and tested, `docs/claude-streaming-protocol.md` exists, and `CHANGELOG.md` released the feature as v0.92.0. This prompt is deliberately a reconciliation, not a re-implementation. If regeneration was not intended, reject it rather than approving it.
- **AC2 is operator-only and is deliberately not a container check.** The real `claude` binary is not present in the build container, so the two-turn memory probe (operator-chosen word, both responses, the `ps` observation taken during turn 2, and the pinned `claude --version`) runs on the host after merge and is recorded in the PR description. No probe word is written into the repository. This matches how prompt 223 handled it.
- **The spec is at `status: generating` in `specs/in-progress/`.** Moving or closing the spec is a dark-factory CLI action owned by the operator; this prompt does not edit the spec's frontmatter or move the file.
- **The `## Unreleased` wording is satisfied by release.** Spec 055's Repository-hygiene constraint says the CHANGELOG entry goes under `## Unreleased`; the entry shipped and is now under `## v0.92.0`. The prompt treats the released entry as satisfying the constraint rather than asking the executor to move it back.
- **Git-dependent checks are avoided.** The prompt uses plain `grep`/`find` rather than `git grep`/`git diff`; `.dark-factory.yaml` sets `workflow: direct` with no `hideGit`, but the daemon does not check verification exit codes, so a bare `git` command that failed would read as a pass.
