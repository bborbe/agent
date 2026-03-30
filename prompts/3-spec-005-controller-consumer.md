---
status: created
spec: ["005"]
created: "2026-03-29T20:00:00Z"
branch: dark-factory/agent-result-capture
---

<summary>
- task/controller gains a Kafka consumer that reads from `agent-task-v1-request` (the command topic)
- Each consumed message is parsed as a `base.Command` (CQRS envelope), the `data` map is extracted and unmarshaled into `lib.TaskResultCommand`
- The command handler calls the `TaskResultWriter` (from the previous prompt) to update the vault task file
- On successful file write, the handler calls `gitClient.CommitAndPush` to persist the result to the vault git repo
- On "file not found" or "already written" (writer returned false), the handler skips CommitAndPush and commits the Kafka offset only
- The new consumer runs concurrently alongside the existing git-sync loop via `service.Run`
- The consumer group `agent-task-controller-result` is distinct from the event-producing group
- `docs/agent-job-interface.md` is updated to confirm the `TASK_ID` env var and result consumption flow
- `make precommit` passes in task/controller/
</summary>

<objective>
Wire the Kafka command consumer into `task/controller` so that agent result commands published to `agent-task-v1-request` are consumed, the vault task file is updated, and the change is committed and pushed to git. This closes the feedback loop: agent publishes result → task/controller writes result → human sees updated task in Obsidian.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `task/controller/pkg/writer/task_result_writer.go` — TaskResultWriter interface (from previous prompt; must exist before running this prompt)
- `task/controller/pkg/factory/factory.go` — existing factory wiring pattern (`CreateSyncLoop`)
- `task/controller/main.go` — how saramaClient, branch, gitClient, and service.Run are used
- `task/controller/pkg/gitclient/git_client.go` — GitClient interface, specifically `CommitAndPush`
- `task/executor/pkg/factory/factory.go` — reference pattern for wiring a Kafka consumer
- `task/executor/pkg/handler/task_event_handler.go` — reference pattern for a message handler
- `lib/agent_cdb-schema.go` — `TaskV1SchemaID` (used to derive the command topic)
- `lib/agent_task-result-command.go` — `TaskResultCommand` struct (from previous prompt)
- `task/controller/mocks/mocks.go` — how counterfeiter annotations are registered
- `docs/agent-job-interface.md` — section to update
</context>

<requirements>
### 1. Create `task/controller/pkg/handler/task_result_command_handler.go`

New package `handler` in `task/controller/pkg/handler/`.

#### Interface

```go
//counterfeiter:generate -o ../../mocks/task_result_command_handler.go --fake-name FakeTaskResultCommandHandler . TaskResultCommandHandler

// TaskResultCommandHandler processes a single agent-task-v1-request command message from Kafka.
type TaskResultCommandHandler interface {
    ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}
```

#### Constructor

```go
// NewTaskResultCommandHandler creates a handler that writes the result to the vault and commits to git.
func NewTaskResultCommandHandler(
    writer writer.TaskResultWriter,
    gitClient gitclient.GitClient,
) TaskResultCommandHandler {
    return &taskResultCommandHandler{
        writer:    writer,
        gitClient: gitClient,
    }
}
```

#### ConsumeMessage implementation

```go
func (h *taskResultCommandHandler) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
    // 1. Skip tombstone / empty messages
    if len(msg.Value) == 0 {
        glog.V(3).Infof("skip empty result command at offset %d", msg.Offset)
        return nil
    }

    // 2. Unmarshal the CQRS envelope
    var command base.Command
    if err := json.Unmarshal(msg.Value, &command); err != nil {
        glog.Warningf("failed to unmarshal result command at offset %d: %v", msg.Offset, err)
        return nil // log and skip malformed messages
    }

    // 3. Extract the payload from command.Data (base.Event = map[FieldName]interface{})
    dataBytes, err := json.Marshal(command.Data)
    if err != nil {
        glog.Warningf("failed to re-marshal command data at offset %d: %v", msg.Offset, err)
        return nil
    }
    var taskResult lib.TaskResultCommand
    if err := json.Unmarshal(dataBytes, &taskResult); err != nil {
        glog.Warningf("failed to unmarshal TaskResultCommand at offset %d: %v", msg.Offset, err)
        return nil
    }

    // 4. Validate minimal required fields
    if taskResult.TaskIdentifier == "" {
        glog.Warningf("result command at offset %d has empty taskIdentifier, skipping", msg.Offset)
        return nil
    }

    // 5. Write result to vault file
    wrote, err := h.writer.WriteResult(ctx, taskResult)
    if err != nil {
        return errors.Wrapf(ctx, err, "write result for task %s", taskResult.TaskIdentifier)
    }
    if !wrote {
        return nil // not found or already written — no commit needed
    }

    // 6. Commit and push
    commitMsg := fmt.Sprintf("[agent-task-controller] result for task %s", taskResult.TaskIdentifier)
    if err := h.gitClient.CommitAndPush(ctx, commitMsg); err != nil {
        return errors.Wrapf(ctx, err, "commit result for task %s", taskResult.TaskIdentifier)
    }
    glog.V(2).Infof("result written and committed for task %s", taskResult.TaskIdentifier)
    return nil
}
```

Import paths:
- `"encoding/json"`
- `"fmt"`
- `"github.com/IBM/sarama"`
- `"github.com/bborbe/errors"`
- `"github.com/golang/glog"`
- `"github.com/bborbe/cqrs/base"`
- `lib "github.com/bborbe/agent/lib"`
- `"github.com/bborbe/agent/task/controller/pkg/gitclient"`
- `"github.com/bborbe/agent/task/controller/pkg/writer"`

### 2. Create `task/controller/pkg/handler/handler_suite_test.go`

Standard Ginkgo bootstrap:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate
func TestSuite(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Handler Suite")
}
```

### 3. Create `task/controller/pkg/handler/task_result_command_handler_test.go`

Use counterfeiter mocks for `TaskResultWriter` and `GitClient`. Tests:

**Happy path — file written and committed:**
- FakeTaskResultWriter.WriteResultReturns(true, nil)
- FakeGitClient.CommitAndPushReturns(nil)
- ConsumeMessage with a valid JSON `base.Command` whose `data` field contains valid TaskResultCommand fields
- Verify: WriteResult was called once, CommitAndPush was called once with the correct commit message prefix

**Idempotent — writer returns (false, nil):**
- FakeTaskResultWriter.WriteResultReturns(false, nil)
- Verify: CommitAndPush is NOT called, ConsumeMessage returns nil

**Empty message:**
- msg.Value is empty
- Verify: WriteResult is NOT called, ConsumeMessage returns nil

**Malformed JSON:**
- msg.Value is `[]byte("not json")`
- Verify: WriteResult is NOT called, ConsumeMessage returns nil

**Empty taskIdentifier:**
- command.Data has no `taskIdentifier` key
- Verify: WriteResult is NOT called, ConsumeMessage returns nil

**Writer error:**
- FakeTaskResultWriter.WriteResultReturns(false, errors.New("disk full"))
- Verify: ConsumeMessage returns an error

**CommitAndPush error:**
- FakeTaskResultWriter.WriteResultReturns(true, nil)
- FakeGitClient.CommitAndPushReturns(errors.New("push rejected"))
- Verify: ConsumeMessage returns an error

To build a valid `base.Command` JSON for tests, construct the struct directly and marshal it:

```go
command := base.Command{
    RequestID:   base.RequestID("test-request-id"),
    RequestTime: time.Now(),
    Operation:   base.CommandOperation("update"),
    ID:          base.EventID("agent-task-v1"),
    Initiator:   iam.Initiator{Subject: "test-agent"},
    Data: base.Event{
        "taskIdentifier": "abc-123",
        "status":         "completed",
        "phase":          "done",
        "output":         "Backtest complete",
    },
}
msgBytes, _ := json.Marshal(command)
```

Import `"github.com/bborbe/cqrs/iam"` for `iam.Initiator`.

### 4. Add `CreateResultConsumer` to `task/controller/pkg/factory/factory.go`

Add a second factory function alongside the existing `CreateSyncLoop`:

```go
// CreateResultConsumer wires together a Kafka consumer that reads agent result commands
// from the agent-task-v1-request topic and writes results back to the vault.
func CreateResultConsumer(
    saramaClient sarama.Client,
    branch base.Branch,
    gitClient gitclient.GitClient,
    vaultPath string,
    taskDir string,
    logSamplerFactory log.SamplerFactory,
) libkafka.Consumer {
    resultWriter := writer.NewTaskResultWriter(vaultPath, taskDir)
    commandHandler := handler.NewTaskResultCommandHandler(resultWriter, gitClient)
    topic := lib.TaskV1SchemaID.CommandTopic(branch)
    offsetManager := libkafka.NewSaramaOffsetManager(
        saramaClient,
        libkafka.Group("agent-task-controller-result"),
        libkafka.OffsetOldest,
        libkafka.OffsetOldest,
    )
    return libkafka.NewOffsetConsumerHighwaterMarks(
        saramaClient,
        topic,
        offsetManager,
        commandHandler,
        run.NewTrigger(),
        logSamplerFactory,
    )
}
```

Required new imports in factory.go:
- `"github.com/IBM/sarama"`
- `"github.com/bborbe/log"`
- `"github.com/bborbe/run"`
- `"github.com/bborbe/agent/task/controller/pkg/handler"`
- `"github.com/bborbe/agent/task/controller/pkg/writer"`

The existing `cdb` import (`"github.com/bborbe/cqrs/cdb"`) may be removed if unused after this change — check for compilation errors.

### 5. Update `task/controller/main.go`

In the `Run` method, after creating `syncLoop`, create the result consumer and add it to `service.Run`:

```go
resultConsumer := factory.CreateResultConsumer(
    saramaClient,
    a.Branch,
    gitClient,
    vaultLocalPath,
    a.TaskDir,
    log.DefaultSamplerFactory,
)

return service.Run(
    ctx,
    syncLoop.Run,
    func(ctx context.Context) error {
        return resultConsumer.Consume(ctx)
    },
    a.createHTTPServer(syncLoop),
)
```

The `saramaClient` is not available in the current `Run` method — you must create one. Add it alongside the existing sync producer setup:

```go
saramaClient, err := libkafka.CreateSaramaClient(
    ctx,
    libkafka.ParseBrokersFromString(a.KafkaBrokers),
)
if err != nil {
    return errors.Wrapf(ctx, err, "create sarama client")
}
defer saramaClient.Close()
```

Add this before the `syncProducer` creation. The existing `syncProducer` uses `libkafka.NewSyncProducer` (which creates its own client internally) — both can coexist.

Required new imports in main.go:
- `libkafka "github.com/bborbe/kafka"` (may already be imported)
- `"github.com/bborbe/log"` (may already be imported)

### 6. Update `task/controller/mocks/mocks.go`

Read the existing `task/controller/mocks/mocks.go` to understand the annotation pattern, then add counterfeiter annotations for the two new interfaces:

```go
//counterfeiter:generate -o task_result_writer.go --fake-name FakeTaskResultWriter github.com/bborbe/agent/task/controller/pkg/writer.TaskResultWriter
//counterfeiter:generate -o task_result_command_handler.go --fake-name FakeTaskResultCommandHandler github.com/bborbe/agent/task/controller/pkg/handler.TaskResultCommandHandler
```

Then run `make generate` in task/controller to generate the mock files:

```bash
cd task/controller && make generate
```

### 7. Update `task/controller/pkg/factory/factory_test.go`

Read `task/controller/pkg/factory/factory_test.go` to understand existing test coverage, then add a test for `CreateResultConsumer` that verifies the function can be called without panicking and returns a non-nil consumer. Use `fake.NewClientset()` is not applicable here — instead use `libkafka.FakeSaramaClient` if available, or skip if the factory test only tests composition (check existing pattern).

If the existing factory test only compiles/wires (no runtime assertions), add a matching compile-time verification for `CreateResultConsumer`.

### 8. Update `docs/agent-job-interface.md`

In the **Pattern B: Job Interface** section, under **Environment Variables**, confirm the `TASK_ID` row is present (it was already added in a previous edit to the spec — verify it matches the table):

The table must include:

| `TASK_ID` | yes | Task identifier. Agent uses this when publishing `agent-task-v1-request` to reference which task to update. |

In the **Job Lifecycle** section, verify step 7 reads correctly:
```
7. task/controller consumes request, updates task file, git commit+push
```

If that step is present, no change needed. If not, add or correct it.

In the **task/executor Role** section, verify the Role description reflects that `TASK_ID` is now passed to Jobs (alongside `TASK_CONTENT`, `KAFKA_BROKERS`, `BRANCH`). Update the bulleted list if `TASK_ID` is missing from it.

### 9. Update CHANGELOG.md

Append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Add Kafka command consumer to task/controller to consume agent-task-v1-request and write results back to vault
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT change the `agent-task-v1-event` topic or the existing git-to-Kafka sync behavior
- task/controller must remain the single git writer — task/executor must NOT be modified in this prompt
- The consumer group must be exactly `"agent-task-controller-result"` — distinct from `"agent-task-executor"`
- `ConsumeMessage` must return `nil` (not error) for malformed messages, unknown task IDs, and already-written results — only return error for I/O failures and git push failures
- Do NOT use `context.Background()` inside `pkg/` — use the passed `ctx` parameter
- Do NOT use `fmt.Errorf` — use `errors.Wrapf(ctx, err, "message")` from `github.com/bborbe/errors`
- Do NOT add raw `go func()` goroutines — the concurrency is managed by `service.Run` and `run.CancelOnFirstErrorWait`
- The `CreateResultConsumer` factory function must have zero business logic — only composition and wiring
- Existing tests in task/controller must still pass after this change
- `make generate` must succeed after adding the counterfeiter annotations
</constraints>

<verification>
Generate mocks:
```bash
cd task/controller && make generate
```
Must exit 0.

Run handler unit tests:
```bash
cd task/controller && go test ./pkg/handler/...
```
Must exit 0.

Run all task/controller tests:
```bash
cd task/controller && make test
```
Must exit 0.

Run full precommit:
```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
