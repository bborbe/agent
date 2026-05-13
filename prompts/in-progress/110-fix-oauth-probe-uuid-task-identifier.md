---
status: committing
summary: Replaced non-UUID `probe-<agent>` task identifiers with deterministic UUIDv5s using `uuid.NewSHA1(probeNamespace, []byte(agentName))`, added four new boundary-contract tests, promoted `github.com/google/uuid` to a direct dependency, documented the UUID contract in `docs/task-service-design.md`, and updated `CHANGELOG.md`.
container: agent-110-fix-oauth-probe-uuid-task-identifier
dark-factory-version: v0.156.1-1-g04f3863-dirty
created: "2026-05-13T19:00:00Z"
queued: "2026-05-13T19:02:51Z"
started: "2026-05-13T19:02:52Z"
---

<summary>
- The weekly OAuth probe loop (shipped in v0.61.4 via spec 024) writes a non-UUID task_identifier (`probe-<agent>`)
- The vault scanner enforces that task_identifier MUST be a valid UUID and silently rewrites non-UUID values with a fresh random UUID on every scan cycle
- The rewrite races with the probe loop's two follow-up writes per tick, producing merge conflicts in the vault and breaking the update-frontmatter re-trigger (it looks up by task_identifier which no longer matches)
- Fix: derive a deterministic UUIDv5 per agent so the task_identifier passes the scanner's UUID check, the value is stable across deploys and ticks, and update-frontmatter lookups continue to resolve
- The file path keeps using the human-readable title (`tasks/probe-<agent>.md`) so operators can still identify which PVC a probe corresponds to without UUID translation
- Document the implicit contract: agent code that publishes create-task commands MUST use UUID task_identifiers
</summary>

<objective>
Replace the non-UUID `probe-<agent>` task identifier in the OAuth probe publisher with a deterministic UUIDv5 derived from the agent name. After this fix, weekly probe ticks no longer trigger the vault scanner's UUID rewrite, no longer produce merge conflicts in the vault, and the update-frontmatter re-trigger resolves to the correct file.
</objective>

<context>
Read `CLAUDE.md` at the repo root for project conventions.

Read these guides before starting:
- `~/.claude/plugins/marketplaces/coding/docs/go-patterns.md` — interface → constructor → struct, counterfeiter annotations
- `~/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — `bborbe/errors`, never `fmt.Errorf`
- `~/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega, external test packages, errcheck on bare `It`-block errors
- `~/.claude/plugins/marketplaces/dark-factory/docs/prompt-writing.md` — "Add imports before tidying" — write the import first, then run go mod tidy

**Key files to read in full before editing:**

- `task/executor/pkg/probe/probe.go` — the runner package shipped in v0.61.4; lines 99-129 (the `Run` method) need a single-line change at task ID construction
- `task/executor/pkg/probe/probe_test.go` — Ginkgo test suite; the four AC cases + the two boundary-validation cases must remain green
- `task/controller/pkg/scanner/vault_scanner.go` lines 234-246 — the enforcement code (do NOT modify; this prompt only adjusts the publisher to comply)
- `task/controller/pkg/scanner/vault_scanner.go` lines 305-320 — `isValidUUID` and `uuid.New().String()` patterns; the project standard for UUID handling

**Inline reference — the current task ID construction in `probe.go` (around line 100-102):**

```go
for _, config := range configs {
    agentName := config.Spec.Assignee
    taskID := lib.TaskIdentifier("probe-" + agentName)  // ← non-UUID, scanner rewrites this

    createCmd := taskcmd.CreateCommand{
        TaskIdentifier: taskID,
        Title:          "probe-" + agentName,  // ← title stays as-is; file path unchanged
        ...
    }
```

**Inline reference — the vault scanner's enforcement at `task/controller/pkg/scanner/vault_scanner.go:239-242`:**

```go
if !isValidUUID(taskID) {
    glog.Warningf("replacing non-UUID task_identifier %q in %s", taskID, relPath)
    return v.injectAndStore(ctx, removeTaskIdentifier(content), relPath, currentFMAssignee)
}
```

This is the line that breaks the probe on every scan cycle. Do NOT change this; the contract is the scanner's, and the publisher must honor it.

**Inline reference — `github.com/google/uuid` API (already in `task/controller`'s import block):**

```go
// uuid.NewSHA1(namespace UUID, data []byte) → UUIDv5: deterministic, stable across processes
//   Same inputs always produce the same UUID. SHA1 of namespace ++ data, then UUID-format encoded.
// uuid.MustParse("00000000-0000-0000-0000-000000000024") → UUID — panics on bad input; safe for a constant.
```

`github.com/google/uuid v1.6.0` is currently `// indirect` in `task/executor/go.mod`. Adding a direct import in `probe.go` and running `go mod tidy` will promote it.

**Why a UUIDv5 (not v4) here:** v5 is deterministic — same agent name always produces the same UUID, so the probe loop's first tick on a fresh deploy and its 100th tick a month later both reference the same vault file. v4 (random) would create a new vault file every executor restart, accumulating probe-task junk indefinitely.
</context>

<requirements>

## 1. Add a deterministic-UUID helper to `task/executor/pkg/probe/probe.go`

Add the new import (alphabetical with the other third-party imports):

```go
"github.com/google/uuid"
```

Define a package-level namespace constant — the value is arbitrary but MUST be stable across deploys (changing it would orphan all existing probe vault files):

```go
// probeNamespace is the UUIDv5 namespace for OAuth probe task identifiers.
// Stable per spec 024 follow-up — do NOT change without a migration plan.
var probeNamespace = uuid.MustParse("00000000-0000-0000-0000-000000000024")
```

Place this above the `Run` method, after the type declarations.

Add a small helper just below the namespace:

```go
// probeTaskID returns the deterministic UUIDv5 for a probe task targeting agentName.
// Same agentName always yields the same UUID, both within a single executor
// process and across restarts.
func probeTaskID(agentName string) lib.TaskIdentifier {
    return lib.TaskIdentifier(uuid.NewSHA1(probeNamespace, []byte(agentName)).String())
}
```

## 2. Replace the task ID construction in `Run`

In `(r *oAuthProbeRunner) Run`, replace the single line:

```go
// before:
taskID := lib.TaskIdentifier("probe-" + agentName)
// after:
taskID := probeTaskID(agentName)
```

**Do NOT change** the `Title` field — it must stay `"probe-" + agentName` so the vault file lands at the human-readable path `tasks/probe-<agent>.md`. Per spec-019, Title controls path resolution and TaskIdentifier controls idempotency / lookup — those are two separate concerns.

The rest of the `Run` method is unchanged.

## 3. Update `task/executor/pkg/probe/probe_test.go`

Two changes:

**a. Boundary-validation tests (`Context "emitted commands satisfy library validation"`):** these already exist and continue to pass — `lib.TaskIdentifier.Validate` only checks non-empty, so a UUID string passes the same way `probe-<agent>` did. No change here.

**b. Add a new `Context` to assert the deterministic-UUID property:**

```go
Context("task IDs are deterministic UUIDv5s per agent (boundary contract)", func() {
    BeforeEach(func() {
        configProvider.GetReturns([]agentv1.Config{
            {
                ObjectMeta: metav1.ObjectMeta{Name: "agent-a"},
                Spec:       agentv1.ConfigSpec{Assignee: "agent-a"},
            },
            {
                ObjectMeta: metav1.ObjectMeta{Name: "agent-b"},
                Spec:       agentv1.ConfigSpec{Assignee: "agent-b"},
            },
        }, nil)
    })

    It("create-task task_identifier is a valid UUID string", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(0)
        createCmd, ok := payload.(taskcmd.CreateCommand)
        Expect(ok).To(BeTrue())
        _, err := uuid.Parse(string(createCmd.TaskIdentifier))
        Expect(err).NotTo(HaveOccurred(), "task_identifier %q must parse as a UUID", createCmd.TaskIdentifier)
    })

    It("repeated invocations produce identical task IDs per agent", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        Expect(runner.Run(ctx)).To(Succeed())

        _, _, agentACreate1 := publisher.PublishArgsForCall(0)  // first run, agent-a create
        _, _, agentACreate2 := publisher.PublishArgsForCall(4)  // second run, agent-a create (after 4 calls in first run)

        cmd1 := agentACreate1.(taskcmd.CreateCommand)
        cmd2 := agentACreate2.(taskcmd.CreateCommand)
        Expect(cmd1.TaskIdentifier).To(Equal(cmd2.TaskIdentifier))
    })

    It("different agents produce different task IDs", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, agentACreate := publisher.PublishArgsForCall(0)
        _, _, agentBCreate := publisher.PublishArgsForCall(2)
        Expect(agentACreate.(taskcmd.CreateCommand).TaskIdentifier).
            NotTo(Equal(agentBCreate.(taskcmd.CreateCommand).TaskIdentifier))
    })

    It("title still uses the human-readable form", func() {
        Expect(runner.Run(ctx)).To(Succeed())
        _, _, payload := publisher.PublishArgsForCall(0)
        createCmd := payload.(taskcmd.CreateCommand)
        Expect(createCmd.Title).To(Equal("probe-agent-a"))
    })
})
```

Add the import `"github.com/google/uuid"` to the test file's import block (alphabetical with other third-party imports).

**Note on the indexing assumption** (`PublishArgsForCall(4)` for the second run's first agent): each `Run` invocation produces 2 publishes per Config × 2 Configs = 4 publishes. The second run continues counting from index 4. If counterfeiter resets between invocations in any future change, adjust accordingly — but counterfeiter's default behavior accumulates call args across invocations.

## 4. Add `github.com/google/uuid` as a direct dependency

```bash
cd task/executor && go mod tidy
```

This promotes the `// indirect` entry to direct now that `probe.go` imports it. Verify:

```bash
grep "github.com/google/uuid" task/executor/go.mod
```

Expected: no `// indirect` comment on that line.

## 5. Document the UUID contract

Add a short note to `docs/task-service-design.md` so the implicit scanner contract becomes explicit.

Find the "Two Responsibilities" section (around line 14). After it (or in a new subsection at the end of the document), add:

```markdown
## Task Identifier Contract

`task_identifier` MUST be a UUID. The vault scanner (`task/controller/pkg/scanner/vault_scanner.go`) enforces this: any task file whose frontmatter contains a non-UUID `task_identifier` is rewritten with a freshly generated UUID on the next scan cycle, breaking any caller that depends on a deterministic identifier.

Publishers (executor probe loop, agents, manual operators) MUST therefore construct UUID task identifiers. For deterministic-per-agent identifiers, prefer `uuid.NewSHA1(namespace, []byte(agentName))` over `uuid.New()` so the value is stable across process restarts and re-deploys.
```

Place this at the end of the document (under a new `## Task Identifier Contract` heading, after the existing sections).

## 6. Update `CHANGELOG.md`

Under `## Unreleased` (at repo root), add:

```markdown
- fix(task/executor): OAuth probe task identifiers are now deterministic UUIDv5s per agent (previously `probe-<agent>` literal strings, which the vault scanner silently rewrote with random UUIDs on each scan — producing merge conflicts and breaking `update-frontmatter` re-triggers). Probe vault files remain at the human-readable path `tasks/probe-<agent>.md` (driven by Title, not by task_identifier).
```

## 7. Run tests

```bash
cd task/executor && make test
```

All existing tests + the four new tests in step 3b must pass.

## 8. Run final precommit

```bash
cd task/executor && make precommit
```

Must exit 0.

</requirements>

<constraints>
- Change is confined to `task/executor/pkg/probe/probe.go`, its test file, `task/executor/go.mod`, `task/executor/go.sum`, `docs/task-service-design.md`, and root `CHANGELOG.md`. No other files modified.
- The `Title` field on `CreateCommand` MUST remain `"probe-" + agentName` — operators identify probe vault files by name, not by UUID. Spec-019 path resolution uses Title; changing it would break the human-readable file paths.
- The `probeNamespace` UUID value is arbitrary but stable. Once shipped, NEVER change it — it would orphan every existing probe vault file (under a different UUIDv5). The current chosen value `00000000-0000-0000-0000-000000000024` references spec 024 for traceability.
- `uuid.NewSHA1` is the v5 constructor (despite the function name's "SHA1") — it produces deterministic UUIDs from a namespace + name. Do NOT substitute `uuid.New()` (random v4) — that would defeat the point.
- The vault scanner enforces UUID format; do NOT modify the scanner to accept non-UUIDs. The publisher must comply with the contract, not the other way around.
- Counterfeiter mocks are NOT regenerated by this prompt (no interface signature changes). `make generate` will be a no-op for `pkg/probe/`.
- Tests use Ginkgo v2 + Gomega + counterfeiter mocks per project convention. External test package `probe_test`. Per `go-testing-guide.md` "Critical Rules", never call an error-returning function bare in an `It` block — wrap with `Expect(...).To(Succeed())` or `Expect(...).To(HaveOccurred())`.
- Error wrapping uses `github.com/bborbe/errors`. Never `fmt.Errorf`. Never bare `context.Background()` in pkg/ code.
- A bullet under `## Unreleased` in root `CHANGELOG.md` is required.
- Do NOT commit — dark-factory handles git.
- `cd task/executor && make precommit` must exit 0.
</constraints>

<verification>

Verify `github.com/google/uuid` is now a direct dependency of `task/executor`:

```bash
grep "github.com/google/uuid" task/executor/go.mod
```
Expected: a direct entry (no `// indirect` comment).

Verify `probeNamespace` and `probeTaskID` exist in the probe package:

```bash
grep -n "probeNamespace\|probeTaskID" task/executor/pkg/probe/probe.go
```
Expected: definitions for both, plus the call site inside `Run`.

Verify the publisher emits a parseable UUID:

```bash
cd task/executor && go test ./pkg/probe/... -run TestProbe -count=1
```
Expected: all specs pass, including the new "task IDs are deterministic UUIDv5s per agent" context.

Verify Title is unchanged:

```bash
grep -n 'Title:.*"probe-"' task/executor/pkg/probe/probe.go
```
Expected: one match, `Title: "probe-" + agentName,`.

Verify the contract is documented:

```bash
grep -n "task_identifier MUST be a UUID\|Task Identifier Contract" docs/task-service-design.md
```
Expected: at least one match.

Verify CHANGELOG updated:

```bash
grep -n -A1 "deterministic UUIDv5\|OAuth probe task identifiers" CHANGELOG.md | head -5
```
Expected: at least one match under `## Unreleased`.

Run precommit:

```bash
cd task/executor && make precommit
```
Expected: exit 0.

</verification>
