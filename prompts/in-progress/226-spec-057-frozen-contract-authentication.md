---
status: approved
spec: [057-interactive-service-authentication]
created: "2026-10-02T23:40:00Z"
queued: "2026-10-02T23:34:59Z"
---

<summary>
- The interactive service's frozen contract document stops describing a service that no longer exists: it now describes an authenticated surface.
- The document's routes table gains a policy column, so each of the four routes names the authentication policy it is served under rather than leaving it to a paragraph.
- The two routes that stay open, readiness and metrics, are recorded as deliberately exempt with the reason, so the exemption is a decision a reader can audit rather than an omission.
- The document no longer contains the sentence that the service adds no authentication — a reader can no longer be told the opposite of what the code does.
- A new section records how a caller authenticates: the header, the scheme, the environment variable the token comes from, what happens when the token is missing, and what the opt-out means.
- The document states plainly what the token is and is not — a shared secret that authenticates "something that knows the token", not a named principal — and that it is worthless over plaintext to an untrusted network.
- The document records the fail-closed property of the authentication decision, so a reader knows that leaving it unset refuses requests rather than serving them.
- Every pre-existing contract row is preserved, and the gated rows gain the qualifier they now need, so nothing the document promised before is silently withdrawn.
- Documentation only: no Go file is touched.

</summary>

<objective>
Update `docs/interactive-service.md` — the frozen contract both consumer images honour — so it describes the authenticated surface the sibling prompt `1-spec-057-interactive-service-authentication.md` built: a policy on every route, an authentication section, the corrected contract rows, and no remaining claim that the service adds no authentication. Implements spec 057 Acceptance Criterion 9 and the documentation half of Desired Behavior 5.
</objective>

<context>
Repository root inside the build container is `/workspace` (single Go module `github.com/bborbe/agent`, Go 1.27.1). Paths below are repo-relative unless they start with `/workspace`.

Read `/workspace/CLAUDE.md` for project conventions and `/workspace/docs/dod.md` for the Definition of Done.

Coding-plugin docs (paths as they exist INSIDE the container):
- `/home/node/.claude/plugins/marketplaces/coding/docs/documentation-guide.md` — documentation structure and how a contract document should read.
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — the documentation criteria.

Files to read IN FULL before editing (repo-relative):
- `docs/interactive-service.md` — the frozen contract this prompt amends. It opens by stating it does not change without a spec; spec 057 is that spec, and it changes the authentication posture ONLY.
- `interactive/auth.go` — the seam the sibling prompt created: the `Auth` value, `NewAuthToken`, `AuthFromEnv`, `AuthDisabled`, the exempt-path function and the gate. Read this file and quote its actual identifiers; do not document names from memory.
- `interactive/service.go` — the two constructors as the sibling prompt left them, so the constructing snippet and the parameter table match the real signatures and the real parameter order.
- `docs/agent-network-security.md` — the network posture reference the contract links to.
- `specs/in-progress/057-interactive-service-authentication.md` — the spec this prompt implements; its Goal, Acceptance Criterion 9, Failure Modes and Security sections are the source of the wording below.

Load-bearing facts, verified against this working tree and the sibling prompt. Do not re-derive these, do not contradict them. **Read `interactive/auth.go` and `interactive/service.go` first and confirm every identifier below against the actual source before writing a line of the document — if any name differs, document the name that is actually there and record the difference in your final report.**

1. The sibling prompt adds `interactive/auth.go` declaring an exported `Auth` value type (two unexported fields), `NewAuthToken(token string) Auth`, `AuthFromEnv(ctx context.Context) (Auth, error)`, and a package-level `var AuthDisabled Auth`. The token environment variable is `INTERACTIVE_AUTH_TOKEN`. The accepted scheme is `Bearer `, on the `Authorization` request header.
2. The gate is one middleware wrapping the whole router: `/readiness` and `/metrics` are exempt by exact path match; every other path — including `/permission`, which is only registered when a permission registry is supplied — requires the bearer token. A request that fails the gate is answered `401` with `WWW-Authenticate: Bearer`. The response body is not part of the contract.
3. The zero `Auth` refuses every gated request; `AuthDisabled` is the explicit opt-out that serves every route unauthenticated; `AuthFromEnv` returns an error when `INTERACTIVE_AUTH_TOKEN` is unset or empty, so a service that cannot authenticate fails to start rather than serving unauthenticated.
4. Both constructors now take the decision as their FIFTH parameter, immediately after `registry` and before `permissions`: `NewService(sessions, listen, providerBaseURL, registry, auth)` and `NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, permissions)`.
5. Because the gate is outermost, an unauthenticated `GET /prompt` is `401` rather than `405`, and an unauthenticated `GET /permission` on a plain `NewService` is `401` rather than `404`. The pre-existing rows in the contract table that describe those statuses are still true **for a request that has already passed the gate**, and must be qualified rather than deleted.
6. Everything the document already fixes for an authenticated caller is unchanged: session-id validation and the absent-vs-empty distinction, the 1 MiB body cap and its truncate-not-reject semantics, the readiness body strings, the two-level locking, and the turn-boundary log pair.
7. `docs/interactive-service.md` currently contains the phrase `adds no authentication` exactly twice: once in the paragraph under the Routes table (the sentence beginning "The service adds no authentication;") and once in the Permission endpoint section (the sentence beginning "The endpoint adds no authentication —"). Both must be gone.
8. `.dark-factory.yaml` sets `workflow: direct`, `autoRelease: false`, and no `hideGit`. No `git` command is used in this prompt's verification regardless, because the daemon does not check verification exit codes.
9. `make precommit` at the repository root runs `ensure format generate test check addlicense`; `make test` runs the whole module with `-race`. Both are run from `/workspace`. `interactive/` has no Makefile of its own.
</context>

<requirements>

Edit ONE file: `docs/interactive-service.md`. This is a documentation-only prompt; do not touch any `.go` file, `README.md` or `CHANGELOG.md` (the sibling prompt handles those).

Preserve the document's existing structure, tone, heading order and every pre-existing table row that is still true. The change is additive plus two sentence replacements, except where a row needs a qualifier.

## 1. Constructing the service — show the auth parameter

In the `## Constructing the service` section, update the Go snippet to the real signatures:

```go
import (
	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
)

svc := interactive.NewService(sessions, listen, providerBaseURL, registry, auth)
svc := interactive.NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, permissions)
```

Add an `auth` row to the parameter table, placed immediately after the `registry` row so the table's order matches the parameter order:

| Parameter | Meaning |
|---|---|
| `auth` | The authentication decision every gated route requires. Build it with `interactive.NewAuthToken(token)` or `interactive.AuthFromEnv(ctx)`, or state the opt-out with `interactive.AuthDisabled`. The zero value is not a usable default — it refuses every gated request. |

Keep every existing row of that table unchanged.

## 2. Routes — a policy on every route

In the `## Routes` section, replace the three-column table with a four-column table that carries a `Policy` column, so each of the four routes names the policy it is served under:

| Route | Method | Behaviour | Policy |
|---|---|---|---|
| `/readiness` | `GET` | Dials the provider; see below | Exempt — kubelet probe |
| `/metrics` | `GET` | Prometheus scrape endpoint over the injected registry | Exempt — Prometheus scrape |
| `/prompt` | `POST` | Runs one turn on the addressed conversation | Bearer token required |
| `/permission` | `GET`, `POST` | Lists the pending permission requests; delivers a verdict to the one it names | Bearer token required |

Keep the existing Behaviour wording verbatim.

Then replace the paragraph under the table that currently ends with "The service adds no authentication; the posture is namespace-scoped reachability, unchanged — see [agent-network-security.md](agent-network-security.md)." with a paragraph that says: any other method on `/prompt` is `405` **for a request that has passed the gate**; `/permission` is served only by a service built with a permission registry, and the plain `NewService` does not register it and answers `404` **for a request that has passed the gate**; every route except `/readiness` and `/metrics` requires the bearer token described in `## Authentication`; and the two exempt routes are exempt by decision, not by omission. Keep the link to `agent-network-security.md`.

The phrase `adds no authentication` must not survive anywhere in this paragraph.

## 3. New section `## Authentication`

Add a `## Authentication` section. Place it at the END of the `## The contract, row by row` section — after the `### Readiness` subsection and before the next `##` heading — so the contract table and its `### Session id` / `### Body cap` / `### Readiness` subsections keep their current parent, and the auth section follows the contract it explains. ⚠️ Do NOT insert it immediately before `### Session id`: an H2 there would re-parent those three H3 subsections under `## Authentication`, so session-id validation, the body cap and readiness would read as parts of the authentication section. It must cover, in this order:

1. **The header.** Every gated route requires `Authorization: Bearer <token>`. The scheme is matched exactly as `Bearer `. A request with no header, a wrong token, a malformed header (a different scheme, or a scheme with no value) or a token that is the right value at the wrong length is refused with `401` before the request reaches the route's own handler — before the body is read, before a session is built and before the session lock is taken, so a refused request cannot occupy a session and emits neither line of the turn-boundary pair. A `401` carries `WWW-Authenticate: Bearer`; its body is not part of the contract.
2. **The comparison.** The token is compared in constant time, so a caller cannot recover the token's length or prefix by measuring how long a refusal takes.
3. **Where the token comes from.** The token is read at startup from the `INTERACTIVE_AUTH_TOKEN` environment variable and exists only as a runtime-injected value: never in source, never in an image layer, never in a committed manifest. An unset or empty variable is an error, so a service that cannot authenticate fails to start rather than serving unauthenticated — the pod does not reach Ready. The token is never logged, at any verbosity, and never returned in an error.
4. **Construction.** The service cannot be built without stating its authentication choice: both constructors take the decision as their fifth parameter, and the zero value fails closed — it refuses every gated request rather than serving everything. A consumer that is deliberately unexposed states the opt-out explicitly with `interactive.AuthDisabled`, which is a visible, greppable line rather than an inherited default.
5. **The exempt routes and why.** `/readiness` and `/metrics` stay open, deliberately. A kubelet readiness probe and a Prometheus scrape cannot present a bearer token without the token being written into the pod spec's probe stanza and the scrape configuration, which multiplies the secret's exposure beyond the runtime injection this design depends on, and a misconfigured readiness probe wedges the pod's Ready state. Neither route grants execution or reveals a credential. State the consequence plainly: these two routes remain readable by anything that can reach the port.
6. **What the token is not.** It is a shared secret, so any holder has the full authority of the endpoint — it authenticates "something that knows the token", not a named principal. Per-caller identity, rotation without restart and revocation are out of scope and are not claimed. Rotating the secret leaves the running pod serving the old token until it is restarted.
7. **Transport.** The gate is worthless over plaintext to an untrusted network. The recorded posture pairs the token with cluster-network binding; exposing the port beyond the cluster without TLS is not covered by this contract.

Write it as prose with short paragraphs, in the document's existing voice — declarative, specific, no bullet-list padding. Do not add a table unless a status needs one.

## 4. The contract, row by row — the auth rows and the qualifiers

The existing table in `## The contract, row by row` is the authority. Keep every existing row's expected value unchanged, and add a one-sentence note directly under the table stating that every **pre-existing** row describes a request that has already passed the authentication gate — so its status assumes a valid `Authorization` header on the gated routes (`/prompt` and `/permission`) and no header on the exempt ones (`/readiness` and `/metrics`) — while the new `401` rows describe the gate's own refusal and are therefore **pre-gate**. ⚠️ Scope the note to the pre-existing rows: it must never claim the `401` rows passed the gate, which would contradict the rows it sits above.

Then add these rows to the same table, grouped with the existing rows they belong to:

- `POST /prompt`, no `Authorization` header → `401`
- `POST /prompt`, `Authorization: Bearer <wrong token>` → `401`
- `POST /prompt`, `Authorization: Bearer <correct token>` → `200` — the same behaviour the existing valid-body row describes, unchanged
- `GET /permission`, permission-enabled, no `Authorization` header → `401`
- `GET /permission`, permission-enabled, correct token, nothing pending → `200`, `Content-Type: application/json`, body exactly `[]` (this already exists as a row; keep it, and let the new note carry the header qualifier)

Do NOT add rows for the exempt routes. `/readiness` and `/metrics` keep their existing rows unchanged; the note under the table carries their header qualifier. Adding `200` rows for them would duplicate rows that are already correct.

Do not delete or reword any pre-existing expected value. In particular the `GET /prompt` → `405` row and the `GET /permission`, plain `NewService` → `404` row stay exactly as they are; the note under the table is what now says they describe a request that has passed the gate.

## 5. Permission endpoint — drop the stale claim

In the `## Permission endpoint` section, the paragraph that currently begins "The endpoint adds no authentication — the posture is namespace-scoped reachability, unchanged, like its siblings — and no logging of its own." must be rewritten so that:

- it no longer contains the phrase `adds no authentication`;
- it states that the endpoint is gated like `/prompt` and that `## Authentication` records the policy;
- it keeps the two facts the paragraph already carries: the endpoint adds no logging of its own, and the tool-input preview is carried to the caller inside the namespace and is never logged;
- it keeps the closing fact that the endpoint is the route `agent-pi` does not serve, because that consumer builds through `NewService`, which takes no permission registry.

## 6. Scope containment

Edit ONLY `docs/interactive-service.md`. Do NOT edit `README.md`, `CHANGELOG.md`, `docs/agent-network-security.md`, or any Go file — the sibling prompt owns the first two, and this spec does not change the network posture document or the code.
</requirements>

<constraints>
- **`docs/interactive-service.md` is a frozen contract.** It opens by stating it does not change without a spec; spec 057 is that spec and it changes the authentication posture ONLY. Every statement it makes about session-id validation, the absent-vs-empty distinction, the 1 MiB body cap and its truncate-not-reject semantics, the readiness body strings, the two-level locking, the turn-boundary log pair and the permission-endpoint JSON shapes stays true and must not be withdrawn or reworded into something different.
- **The two exempt routes are a deliberate, recorded exception, not an oversight.** `/readiness` and `/metrics` stay open; the document must give the reason (a kubelet probe and a Prometheus scrape cannot carry a token without the token being written into the pod spec and the scrape configuration, and a misconfigured readiness probe wedges the pod) and must state the consequence plainly: those two routes remain readable by anything that can reach the port.
- **The token is a shared secret, not a named principal.** The document must not claim per-caller identity, rotation without restart, or revocation. It must say that a running pod keeps serving the old token until it is restarted.
- **Secret handling in the document itself.** Never write a token value, a token-shaped example, or anything that could be mistaken for a live credential. Where an example header is needed, write the placeholder `<token>` — never a realistic-looking literal. Evidence for anything token-shaped is a key name, a status or a count, never a value.
- **The document is the contract the tests assert against.** Do not introduce a status, a header or a body string that the code does not produce. Read `interactive/auth.go` and `interactive/service.go` first and document what is actually there.
- **Documentation only.** No Go file, no README, no CHANGELOG, no other doc is touched by this prompt.
- **Repository hygiene.** `make precommit` must still pass, which for a doc-only change means the existing suite, lint and license checks are unaffected.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run from the repository root inside the container (`/workspace`). All commands below are container-executable; none needs host tooling, Docker, a cluster or a second repository.

```bash
# 1. The document names the Authorization header.
grep -n 'Authorization' docs/interactive-service.md
# Must return >= 1 line.

# 2. The stale claim is gone, from both places.
grep -c 'adds no authentication' docs/interactive-service.md
# Must print 0.

# 3. The routes table carries a policy on all four rows.
grep -n 'Policy' docs/interactive-service.md
grep -c 'Bearer token required' docs/interactive-service.md
grep -c 'Exempt' docs/interactive-service.md
# The header must appear; "Bearer token required" must count >= 2 and "Exempt" >= 2.

# 4. The token's source and the fail-closed property are recorded.
grep -n 'INTERACTIVE_AUTH_TOKEN' docs/interactive-service.md
grep -n 'AuthDisabled' docs/interactive-service.md
grep -n 'fails closed' docs/interactive-service.md
# Each must return >= 1 line.

# 5. The constructing snippet and the parameter table match the real signatures.
grep -n 'registry, auth)' docs/interactive-service.md
grep -n 'registry, auth, permissions)' docs/interactive-service.md
# Each must return >= 1 line.

# 6. The pre-existing contract rows and facts are intact. Each baseline below was measured
# on the pre-change document; the count must be >= it (adding auth rows may raise it, never lower it).
grep -c 'X-Session-Id' docs/interactive-service.md                      # >= 4
grep -c 'GET /prompt' docs/interactive-service.md                       # >= 1
grep -c 'truncates' docs/interactive-service.md                         # >= 1
grep -c 'does not change without a spec' docs/interactive-service.md    # >= 1
grep -c 'never logged' docs/interactive-service.md                      # >= 2

# 7. No realistic-looking credential was written into the document.
grep -nE 'Bearer [A-Za-z0-9_-]{8,}' docs/interactive-service.md
# Must print NOTHING — placeholders are written as `<token>`.

# 8. The document is the only file this prompt touched.
grep -c 'adds no authentication' docs/interactive-service.md
# Must print 0. Re-run the two AC9 greps together as the final word:
grep -n 'Authorization' docs/interactive-service.md
grep -c 'adds no authentication' docs/interactive-service.md

# 9. The repository still validates.
make precommit
# Must exit 0.
```

If any target fails, fix it and re-run ONLY the failing target until it passes, then re-run `make precommit` once more.
</verification>

---

## REVIEWER NOTES (audit-time only — not actionable by the executor)

- **Doc-only, and deliberately second.** Per the spec's § Suggested Decomposition this prompt covers Acceptance Criterion 9 and the documentation half of Desired Behavior 5, and it depends on the sibling prompt: the document would be dishonest describing behaviour that does not exist yet. It is doc-only precisely so its verification greps (`Authorization` present, `adds no authentication` absent, four policy rows) are crisp and cannot be satisfied by a code change alone.
- **The `Policy` column is the chosen shape.** AC9 says "the routes table carries four rows each naming its policy" without fixing the mechanism. A fourth `Policy` column is the decision, because it makes each row's policy independently greppable. Add the column; do not move the policy into prose.
- **The `401`-before-`405`/`404` precedence is the subtle part of this document change.** The gate is outermost, so an unauthenticated `GET /prompt` is `401`, not `405`, and an unauthenticated `GET /permission` on a plain `NewService` is `401`, not `404`. The pre-existing rows stay literally true only for a request that has passed the gate, which is why the prompt adds one qualifying note under the table rather than rewriting those rows. A reviewer who instead wants the rows rewritten should check that the sibling prompt's tests still assert the same statuses.
- **`README.md` and `CHANGELOG.md` belong to the sibling prompt.** They are named here only to keep them out of this prompt's scope. Do not touch either file.
- **Operator-executable rung (not run in the container).** Acceptance Criterion 10 is a post-deploy triple probe against the deployed pod plus a count-only token search over the built image and the committed manifests. It needs a cluster, a built image and the consumer release, none of which exist inside the container; it belongs on the spec's Verification ladder, not in this prompt.
