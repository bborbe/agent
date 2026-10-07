---
status: prompted
approved: "2026-10-01T05:03:51Z"
generating: "2026-10-01T07:38:27Z"
prompted: "2026-10-07T11:08:08Z"
branch: dark-factory/claude-streaming-session
---

## Summary

- `github.com/bborbe/agent` gains a Claude session implementation: one long-lived `claude` process per session, driven over the CLI's stream-json protocol on stdio.
- Today the Claude runner spawns `claude --print` once per call and exits. Nothing holds a process, so no conversation survives between requests and no permission gate can pause one.
- The pi side already has continuity through `--session-id`. Claude needs a different mechanism — streaming input mode — which is why the shared session interface exists as a seam rather than a single implementation.
- Permission requests are routed out of the process to the pod's own HTTP endpoint, which is what makes an interactive Claude pod answerable rather than silently blocked.

## Problem

`agent-claude` runs one Claude invocation per phase and exits. Its runner builds a command, spawns `claude --print`, reads the result and returns; the CLI's `SessionID` is captured but never used to resume anything. That shape cannot serve an interactive session: a second request has no process to talk to, so continuity would have to be re-established from a transcript file on every turn, and a tool-permission prompt would block a process nobody is listening to. The parent goal's interactive Claude worker needs a process that stays up, accepts turns, and can ask a question mid-turn.

## Goal

A Claude session implementation in `github.com/bborbe/agent` that satisfies the shared session interface and holds one long-lived process per session, speaking the CLI's streaming-input protocol. Two prompts on one session id share one process and one conversation, and a permission request raised mid-turn reaches the pod's HTTP surface instead of deadlocking.

## Non-goals

- The shared session interface, the HTTP service and the pi implementation — sibling spec `shared-interactive-agent-service.md`, which this spec depends on.
- Adding service mode to `agent-claude` itself, or pinning its Dockerfile. Both are hand-offs delivered through that repo's own pipeline.
- The `claude-interactive` Config CR and its manifests — the parent goal's SC1.
- Changing `claude.ClaudeRunner`'s signature. It is consumed elsewhere and keeps its current shape; the session implementation is a sibling of it, not a replacement.
- Resume-from-transcript. Continuity comes from the held process, not from rehydrating a JSONL file between turns.

## Assumptions

- **`ClaudeSDKClient` is a Python class and this repo is Go.** The SDK is not importable here. What the SDK wraps is the CLI protocol `claude -p --input-format stream-json --output-format stream-json --verbose` — a long-lived process reading JSON turns on stdin and writing JSON events on stdout. This spec targets that protocol directly. The SDK remains the reference for its message shapes. `--verbose` is included because the existing runner passes it alongside `--output-format stream-json`; the exact flag set must be confirmed against the pinned CLI version and recorded with AC2's result.
- **The CLI is already installed in the image.** `agent-claude/Dockerfile` installs `@anthropic-ai/claude-code` today; this spec does not change how it gets there, only that a version must be pinned before the behaviour is relied on (hand-off).
- **A held process survives between requests.** The pod does not restart between turns, and the process is not reaped by an idle timeout. If the CLI has an idle timeout, the implementation must either disable it or surface the loss as a per-turn error rather than silently re-establishing a fresh conversation.
- **The permission-gate wiring is reachable.** The pod's own HTTP surface (sibling spec) is what a mid-turn permission request is routed to. If that surface is unavailable, the request must fail the turn rather than block indefinitely.

## Acceptance Criteria

- [ ] **AC1 — one process serves the whole session, and both turns go to it.** Across two `Send` calls on one session, the implementation starts exactly one subprocess **and both turns' payloads are written to that same process's stdin**; the spawn command line contains `--input-format stream-json` and `--output-format stream-json`.
  - Evidence: a test whose fake process records every byte written to its stdin, asserting one process start across two sends **and** that the second turn's payload appears in that one process's recorded stdin stream; plus the process identity (the same `*exec.Cmd`) unchanged across the two sends **and the process still running when turn 2 is written** — `*exec.Cmd` identity survives process death, so identity alone does not prove liveness; plus a test asserting both protocol flags appear in the constructed command. A count-only assertion is insufficient — a stub that starts one process and never writes a turn to it would pass it. A per-turn `claude --print --resume <id>` wrapper fails all three halves. `git grep -n 'ClaudeSDKClient'` is explicitly **not** acceptable evidence: an invented type name satisfies it without any long-lived process existing.
- [ ] **AC2 — the Claude path is exercised against the real CLI, not merely compiled.** Two sequential prompts on one session id, against a session implementation wired to the real `claude` binary, return a second reply carrying a value only the first turn could have supplied. Turn 1 asks the session to remember a word **the operator picks at run time and does not write into this spec**; turn 2 asks for it back. While turn 2 is in flight, exactly one `claude` process is running.
  - Evidence: the operator's chosen word, both responses, and the `ps` observation taken during turn 2; plus `claude --version` (or `npm ls -g @anthropic-ai/claude-code`) against the same binary, recorded in the PR description. Operator-executable — the CLI is not present in the YOLO container. A reply hardcoded to a spec-known word is not evidence, which is why the word is not written here.
- [ ] **AC3 — the implementation satisfies the shared session interface.** It is substitutable for the pi implementation in the shared service: a test constructs the service with the Claude session implementation and drives the AC2 contract table through the HTTP path, with the CLI boundary faked.
  - Evidence: the shared service's contract test passes with the Claude session implementation wired in place of the pi one.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — format, generate, test, lint, license all clean
- `make test` — Ginkgo suite passes, including the subprocess-count and protocol-flag tests
- `git grep -n -- '--input-format' -- 'claude/*.go'` — ≥1 match (a smoke check only, **not** evidence: a comment containing the literal satisfies it; AC1's test is the evidence)
- `go vet ./...` — no findings

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `cd ~/Documents/workspaces/agent && make test` — library suite green on the host
- The AC2 two-turn probe, using the operator-chosen word, against a session implementation wired to the host's real `claude` binary, with the CLI version recorded
- `ps` during a held session shows one `claude` process, not one per turn

## Desired Behavior

1. **One process per session, held for the session's life.** `OpenSession(id)` starts a single `claude` process in streaming-input mode and keeps it. Each `Send` writes one JSON turn to its stdin and reads JSON events from its stdout until that turn completes. `Close` terminates the process. (The interface's method names are the sibling spec's to define; this spec uses them descriptively.)

2. **Continuity comes from the process, not from a file.** The second turn's context is whatever the held process still holds. The implementation must not re-spawn between turns, and must not silently fall back to a fresh conversation when the held process has died — a dead process is a per-turn error the caller sees.

3. **Permission requests leave the process.** A tool-permission request raised mid-turn is routed to the pod's HTTP surface and the turn waits on that answer. The pause is the held process waiting, not a busy loop.

4. **Spawn configuration reuses what the runner already has.** The session implementation takes the same configuration `ClaudeRunnerConfig` already carries — config dir, allowed tools, model, working directory, environment — rather than introducing a parallel config type.

## Constraints

**Dependency direction.** This spec depends on `shared-interactive-agent-service.md` for the session interface. It cannot be implemented before that spec's prompt 1 has landed.

**`ClaudeRunnerConfig` gains no flag pair.** pi's `PersistSession`/`SessionID` exists because pi is a subprocess that must be told to persist. The Claude session holds its own process, so there is no subprocess flag pair to mirror and no consumer for one. Add a field only if the implementation demonstrably needs it, and name that consumer in the prompt.

**Process hygiene.** The process is terminated on `Close` and on context cancellation; a leaked process per session is a resource leak the pod cannot recover from without a restart. The implementation must not read from the process's stdout concurrently with a write to its stdin outside a single turn.

**Error handling.** Errors are wrapped with `github.com/bborbe/errors` — `errors.Wrap(ctx, err, "…")`, never `fmt.Errorf` and never a bare `return err`. A malformed event on stdout fails the turn rather than being skipped silently.

**Code conventions.** Per `docs/dod.md`: exported types carry doc comments; Interface → Constructor → Struct → Method with the implementation struct private and named as the interface with a lowercased first letter; no package-level mutable state and no `init()`; no `context.Background()` in business logic; Ginkgo v2 / Gomega tests with counterfeiter mocks for the CLI boundary.

**Repository hygiene.** `README.md` is updated; `CHANGELOG.md` gets an entry under `## Unreleased`. The stream-json turn-loop protocol this implementation depends on — its event vocabulary, its turn-boundary rule, and the shape of a permission-request event — is written to a new `docs/claude-streaming-protocol.md` and referenced from this spec, so the knowledge does not die with it.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| The held process dies between turns | The next `Send` fails with a wrapped error naming the exit; the turn returns an error rather than starting a fresh conversation | The caller retries with a new session, or the pod is restarted |
| The process emits an unparseable event | The turn fails with the offending line's shape recorded (not its full content); the process is closed | Inspect the pod log; the CLI version may have changed its event vocabulary |
| A permission request is raised and the HTTP surface is unavailable | The turn fails rather than blocking indefinitely | Restore the surface; the session is usable again on the next turn |
| The CLI has an idle timeout and closes the process | Surfaced as a per-turn failure, not a silent new conversation | Disable the timeout in the spawn flags, or treat the session as ended |
| `Close` is not called and the pod exits | The process is reaped with the pod | None — bounded by pod lifetime |
| The CLI version changes its stream-json vocabulary | The event parser fails loudly on the first unrecognised shape | Pin the version (hand-off) so the vocabulary cannot change under the implementation |

## Security / Abuse Cases

- **Prompt content in logs.** Turn content is not logged; the digest discipline of the sibling spec's HTTP layer carries through, and this layer adds no content logging of its own.
- **Process arguments.** The session id is not interpolated into a shell string — the process is spawned with an argument vector, not a shell command, so an id cannot inject flags or shell syntax. The sibling spec's regex already constrains the id's alphabet.
- **Permission requests.** The permission path is the one place the implementation accepts an instruction from outside the process. It carries only a tool name, a description and an input preview to the HTTP surface, and accepts only an allow/deny verdict back. The preview is not logged.
- **Resource bounds.** One process per session, and the sibling spec accepts an unevicted session map — so process count is bounded by distinct session ids, which is operator-controlled and namespace-scoped.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Claude session implementation: held process, stream-json turn loop, permission routing, `Close`/cancellation, tests | 1, 2, 3, 4 | 1, 2, 3 | `shared-interactive-agent-service.md` prompt 1 |

One prompt. The implementation is a single package with one responsibility and one seam (the process boundary); splitting it would separate the turn loop from the process it reads, which is the thing being tested.

### Hand-offs

None of this spec's own. The `agent-claude` port — including pinning `@anthropic-ai/claude-code` to the version AC2 records — is a single hand-off owned by the sibling spec's table (`shared-interactive-agent-service.md` § Hand-offs). It is not restated here, so it cannot be generated twice.

## Do-Nothing Option

Interactive Claude runs as a process-per-request wrapper: `claude --print --resume <id>` on every turn, rehydrating continuity from the transcript file. It works for a plain prompt-and-answer exchange and costs a full CLI start per turn. It cannot carry a permission gate — there is no process alive to wait on one — so the interactive worker would be able to answer questions but never ask them, which is the capability the parent goal exists to deliver. Retrofitting streaming mode afterwards means rewriting the same package once the HTTP surface and the config already depend on it.

## Scenarios

No new scenario. The behaviour this spec adds is a process boundary and a protocol loop, both of which a test double can hold precisely — that is what AC1 and AC3 assert. The end-to-end proof needs the real CLI, which the YOLO container does not have; the AC2 host-side probe is the strongest reachable evidence, and a scenario harness inside the container would be strictly weaker than it.
