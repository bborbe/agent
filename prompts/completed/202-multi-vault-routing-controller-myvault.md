---
status: completed
spec: [044-multi-vault-routing]
summary: 'Wired vault routing into task/controller: added required MY_VAULT env var, new pkg/routing package with ShouldProcess predicate (100% coverage, 10 specs) and ValidateMyVault startup check, extended NewCreateTaskExecutor with myVault arg + V(2) skip-log, threaded myVault through factory.CreateCommandConsumer and application.Run, added 4 new vault-routing executor tests + reflect-based MyVault field test, and updated CHANGELOG + controller-design.md'
container: agent-multi-vault-routing-exec-202-multi-vault-routing-controller-myvault
dark-factory-version: v0.177.1
created: "2026-06-14T21:22:30Z"
queued: "2026-06-14T21:26:34Z"
started: "2026-06-14T21:30:53Z"
completed: "2026-06-14T21:39:11Z"
branch: dark-factory/multi-vault-routing
---

<summary>
- The task controller binary gains a required MY_VAULT env var (with --my-vault flag) naming the single Obsidian vault it serves — startup fails fast if it is missing or malformed
- A new tiny pure function lives in a controller-internal package and answers "should this controller act on this create command?" against a 2-by-3 matrix of vault combinations
- The CreateTaskExecutor consults the predicate before doing any work; on a miss, it returns no error and performs no git write, no result publish, and no offset-blocking side effect
- A single structured log line at V(2) names the command's TargetVault, the effective target, and the controller's MY_VAULT on every skip so operators can verify routing decisions in the controller logs
- The legacy fallback is hard-coded to the literal string "openclaw" so today's producers that emit no TargetVault keep flowing to the openclaw controller
- The factory.CreateCommandConsumer signature gains one argument (myVault string) and the new wiring is the only place the predicate's value is captured
- The CHANGELOG gets a single Unreleased entry naming both the new field and the new env var; the controller design doc gets one paragraph in the Command Processing section describing the routing rule
- The single existing in-repo caller of CreateCommandConsumer (main.go) is updated to pass the new myVault argument; the test main for the application struct gains a reflect-based test that the MY_VAULT env tag and arg tag exist

</summary>

<objective>
Wire vault routing into the task controller: add a required `MY_VAULT` env var, introduce a small separately-testable predicate (`ShouldProcess(cmd task.CreateCommand, myVault string) bool`) in a new controller-internal package, teach `CreateTaskExecutor` to consult the predicate and skip commands whose effective target vault is not its own, emit a V(2) log line on each skip, update `factory.CreateCommandConsumer` and `application.Run` to plumb `myVault` through, and finish with the CHANGELOG entry and the controller design doc update. This is the final prompt in the spec — it depends on prompts 1 and 2 having shipped the `TargetVault` field and the `NewCreateCommandSender` two-argument form.
</objective>

<context>
Read `CLAUDE.md` for project conventions.
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-patterns.md` (Interface → Constructor → Struct + error wrapping rules).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` (use `github.com/bborbe/errors`, never `fmt.Errorf`; use `Wrapf` for formatted messages).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-validation-framework-guide.md` (`validation.All` / `validation.Name` / `validation.HasValidationFunc`).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-logging-guide.md` (V(2)=heartbeat, V(3)=per-item, structured key=value log lines).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md` (factory has zero logic, constructors in implementation files, `Create*` vs `New*`).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` (the `## Unreleased` placement rule, the conventional prefixes).
Read `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` (Ginkgo v2 / Gomega / no direct `*testing.T` outside the suite entry-point / no stdlib table tests / counterfeiter mocks).

Key files to read in full before editing:
- `/workspace/task/controller/main.go` — the file to modify; the `application` struct (lines 47-62) gains one field, and `application.Run` (lines 65-152) gains the MY_VAULT startup validation and passes `myVault` into `factory.CreateCommandConsumer` (line 136)
- `/workspace/task/controller/main_internal_test.go` — the existing reflect-based tests for the `application` struct's `BuildGitVersion` field (lines 12-57); a new test for the `MyVault` field follows the same pattern
- `/workspace/task/controller/pkg/factory/factory.go` — `CreateCommandConsumer` (lines 21-45) gains one parameter and threads it into `NewCreateTaskExecutor`
- `/workspace/task/controller/pkg/command/task_create_task_executor.go` — `NewCreateTaskExecutor` (lines 31-90) gains one parameter and consults the routing predicate after `MarshalInto` succeeds
- `/workspace/task/controller/pkg/command/task_create_task_executor_test.go` — the existing Ginkgo test file; inside the `BeforeEach` block, the `executor = command.NewCreateTaskExecutor(...)` call constructs the executor with two arguments and needs one more; a new `Context("vault routing", ...)` block exercises the four AC matrix cases
- `/workspace/task/controller/mocks/git_client.go` — counterfeiter-generated fake; the `GitClient` interface is unchanged so the mock is unchanged
- `/workspace/CHANGELOG.md` — append one `feat:` bullet under `## Unreleased` (section does not exist yet; create it)
- `/workspace/docs/controller-design.md` — append one paragraph to the "Command Processing (Kafka → git)" section (around line 30)

Inlined load-bearing snippets (copy verbatim into the new code, do not paraphrase from memory):

Current `application` struct (lines 47-62 of `task/controller/main.go`):
```go
type application struct {
    SentryDSN       string            `required:"true"  arg:"sentry-dsn"        env:"SENTRY_DSN"        usage:"SentryDSN"                                                                 display:"length"`
    SentryProxy     string            `required:"false" arg:"sentry-proxy"      env:"SENTRY_PROXY"      usage:"Sentry Proxy"`
    Listen          string            `required:"true"  arg:"listen"            env:"LISTEN"            usage:"address to listen to"`
    KafkaBrokers    string            `required:"true"  arg:"kafka-brokers"     env:"KAFKA_BROKERS"     usage:"comma-separated Kafka broker addresses"`
    Branch          base.Branch       `required:"true"  arg:"branch"            env:"BRANCH"            usage:"Kafka topic prefix branch (develop/live)"`
    PollInterval    time.Duration     `required:"false" arg:"poll-interval"     env:"POLL_INTERVAL"     usage:"vault polling interval"                                                                     default:"60s"`
    TaskDir         string            `required:"false" arg:"task-dir"          env:"TASK_DIR"          usage:"task directory within vault"                                                                default:"24 Tasks"`
    DataDir         string            `required:"true"  arg:"data-dir"          env:"DATA_DIR"          usage:"directory for BoltDB offset storage"`
    NoSync          bool              `required:"false" arg:"no-sync"           env:"NO_SYNC"           usage:"disable BoltDB fsync (for testing only)"`
    GitRestURL      string            `required:"false" arg:"git-rest-url"      env:"GIT_REST_URL"      usage:"git-rest HTTP API base URL"                                                                 default:"http://vault-obsidian-openclaw:9090"`
    GatewaySecret   string            `required:"false" arg:"gateway-secret"    env:"GATEWAY_SECRET"    usage:"shared secret for git-rest gateway auth (sent as X-Gateway-Secret header)" display:"length" default:""`
    BuildGitVersion string            `required:"false" arg:"build-git-version" env:"BUILD_GIT_VERSION" usage:"Build Git version (git describe --tags --always --dirty)"                                   default:"dev"`
    BuildGitCommit  string            `required:"false" arg:"build-git-commit"  env:"BUILD_GIT_COMMIT"  usage:"Build Git commit hash"                                                                      default:"none"`
    BuildDate       *libtime.DateTime `required:"false" arg:"build-date"        env:"BUILD_DATE"        usage:"Build timestamp (RFC3339)"`
}
```

Current `factory.CreateCommandConsumer` (lines 21-45 of `task/controller/pkg/factory/factory.go`):
```go
func CreateCommandConsumer(
    saramaClientProvider libkafka.SaramaClientProvider,
    syncProducer libkafka.SyncProducer,
    db libkv.DB,
    branch base.Branch,
    resultWriter result.ResultWriter,
    gitClient gitclient.GitClient,
    taskDir string,
) run.Func {
    executors := cdb.CommandObjectExecutorTxs{
        command.NewTaskResultExecutor(resultWriter),
        command.NewIncrementFrontmatterExecutor(gitClient, taskDir),
        command.NewUpdateFrontmatterExecutor(gitClient, taskDir),
        command.NewCreateTaskExecutor(gitClient, taskDir),
    }
    return cdb.RunCommandConsumerTxDefault(
        saramaClientProvider,
        syncProducer,
        db,
        lib.TaskV1SchemaID,
        branch,
        true, // ignoreUnsupported: skip commands with unknown operations
        executors,
    )
}
```

Current `NewCreateTaskExecutor` constructor signature and its first ~15 lines (lines 31-50 of `task/controller/pkg/command/task_create_task_executor.go`):
```go
func NewCreateTaskExecutor(
    gitClient gitclient.GitClient,
    taskDir string,
) cdb.CommandObjectExecutorTx {
    return cdb.CommandObjectExecutorTxFunc(
        task.CreateCommandOperation,
        true,
        func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
            var cmd task.CreateCommand
            if err := commandObject.Command.Data.MarshalInto(ctx, &cmd); err != nil {
                return nil, nil, errors.Wrapf(
                    ctx,
                    cdb.ErrCommandObjectSkipped,
                    "malformed CreateTaskCommand: %v",
                    err,
                )
            }
            if err := cmd.TaskIdentifier.Validate(ctx); err != nil {
                return nil, nil, errors.Wrapf(ctx, err, "validate task_identifier")
            }
            ...
```

The existing `cdb.ErrCommandObjectSkipped` sentinel is the project's pattern for "this command is unprocessable but the consumer should advance the offset". The vault-skip case is different — it is a valid command that the controller chose to ignore — so it returns `(nil, nil, nil)` (the third value is the error), matching the "file already exists (idempotent)" path at lines 56-62 of the same file.

Spec being implemented: `specs/in-progress/044-multi-vault-routing.md`. The exact startup validation rule (Desired Behavior #5), the routing rule with the openclaw legacy fallback (Desired Behavior #6), the skip-log shape (Desired Behavior #7), and the eight Acceptance Criteria this prompt covers (AC 9, 10, 11, 12, 13, 14, 15, 16, 17) are spelled out in the spec.

Predecessor prompts (must run first and complete successfully):
- `1-multi-vault-routing-targetvault-field.md` — the `TargetVault` field and the slug regex on `CreateCommand` must exist
- `2-multi-vault-routing-sender-default.md` — the `NewCreateCommandSender` two-argument form must be in place (the controller's `factory.CreateCommandConsumer` does not use the sender directly, but the field is what consumers will set, so the controller-side routing must agree with the sender's behavior)

The legacy-fallback string `openclaw` is hard-coded as a package constant in the new routing package (per spec Desired Behavior #6: "the effective target is the literal string `openclaw` (legacy fallback)"). Do NOT make it configurable.
</context>

<requirements>

1. **Create the new routing package with the predicate**

   Create a new file `/workspace/task/controller/pkg/routing/routing.go` (and its test file `/workspace/task/controller/pkg/routing/routing_test.go`). The package name is `routing` and it has zero dependencies on the controller's other packages — only on the `task.CreateCommand` type and the `regexp` / `errors` / `validation` stdlib+lib. The file contains:

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   // Package routing decides whether a task controller should process a
   // given CreateCommand based on the command's target vault and the
   // controller's configured MY_VAULT.
   package routing

   import (
       "context"
       "regexp"

       "github.com/bborbe/errors"
       "github.com/bborbe/validation"

       task "github.com/bborbe/agent/lib/command/task"
   )

   // LegacyDefaultVault is the vault a controller acts on when a command
   // leaves its TargetVault empty. Hard-coded per spec 044; do not make configurable.
   const LegacyDefaultVault = "openclaw"

   // targetVaultSlugRegexp must stay in sync with the same regex on
   // task.CreateCommand.Validate (lib/command/task/create-command.go).
   var targetVaultSlugRegexp = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

   // ValidateMyVault returns a wrapped validation error when myVault is empty
   // or does not match the slug regex ^[a-z][a-z0-9-]*$.
   func ValidateMyVault(ctx context.Context, myVault string) error {
       if myVault == "" {
           return errors.Wrap(ctx, validation.Error, "MY_VAULT must not be empty")
       }
       if !targetVaultSlugRegexp.MatchString(myVault) {
           return errors.Wrapf(
               ctx,
               validation.Error,
               "MY_VAULT %q must match ^[a-z][a-z0-9-]*$",
               myVault,
           )
       }
       return nil
   }

   // ShouldProcess returns true iff the controller's myVault owns this command.
   // An empty cmd.TargetVault falls back to LegacyDefaultVault (spec 044 AC 12).
   // A non-empty cmd.TargetVault is compared verbatim (no case-folding).
   func ShouldProcess(cmd task.CreateCommand, myVault string) bool {
       effective := cmd.TargetVault
       if effective == "" {
           effective = LegacyDefaultVault
       }
       return effective == myVault
   }
   ```

   The `ValidateMyVault` function is the single source of truth for the startup check; the controller's `application.Run` calls it (requirement 5). The `ShouldProcess` function is the pure routing predicate; the executor calls it (requirement 4). Both are independently testable.

2. **Write the routing package tests (AC 12)**

   In `/workspace/task/controller/pkg/routing/routing_test.go`, write a Ginkgo suite that covers all three AC 12 test contracts. Use a `DescribeTable` for the routing matrix (8 cases) and two `It` blocks for the validation function.

   ```go
   // Copyright (c) 2026 Benjamin Borbe All rights reserved.
   // Use of this source code is governed by a BSD-style
   // license that can be found in the LICENSE file.

   package routing_test

   import (
       "context"
       "testing"

       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"

       lib "github.com/bborbe/agent/lib"
       task "github.com/bborbe/agent/lib/command/task"
       "github.com/bborbe/agent/task/controller/pkg/routing"
   )

   func TestSuite(t *testing.T) {
       RegisterFailHandler(Fail)
       RunSpecs(t, "routing")
   }

   var _ = Describe("ShouldProcess", func() {
       DescribeTable("routing matrix",
           func(cmdTargetVault, myVault string, want bool) {
               cmd := task.CreateCommand{
                   TaskIdentifier: lib.TaskIdentifier("task-1"),
                   Title:          "T",
                   Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
                   TargetVault:    cmdTargetVault,
               }
               Expect(routing.ShouldProcess(cmd, myVault)).To(Equal(want))
           },
           // (cmd empty, my openclaw) → true (legacy fallback to openclaw)
           Entry("empty target, myVault=openclaw → true (legacy fallback)", "", "openclaw", true),
           // (cmd openclaw, my openclaw) → true
           Entry("openclaw target, myVault=openclaw → true", "openclaw", "openclaw", true),
           // (cmd personal, my personal) → true
           Entry("personal target, myVault=personal → true", "personal", "personal", true),
           // (cmd empty, my personal) → false (legacy fallback is openclaw, not personal)
           Entry("empty target, myVault=personal → false (legacy is openclaw)", "", "personal", false),
           // (cmd openclaw, my personal) → false
           Entry("openclaw target, myVault=personal → false", "openclaw", "personal", false),
           // (cmd other, my openclaw) → false
           Entry("other target, myVault=openclaw → false", "other", "openclaw", false),
       )
   })

   var _ = Describe("ValidateMyVault", func() {
       var ctx context.Context
       BeforeEach(func() { ctx = context.Background() })

       It("rejects empty MY_VAULT", func() {
           err := routing.ValidateMyVault(ctx, "")
           Expect(err).To(HaveOccurred())
           Expect(err.Error()).To(ContainSubstring("MY_VAULT"))
       })

       It("rejects invalid slug 'Bad'", func() {
           err := routing.ValidateMyVault(ctx, "Bad")
           Expect(err).To(HaveOccurred())
           Expect(err.Error()).To(ContainSubstring("MY_VAULT"))
           Expect(err.Error()).To(ContainSubstring("^[a-z][a-z0-9-]*$"))
       })

       It("accepts openclaw", func() {
           Expect(routing.ValidateMyVault(ctx, "openclaw")).To(Succeed())
       })

       It("accepts personal", func() {
           Expect(routing.ValidateMyVault(ctx, "personal")).To(Succeed())
       })
   })
   ```

   The 6 routing-matrix `Entry` lines are the spec's AC 12 matrix (the spec lists exactly these 6 cases). The 4 `ValidateMyVault` tests cover the AC 9 and AC 10 evidence: empty is rejected, "Bad" is rejected, both valid slugs are accepted.

3. **Extend `NewCreateTaskExecutor` with the `myVault` argument**

   In `/workspace/task/controller/pkg/command/task_create_task_executor.go`, change the constructor signature to add a third argument. The new signature:

   ```go
   func NewCreateTaskExecutor(
       gitClient gitclient.GitClient,
       taskDir string,
       myVault string,
   ) cdb.CommandObjectExecutorTx {
   ```

   Inside the closure, AFTER the existing `if err := cmd.TaskIdentifier.Validate(ctx); err != nil` block (and BEFORE the `validateCreateTaskFrontmatter` call), add the routing check:

   ```go
   if !routing.ShouldProcess(cmd, myVault) {
       effective := cmd.TargetVault
       if effective == "" {
           effective = routing.LegacyDefaultVault
       }
       glog.V(2).Infof(
           "create-task: skipped vault mismatch target=%q effective=%q my=%q task=%s",
           cmd.TargetVault, effective, myVault, cmd.TaskIdentifier,
       )
       return nil, nil, nil
   }
   ```

   The log line format is `key=value` style (matching the existing `glog.V(2).Infof` lines in this file) and includes all three values the spec requires for skip-log evidence (AC 14): the command's `TargetVault`, the effective target, and `MY_VAULT`. The `task_identifier` is included for log correlation with the rest of the controller's logs. The function returns `(nil, nil, nil)` — no error, no event, no offset block — matching the "file already exists (idempotent)" path in the same file.

   Add `"github.com/bborbe/agent/task/controller/pkg/routing"` to the import block of this file (alphabetical: it goes after `pkg/result` and before any stdlib additions). The `glog` import is already present.

4. **Update `factory.CreateCommandConsumer` to thread `myVault` through**

   In `/workspace/task/controller/pkg/factory/factory.go`, change the `CreateCommandConsumer` signature to add one parameter (placed at the end, after `taskDir`, so the existing call site in `main.go` is the only one that needs updating). The new signature:

   ```go
   func CreateCommandConsumer(
       saramaClientProvider libkafka.SaramaClientProvider,
       syncProducer libkafka.SyncProducer,
       db libkv.DB,
       branch base.Branch,
       resultWriter result.ResultWriter,
       gitClient gitclient.GitClient,
       taskDir string,
       myVault string,
   ) run.Func {
   ```

   The `NewCreateTaskExecutor` call inside (line 34) gains the new argument:

   ```go
   command.NewCreateTaskExecutor(gitClient, taskDir, myVault),
   ```

   The other three executors (`NewTaskResultExecutor`, `NewIncrementFrontmatterExecutor`, `NewUpdateFrontmatterExecutor`) are unchanged — they do not consume the routing field because they do not write to the vault based on a producer-supplied target (result writers update existing files; frontmatter mutators update existing files; only `create-task` materializes a new file in the vault, and only it is subject to routing).

5. **Add `MyVault` to the `application` struct with validation**

   In `/workspace/task/controller/main.go`, add a new field to the `application` struct. The new field is the LAST field (so existing fields keep their relative order). The new line:

   ```go
   MyVault         string            `required:"true"  arg:"my-vault"          env:"MY_VAULT"          usage:"vault slug this controller serves (e.g. openclaw, personal); legacy empty targetVault defaults to openclaw"`
   ```

   The `required:"true"` tag is the project's bborbe/flag mechanism for marking the field as required (the `service.Main` machinery in `main()` enforces it). The `env:"MY_VAULT"` and `arg:"my-vault"` tags are the spec's required env var and CLI flag. The `usage` text is the operator-facing help string — it explains the legacy fallback so a new operator doesn't get confused about why their `personal` controller still acts on legacy commands targeting `openclaw`.

6. **Validate `MyVault` at startup and pass it through**

   In `application.Run` (currently around line 65), add the validation call as the FIRST line of the function body, before any other initialization. The new function body (add to the top of the existing `Run` function):

   ```go
   func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
       if err := routing.ValidateMyVault(ctx, a.MyVault); err != nil {
           return err
       }
       libmetrics.NewBuildInfoMetrics().SetBuildInfo(a.BuildGitVersion, a.BuildGitCommit, a.BuildDate)
       // ... rest unchanged
   }
   ```

   Add `"github.com/bborbe/agent/task/controller/pkg/routing"` to the import block of `main.go` (alphabetical: it goes after `pkg/result` and before `pkg/scanner`).

   Then update the `factory.CreateCommandConsumer` call (line 136) to pass `a.MyVault` as the eighth argument. The new call site:

   ```go
   commandConsumer := factory.CreateCommandConsumer(
       saramaClientProvider,
       syncProducer,
       db,
       a.Branch,
       resultWriter,
       gitClient,
       a.TaskDir,
       a.MyVault,
   )
   ```

   The error from `routing.ValidateMyVault` is returned unwrapped (it is already wrapped with `errors.Wrap(ctx, validation.Error, ...)` and includes the `MY_VAULT` substring in its message, which the AC 9 / AC 10 evidence requires). The process exits non-zero via `service.Main`'s machinery, and the operator sees the error on stderr.

7. **Update the existing `task_create_task_executor_test.go` call site**

   In `/workspace/task/controller/pkg/command/task_create_task_executor_test.go`, locate the `executor = command.NewCreateTaskExecutor(fakeGit, taskDir)` line inside the `BeforeEach` block (grep for the constructor call to find the exact line). Add the third argument:

   ```go
   executor = command.NewCreateTaskExecutor(fakeGit, taskDir, "openclaw")
   ```

   The test file's existing tests all pass commands with `TargetVault=""` (which falls back to the legacy `openclaw` per the predicate) so the `myVault="openclaw"` keeps every existing test green. This is the minimal-invasive change that satisfies the new constructor signature; the new vault-routing tests are added in requirement 8.

8. **Add the executor-level vault-routing tests (AC 13, AC 14)**

   In the same `task_create_task_executor_test.go` file, add a new `Context("vault routing", ...)` block at the END of the existing `Describe("HandleCommand", ...)` block (after the last existing `Context`). The new context contains four `It` entries that exercise the AC 13 and AC 14 evidence:

   ```go
   Context("vault routing", func() {
       It("skips a command whose TargetVault is openclaw when myVault=personal (no git write, no error)", func() {
           executor := command.NewCreateTaskExecutor(fakeGit, taskDir, "personal")
           cmdObj := buildCmdObj(task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "Personal Task",
               Frontmatter: lib.TaskFrontmatter{
                   "assignee": "claude",
                   "status":   "next",
               },
               TargetVault: "openclaw",
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
           Expect(err).NotTo(HaveOccurred())
           Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
       })

       It("processes a command whose TargetVault is openclaw when myVault=openclaw (one git write)", func() {
           executor := command.NewCreateTaskExecutor(fakeGit, taskDir, "openclaw")
           cmdObj := buildCmdObj(task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "Openclaw Task",
               Frontmatter: lib.TaskFrontmatter{
                   "assignee": "claude",
                   "status":   "next",
               },
               TargetVault: "openclaw",
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
           Expect(err).NotTo(HaveOccurred())
           Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
       })

       It("processes a command with empty TargetVault when myVault=openclaw (legacy fallback)", func() {
           executor := command.NewCreateTaskExecutor(fakeGit, taskDir, "openclaw")
           cmdObj := buildCmdObj(task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "Legacy Task",
               Frontmatter: lib.TaskFrontmatter{
                   "assignee": "claude",
                   "status":   "next",
               },
               // TargetVault deliberately empty — legacy producer.
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
           Expect(err).NotTo(HaveOccurred())
           Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
       })

       It("skips a command with empty TargetVault when myVault=personal (legacy fallback is openclaw, not personal)", func() {
           executor := command.NewCreateTaskExecutor(fakeGit, taskDir, "personal")
           cmdObj := buildCmdObj(task.CreateCommand{
               TaskIdentifier: lib.TaskIdentifier("task-1"),
               Title:          "Legacy Task",
               Frontmatter: lib.TaskFrontmatter{
                   "assignee": "claude",
                   "status":   "next",
               },
               // TargetVault deliberately empty.
           })
           _, _, err := executor.HandleCommand(ctx, nil, cmdObj)
           Expect(err).NotTo(HaveOccurred())
           Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
       })
   })
   ```

   These four tests use local `executor` variables (not the `BeforeEach`-assigned one) because each test needs a different `myVault`. The `buildCmdObj` helper at line 63 is reused — it builds a `cdb.CommandObject` from a `task.CreateCommand` via `base.ParseEvent`. The 4 cases cover the 4 AC 13/14 evidence rows: (cmd openclaw, my personal) skip, (cmd openclaw, my openclaw) process, (cmd empty, my openclaw) process via legacy, (cmd empty, my personal) skip via legacy.

9. **Skip-log content evidence (AC 14 second half)**

   AC 14 has two halves: (a) the skip-path is taken without error (covered by the skip `It` in requirement 8), and (b) the V(2) log line contains all three spec-required values (`target=`, `effective=`, `my=`).

   The project does **not** standardize on capturing `glog` output in tests (verified: `grep -rln 'glog.SetOutput\|bytes.Buffer.*glog' --include='*_test.go'` in this repo returns no matches). Implementing per-test stderr redirection just for this AC would introduce a new test pattern.

   Instead, AC 14 (b) is satisfied by the verification-block grep that confirms the literal format string with all three `%q` placeholders exists in the new code:

   ```bash
   grep -cE 'create-task: skipped vault mismatch target=%q effective=%q my=%q task=%s' \
     pkg/command/task_create_task_executor.go
   # Must return exactly 1
   ```

   This grep is part of the `<verification>` block at the end of the prompt. **No additional `It` block is required for this requirement** — the runtime behavior is covered by requirement 8's skip-path test, and the log-content shape is covered by the grep.

10. **Add the application-struct field test (AC 9 / AC 10 second evidence)**

    In `/workspace/task/controller/main_internal_test.go`, add a new function `TestApplicationMyVaultFieldExists` that follows the exact pattern of the existing `TestApplicationBuildGitVersionFieldExists` (lines 12-30). The new test asserts the field has the correct env tag, arg tag, and required tag:

    ```go
    func TestApplicationMyVaultFieldExists(t *testing.T) {
        typ := reflect.TypeOf(application{})
        f, ok := typ.FieldByName("MyVault")
        if !ok {
            t.Fatalf("application struct is missing MyVault field")
        }
        if f.Type.Kind() != reflect.String {
            t.Fatalf("MyVault must be string, got %s", f.Type.Kind())
        }
        if got, want := f.Tag.Get("env"), "MY_VAULT"; got != want {
            t.Errorf("MyVault env tag = %q, want %q", got, want)
        }
        if got, want := f.Tag.Get("arg"), "my-vault"; got != want {
            t.Errorf("MyVault arg tag = %q, want %q", got, want)
        }
        if got, want := f.Tag.Get("required"), "true"; got != want {
            t.Errorf("MyVault required tag = %q, want %q", got, want)
        }
    }
    ```

    This is the AC 9 / AC 10 secondary evidence — the field exists, has the right tags, and is required. The primary evidence (the `application.Run` actually failing) is harder to test in a unit harness because `Run` touches Kafka and git-rest, so the project uses this reflect-based test pattern (per the existing precedent for `BuildGitVersion`).

11. **Append the CHANGELOG entry (AC 15)**

    In `/workspace/CHANGELOG.md` (repo root), if no `## Unreleased` section exists, insert one immediately after the SemVer preamble block (the four bullets explaining MAJOR/MINOR/PATCH) and before the first `## v0.X.Y` section (`## v0.66.0` at line 11). The section header is `## Unreleased` (no version number, per the changelog guide).

    Inside `## Unreleased`, add exactly one bullet (a single `feat:` line that covers both the field and the env var; do NOT add separate bullets for each — spec AC 15 is a single grep-returns-2-matches check that the two identifiers are mentioned in the Unreleased section):

    ```
    - feat(task/controller, lib/command/task): add `targetVault` field on CreateCommand (omitted from wire form when empty) and require `MY_VAULT` env var on the task controller; commands whose effective target vault does not match the controller's MY_VAULT are skipped silently with a V(2) log line, and the legacy empty-targetVault fallback routes to `openclaw`
    ```

    The entry mentions `targetVault` and `MY_VAULT` — the spec's AC 15 evidence requires both substrings to appear in the Unreleased section.

12. **Update the controller design doc (AC 16)**

    In `/workspace/docs/controller-design.md` (at the repo root), append one new paragraph to the "## Command Processing (Kafka → git)" section (around line 30, before the "## Frontmatter Merge" section). Insert the paragraph immediately after the "```" closing fence of the command-processing diagram block (the diagram ends around line 48 with the "CQRS framework publishes success/failure result to agent-task-v1-result" line). The new paragraph:

    > The controller reads a required `MY_VAULT` env var (CLI flag `--my-vault`) at startup naming the single Obsidian vault it serves. Every CreateCommand is checked against `MY_VAULT` via the `pkg/routing.ShouldProcess` predicate: the effective target is `cmd.targetVault` if non-empty, otherwise the legacy fallback `openclaw`; commands whose effective target is not `MY_VAULT` are skipped without side effects (no git write, no result publish, no error) and emit a single `glog.V(2)` line naming the command's `targetVault`, the effective target, and `MY_VAULT` so operators can confirm routing decisions. Two controllers (e.g. one per vault) can therefore share the `agent-task-v1-request` topic without duplicating task materializations. The `targetVault` field is added to `task.CreateCommand` with `omitempty`; legacy producers that emit no `targetVault` continue to flow to the `openclaw` controller.

    Do NOT remove or rewrite the existing diagram or any surrounding content — this is a one-paragraph addendum.

13. **Run `make precommit` in the task/controller service directory**

    From `/workspace/task/controller`:

    ```bash
    cd /workspace/task/controller && make precommit
    ```

    Must exit 0. All new tests (the 6 routing-matrix + 4 ValidateMyVault tests in `routing_test.go`; the 5 vault-routing tests in `task_create_task_executor_test.go`; the new field test in `main_internal_test.go`) turn green. All pre-existing tests in the controller's `pkg/...` packages remain green (the `BeforeEach` change in `task_create_task_executor_test.go` and the `factory.CreateCommandConsumer` call site in `main.go` are the only breaking-signature changes, and they are both updated in this prompt).

</requirements>

<constraints>
- Do NOT make `LegacyDefaultVault` configurable. It is a hard-coded package constant in `pkg/routing`; the spec's Desired Behavior #6 says "the effective target is the literal string `openclaw` (legacy fallback)" — a config knob would defeat the spec's contract and is explicitly forbidden by Non-goal #5.
- Do NOT add a config flag to disable the routing filter. Non-goal #4 forbids it; the routing is the spec's design.
- Do NOT add per-vault Kafka topics. Non-goal #1 forbids it; the existing `agent-task-v1-request` topic carries both vaults and the routing happens on the payload.
- Do NOT add a per-vault URL map inside a single controller. Non-goal #3 forbids it; one controller, one vault, one `GIT_REST_URL` is the invariant.
- Do NOT migrate the existing frontmatter-key path. Non-goal #2 forbids it; `TargetVault` is a top-level field, not a frontmatter key.
- Do NOT change the executor's signature for the OTHER three executors (`NewTaskResultExecutor`, `NewIncrementFrontmatterExecutor`, `NewUpdateFrontmatterExecutor`). They do not consume the routing field and adding it to them would couple unrelated code paths.
- Do NOT add a new scenario under `scenarios/`. The spec's "Scenario coverage" section says NO new scenario — unit tests via fake `GitClient` are sufficient.
- Do NOT regenerate the `GitClient` counterfeiter mock. The `GitClient` interface is unchanged; `mocks/git_client.go` stays byte-identical.
- Do NOT add a `human_review` task spawn for skipped commands. Skipped commands return `(nil, nil, nil)` and the consumer advances the offset; this is by design (per spec Failure Modes row "Legacy command with empty targetVault reaches personal controller").
- Do NOT change the `bborbe/flag` struct tag convention on the `application` struct. The new `MyVault` field uses the same `required/arg/env/usage` tag set as the existing fields.
- Do NOT add a separate `cmd.Validate` call on the executor's side for the `TargetVault` field. The sender (prompt 2) and the controller-side `MarshalInto` already round-trip the struct, and adding a third validation would duplicate work. The executor's only job regarding `TargetVault` is the routing check.
- Do NOT commit — dark-factory handles git.
- All existing tests in `task/controller/...` must continue to pass after the change.
- Follow the project's `bborbe/errors`, `github.com/bborbe/validation`, `github.com/golang/glog`, and `counterfeiter` patterns (no `fmt.Errorf`, no `context.Background()` in business logic, Ginkgo/Gomega for tests, factory has zero logic, `Create*` vs `New*`).
- Coverage for the new `routing` package and the modified `NewCreateTaskExecutor` must be ≥80% per the project's DoD — the new tests cover every branch of `ShouldProcess` (6 matrix entries) and `ValidateMyVault` (4 cases), and every branch of the executor's routing check (4 cases).
- Use the slug regex literal `^[a-z][a-z0-9-]*$` verbatim in the new `pkg/routing` package. It must match the regex on `task.CreateCommand.Validate` byte-for-byte; if you suspect a drift, run `grep -n 'targetVault\|targetVaultSlugRegexp' /workspace/lib/command/task/create-command.go /workspace/task/controller/pkg/routing/routing.go` and confirm both regex literals are identical.
- Use the literal `glog.V(2).Infof("create-task: skipped vault mismatch target=%q effective=%q my=%q task=%s", ...)` for the skip log line. The exact format string is what the AC 14 evidence grep matches against.
- Use the literal `routing.LegacyDefaultVault` constant in the executor and in `pkg/routing`'s tests. Do NOT inline the string `"openclaw"` in the executor or in `ShouldProcess` — the constant is the single source of truth.
</constraints>

<verification>
```bash
cd /workspace/task/controller && go test ./pkg/routing/... -v
```
Must show all 6 routing-matrix `Entry` cases + 4 `ValidateMyVault` `It` cases turning green (10 Ginkgo assertions total).

```bash
cd /workspace/task/controller && go test ./pkg/command/... -v -run 'vault routing'
```
Must show the 5 new vault-routing `It` cases turning green (4 from requirement 8 + 1 from requirement 9).

```bash
cd /workspace/task/controller && go test ./pkg/command/... -v
```
Must pass with all existing tests in `pkg/command` (the pre-existing tests use `myVault="openclaw"` so they keep their original behavior for empty-TargetVault inputs).

```bash
cd /workspace/task/controller && go test . -v -run 'TestApplicationMyVaultFieldExists'
```
Must show the new reflect-based test turning green.

```bash
cd /workspace/task/controller && grep -cE 'create-task: skipped vault mismatch target=%q effective=%q my=%q task=%s' pkg/command/task_create_task_executor.go
```
Must equal 1 (the single skip-log format string at requirement 3, including all three `%q` placeholders that satisfy AC 14's "naming TargetVault, effective target, and myVault" requirement).

```bash
cd /workspace/task/controller && grep -nE 'NewCreateTaskExecutor\(' main.go
```
Must show the call still has three arguments (the existing two plus the new `a.MyVault`).

```bash
grep -n 'targetVault\|MY_VAULT' /workspace/CHANGELOG.md
```
Must return at least two matching lines inside the `## Unreleased` section (the spec's AC 15 evidence — both identifiers appear in the new bullet).

```bash
grep -n 'MY_VAULT' /workspace/docs/controller-design.md
```
Must return at least one match (AC 16 evidence — the new doc paragraph mentions the env var).

```bash
cd /workspace/task/controller && make precommit
```
Must exit 0.
</verification>

## DARK-FACTORY-REPORT
```yaml
status: success # or: failed, partial
summary: <one-paragraph description of what changed>
verification:
  command: "cd /workspace/task/controller && make precommit"
  exitCode: 0
improvements:
  - <category: PROMPT|GUIDE|GLOBAL>: <one-line suggestion>  # or omit if none
```
</content>
</invoke>