---
status: committing
spec: [018-use-git-rest-for-vault-writes]
summary: Created pkg/gitrestclient package with GitRestClient interface, production implementation with retry backoff, Counterfeiter mock, 13+ tests at 84.8% coverage, and two new Prometheus metrics
container: agent-091-spec-018-gitrestclient-package
dark-factory-version: dev
created: "2026-05-02T19:50:00Z"
queued: "2026-05-02T19:43:29Z"
started: "2026-05-02T19:43:31Z"
branch: dark-factory/use-git-rest-for-vault-writes
---

<summary>
- New `task/controller/pkg/gitrestclient/` package provides a low-level HTTP client for git-rest's `/api/v1/files` REST API
- `GitRestClient` interface exposes 5 operations: `Get`, `Post`, `Delete`, `List`, `IsReady` — the primitive HTTP verbs used for all vault reads and writes
- Production implementation uses `http.Client` with 30 s timeout; `Post` and `Delete` retry up to 5 attempts with 1/2/4/8 s exponential backoff on 5xx or network errors
- Counterfeiter mock generated to `task/controller/mocks/git_rest_client.go` for use in downstream prompt tests
- Two new Prometheus counters added to `pkg/metrics/metrics.go`: `controller_gitrest_calls_total{op,status}` (tracks every HTTP call) and `controller_kafka_consume_paused_total` (tracks retry-induced pauses)
- All new metrics pre-initialized in `init()` to prevent sparse Prometheus series
- No changes to any existing package — this prompt is purely additive
</summary>

<objective>
Create the `pkg/gitrestclient/` package in `task/controller/` that wraps git-rest's HTTP API. This is the foundation for spec-018: replacing the embedded git binary with HTTP calls. The GitClient adapter (wiring this into the existing `gitclient.GitClient` interface) is in prompt 2.
</objective>

<context>
Read `CLAUDE.md` for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface + constructor + struct pattern, counterfeiter annotations
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo/Gomega, httptest, Counterfeiter mocks, external test packages
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — bborbe/errors, never fmt.Errorf
- `go-prometheus-metrics-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — promauto, init() pre-initialization pattern

**Key files to read in full before editing:**

- `task/controller/pkg/metrics/metrics.go` — existing metric declarations and `init()` pre-initialization pattern. The new metrics must follow the same `promauto.NewCounterVec` / `promauto.NewCounter` pattern. `KafkaConsumePausedTotal` has NO labels, so use `promauto.NewCounter` (not `NewCounterVec`).
- `task/controller/pkg/gitclient/git_client.go` — the `GitClient` interface that the gitRestGitClientAdapter (prompt 2) will implement. Read to understand the method signatures `AtomicWriteAndCommitPush`, `AtomicReadModifyWriteAndCommitPush`, `Path()`.
- `task/controller/mocks/git_client.go` — how Counterfeiter generates mocks; same pattern for `FakeGitRestClient`.
- `task/controller/pkg/command/task_increment_frontmatter_executor.go` — example of how metrics are consumed from `pkg/metrics`.

**git-rest API (target service `vault-obsidian-openclaw` deployed in dev — runs `git-rest v0.16.0`):**

Pinned API contract (verified from git-rest source — do NOT re-verify at runtime):
- `GET /api/v1/files/{path}` — returns raw file bytes (`Content-Type: application/octet-stream`); 200 on success, 404 if missing.
- `POST /api/v1/files/{path}` — body is raw file bytes; any `Content-Type` accepted (server reads `req.Body`). git-rest auto-commits + pushes. 200 on success with JSON `{"ok":true}`. 413 if body > 10 MiB.
- `DELETE /api/v1/files/{path}` — git-rest auto-commits + pushes. 200 on success.
- `GET /api/v1/files/?glob={pattern}` — **returns a JSON array of strings** (`Content-Type: application/json`) via git-rest's `SendJSONResponse`. Empty match returns `[]` (NOT empty body). Single-level glob via Go `filepath.Match`.
- `GET /readiness` — 200 if ready, 503 if push stuck or conflict pending.

No need to grep the git-rest module at runtime; the formats above are authoritative for this prompt.

**Coordination:** spec 017 (`create-task-command`) is currently `verifying` and adds an executor under `pkg/command/`. This prompt only adds new files under `pkg/gitrestclient/` and `pkg/metrics/`; no conflict.

Run before editing to see current package structure:
```bash
ls task/controller/pkg/
grep -n "promauto\|NewCounterVec\|NewCounter\b" task/controller/pkg/metrics/metrics.go | head -20
grep -n "counterfeiter:generate" task/controller/mocks/mocks.go task/controller/pkg/gitclient/git_client.go
```
</context>

<requirements>

1. **Create `task/controller/pkg/gitrestclient/git_rest_client.go`**

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package gitrestclient

   import (
       "bytes"
       "context"
       "io"
       "net/http"
       "strings"
       "time"

       "github.com/bborbe/errors"

       "github.com/bborbe/agent/task/controller/pkg/metrics"
   )

   //counterfeiter:generate -o ../../mocks/git_rest_client.go --fake-name FakeGitRestClient . GitRestClient

   // GitRestClient is the HTTP client for git-rest's /api/v1/files REST API.
   // All paths are relative to the repo root (e.g. "tasks/foo.md").
   type GitRestClient interface {
       // Get retrieves the current content of the file at relPath.
       Get(ctx context.Context, relPath string) ([]byte, error)
       // Post writes content to relPath; git-rest auto-commits and pushes.
       Post(ctx context.Context, relPath string, content []byte) error
       // Delete removes the file at relPath; git-rest auto-commits and pushes.
       Delete(ctx context.Context, relPath string) error
       // List returns relative paths matching the single-level glob pattern (e.g. "tasks/*.md").
       List(ctx context.Context, glob string) ([]string, error)
       // IsReady reports whether git-rest's /readiness returns 200.
       // Returns (false, nil) when git-rest returns 503 — that is a valid not-ready state, not an error.
       // Returns (false, err) only on network failure or unexpected response.
       IsReady(ctx context.Context) (bool, error)
   }

   // NewGitRestClient creates a GitRestClient targeting the git-rest instance at baseURL.
   // baseURL example: "http://vault-obsidian-openclaw:9090"
   func NewGitRestClient(baseURL string) GitRestClient {
       return &gitRestClient{
           baseURL:    strings.TrimRight(baseURL, "/"),
           httpClient: &http.Client{Timeout: 30 * time.Second},
       }
   }

   type gitRestClient struct {
       baseURL    string
       httpClient *http.Client
   }
   ```

   **Implement `Get`:** Makes a single `GET /api/v1/files/{relPath}` request. On 200 returns the body bytes. On any non-200 returns a wrapped error with the status code and body preview. Increments `metrics.GitRestCallsTotal.WithLabelValues("get", "success")` on success and `WithLabelValues("get", "error")` on failure.

   **Implement `Post` with retry:**
   ```
   for attempt := 0; attempt < 5; attempt++ {
       if attempt > 0 {
           // increment KafkaConsumePausedTotal — Kafka goroutine is blocked here
           metrics.KafkaConsumePausedTotal.Inc()
           backoff := time.Duration(1<<uint(attempt-1)) * time.Second // 1s, 2s, 4s, 8s
           select {
           case <-ctx.Done():
               return ctx.Err() wrapped
           case <-time.After(backoff):
           }
       }
       // POST with bytes.NewReader(content)
       // Set Content-Type: application/octet-stream
       // On 2xx: increment "post","success" metric and return nil
       // On non-2xx or network error: increment "post","error" metric, save lastErr, continue
   }
   return lastErr wrapped with "POST {relPath} failed after 5 attempts"
   ```

   **Implement `Delete` with retry:** Same retry pattern as `Post` but `http.MethodDelete` with no body. Op label is `"delete"`.

   **Implement `List`:** Single `GET /api/v1/files/?glob={glob}` request. Response is JSON array of strings (e.g. `["tasks/foo.md","tasks/bar.md"]`); empty result is `[]` (NOT empty body). Parse with `encoding/json` into `[]string`. On parse error or non-200, return wrapped error and increment `"list","error"`. On success increment `"list","success"`. Use `url.Values{}` and `req.URL.RawQuery = q.Encode()` to glob-encode safely.

   **Implement `IsReady`:** Single `GET /readiness` request. Returns `(true, nil)` on 200, `(false, nil)` on 503 (not an error — expected when git-rest has a pending push), `(false, error)` on network failure or unexpected status. Op label is `"readiness"`. Always drain the response body before returning (`io.Copy(io.Discard, resp.Body)`).

2. **Create `task/controller/pkg/gitrestclient/gitrestclient_suite_test.go`**

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package gitrestclient_test

   import (
       "testing"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"
   )

   func TestGitrestclient(t *testing.T) {
       RegisterFailHandler(Fail)
       RunSpecs(t, "Gitrestclient Suite")
   }
   ```

3. **Create `task/controller/pkg/gitrestclient/git_rest_client_test.go`**

   External test package (`gitrestclient_test`). Use `net/http/httptest.NewServer` to test all 5 interface methods. No real network calls.

   Required test cases:

   a. **Get — 200:** server returns `"---\nfoo: bar\n---\nbody"`; `Get` returns those bytes and nil error.
   b. **Get — 404:** server returns 404; `Get` returns non-nil error.
   c. **Get — network error:** `client.Get` on a stopped server returns non-nil error.
   d. **Post — success on first attempt:** server returns 201; `Post` returns nil.
   e. **Post — success after 1 retry:** server returns 503 on first attempt, 200 on second; `Post` returns nil (verify 2 requests received).
   f. **Post — fail after 5 attempts:** server always returns 503; `Post` returns non-nil error after 5 requests.
   g. **Delete — success:** server returns 200; `Delete` returns nil.
   h. **Delete — fail after 5 attempts:** server always returns 503; `Delete` returns non-nil error.
   i. **List — 2 paths:** server returns `Content-Type: application/json` with body `["tasks/foo.md","tasks/bar.md"]`; `List` returns `[]string{"tasks/foo.md","tasks/bar.md"}`, error nil. Verify glob is propagated as a query param (`r.URL.Query().Get("glob")` matches the input).
   j. **List — empty array:** server returns 200 with body `[]`; `List` returns an empty (non-nil) slice and nil error.
   j2. **List — malformed JSON:** server returns 200 with body `not-json`; `List` returns nil and a non-nil error (boundary contract — exercises the `encoding/json` parse path with an actual fixture, not a shape assertion).
   k. **IsReady — 200:** returns `true, nil`.
   l. **IsReady — 503:** returns `false, nil` (not an error).
   m. **IsReady — network error:** returns `false, error`.

   Use a request-counter pattern for retry tests:
   ```go
   callCount := 0
   server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
       callCount++
       if callCount < 2 {
           w.WriteHeader(http.StatusServiceUnavailable)
           return
       }
       w.WriteHeader(http.StatusOK)
   }))
   ```

   Note: retry tests with 5 attempts have a ~30 s backoff total. To keep tests fast, inject the backoff function via an unexported test helper:

   ```go
   // In production code (git_rest_client.go), define an unexported field:
   type gitRestClient struct {
       baseURL    string
       httpClient *http.Client
       backoff    func(attempt int) time.Duration // injectable; defaults to exponential
   }

   // NewGitRestClient sets backoff to the production exponential function.
   // For tests, expose a package-internal constructor newGitRestClientForTest
   // (or use an export_test.go) that takes a custom backoff returning 0 or 1ms.
   ```

   Use `export_test.go` to expose the test constructor without polluting the public API. Retry tests run in <100ms total. Do NOT use `Label("slow")`.

4. **Add new metrics to `task/controller/pkg/metrics/metrics.go`**

   Append these declarations BEFORE `func init()`:

   ```go
   // GitRestCallsTotal counts git-rest HTTP API calls by operation and outcome.
   var GitRestCallsTotal = promauto.NewCounterVec(
       prometheus.CounterOpts{
           Name: "controller_gitrest_calls_total",
           Help: "Total number of git-rest HTTP API calls by operation and outcome.",
       },
       []string{"op", "status"},
   )

   // KafkaConsumePausedTotal counts times a Kafka command executor blocked
   // waiting for git-rest to become available (i.e. retry attempts after the first).
   var KafkaConsumePausedTotal = promauto.NewCounter(prometheus.CounterOpts{
       Name: "controller_kafka_consume_paused_total",
       Help: "Total number of times Kafka consumption was paused waiting for git-rest.",
   })
   ```

   Add to `func init()` (inside the existing function body, after the existing pre-initializations):

   ```go
   for _, op := range []string{"get", "post", "delete", "list", "readiness"} {
       for _, status := range []string{"success", "error"} {
           GitRestCallsTotal.WithLabelValues(op, status).Add(0)
       }
   }
   ```

   (`KafkaConsumePausedTotal` is a plain Counter — no `WithLabelValues` needed for pre-initialization.)

5. **Run `make generate` in `task/controller/`** to generate the Counterfeiter mock at `mocks/git_rest_client.go`.

   Verify the annotation is correct:
   ```bash
   grep -n "counterfeiter:generate" task/controller/pkg/gitrestclient/git_rest_client.go
   ```
   Then run:
   ```bash
   cd task/controller && make generate
   ls task/controller/mocks/git_rest_client.go
   ```

6. **Update `CHANGELOG.md` at repo root**

   Append to `## Unreleased`. The current `CHANGELOG.md` starts with `# Changelog` followed directly by `## v0.54.9` — there is no `## Unreleased` section. Insert a new `## Unreleased` heading immediately below `# Changelog` and above `## v0.54.9`, then add the bullets under it. (dark-factory's autoRelease bumps `## Unreleased` → `## vX.Y.Z` on push.)

   ```markdown
   - feat(task/controller): add `pkg/gitrestclient` — HTTP client for git-rest API with Get/Post/Delete/List/IsReady, retry with exponential backoff, and Counterfeiter mock
   - feat(task/controller): add `controller_gitrest_calls_total` and `controller_kafka_consume_paused_total` Prometheus metrics
   ```

7. **Run tests:**

   ```bash
   cd task/controller && make test
   ```
   Must pass. Then:
   ```bash
   cd task/controller && make precommit
   ```
   Must exit 0.

</requirements>

<constraints>
- New files only: `task/controller/pkg/gitrestclient/git_rest_client.go`, test files, `task/controller/mocks/git_rest_client.go` (generated)
- Only existing file modified: `task/controller/pkg/metrics/metrics.go`
- Do NOT change any other existing file — no gitclient, no scanner, no factory, no main.go
- `KafkaConsumePausedTotal` is a plain `promauto.NewCounter` (no labels) — `Inc()` with no args
- `GitRestCallsTotal` uses `promauto.NewCounterVec` with `[]string{"op", "status"}`
- HTTP client timeout: 30 s
- Retry only on `Post` and `Delete` — `Get`, `List`, `IsReady` do NOT retry. Reads fail-fast so the Kafka loop sees git-rest unavailability immediately and pauses; writes retry to absorb transient pushes.
- Retry uses context-aware `select` with `time.After` to respect cancellation
- Error wrapping via `github.com/bborbe/errors` — never `fmt.Errorf`
- External test package (`gitrestclient_test`) — matches project convention
- Counterfeiter annotation format must exactly match existing annotations in the codebase (grep `task/controller/pkg/gitclient/git_client.go`)
- Do NOT commit — dark-factory handles git
- `cd task/controller && make precommit` must exit 0
</constraints>

<verification>
```bash
ls task/controller/pkg/gitrestclient/
# Must show: git_rest_client.go, gitrestclient_suite_test.go, git_rest_client_test.go

ls task/controller/mocks/git_rest_client.go
# Must exist

grep -n "GitRestClient\|func New" task/controller/pkg/gitrestclient/git_rest_client.go
# Must show: interface definition, NewGitRestClient constructor

grep -n "GitRestCallsTotal\|KafkaConsumePausedTotal" task/controller/pkg/metrics/metrics.go
# Must show both new metrics

grep -n "controller_gitrest_calls_total\|controller_kafka_consume_paused_total" task/controller/pkg/metrics/metrics.go
# Must show the Prometheus metric names

cd task/controller && go test -v ./pkg/gitrestclient/...
# Must exit 0

cd task/controller && go test -v ./pkg/metrics/...
# Must exit 0

cd task/controller && make precommit
# Must exit 0

grep -n "gitrestclient\|GitRestCalls" CHANGELOG.md
# Must show Unreleased entry
```
</verification>
