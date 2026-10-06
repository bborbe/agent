---
status: completed
summary: Made the A2A Agent Card name caller-configurable by replacing both interactive constructors' publicURL string parameter with a CardConfig{Name, PublicURL} value, defaulting an empty Name to "interactive", with tests, frozen-contract docs and CHANGELOG updated.
execution_id: agent-card-name-exec-230-interactive-agent-card-name
dark-factory-version: dev
created: "2026-10-06T08:12:32Z"
queued: "2026-10-06T08:12:32Z"
started: "2026-10-06T08:13:04Z"
completed: "2026-10-06T08:21:16Z"
---

<summary>
- The A2A Agent Card the interactive service publishes now names the deployed agent instead of the fixed word `interactive`
- Callers choose that name when they construct the service, alongside the public address the card already advertises
- The name and the public address travel together in one small configuration value, so the two strings can no longer be swapped by accident in a long positional argument list
- A caller that supplies no name keeps today's card name, `interactive`, so an omitted name never yields a nameless card
- The frozen contract document, CHANGELOG and tests are updated to match
- This is a breaking change to both service constructors; consumer repositories adopt it when they bump the library
</summary>

<objective>
Make the Agent Card `name` configurable by the caller of `interactive.NewService` / `interactive.NewServiceWithPermissions`, so a deployed agent (for example `claude-interactive`) advertises its own name at `/.well-known/agent-card.json`. The card's name and its public URL are passed together as one struct value, replacing the current `publicURL string` parameter.
</objective>

<context>
Read `CLAUDE.md` at the repo root first.

Current state (verified at `v0.96.0`, commit `4321f67`):

- `interactive/a2a.go:23` — `const agentCardName = "interactive"`; `newAgentCard(publicURL string) *a2a.AgentCard` (around line 47) sets `Name: agentCardName`.
- `interactive/service.go:52` — `NewService(sessions agentlib.SessionFactory, listen string, providerBaseURL string, registry *prometheus.Registry, auth Auth, publicURL string) Service` delegates to `NewServiceWithPermissions(..., publicURL, nil)`.
- `interactive/service.go:81` — `NewServiceWithPermissions(sessions, listen, providerBaseURL, registry, auth, publicURL string, permissions PermissionRegistry) Service` stores `card: newAgentCard(publicURL)`.
- Callers inside this repo (all tests): `interactive/auth_test.go:51,64`, `interactive/service_test.go:63,682`, `interactive/permission_test.go:133`, and the helper `newA2ATestServer` in `interactive/a2a_test.go:35-40` used by `interactive/a2a_handler_test.go`.
- `interactive/a2a.go` also exports `A2APublicURLFromEnv(ctx)`; leave it unchanged.
- Frozen contract: `docs/interactive-service.md` — constructor lines 20-21, the parameter table row for `publicURL` at line 31, the breaking-change note at lines 34-36, and the card description around lines 113 and 344-347.
- This is a deliberate, operator-approved change to the frozen contract outside a spec: spec 058 (AC1, "whose `name` names the agent") owns the card, and spec 056's "no config struct on NewService" rationale (agent-pi compiling unchanged) no longer holds since specs 057/058 already broke the signature.
- `.maintainer.yaml` has `release.autoRelease: true`: add the CHANGELOG bullet under `## Unreleased` and never create a version heading or tag.

Read the coding guides that apply before editing: `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md`, `go-error-wrapping-guide.md`, `go-testing-guide.md` (if a listed file is absent, continue with the others).
</context>

<requirements>
1. In `interactive/a2a.go`, add an exported struct:

   ```go
   // CardConfig is what the A2A Agent Card advertises about this service.
   type CardConfig struct {
       // Name names the deployed agent in the Agent Card. Empty means the card keeps
       // the library default, "interactive".
       Name string
       // PublicURL is the externally reachable A2A endpoint the card advertises
       // verbatim. It is never derived from the listen address or a request header.
       PublicURL string
   }
   ```

   Keep the existing constant, renamed to `defaultAgentCardName = "interactive"`, and change `newAgentCard` to take `CardConfig`: `Name` is `config.Name` when non-empty, otherwise `defaultAgentCardName`; the interface URL is `config.PublicURL`. Nothing else in the card changes (version, description, skills, modes, capabilities). Update `newAgentCard`'s doc comment to refer to `config.PublicURL` and `config.Name`.

2. In `interactive/service.go`, replace the `publicURL string` parameter of **both** `NewService` and `NewServiceWithPermissions` with `card CardConfig`, in the same position. `NewService` passes `card` through. Update both doc comments: `card` names the agent in the Agent Card and carries the public address, which is never derived from `listen`. Do not add a third constructor and do not add variadic options.

3. Update every in-repo caller to the new signature. For the test helper `newA2ATestServer`, keep its `publicURL` argument and build `interactive.CardConfig{PublicURL: publicURL}` inside it, so the many `a2a_handler_test.go` call sites need no change.

4. Tests (Ginkgo/Gomega, external `interactive_test` package, following the existing card specs in `interactive/a2a_test.go`):
   - a service built with `interactive.NewService(newAuthMockFactory(), ":0", "", prometheus.NewRegistry(), interactive.NewAuthToken(authTestToken), interactive.CardConfig{Name: "claude-interactive", PublicURL: <url>})`, wrapped in `httptest.NewServer` and fetched via `getAgentCard`, serves `name` == `"claude-interactive"` and `supportedInterfaces[0].url` == `<url>` (decode the JSON body; do not call unexported helpers);
   - a card built with an empty `Name` serves `name` == `"interactive"` (the existing "serves the agent card without a credential" spec already asserts this through `newA2ATestServer`; keep it, do not duplicate it);
   - existing specs that assert the advertised URL keep passing.

5. `docs/interactive-service.md`: update the two constructor lines and the parameter table (replace the `publicURL` row with a `card` row describing `Name` and `PublicURL`, keeping the existing `A2APublicURLFromEnv` guidance under `PublicURL`); update the breaking-change note to name this signature change; at line ~113 keep "whose `name` names the agent" and add that the name is the caller's `CardConfig.Name` (default `interactive`); in the Agent Card paragraph (~line 344-348) replace "supplied as the constructor's `publicURL` parameter" with "supplied as the constructor's `card` parameter's `PublicURL` field". After this step no lowercase `publicURL` remains anywhere in the document.

6. `CHANGELOG.md`: one bullet under `## Unreleased` (create the heading above the newest `## v` heading if absent), prefixed `feat!:` — the constructors' `publicURL string` parameter becomes `card CardConfig{Name, PublicURL}`, so the Agent Card names the deployed agent; empty `Name` keeps `interactive`.
</requirements>

<constraints>
- Do not change routes, auth exemptions, the JSON-RPC handler, `A2APublicURLFromEnv`, or any other card field.
- Do not read any name from the environment inside the library; the caller supplies it.
- No `fmt.Errorf`; no `context.Background()` in non-test code.
- Do not touch any consumer repository; do not add a `replace` directive to `go.mod`.
- Do not commit; dark-factory handles git.
</constraints>

<verification>
```bash
make precommit
grep -n 'defaultAgentCardName = "interactive"' interactive/a2a.go     # exactly one match
grep -c 'publicURL string' interactive/service.go                      # prints 0
grep -n 'card CardConfig' interactive/service.go                       # two matches (both constructors)
grep -n 'Equal("claude-interactive")' interactive/a2a_test.go           # the named-card assertion exists
grep -c 'CardConfig' CHANGELOG.md                                      # >= 1 (0 = bullet missing)
awk '/^## /{sec=$0} /CardConfig/{print "sits under: " sec}' CHANGELOG.md   # at least one line; every line: sits under: ## Unreleased
grep -nw 'publicURL' docs/interactive-service.md                       # no output
grep -c 'CardConfig' docs/interactive-service.md                       # >= 1
```
</verification>
</content>
</invoke>
