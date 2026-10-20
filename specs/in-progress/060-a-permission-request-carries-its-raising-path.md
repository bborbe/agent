---
status: verifying
approved: "2026-10-14T14:37:14Z"
generating: "2026-10-14T15:14:59Z"
prompted: "2026-10-14T15:47:54Z"
verifying: "2026-10-20T21:00:29Z"
branch: dark-factory/a-permission-request-carries-its-raising-path
---

## Summary

- The interactive service serves `/prompt` and `/a2a` from **one** session cache and **one** permission decider, and `PermissionRequest` carries no field saying which of the two raised the request.
- A decider therefore cannot branch: it must treat an A2A-raised permission request exactly as it treats an interactive one.
- That blocks a consumer that must answer the two differently — an A2A worker's gate belongs on the attention board, while a `/prompt` gate belongs on the pod's own `/permission` route.
- This spec adds the missing discriminator to the permission seam, so a decider can tell which path it is serving.
- It changes a frozen contract (`docs/interactive-service.md`) and therefore updates that document in the same change.

## Problem

`bborbe/agent-claude`'s A2A worker raises a permission prompt for a tool outside its allowlist, and the request surfaces on the pod's own `GET /permission` route where the turn blocks until a verdict is posted — measured live on 2026-10-14 against a k8s worker, which blocked 3 m 21 s and resumed on a `/permission` allow. Nothing about that gate reaches the attention board, so the operator the fleet is built around never sees it.

The consumer that must fix this is `agent-claude`, and it must fix it **for the A2A path only**: the interactive `/prompt` path's gate is answered in the pane and must keep behaving as it does today. But `agent-claude` constructs one `SessionFactory` and one `PermissionDecider` and hands both to the single service that serves `/prompt`, `/permission` and `/a2a` alike. `PermissionRequest` carries `ToolName`, `Description` and `InputPreview` — and nothing else. A decider that cannot tell which path raised the request cannot answer the two differently, and there is no seam outside this library through which to inject that knowledge: `agent-claude` does not own the HTTP handler, because `Handler()` builds it and `Run` serves it.

## Goal

A `PermissionDecider` implementation can determine, from the value it is handed, whether the permission request it is deciding was raised by an A2A turn or by a `/prompt` turn — and can therefore answer the two differently. The interactive service's documented contract reflects the new field, and no existing consumer's behaviour changes.

## Non-goals

- **Publishing anything to the attention store.** That is `agent-claude`'s half; this spec only makes the distinction expressible.
- **Changing the permission-decider interface's method set or signature.** The seam gains information, not a second method.
- **Changing what a `/prompt` gate does today.** It keeps blocking on `/permission` and keeps resuming on a posted verdict.
- **Any change to the A2A protocol, the attention store, or the pod's HTTP routes.**
- **Answering the request.** This spec carries the question, not the verdict.

## Assumptions

Three, each read at source in this worktree rather than assumed:

1. **The `ctx` handed to `Session.Prompt` reaches `DecidePermission`.** It does today: `Prompt` → `turn` → `readTurn` → `handle` → `answerPermission` (`claude/claude-session.go:120-289`) passes one context down. If a later change breaks that chain, the discriminator has no carriage that does not alter `Session.Prompt`'s signature.
2. **`/prompt` and `/a2a` are the only routes that serve a turn.** True today (`interactive/service.go:169-178`). A third turn-serving route is Failure Modes row 1.
3. **`bborbe/agent-claude` is the only consumer that needs the distinction today.** `agent-pi` consumes the same `Session.Prompt`, but nothing in this repo requires it to branch.

## Acceptance Criteria

- [ ] **AC1 — a decider can distinguish the two paths.** A test constructs the interactive service over a session factory whose decider records the `PermissionRequest` it is handed, drives one turn through `POST /prompt` and one through the A2A route **with the same session id and the same tool name**, and asserts the two recorded requests differ in `Source`. Evidence: the test's name and its assertion output; `go test ./interactive/... -run <TestName> -v` shows the two recorded `Source` values, and they are not equal.
- [ ] **AC2 — `Source` is populated on both paths, never empty.** The same test asserts each recorded `Source` is non-empty and each matches the path that raised it. Evidence: the two printed `Source` values, one per path.
- [ ] **AC3 — the `/prompt` path's observable behaviour is unchanged.** Evidence: `docs/interactive-service.md`'s existing `/prompt` contract rows still hold — a turn raising a permission request still blocks until `POST /permission` carries a verdict, and still resumes on it. Evidence shape: the existing contract test for the permission endpoint still passes unchanged (`go test ./interactive/...` exits 0), and `git diff --stat docs/interactive-service.md` shows additions only — no existing contract row is removed or reworded.
- [ ] **AC4 — a nil decider still fails a turn loudly.** A service built without a decider still spawns the session process without `--permission-prompt-tool stdio`, so a tool outside the allowlist fails that turn rather than blocking on a decider nobody answers. Evidence: the existing test for the nil-decider case still passes; `grep -n 'permission-prompt-tool' claude/claude-session.go` still shows the flag appended only when the decider is non-nil.
- [ ] **AC5 — the frozen contract document is updated.** `docs/interactive-service.md` documents `PermissionRequest.Source` inside its `## Permission endpoint` section, naming the closed set of values it can take. Evidence: `grep -n 'Source' docs/interactive-service.md` returns ≥1 line, and the added lines sit between the `## Permission endpoint` heading and the next heading (`git diff docs/interactive-service.md`).
- [ ] **AC6 — no consumer in this repo regresses.** `make precommit` exits 0.

## Verification

```bash
make precommit
go test ./interactive/... ./claude/... -v -run Permission
git diff --stat docs/interactive-service.md
```

## Desired Behavior

1. A permission request raised mid-turn carries a `Source` value identifying which of the service's two turn-serving routes raised it.
2. That value is present and correct on both routes; it is never empty.
3. A `PermissionDecider` written outside this library can read it and return a different verdict for the two routes.
4. A decider that ignores the new value behaves exactly as it does today on both routes.
5. The `/prompt` route's gate still appears on `GET /permission` and still resumes the turn when a verdict is posted there.
6. A service constructed without a decider still fails an out-of-allowlist turn rather than blocking on it.
7. `docs/interactive-service.md` describes the new field as part of the frozen contract, so the next consumer reads it rather than rediscovering it.

## Constraints

- **`docs/interactive-service.md` is a frozen contract.** Its own text: *"This document is the frozen contract. Both consumers honour the interface described here, and it does not change without a spec."* This spec is that spec; the document changes in the same prompt.
- **`Session.Prompt(ctx, prompt)` keeps its signature.** `agent-claude` and `agent-pi` both consume it; a signature change would break both for a value only the decider needs.
- **`PermissionDecider.DecidePermission(ctx, request) (PermissionDecision, error)` keeps its signature.** Its shape is consumed by a counterfeiter mock (`//counterfeiter:generate -o ../mocks/claude-permission-decider.go`), and `agent-claude` implements it.
- **The existing `/permission` route keeps its contract** — `GET` lists pending ids, `POST` delivers `{id, allow, message}`; an unknown id is still refused and creates nothing.
- **No new package dependency.** The value must not require importing anything from the attention stack into this library, which knows nothing about it.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| A future third route serves turns | Its requests carry a discriminator value distinct from the existing two, or the field is documented as the two-value enum it is today and the new route updates it | Add the value to the enum and to `docs/interactive-service.md`; the type's doc comment names the closed set |
| A decider written before this change | Compiles and behaves exactly as today — the field is additive | None; no migration |
| `Source` is carried per session rather than per turn | A later turn on the same cached session reads the previous turn's route — the session cache is shared by both routes | Carry it per turn and never on session state; AC1 interleaves the two routes on one session id, so a sticky value fails it |
| `Source` is not populated on some path | The decider sees an empty `Source` and cannot branch, silently falling back to today's behaviour | AC2 asserts non-empty on both paths, so an unpopulated path fails rather than degrading |
| A turn is cancelled while its request is pending | Unchanged from today: the request leaves the pending set and no verdict is delivered | None |

## Security / Abuse

The new field carries a route name, not conversation content, a credential or a tool input — it is strictly less sensitive than `InputPreview`, which the seam already carries and does not log. It must not be populated from anything caller-supplied: `X-Session-Id` is caller-controlled and must not decide it, or a `/prompt` caller could make its gate look like an A2A gate and route a pane-answered request to a channel the operator answers. The value is set by the route that serves the turn, never read from the request.

## Suggested Decomposition

One prompt.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Add the discriminator to the permission seam, populate it on both routes, update the frozen contract doc | 1-7 | 1-6 | — |

Rationale: the change is one seam and one document; splitting it would separate the field from the contract that describes it.

## Do-Nothing Option

`agent-claude` cannot route an A2A worker's permission gate to the attention board without also rerouting the interactive path's, because it cannot tell them apart. The parent row's Success Criteria — a real gate from a worker reaching the board, and the interactive path unchanged — are jointly unsatisfiable in that repo alone. The cost of not doing this is that the goal's binding criterion stays unmet, and a workaround (a second decider, a second service, a second port) would duplicate the whole interactive surface to avoid one field.
