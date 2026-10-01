---
status: draft
---

## Summary

- The shared interactive agent service gains a **permission endpoint**: the pod's HTTP surface holds a mid-turn tool-permission request open until a caller outside the pod posts a verdict for it.
- The library already defines the seam a permission request is routed out through, and already routes one there. It ships **no implementation** of that seam, and the one binary that would supply one passes nothing deliberately — so a session that raises a permission **fails its turn** instead of pausing.
- The service today serves readiness, metrics and prompt intake. There is no surface for a request to appear on and none for a verdict to arrive on, so even a caller willing to answer has nowhere to answer.
- This closes the gap between "a pod holds a conversation" and "a pod can ask" — the capability the parent goal exists to deliver.

## Problem

An interactive Claude worker that cannot ask a question is not interactive. The streaming session was chosen precisely because a one-shot `--print` invocation has no process alive to wait on a permission gate, and the library honours that: the held process pauses, the request is parsed out of the event stream, and the permission-decider seam is called. But no shipped wiring supplies a decider — `agent-claude/pkg/factory/factory.go:57-63` passes `nil` deliberately, with a comment saying so — so the pause never happens and the turn returns an error the moment a tool outside the allowlist is reached.

The missing piece is not the pause mechanism; it is the **answer path**. A pod in a cluster has no operator at its terminal. Something outside the process must be able to see that a decision is wanted and to supply one. Today nothing can, so the seam is wired to nothing.

## Goal

The interactive service serves a permission endpoint over which a mid-turn permission request becomes visible to a caller outside the pod, and over which that caller posts the verdict the paused turn is waiting for. A session built with the endpoint's decider pauses on a permission request and resumes on the verdict, instead of failing the turn.

## Non-goals

- **The attention store's auth and cluster-network binding.** Owned by [[Add a Shared Bearer Token and Cluster-Network Binding to the Attention Store for Pod Workers]]. This spec adds a local surface and no client for it.
- **Carrying a request to the operator and an answer back.** Owned by [[Prove the Cluster Question Path Against a Real Pod]]. This spec makes the pod answerable; who answers it, and through what, is not settled here.
- **Changing the existing permission-decider interface.** Its signature is consumed by the session and by a counterfeiter mock; this spec implements it, it does not reshape it.
- **The `claude-interactive` Config CR, its manifests and its deployment.** The parent goal's SC1, a separate row.
- **Wiring `agent-claude` to the endpoint.** A consumer port, delivered as a hand-off through that repo's own pipeline (see § Suggested Decomposition).
- **Authenticating the endpoint.** The posture is unchanged and namespace-scoped — see `docs/agent-network-security.md`, which the existing routes already rely on.

## Acceptance Criteria

- [ ] **AC1 — the endpoint is served and its empty state is well defined.** `GET /permission` on a service built through **`NewServiceWithPermissions`** returns `200` with a JSON array that parses and has length 0. A service built through the plain `NewService` is a different case and answers `404` — see the Failure Modes table.
  - Evidence: the HTTP status and the exact response body, quoted, with the constructor used named.
- [ ] **AC2 — a pending request is visible while its turn is blocked.** With a turn in flight that has raised a permission request and is waiting on it, `GET /permission` returns `200` and a JSON array of length exactly 1 whose single element carries the raised tool's name and a non-empty id.
  - Evidence: the response body quoted, plus the tool name the fixture raised, so the two can be compared.
- [ ] **AC3 — a posted verdict resolves the request it names, in both directions.** `POST /permission` naming a pending id returns `200`; an allow body makes the blocked turn return a successful result, and a deny body makes the decider's caller observe the deny outcome carrying the exact message string that was posted. A verdict naming an id that no request holds returns `404`, and a subsequent `GET /permission` returns length 0.
  - Evidence: for each direction, the POST status and body and the resulting turn result or observed decision, quoted, with the message compared character for character; for the unknown id, the `404` plus the follow-up `GET` body — that negative half is what proves the POST neither created nor resurrected an entry.
- [ ] **AC4 — cancelling the waiting turn releases the waiter and leaves nothing pending.** When the context of the turn that raised the request is cancelled, the decider returns a non-nil error, and a subsequent `GET /permission` returns length 0.
  - Evidence: the returned error, plus the follow-up `GET` body showing 0. A test asserting only the error does not satisfy this — the leak is the second half.
- [ ] **AC5 — the pending wait does not hold the session cache's map lock.** While one session id is blocked on a permission request, a `POST /prompt` addressing a **different** session id returns `200` with an answer.
  - Evidence: the second request's status and body, taken while the first is provably still pending — the fixture asserts the first is still blocked at that moment, not merely that it was started.
- [ ] **AC6 — two services in one process do not share pending state.** With two services constructed in the same test binary and one of them holding a pending request, the other's `GET /permission` returns `200` with length 0.
  - Evidence: both responses quoted side by side — the holder's showing length 1, the other's length 0 — taken in the same test.
- [ ] **AC7 — the endpoint and the sessions it serves resolve through one registry.** A service built through the permission-enabled constructor serves requests raised by the sessions it was built with: a prompt sent to that service's own session appears in that service's own `GET /permission`, with no wiring step between them.
  - Evidence: the prompt's session id, the `GET /permission` body showing that request, and the construction call, so a reader can see the same registry reached both.
- [ ] **AC8 — the documented contract is extended, and its existing rows are unregressed.** `docs/interactive-service.md` carries a Routes row for the endpoint and its contract rows, and the pre-existing rows of its contract table still hold. This criterion serves two masters: the **doc constraint** in § Constraints (the frozen contract is updated in the same change) and Desired Behavior 1, whose "the existing routes are unchanged, row for row" is exactly what the unregressed half measures.
  - Evidence: `grep -n '/permission' docs/interactive-service.md` returning line numbers ≥ 1, plus the existing contract test suite passing unchanged.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — lint, vet and shellcheck clean, exit 0
- `make test` — the full suite passes, exit 0
- `grep -rn '"/permission"' interactive/` — the route is registered somewhere in the package, returns ≥ 1 line
- `grep -n '/permission' docs/interactive-service.md` — the contract documents it, returns ≥ 1 line

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `git tag --contains <merge-sha>` — the release tag carries the merge, so the change is consumable
- in `bborbe/agent-pi` and `bborbe/agent-claude`: after the dependency is bumped, `go build ./...` exits 0 — the compatibility claim in § Constraints, checked rather than assumed

**Precondition for the second rung:** it needs both consumer repos checked out on the operator's host, and `agent-claude`'s build additionally needs its own hand-off port to have landed (see § Hand-offs). Until that port lands, only `agent-pi` can be built against the new version — and that is the consumer whose compatibility this rung actually tests, since `agent-pi` is the one the resolved constructor shape exists to protect.

## Desired Behavior

1. **The service serves a permission endpoint alongside its existing three routes.** The existing routes and their responses are unchanged, row for row.
2. **A raised request becomes visible before it is answered.** While a turn is paused on a permission request, that request is readable from outside the process, carrying at least the tool's name and an identifier the verdict can name.
3. **A posted verdict resolves the pending request it names.** An allow lets the tool run; a deny returns the denial, including any message the caller supplied, to the process that asked; an id that no request holds is refused, and no pending entry is created, resurrected or silently accepted.
4. **Cancellation releases the waiter.** A turn cancelled while paused stops waiting, surfaces an error to its caller, and leaves no pending entry behind.
5. **The pause is confined to the session that raised it.** A second session's turn proceeds while the first is paused, so the pause cannot become a service-wide stall.
6. **The endpoint and the session factory resolve through one registry, and no two services share state.** A service cannot be built whose endpoint resolves through a different registry than the one its sessions were built with, and a second service in the same process observes none of the first's pending requests.

## Constraints

- **The permission-enabled constructor is resolved, not deferred.** `NewService` keeps its four-parameter signature, so `bborbe/agent-pi` — a different repo, whose update would be a hand-off — needs no change. The endpoint is enabled by a second constructor, `NewServiceWithPermissions`, which takes the permission registry as a fifth parameter; `NewService` delegates to it with none, and the endpoint is simply not served. The caller constructs the registry first and passes that one instance to both the session factory and the service, which is what makes Desired Behavior 6 true by construction rather than by convention. This is a frozen public-API decision, not an implementation choice: the library has two consumers in two other repos.
- **`docs/interactive-service.md` is a frozen contract and is updated in the same change.** Its Routes table and its contract table are the authority for this package's behaviour; the new route is a new row in both. Its `## Locking` section states that **no request is ever rejected because of the lock**, and that two-level locking is deliberate — the map lock guards the map and is released before the entry's own lock is taken. This change must not widen that into one lock held across the wait.
- **The pending wait must not hold the session cache's map lock.** The cache's two-level discipline is what keeps a paused session from stalling every other session; a registry that borrows the map lock while blocking breaks Desired Behavior 5 and the documented invariant.
- **The wait must observe context cancellation.** The turn's context is the caller's request scope; a waiter that ignores it leaks one entry per cancelled turn, which the pod cannot recover from without a restart.
- **The registry and the decider are per-service state, not package state.** No package-level variable, no `init()`, no singleton — a test binary may hold more than one service, and a global would couple them. This is what Desired Behavior 6 and AC6 measure.
- **The existing permission-decider interface's signature is frozen.** `claude.PermissionDecider` and its counterfeiter mock are consumed by the session; implement against them.
- **Consumers keep building.** `agent-pi` compiles unchanged against the new version, because its constructor call is untouched; `agent-claude` compiles after its hand-off port. Both are checked in the operator-executable rung, not assumed.
- **Error handling** uses `github.com/bborbe/errors` — `errors.Wrap` / `errors.Wrapf` / `errors.Errorf`, never `fmt.Errorf`, never a bare `return err`.
- **Code conventions** per `docs/dod.md`: exported items carry doc comments; Interface → Constructor → Struct → Method with the implementation struct private and named after the interface with a lowercased first letter; no `context.Background()` in business logic; Ginkgo v2 / Gomega tests with counterfeiter mocks.
- **Repository hygiene.** `README.md` is updated where it describes the service's routes — its `interactive/` row at `README.md:43` names the three current routes and is the line that goes stale. `CHANGELOG.md` gains an entry under `## Unreleased`, **which does not exist yet**: verified 2026-10-01, the file's newest section is `## v0.92.0` at line 11, so the heading is created rather than appended to.

## Failure Modes

| Trigger | Expected behavior | Recovery | Concurrency |
|---|---|---|---|
| A request is raised and no verdict is ever posted | The turn stays paused for as long as its caller's context allows; the caller's own timeout ends it. The pause is the held process waiting, not a busy loop | The caller's deadline expires and the turn fails; the operator confirms with `GET /permission` that the entry is gone | The session lock is held for the turn's duration, so the same id serialises and a second id is unaffected — AC5 |
| The waiting turn's context is cancelled | The decider returns a wrapped error and the pending entry is removed | `GET /permission` returns length 0 — AC4's second half is the check | Cancellation races the verdict: whichever lands first removes the entry; the loser finds no pending id and returns `404` |
| A verdict arrives for an id that is no longer pending | `404`; no entry is created and no waiter is signalled | The caller re-reads `GET /permission`; a stale id means the request already ended | Two posts for one id race: the first to take the entry wins, the second sees no entry and returns `404` |
| The same id is posted twice, the first accepted | The second finds no pending entry and returns `404` | By construction — the first acceptance removes the entry | Resolved by the same single-removal rule as the row above |
| A service is constructed without a permission registry | The endpoint is not served and its route answers `404`; the three existing routes are unaffected | `GET /permission` returns `404`; expected for `agent-pi`, which raises no permission requests and is the named consumer of the plain constructor | No shared state exists to race — there is no registry |
| A turn panics while paused | The deferred unlock still releases the session lock, so the next request on that id does not deadlock | The pod log carries the panic; `GET /permission` shows the entry until its context is cancelled, which is the same path as the cancellation row | The panic does not hold the map lock, so other sessions are unaffected |

## Security / Abuse Cases

- **The endpoint is unauthenticated, like its siblings.** The posture is namespace-scoped reachability, unchanged and documented in `docs/agent-network-security.md`. This spec neither widens nor narrows it; adding auth is the bearer-token row's ground.
- **A posted verdict can authorise a tool call.** That is the endpoint's purpose, and it is why the endpoint must not be reachable more widely than the pod already is. The verdict carries an outcome and a message and nothing else — it cannot name a different request than the id it addresses, and an id that is not pending is refused.
- **The request carries a preview of the tool input.** The existing request type already bounds that preview and already documents it as not logged. The endpoint returns it to a caller inside the namespace; it adds no logging of its own.
- **The identifier is not caller-controlled state.** It is generated per request and is the only handle a verdict can use; an unknown one is refused rather than treated as a new request.
- **Unbounded pending entries are a resource risk.** Entries are removed on verdict and on cancellation, so the pending set is bounded by concurrent paused turns, which is bounded by distinct session ids.

## Suggested Decomposition

One prompt. The registry, the decider, the route and the constructor are one mechanism with one seam — the wait — and the route is the only way that wait is observed, so a prompt that owns the registry but not the route cannot satisfy any of its own acceptance criteria.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Permission registry and decider, the endpoint route, the permission-enabled constructor, the frozen-contract doc rows, and tests | 1, 2, 3, 4, 5, 6 | 1, 2, 3, 4, 5, 6, 7, 8 | — |

**Why one prompt, against a size budget that normally argues for splitting.** Two facts were measured rather than assumed, and both are recorded here because an earlier revision of this section got them wrong.

- **The product is inside the budget.** 6 desired behaviors × 8 acceptance criteria = **48**, under the 50 threshold that signals a split. (An earlier revision carried 8 behaviors × 8 criteria = 64, over the line; merging the three verdict-handling behaviors into one brought it back under. The merge was the fix, not the split.)
- **The "layers" are not independent components.** The size budget's layer clause names things like *publisher + classifier + CRD + sweeper + tests* — separately-deployable concerns each needing its own research pass. Here the whole change is **one Go package** (`interactive/`) plus that package's own frozen contract doc. The doc is not a second layer; it is the package's contract, and the route does not exist without its row.
- **A split was tried and it does not map.** An earlier revision split this into a mechanism prompt and a surface prompt. Every acceptance criterion except the doc-grep half is HTTP-level — each asserts a `GET` or `POST` against the endpoint — so the mechanism prompt was assigned four criteria it could not satisfy until the surface prompt landed. A decomposition whose rows cannot pass their own criteria is worse than no split.

The prompt-creator is free to split differently if it finds a seam this analysis missed; the constraint is that any row's `Covers ACs` must be satisfiable by that row alone.

### Hand-offs

The `agent-claude` port — constructing the registry and passing it to both the session factory and the permission-enabled service, and updating the stale `nil`-decider comment in `pkg/factory/factory.go` — is a hand-off owned by [[Build claude-interactive]], through `bborbe/agent-claude`'s own pipeline. It is named here so it is not silently absorbed, and not decomposed here so it cannot be generated twice. `bborbe/agent-pi` needs no hand-off: its constructor call is untouched by the resolved constructor shape above.

## Do-Nothing Option

Ship `claude-interactive` with the decider left `nil`. The pod holds a conversation and answers prompts, and every tool call outside its allowlist fails its turn with a permission error. The interactive worker would then be able to answer questions but never ask one — which is the capability the parent goal exists to deliver, and the reason the streaming session was chosen over a one-shot in the first place. Retrofitting the answer path afterwards means adding a surface to a service already deployed and already depended on by two binaries, against a frozen contract that will by then have consumers.

## Scenarios

No new scenario. The behaviour is an HTTP surface and a blocking wait, both of which a test double holds precisely — a fake decider asserts the pause, and an HTTP test drives the endpoint. The end-to-end proof (a real gate reaching a real operator) needs a deployed pod and the attention store, which is [[Prove the Cluster Question Path Against a Real Pod]]'s ground, not this spec's. A scenario harness here would be strictly weaker than the integration test it would duplicate.
