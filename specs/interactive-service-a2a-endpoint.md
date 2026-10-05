---
status: draft
---

## Summary

- The `interactive` service gains an **A2A surface**: an Agent Card at `/.well-known/agent-card.json` and a JSON-RPC `message/send` endpoint at `/a2a`, so a standards-compliant A2A client can drive the agent over the network.
- Both routes ride the **existing router**, so `requireAuth` gates `message/send` by construction — the card joins `authExempt` as one more literal path, alongside `/readiness` and `/metrics`, because discovery is public by design.
- An A2A message is bridged to the **existing session seam**: `message/send` runs one turn on the addressed conversation through `Session.Prompt`, reusing the same session cache, per-session lock and session-id validation `POST /prompt` uses.
- The card's advertised `url` is **configuration, not a constant** — the deployed address is never the container-local one, and an unset address fails closed rather than advertising `0.0.0.0`.
- **No second credential check.** The bearer token in `auth.go` remains the only gate; the A2A handler adds none of its own.

## Problem

An agent in this fleet is reachable only through bespoke HTTP routes — `POST /prompt` takes a body and an `X-Session-Id` header that no other system knows. Anything that is not a hand-written client therefore cannot call an agent at all. A2A is the emerging cross-vendor standard for exactly this call, and validating it needs a real A2A server — one that advertises itself and answers a standards-compliant request — rather than one more bespoke route. The `interactive` service already holds the three things such a surface needs: a router, a session seam, and a bearer-token gate that wraps the whole router. The gap is the A2A surface itself, not a new service.

## Goal

A running `interactive` service answers a standards-compliant A2A client end to end: it serves an Agent Card naming its **real network endpoint**, accepts an authenticated `message/send` and returns the agent's computed result for that input, and refuses an unauthenticated request with `401` — all through the router and session seam it already has, with no second credential and no change to any existing route's contract.

## Non-goals

- **Streaming and push notifications** — `message/stream`, `tasks/pushNotificationConfig/set` and task polling are out; one-shot `message/send` only.
- **The client side** — this spec adds a server. An A2A client, and the MCP wrapper around one, are separate work.
- **Network exposure and deployment** — NodePort, Ingress, TLS, the `agent-claude` dependency bump and the pod rollout all live in other repos. This spec makes the surface *servable and testable in-process*; it does not expose or deploy it.
- **Changing the existing routes** — `/readiness`, `/metrics`, `/prompt` and `/permission` keep their current contract exactly.
- **A second credential or scheme** — no API key, no mTLS, no per-route token.

## Assumptions

- The `github.com/a2aproject/a2a-go/v2` SDK supplies the Agent Card type and the JSON-RPC binding, so the handler does not hand-roll the wire format. If it does not, the handler must implement the binding itself — a larger change than this spec's decomposition assumes.
- A **deterministic input** exists whose `Session.Prompt` output is a stable computed artifact. Without one, AC3's equality check cannot be made to fail, and the criterion must be re-scoped before implementation.

## Acceptance Criteria

- [ ] **AC1 — the service serves an Agent Card without a credential.** Evidence: `curl -sS -w '\n%{http_code}' <base>/.well-known/agent-card.json` with **no** `Authorization` header returns `200`, and the body parses as JSON whose `name` names the agent. `<base>` is the base URL of an **auth-enabled** `httptest` server (`newAuthTestServer`; ephemeral port, not the deployed `:9090` default). That fixture is load-bearing: the package's default `newTestServer` builds with `AuthDisabled`, where *every* route answers `200` without a credential, so a `200` there would prove nothing about the card's exemption.
- [ ] **AC2 — the card's `url` is configuration, and an unset address fails closed.** Evidence: with `A2A_PUBLIC_URL` set to a distinct value, the served card's `url` equals that value **verbatim** (`jq -r .url`; not `0.0.0.0`, not `localhost`); with the variable unset or empty, construction returns an error naming the variable — the fail-closed shape `AuthFromEnv` already uses.
- [ ] **AC3 — an authenticated `message/send` returns the agent's own computed result.** Evidence: an A2A client posts `message/send` with a valid bearer token for the deterministic input of § Assumptions; the response is a `completed` task whose artifact equals the value `POST /prompt` returns for the same input on the same conversation. The test stub must **echo or transform** its input (e.g. `return "echo:" + p`), never return a constant: against a constant stub a handler that drops the request text entirely still passes, so the equality would prove nothing about input-dependence. The comparison is against the **native route**, not the A2A endpoint — otherwise it is circular — and the compared value is a computed artifact, never free-form model text.
- [ ] **AC4 — an unauthenticated `message/send` is refused with `401`.** Evidence: the same request with **no** `Authorization` header and a second with a **wrong** token each return `401`, quoted verbatim; AC3's valid-token request returns `200`. The pair is what makes this falsifiable — a service with no gate that returns `400`/`404` for another reason must not pass.
- [ ] **AC5 — the enumerated existing-route rows are unchanged.** Evidence: each row below still holds against the **in-process `httptest` server** — this repo carries no `package main`, the serving binary lives in `agent-claude` and is out of scope — with its status quoted. `/readiness` and `/metrics` are unauthenticated; the `/prompt` rows assume the gate is passed, **except** the explicit no-header `401` row below.
  - `GET /readiness`, `PROVIDER_BASE_URL` unset → `200` with the frozen body
  - `GET /metrics` → `200`
  - `GET /prompt` → `405`
  - `POST /prompt`, no `X-Session-Id`, valid token → `200`
  - `POST /prompt`, empty body, valid token → `400`
  - `POST /prompt`, no `Authorization` → `401`
- [ ] **AC6 — the credential path stays confined to `auth.go`.** Evidence: `grep -rln --include='*.go' --exclude='*_test.go' -E 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/` prints **exactly** `interactive/auth.go` — no other non-test source file references the auth internals. The pattern matches **identifiers**, not the English word: a plain `grep 'Authorization'` also matches a `//` comment that merely *describes* the gate, so a correct implementation documenting itself in a new file would fail it. `--exclude='*_test.go'` is likewise load-bearing — `auth_test.go` legitimately sets the header. This grep is a **proxy**, not the invariant; AC4's `401`/`200` pair is the load-bearing check, and it is what a handler evading the grep would still fail.
- [ ] **AC7 — the A2A path shares the session cache and per-session lock.** Evidence: two `message/send` calls on **one** `contextId` are issued concurrently; the pod log (or the test's captured log) shows the `turn start id=<id>` / `turn end id=<id>` pair for the second **after** the first's `turn end` — the same serialisation `POST /prompt` exhibits. Two *different* `contextId`s interleave. A `message/send` with **no** `contextId` resolves to the default conversation `identity`, evidenced by a `turn start id=identity` line — the same default an absent `X-Session-Id` takes today. This is what proves DB4 rather than a fresh uncached session per call.
- [ ] **AC8 — `contextId` is validated before any session is built.** Evidence: `message/send` with `contextId: "-x"` and with `contextId: "a/b"` each return a JSON-RPC error response, and no `turn start` line appears for either id — the anchored-regex rejection the service already applies to `X-Session-Id`. This is a named security control (§ Security / Abuse), not a nicety.
- [ ] **AC9 — the endpoint reports failure honestly.** Evidence: a JSON-RPC request naming an **unimplemented method** returns a JSON-RPC error object — not a panic, not a silent `200`. A `message/send` whose backend turn fails returns a **`failed`** task, and the backend error text appears in the captured log (`grep` returns ≥1 line) while appearing **zero** times in the response body.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — exits 0
- `make test` — unit + integration suite passes; AC1–AC8 are exercised here, against the in-process `httptest` server
- `grep -rln --include='*.go' --exclude='*_test.go' -E 'authorizationHeader|authTokenEnv|AuthFromEnv|NewAuthToken|authExempt' interactive/` — prints exactly `interactive/auth.go`
- `grep -n -A6 'func authExempt' interactive/auth.go` — shows the `switch` with the card path among the literal cases
- `go test ./interactive/... -count=1` — exits 0. The package's only test entry point is `TestInteractive` (the Ginkgo suite), so a `-run <name>` filter matching no function would exit 0 without running anything; no such filter is used as evidence here.

### Operator-executable

**Omitted, deliberately.** This spec ships a library change: the pod that would serve the A2A surface runs `bborbe/agent-claude`, which must bump `github.com/bborbe/agent` and be redeployed — both named out of scope in § Non-goals. There is therefore no deployed surface this spec can observe, and per `spec-writing.md` § Verification an absent operator rung is omitted rather than invented. The deployed check belongs to the consuming change, not here.

## Desired Behavior

1. `GET /.well-known/agent-card.json` is served **without a credential** and returns the Agent Card as JSON. The path is one more literal `case` in the service's existing `authExempt` switch, alongside `/readiness` and `/metrics`.
2. The card's `url` field is the value of the public-address setting, **verbatim** — the externally reachable URL of the A2A endpoint, including its path. The service never derives it from the listen address or the request `Host` header, so the container-local `0.0.0.0` address can never be advertised.
3. `POST /a2a` serves the A2A JSON-RPC binding. `message/send` is implemented; an unimplemented method returns a JSON-RPC error, not a panic and not a silent `200`.
4. An authenticated `message/send` runs **one turn** on the conversation named by the request's `contextId`, through the same session cache and per-session lock `POST /prompt` uses. An absent `contextId` resolves to the default conversation `identity`, exactly as an absent `X-Session-Id` does today. The response is a `completed` task whose artifact carries the agent's reply; a backend error yields a `failed` task, with the error logged and never returned in the body.
5. `contextId` is validated against the service's existing anchored session-id regex **before** any session is built or any body is read.
6. An unauthenticated request to `/a2a` is refused `401` by the existing gate, before the route's handler runs.

## Constraints

- **`docs/interactive-service.md` is a frozen contract** — its own words: *"it does not change without a spec."* This spec is that spec, so the document is updated in the same change, covering both new routes and their open/closed policy. That update is a closure step, not a behavior.
- **`requireAuth` keeps wrapping the whole router.** The A2A endpoint is registered on the wrapped router, never beside it. This is what makes AC6 hold by construction rather than by review.
- **`authExempt` stays an exact-path switch.** The card joins it as a literal `case`; no prefix, glob or pattern matching is added, and the function is not relocated.
- **`auth.go`'s credential logic is unchanged** — `Auth`, `AuthDisabled`, `AuthFromEnv`, `NewAuthToken` and the `Bearer ` scheme are untouched. The only edit to that file is one `case` line in `authExempt`.
- **The public address reaches the service as a constructor parameter**, built by an accessor mirroring `AuthFromEnv` (which reads its own environment variable and returns an error when it is unset). The variable is **`A2A_PUBLIC_URL`**. It is not read from the environment inside the handler, so no hidden env dependency is introduced. This changes the frozen constructor's signature, which the doc update above covers.
- **No new credential.** `INTERACTIVE_AUTH_TOKEN` remains the only one.
- **New dependency:** `github.com/a2aproject/a2a-go/v2` — note the **`/v2`** suffix; the un-suffixed module is the superseded v1 line. Requires Go ≥ 1.25.
- **Body cap and session id semantics are inherited unchanged** — 1 MiB truncating cap, and the anchored `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$` regex.
- **Network posture is unchanged** — see [agent-network-security.md](../docs/agent-network-security.md). This spec adds no exposure, no egress and no policy.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| Public-address setting unset or empty | construction returns an error naming the variable; nothing is served | set the variable and rebuild |
| `GET` on an unknown path | `404` | — |
| Malformed JSON-RPC body on `/a2a` | JSON-RPC error response; no session built | — |
| Unimplemented A2A method | JSON-RPC "method not found" error | — |
| `contextId` fails the regex | refused before the body is read; no session built | caller fixes the id |
| `Session.Prompt` returns an error | `failed` task; error logged, never returned | `kubectlnukedev -n dev logs <pod>` in the consuming deployment, then inspect the turn |
| Body larger than 1 MiB | truncated to 1 MiB and processed, as `/prompt` does | caller sends less |
| A2A endpoint registered outside the wrapped router | AC6's grep and the auth pair both fail — the change is rejected | register on the wrapped router |
| Two concurrent calls on one `contextId` | serialise on that conversation's lock; neither is rejected | — (this is AC7's expected behavior, not a fault) |

## Security / Abuse

- **The card is public by design.** It advertises the endpoint and skills and carries **no credential** — the same posture as a public `robots.txt`. It must never embed the bearer token or any other secret.
- **The endpoint runs arbitrary prompts** with the pod's `ALLOWED_TOOLS` (including `Bash`) under the cluster's credentials. Its blast radius is exactly `/prompt`'s, and the bearer gate is the control — which is why the route must be added *inside* the wrapped router. Network posture is documented in [agent-network-security.md](../docs/agent-network-security.md) and is unchanged by this spec.
- **`contextId` is attacker-controlled.** It reaches a backend CLI as an argument, so it is validated against the anchored regex before use: a leading `-` would be read as a flag, and `.` or `/` would escape a session directory. AC8 is the check.
- **No credential value is logged or returned** — statuses, key names and counts only, as the existing service already does.
- **A denial-of-service surface exists and is not addressed here:** each `message/send` occupies its conversation's session lock for the duration of a turn, so a caller can serialise behind a long turn. This matches `/prompt`'s existing exposure and is out of scope for this spec.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | A2A dependency, card construction, and the fail-closed public-address setting | 2 | 2 | — |
| 2 | Card route + the `authExempt` literal case | 1 | 1 | prompt 1 |
| 3 | A2A JSON-RPC handler and the executor bridging to the session seam | 3, 4, 5, 6 | 3, 4, 7, 8, 9 | prompt 1 |
| 4 | Contract doc update — both new routes and their policy, plus the constructor signature | — | — | prompt 3 |
| 5 | Tests: card content, the `401`/`200` auth pair, the serialisation pair, the `contextId` rejections, the error paths, and the existing-route regression | — | 1–9 | prompts 2, 3 |

Rationale: prompt 1 establishes the card and its config, which prompt 2 mounts and prompt 3's handler advertises. Prompt 3 is the only one that touches the session seam. Prompt 4 documents the shipped policy and the changed constructor signature, so it depends on prompt 3. Prompt 5 is test-only and closes the regression surface AC5 names.

## Do-Nothing Option

Without this, A2A stays a hypothesis. The fleet's agents remain callable only by hand-written clients, so the next decision about agent-to-agent interoperability — whether to standardise on A2A, what it costs to expose an agent, whether a Claude session can drive one remotely — is made without evidence. The cost is not the code this spec adds; it is that the answer to "does this work for us" keeps being deferred.
