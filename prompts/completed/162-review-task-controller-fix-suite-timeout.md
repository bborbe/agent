---
status: completed
summary: Added suiteConfig.Timeout to metrics/result/command Ginkgo suites; added missing time.Local, format.TruncatedDiff, GinkgoConfiguration setup to gitrestclient and main test suites; replaced time.Now() with libtimetest.ParseDateTime fixed time in task_result_executor_test.go
container: agent-exec-162-review-task-controller-fix-suite-timeout
dark-factory-version: v0.173.0
created: "2026-05-24T00:00:00Z"
queued: "2026-05-25T21:00:25Z"
started: "2026-05-25T22:27:29Z"
completed: "2026-05-25T22:30:13Z"
---

<summary>
- Adds suiteConfig.Timeout = 60 * time.Second to 3 Ginkgo suite files
- Adds full Ginkgo suite setup (time.Local, format.TruncatedDiff, GinkgoConfiguration) to gitrestclient_suite_test.go
- Adds full Ginkgo suite setup to main_test.go
- Fixes time.Now() usage in task_result_executor_test.go
</summary>

<objective>
Three Ginkgo suite files (`metrics_suite_test.go`, `result_suite_test.go`, `command_suite_test.go`) lack `suiteConfig.Timeout` in their `TestXxx` function. `gitrestclient_suite_test.go` and `main_test.go` lack all required suite setup (`time.Local = time.UTC`, `format.TruncatedDiff = false`, `GinkgoConfiguration`). Additionally, `task_result_executor_test.go` uses `time.Now()` which should be a fixed test time.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `docs/dod.md` for Definition of Done.

Files to read before making changes:
- `task/controller/pkg/metrics/metrics_suite_test.go`
- `task/controller/pkg/result/result_suite_test.go`
- `task/controller/pkg/command/command_suite_test.go`
- `task/controller/pkg/gitrestclient/gitrestclient_suite_test.go`
- `task/controller/main_test.go`
- `task/controller/pkg/command/task_result_executor_test.go`
</context>

<requirements>

### 1. Add suiteConfig.Timeout to metrics, result, command suites

In each of `metrics_suite_test.go`, `result_suite_test.go`, and `command_suite_test.go`, update the `TestXxx` function:

**Before:**
```go
func TestXxx(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Xxx Suite")
}
```

**After:**
```go
func TestXxx(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    suiteConfig, reporterConfig := GinkgoConfiguration()
    suiteConfig.Timeout = 60 * time.Second
    RunSpecs(t, "Xxx Suite", suiteConfig, reporterConfig)
}
```

### 2. Fix gitrestclient_suite_test.go

Add all missing setup to `gitrestclient_suite_test.go`:
```go
func TestGitrestclient(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    suiteConfig, reporterConfig := GinkgoConfiguration()
    suiteConfig.Timeout = 60 * time.Second
    RunSpecs(t, "Gitrestclient Suite", suiteConfig, reporterConfig)
}
```

Add imports for `time` and `github.com/onsi/gomega/format`.

### 3. Fix main_test.go

Add all missing setup to `main_test.go`:
```go
func TestSuite(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    suiteConfig, reporterConfig := GinkgoConfiguration()
    suiteConfig.Timeout = 60 * time.Second
    RunSpecs(t, "Main Suite", suiteConfig, reporterConfig)
}
```

Add imports for `time` and `github.com/onsi/gomega/format`.

### 4. Fix time.Now() in task_result_executor_test.go

Replace `time.Now()` with a fixed `libtime.DateTime` value:
```go
now := libtime.DateTime(time.Now())  // replace with fixed test value
```

Use `libtimetest.ParseDateTime("2026-01-15T10:00:00Z")` or similar fixed timestamp.

### 5. Run tests:
```bash
cd task/controller && make test
```

### 6. Run precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.

</requirements>

<constraints>
- Only change test files listed above
- Do NOT commit — dark-factory handles git
- Follow Ginkgo v2 + Gomega patterns
</constraints>

<verification>
cd task/controller && make precommit
</verification>
