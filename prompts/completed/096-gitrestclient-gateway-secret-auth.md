---
status: completed
summary: 'Wired shared-secret auth into task/controller gitrestclient: added gatewaySecret/gatewayInitiator fields, setAuthHeaders helper (called in Get/Post/Delete/List, not IsReady), updated NewGitRestClient and NewGitRestClientForTest signatures, added 8 new test cases (q-x), added GatewaySecret field to main.go, added gateway-secret to K8s Secret manifest sourced via teamvaultPassword, added GATEWAY_SECRET secretKeyRef to StatefulSet, and updated CHANGELOG.'
container: agent-096-gitrestclient-gateway-secret-auth
dark-factory-version: dev
created: "2026-05-02T22:20:00Z"
queued: "2026-05-02T20:24:37Z"
started: "2026-05-02T20:24:38Z"
completed: "2026-05-02T20:31:38Z"
---

<summary>
- The git-rest service deployed at `vault-obsidian-openclaw` now requires shared-secret HTTP auth on `/api/v1/*` (git-rest spec 004). Calls without the right headers get 500 / 401.
- The just-shipped `pkg/gitrestclient` does not send the auth headers; this prompt adds them.
- Two new headers go on every file-API call: `X-Gateway-Secret` (the shared secret) and `X-Gateway-Initator` (caller identity, deliberate misspelling per the contract).
- The IsReady probe (`/readiness`) stays header-free — probes are unauthenticated by design.
- The controller reads the secret from a new `GATEWAY_SECRET` env var; the K8s manifest sources it from a teamvault-backed Secret data key.
- Backward compatible: empty / unset secret → no headers sent → works against an unauth-enabled git-rest exactly like today.
</summary>

<objective>
Wire shared-secret auth into the controller's git-rest HTTP client so it can talk to the auth-enabled `vault-obsidian-openclaw` service. After this prompt, every `/api/v1/*` call from the controller carries `X-Gateway-Secret` and `X-Gateway-Initator: agent-task-controller`; the IsReady probe stays header-free.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, httptest, Counterfeiter, external test packages
- `k8s-manifest-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — env-from-secretKeyRef pattern

**Frozen header contract (from git-rest spec 004 — DO NOT alter):**
- `X-Gateway-Secret` — the configured shared secret value, exact string match.
- `X-Gateway-Initator` — caller identity; deliberately misspelled (no second `i`). Do NOT "correct" it. The controller's value is the literal string `agent-task-controller`.
- Both headers are required on every call to `/api/v1/*` when the secret is configured server-side.
- `/healthz`, `/readiness`, `/metrics` are unauthenticated and must NOT receive these headers.

**Server behaviour reminders:**
- Missing `X-Gateway-Initator` → HTTP 500 with body `header 'X-Gateway-Initator' missing`.
- Missing / wrong `X-Gateway-Secret` → HTTP 401 with body `secret in header 'X-Gateway-Secret' is invalid => access denied`.
- The server strips `X-Gateway-Secret` before the inner handler, so headers are not echoed back.

**Backward compatibility:**
- When the controller's `GATEWAY_SECRET` is empty / unset, it sends NO auth headers. This keeps tests against an in-process httptest server straightforward and lets the controller continue to talk to a git-rest that has auth disabled.

**Key files to read in full before editing:**

- `task/controller/pkg/gitrestclient/git_rest_client.go` — current `gitRestClient` struct and the 5 methods. Fields to add: `gatewaySecret string`, `gatewayInitiator string`. Headers go on `Get`, `Post`, `Delete`, `List`. `IsReady` must NOT receive them.
- `task/controller/pkg/gitrestclient/git_rest_client_test.go` — existing test cases (a–p). Add header assertions to the relevant cases; add new cases for header propagation + 401 / 500 server response handling.
- `task/controller/main.go` — `application` struct (look for the `GitRestURL` field) and the `Run` method's gitClient construction (search for `gitrestclient.NewGitRestClient`). Add a `GatewaySecret` field and pass it through.
- `task/controller/pkg/gitrestclient/export_test.go` — `NewGitRestClientForTest(baseURL, backoff)`. Almost every existing test in `git_rest_client_test.go` constructs the client through this helper, not the public `NewGitRestClient`. Its signature MUST be extended in step 2.
- `task/controller/k8s/agent-task-controller-secret.yaml` — currently has only `sentry-dsn` (data key sourced via `teamvaultUrl`). Add a `gateway-secret` data key sourced via `'{{ "OBSIDIAN_OPENCLAW_GATEWAY_SECRET" | env | teamvaultPassword | base64 }}'`. Use `teamvaultPassword` (NOT `teamvaultUrl`) — the value lives in teamvault's password field, matching how `vault-obsidian-openclaw-secret.yaml` (the git-rest server side) reads the same env var. Both ends MUST resolve to the same string for auth to work.
- `task/controller/k8s/agent-task-controller-sts.yaml` — env list. Add `GATEWAY_SECRET` via `secretKeyRef` against the new data key.

Run before editing:
```bash
grep -n "NewGitRestClient\|gatewaySecret\|GATEWAY_SECRET" task/controller/main.go task/controller/pkg/gitrestclient/git_rest_client.go
grep -n "secretKeyRef\|sentry-dsn\|gemini-api-key" task/controller/k8s/agent-task-controller-secret.yaml task/controller/k8s/agent-task-controller-sts.yaml
```
</context>

<requirements>

1. **Extend `task/controller/pkg/gitrestclient/git_rest_client.go`**

   Add two unexported fields to `gitRestClient`:
   ```go
   type gitRestClient struct {
       baseURL          string
       httpClient       *http.Client
       backoff          func(attempt int) time.Duration
       gatewaySecret    string
       gatewayInitiator string
   }
   ```

   Change the public constructor signature to accept the secret and initiator. The initiator is a constant string supplied by the caller (the controller passes `"agent-task-controller"`):
   ```go
   // NewGitRestClient creates a GitRestClient targeting the git-rest instance at baseURL.
   // gatewaySecret is the shared secret enforced by git-rest's gateway-secret auth (git-rest spec 004).
   // When gatewaySecret is empty, no auth headers are sent (backward-compat with auth-disabled git-rest).
   // gatewayInitiator is the caller identity logged by git-rest on auth failure;
   // pass a stable, human-readable value (e.g. "agent-task-controller").
   func NewGitRestClient(baseURL, gatewaySecret, gatewayInitiator string) GitRestClient {
       return newGitRestClientWithBackoff(baseURL, gatewaySecret, gatewayInitiator, exponentialBackoff)
   }

   func newGitRestClientWithBackoff(
       baseURL, gatewaySecret, gatewayInitiator string,
       backoff func(attempt int) time.Duration,
   ) GitRestClient {
       return &gitRestClient{
           baseURL:          strings.TrimRight(baseURL, "/"),
           httpClient:       &http.Client{Timeout: 30 * time.Second},
           backoff:          backoff,
           gatewaySecret:    gatewaySecret,
           gatewayInitiator: gatewayInitiator,
       }
   }
   ```

   Add an unexported helper that sets the auth headers on a request when the secret is non-empty:
   ```go
   // setAuthHeaders sets the gateway-secret auth headers on req when the secret is configured.
   // No-op when gatewaySecret is empty — keeps backward compatibility with auth-disabled git-rest.
   //
   // Header names are part of the git-rest public contract (spec 004) — do NOT alter:
   //   X-Gateway-Secret    — the shared secret
   //   X-Gateway-Initator  — caller identity (deliberate misspelling, do NOT change to "Initiator")
   func (g *gitRestClient) setAuthHeaders(req *http.Request) {
       if g.gatewaySecret == "" {
           return
       }
       req.Header.Set("X-Gateway-Secret", g.gatewaySecret)
       req.Header.Set("X-Gateway-Initator", g.gatewayInitiator)
   }
   ```

   Call `g.setAuthHeaders(req)` AFTER creating each `http.Request` and BEFORE `g.httpClient.Do(req)` in:
   - `Get`
   - `Post` (inside the retry loop, on every retry — each attempt builds a fresh request)
   - `Delete` (same)
   - `List`

   Do NOT call `setAuthHeaders` from `IsReady` — `/readiness` is an unauthenticated probe.

2. **Update tests in `task/controller/pkg/gitrestclient/`**

   First update the test helper at `export_test.go`. `NewGitRestClientForTest` must accept the two new strings and forward them to `newGitRestClientWithBackoff`:
   ```go
   // NewGitRestClientForTest creates a GitRestClient with a custom backoff for use in tests.
   // Pass a function returning 0 or 1ms to make retry tests run fast.
   func NewGitRestClientForTest(
       baseURL, gatewaySecret, gatewayInitiator string,
       backoff func(attempt int) time.Duration,
   ) GitRestClient {
       return newGitRestClientWithBackoff(baseURL, gatewaySecret, gatewayInitiator, backoff)
   }
   ```

   Then update every call site in `git_rest_client_test.go`. Existing tests that don't care about auth become `NewGitRestClientForTest(server.URL, "", "", zeroBackoff)` (or the equivalent backoff helper used in that test). The single test that uses the public `NewGitRestClient(server.URL)` directly becomes `NewGitRestClient(server.URL, "", "")`.

   Add a new `Describe` / `Context` block exercising auth header propagation. Required cases:

   q. **Get sends `X-Gateway-Secret` and `X-Gateway-Initator` when configured** — construct the client with `NewGitRestClient(server.URL, "test-secret", "test-caller")`. The test server captures `r.Header.Get("X-Gateway-Secret")` and `r.Header.Get("X-Gateway-Initator")`, returns 200 with body. Assert both header values match.
   r. **Post sends both headers on each retry** — server returns 503 twice then 200. Capture headers on each request. Assert all 3 requests had both headers set correctly.
   s. **Delete sends both headers** — analogous to Get.
   t. **List sends both headers** — analogous to Get; server returns `[]`.
   u. **IsReady does NOT send auth headers** — server captures headers and asserts both are the empty string. Returns 200 OK on `/readiness`.
   v. **Empty secret → no headers** — `NewGitRestClient(server.URL, "", "")`. Server captures `X-Gateway-Secret` and `X-Gateway-Initator` and asserts both are empty strings on a Get request. Confirms backward compat.
   w. **Server returns 401** — server returns 401 with body `secret in header 'X-Gateway-Secret' is invalid => access denied` on Get. The client returns a non-nil error containing the status code; metric `controller_gitrest_calls_total{op="get",status="error"}` increments. Same shape applies to Post / Delete / List but one test case is enough.
   x. **Server returns 500 with missing-initiator body** — server returns 500 with body `header 'X-Gateway-Initator' missing` on Get. Client returns a non-nil error.

   Use a header-capture pattern:
   ```go
   var capturedSecret, capturedInitiator string
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
       capturedSecret = r.Header.Get("X-Gateway-Secret")
       capturedInitiator = r.Header.Get("X-Gateway-Initator")
       w.WriteHeader(http.StatusOK)
       _, _ = w.Write([]byte("body"))
   }))
   ```

3. **Add `GATEWAY_SECRET` field to `task/controller/main.go`**

   Find the `application` struct (it already has `GitRestURL` and `UseGitRest`). Add a sibling field — `display:"length"` keeps the secret value out of startup config logs:
   ```go
   GatewaySecret string `required:"false" arg:"gateway-secret" env:"GATEWAY_SECRET" usage:"shared secret for git-rest gateway auth (sent as X-Gateway-Secret header)" default:"" display:"length"`
   ```

   In the gitClient construction block (look for the existing `gitrestclient.NewGitRestClient(a.GitRestURL)` call), update to:
   ```go
   restClient := gitrestclient.NewGitRestClient(a.GitRestURL, a.GatewaySecret, "agent-task-controller")
   ```
   The literal `"agent-task-controller"` is the initiator value — stable, matches the K8s app label, useful for git-rest log forensics. Do NOT extract it to a constant; it appears once.

4. **Add the secret data key to `task/controller/k8s/agent-task-controller-secret.yaml`**

   Append a third data key alongside `sentry-dsn` and `gemini-api-key`:
   ```yaml
   gateway-secret: '{{ "OBSIDIAN_OPENCLAW_GATEWAY_SECRET" | env | teamvaultPassword | base64 }}'
   ```
   The env var `OBSIDIAN_OPENCLAW_GATEWAY_SECRET` is already exported in `./dev.env` and `./prod.env` at the repo root (teamvault keys `YLbzyL` and `3OlKKw` respectively). Do NOT modify these files.

5. **Inject `GATEWAY_SECRET` env into `task/controller/k8s/agent-task-controller-sts.yaml`**

   Add a new env entry to the controller's `env:` list, placed BETWEEN the existing `SENTRY_DSN` block (sourced from secretKeyRef) and `SENTRY_PROXY` (plain value). This keeps the two `secretKeyRef`-sourced entries adjacent:
   ```yaml
   - name: GATEWAY_SECRET
     valueFrom:
       secretKeyRef:
         key: gateway-secret
         name: agent-task-controller
   ```

6. **Update `CHANGELOG.md` at repo root**

   Append bullets to `## Unreleased`. If the section does not exist (CHANGELOG starts directly with `## v0.54.x`), insert `## Unreleased` between `# Changelog` and the latest version heading first:
   ```markdown
   - feat(task/controller): gitrestclient sends `X-Gateway-Secret` + `X-Gateway-Initator` headers on `/api/v1/*` calls when `GATEWAY_SECRET` is set; matches git-rest spec 004 auth contract
   - feat(task/controller): add `GATEWAY_SECRET` env / `--gateway-secret` flag (sourced from `OBSIDIAN_OPENCLAW_GATEWAY_SECRET` teamvault key in dev/prod manifests)
   ```

7. **Run final verification:**
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- Header names are EXACT-CASE and MUST match the git-rest contract (spec 004): `X-Gateway-Secret` and `X-Gateway-Initator` (deliberate misspelling — no second `i`). Do NOT "correct" `Initator` to `Initiator`.
- `IsReady` MUST NOT send auth headers — `/readiness` is an unauthenticated probe.
- Empty `gatewaySecret` MUST suppress both headers entirely (no `X-Gateway-Secret: ` empty line, no `X-Gateway-Initator` line) — backward compatibility with auth-disabled git-rest.
- The initiator value used by the controller is the literal string `"agent-task-controller"`. Pass it inline at the `NewGitRestClient` call site; do NOT introduce a package-level constant for it.
- Constructor signature change is acceptable — the only production caller of `NewGitRestClient` is `main.go`, and tests update in step 2.
- `display:"length"` on `GatewaySecret` keeps the value out of `effective config` log lines.
- `make precommit` MUST exit 0 in `task/controller/`.
- Counterfeiter mocks for `GitRestClient` regenerate cleanly via `make generate` (no annotation changes — the interface signature is unchanged).
- Scope: controller-only. `task/executor/` does not call git-rest today; do not touch any executor manifest, code, or test in this prompt.
- Error wrapping via `github.com/bborbe/errors` — never `fmt.Errorf`.
- Ginkgo v2 + Gomega; use `httptest.NewServer` for the new tests.
- External test package (`gitrestclient_test`) — matches existing convention.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
```bash
# Verify constructor signature change
grep -n "func NewGitRestClient" task/controller/pkg/gitrestclient/git_rest_client.go
# Must show: NewGitRestClient(baseURL, gatewaySecret, gatewayInitiator string)

# Verify setAuthHeaders helper exists
grep -n "setAuthHeaders" task/controller/pkg/gitrestclient/git_rest_client.go
# Must show definition + 4 call sites (Get, Post, Delete, List) — NOT in IsReady

# Verify IsReady does NOT call it
awk '/func .g \*gitRestClient. IsReady/,/^}/' task/controller/pkg/gitrestclient/git_rest_client.go | grep -c "setAuthHeaders"
# Must print: 0

# Verify header names spelled correctly (Initator without second i)
grep -n "X-Gateway-Secret\|X-Gateway-Initator" task/controller/pkg/gitrestclient/git_rest_client.go
# Must show both header names exactly as written above
grep -n "X-Gateway-Initiator" task/controller/pkg/gitrestclient/git_rest_client.go
# Must return no matches (the second-i form is wrong)

# Verify GATEWAY_SECRET wired in main.go
grep -n "GatewaySecret\|GATEWAY_SECRET\|agent-task-controller" task/controller/main.go
# Must show the field, env tag, and the literal initiator at the constructor call site

# Verify secret manifest updated
grep -n "gateway-secret\|OBSIDIAN_OPENCLAW_GATEWAY_SECRET" task/controller/k8s/agent-task-controller-secret.yaml
# Must show the new data key sourced via teamvaultPassword

# Verify StatefulSet env updated
grep -n "GATEWAY_SECRET\|gateway-secret" task/controller/k8s/agent-task-controller-sts.yaml
# Must show the env entry with secretKeyRef

# Run all checks
cd task/controller && make precommit
# Must exit 0

# Confirm CHANGELOG entry
grep -A20 "^## Unreleased" CHANGELOG.md | grep -E "X-Gateway-Secret|GATEWAY_SECRET|gateway auth"
# Must show the bullets added in step 6
```
</verification>
