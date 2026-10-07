---
status: draft
spec: [055-claude-streaming-session]
created: "2026-10-07T11:03:45Z"
branch: dark-factory/claude-streaming-session
---

<summary>
- The Claude session behaviour this spec asks for — one long-lived Claude process per conversation, held across turns — is already in the tree, so this prompt confirms it rather than rebuilding it.
- Two prompts on one session id still reach the same process and the same conversation, and that stays proven by tests that record what the one process actually received on its stdin.
- A mid-turn tool-permission request still leaves the process, and the turn still waits on a real allow/deny verdict instead of hanging or auto-approving.
- The held process is still cleaned up when a session is closed and when an in-flight turn is cancelled, so a pod cannot accumulate orphaned processes.
- The one thing out of step with the code is the written protocol reference: it lists the flags the session spawns with and omits one flag the code has added since, so a reader reconstructs a command line that is not the one being sent.
- The reference now lists that flag and says why it is there, so the documented command line and the code agree again.
- Nothing else changes: the session implementation, its tests, the existing one-shot runner and the public configuration type are left exactly as they are, and the documented rule that the flag set lives in one place is kept.
- The end-to-end proof against the real Claude CLI stays an operator step on the host, because the build container has no Claude CLI installed and no probe word is written into the repository.
</summary>

<objective>
Bring the tree into the state spec 055 requires. The Claude session implementation the spec describes has already landed in this tree — one held process per session, a stream-json turn loop, permission routing out of the process, and `Close`/cancellation — so this prompt verifies each spec requirement against the current tree, names the test that proves it, and closes the one gap it finds: the written protocol reference understates the flag set the session actually spawns with. Where a requirement is already satisfied, leave the code unchanged. Do not rewrite a working implementation.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done. **This repo is a single Go module with exactly one `Makefile`, at the repo root** — run `make` there, never inside a package directory. (CLAUDE.md's "run in service dir, never at root" line is inherited from a sibling monorepo and does not describe this repo.)

Coding-plugin docs (read before editing, paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-architecture-patterns.md` — Interface → Constructor → Struct → Method; the interface is exported, the implementation struct is private, `New*` returns the interface.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 / Gomega, external test packages (`*_test`), counterfeiter directives, the suite timeout, coverage ≥ 80% for new code.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `github.com/bborbe/errors` conventions; no `fmt.Errorf`, no bare `return err`, no `context.Background()` in business logic.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-concurrency-patterns.md` — the `no-raw-go-func` MUST rule.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md` — counterfeiter mocks generated into `mocks/`.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — GoDoc comments on every exported type, field and function.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md` — `make precommit` composition; funlen 80, nestif 4, golines 100.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules.

Files to read IN FULL before editing:
- `specs/in-progress/055-claude-streaming-session.md` — the spec this prompt reconciles against (Goal, Non-goals, Acceptance Criteria, Desired Behavior, Constraints, Failure Modes, Security / Abuse Cases, Repository hygiene).
- `claude/claude-session.go` — the session implementation under verification: `PermissionRequest`, `PermissionDecision`, `PermissionDecider`, `NewSession`, `NewSessionFactory`, `sessionFactory`, `session`, `Prompt`, `process`, `abort`, `Close`, `detach`, `turn`, `readTurn`, `handle`, `answerPermission`, `endOfStream`, `sessionEvent`, `controlRequest`, `parseSessionEvent`, `writeTurn`, `writePermissionResponse`, `previewOf`, `sessionArgs`, `startProcess`.
- `claude/claude-session_test.go` — the AC1 suite and the failure-mode/permission suite (external `package claude_test`).
- `claude/claude-runner-config.go` — `ClaudeRunnerConfig`, the configuration the session reuses.
- `claude/claude-runner.go` — the one-shot runner that must stay untouched; also where `resolveConfigDir(ctx, config)` and `buildSubprocessEnv(ctx, config)` are package-level functions.
- `agent_session.go` — the shared `Session` / `SessionFactory` interface landed by spec 054.
- `interactive/claude-session_test.go` — the AC3 test that wires the Claude factory into the shared service's contract table.
- `docs/claude-streaming-protocol.md` — the protocol reference that requirement 6 corrects.

Load-bearing facts, verified against this working tree. Do not re-derive these, do not contradict them:

1. **The shared session interface already exists** (spec 054's prompt 1 has landed). `agent_session.go` declares `package lib` (module path `github.com/bborbe/agent`, imported elsewhere as `agentlib "github.com/bborbe/agent"`): `Session` has `Prompt(ctx context.Context, prompt string) (string, error)` and `Close(ctx context.Context) error`; `SessionFactory` has `Create(id string) Session`. Spec 055's names `Session`, `SessionFactory`, `OpenSession`, `Send`, `Close` are descriptive; these are the real ones. Do NOT define a second copy of this seam.
2. **`claude/claude-session.go` already exists and is complete.** It exports `NewSession(config ClaudeRunnerConfig, decider PermissionDecider) agentlib.Session`, `NewSessionFactory(base ClaudeRunnerConfig, decider PermissionDecider) agentlib.SessionFactory`, `PermissionRequest{ToolName, Description, InputPreview string}`, `PermissionDecision{Allow bool, Message string}`, and the `PermissionDecider` interface `DecidePermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)`.
3. **The spawn is `exec.Command("claude", args...)`** (never through a shell), and `exec.Command(` appears exactly once in the file, inside `startProcess`. A test asserts that count.
4. **`sessionArgs` builds the argv.** Its body, verbatim:
   ```go
   args := []string{
       "--print",
       "--input-format", "stream-json",
       "--output-format", "stream-json",
       "--verbose",
       "--strict-mcp-config",
   }
   if decider != nil {
       args = append(args, "--permission-prompt-tool", "stdio")
       // The CLI's default permission mode is its auto mode, which approves a
       // tool invocation without asking. Left unset, a wired decider is therefore
       // never consulted and the service's permission endpoint stays empty
       // however many turns run. manual is the mode that asks; it reports as
       // "default" in the CLI's init event, and --permission-prompt-tool carries
       // the ask to the decider.
       args = append(args, "--permission-mode", "manual")
   }
   if len(config.AllowedTools) > 0 {
       args = append(args, "--allowedTools", config.AllowedTools.String())
   }
   if config.Model != "" {
       args = append(args, "--model", config.Model.String())
   }
   return args
   ```
   The session id is never an argument, so the argv is identical for every id.
5. **`ClaudeRunnerConfig` (`claude/claude-runner-config.go`) has exactly five fields** — `ClaudeConfigDir ClaudeConfigDir`, `AllowedTools AllowedTools`, `Model ClaudeModel`, `WorkingDirectory AgentDir`, `Env map[string]string` — and no session-continuity field. Spec 055's Constraints make "no flag pair" a hard veto; keep it that way.
6. **`claude.ClaudeRunner` is frozen.** `Run(ctx context.Context, prompt string) (*ClaudeResult, error)` and `claudeRunner.Run`'s observable behaviour stay exactly as they are. The session is a sibling of the runner, not a replacement.
7. `github.com/bborbe/errors` is at `v1.6.1` and exports `New(ctx, message string) error`, `Errorf(ctx, format string, args ...interface{}) error`, `Wrap(ctx, err error, message string) error`, `Wrapf(ctx, err error, format string, args ...interface{}) error`, `Join`, `Is`, `As`, `Cause`. There is no `Newf`.
8. `mocks/claude-permission-decider.go` exists (counterfeiter fake name `ClaudePermissionDecider`).
9. **`docs/claude-streaming-protocol.md` exists** with six `##` sections: Launch, Input turn shape, Output event vocabulary, Turn-boundary rule, Permission-request shape, Version confirmation. Its Launch section currently ends its conditional-flag list like this, verbatim:
   ```
   Three further flags are conditional:

   - `--permission-prompt-tool stdio` — added only when a permission decider is
     configured. It is what makes the CLI raise a tool decision on the stdio control
     channel instead of resolving it itself.
   - `--allowedTools <list>` — added only when `ClaudeRunnerConfig.AllowedTools` is
     non-empty.
   - `--model <name>` — added only when `ClaudeRunnerConfig.Model` is non-empty.
   ```
   It does NOT mention `--permission-mode manual`, which fact 4 shows the code sends whenever a decider is wired. That is the gap requirement 6 closes.
10. `interactive/claude-session_test.go` exists and drives the shared service's single-sourced contract table (`contractEntries` / `runContractTable`) with `claude.NewSessionFactory` in place of the pi backend.
11. `README.md` already names the Claude session implementation, and `CHANGELOG.md` already carries an entry for it in a released `## vX.Y.Z` section. There is currently **no** `## Unreleased` section.
12. `.dark-factory.yaml` sets `workflow: direct`; the daemon runs with `hideGit=true`, so `.git` is masked in the container and **no `git` command is usable**. Use plain `grep`, never `git grep`. Also note `ROOTDIR` is empty under the masked `.git`, so pass `ROOTDIR=/workspace` to `make`.
</context>

<requirements>

## 0. Reconcile with the tree before changing anything

Read every file in `<context>`. For each of requirements 1–5, decide from the code whether the current tree already satisfies the spec 055 requirement, and record the decision together with the test that proves it. Then make exactly the change requirement 6 names, plus the CHANGELOG entry of requirement 7. Do not make any other edit.

## 1. Verify Desired Behavior 1 and 2 — one process per session, continuity from the process

Confirm by reading `claude/claude-session.go` and running the suite that:

- `claude.NewSession` returns an `agentlib.Session` whose first `Prompt` starts exactly one `claude` process and whose later `Prompt` calls write to that same process's stdin — never to a new one.
- The argv contains `--input-format` followed by `stream-json` and `--output-format` followed by `stream-json`.
- The session id is not passed to the process, so the argv is identical for every id.
- A held process that dies between turns surfaces as a per-turn error, and the next turn never silently starts a fresh conversation (`abort` marks the session closed; `process` then refuses).

No code change is expected. If a check fails, do not fix it here — report it (see requirement 8).

## 2. Verify Desired Behavior 3 — permission requests leave the process

Confirm that:

- A `control_request` whose `request.subtype` is `can_use_tool` (with `permission` accepted as the fallback spelling) is routed to the `PermissionDecider`, and the turn blocks on that call rather than polling or sleeping.
- The verdict is written back to the same process's stdin, carrying the same `request_id`, as `behavior: "allow"` or `behavior: "deny"`.
- A `PermissionDecider` error fails the turn — no auto-allow, no auto-deny, no retry — and a `control_request` with an unrecognised subtype also fails the turn rather than being ignored.
- `sessionArgs` adds `--permission-prompt-tool stdio` **and** `--permission-mode manual` together and only when a decider is wired (fact 4).

No code change is expected.

## 3. Verify Desired Behavior 4 — configuration reuse, no new field

Confirm `claude.NewSession` takes the existing `ClaudeRunnerConfig` and that `ClaudeRunnerConfig` still has exactly the five fields of fact 5, with no session-continuity field. No code change is expected.

## 4. Verify AC1 and AC3 coverage

Run the suites and confirm these tests exist and pass, and report their names:

- **AC1** in `claude/claude-session_test.go`: one process across two sends; both turns' payloads on that one process's stdin; the process still running when the second turn is written; the stream-json protocol flags in the spawned argv; and the single `exec.Command(` call site.
- **AC3** in `interactive/claude-session_test.go`: the shared service's single-sourced contract table driven with the Claude factory in place of the pi one, plus the one-process-across-two-prompts row.

**AC2 is operator-executable only.** The real `claude` binary is absent from the build container, so the two-turn memory probe (operator-chosen word, both responses, the `ps` observation, the pinned `claude --version`) runs on the host after merge. Do NOT add a container test for it, do NOT attempt to run the real CLI, and do NOT write any probe word into the repository.

## 5. Verify repository hygiene is intact

Confirm `README.md` names the Claude session implementation and that `docs/claude-streaming-protocol.md` carries its six sections. Confirm `CHANGELOG.md` carries an entry describing the Claude session. No code change is expected here.

## 6. Close the protocol-reference gap — the one substantive change this prompt makes

`docs/claude-streaming-protocol.md`'s **Launch** section (fact 9) lists three conditional flags and omits `--permission-mode manual`, which `sessionArgs` sends whenever a decider is wired (fact 4). Edit the Launch section so the documented flag set matches the code:

1. Change the lead-in so it no longer says "Three further flags are conditional" — four flags are conditional.
2. Add `--permission-mode manual` to the conditional-flag list, grouped with `--permission-prompt-tool stdio` and stated as added under the same condition (only when a permission decider is configured; the two are added and removed together).
3. State why it is there, in the document's own voice: the CLI's default permission mode approves a tool invocation without asking, so without `manual` a wired decider is never consulted and the permission endpoint stays empty however many turns run; `manual` is the mode that makes the CLI ask, and `--permission-prompt-tool stdio` carries the ask to the decider.
4. Keep, unchanged, the existing sentence that the flag set lives in exactly one function (`sessionArgs`) so a correction is a one-line change, and the existing sentence that the session id is deliberately not passed.

Then read the **Permission-request shape** section and update its closing paragraph **only if** it now contradicts the Launch section. Otherwise leave it alone. Do not restructure the document, do not renumber or rename sections, and do not touch any other section.

## 7. CHANGELOG entry

Add a `docs:` entry under `## Unreleased` describing the protocol reference now recording the `--permission-mode manual` flag the session sends when a permission decider is wired, and why. `## Unreleased` does not exist at this revision (fact 11): create it at the top, below the preamble and above the newest released `## vX.Y.Z` section. Follow `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — one bullet, a required `docs:` prefix, and specific enough to name the flag and the document.

## 8. Scope containment and gap reporting

Edit **only** these two files:

- `docs/claude-streaming-protocol.md`
- `CHANGELOG.md`

Do NOT modify any `.go` file, `mocks/`, `README.md`, `docs/interactive-service.md`, `agent_session.go`, `claude/claude-runner.go`, `claude/claude-runner-config.go`, or any spec.

If requirements 1–5 find a genuine gap between the spec and the code — a missing test, a missing failure-mode branch, a spec requirement with no implementation — do NOT fix it in this prompt. Finish the documentation change, then report the gap in your final report and in `## Improvements` with the category `PROMPT`, naming the spec requirement and the file. This prompt's job is to confirm the landed behaviour and reconcile its written reference; expanding into a code change would rewrite an implementation that already passes.

</requirements>

<constraints>
- **The session implementation is already landed.** `claude/claude-session.go` and `claude/claude-session_test.go` exist and pass. Do not create, rewrite, rename or restructure them. Do not re-derive the protocol in code.
- **The shared session interface is not this prompt's to define.** It already exists in `agent_session.go` (fact 1). Never define a second copy of the seam.
- **`ClaudeRunnerConfig` gains no field (hard veto).** Spec 055 Constraints: pi's `PersistSession`/`SessionID` exists because pi is a subprocess that must be told to persist; the Claude session holds its own process, so there is no subprocess flag pair to mirror and no consumer for one. Add NO field to `ClaudeRunnerConfig` and NO opt-out flag anywhere.
- **`claude.ClaudeRunner` is frozen.** Its signature, its generated mock and `claudeRunner.Run`'s observable behaviour stay exactly as they are. Existing tests pass unmodified.
- **Do not regress the landed permission flags.** `sessionArgs` must keep adding `--permission-prompt-tool stdio` **and** `--permission-mode manual` together when a decider is wired, and must keep adding neither when it is not. The tests `asks the CLI for a verdict when a decider is wired` and `leaves the permission flags off when no decider is wired` in `claude/claude-session_test.go` must keep passing.
- **No Python SDK.** `ClaudeSDKClient` is a Python class from `claude-agent-sdk-python` and is NOT importable in this Go repository. Do not import, reference, vendor or shell out to it. This repository targets the CLI protocol the SDK wraps.
- **No probe word.** Spec 055 AC2 asks the session to remember a word the operator picks at run time. Write no such word, and no test depending on one, into the repository.
- **Security.** Turn content is never logged — a malformed line's shape (byte length, at most a short digest) may be recorded, its content may not. The tool-input preview is never logged. The process is spawned with an argument vector, never a shell string.
- **Documentation only.** The only files this prompt may change are `docs/claude-streaming-protocol.md` and `CHANGELOG.md`. No Go file, no generated mock, no README.
- **No `git` commands.** `.git` is masked in the container (`hideGit=true`). Use `grep` / `find`, never `git grep`, `git diff` or `git log`.
- **Make target invocation.** Pass `ROOTDIR=/workspace` to `make` — the masked `.git` leaves the Makefile's `git rev-parse --show-toplevel` default empty, and `lint` resolves `.golangci.yml` through `$(ROOTDIR)`.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). Every command below is container-executable; none of them uses `git` and none needs Docker, a cluster or the dark-factory CLI.

```bash
# 1. The implementation and its protocol reference are present.
ls /workspace/claude/claude-session.go /workspace/docs/claude-streaming-protocol.md

# 2. The protocol reference now records the permission-mode flag the code sends.
grep -n -- '--permission-mode' /workspace/docs/claude-streaming-protocol.md
# Must match at least one line, in the Launch section.

# 3. Code and reference agree on the conditional flag pair.
grep -n -- '--permission-prompt-tool' /workspace/claude/claude-session.go /workspace/docs/claude-streaming-protocol.md
# Both files must match.

# 4. The flag set still lives in exactly one function in the code.
grep -c -- '--input-format' /workspace/claude/claude-session.go
# Must print exactly 1.

# 5. Exactly one process spawn in the session file.
grep -c 'exec.Command(' /workspace/claude/claude-session.go
# Must print exactly 1.

# 6. The config type gained no session-continuity field.
! grep -n 'SessionID\|PersistSession' /workspace/claude/claude-runner-config.go
# Must print nothing.

# 7. No Python SDK reference anywhere in the Go tree.
! grep -rn 'claude_agent_sdk\|ClaudeSDKClient' --include='*.go' /workspace
# Must print nothing.

# 8. The protocol reference keeps its six sections.
grep -c '^## ' /workspace/docs/claude-streaming-protocol.md
# Must be >= 6.

# 9. AC1's tests are present and pass.
go test -mod=mod -race -v ./claude/ > /tmp/claude-session-test.log 2>&1
grep -E "starts exactly one process across two sends|writes both turns' payloads to that one process's stdin|the process is still running when the second turn is written|spawns with the stream-json protocol flags|creates the process in exactly one place|asks the CLI for a verdict when a decider is wired" /tmp/claude-session-test.log
# Must exit 0 and match every pattern.

# 10. AC3's test is present and passes.
go test -mod=mod -race -v ./interactive/ > /tmp/interactive-test.log 2>&1
grep -E "Service with the claude session backend|holds one process across two prompts on one session id" /tmp/interactive-test.log
# Must exit 0 and match every pattern.

# 11. The CHANGELOG carries the entry.
grep -n '^## Unreleased' /workspace/CHANGELOG.md
grep -n -- '--permission-mode' /workspace/CHANGELOG.md
# Both must match.

# 12. No findings.
go vet ./...

# 13. Full pipeline — format, generate, test, lint, license.
ROOTDIR=/workspace make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `ROOTDIR=/workspace make precommit` once more. A non-zero exit code from `make precommit` means the run failed, whatever the reason.
</verification>

---

## REVIEWER OPEN QUESTIONS (audit-time only — not actionable by the executor)

- **This spec's implementation is already in the tree, and this prompt was triggered by a daemon state reset, not by a spec change.** The daemon log records `spec generation container not found, resetting spec to approved file=055-claude-streaming-session.md` immediately followed by `spec file created in in-progress, triggering generation`. The prior generation's output is `prompts/completed/223-spec-055-claude-streaming-session.md`, and its work is landed: `claude/claude-session.go` + `claude/claude-session_test.go` (the full AC1 suite, the failure-mode/permission suite, the `SHIM_DIR` shim, the single-`exec.Command` structural guard), `interactive/claude-session_test.go` (AC3), `docs/claude-streaming-protocol.md`, the README line, and the CHANGELOG entry in a released `## vX.Y.Z` section. **Before approving, confirm regeneration is intended.** If it is not, reject this prompt — the alternative reading is that the spec should simply be moved back to `prompted`/`verifying` rather than re-prompted.
- **This prompt is deliberately a reconciliation prompt, not a from-scratch implementation prompt.** Writing the spec's requirements as "create `claude/claude-session.go`" would instruct the executor to rebuild a working implementation, and would regress landed behaviour the spec does not mention: `sessionArgs` adds `--permission-mode manual` alongside `--permission-prompt-tool stdio` (changelog `v0.93.1`, measured against CLI 2.1.287) because the CLI's default permission mode auto-approves and a wired decider was otherwise never consulted. A faithful-from-scratch prompt would drop that flag and silently break the permission endpoint. Requirement 6 is therefore the only substantive change, and it fixes a real drift: the protocol reference's Launch section omits `--permission-mode manual`, so a reader reconstructs a command line the code does not send.
- **AC2 remains operator-only and is deliberately not a container check.** The real `claude` binary is not present in the build container, so the two-turn memory probe (operator-chosen word, both responses, the `ps` observation taken during turn 2, and the pinned `claude --version`) runs on the host after merge and is recorded in the PR description. No probe word is written into the repository.
- **The `--permission-mode manual` flag is not in spec 055's Assumptions flag set.** The spec's Assumptions name `claude -p --input-format stream-json --output-format stream-json --verbose` and require the exact flag set to be confirmed against the pinned CLI version and recorded with AC2's result. `manual` was added afterwards under `v0.93.1`. If the reviewer wants the spec text itself to record it, that is a spec edit, not a prompt edit — flagged rather than done, because spec 055 is in `specs/in-progress/` and owned by this generation's reviewer.
- **No scenario prompt.** Spec 055's Scenarios section records "No new scenario" and gives the reason: the behaviour is a process boundary and a protocol loop, both of which a test double holds precisely, and the end-to-end proof needs the real CLI, which the container lacks. This prompt follows that decision — AC1 and AC3 are the evidence, AC2 is the host-side probe.
- **Git-dependent and operator-only checks are kept out of `<verification>`.** `.dark-factory.yaml` sets `workflow: direct`, but the daemon runs with `hideGit=true` (its log records `hideGit=true hideGitSource=arg`), so `.git` is masked and a `git` command in the container would fail silently while reading as a pass — the daemon does not check verification exit codes. The spec's `git grep` smoke check is therefore expressed as plain `grep`, and `make` is invoked as `ROOTDIR=/workspace make ...` because the masked `.git` leaves the Makefile's `ROOTDIR ?= $(shell git rev-parse --show-toplevel)` empty and `lint` resolves `.golangci.yml` through `$(ROOTDIR)`.
