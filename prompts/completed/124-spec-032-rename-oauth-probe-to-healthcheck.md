---
status: completed
spec: [032-rename-oauth-probe-to-healthcheck]
summary: 'Renamed executor oauth-probe pipeline to healthcheck: interface OAuthProbeRunner→HealthcheckRunner, HTTP route /oauth-probe-trigger→/healthcheck-trigger, env var OAUTH_PROBE_CRON_EXPRESSION→HEALTHCHECK_CRON_EXPRESSION, factory functions CreateOAuthProbeRunner/Cron→CreateHealthcheckRunner/Cron, task_type literal replaced with lib.TaskTypeHealthcheck.String(), mock regenerated as FakeHealthcheckRunner, old mock deleted, all test and doc references updated.'
container: agent-124-spec-032-rename-oauth-probe-to-healthcheck
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-14T12:43:20Z"
queued: "2026-05-14T12:55:25Z"
started: "2026-05-14T13:01:53Z"
completed: "2026-05-14T13:06:50Z"
branch: dark-factory/rename-oauth-probe-to-healthcheck
---

<summary>
- The executor's probe package, interface, struct, constructor, factory functions, and HTTP handler all drop the `OAuthProbe` name in favour of `Healthcheck`
- The probe publishes tasks with `task_type: healthcheck` (via `lib.TaskTypeHealthcheck.String()`) instead of the hard-coded string `"oauth-probe"`
- The HTTP trigger route changes from `/oauth-probe-trigger` to `/healthcheck-trigger`; the old route is gone and returns 404
- The CLI flag is `healthcheck-cron-expression`; the env var is `HEALTHCHECK_CRON_EXPRESSION`; the default cron expression `0 0 8 * * 1` is preserved unchanged
- The counterfeiter mock file is regenerated under the `FakeHealthcheckRunner` name; the old `fake_o_auth_probe_runner.go` file is deleted
- `lib.TaskTypeHealthcheck` constant is added to `lib/agent_task-type.go` if not already present (spec 031 may have shipped it first); `TaskTypeOAuthProbe` is NOT removed
- The UUIDv5 namespace constant (`00000000-0000-0000-0000-000000000024`) is unchanged so in-flight probe tasks self-heal on the next tick
- `grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/` returns zero matches after this change (CHANGELOG excluded)
</summary>

<objective>
Rename the executor's `oauth-probe` concept to `healthcheck` across Go code, HTTP routes, env vars, and mock files. The probe verifies liveness (binary boots, CLI present, auth valid, plugins loaded) — not OAuth specifically. The rename makes the intent clear and unblocks moving the agent fleet off OAuth. Hard cut at deploy time: no redirect, no legacy env alias, no acceptance of the old task_type value by the renamed publisher.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-factory-pattern.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `go-http-handler-refactoring-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/`
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/`

**Key files to read in full before editing:**
- `lib/agent_task-type.go` — contains `TaskType` named type, existing constants including `TaskTypeOAuthProbe`; `TaskTypeHealthcheck` must exist here
- `lib/agent_task-type_test.go` — existing test entries for constants; update for `TaskTypeHealthcheck`
- `task/executor/pkg/probe/probe.go` — interfaces + implementations to rename; `task_type` literal to replace
- `task/executor/pkg/probe/probe_test.go` — test references to rename
- `task/executor/pkg/handler/oauth_probe_trigger_handler.go` — handler function to rename
- `task/executor/pkg/handler/oauth_probe_trigger_handler_test.go` — test references to rename
- `task/executor/pkg/factory/factory.go` — two factory functions to rename
- `task/executor/main.go` — config field, env var, local vars, HTTP route to rename
- `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go` — `oauth-probe` test data to replace

**lib module is local (replace directive):** `task/executor/go.mod` has `replace github.com/bborbe/agent/lib => ../../lib`. Local edits to `lib/` are immediately visible to executor builds — no version bump needed.

**Inline reference — current `OAuthProbeRunner` counterfeiter annotation in `probe.go`:**
```go
//counterfeiter:generate -o mocks/fake_o_auth_probe_runner.go --fake-name FakeOAuthProbeRunner . OAuthProbeRunner
```
Change to:
```go
//counterfeiter:generate -o mocks/fake_healthcheck_runner.go --fake-name FakeHealthcheckRunner . HealthcheckRunner
```

**Inline reference — current `task_type` literal in `probe.go` Run method:**
```go
"task_type": "oauth-probe",
```
Change to:
```go
"task_type": lib.TaskTypeHealthcheck.String(),
```

**Inline reference — frozen UUIDv5 namespace constant (DO NOT CHANGE):**
```go
var probeNamespace = uuid.MustParse("00000000-0000-0000-0000-000000000024")
```

**Symbol verification:**
```bash
# Check if TaskTypeHealthcheck already exists (from spec 031)
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go

# Confirm TaskTypeOAuthProbe still present
grep -n "TaskTypeOAuthProbe" lib/agent_task-type.go

# Confirm local replace directive is active
grep "replace" task/executor/go.mod | grep lib
```
</context>

<requirements>

## 1. Add `lib.TaskTypeHealthcheck` constant to `lib/agent_task-type.go` (if absent)

**Check first:**
```bash
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```

**If the grep returns a match:** the constant already exists (spec 031 shipped). Skip to step 3. Do NOT duplicate it.

**If the grep returns no match:** add the constant and update the deprecated GoDoc.

Read the full `lib/agent_task-type.go` file before editing.

In the `const` block, add the following line immediately after the `TaskTypeOAuthProbe` constant:
```go
// TaskTypeHealthcheck is the task type for agent liveness health-check jobs.
TaskTypeHealthcheck TaskType = "healthcheck"
```

Also update the GoDoc on `TaskTypeOAuthProbe` to drop the "once introduced" qualifier:
```go
// TaskTypeOAuthProbe is the task type for OAuth probe health-check jobs.
//
// Deprecated: use TaskTypeHealthcheck.
TaskTypeOAuthProbe TaskType = "oauth-probe"
```

Verify:
```bash
grep -n "TaskTypeHealthcheck\|TaskTypeOAuthProbe" lib/agent_task-type.go
```
Expected: both constants present.

## 2. Update `lib/agent_task-type_test.go` (if lib was changed in step 1)

**Only perform this step if step 1 made a change** (i.e. you added `TaskTypeHealthcheck`).

Read the full `lib/agent_task-type_test.go` file. Find the `DescribeTable("valid values", ...)` block. The table already has an entry for `lib.TaskTypeOAuthProbe`. Add a new entry immediately after it:

```go
Entry("healthcheck constant", lib.TaskTypeHealthcheck),
```

Verify:
```bash
grep -n "TaskTypeHealthcheck\|TaskTypeOAuthProbe" lib/agent_task-type_test.go
```
Expected: both entries present.

## 3. Run `lib` precommit (only if lib was changed)

**Only perform this step if steps 1–2 made changes.**

```bash
cd lib && make precommit
```

Must exit 0. If any linter fails, run only the failing target (e.g. `make lint`, `make errcheck`) and fix before retrying.

## 4. Update `task/executor/pkg/probe/probe.go`

Read the full file before editing. Make these changes:

**a. Update counterfeiter annotation:**
```go
// OLD:
//counterfeiter:generate -o mocks/fake_o_auth_probe_runner.go --fake-name FakeOAuthProbeRunner . OAuthProbeRunner

// NEW:
//counterfeiter:generate -o mocks/fake_healthcheck_runner.go --fake-name FakeHealthcheckRunner . HealthcheckRunner
```

**b. Rename the interface:**
```go
// OLD:
// OAuthProbeRunner executes one probe tick: publishes create-task + update-frontmatter per Config CR.
type OAuthProbeRunner interface {
    Run(ctx context.Context) error
}

// NEW:
// HealthcheckRunner executes one liveness check tick: publishes create-task + update-frontmatter per Config CR.
type HealthcheckRunner interface {
    Run(ctx context.Context) error
}
```

**c. Rename the struct and constructor:**
```go
// OLD:
type oAuthProbeRunner struct { ... }

func NewOAuthProbeRunner(
    configProvider ConfigProvider,
    publisher CommandPublisher,
) OAuthProbeRunner {
    return &oAuthProbeRunner{ ... }
}

// NEW:
type healthcheckRunner struct { ... }

func NewHealthcheckRunner(
    configProvider ConfigProvider,
    publisher CommandPublisher,
) HealthcheckRunner {
    return &healthcheckRunner{ ... }
}
```

**d. Update the method receiver on `Run`:**
```go
// OLD:
func (r *oAuthProbeRunner) Run(ctx context.Context) error {

// NEW:
func (r *healthcheckRunner) Run(ctx context.Context) error {
```

**e. Replace the `task_type` literal with the typed constant:**
In the `Run` method, find:
```go
"task_type": "oauth-probe",
```
Replace with:
```go
"task_type": lib.TaskTypeHealthcheck.String(),
```

**f. Keep `probeNamespace` and `probeTaskID` unchanged.** The UUIDv5 namespace value `00000000-0000-0000-0000-000000000024` is frozen by spec constraint.

Verify after all changes:
```bash
grep -n "OAuthProbe\|oauth-probe" task/executor/pkg/probe/probe.go
```
Expected: zero matches.

```bash
grep -n "HealthcheckRunner\|healthcheckRunner\|TaskTypeHealthcheck" task/executor/pkg/probe/probe.go
```
Expected: multiple matches.

## 5. Update `task/executor/pkg/probe/probe_test.go`

Read the full file before editing. Replace all `OAuthProbeRunner` references with `HealthcheckRunner` and `NewOAuthProbeRunner` with `NewHealthcheckRunner`:

- `Describe("OAuthProbeRunner"` → `Describe("HealthcheckRunner"`
- `runner probe.OAuthProbeRunner` → `runner probe.HealthcheckRunner`
- `runner = probe.NewOAuthProbeRunner(configProvider, publisher)` → `runner = probe.NewHealthcheckRunner(configProvider, publisher)`

Verify:
```bash
grep -n "OAuthProbeRunner\|NewOAuthProbeRunner" task/executor/pkg/probe/probe_test.go
```
Expected: zero matches.

## 6. Rename and update the HTTP handler file

**a. Rename the file:**
```bash
cd task/executor && git mv pkg/handler/oauth_probe_trigger_handler.go pkg/handler/healthcheck_trigger_handler.go
```

**b. Edit the renamed file** (`pkg/handler/healthcheck_trigger_handler.go`). Update:
- GoDoc: `NewOAuthProbeTriggerHandler` → `NewHealthcheckTriggerHandler`; "OAuth probe runner" → "healthcheck runner"
- Function signature:
  ```go
  // OLD:
  func NewOAuthProbeTriggerHandler(ctx context.Context, runner probe.OAuthProbeRunner) http.Handler {

  // NEW:
  func NewHealthcheckTriggerHandler(ctx context.Context, runner probe.HealthcheckRunner) http.Handler {
  ```
- Body is unchanged: `return libhttp.NewBackgroundRunHandler(ctx, runner.Run)`

Verify:
```bash
grep -n "OAuthProbe\|oauth.probe" task/executor/pkg/handler/healthcheck_trigger_handler.go
```
Expected: zero matches.

## 7. Rename and update the HTTP handler test file

**a. Rename the file:**
```bash
cd task/executor && git mv pkg/handler/oauth_probe_trigger_handler_test.go pkg/handler/healthcheck_trigger_handler_test.go
```

**b. Edit the renamed file** (`pkg/handler/healthcheck_trigger_handler_test.go`). Update all references:
- `Describe("OAuthProbeTriggerHandler"` → `Describe("HealthcheckTriggerHandler"`
- `fakeRunner *mocks.FakeOAuthProbeRunner` → `fakeRunner *mocks.FakeHealthcheckRunner`
- `fakeRunner = new(mocks.FakeOAuthProbeRunner)` → `fakeRunner = new(mocks.FakeHealthcheckRunner)`
- `h = handler.NewOAuthProbeTriggerHandler(ctx, fakeRunner)` → `h = handler.NewHealthcheckTriggerHandler(ctx, fakeRunner)`
- Leave all HTTP request URLs (`/oauth-probe/trigger`) unchanged — the handler does not validate the URL path; these are just test request paths and do not affect the route registration in `main.go`

Verify:
```bash
grep -n "OAuthProbe\|NewOAuthProbe" task/executor/pkg/handler/healthcheck_trigger_handler_test.go
```
Expected: zero matches.

## 8. Update `task/executor/pkg/factory/factory.go`

Read the full file before editing. Make these changes:

**a. Rename `CreateOAuthProbeRunner` → `CreateHealthcheckRunner`:**
```go
// OLD:
// CreateOAuthProbeRunner creates the OAuth probe runner shared between the cron path and the
// HTTP trigger path. Callers must pass the same instance to both CreateOAuthProbeCron and
// the HTTP handler so probe behavior is identical regardless of invocation path.
func CreateOAuthProbeRunner(
    configProvider pkg.EventHandlerConfig,
    syncProducer libkafka.SyncProducer,
    branch base.Branch,
) probe.OAuthProbeRunner {
    sender := cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory)
    publisher := probe.NewCommandPublisher(sender)
    return probe.NewOAuthProbeRunner(configProvider, publisher)
}

// NEW:
// CreateHealthcheckRunner creates the healthcheck runner shared between the cron path and the
// HTTP trigger path. Callers must pass the same instance to both CreateHealthcheckCron and
// the HTTP handler so probe behavior is identical regardless of invocation path.
func CreateHealthcheckRunner(
    configProvider pkg.EventHandlerConfig,
    syncProducer libkafka.SyncProducer,
    branch base.Branch,
) probe.HealthcheckRunner {
    sender := cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory)
    publisher := probe.NewCommandPublisher(sender)
    return probe.NewHealthcheckRunner(configProvider, publisher)
}
```

**b. Rename `CreateOAuthProbeCron` → `CreateHealthcheckCron`:**
```go
// OLD:
// CreateOAuthProbeCron wraps the given runner in a cron scheduler. Pass the runner returned by
// CreateOAuthProbeRunner so the cron and the HTTP handler share the same instance.
func CreateOAuthProbeCron(
    expression libcron.Expression,
    runner probe.OAuthProbeRunner,
) run.Runnable {
    return libcron.NewExpressionCron(expression, runner)
}

// NEW:
// CreateHealthcheckCron wraps the given runner in a cron scheduler. Pass the runner returned by
// CreateHealthcheckRunner so the cron and the HTTP handler share the same instance.
func CreateHealthcheckCron(
    expression libcron.Expression,
    runner probe.HealthcheckRunner,
) run.Runnable {
    return libcron.NewExpressionCron(expression, runner)
}
```

Verify:
```bash
grep -n "OAuthProbe\|oauth.probe" task/executor/pkg/factory/factory.go
```
Expected: zero matches.

## 9. Update `task/executor/main.go`

Read the full file before editing. Make these changes:

**a. Rename the config struct field:**
```go
// OLD:
OAuthProbeCronExpression string `                 arg:"oauth-probe-cron-expression" env:"OAUTH_PROBE_CRON_EXPRESSION" usage:"Cron expression for Claude OAuth health probes"                            default:"0 0 8 * * 1"`

// NEW:
HealthcheckCronExpression string `                 arg:"healthcheck-cron-expression" env:"HEALTHCHECK_CRON_EXPRESSION" usage:"Cron expression for agent liveness health checks"                          default:"0 0 8 * * 1"`
```
The default value `0 0 8 * * 1` is preserved unchanged.

**b. Rename local variables and factory calls in `Run()`:**
```go
// OLD:
oAuthProbeRunner := factory.CreateOAuthProbeRunner(
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
probeCron := factory.CreateOAuthProbeCron(
    libcron.Expression(a.OAuthProbeCronExpression),
    oAuthProbeRunner,
)

// NEW:
healthcheckRunner := factory.CreateHealthcheckRunner(
    eventHandlerConfig,
    syncProducer,
    a.Branch,
)
healthcheckCron := factory.CreateHealthcheckCron(
    libcron.Expression(a.HealthcheckCronExpression),
    healthcheckRunner,
)
```

**c. Update the `createHTTPServer` call and `service.Run` wiring:**
```go
// In service.Run, update:
a.createHTTPServer(eventHandlerConfig, oAuthProbeRunner),
probeCron.Run,

// To:
a.createHTTPServer(eventHandlerConfig, healthcheckRunner),
healthcheckCron.Run,
```

**d. Update `createHTTPServer` signature and body:**
```go
// OLD:
func (a *application) createHTTPServer(
    configProvider pkg.EventHandlerConfig,
    runner probe.OAuthProbeRunner,
) run.Func {
    ...
    router.Path("/oauth-probe-trigger").Handler(
        handler.NewOAuthProbeTriggerHandler(ctx, runner),
    )
    ...
}

// NEW:
func (a *application) createHTTPServer(
    configProvider pkg.EventHandlerConfig,
    runner probe.HealthcheckRunner,
) run.Func {
    ...
    router.Path("/healthcheck-trigger").Handler(
        handler.NewHealthcheckTriggerHandler(ctx, runner),
    )
    ...
}
```

Verify:
```bash
grep -n "OAuthProbe\|oauth.probe\|OAUTH_PROBE" task/executor/main.go
```
Expected: zero matches.

```bash
grep -n "healthcheck-cron-expression\|HEALTHCHECK_CRON_EXPRESSION\|healthcheck-trigger" task/executor/main.go
```
Expected: three matches.

## 10. Update `task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go`

Read the file to understand the context, then replace every occurrence of `"oauth-probe"` with `"healthcheck"` in this file. These are test data strings used to exercise CRD TaskTypes field validation — the specific value does not matter as long as it is a valid task type format (lowercase alphanumeric with dashes), and `"healthcheck"` satisfies this.

**Replace all occurrences** — use `replace_all: true` with Edit tool for the string `"oauth-probe"` → `"healthcheck"` in this file.

Also update the string literal `"taskTypes":["pr-review","oauth-probe"]` → `"taskTypes":["pr-review","healthcheck"]`.

Verify:
```bash
grep -n "oauth-probe" task/executor/k8s/apis/agent.benjamin-borbe.de/v1/types_test.go
```
Expected: zero matches.

## 10b. Clean up remaining `oauth-probe` references in tests and docs

The final acceptance-criteria grep (step 14) covers all of `task/executor/` (excluding `CHANGELOG.md`). The following files contain `oauth-probe` references that previous steps did NOT touch. Each must be updated.

**`task/executor/README.md` — 2 URL references**

Replace every occurrence of `oauth-probe-trigger` with `healthcheck-trigger` (URLs only). The README explains how operators trigger the probe; the new route name reflects the rename.

```bash
grep -n "oauth-probe" task/executor/README.md
```
Expected after edit: zero matches.

**`task/executor/pkg/result_publisher_test.go` — 2 fixture references**

Read the file. The two occurrences (around lines 188 and 209) use `"oauth-probe"` as a test-fixture string asserting the type-mismatch failure message contains an unknown task type. The specific value is arbitrary for this test — replace `"oauth-probe"` with `"healthcheck"` in both spots. Tests still pass because they assert the SHAPE of the mismatch message, not the specific task-type string.

```bash
grep -n "oauth-probe" task/executor/pkg/result_publisher_test.go
```
Expected after edit: zero matches.

**`task/executor/pkg/task_type_filter_test.go` — 8 fixture references**

Read the file. These fixtures test `EffectiveTaskTypes` and `TaskTypeInSet` helpers using `"oauth-probe"` as an arbitrary valid task-type value. Replace every occurrence of `"oauth-probe"` with `"healthcheck"` in this file. Test semantics are unchanged — the helpers are task-type-agnostic.

Use `Edit` with `replace_all: true` for the string `"oauth-probe"` → `"healthcheck"`.

```bash
grep -n "oauth-probe" task/executor/pkg/task_type_filter_test.go
```
Expected after edit: zero matches.

**`task/executor/pkg/handler/task_event_handler_test.go` — 5 fixture references**

Read the file. The fixtures at lines ~840, 851, 869, 880, 898 use `TaskTypes: []string{"oauth-probe"}` and `"task_type": "oauth-probe"` in test setups. Replace every occurrence of `"oauth-probe"` with `"healthcheck"` in this file.

Use `Edit` with `replace_all: true` for the string `"oauth-probe"` → `"healthcheck"`.

```bash
grep -n "oauth-probe" task/executor/pkg/handler/task_event_handler_test.go
```
Expected after edit: zero matches.

**`.update-logs/` directory**

This is a local build-log directory containing prior tool output. It is gitignored / build-artifact-only and not part of the source tree. The final grep (step 14) MUST exclude it. The `task/executor/` parent is in scope but `.update-logs/` is noise.

Verify the directory exists; if present, no edit needed — the grep in step 14 already excludes it via `--exclude-dir=.update-logs`.

## 11. Run `make generate` to regenerate counterfeiter mocks

```bash
cd task/executor && make generate
```

This processes the updated `//counterfeiter:generate` annotation in `probe.go` and writes `task/executor/pkg/probe/mocks/fake_healthcheck_runner.go` with type `FakeHealthcheckRunner`.

Verify the new mock file exists:
```bash
ls task/executor/pkg/probe/mocks/
```
Expected: `fake_healthcheck_runner.go` present (plus the unchanged `fake_config_provider.go` and `fake_command_publisher.go`).

**Delete the stale old mock file:**
```bash
cd task/executor && git rm pkg/probe/mocks/fake_o_auth_probe_runner.go
```

Verify:
```bash
ls task/executor/pkg/probe/mocks/
```
Expected: `fake_o_auth_probe_runner.go` absent.

## 12. Build check

```bash
cd task/executor && go build ./...
```

Fix compile errors before proceeding. Common issues:
- Any remaining reference to `FakeOAuthProbeRunner` in test files — check `pkg/handler/healthcheck_trigger_handler_test.go`
- Any remaining reference to `probe.OAuthProbeRunner` — check `factory.go`, `main.go`, handler test
- Import still needed: `probe` package import in `main.go` is still required for `probe.HealthcheckRunner`

## 13. Run iterative tests

```bash
cd task/executor && go test ./...
```

Expected: exit 0. Fix any failures before proceeding.

Coverage check for the probe package:
```bash
cd task/executor && go test -coverprofile=/tmp/probe-cover.out ./pkg/probe/... && go tool cover -func=/tmp/probe-cover.out | grep "total:"
```
Expected: ≥80% total coverage.

## 14. Acceptance criteria verification

```bash
# Zero oauth-probe occurrences in executor (excluding CHANGELOG)
grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/ --exclude=CHANGELOG.md --exclude-dir=.update-logs
```
Expected: zero matches.

```bash
# Zero oauth-probe occurrences in k8s manifests dir
grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/k8s/ --exclude-dir=.update-logs
```
Expected: zero matches.

```bash
# TaskTypeHealthcheck constant present
grep -n 'TaskTypeHealthcheck' lib/agent_task-type.go
```
Expected: at least one match.

```bash
# TaskTypeOAuthProbe constant still present (NOT removed — deferred per spec)
grep -n 'TaskTypeOAuthProbe' lib/agent_task-type.go
```
Expected: at least one match.

```bash
# Renamed publisher uses typed constant, not string literal
grep -n 'TaskTypeHealthcheck.String()' task/executor/pkg/probe/probe.go
```
Expected: one match.

```bash
# UUIDv5 namespace is unchanged
grep -n '00000000-0000-0000-0000-000000000024' task/executor/pkg/probe/probe.go
```
Expected: one match.

## 15. Update `CHANGELOG.md` at repo root

Check for existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```

If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add two bullets (both required by the spec ACs):

```markdown
- BREAKING(task/executor): rename oauth-probe probe pipeline to healthcheck — HTTP route `/oauth-probe-trigger` → `/healthcheck-trigger` (404 on old path after deploy); env var `OAUTH_PROBE_CRON_EXPRESSION` → `HEALTHCHECK_CRON_EXPRESSION` (default `0 0 8 * * 1` unchanged); factory `CreateOAuthProbeRunner`/`CreateOAuthProbeCron` → `CreateHealthcheckRunner`/`CreateHealthcheckCron`; interface `OAuthProbeRunner` → `HealthcheckRunner`; published task_type changes from `oauth-probe` to `healthcheck`; in-flight probe tasks with stale frontmatter self-heal on next cron tick via same UUIDv5 task identifier
- chore(lib): `TaskTypeOAuthProbe` constant intentionally retained in `lib/agent_task-type.go` for trading/maintainer consumers — removal deferred until their dispatch specs ship
```

Verify:
```bash
grep -A 3 "^## Unreleased" CHANGELOG.md
```
Expected: both bullets present.

## 16. Run final precommit in `task/executor/`

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run only the failing target (`make lint`, `make gosec`, `make errcheck`) and fix before retrying. Do NOT re-run full `make precommit` until all individual targets pass.

If lib was changed in steps 1–3:
```bash
cd lib && make precommit
```
Must also exit 0.

</requirements>

<constraints>
- **Dependency: spec 031 must merge before this PR is merged** — its `lib.TaskTypeHealthcheck` constant must exist at the SHA this PR targets. This prompt adds it if absent so the YOLO container can compile; the merge gate (AC grep check) applies to the PR level, not the container level.
- **Do NOT remove `TaskTypeOAuthProbe` from `lib/agent_task-type.go`** — trading and maintainer agents still consume `oauth-probe` task types until their own dispatch specs ship.
- **UUIDv5 namespace constant is frozen.** The value `00000000-0000-0000-0000-000000000024` in `probe.go` must not change. Same UUIDv5 → same vault path → in-flight probe tasks self-heal on next tick.
- **Default cron expression `0 0 8 * * 1` is preserved.** Only the env var and CLI flag names change.
- **No backward-compatibility shims.** No HTTP redirect from `/oauth-probe-trigger`, no env alias for `OAUTH_PROBE_CRON_EXPRESSION`, no acceptance of `task_type: oauth-probe` by the renamed publisher.
- **File renames use `git mv`.** Handler and handler test files are renamed with `git mv` so git tracks the rename.
- **`make generate` regenerates mocks; old mock file is deleted with `git rm`.** The file `mocks/fake_o_auth_probe_runner.go` must not exist after this prompt completes.
- **The `task_type` literal in `probe.go` uses `lib.TaskTypeHealthcheck.String()`, not the string `"healthcheck"` directly.**
- **`ConfigProvider` and `CommandPublisher` interfaces are unchanged** — only `OAuthProbeRunner` is renamed to `HealthcheckRunner`.
- **Factory functions have zero business logic** — `CreateHealthcheckRunner` and `CreateHealthcheckCron` are pure composition, same as their predecessors.
- **`probeNamespace` and `probeTaskID` keep their existing names** — they are not `OAuthProbe`-prefixed and do not need renaming.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`, never bare `context.Background()` in pkg/.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Precondition: `TaskTypeHealthcheck` exists in lib:
```bash
grep -n "TaskTypeHealthcheck" lib/agent_task-type.go
```
Expected: at least one match.

Precondition: `TaskTypeOAuthProbe` still present in lib:
```bash
grep -n "TaskTypeOAuthProbe" lib/agent_task-type.go
```
Expected: at least one match.

Zero `oauth-probe`/`OAuthProbe`/`OAUTH_PROBE` in executor (CHANGELOG excluded):
```bash
grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/ --exclude=CHANGELOG.md
```
Expected: zero matches.

Zero occurrences in k8s dir:
```bash
grep -ri 'oauth-probe\|OAuthProbe\|OAUTH_PROBE' task/executor/k8s/
```
Expected: zero matches.

New route is present in main.go:
```bash
grep -n "healthcheck-trigger" task/executor/main.go
```
Expected: one match.

New env var and flag present in main.go:
```bash
grep -n "HEALTHCHECK_CRON_EXPRESSION\|healthcheck-cron-expression" task/executor/main.go
```
Expected: two matches.

Default cron expression preserved:
```bash
grep -n '0 0 8 \* \* 1' task/executor/main.go
```
Expected: one match.

UUIDv5 namespace unchanged:
```bash
grep -n '00000000-0000-0000-0000-000000000024' task/executor/pkg/probe/probe.go
```
Expected: one match.

Published task_type uses typed constant:
```bash
grep -n 'TaskTypeHealthcheck.String()' task/executor/pkg/probe/probe.go
```
Expected: one match.

New mock file exists; old file deleted:
```bash
ls task/executor/pkg/probe/mocks/
```
Expected: `fake_healthcheck_runner.go` present, `fake_o_auth_probe_runner.go` absent.

CHANGELOG updated:
```bash
grep -A 5 "^## Unreleased" CHANGELOG.md | grep -i "healthcheck"
```
Expected: at least one match.

Run all executor tests:
```bash
cd task/executor && go test ./...
```
Expected: exit 0.

Run precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
