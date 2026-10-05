---
status: draft
spec: [058-interactive-service-a2a-endpoint]
created: "2026-10-05T20:20:00Z"
branch: dark-factory/interactive-service-a2a-endpoint
---

# Update the frozen interactive-service contract for the A2A surface

<summary>
- The frozen contract document that describes the interactive service's HTTP surface now describes both new A2A routes and their open/closed policy, so the document and the code agree.
- The discovery route is recorded as public by design, alongside readiness and metrics, with the reason stated rather than left implicit.
- The execution route is recorded as gated by the existing bearer token, with no second credential.
- The document records that the advertised endpoint address is configuration, never derived, and that an unset address fails closed.
- The document records the new constructor parameter every consumer must now supply, and the fail-closed accessor that supplies it.
- The project README's one-line summary of the package mentions the A2A surface.
- The changelog gains an entry describing the new capability and the breaking constructor change.
- The document's existing rows and existing policy text are preserved — only additions and the constructor description change.
- No code changes in this prompt.

</summary>

<objective>
Bring the frozen contract document `docs/interactive-service.md` in line with the shipped A2A surface — both new routes, their open/closed policy, the advertised-address rule, and the new constructor parameter — and record the capability in the README and the changelog. Implements spec 058's constraint that the frozen contract is updated in the same change as the behaviour. Depends on both code prompts having landed.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1; `interactive/` is a package in that root module, not a separate module). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry shape, the frozen preamble, and the conventional-prefix rule.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-library-guide.md` — public-API compatibility for a library consumed from other repositories; the constructor change is breaking for the consumers.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — the README and CHANGELOG requirements.

Files to read IN FULL before editing:
- `docs/interactive-service.md` — the frozen contract. Its own words: "This document is the frozen contract. Both consumers honour the interface described here, and it does not change without a spec." Spec 058 is that spec, so this document is updated here. Read every section; you will add to "Constructing the service", "Routes", "The contract, row by row" and "Authentication", and add one new section describing the A2A surface.
- `interactive/service.go` — read the SHIPPED constructor signatures and the SHIPPED `Handler()` so the documented parameter list and route list match the code exactly. The constructors now take a `publicURL string` parameter after `auth` (and before `permissions` in `NewServiceWithPermissions`).
- `interactive/a2a.go` — read the SHIPPED `A2APublicURLFromEnv`, `a2aPublicURLEnv` and `newAgentCard` so the documented accessor name, variable name and card shape match the code.
- `interactive/a2a-handler.go` — read the SHIPPED executor to document what the execution route does (one turn, contextId default, id validation, completed/failed task, error not leaked).
- `README.md` — the `interactive/` row in the package table (around line 43) is the one line this prompt changes.
- `CHANGELOG.md` — the top of the file. The newest released section is `## v0.94.0`; the frozen preamble (the SemVer bullets) sits above it and is not modified.
- `specs/in-progress/058-interactive-service-a2a-endpoint.md` — the spec this prompt implements; § Desired Behavior, § Constraints and § Acceptance Criteria are the source for the documented policy.

Load-bearing facts, verified against the shipped code and the A2A SDK source at `$(go env GOPATH)/pkg/mod/github.com/a2aproject/a2a-go/v2@v2.6.0`:

1. The two new routes are `GET /.well-known/agent-card.json` (the Agent Card, exempt from the bearer gate because discovery is public by design) and `POST /a2a` (the A2A JSON-RPC binding, gated by the existing bearer token). Both are registered on the router `requireAuth` wraps.
2. The Agent Card advertises the configured public address. In the v2 SDK the advertised endpoint is `supportedInterfaces[0].url` (the SDK's `a2a.AgentCard` has no top-level `url` field — this is the A2A protocol 1.0 shape). The address is supplied as configuration and is NEVER derived from the listen address or the request `Host` header.
3. The address is read by `interactive.A2APublicURLFromEnv(ctx) (string, error)` from the environment variable `A2A_PUBLIC_URL`; an unset or empty value is an error naming the variable, so a service that cannot advertise a real endpoint fails to start.
4. The A2A JSON-RPC method this service implements is `SendMessage` (the v2 SDK's A2A protocol 1.0 wire name). An unimplemented method returns a JSON-RPC error object. The spec's prose calls this "message/send" (the superseded v0.3 spelling) — document the SHIPPED wire name.
5. An authenticated `SendMessage` runs one turn on the conversation named by the request's `contextId`, through the same session cache and per-session lock `POST /prompt` uses. An absent `contextId` resolves to the default conversation `identity`, exactly as an absent `X-Session-Id` does. The `contextId` is validated against the anchored session-id regex `^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$` before any session is built. A backend error yields a `failed` task with the error logged and never returned in the body.
6. The constructor signature change is breaking for the two consumer repositories that call `interactive.NewService` / `interactive.NewServiceWithPermissions` — the document must say so.
7. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. No `git` command is used in this prompt's verification.

</context>

<requirements>

## 1. Update `docs/interactive-service.md`

Make additive edits only; do not rewrite or restructure existing sections, and do not change any existing row's content.

### 1a. "Constructing the service" section

1. Update the two construction examples to include the new parameter in its shipped position:

   ```go
   svc := interactive.NewService(sessions, listen, providerBaseURL, registry, auth, publicURL)
   svc := interactive.NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, publicURL, permissions)
   ```

2. Add a row to the parameter table for `publicURL`:

   ```
   | `publicURL` | The externally reachable address the Agent Card advertises, verbatim. Build it with `interactive.A2APublicURLFromEnv(ctx)`, which reads `A2A_PUBLIC_URL` and returns an error when it is unset or empty. It is never derived from `listen` or from a request header, so the container-local address can never be advertised. |
   ```

3. Add one sentence noting the parameter is a breaking addition for the consumer repositories, and that `interactive.A2APublicURLFromEnv(ctx) (string, error)` is the fail-closed accessor for it (the address accessor's counterpart to the token accessor).

### 1b. "Routes" section

Add two rows to the routes table:

```
| `/.well-known/agent-card.json` | `GET` | The A2A Agent Card as JSON | Exempt — public discovery |
| `/a2a` | `POST` | The A2A JSON-RPC binding; runs one turn on the conversation named by the request's `contextId` | Bearer token required |
```

Add a sentence after the table stating that the card route is exempt by decision (discovery is public by design, the same posture as a public `robots.txt`), that it carries no credential and never the bearer token, and that `/a2a` is gated exactly like `/prompt`.

Then amend the existing sentence that follows the table so its exempt enumeration is complete — it currently reads `Every route except /readiness and /metrics requires the bearer token described in [Authentication](#authentication); those two are exempt by decision, not by omission, and the reason is recorded there.` and must become `Every route except /readiness, /metrics and /.well-known/agent-card.json requires the bearer token described in [Authentication](#authentication); those three are exempt by decision, not by omission, and the reason is recorded there.` The same enumeration appears again in "The contract, row by row" (the line reading `no header on the exempt ones (/readiness and /metrics)`) — amend that one too.

### 1c. "The contract, row by row" section

Add these rows to the table. Every pre-existing row keeps its current text and status.

```
| `GET /.well-known/agent-card.json`, no `Authorization` header | `200`, body is the Agent Card JSON whose `name` names the agent |
| `GET /.well-known/agent-card.json`, `A2A_PUBLIC_URL` set | the card's `supportedInterfaces[0].url` equals that value verbatim — never `0.0.0.0`, never `localhost` |
| `POST /a2a`, no `Authorization` header | `401` |
| `POST /a2a`, `Authorization: Bearer <wrong token>` | `401` |
| `POST /a2a`, `Authorization: Bearer <correct token>`, `SendMessage` | `200`, a `completed` task whose artifact carries the agent's reply for that conversation |
| `POST /a2a`, `contextId` failing the session-id regex | JSON-RPC error response; no session built |
| `POST /a2a`, unimplemented method | JSON-RPC "method not found" error object |
| `POST /a2a`, backend turn fails | `failed` task; the error is logged and never returned in the body |
| `POST /a2a`, malformed JSON-RPC body | JSON-RPC error response; no session built |
| `POST /a2a`, body larger than 1 MiB | refused by the 1 MiB cap before the SDK handler reads it — **not** truncated as `/prompt` does, because a truncated JSON body cannot parse |
```

### 1d. "Authentication" section

Add one paragraph recording the A2A policy: the Agent Card route is exempt by decision, alongside `/readiness` and `/metrics`, for the same reason (discovery is public and the card carries no credential); the `/a2a` route is gated by the same bearer token as every other non-exempt route, and the A2A surface adds no second credential, no API key and no per-route token.

### 1e. New section "A2A surface"

Add a new section at the end of the document (before the "Turn-boundary logging" section is acceptable, or after it — keep it a single self-contained section) describing:

- The wire method the service implements is `SendMessage` (the A2A protocol 1.0 JSON-RPC method name); an unimplemented method returns a JSON-RPC error object.
- One `SendMessage` runs one turn on the conversation named by the request's `contextId`, through the same session cache and per-session lock `POST /prompt` uses. An absent `contextId` resolves to the default conversation `identity`.
- `contextId` is validated against the anchored session-id regex before any session is built — the same security boundary the `X-Session-Id` header has, and for the same reason (the id reaches a backend CLI as an argument).
- A successful turn returns a `completed` task whose artifact carries the agent's reply; a backend error returns a `failed` task with the error logged and never returned in the body.
- The Agent Card advertises exactly one A2A interface — the JSON-RPC binding — whose URL is the configured public address verbatim.
- This repository authors only the one-shot `SendMessage` bridge. The SDK's JSON-RPC handler also routes `SendStreamingMessage` (SSE) through the same executor; that inherited method is not part of this contract, and push notifications and task polling are not built here. Verify against the shipped `interactive/a2a-handler.go` and state exactly what the endpoint answers — do not claim only one method is served if the handler routes more.

## 2. Update `README.md`

Change the `interactive/` row (around line 43) so its description also names the A2A surface: the Agent Card at `/.well-known/agent-card.json` and the `/a2a` JSON-RPC endpoint, with the card public and `/a2a` gated by the bearer token. Keep the edit to that one row; do not restructure the table.

## 3. Add a CHANGELOG entry

In `CHANGELOG.md`, create a `## Unreleased` section immediately after the frozen SemVer preamble block (after the last MAJOR/MINOR/PATCH bullet and its blank line) and directly above `## v0.94.0` — never above or inside any line of the preamble. Add ONE bullet with the `feat:` prefix, per `changelog-guide.md`.

The bullet must name: the two new routes and their policy (the card public by design, `/a2a` gated by the bearer token); that an authenticated `SendMessage` runs one turn on the conversation named by `contextId` through the same session cache and per-session lock `POST /prompt` uses, with an absent `contextId` defaulting to `identity` and a `contextId` validated against the anchored session-id regex before any session is built; that a backend error yields a `failed` task with the error logged and never returned; that the Agent Card's advertised address is configuration read from `A2A_PUBLIC_URL` via the new fail-closed `interactive.A2APublicURLFromEnv` accessor and is never derived from the listen address; that the new dependency is `github.com/a2aproject/a2a-go/v2`; and that both constructors gain a `publicURL` parameter, which is breaking for the consumer repositories. Do not describe verification; describe what was implemented.

</requirements>

<constraints>
- **`docs/interactive-service.md` is a frozen contract** — its own words: "it does not change without a spec." Spec 058 is that spec. Update it in this change; do not leave it describing only the pre-A2A surface.
- **Additive edits only, with one required exception.** Do not rewrite, reorder or delete existing sections, existing rows, or existing policy text — **except** the single sentence in "Routes" that enumerates the exempt routes (`Every route except /readiness and /metrics requires the bearer token`), which MUST be amended to include `/.well-known/agent-card.json`: it is false the moment the card route lands, and a frozen contract asserting both "every route except readiness and metrics requires the token" and "the card route is exempt" contradicts itself. The pre-existing contract rows keep their current content and status.
- **Document the shipped behaviour, not the spec's prose.** Where the spec's vocabulary and the shipped wire format differ (the spec says `message/send`; the shipped SDK method is `SendMessage`; the spec says the card's `url`; the shipped card advertises `supportedInterfaces[0].url`), document what the code actually does. See REVIEWER NOTES.
- **The card is public by design and carries no credential.** The document must state the exemption is a decision, not an oversight, and must state that the card never carries the bearer token or any secret.
- **The advertised address is configuration, never derived.** The document must state it is never derived from the listen address or a request header, and that an unset `A2A_PUBLIC_URL` fails closed.
- **The `/a2a` route adds no second credential.** The document must state the existing bearer token is the only gate.
- **The CHANGELOG preamble is frozen.** Nothing is inserted above or inside the SemVer preamble block; `## Unreleased` goes directly below it and above `## v0.94.0`.
- **One CHANGELOG bullet, `feat:` prefix.** No bash comments, no prompt filename, no per-file listing.
- **This prompt changes documentation only.** Do NOT edit any Go file, `go.mod`, `go.sum`, or any code. If the shipped code contradicts what this prompt says to document, STOP and report the discrepancy rather than documenting code that does not exist.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
This prompt changes markdown only — no Go code — so run the documentation checks below, NOT `make precommit`. Run from `/workspace`.

```bash
# 1. The frozen contract names both new routes and the policy.
grep -n '/.well-known/agent-card.json' docs/interactive-service.md
grep -n '/a2a' docs/interactive-service.md
grep -n 'A2APublicURLFromEnv' docs/interactive-service.md
grep -n 'A2A_PUBLIC_URL' docs/interactive-service.md
grep -n 'supportedInterfaces' docs/interactive-service.md
grep -n 'SendMessage' docs/interactive-service.md
grep -n 'publicURL' docs/interactive-service.md

# 2. The existing contract rows are preserved (spot-check two that must survive verbatim).
grep -n 'PROVIDER_BASE_URL unset' docs/interactive-service.md
grep -n 'charset=utf-8' docs/interactive-service.md

# 3. The README row mentions the A2A surface.
grep -n 'agent-card.json' README.md

# 4. The CHANGELOG has an Unreleased section above v0.94.0 with a feat bullet.
grep -n '## Unreleased' CHANGELOG.md
awk '/^## /{sec=$0} sec=="## Unreleased" && /^- feat:/{found=1} END{exit !found}' CHANGELOG.md && echo "CHANGELOG feat OK"
grep -n 'a2aproject/a2a-go/v2' CHANGELOG.md
grep -n 'A2A_PUBLIC_URL' CHANGELOG.md

# 5. No Go file changed (this prompt is documentation-only).
#    (Compare against the pre-prompt state yourself; if you touched any .go file, revert it.)
grep -rn 'supportedInterfaces' interactive/*.go | head
```

Every command must produce the annotated result or the prompt is not done.

**Self-check before finishing:** re-run the block above and confirm each result; then walk spec 058's Constraint "docs/interactive-service.md is a frozen contract" and § Desired Behavior 1-6 against the document and confirm every behaviour is described, including the card's public policy, the advertised-address rule, the `/a2a` gate, the `contextId` validation, the `completed`/`failed` task outcomes and the new constructor parameter.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Decomposition.** This is the spec's prompt 4 ("contract doc update — both new routes and their policy, plus the constructor signature"). It is the spec's "closure step": the frozen contract is updated in the same change as the behaviour, so it depends on both code prompts. The README row and the CHANGELOG entry ride along here because the repo's Definition of Done requires them and this is the change's documentation-closure prompt.
- **Open question 1 — the document records the shipped wire format, which differs from the spec's prose.** The spec says `message/send` and the card's `url`; the shipped A2A SDK (protocol 1.0) uses the method name `SendMessage` and advertises the endpoint at `supportedInterfaces[0].url`. This prompt documents the shipped shape. If the reviewer wants the document to note the v0.3 spellings as well, add a sentence; do not change the shipped code.
- **Open question 2 — CHANGELOG placement.** The entry lands in the last prompt of the change rather than the first code prompt. Both prompts run on the same branch and the entry describes the whole feature, so it is written once, here, against the finished behaviour. If the operator's release tooling reads the prefix from a specific commit, moving the entry to prompt 1 is a copy-paste.
- **Open question 3 — the version bump.** The `feat:` prefix is the only prefix in `changelog-guide.md` that fits a new capability, while the constructor signature change is API-breaking (MAJOR by the repo's own SemVer preamble). The bullet says the change is breaking in prose; the version arithmetic is the release tooling's call, not this prompt's.
- **Operator-executable rung (not run in the container).** The spec's Verification ladder deliberately omits an operator rung: the pod that would serve this surface runs a different repository's binary, which must bump `github.com/bborbe/agent` and be redeployed — both named out of scope in the spec's Non-goals. There is therefore no deployed surface this change can observe, and no operator check belongs in this prompt.
