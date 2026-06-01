---
status: completed
spec: [043-surface-vault-scanner-skip-failures]
container: agent-vault-scanner-alerts-exec-195-spec-043-scanner-instrumentation
dark-factory-version: v0.173.0
created: "2026-06-01T20:00:00Z"
queued: "2026-06-01T20:36:10Z"
started: "2026-06-01T20:37:25Z"
completed: "2026-06-01T21:25:15Z"
branch: dark-factory/surface-vault-scanner-skip-failures
---

<summary>
- The vault scanner now increments a labelled Prometheus counter at every place it decides to skip a file, so an operator can answer "is the scanner currently skipping anything, and why?" from a dashboard alone
- Six skip sites are wired: a file that fails to read, three places where frontmatter parsing fails at different layers, an empty status field, and an injection failure for the task identifier
- Four of those six sites also bump their log level from warning to error so existing log-scrape alert routing picks up the operator-actionable broken-file events
- The two scanner constructors gain a metrics dependency; the single production call site in main.go and the test call sites pass through a real metrics instance
- Each skip reason gets its own Ginkgo test that confirms the counter ticks by one per cycle and by two across two cycles — by design, re-scans of an unrepaired broken file keep the rate positive
- One regression test pairs a broken file with a valid one to prove the scan still publishes the valid task and the broken file does not get short-circuited on the next pass
- The changelog gets a single line under the unreleased section naming the new counter, the incident it responds to, and the doctrine it advances
- The controller design doc gains a one-sentence operator-facing note in the scanner section about the counter and the rate-based alert signal
- A new test walks through every skip site and asserts the counter call sits next to every skipping log line in the same function body, locking in the invariant for future maintainers
</summary>

<objective>
Instrument every skip site in `task/controller/pkg/scanner/vault_scanner.go` with the new `SkippedFilesTotal` counter from prompt one, promote four operator-actionable log lines from warning to error, extend both scanner constructors to accept a `Metrics` dependency, update the production call site in `main.go` to pass the existing `metrics.New()` singleton, add the per-reason and regression tests, and finish with the changelog entry and the controller-design doc update.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-prometheus-metrics-guide.md` (Interface Patterns section for the dependency-injection pattern this prompt follows).

Key files to read in full before editing:
- `task/controller/pkg/scanner/vault_scanner.go` — the file to modify; read fully to anchor each skip site by its log-string prefix, not by line number
- `task/controller/pkg/scanner/vault_scanner_test.go` — read fully; five call sites use the existing constructors and need to pass metrics
- `task/controller/pkg/scanner/scanner_suite_test.go` — Ginkgo suite; no changes
- `task/controller/main.go` — the single production call site at line 100 that must pass the metrics singleton
- `task/controller/pkg/metrics/metrics.go` — already has the new `SkippedFilesTotal` method, the five reason constants, and the pre-initialised labels from prompt one
- `task/controller/pkg/sync/sync_loop.go` — uses `scanner.VaultScanner` through the interface; no changes needed (interface unchanged)
- `task/controller/mocks/vault_scanner.go` — counterfeiter mock for `VaultScanner`; unchanged (the public interface is not modified)
- `task/controller/mocks/metrics.go` — counterfeiter mock for `Metrics`; already includes the new method from prompt one

Reference docs (read but do not edit):
- `docs/controller-design.md` — append one sentence naming the new counter
- `CHANGELOG.md` — append one entry under the existing `## Unreleased` section (must be created if absent)

Spec being implemented: `specs/in-progress/043-surface-vault-scanner-skip-failures.md`. The exact skip-site list, the log-level promotion map, the constructor signature constraint, and all 13 acceptance criteria are spelled out in the spec.

Predecessor prompt: `spec-043-metrics-counter.md` (must run first and complete successfully — the `Metrics` interface method and the `SkippedFilesTotal` counter must exist before this prompt starts).

**Spec-internal note on log-level promotion:** the spec's Desired Behavior #4 text says "the three `glog.Warningf("skipping %s: invalid frontmatter: …")` call sites and the `glog.Warningf("skipping %s: status is empty")` call site are promoted" (4 sites), but its rationale paragraph also says "duplicate_frontmatter_invalid ... stays at [its] current log level". The two statements are inconsistent. The AC#4 evidence shape ("four lines all using `Errorf`; the count of `Warningf("skipping` matches in the file is zero") is the testable contract and requires all 4 `skipping` sites with the `invalid frontmatter` text or the `status is empty` text to be promoted to `Errorf`. This prompt follows the AC#4 contract and promotes all four. Document this resolution in the `## Improvements` section of the completion report.
</context>

<requirements>

1. **Add the metrics import to the scanner package**

   In `task/controller/pkg/scanner/vault_scanner.go`, add a new import alongside the existing block. The existing import group is:

   ```go
   lib "github.com/bborbe/agent/lib"
   gitclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"
   ```

   Add the metrics import to the project import group (alphabetical order is enforced by `goimports-reviser` — do NOT hand-place; just add the line anywhere in the project group):

   ```go
   "github.com/bborbe/agent/task/controller/pkg/metrics"
   ```

   Then run `make format` (or rely on `make precommit`) to enforce final ordering. The formatter will place `metrics` AFTER `gitrestclient` (it sorts by full path: `…/lib` < `…/gitrestclient` < `…/metrics`).

2. **Extend the scanner struct**

   Add a `metrics metrics.Metrics` field to the `vaultScanner` struct, immediately after the `trigger` field (so the field order is: `gitClient`, `taskDir`, `pollInterval`, `hashes`, `trigger`, `metrics`, `ops`):

   ```go
   type vaultScanner struct {
       gitClient    gitclient.GitClient
       taskDir      string
       pollInterval time.Duration
       hashes       map[string]fileEntry
       trigger      <-chan struct{}
       metrics      metrics.Metrics
       ops          fileOps
   }
   ```

   The type is the interface `metrics.Metrics` (the public one from the metrics package), not the concrete `*defaultMetrics`, so the constructor stays testable via injection.

3. **Update `NewVaultScanner`**

   Change the signature to add the new parameter, and assign it into the struct:

   ```go
   func NewVaultScanner(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       trigger <-chan struct{},
       metrics metrics.Metrics,
   ) VaultScanner {
       return &vaultScanner{
           gitClient:    gitClient,
           taskDir:      taskDir,
           pollInterval: pollInterval,
           hashes:       make(map[string]fileEntry),
           trigger:      trigger,
           metrics:      metrics,
           ops:          newLocalFileOps(gitClient.Path()),
       }
   }
   ```

   The parameter name `metrics` shadows the imported package name within the function body. This matches the existing constructor convention in this package (e.g., `gitClient` is a parameter named after the package, not a renamed variable). If the project's linter (`golangci-lint`) flags the shadow, rename the local parameter to `m` and reference it as `m` in the struct literal — but try the shadow first.

4. **Update `NewGitRestVaultScanner`**

   Same change applied to the other constructor:

   ```go
   func NewGitRestVaultScanner(
       gitClient gitclient.GitClient,
       taskDir string,
       pollInterval time.Duration,
       trigger <-chan struct{},
       metrics metrics.Metrics,
   ) VaultScanner {
       return &vaultScanner{
           gitClient:    gitClient,
           taskDir:      taskDir,
           pollInterval: pollInterval,
           hashes:       make(map[string]fileEntry),
           trigger:      trigger,
           metrics:      metrics,
           ops: fileOps{
               listFiles: gitClient.ListFiles,
               readFile:  gitClient.ReadFile,
               writeFile: gitClient.WriteFile,
           },
       }
   }
   ```

   The two constructors stay otherwise identical. Do NOT add a helper to deduplicate the struct construction — keeping them parallel is the project convention and the scanner file is small.

5. **Instrument the `read_failed` skip site**

   In `processFile`, locate the `failed to read %s` log line and add the counter increment immediately before the `return`:

   ```go
   content, readErr := v.ops.readFile(ctx, relPath)
   if readErr != nil {
       glog.Warningf("failed to read %s: %v", relPath, readErr)
       v.metrics.SkippedFilesTotal(metrics.ReasonReadFailed).Inc()
       return nil, "", false
   }
   ```

   The log level stays `Warningf` (spec Desired Behavior #4 — `read_failed` is rare; the counter alone is sufficient).

6. **Instrument the `invalid_frontmatter` skip site at `extractFrontmatter`**

   Locate the first `skipping %s: invalid frontmatter: %v` call in `processFile` (this is the one immediately after the `extractFrontmatter` call). Change the log level to `Errorf` and add the counter:

   ```go
   fmYAML, err := extractFrontmatter(ctx, content)
   if err != nil {
       glog.Errorf("skipping %s: invalid frontmatter: %v", relPath, err)
       v.metrics.SkippedFilesTotal(metrics.ReasonInvalidFrontmatter).Inc()
       return nil, "", false
   }
   ```

   Log level promotion is per spec Desired Behavior #4 (one of the four sites).

7. **Instrument the `duplicate_frontmatter_invalid` skip site at `DeduplicateFrontmatter`**

   Locate the second `skipping %s: invalid frontmatter: %v` call (this one is immediately after the `DeduplicateFrontmatter` call). Log level is promoted to `Errorf` (per AC#4 evidence shape — see the "Spec-internal note on log-level promotion" paragraph in `<context>`). Add the counter with the dedicated reason constant:

   ```go
   dedupedYAML, hasDuplicates, dedupErr := DeduplicateFrontmatter(ctx, fmYAML)
   if dedupErr != nil {
       glog.Errorf("skipping %s: invalid frontmatter: %v", relPath, dedupErr)
       v.metrics.SkippedFilesTotal(metrics.ReasonDuplicateFrontmatterInvalid).Inc()
       return nil, "", false
   }
   ```

8. **Instrument the `invalid_frontmatter` skip site at `yaml.Unmarshal`**

   Locate the third `skipping %s: invalid frontmatter: %v` call (this is the one immediately after the `yaml.Unmarshal` call). Log level promoted to `Errorf`; reason is the same `invalid_frontmatter` as the first site (they are the same upstream failure class from the operator's perspective):

   ```go
   var fmMap map[string]interface{}
   if err := yaml.Unmarshal([]byte(dedupedYAML), &fmMap); err != nil {
       glog.Errorf("skipping %s: invalid frontmatter: %v", relPath, err)
       v.metrics.SkippedFilesTotal(metrics.ReasonInvalidFrontmatter).Inc()
       return nil, "", false
   }
   ```

9. **Instrument the `empty_status` skip site**

   Locate the `skipping %s: invalid frontmatter: status is empty` log line. Log level promoted to `Errorf`:

   ```go
   if frontmatter.Status() == "" {
       glog.Errorf("skipping %s: invalid frontmatter: status is empty", relPath)
       v.metrics.SkippedFilesTotal(metrics.ReasonEmptyStatus).Inc()
       return nil, "", false
   }
   ```

   The exact log message string (with the `invalid frontmatter` prefix) is preserved — the spec only changes the level, not the text.

10. **Instrument the `inject_task_identifier_failed` skip site**

    In `injectAndStore`, locate the `skipping %s: failed to inject task_identifier: %v` log line. Log level stays `Warningf` (this is the one `skipping` site that is NOT promoted per AC#4 — only 4 of the 5 `skipping` sites are promoted). Add the counter:

    ```go
    id := uuid.New().String()
    newContent, injectErr := InjectTaskIdentifier(ctx, content, id)
    if injectErr != nil {
        glog.Warningf("skipping %s: failed to inject task_identifier: %v", relPath, injectErr)
        v.metrics.SkippedFilesTotal(metrics.ReasonInjectTaskIdentifierFailed).Inc()
        return nil, "", false
    }
    ```

11. **Update `task/controller/main.go`**

    In the `application.Run` function, the `scanner.NewGitRestVaultScanner` call at line 100 needs one new argument. The `metrics.New()` singleton already exists a few lines earlier (line 73, passed to `gitrestclient.NewGitRestClient`) — call `metrics.New()` again to construct a separate instance for the scanner (each consumer holds its own `Metrics` reference; the counter lives in the promauto default registry so all instances share the same counter state):

    ```go
    syncLoop := pkgsync.NewSyncLoop(
        scanner.NewGitRestVaultScanner(gitClient, a.TaskDir, a.PollInterval, trigger, metrics.New()),
        publisher.NewTaskPublisher(eventObjectSender, lib.TaskV1SchemaID, currentDateTime),
        trigger,
        metrics.New(),
    )
    ```

    The `metrics` package import is already present at the top of `main.go` (`"github.com/bborbe/agent/task/controller/pkg/metrics"`). No new import is needed.

12. **Update existing scanner test call sites**

    In `task/controller/pkg/scanner/vault_scanner_test.go`, five call sites pass through the new constructor signatures. They are:

    - `BeforeEach` (around line 173): `s = scanner.NewVaultScanner(fakeGit, taskDir, time.Second, make(chan struct{}))` — append `, metrics.New()` to the argument list.
    - `Describe("NewVaultScanner")` (line 484): `scanner.NewVaultScanner(fakeGit, taskDir, time.Hour, nil)` — same.
    - `Describe("NewGitRestVaultScanner")` (line 493): `scanner.NewGitRestVaultScanner(gitClient, taskDir, time.Hour, nil)` — same.
    - `Describe("Run")` first test (line 513): `scanner.NewVaultScanner(fakeGit, taskDir, time.Hour, nil)` — same.
    - `Describe("Run")` second test (line 529): `scanner.NewVaultScanner(fakeGit, taskDir, time.Hour, trigger)` — same.

    For each, add the import `"github.com/bborbe/agent/task/controller/pkg/metrics"` to the test file's import block (the existing test imports `github.com/bborbe/agent/task/controller/pkg/scanner` already, so the new import slots in next to it in alphabetical order) and append `, metrics.New()` as the fifth argument.

13. **Add the per-reason counter test contexts**

    In the same `vault_scanner_test.go` file, add a new **top-level** `var _ = Describe("SkippedFilesTotal counter", ...)` block. **Anchoring (critical):** `Describe("runCycle", ...)` is NESTED inside the outer `Describe("VaultScanner", ...)` block. The new block must NOT be inserted inside the `VaultScanner` Describe — it goes **between the closing `})` of the outer `VaultScanner` Describe and the existing `var _ = Describe("domain.NormalizeTaskPhase …` block**. If you see `})` `})` on consecutive lines followed by a blank line and then `var _ = Describe("domain.…`, that blank line is the insertion point. Inside, declare a local helper closure to read the current value of a labelled counter via `prometheus.DefaultGatherer.Gather()`:

    ```go
    counterValue := func(reason string) float64 {
        mfs, err := prometheus.DefaultGatherer.Gather()
        Expect(err).NotTo(HaveOccurred())
        for _, mf := range mfs {
            if mf.GetName() != "agent_controller_vault_scanner_skipped_files_total" {
                continue
            }
            for _, m := range mf.GetMetric() {
                for _, lp := range m.GetLabel() {
                    if lp.GetName() == "reason" && lp.GetValue() == reason {
                        return m.GetCounter().GetValue()
                    }
                }
            }
        }
        return 0
    }
    ```

    Add `"github.com/prometheus/client_golang/prometheus"` to the test file's import block. The other required packages (`os`, `context`, `time`, `github.com/bborbe/agent/task/controller/pkg/metrics`, `github.com/bborbe/agent/task/controller/pkg/scanner`, Ginkgo, Gomega) are already imported.

    Add a `Context` per reason inside the new `Describe`. Each context writes the corresponding broken file, runs one cycle, and asserts the counter ticks by 1. **Re-scan-increment semantics differ by reason** (this is a load-bearing detail): for the 4 reasons that skip the file BEFORE the `v.hashes[relPath] = fileEntry{...}` line (`invalid_frontmatter`, `duplicate_frontmatter_invalid`, `read_failed`, `inject_task_identifier_failed`), a second cycle re-processes the file and the counter ticks again (cumulative = 2). For the `empty_status` reason, the scanner stores the file hash in `v.hashes` BEFORE the empty-status check, so the second cycle short-circuits at the `existing.hash == hash` check and the counter does NOT tick again (cumulative stays at 1). The per-Context test bodies assert the matching behaviour; document this in a comment above each test so future maintainers understand why two of the five tests do not show the by-design 2x re-scan increment.

    - `Context("invalid_frontmatter reason")` — write a file whose frontmatter contains a bare `[` (the existing test `It("skips file when YAML is invalid and fails DeduplicateFrontmatter first unmarshal", ...)` uses exactly this content; reuse the same content string). Asserts delta is 1 after one cycle, 2 after two.

    - `Context("duplicate_frontmatter_invalid reason")` — same content as `invalid_frontmatter` (a bare `[` makes the file fail at `DeduplicateFrontmatter`, which is the FIRST `invalid_frontmatter` site in the source order — the first counter increment for this content is actually `duplicate_frontmatter_invalid`, not `invalid_frontmatter`). Document this in the test by explicitly asserting the `duplicate_frontmatter_invalid` label ticks and the `invalid_frontmatter` label does NOT tick. This is a subtle test correctness detail: the test for `invalid_frontmatter` needs different content (e.g. a content that passes `extractFrontmatter` and `DeduplicateFrontmatter` but fails `yaml.Unmarshal`, such as one with an unparseable scalar). For the bare-`[` content: the `extractFrontmatter` step parses the raw frontmatter string, then `DeduplicateFrontmatter` does an internal unmarshal that fails on the bare `[`, so the FIRST skip site hit is `DeduplicateFrontmatter` → `duplicate_frontmatter_invalid`. To exercise `invalid_frontmatter` from the `yaml.Unmarshal` site specifically, use content like `"---\ntask_identifier: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\nstatus: : invalid\n---\n"` (a YAML mapping with a colon that fails `yaml.Unmarshal`). To exercise `invalid_frontmatter` from the `extractFrontmatter` site specifically is not possible without an internal fault-injection point (out of scope per spec Non-goals) — so the `invalid_frontmatter` Context asserts the `yaml.Unmarshal` site only and uses content that passes the earlier two steps.

    - `Context("empty_status reason")` — write a file with valid YAML frontmatter but no `status` field. Use content: `"---\ntask_identifier: 88888888-8888-4888-8888-888888888888\nassignee: claude\n---\n# Empty status\n"`. The file's `processFile` will pass frontmatter parsing, set `taskID`, then hit the `frontmatter.Status() == ""` check and skip.

    - `Context("read_failed reason")` — introduce a small test-local struct `failingReadGitClient` in the test file that wraps `testGitClient` and overrides `ReadFile` to return `os.ErrNotExist`. The struct lives in the test file (not exported). It must implement the `gitclient.GitClient` interface — the simplest approach is to embed `*testGitClient` and override only `ReadFile` and (if needed) any other methods that the constructor calls. The test creates a `failingReadGitClient` with a `path` that points to a non-existent subdirectory of `tmpDir` so the file list is empty; alternatively, set the embed's `path` to a real directory and have the override `ReadFile` always return an error regardless of `relPath`. The test asserts: after one `RunCycle` the `read_failed` label ticks by 1, after two cycles it ticks by 2. Note: with an empty file list, `RunCycle` may not invoke `ReadFile` at all — to force the failure path, the test must write a real file (or stub the list) AND have `ReadFile` return an error. Easiest approach: write a file in the test, then use a `failingReadGitClient` whose `ListFiles` returns that one path but whose `ReadFile` returns `os.ErrNotExist`.

    - `Context("inject_task_identifier_failed reason")` — the existing scanner code path makes this counter increment hard to exercise from the external test package, because the file must reach `injectAndStore` and then `InjectTaskIdentifier` must fail. `InjectTaskIdentifier` returns an error when the content does not start with the frontmatter delimiter, but by the time `processFile` reaches `injectAndStore`, the content has already passed the `extractFrontmatter` step which requires the frontmatter delimiter. The path is unreachable through `RunCycle` without an internal fault-injection point. The cleanest approach: add a new file `vault_scanner_internal_test.go` in package `scanner` (not `scanner_test`) containing one `Describe("injectAndStore", ...)` block with one `It` that:
        1. Creates a `*vaultScanner` directly with a real `metrics.New()`.
        2. Calls `(v *vaultScanner).injectAndStore(ctx, []byte("no frontmatter at all"), "rel.md", "")` directly (the method is unexported but accessible from the same package).
        3. Asserts the `inject_task_identifier_failed` counter ticks by 1, the `ReadFile` / `ListFiles` errors are not relevant, the write counter does not increment, and the returned `(*lib.Task, string, bool)` is `(nil, "", false)`.
        The `Describe` in the new file uses the same Ginkgo style as `vault_scanner_test.go` but is in package `scanner` (internal test). The `BeforeSuite` / `RegisterFailHandler` is already in `scanner_suite_test.go` and the Ginkgo registration is global per package, so the new file just needs to add a `var _ = Describe(...)` declaration — no separate test entry point. The file imports `task/controller/pkg/metrics` for the `metrics.New()` and the `ReasonInjectTaskIdentifierFailed` constant.

    Each test asserts the matching re-scan behaviour. For the 4 reasons that skip the file before the hash is stored (`invalid_frontmatter`, `duplicate_frontmatter_invalid`, `read_failed`, `inject_task_identifier_failed`): after one `RunCycle` (or one direct `injectAndStore` call), the counter is exactly 1 more than the pre-cycle value; after a second `RunCycle`, it is exactly 2 more. For the `empty_status` reason: after one `RunCycle`, the counter is exactly 1 more than the pre-cycle value; after a second `RunCycle`, it is unchanged (still 1 more than the pre-cycle value). The per-Context bodies explicitly assert these values and assert that the other four reason labels do NOT tick in the same test, so a future scanner change that reorders the empty_status check (e.g. moving it before the hash storage) is caught by the test breaking.

14. **Add the broken-vs-valid regression test**

    In the same `vault_scanner_test.go` file, add a new `It` block inside the new `Describe("SkippedFilesTotal counter", ...)` block. The test:

    - Writes two files: one with content `"---\n<<<<<<< HEAD\ntask_identifier: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\nstatus: todo\nassignee: claude\n---\n# Broken"` (contains an unresolved git merge marker; will fail at `extractFrontmatter` because the merge marker lines are not valid YAML) and one with valid content `"---\ntask_identifier: bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nstatus: todo\nassignee: claude\n---\n# Valid"`.
    - Calls `s.RunCycle(ctx, results)`.
    - Asserts the `Changed` slice contains the valid file's task and does NOT contain the broken one.
    - Asserts the `invalid_frontmatter` counter increased by 1.
    - Calls `s.RunCycle(ctx, results)` a second time.
    - Asserts the `invalid_frontmatter` counter increased by 2 total (i.e. +1 on the second cycle) — this proves the broken file's hash was NOT stored in `hashes`, so the scanner re-processed it.

    The test file's existing `BeforeEach` (which sets up `tmpDir`, `taskDir`, `fakeGit`, `results`, and `s`) is reused; the two files are written inside the `It` body using `os.WriteFile`.

15. **Add the source-grep invariant test**

    In `task/controller/pkg/scanner/vault_scanner_test.go`, add a final `It` block (still inside the new `Describe("SkippedFilesTotal counter", ...)` block) that re-states the AC#6 invariant as an executable test. The test runs the `awk` + `grep -cE` pipeline from AC#6 and asserts both counts equal 6:

    ```go
    It("maintains counter-call parity with skip-site log lines (AC#6 invariant)", func() {
        // The test file is at pkg/scanner/vault_scanner_test.go, so the source is
        // at pkg/scanner/vault_scanner.go — same directory.
        scannerSrc, err := filepath.Abs("vault_scanner.go")
        Expect(err).NotTo(HaveOccurred())

        cmd := exec.Command("awk", `/^func \(v \*vaultScanner\) (processFile|injectAndStore)\(/,/^}/`, scannerSrc)
        out, err := cmd.Output()
        Expect(err).NotTo(HaveOccurred())
        body := string(out)

        skipCount := strings.Count(body, `glog.Warningf("skipping`) +
            strings.Count(body, `glog.Errorf("skipping`) +
            strings.Count(body, `glog.Warningf("failed to read`)
        counterCount := strings.Count(body, `SkippedFilesTotal(`)
        Expect(skipCount).To(Equal(6), "expected 6 skip-site log lines, got %d", skipCount)
        Expect(counterCount).To(Equal(6), "expected 6 counter increment calls, got %d", counterCount)
    })
    ```

    The count is 6 (not 5) because the `read_failed` site uses the `failed to read %s` prefix, not the `skipping` prefix, but the AC#6 grep matches both prefixes together.

    The test must be added to the same file and uses the existing imports. The `exec`, `strings`, and `filepath` packages are already imported. No new imports are needed for this test.

16. **Add the `CHANGELOG` entry**

    In `CHANGELOG.md` at the repository root, if no `## Unreleased` section exists, insert one immediately after the SemVer preamble block (the bullets describing MAJOR/MINOR/PATCH) and before the first `## v0.X.Y` section. The section header is `## Unreleased` (no version number — version is assigned at release time per the changelog guide).

    Inside `## Unreleased`, add exactly one entry:

    ```
    - feat(task/controller): add `agent_controller_vault_scanner_skipped_files_total{reason}` counter and promote operator-actionable skip logs to `glog.Errorf`, restoring Prometheus observability for files silently skipped by the vault scanner; references the 2026-05-31 / 2026-06-01 incident and advances [[Make Parked Agent Tasks Visible to Operator]]
    ```

    The entry is `feat:` (per spec AC#10 — the team treats counter addition as a new feature, not a bug fix; this is the default and only changes if the team explicitly decides otherwise). The wording must mention the counter name, the date, and the doctrine page — AC#10 is a hard contract.

17. **Update the controller design doc**

    In `docs/controller-design.md` (at the repository root, NOT under `task/controller/`), add one new paragraph to the scanner section. The best insertion point is at the end of the `## Change Detection (git → Kafka)` numbered list block (the section starts around line 19). Add:

    > The scanner increments `agent_controller_vault_scanner_skipped_files_total{reason=<closed enum>}` at every skip site (broken frontmatter, unreadable file, empty status, injection failure, unresolvable duplicate frontmatter). The counter is pre-initialised at zero for every reason label so dashboards see all five before the first skip. Operators alert on `rate(agent_controller_vault_scanner_skipped_files_total[5m]) > 0`; a positive rate means a broken file is currently in the vault and is not being scanned.

    This satisfies AC#11. Do NOT remove or rewrite the existing scanner-section content.

18. **Run precommit**

    From `task/controller`:

    ```bash
    cd task/controller && make precommit
    ```

    Must pass with exit code 0. All new tests turn green; all existing tests in `pkg/scanner` and `pkg/metrics` remain green (AC#7 — no regression).
</requirements>

<constraints>
- Do NOT change the public `VaultScanner` interface or the `ScanResult` struct. The interface and result shape are contracts consumed by `pkg/sync` and the publisher; changing them is out of scope (spec Non-goal #2 / "Must not change" in spec Constraints).
- Do NOT change the `hashes` map keying or its contents. Broken files stay un-keyed (the existing behaviour is preserved — re-scan increment is intentional).
- Do NOT add a per-file hash entry for skipped files. Spec Non-goal #7 forbids it.
- Do NOT add a config knob, env var, or feature flag to disable the counter increment or the log-level promotion. Spec Non-goal #5 forbids it.
- Do NOT add a `human_review` task spawn for broken files. Spec Non-goal #4 defers that to a follow-up spec.
- Do NOT change the scanner's public constructor names (`NewVaultScanner` and `NewGitRestVaultScanner`). The signatures gain one parameter; the names stay (spec Non-goal #6).
- Do NOT change the existing per-cycle log lines for git pull, list, and commit/push failures. Only the four skip sites named in Desired Behavior #4 change log level.
- Do NOT change the existing seven metric names, their label sets, or their pre-initialised values. Those were established in prompt one.
- Do NOT change the file-level log levels for `read_failed` and `inject_task_identifier_failed`. Only the four sites covered by AC#4 are promoted (extractFrontmatter, DeduplicateFrontmatter, yaml.Unmarshal, empty_status). The `duplicate_frontmatter_invalid` site is promoted per the AC#4 evidence shape despite the spec rationale paragraph's contrary statement — see the "Spec-internal note on log-level promotion" in `<context>`.
- Do NOT change `result_writer.go` or any code in `pkg/result` — out of scope.
- Do NOT add a new scenario test under `scenarios/`. Spec AC#12 explicitly forbids it; unit + integration tests via `prometheus.DefaultGatherer.Gather()` are sufficient.
- Do NOT add a per-file path or frontmatter content to a label value. The `reason` label is a closed enum; high-cardinality labels are forbidden by the metrics guide.
- Do NOT commit — dark-factory handles git.
- All existing tests must still pass.
- Follow the project's `bborbe/errors` and `github.com/golang/glog` patterns.
- The two scanner constructors MUST stay parallel in shape; do not extract a shared struct-construction helper.
- The five reason constants live in the metrics package (from prompt one). Reference them by name (`metrics.ReasonInvalidFrontmatter`, etc.) — do NOT inline the string literals in the scanner code.
</constraints>

<verification>
```bash
cd task/controller && go test ./pkg/scanner/... -v -run 'SkippedFilesTotal|skipped_files_total'
```
Must show all new per-reason and regression tests turning green; the source-grep invariant test must also pass.

```bash
cd task/controller && go test ./pkg/scanner/... -v
```
Must pass with all existing tests still green (the existing tests that use the constructors now pass `metrics.New()` and must continue to work).

```bash
cd task/controller && go test ./pkg/metrics/... -v
```
Must pass (regression on prompt one's tests).

```bash
cd task/controller && grep -cE 'glog\.Errorf\("skipping' pkg/scanner/vault_scanner.go
```
Must equal 4 (the four promoted sites: extractFrontmatter, DeduplicateFrontmatter, yaml.Unmarshal, empty_status).

```bash
cd task/controller && grep -cE 'glog\.Warningf\("skipping' pkg/scanner/vault_scanner.go
```
Must equal 1 (the one non-promoted skip site: `inject_task_identifier_failed`).

```bash
cd task/controller && grep -cE 'SkippedFilesTotal\(' pkg/scanner/vault_scanner.go
```
Must equal 6 (one counter call per skip site).

```bash
cd task/controller && grep -nE 'NewGitRestVaultScanner' main.go
```
Must show the call now passes five arguments (the new `metrics.New()` is the fifth).

```bash
cd task/controller && grep -n 'vault_scanner_skipped_files_total' CHANGELOG.md
```
Must show one match inside the new `## Unreleased` entry (AC#10).

```bash
cd task/controller && grep -n 'vault_scanner_skipped_files_total' /workspace/docs/controller-design.md
```
Must show at least one match (AC#11).

```bash
cd task/controller && make precommit
```
Must exit 0 (AC#9 — final log line contains `ready to commit`).
</verification>
