---
status: draft
spec: [055-claude-streaming-session]
created: "2026-10-21T07:28:39Z"
branch: dark-factory/claude-streaming-session
---

<summary>
- The Claude streaming-session feature described by spec 055 is already present in this tree; this prompt confirms it still meets the spec instead of building it a second time.
- It checks that one held process serves two turns on the same session, that both turn payloads reach that process's stdin, and that the process is still alive when the second turn is written.
- It checks that the spawn command carries the streaming-input and streaming-output flags.
- It checks that the shared interactive service can be driven with the Claude session implementation through the same frozen-contract table used for the mock backend.
- It checks that the failure paths the spec names are covered: dead process, unparseable event, decider error, cancellation, and close.
- It checks that the protocol reference document, the README note, and the changelog entry still exist and still describe what the code does.
- If any of those checks is missing, the prompt adds only the missing evidence and leaves the working implementation alone.
- The real end-to-end check against the actual Claude CLI stays an operator step on the host; this prompt records it as a hand-off rather than pretending to run it.
</summary>

<objective>
Reconcile the already-landed Claude streaming-session implementation with spec 055: verify that Acceptance Criteria 1 and 3 still hold in the current tree (which has since gained the permission endpoint, authentication, A2A, claims streaming and permission-source specs), add only any genuinely missing test or documentation evidence, and record Acceptance Criterion 2 as an operator-executable hand-off. Do NOT reimplement `claude/claude-session.go` and do NOT overwrite the existing tests.
</objective>

<context>
Repository root inside the container is `/workspace`. This is a single Go module `github.com/bborbe/agent`, Go 1.27.1. Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Read IN FULL before changing anything (repo-relative):
- `specs/in-progress/055-claude-streaming-session.md` — the spec being verified. Pay particular attention to Acceptance Criteria 1 and 3, Failure Modes, Security / Abuse Cases, Constraints, and Repository hygiene.
- `specs/in-progress/054-shared-interactive-agent-service.md` — the sibling spec that owns the shared session interface and the HTTP service.
- `agent_session.go` — the shared interface. The root package is named `lib`; its import path is `github.com/bborbe/agent` and it is imported elsewhere as `agentlib`.
- `claude/claude-session.go` — the implementation under verification.
- `claude/claude-session_test.go` — the AC1 and failure-mode tests.
- `interactive/claude-session_test.go` — the AC3 wiring of the Claude backend into the shared service's contract table.
- `interactive/service_test.go` — the shared contract table helpers `runContractTable` and `contractEntries`, and the mock-backed run at the end of the file.
- `claude/claude-event.go` — the stream-json vocabulary the session reuses.
- `claude/claude-runner-config.go` — `ClaudeRunnerConfig`, which the session takes unchanged.
- `claude/claude-runner.go` — the one-shot runner, and the package-level helpers `buildSubprocessEnv` and `resolveConfigDir` the session reuses.
- `docs/claude-streaming-protocol.md` — the protocol reference deliverable.
- `README.md` — the package table.
- `CHANGELOG.md` — the historical entry for this feature.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md`

Load-bearing facts verified against this working tree. Do not contradict them and do not re-derive them:

1. The shared interface is:
   ```go
   type Session interface {
       Prompt(ctx context.Context, prompt string) (string, error)
       Close(ctx context.Context) error
   }
   type SessionFactory interface {
       Create(id string) Session
   }
   ```
   It lives in `agent_session.go` (package `lib`, import path `github.com/bborbe/agent`).

2. The implementation constructors are:
   ```go
   func NewSession(config ClaudeRunnerConfig, decider PermissionDecider) agentlib.Session
   func NewSessionFactory(base ClaudeRunnerConfig, decider PermissionDecider) agentlib.SessionFactory
   ```
   The permission seam is:
   ```go
   type PermissionDecider interface {
       DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)
   }
   ```
   `PermissionRequest` carries `ToolName`, `Description`, `InputPreview` (all `string`) and `Source PermissionSource`; `PermissionDecision` carries `Allow bool` and `Message string`.

3. `claude/claude-session.go` contains exactly one `exec.Command` call site, in `startProcess`. `sessionArgs` builds the argv. `Prompt` never spawns.

4. `claude/claude-session_test.go` is in the external `package claude_test` and already contains, under `Describe("claudeSession held process")`:
   - `It("starts exactly one process across two sends")`
   - `It("writes both turns' payloads to that one process's stdin")`
   - `It("the process is still running when the second turn is written")`
   - `It("spawns with the stream-json protocol flags")`
   - `It("creates the process in exactly one place")`
   and under `Describe("claudeSession failure and permission")` the dead-process, malformed-event, permission-routing, decider-error, argv-stability, `Close` and cancellation tests.

5. `interactive/claude-session_test.go` wires `claude.NewSessionFactory` into the shared service through `newClaudeBackend(shimDir)` and runs the SAME table via `runContractTable("frozen :9090 contract (claude backend)", ...)`; it also has `It("holds one process across two prompts on one session id")`. `contractEntries` and `runContractTable` live in `interactive/service_test.go`.

6. `docs/claude-streaming-protocol.md` exists and has sections `Launch`, `Input turn shape`, `Output event vocabulary`, `Turn-boundary rule`, `Permission-request shape`, `Version confirmation`.

7. `README.md` already names the root `Session` / `SessionFactory` seam and the `claude/` session implementation; `CHANGELOG.md` already carries the feature entry under `## v0.92.0` (this repository has since released through `## v0.109.0`, and has no `## Unreleased` section at the top).

8. Both suites set a 60-second Ginkgo timeout (`claude/claude_suite_test.go`, `interactive/interactive_suite_test.go`).

9. The current tree is NOT a git checkout (`git status` fails with "not a git repository"), so every check below uses `grep`/`ls`/`go test`, never `git`.
</context>

<requirements>

## 0. Premise and read-only first pass

The implementation for this spec has already landed. Treat the existing files as the deliverable and this prompt as its acceptance re-check.

1. Read every file listed in `<context>` before editing anything.
2. Run the checks in `<verification>` steps 1-11 in order and record each result.
3. If every check passes, make NO source change. The completion report states that the spec's evidence was verified in place and lists the commands that passed.
4. Only if a specific check fails may you change files, and only in the way requirement 8 permits.

Do not create a second session implementation, a second `PermissionDecider`, a second session factory, or a parallel event vocabulary. Spec 055 Non-goals make the existing seam the single one.

## 1. Session-interface conformance

Confirm `claude.NewSession` returns `agentlib.Session` and that the implementation satisfies it with exactly the interface's two methods:

- `Prompt(ctx context.Context, prompt string) (string, error)`
- `Close(ctx context.Context) error`

Confirm `claude.NewSessionFactory` returns `agentlib.SessionFactory` whose `Create(id string) Session` builds a session per id.

If, and only if, the shared interface has gained methods since this prompt was written, adapt the implementation to the interface as it exists in `agent_session.go`; never change `agent_session.go` from this prompt.

## 2. AC1 — one process serves both turns

Confirm these evidence tests exist and pass, with these exact names:

1. `starts exactly one process across two sends`
2. `writes both turns' payloads to that one process's stdin`
3. `the process is still running when the second turn is written`
4. `spawns with the stream-json protocol flags`
5. `creates the process in exactly one place`

The assertions must be behavioural, not shape-only:

- Test 1 asserts the shim's `starts.log` has exactly one line across two `Prompt` calls.
- Test 2 asserts `stdin.log` has exactly two lines, the first carrying `turn-one` and the second `turn-two`, and that the first line parses as JSON with `type == "user"`.
- Test 3 asserts the pid that consumed turn 2 equals the pid recorded at start and still answers `Signal(syscall.Signal(0))`; `*exec.Cmd` identity alone is explicitly not sufficient.
- Test 4 asserts the argv contains `--input-format` followed by `stream-json`, and `--output-format` followed by `stream-json`.
- Test 5 asserts `exec.Command` appears exactly once in `claude/claude-session.go`.

If a test is missing, add it to `claude/claude-session_test.go` following the existing shim pattern in that file (the shim reads `$SHIM_DIR`, which the test passes through `ClaudeRunnerConfig.Env`). Do not weaken or rename an existing passing test.

## 3. AC3 — substitutable in the shared service

Confirm `interactive/claude-session_test.go` constructs the shared service with `claude.NewSessionFactory` and drives the shared contract table through the HTTP path with the CLI boundary faked.

- The table must remain single-sourced: it must come from `contractEntries()` / `runContractTable(...)` in `interactive/service_test.go`, not a copied list.
- The Claude-backed run must still be present alongside the mock-backed run.
- `It("holds one process across two prompts on one session id")` must still pass.

If the shared service's constructor signature has changed since this prompt was written (specs 056-061 added permissions, authentication, A2A and push options), update ONLY the Claude-backend test wiring to the current constructor; do not change the service or the mock backend.

## 4. Failure-mode coverage

Confirm the following behaviour tests exist and pass in `claude/claude-session_test.go`:

1. `fails the next turn when the held process died between turns` — a later turn errors and `starts.log` still has one line (no silent re-spawn).
2. `fails the turn on an unparseable event without logging its content` — the error and captured `glog` output contain neither the canary nor turn content, only the line's shape.
3. `routes a permission request out and writes the verdict back` — the decider receives the tool name and the response carries the same `request_id`.
4. `fails the turn when the permission decider errors` — no result is returned and the turn does not hang.
5. `fails the turn when a permission is requested with no decider` — a nil decider fails the turn rather than blocking.
6. `builds the same argv regardless of the session id` — the argv is independent of the id.
7. `terminates the process on Close` — `exited.log` appears within a bounded `Eventually`.
8. `terminates the process when an in-flight turn's context is cancelled` — the send errors, a later send fails, one process was started, and the pid no longer answers signal 0.

If any of these is absent, add it; do not replace an existing equivalent test with a weaker one.

## 5. Protocol reference

Confirm `docs/claude-streaming-protocol.md` still describes what the code actually does, including all six sections listed in `<context>` fact 6. Specifically:

- the launch argv matches `sessionArgs` in `claude/claude-session.go`, including `--permission-prompt-tool stdio` and `--permission-mode manual` when a decider is configured;
- the input turn shape matches `sessionTurn` / `writeTurn`;
- the output vocabulary matches `claudeEvent` plus `sessionEvent`'s `control_request` envelope;
- the permission-request and response shapes match `controlRequest` and `writeControlResponse`/`writePermissionResponse` as they exist in the tree;
- the version-confirmation section still says the flag set is confirmed by the operator at AC2 time, because the build container has no `claude` binary.

If a section drifted from the code, correct the DOCUMENT to match the CODE; do not change working code to match prose.

## 6. Repository hygiene

- `README.md` names the shared `Session` / `SessionFactory` seam and the `claude/` session implementation. If either mention has been removed, restore it in the surrounding style.
- `CHANGELOG.md` already carries the feature entry under `## v0.92.0`. Do NOT add a duplicate entry and do NOT create an `## Unreleased` section for a verification that changes no code. Only if requirement 8 makes a substantive code or test change, add one bullet under `## Unreleased` (creating the section directly above the newest released `## vX.Y.Z` section) following the changelog guide.

## 7. AC2 — operator hand-off, not container evidence

Do not attempt to run the real `claude` binary: it is absent from the build container.

- Do not write any probe word, expected reply, or credential into the repository or the tests.
- Record in the completion report that AC2 remains an operator-executable hand-off: two sequential prompts on one session id against the host's real CLI, with the operator choosing the word at run time, both responses and the `ps` observation taken during turn 2, plus `claude --version` (or `npm ls -g @anthropic-ai/claude-code`) against the same binary, all recorded in the PR description.
- Do not claim any container test as AC2 evidence.

## 8. Gap-closure rules

If a check fails, fix the smallest thing that makes the spec's evidence true:

- Missing AC1/failure-mode test -> add it to `claude/claude-session_test.go`.
- Missing or drifted AC3 wiring -> update `interactive/claude-session_test.go`.
- Drifted protocol prose -> update `docs/claude-streaming-protocol.md`.
- Missing README mention -> restore it.
- A real behavioural bug in `claude/claude-session.go` -> fix it in place, preserving the held-process design (one spawn, one process per session, no re-spawn between turns).

Never do any of the following while closing a gap:

- add a field to `ClaudeRunnerConfig`;
- change `claude.ClaudeRunner`, `claudeRunner.Run`, or the generated ClaudeRunner mock;
- introduce a Python SDK import or a shell string for the spawn;
- add a background reader goroutine or a raw `go func`;
- rename or delete an existing passing test;
- change `agent_session.go`, `interactive/service.go`, the mock backend, or `pi/`.

## 9. Scope containment

Edit only these files, and only if a check requires it:

- `claude/claude-session.go`
- `claude/claude-session_test.go`
- `interactive/claude-session_test.go`
- `docs/claude-streaming-protocol.md`
- `README.md`
- `CHANGELOG.md`

Do not touch `pi/`, `metrics/`, `delivery/`, `healthcheck/`, the root `agent_*.go` files other than reading them, `claude/claude-runner.go`, `claude/claude-runner-config.go`, or any spec file.

</requirements>

<constraints>
- **Do not reimplement.** The feature already exists in `claude/claude-session.go`. Overwriting it is a failure of this prompt, not a success. A no-change outcome is the expected outcome when every check passes.
- **One process per session (hard requirement).** Continuity comes from the held process, never from `--resume`, `--session-id`, or a transcript file re-read between turns. `exec.Command` appears exactly once in `claude/claude-session.go`.
- **`ClaudeRunnerConfig` gains no field (hard veto).** Spec 055 Constraints: there is no subprocess flag pair to mirror and no consumer for one. Add NO field and NO opt-out flag.
- **`claude.ClaudeRunner` is frozen.** Its signature `Run(ctx context.Context, prompt string) (*ClaudeResult, error)`, its mock, and `claudeRunner.Run`'s observable behaviour stay unchanged. The session is a sibling, not a replacement.
- **No Python SDK.** `ClaudeSDKClient` is a Python class and is not importable here. Never import, vendor, reference in Go code, or shell out to it.
- **Process hygiene.** One process per session; terminated on `Close` and on cancellation of an in-flight turn. No stdout read concurrent with a stdin write outside a single turn. No goroutine outliving `Close`. No raw `go func` outside `main.go` / `cmd/**`; if a goroutine is genuinely needed, use `github.com/bborbe/run`.
- **Error handling.** Wrap with `github.com/bborbe/errors`: `errors.Wrap(ctx, err, "…")`, `errors.New(ctx, "…")`, `errors.Errorf(ctx, "…")`. Never `fmt.Errorf`, never a bare `return err`, never `context.Background()` in business logic.
- **Security.** Turn content is never logged; a malformed line's shape (byte length) may be recorded, its content may not. The tool-input preview is never logged. The process is spawned with an argument vector, never through a shell. The permission verdict is the only instruction accepted from outside the process.
- **No probe word.** Spec 055 AC2 requires a word the operator picks at run time. Write none into this repository.
- **Tests.** Ginkgo v2 / Gomega in external `*_test` packages; counterfeiter mocks only (`mocks.ClaudePermissionDecider`), never hand-written mocks; coverage for new or changed code >= 80%.
- **Lint limits.** golines 100, funlen 80 lines / 50 statements, nestif 4.
- **No new module or dependency.** Do not add a `replace` or `exclude` directive.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands are container-executable. This tree is not a git checkout, so no `git` command is used.

```bash
# 1. The implementation, its tests, the contract wiring and the protocol doc exist.
ls /workspace/claude/claude-session.go \
   /workspace/claude/claude-session_test.go \
   /workspace/interactive/claude-session_test.go \
   /workspace/docs/claude-streaming-protocol.md \
   /workspace/mocks/claude-permission-decider.go

# 2. Exactly one process spawn in the session file.
grep -cE ':= *exec\.Command(Context)?\(' /workspace/claude/claude-session.go
# Must print exactly 1.

# 3. The streaming protocol flags are constructed.
grep -n -- '--input-format' /workspace/claude/claude-session.go
grep -n -- '--output-format' /workspace/claude/claude-session.go
# Each must match at least one line. Smoke check only; the shim tests are the evidence.

# 4. No Python SDK reference anywhere in the Go tree.
! grep -rn 'claude_agent_sdk\|ClaudeSDKClient' --include='*.go' /workspace
# Must print nothing.

# 5. No session-continuity field was added to the config.
! grep -n 'SessionID\|PersistSession' /workspace/claude/claude-runner-config.go
# Must print nothing.

# 6. The ClaudeRunner interface is unchanged.
grep -n 'Run(ctx context.Context, prompt string) (\*ClaudeResult, error)' /workspace/claude/claude-runner.go
# Must match exactly once.

# 7. AC1 evidence tests are present.
grep -n "starts exactly one process across two sends\|writes both turns' payloads to that one process's stdin\|the process is still running when the second turn is written\|spawns with the stream-json protocol flags\|creates the process in exactly one place" /workspace/claude/claude-session_test.go
# Must match five lines.

# 8. AC1 and failure-mode tests pass. (The suite takes ~35s; the Ginkgo timeout is 60s.)
go test -v ./claude/ > /tmp/claude-055-test.log 2>&1
# Must exit 0. Each of these must match at least one line:
grep -E "starts exactly one process across two sends|writes both turns' payloads to that one process's stdin|the process is still running when the second turn is written|spawns with the stream-json protocol flags" /tmp/claude-055-test.log
grep -E "fails the next turn when the held process died between turns|fails the turn on an unparseable event without logging its content|routes a permission request out and writes the verdict back|fails the turn when the permission decider errors|builds the same argv regardless of the session id|terminates the process on Close|terminates the process when an in-flight turn's context is cancelled" /tmp/claude-055-test.log

# 9. AC3 — the shared contract table runs with the Claude backend.
go test -v ./interactive/ > /tmp/interactive-055-test.log 2>&1
# Must exit 0. These must match:
grep -E 'frozen :9090 contract \(claude backend\)|holds one process across two prompts on one session id' /tmp/interactive-055-test.log
grep -c 'frozen :9090 contract' /tmp/interactive-055-test.log
# Must be >= 2 (the mock-backed run and the Claude-backed run).

# 10. The protocol reference is complete and in sync.
grep -c '^## ' /workspace/docs/claude-streaming-protocol.md
# Must be >= 6: Launch, Input turn shape, Output event vocabulary,
# Turn-boundary rule, Permission-request shape, Version confirmation.
grep -n -- '--permission-prompt-tool' /workspace/docs/claude-streaming-protocol.md
grep -n -- '--permission-mode' /workspace/claude/claude-session.go /workspace/docs/claude-streaming-protocol.md
# Both the code and the doc must mention the permission flags.

# 11. Coverage for the session file.
go test -coverprofile=/tmp/cover-055.out ./claude/ > /tmp/cover-055.log 2>&1 && go tool cover -func=/tmp/cover-055.out | grep -E 'claude-session|total'
# Every claude-session.go function must be >= 80%; the file total must be >= 80%.

# 12. No findings.
go vet ./...

# 13. Full pipeline — format, generate, test, lint, license.
make precommit
# Must exit 0.
```

If `make precommit` fails, fix the failure and re-run ONLY the failing target (`make lint`, `make vet`, etc.) until it passes, then run `make precommit` once more. A non-zero exit code is a failed run.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **This spec is already delivered in the working tree.** `claude/claude-session.go`, `claude/claude-session_test.go`, `interactive/claude-session_test.go`, `docs/claude-streaming-protocol.md`, the mock and the README/CHANGELOG entries all exist, and `go test ./claude/... ./interactive/...` passes (coverage 90.1% on `claude/`). The spec file nevertheless sits at `status: generating`, and prompt `223-spec-055-claude-streaming-session.md` is already in `prompts/completed/`. Regenerating the implementation prompt would overwrite landed code, so this prompt is deliberately an idempotent acceptance re-check: it may legitimately complete with no diff. If the daemon re-queued generation because the spec state machine is stuck rather than because work is missing, the correct fix is on the daemon/spec-state side, not another prompt.
- **AC2 is operator-only.** The real `claude` binary is absent from the build container. The two-turn memory probe (operator-chosen word, both responses, `ps` during turn 2, pinned `claude --version`) runs on the host after merge and is recorded in the PR description. No probe word is written into the repository.
- **The spec's `git grep` smoke check is replaced with plain `grep`.** The container is not a git checkout, so a bare `git` command would fail and — because the daemon does not gate on verification exit codes — could read as a pass. Every check above is a plain shell check.
- **The changelog entry is historical, not `## Unreleased`.** Spec 055 Repository hygiene asked for an `## Unreleased` entry; the feature has since been released under `## v0.92.0` and the repository is at `## v0.109.0` with no `## Unreleased` section. Adding a duplicate now would be churn, so requirement 6 adds one only if requirement 8 produces a substantive change.
- **Permission routing now carries a `Source`.** Later spec 060 extended `PermissionRequest` with the route that raised the turn. The protocol doc already documents it. This prompt verifies that extension is still described consistently; it does not re-introduce it.
</content>
