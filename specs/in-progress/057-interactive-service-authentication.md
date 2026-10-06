---
status: verifying
approved: "2026-10-02T23:04:44Z"
generating: "2026-10-02T23:14:15Z"
prompted: "2026-10-02T23:14:15Z"
verifying: "2026-10-06T06:07:33Z"
branch: dark-factory/interactive-service-authentication
---

## Summary

- The `interactive` package serves an HTTP surface with **no authentication of any kind** — no header handling, no middleware, no token.
- A Kubernetes NodePort binds on the node's LAN IP. Exposing this service as it stands would let anything on the network run prompts and obtain **shell execution inside the pod under the cluster's credentials**.
- This spec adds a **bearer-token gate** to the shared service, and makes the auth decision **impossible to omit** at construction.
- The token follows the shape the operator already settled for pod-to-service auth: a **shared bearer token delivered as a runtime-only pod secret**.
- The service's frozen contract document is updated in the same change, so it stops describing a service that no longer exists.

## Problem

`github.com/bborbe/agent/interactive` is the shared HTTP surface both interactive agent images run. It serves four routes — `/readiness`, `/metrics`, `/prompt` and `/permission` — and none of them authenticates the caller. The pod carrying it runs with `ALLOWED_TOOLS` including `Bash` and holds the cluster's `ANTHROPIC_AUTH_TOKEN`, so an unauthenticated caller does not merely read state: `POST /prompt` runs a turn, and `POST /permission` **delivers a verdict that approves a tool execution**. Today that is survivable only because neither pod has a Service object and the posture is namespace-scoped reachability (see [agent-network-security.md](../docs/agent-network-security.md)). The moment anything binds a LAN-reachable address in front of it, the posture is gone. The operator's ruling on 2026-10-02 was **auth first, then expose** — this spec is the auth half.

## Goal

Every route of the interactive service is either **authenticated** or **explicitly exempted with a recorded reason**, and the decision cannot be made by omission: a consumer of the shared package must state its auth choice to construct the service at all. The bearer token exists only as a runtime-injected value — never in source, an image layer, or a committed manifest — and the frozen contract document describes the authenticated surface rather than the previous open one.

## Non-goals

- **Exposing the service.** No `Service`, no `NodePort`, no ingress. This spec makes the endpoint safe to expose; it does not expose it.
- **Changing what the pod may do once authenticated.** The `ALLOWED_TOOLS` set is untouched; this adds a gate in front of the existing surface.
- **The attention store's bearer token.** Owned by its own task; this spec reuses that mechanism's shape for the interactive service's inbound endpoint.
- **A different auth mechanism.** Mutual TLS and an SSH tunnel were offered and not chosen.
- **The supervisor-side cluster target.** Already shipped and out of this repo.
- **Wiring the token into any deployment, or bumping a consumer's dependency.** The Kubernetes manifests, the pod secret, and the consumer image's dependency bump land in their own repos through their own flows; this spec covers the library and its contract.

## Assumptions

- The bearer-token-plus-runtime-pod-secret shape the operator settled for the attention store is reusable here; this spec applies it rather than inventing a second mechanism.
- Both consumer binaries can absorb a constructor signature change on their own release cadence — the library change does not need to land atomically with them.
- The pod secret is delivered at runtime by the deployment (its own repo's concern), so the library reads a token that is present in the environment at startup and absent from every committed file.
- The two routes that serve infrastructure rather than callers — `/readiness` and `/metrics` — are exempted, and that exemption is accepted rather than accidental. See Acceptance Criterion 4.

## Acceptance Criteria

- [ ] A request to `/prompt` carrying **no** `Authorization` header is refused — evidence: unit test row `rejects a request with no Authorization header` passes; the asserted status is `401`.
- [ ] A request carrying a **wrong** token is refused — evidence: unit test row `rejects a request with a wrong token` passes; asserted status `401`.
- [ ] A request carrying the **correct** token is served — evidence: unit test row `serves a request with the correct token` passes; asserted status `200`. **This is the control for the two above**: without it, a build that refuses everything passes both.
- [ ] Each of the four routes behaves as recorded — evidence: a table-driven test asserts `401` for `/prompt` and `/permission` when unauthenticated, and asserts `/readiness` and `/metrics` are served (`200`) unauthenticated. The two exempt routes are exempt **deliberately**: a kubelet probe and a Prometheus scrape cannot present a bearer token without the token being written into the pod spec's probe stanza and the scrape configuration, which multiplies the secret's exposure and can wedge the pod's Ready state; neither route grants execution or reveals a credential.
- [ ] A refused request performs no session work — evidence: negative evidence, `grep -c 'turn start'` over the captured log of a refused request returns `0`, and the same grep over an authenticated request returns `1`. This pins that refusal precedes the body read, session construction and lock acquisition.
- [ ] The auth decision cannot be omitted **or left at its zero value** — evidence: `grep -n 'func NewService' interactive/service.go` shows the constructor requires an auth argument, and unit test row `rejects a zero-value auth decision` passes.
- [ ] The disabled option is explicit, greppable, **and behaves as declared** — evidence: `grep -rn 'AuthDisabled' interactive/` returns ≥1 declaration, and unit test row `serves unauthenticated when auth is explicitly disabled` passes. ⚠️ This opt-out is the operator's deliberate design — fail-closed by explicitness, chosen over both a forced token and silent opt-in middleware — so it is **tested rather than assumed**. The residual risk of passing it on a pod that is later exposed is recorded in Failure Modes.
- [ ] Secret hygiene holds — evidence: three counts, all `0` — `grep -rcE 'Bearer [A-Za-z0-9_-]{8,}' interactive/` returns `0` for every file; `grep -c` for the token value over captured test and run output returns `0`; and `grep -n` shows the token is read from the process environment rather than a literal.
- [ ] The frozen contract describes the authenticated surface — evidence: `grep -n 'Authorization' docs/interactive-service.md` returns ≥1 line; the routes table carries four rows each naming its policy; and `grep -c 'adds no authentication' docs/interactive-service.md` returns `0`.
- [ ] **Post-Deploy (Rung-2):** the triple probe against the deployed pod behaves as specified — evidence: an unauthenticated `POST /prompt` returns `401`, the same call with a wrong token returns `401`, and with the correct token returns `200`; each status quoted verbatim, and an unauthenticated request to each of the four routes matches the policy recorded in the preceding criterion.
  - `deploy_check:` `kubectlnukedev -n dev get pods -l app=claude-interactive -o jsonpath='{.items[0].spec.containers[0].image}'`
  - `deploy_target:` `docker.prod.nuke.benjamin-borbe.de:443/bborbe/agent-claude:<TAG>` — **the literal is pinned when the consumer release is cut.** This spec is authored before that tag exists, so the value cannot be written here; it is recorded in the task file at deploy time. ⚠️ Approval is gated on that tag existing: do not approve this spec with the placeholder unexamined.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — format, generate, test, lint, license; exits `0`
- `make test` — Ginkgo suite passes, including the new middleware rows
- `grep -n 'func NewService' interactive/service.go` — shows the required auth argument
- `grep -rn 'AuthDisabled' interactive/` — returns the declaration
- `grep -rcE 'Bearer [A-Za-z0-9_-]{8,}' interactive/` — returns `0` for every file
- `grep -c 'adds no authentication' docs/interactive-service.md` — returns `0`

### Operator-executable (runs on the host after the deploy chain lands)

- `kubectlnukedev -n dev get pods -l app=claude-interactive -o jsonpath='{.items[0].spec.containers[0].image}'` — returns the new `bborbe/agent-claude` tag, not `v0.5.0`
- `curl` probes against the deployed pod for the no-token / wrong-token / correct-token triple, each status quoted
- `grep -c` count-only search for the token value across the built image and the committed manifests — returns `0`

## Desired Behavior

1. **The gate is one middleware over the whole handler.** Every route the handler registers passes through it, including a route registered conditionally. A route added after this change is gated by construction rather than by remembering.
2. **An unauthenticated request is refused before any work happens.** The refusal precedes body reads, session construction and lock acquisition, so a refused request cannot occupy a session or appear in the turn-boundary log pair.
3. **The correct token is accepted and the request proceeds unchanged.** Everything the frozen contract already specifies about `/prompt` — session id validation, the 1 MiB cap, turn-boundary logging — behaves identically for an authenticated request.
4. **Construction requires an explicit auth decision.** The service cannot be built by passing nothing. A consumer that serves no permission endpoint and is deliberately unexposed says so in one visible line rather than by inheriting a default.
5. **The token is read from the process environment at startup and never logged** — not at any verbosity, not in an error path, not in test failure output. The frozen contract records the authenticated surface, each route's policy, and the reason the two exempt routes are exempt.

## Constraints

- **The frozen contract's existing behaviour must not change for an authenticated caller.** Session-id validation, the absent-vs-empty distinction, the body cap and its truncate-not-reject semantics, the readiness body strings, the two-level locking, and the turn-boundary log pair all keep their current contract. [interactive-service.md](../docs/interactive-service.md) opens by stating it does not change without a spec; this is that spec, and it changes the authentication posture **only**.
- **The permission endpoint stays conditionally registered.** It is served only when a permissions registry is supplied; this change does not make it unconditional.
- **The library must keep building for both consumers.** One consumer does not wire a permissions registry; the change must be expressible at its call site without it adopting the permission endpoint.
- **No new dependency** for token comparison; the standard library is sufficient.
- **The comparison must not be timing-variable** — a byte-wise early-exit comparison leaks token length and prefix through response timing.
- Tests follow the repo's conventions: Ginkgo/Gomega, external test packages.

## Failure Modes

| Trigger | Expected behaviour | Recovery | Detection |
|---|---|---|---|
| `Authorization` header absent | `401`, before any session work | Caller adds the header; no state to clean up | The caller's own error |
| `Authorization` header malformed (wrong scheme, no value) | `401`, same path as absent | Caller corrects the header format | The caller's own error |
| Token correct in value, wrong in length | `401`, constant-time comparison, no partial-match signal | Caller uses the full token | The caller's own error |
| Token env var unset at startup | The service **fails to start** rather than serving unauthenticated | Operator restores the secret; the pod does not reach Ready | Pod not Ready; `kubectl describe pod` shows the exit |
| Token rotated while the pod runs | The pod keeps serving the old token until restarted; requests with the new token are `401` | Operator restarts the pod to pick up the new secret | Callers report `401` while the secret in the cluster is already the new value — the two disagreeing is the signal |
| A consumer passes the disabled opt-out on a pod that is later exposed | The pod serves unauthenticated — **this is the residual risk this design accepts**, made visible by one greppable line | Reviewer greps for the opt-out before adding a Service | The grep in criterion 7 is the audit |
| Readiness probe gated without probe configuration | The pod never becomes Ready and is restarted in a loop | The recorded policy exempts `/readiness`, so this path is closed by decision rather than by luck | Pod not Ready; restart count climbing |

## Security / Abuse

This change exists because of an abuse path, so the threat is stated explicitly.

- **The abuse path being closed:** anything able to reach the port can `POST /prompt` and obtain shell execution inside the pod under the cluster's credentials, and can `POST /permission` to approve tool executions. Both are reachable today from anywhere in the namespace and would be reachable from the LAN the moment a NodePort is bound.
- **What this change does not close:** the token is a **shared secret**, so any holder has the full authority of the endpoint. It authenticates the caller as "something that knows the token", not as a named principal. Per-caller identity, rotation without restart, and revocation are out of scope and are not claimed.
- **The two exempt routes are a deliberate, recorded exception, not an oversight.** `/readiness` and `/metrics` stay open. `/readiness` answers only whether the provider is dialable and leaks no credential; `/metrics` is a Prometheus scrape endpoint. Gating either would require writing the token into the pod spec's probe stanza and the scrape configuration — spreading the secret beyond the runtime injection this design depends on — and a misconfigured readiness probe wedges the pod. The consequence, stated plainly: **these two routes remain readable by anything that can reach the port.** Neither grants execution.
- **Transport:** the gate is worthless over plaintext to an untrusted network. The recorded posture pairs the token with cluster-network binding; exposing beyond the cluster without TLS is not covered by this spec.
- **Secret handling:** the token must never be printed. Not in logs at any verbosity, not in error messages, not in test failure output, and never via `kubectl get -o yaml`/`-o json`, whose `last-applied-configuration` annotation embeds the whole object as plaintext. Evidence for anything token-shaped is a **key name, a status, or a count** — never a value.
- **Timing:** comparison is constant-time so that a response-time oracle cannot recover the token prefix.
- **Failure is closed:** a missing or unreadable token stops the process. A service that cannot authenticate does not serve.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Auth seam in `interactive`: the middleware, the constructor requirement, the explicit disabled value, and the unit/integration tests | 1-5 | 1-8 | — |
| 2 | Frozen-contract update: the four-route table with per-route policy and the reason the two exempt routes are exempt | 5 | 9 | prompt 1 (the doc describes behaviour that must exist first) |

Rationale: prompt 1 establishes the behaviour; prompt 2 is doc-only and would be dishonest before the behaviour exists. Acceptance Criterion 10 is not prompt-scoped — it is verified on the host after the deploy chain (the consumer image bump and the manifest change) lands, which happens in other repositories.

## Do-Nothing Option

The service stays unauthenticated. That is survivable exactly as long as nothing binds a LAN-reachable address in front of it — and the reason this spec exists is that the very next piece of planned work does precisely that. Deferring means either blocking that work indefinitely or shipping an unauthenticated shell-execution endpoint onto the network, where the exposure would be discovered by whoever finds it first. The cost of this spec is one middleware, one constructor change, and a doc update.
