---
status: draft
spec: [033-per-stage-probe-task-identity]
created: "2026-05-14T13:10:00Z"
branch: dark-factory/per-stage-probe-task-identity
---

<summary>
- Dev and prod executor clusters now publish independent probe tasks — no shared vault path, no shared task identifier
- Probe vault file name changes from `probe-{agent}.md` to `probe-{agent}-{stage}.md` (e.g. `probe-claude-agent-dev.md`)
- Probe task identifier (UUIDv5) is now keyed on `(agent_name, stage)` — the dev and prod probes for the same agent produce different UUIDs
- Published frontmatter gains a `stage:` field whose value equals the executor's `Branch` argument verbatim
- Every probe cycle publishes `phase: in_progress` (was `planning`) so the executor picks up the probe regardless of the prior cycle's terminal state
- `NewOAuthProbeRunner` accepts a new `branch base.Branch` parameter; the factory wires it through automatically
- The executor's task event handler, stage filter, status filter, and type filter are unchanged
- CHANGELOG documents the behavior change and the one-time operator cleanup step for pre-existing stage-less probe files
</summary>

<objective>
Make the probe runner publish per-stage task files and identifiers so that the dev and prod executor clusters maintain fully independent probes that each pass the executor's existing stage filter at `task_event_handler.go:150`. Today both clusters write to the same shared vault path and identifier, so each cluster's stage filter drops the other's probe — leaving both pushgateways with zero `agent_job_*` rows.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `go-patterns.md` in `~/.claude/plugins/marketplaces/coding/docs/` — interface → constructor → struct, error wrapping
- `go-testing-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — Ginkgo v2/Gomega, external test packages, coverage ≥80%
- `go-error-wrapping-guide.md` in `~/.claude/plugins/marketplaces/coding/docs/` — never `fmt.Errorf`, always `errors.Wrapf`
- `test-pyramid-triggers.md` in `~/.claude/plugins/marketplaces/coding/docs/` — which test types to write for each code change

**Key files to read in full before editing:**
- `task/executor/pkg/probe/probe.go` — the file being changed; contains `OAuthProbeRunner`, `oAuthProbeRunner`, `NewOAuthProbeRunner`, `probeTaskID`, `probeNamespace`, and `Run`
- `task/executor/pkg/probe/probe_test.go` — the test file being updated; pay attention to all existing assertions that reference title format or task identity
- `task/executor/pkg/factory/factory.go` — `CreateOAuthProbeRunner` on line ~114; passes `branch` to `cdb.NewCommandObjectSender` but not yet to `probe.NewOAuthProbeRunner`
- `task/executor/pkg/handler/task_event_handler.go` — read to confirm you are NOT modifying it; stage filter is at line ~150

**Inline reference — current `NewOAuthProbeRunner` signature (will change):**
```go
func NewOAuthProbeRunner(
    configProvider ConfigProvider,
    publisher      CommandPublisher,
) OAuthProbeRunner
```

**Inline reference — new `NewOAuthProbeRunner` signature:**
```go
func NewOAuthProbeRunner(
    configProvider ConfigProvider,
    publisher      CommandPublisher,
    branch         base.Branch,
) OAuthProbeRunner
```

**Inline reference — current `probeTaskID` (will change):**
```go
func probeTaskID(agentName string) lib.TaskIdentifier {
    return lib.TaskIdentifier(uuid.NewSHA1(probeNamespace, []byte(agentName)).String())
}
```

**Inline reference — new `probeTaskID`:**
```go
func probeTaskID(agentName, stage string) lib.TaskIdentifier {
    return lib.TaskIdentifier(uuid.NewSHA1(probeNamespace, []byte(agentName+"-"+stage)).String())
}
```

**Inline reference — current `createCmd` in `Run` (will change):**
```go
createCmd := taskcmd.CreateCommand{
    TaskIdentifier: taskID,
    Title:          "probe-" + agentName,
    Frontmatter: lib.TaskFrontmatter{
        "task_type": "oauth-probe",
        "status":    "in_progress",
        "phase":     "planning",
        "assignee":  agentName,
    },
    Body: "reply 'ok'",
}
```

**Inline reference — new `createCmd`:**
```go
createCmd := taskcmd.CreateCommand{
    TaskIdentifier: taskID,
    Title:          "probe-" + agentName + "-" + string(r.branch),
    Frontmatter: lib.TaskFrontmatter{
        "task_type": "oauth-probe",
        "status":    "in_progress",
        "phase":     "in_progress",
        "assignee":  agentName,
        "stage":     string(r.branch),
    },
    Body: "reply 'ok'",
}
```

**Inline reference — current `updateCmd` in `Run` (phase field changes):**
```go
updateCmd := taskcmd.UpdateFrontmatterCommand{
    TaskIdentifier: taskID,
    Updates: lib.TaskFrontmatter{
        "phase":         "planning",
        "trigger_count": 0,
        "retry_count":   0,
    },
}
```

**Inline reference — new `updateCmd`:**
```go
updateCmd := taskcmd.UpdateFrontmatterCommand{
    TaskIdentifier: taskID,
    Updates: lib.TaskFrontmatter{
        "phase":         "in_progress",
        "trigger_count": 0,
        "retry_count":   0,
    },
}
```

**Inline reference — factory change in `CreateOAuthProbeRunner`:**
```go
// before:
return probe.NewOAuthProbeRunner(configProvider, publisher)

// after:
return probe.NewOAuthProbeRunner(configProvider, publisher, branch)
```

**Symbol verification — run before writing:**
```bash
# Confirm probeNamespace and probeTaskID locations
grep -n "probeNamespace\|probeTaskID\|NewOAuthProbeRunner" task/executor/pkg/probe/probe.go

# Confirm Branch type
grep -n "type Branch\|Branch " $(go env GOPATH)/pkg/mod/github.com/bborbe/cqrs*/base/*.go 2>/dev/null | head -5
# Fallback if mod cache unavailable — check existing usage in factory:
grep -n "base.Branch" task/executor/pkg/factory/factory.go

# Confirm factory call site
grep -n "NewOAuthProbeRunner" task/executor/pkg/factory/factory.go

# Confirm task_event_handler is NOT touched (read the file, do not edit it)
grep -n "stage\|branch\|Branch" task/executor/pkg/handler/task_event_handler.go | head -10
```
</context>

<requirements>

## 1. Update `task/executor/pkg/probe/probe.go`

Read the full file before editing.

### 1a. Add `base.Branch` import

Add `"github.com/bborbe/cqrs/base"` to the import block. Preserve the existing import grouping style.

### 1b. Update `probeNamespace` comment

The existing comment already satisfies the spec ("do NOT change without a migration plan"). Preserve it verbatim — do NOT rewrite it.

### 1c. Change `probeTaskID` to accept both `agentName` and `stage`

Replace:
```go
func probeTaskID(agentName string) lib.TaskIdentifier {
    return lib.TaskIdentifier(uuid.NewSHA1(probeNamespace, []byte(agentName)).String())
}
```
With:
```go
func probeTaskID(agentName, stage string) lib.TaskIdentifier {
    return lib.TaskIdentifier(uuid.NewSHA1(probeNamespace, []byte(agentName+"-"+stage)).String())
}
```

This ensures `(claude-agent, dev)` and `(claude-agent, prod)` produce different UUIDs while remaining deterministic across restarts.

### 1d. Add `branch base.Branch` field to `oAuthProbeRunner` struct

```go
type oAuthProbeRunner struct {
    configProvider ConfigProvider
    publisher      CommandPublisher
    branch         base.Branch
}
```

### 1e. Update `NewOAuthProbeRunner` to accept and store `branch`

Replace:
```go
func NewOAuthProbeRunner(
    configProvider ConfigProvider,
    publisher CommandPublisher,
) OAuthProbeRunner {
    return &oAuthProbeRunner{
        configProvider: configProvider,
        publisher:      publisher,
    }
}
```
With:
```go
func NewOAuthProbeRunner(
    configProvider ConfigProvider,
    publisher      CommandPublisher,
    branch         base.Branch,
) OAuthProbeRunner {
    return &oAuthProbeRunner{
        configProvider: configProvider,
        publisher:      publisher,
        branch:         branch,
    }
}
```

### 1f. Update `Run` method to use per-stage identity

In `Run`, change:
```go
taskID := probeTaskID(agentName)
```
to:
```go
taskID := probeTaskID(agentName, string(r.branch))
```

Replace the `createCmd` literal with the new version (per inline reference in `<context>`):
- Title: `"probe-" + agentName + "-" + string(r.branch)`
- `"phase"` changes from `"planning"` to `"in_progress"`
- Add `"stage": string(r.branch)` to the Frontmatter map

Replace the `updateCmd` literal:
- `"phase"` changes from `"planning"` to `"in_progress"`

Verify after editing:
```bash
grep -n "string(r.branch)\|in_progress\|stage" task/executor/pkg/probe/probe.go
```
Expected: `string(r.branch)` appears in taskID call, title, and stage frontmatter; `in_progress` appears twice (create and update); `stage` key appears in createCmd frontmatter.

Build check:
```bash
cd task/executor && go build ./pkg/probe/...
```
Expected: exit 0.

## 2. Update `task/executor/pkg/factory/factory.go`

Read the full file before editing.

In `CreateOAuthProbeRunner`, change the final `return` line from:
```go
return probe.NewOAuthProbeRunner(configProvider, publisher)
```
to:
```go
return probe.NewOAuthProbeRunner(configProvider, publisher, branch)
```

No other changes to the factory. `branch` is already an argument of `CreateOAuthProbeRunner` so no new parameter is needed.

Verify:
```bash
grep -n "NewOAuthProbeRunner" task/executor/pkg/factory/factory.go
```
Expected: one match showing `branch` as third argument.

Build check:
```bash
cd task/executor && go build ./...
```
Expected: exit 0.

## 3. Update `task/executor/pkg/probe/probe_test.go`

Read the full file before editing.

### 3a. Update `NewOAuthProbeRunner` calls to supply branch

In `BeforeEach`, the runner is created as:
```go
runner = probe.NewOAuthProbeRunner(configProvider, publisher)
```
Change to:
```go
runner = probe.NewOAuthProbeRunner(configProvider, publisher, "dev")
```

This establishes `"dev"` as the stage for all existing tests.

### 3b. Fix the title assertion

Find the test: `"title still uses the human-readable form"`. Update the expected value:
```go
// before:
Expect(createCmd.Title).To(Equal("probe-agent-a"))
// after:
Expect(createCmd.Title).To(Equal("probe-agent-a-dev"))
```

### 3c. Add new test contexts for per-stage behavior

After the existing `"task IDs are deterministic UUIDv5s per agent (boundary contract)"` context block, add a new `Context` block:

```go
Context("per-stage identity (boundary contract)", func() {
    BeforeEach(func() {
        configProvider.GetReturns([]agentv1.Config{
            {
                ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
                Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
            },
        }, nil)
    })

    It("title includes the stage suffix", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(0)
        createCmd, ok := payload.(taskcmd.CreateCommand)
        Expect(ok).To(BeTrue())
        Expect(createCmd.Title).To(Equal("probe-agent-a-dev"))
    })

    It("create-task frontmatter includes stage field matching the runner's branch", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(0)
        createCmd, ok := payload.(taskcmd.CreateCommand)
        Expect(ok).To(BeTrue())
        Expect(createCmd.Frontmatter).To(HaveKeyWithValue("stage", "dev"))
    })

    It("create-task frontmatter has phase in_progress", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(0)
        createCmd, ok := payload.(taskcmd.CreateCommand)
        Expect(ok).To(BeTrue())
        Expect(createCmd.Frontmatter).To(HaveKeyWithValue("phase", "in_progress"))
    })

    It("update-frontmatter has phase in_progress", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(1)
        updateCmd, ok := payload.(taskcmd.UpdateFrontmatterCommand)
        Expect(ok).To(BeTrue())
        Expect(updateCmd.Updates).To(HaveKeyWithValue("phase", "in_progress"))
    })

    It("dev and prod runners produce different task IDs for the same agent", func() {
        devRunner := probe.NewOAuthProbeRunner(configProvider, publisher, "dev")
        prodPublisher := new(mocks.FakeCommandPublisher)
        configProvider.GetReturns([]agentv1.Config{
            {
                ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
                Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
            },
        }, nil)
        prodRunner := probe.NewOAuthProbeRunner(configProvider, prodPublisher, "prod")

        Expect(devRunner.Run(ctx)).To(Succeed())
        Expect(prodRunner.Run(ctx)).To(Succeed())

        _, _, devPayload := publisher.PublishArgsForCall(0)
        _, _, prodPayload := prodPublisher.PublishArgsForCall(0)
        devCmd, okDev := devPayload.(taskcmd.CreateCommand)
        Expect(okDev).To(BeTrue())
        prodCmd, okProd := prodPayload.(taskcmd.CreateCommand)
        Expect(okProd).To(BeTrue())

        Expect(devCmd.TaskIdentifier).NotTo(Equal(prodCmd.TaskIdentifier),
            "dev and prod probes for the same agent must have different task identifiers")
    })
})
```

### 3d. Update the CommandPublisher real-implementation test

The test at line ~162 passes a `TaskIdentifier: "probe-agent-a"` to `publisher.Publish`. This is just a direct call to the publisher mock — it does not go through `NewOAuthProbeRunner`, so no change is needed there. Verify the test still passes after the runner constructor change.

Run iterative tests:
```bash
cd task/executor && go test ./pkg/probe/...
```
Fix any compile errors before continuing. Expected: exit 0.

Coverage check:
```bash
cd task/executor && go test -coverprofile=/tmp/probe-cover.out ./pkg/probe/... && \
  go tool cover -func=/tmp/probe-cover.out | grep -E "probe\.go|total"
```
Expected: `probe.go` coverage ≥80%.

## 4. Update `CHANGELOG.md` at repo root

Check for an existing `## Unreleased` section:
```bash
grep -n "^## Unreleased" CHANGELOG.md | head -3
```
If it exists, append to it. If not, insert a new `## Unreleased` section immediately above the first `## v` header.

Add these two bullets:
```markdown
- feat(task/executor): probe runner publishes per-stage vault files and task identifiers; `stage:` frontmatter field matches executor branch (spec 033)
- docs: operator cleanup step — after deploy, delete stale `tasks/probe-<agent>.md` files (no stage suffix) from the OpenClaw vault host clone: `git rm tasks/probe-*.md && git commit -m "remove stale shared probe files" && git push`
```

Verify:
```bash
grep -A 5 "^## Unreleased" CHANGELOG.md
```
Expected: both bullets present.

## 5. Run final precommit in `task/executor/`

```bash
cd task/executor && make precommit
```

Must exit 0. If any linter fails, run ONLY the failing target (e.g. `make lint`, `make gosec`, `make errcheck`) and fix before retrying. Do NOT re-run full `make precommit` until all individual targets pass.

</requirements>

<constraints>
- **The executor's `task_event_handler.go` is frozen.** Do not edit it. The stage filter, status filter, type filter, assignee filter, and phase filter logic at `pkg/handler/task_event_handler.go` must remain exactly as-is.
- **`probeNamespace` is frozen.** Do not change the UUID value `"00000000-0000-0000-0000-000000000024"`. Only the data input to `uuid.NewSHA1` changes (from `[]byte(agentName)` to `[]byte(agentName+"-"+stage)`).
- **No new CLI flags, env vars, HTTP routes, or cron expressions.** Stage is sourced from the executor's existing `branch` argument already wired through `CreateOAuthProbeRunner`.
- **Task type literal unchanged.** The `"task_type": "oauth-probe"` frontmatter value stays as-is. Spec 032 handles the rename to `"healthcheck"`.
- **`NewOAuthProbeRunner` signature change is the only public API change.** The `OAuthProbeRunner` interface, `ConfigProvider` interface, and `CommandPublisher` interface are unchanged.
- **`CreateOAuthProbeRunner` in factory.go is unchanged except for the `branch` pass-through.** The function signature, its callers in `main.go`, and all other factory functions remain the same.
- Error wrapping: `github.com/bborbe/errors` — never `fmt.Errorf`. Use `errors.Wrapf(ctx, err, "message")` for wrapping.
- Tests must use `"dev"` as the branch value in `NewOAuthProbeRunner` calls so assertions on title (e.g. `"probe-agent-a-dev"`) are deterministic.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass after the constructor change.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify probe.go changes:
```bash
# Branch field added to struct
grep -n "branch.*base.Branch\|base.Branch.*branch" task/executor/pkg/probe/probe.go

# probeTaskID accepts two args
grep -n "func probeTaskID\|probeTaskID(" task/executor/pkg/probe/probe.go

# Stage field in frontmatter
grep -n '"stage"' task/executor/pkg/probe/probe.go

# Phase changed to in_progress in both commands
grep -n '"phase"' task/executor/pkg/probe/probe.go
```
Expected: `branch base.Branch` present; `probeTaskID` takes two string params; `"stage"` key present; `"phase"` appears twice, both with value `"in_progress"`.

Verify factory wiring:
```bash
grep -n "NewOAuthProbeRunner" task/executor/pkg/factory/factory.go
```
Expected: one match showing `branch` as third argument.

Verify task_event_handler not touched:
```bash
git diff task/executor/pkg/handler/task_event_handler.go
```
Expected: empty (no changes).

Run all executor tests:
```bash
cd task/executor && go test ./...
```
Expected: exit 0.

Run probe coverage:
```bash
cd task/executor && go test -coverprofile=/tmp/probe-cover.out ./pkg/probe/... && \
  go tool cover -func=/tmp/probe-cover.out | grep "total:"
```
Expected: ≥80% total for the probe package.

Verify CHANGELOG bullets:
```bash
grep -A 8 "^## Unreleased" CHANGELOG.md
```
Expected: `feat(task/executor)` bullet and `docs:` operator cleanup bullet both present.

Run final precommit:
```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
