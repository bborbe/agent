---
status: completed
summary: Raised the interactive HTTP server's write deadline to 10 minutes via a serverOptionFns closure wired into libhttp.NewServer, with an export_test.go bridge, a Ginkgo spec that fails against the 30s library default, and a changelog entry.
execution_id: agent-write-timeout-exec-231-interactive-write-timeout
dark-factory-version: v0.196.0
created: "2026-10-06T18:47:10Z"
queued: "2026-10-06T19:25:12Z"
started: "2026-10-06T19:26:22Z"
completed: "2026-10-06T19:33:37Z"
---

# Raise the interactive server's write deadline to 10 minutes

<summary>
- A long agent turn on the interactive HTTP surface can now deliver its answer instead of having it cut off mid-flight
- The surface's server-wide response-write deadline is raised from the library's 30-second default to 10 minutes
- The deadline is raised once for the whole server, because the server is built from a single options value — there is no per-route setting to raise
- The three routes that hold a request open for the length of a turn are the ones that benefit
- The readiness and metrics probes keep the same longer cap and are unaffected by it, because their handlers return immediately
- Nothing else changes: no route, handler, middleware, auth rule, read deadline, read-header deadline, idle timeout or shutdown timeout is touched
- A test locks the new deadline and fails against the 30-second default, so a later edit cannot quietly revert the fix
- The frozen contract document is deliberately not touched — this is a standalone change, not a spec
- A changelog entry records the fix
</summary>

<objective>
Raise the interactive service's HTTP write deadline from the `github.com/bborbe/http` default of 30 seconds to 10 minutes, so a `POST /prompt`, `POST /a2a` or `/permission` request whose agent turn runs longer than 30 seconds can still write its response.

Why: Go sets the write deadline when the request headers are read. A handler that runs past that deadline reaches a deadline that has already passed and can no longer write its response — the turn completes server-side and the answer is silently lost, surfacing to the caller as `502 Bad Gateway`. Measured against the deployed `claude-interactive` service in nuke dev: 2.8 s → 200; 27.9 s → 200 (6918 bytes); 32.5 s, 39.5 s, 44.9 s, 48.1 s, 51.2 s and 104.8 s → all 502.
</objective>

<context>
Read `CLAUDE.md` at the repo root first — it states the test conventions (Ginkgo v2/Gomega, external `*_test` packages, Counterfeiter mocks) and that `make precommit` is the gate.

Read before editing:

- `interactive/service.go` — `Run(ctx context.Context) error` builds the server with `runServer := libhttp.NewServer(s.listen, s.Handler())` and no option functions, so the library default `WriteTimeout: 30 * time.Second` applies. `Handler()` registers exactly six routes: `/readiness`, `/metrics`, `/prompt`, `/.well-known/agent-card.json` (`a2asrv.WellKnownAgentCardPath`), `/a2a` (`a2aPath` in `interactive/a2a-handler.go`) and `/permission` (only when a permission registry was supplied).
- `interactive/service_test.go` — the style to follow for a new spec: `package interactive_test`, Ginkgo `Describe`/`It`, Gomega matchers, and the shared suite bootstrap in `interactive/interactive_suite_test.go` (which already sets `suiteConfig.Timeout = 60 * time.Second` — one more reason the spec must not sleep).
- `docs/interactive-service.md` — READ ONLY. It declares itself the frozen contract and states it does not change without a spec. This change is a standalone prompt, not a spec, so that document stays untouched. It documents only the readiness dial timeout (5 s), never a server write deadline, so nothing in it contradicts this change.
- `.dark-factory.yaml` — `testCommand: make test`, `validationPrompt: docs/dod.md`.

This is the only server construction site in the repository: `libhttp.NewServer` appears exactly once (`interactive/service.go`), and `Run` is the only `Run(ctx)` method in the `interactive` package. No other entry point needs the same change, and no other package sets a read, write, idle or shutdown timeout today.

Library facts, verified against the pinned `github.com/bborbe/http@v1.26.26` (`go.mod` pins that version; the module source is readable in the container at `/home/node/go/pkg/mod/github.com/bborbe/http@v1.26.26/http_server.go`):

```go
// ServerOptions configures HTTP server behavior.
type ServerOptions struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration // default 30 * time.Second
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	TLSConfig         *tls.Config
	CertFile          string
	KeyFile           string
}

func CreateServerOptions(optionFns ...func(serverOptions *ServerOptions)) ServerOptions
func CreateHTTPServer(addr string, router http.Handler, serverOptions ServerOptions) *http.Server
func NewServer(addr string, router http.Handler, optionFns ...func(serverOptions *ServerOptions)) run.Func
```

`CreateServerOptions` defaults are `ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`, `WriteTimeout: 30s`, `IdleTimeout: 60s`, `ShutdownTimeout: 5s`, `MaxHeaderBytes: 1 << 20`. `CreateHTTPServer` copies `ServerOptions.WriteTimeout` onto the `net/http` `Server`'s `WriteTimeout` field, which is what `Run` ultimately serves with. There is no `WithWriteTimeout` server helper — the option closure is the mechanism.

The option-closure shape is existing house style. This is the same library and the same closure shape used by a sibling service (shown here inline because sibling repos are not mounted in the container):

```go
return libhttp.NewServer(
	a.Listen,
	factory.CreateMetricsMiddleware(m, router),
	func(o *libhttp.ServerOptions) {
		o.ReadTimeout = 60 * time.Second
		o.WriteTimeout = 60 * time.Second
		o.IdleTimeout = 120 * time.Second
	},
).Run(ctx)
```

The `export_test.go` bridge is also existing house style in these repos (again inlined — sibling repos are not mounted): an unexported symbol is re-exported to the external test package from a `_test.go` file that is compiled only into test builds, e.g.

```go
// Copyright (c) 2025 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ops

// SessionTurnTimeout exposes the unexported sessionTurnTimeout constant to the
// external ops_test package so tests can assert the duration StartSession hands to
// its waiter against the real value, rather than against a copied literal. Test-only:
// this file is a _test.go file and is not part of the package's public API.
const SessionTurnTimeout = sessionTurnTimeout
```

Read these coding guides before editing (in-container paths; if one is absent, continue with the others):
`/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`, `/home/node/.claude/plugins/marketplaces/coding/docs/go-http-service-guide.md`, `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`.
</context>

<requirements>
1. **`interactive/service.go` — add the write deadline.** Add `"time"` to the import block (currently `context` and `net/http` only). Add this unexported helper next to `Run`:

   ```go
   // serverOptionFns returns the option closures the interactive HTTP server is built
   // with. The write deadline lives here rather than inline in Run so the external test
   // package can assert it through github.com/bborbe/http without a request that has to
   // outlive the library's 30-second default.
   func serverOptionFns() []func(*libhttp.ServerOptions) {
   	return []func(*libhttp.ServerOptions){
   		func(o *libhttp.ServerOptions) {
   			o.WriteTimeout = 10 * time.Minute
   		},
   	}
   }
   ```

   `Run` changes by exactly one argument:

   ```go
   // before
   runServer := libhttp.NewServer(s.listen, s.Handler())
   // after
   runServer := libhttp.NewServer(s.listen, s.Handler(), serverOptionFns()...)
   ```

2. **`interactive/service.go` — the route-policy comment, at the `NewServer` call.** Immediately above the `runServer := libhttp.NewServer(...)` line inside `Run`, add a comment that states the policy explicitly. It must say all of:

   - One server-wide write deadline governs all six routes this service registers — `/readiness`, `/metrics`, `/prompt`, `/.well-known/agent-card.json`, `/a2a` and `/permission` — because `libhttp.NewServer` builds a single `http.Server` from a single `libhttp.ServerOptions` value: there is no per-route deadline, so raising it for one route raises it for every route and no route can keep the 30-second default while another is raised.
   - What the 10 minutes buy: the three turn-holding routes — `POST /prompt`, `POST /a2a` and `GET`/`POST /permission` — each run one agent turn whose provider answers in tens of seconds. Go sets the write deadline when the request headers are read, so a handler that outlives the deadline reaches a deadline that has already passed and cannot write its response; the turn completes server-side and the answer is silently lost. Quote the measured evidence: 27.9 s → 200, 32.5 s and above → 502 against the deployed `claude-interactive` service.
   - Why `/readiness` and `/metrics` are acceptable at the longer cap: both handlers return immediately, so the longer deadline can only bite a client that stops reading a response the handler has already produced.

   Use the wording above as the substance; keep every comment line at most 100 characters (the repo formats with `golines --max-len=100`). Do not move the existing `Run` doc comment and do not restate this policy on `Handler()`.

3. **`interactive/export_test.go` — new file, the test bridge.** Create it with the repo's standard license header (copy the three-line BSD header from `interactive/service.go`) and:

   ```go
   package interactive

   // ServerOptionFns exposes the unexported serverOptionFns to the external
   // interactive_test package, so a spec can assert the server-wide write deadline
   // through github.com/bborbe/http. Test-only: this file is a _test.go file, is
   // compiled only into test builds, and adds nothing to the package's public API.
   var ServerOptionFns = serverOptionFns
   ```

   Do not export anything from `interactive/service.go` — the public API of this package stays exactly as it is today.

4. **`interactive/server-options_test.go` — new file, the spec.** Create it as an external `package interactive_test` Ginkgo spec (the suite bootstrap already exists in `interactive/interactive_suite_test.go`; do not add a second `RunSpecs`). It must build the real `net/http` server from the option closures `Run` passes and assert the deadline that lands on it:

   ```go
   package interactive_test

   import (
   	"net/http"
   	"time"

   	libhttp "github.com/bborbe/http"
   	. "github.com/onsi/ginkgo/v2"
   	. "github.com/onsi/gomega"

   	"github.com/bborbe/agent/interactive"
   )

   var _ = Describe("server write deadline", func() {
   	It("raises the write deadline to ten minutes, above the library default", func() {
   		server := libhttp.CreateHTTPServer(
   			":0",
   			http.NotFoundHandler(),
   			libhttp.CreateServerOptions(interactive.ServerOptionFns()...),
   		)

   		// The duration is asserted as a literal on purpose: an assertion against an
   		// exported alias would move with the constant and lock nothing.
   		Expect(server.WriteTimeout).To(Equal(10 * time.Minute))
   		Expect(server.WriteTimeout).To(BeNumerically(">", 30*time.Second))
   	})
   })
   ```

   This is the boundary test: it drives the option closures through the same library functions `Run` uses (`CreateServerOptions` → `CreateHTTPServer` → the `net/http` `Server.WriteTimeout` field), so it fails if the closure's **value** is wrong. It does not by itself prove the closure reached `Run`'s call site — the wiring that hands `serverOptionFns()` to `NewServer` is pinned separately by the `<verification>` grep. A test that asserts the shape of the closure, or the value of a constant, does not satisfy this requirement. The spec must not sleep: the suite timeout is 60 seconds, and a behavioural spec that distinguishes 30 s from 10 min would have to outlive the 30-second default.

5. **Prove the spec is load-bearing (red before green).** With the change in place and green, temporarily set the closure's value to the library default — `o.WriteTimeout = 30 * time.Second` — and run the focused spec:

   ```bash
   cd /workspace
   set -o pipefail
   go test -v -mod=mod -race ./interactive/ -ginkgo.focus='write deadline' -ginkgo.v 2>&1 | tee /tmp/red.log | tail -40
   ```

   Substituting the default value is the compiling form of "the deadline was never raised": deleting the closure outright would leave the `time` import unused and the package would not build, so the red proof would be a compile error rather than a failed assertion. The run must FAIL, and the failure output must show `30000000000` where `600000000000` was expected — Gomega renders `time.Duration` as raw nanoseconds (`Expected <time.Duration>: 30000000000 / to equal <time.Duration>: 600000000000`), never as `30s` / `10m0s`, because `format.UseStringerRepresentation` is false and `time.Duration` has reflect kind `Int64`. The red log must also contain a `1 Failed` line, so a red run that failed to *compile* is not mistaken for a red run that fired the assertion. Then restore `10 * time.Minute`, re-run the same command to `/tmp/green.log`, and confirm it passes. Quote the red failure line in your completion summary. Leave `/tmp/red.log` and `/tmp/green.log` in place — the verification step reads them.

6. **`CHANGELOG.md` — one bullet.** Add a `## Unreleased` section directly above the newest `## v` heading (there is no `## Unreleased` section today) and put one `fix:` bullet under it, stating that the `interactive` service's HTTP write deadline is raised to 10 minutes so a `POST /prompt`, `POST /a2a` or `/permission` turn that outlives the 30-second `github.com/bborbe/http` default can still write its answer instead of losing it to an already-expired deadline. Do not add a version heading and do not touch the `# Changelog` preamble.

7. Run `ROOTDIR=/workspace make test` (this repo's test command) and confirm every package passes, then run the full gate `ROOTDIR=/workspace make precommit`.

8. **Self-check before finishing.** Re-run `<verification>` end to end and confirm every command behaves as annotated. Then walk each `<constraints>` bullet against your diff and confirm it holds.
</requirements>

<constraints>
- Do NOT edit `docs/interactive-service.md`. That file declares itself the frozen contract and states it does not change without a spec; this change is a standalone prompt, not a spec, so the frozen contract is out of scope.
- Do NOT change any route, handler, middleware or auth behaviour. The ONLY behavioural change is the write deadline.
- Do NOT change the read timeout, read-header timeout, idle timeout or shutdown timeout. `ReadTimeout`, `ReadHeaderTimeout`, `IdleTimeout` and `ShutdownTimeout` must keep their `libhttp.CreateServerOptions` defaults.
- Keep the change minimal: one option closure, one unexported seam for the test, one `_test.go` bridge, their comments, and the changelog bullet. Do NOT add a constructor parameter, a config field, a flag, an environment variable, a metric or a per-route deadline — the frozen contract's constructor signatures stay byte-identical.
- Do not touch any package other than `interactive/` and `CHANGELOG.md`.
- Tests are Ginkgo v2 / Gomega in an external `*_test` package (repo convention). Do not add a second Ginkgo suite bootstrap.
- No `fmt.Errorf`; no `context.Background()` in non-test code; no `print`/`fmt.Printf` debug output.
- Existing tests must still pass.
- Do NOT commit — dark-factory handles git.
</constraints>

<verification>
Run every command in ONE shell from `/workspace`. `git` does not work in this container, so `ROOTDIR=/workspace` must be passed on every `make` call.

```bash
cd /workspace
set -o pipefail   # a `go test | tee | tail` pipeline otherwise reports only tail's exit status

# red proof captured in step 5 (deadline substituted with the library default).
# gomega renders time.Duration as raw nanoseconds, never as 30s/10m0s; the `1 Failed`
# clause is required so a red run that failed to COMPILE is not read as one that fired
# the assertion.
grep -q '30000000000' /tmp/red.log && grep -q '1 Failed' /tmp/red.log && echo RED-CONFIRMED

# focused spec, green — -v is REQUIRED: without it `go test` discards a passing package's
# stdout and the ginkgo summary never reaches the log. Do NOT anchor on \b: ginkgo colours
# the summary and `m` sits immediately before the 0, so no word boundary exists.
go test -v -mod=mod -race ./interactive/ -ginkgo.focus='write deadline' -ginkgo.v 2>&1 | tee /tmp/green.log | tail -40
# "Ran 1 of 1" first: a focus string that matches nothing ALSO prints "0 Failed" and exits 0,
# so the spec count is what proves a spec actually ran.
grep -E 'Ran 1 of 1' /tmp/green.log || echo "ASSERTION FAILED: no spec ran — the focus matched nothing"
grep -E '0 Failed' /tmp/green.log   || echo "ASSERTION FAILED: the focused spec did not pass"

# production change shape. Every line echoes on failure: this block's exit code is not read
# by the daemon, so a silent `[ ]` failure is indistinguishable from a pass.
[ "$(grep -c 'o.WriteTimeout = 10 \* time.Minute' interactive/service.go)" = 1 ] \
  || echo "ASSERTION FAILED: the write-deadline closure is missing or duplicated"
[ "$(grep -c 'libhttp.NewServer(s.listen, s.Handler(), serverOptionFns()...)' interactive/service.go)" = 1 ] \
  || echo "ASSERTION FAILED: the options are not wired into NewServer — this is the only guard on that wiring"
! grep -q 'NewServer(s.listen, s.Handler())' interactive/service.go \
  || echo "ASSERTION FAILED: an option-less NewServer call is still present"
[ "$(grep -c 'var ServerOptionFns = serverOptionFns' interactive/export_test.go)" = 1 ] \
  || echo "ASSERTION FAILED: the export_test.go bridge is missing or duplicated"
[ "$(grep -c 'interactive.ServerOptionFns()' interactive/server-options_test.go)" = 1 ] \
  || echo "ASSERTION FAILED: the spec does not call the exported closure"

# nothing else moved: the closure sets no other deadline, and the frozen contract is untouched
! grep -nE 'o\.(ReadTimeout|ReadHeaderTimeout|IdleTimeout|ShutdownTimeout)' interactive/service.go \
  || echo "ASSERTION FAILED: the closure sets a deadline other than the write deadline"
! grep -q 'WriteTimeout' docs/interactive-service.md \
  || echo "ASSERTION FAILED: the frozen contract was edited"

# changelog
awk '/^## /{print "first section: "$0; exit}' CHANGELOG.md     # must print "first section: ## Unreleased"

# full gate
ROOTDIR=/workspace make precommit
```

`ROOTDIR=/workspace make precommit` must end with `ready to commit`.
</verification>
