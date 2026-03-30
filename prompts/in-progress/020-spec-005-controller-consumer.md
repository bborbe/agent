---
status: approved
spec: ["005"]
created: "2026-03-29T20:15:00Z"
queued: "2026-03-30T13:58:01Z"
branch: dark-factory/agent-result-capture
---

<summary>
- Wire CQRS command consumer into task/controller to consume agent result requests from Kafka
- Add BoltDB for persistent Kafka offset storage (DataDir + NoSync CLI flags)
- Command executor decodes result payload and delegates to ResultWriter
- Runs alongside existing git-sync loop via service.Run
- Closes the feedback loop: agent publishes result → task/controller writes it to vault
</summary>

<objective>
Wire the CQRS command consumer into `task/controller` so it consumes `agent-task-v1-request` messages and calls `ResultWriter.WriteResult`. This is the final prompt for spec-005: after this, agent-published results flow end-to-end from Kafka into vault task files.
</objective>

<context>
Read CLAUDE.md for project conventions and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `task/controller/main.go` — the application struct and Run method to extend with BoltDB + consumer wiring
- `task/controller/pkg/factory/factory.go` — `CreateSyncLoop`; the new `CreateCommandConsumer` function goes here
- `task/controller/go.mod` — to add `github.com/bborbe/boltkv` as a direct dependency
- `task/controller/pkg/result/result_writer.go` — `ResultWriter` interface (written in prompt 2); used by the new command executor
- `lib/agent_task-file.go` — `TaskFile` (written in prompt 2); the command payload type
- `lib/agent_task-frontmatter.go` — `TaskFrontmatter` (written in prompt 2); typed map with Status/Phase/Assignee accessors
- `lib/agent_cdb-schema.go` — `TaskV1SchemaID`; use `TaskV1SchemaID` as the schema for the command consumer
- Reference CQRS command consumer API: `/home/node/go/pkg/mod/github.com/bborbe/cqrs@v0.2.3/cdb/cdb_run-command-consumer-tx.go`
- Reference `CommandObjectExecutorTx` interface: `/home/node/go/pkg/mod/github.com/bborbe/cqrs@v0.2.3/cdb/cdb_command-object-executor-tx.go`
- Reference boltkv API: `/home/node/go/pkg/mod/github.com/bborbe/boltkv@v1.11.6/boltkv_db.go` — use `boltkv.OpenDir(ctx, dataDir)`
</context>

<requirements>
### 1. Add `boltkv` direct dependency to `task/controller/go.mod`

Add `github.com/bborbe/boltkv v1.11.6` to the `require` block in `task/controller/go.mod` (it is already an indirect dep in `go.sum`; promote to direct).

After editing go.mod, run:
```bash
cd task/controller && go mod tidy
```

### 2. Extend `task/controller/main.go` — add CLI flags

Add two new fields to the `application` struct:

```go
DataDir string `required:"true"  arg:"data-dir" env:"DATA_DIR" usage:"directory for BoltDB offset storage"`
NoSync  bool   `required:"false" arg:"no-sync"  env:"NO_SYNC"  usage:"disable BoltDB fsync (for testing only)"`
```

### 3. Extend `task/controller/main.go` — open BoltDB and wire consumer

In the `Run` method, after the existing syncProducer setup, open BoltDB:

```go
var boltOptions []boltkv.ChangeOptions
if a.NoSync {
    boltOptions = append(boltOptions, func(opts *bolt.Options) {
        opts.NoSync = true
    })
}
db, err := boltkv.OpenDir(ctx, a.DataDir, boltOptions...)
if err != nil {
    return errors.Wrapf(ctx, err, "open boltkv dir %s", a.DataDir)
}
defer db.Close()
```

Import paths: `boltkv "github.com/bborbe/boltkv"` and `bolt "go.etcd.io/bbolt"`.

Create a `SaramaClientProvider` (needed by `cdb.RunCommandConsumerTxDefault`), then the `ResultWriter` and command consumer:

```go
saramaClientProvider, err := libkafka.NewSaramaClientProviderByType(
    ctx,
    libkafka.SaramaClientProviderTypeReused,
    libkafka.ParseBrokersFromString(a.KafkaBrokers),
)
if err != nil {
    return errors.Wrapf(ctx, err, "create sarama client provider")
}
defer saramaClientProvider.Close()

resultWriter := result.NewResultWriter(gitClient, a.TaskDir)
commandConsumer := factory.CreateCommandConsumer(
    saramaClientProvider,
    syncProducer,
    db,
    a.Branch,
    resultWriter,
)
```

Add `commandConsumer` to the `service.Run(ctx, ...)` call alongside the existing `syncLoop.Run` and `createHTTPServer`:

```go
return service.Run(
    ctx,
    syncLoop.Run,
    commandConsumer,
    a.createHTTPServer(syncLoop),
)
```

Import `"github.com/bborbe/agent/task/controller/pkg/result"` in main.go.

### 4. Create `task/controller/pkg/command/task_result_executor.go`

New package `command` — uses `cdb.CommandObjectExecutorTxFunc` adapter (same pattern as trading's `core/account/controller/pkg/command/command-object-executor-create.go`):

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command

import (
    "context"

    "github.com/bborbe/cqrs/base"
    "github.com/bborbe/cqrs/cdb"
    "github.com/bborbe/errors"
    libkv "github.com/bborbe/kv"
    "github.com/golang/glog"

    lib "github.com/bborbe/agent/lib"
    "github.com/bborbe/agent/task/controller/pkg/result"
)

// TaskResultCommandOperation is the CQRS command operation name for task result updates.
const TaskResultCommandOperation base.CommandOperation = "UpdateResult"

// NewTaskResultExecutor creates a cdb.CommandObjectExecutorTx for UpdateResult commands.
// Uses cdb.CommandObjectExecutorTxFunc adapter — same pattern as trading command handlers.
func NewTaskResultExecutor(writer result.ResultWriter) cdb.CommandObjectExecutorTx {
    return cdb.CommandObjectExecutorTxFunc(
        TaskResultCommandOperation,
        false, // sendResult: no result event needed
        func(ctx context.Context, tx libkv.Tx, commandObject cdb.CommandObject) (*base.EventID, base.Event, error) {
            var req lib.TaskFile
            if err := commandObject.Command.Data.MarshalInto(ctx, &req); err != nil {
                glog.Warningf("malformed TaskFile command, skipping: %v", err)
                return nil, nil, nil
            }
            if err := req.Validate(ctx); err != nil {
                glog.Warningf("invalid TaskFile (taskID=%s), skipping: %v", req.TaskIdentifier, err)
                return nil, nil, nil
            }
            if err := writer.WriteResult(ctx, req); err != nil {
                return nil, nil, errors.Wrapf(ctx, err, "write result for task %s", req.TaskIdentifier)
            }
            return nil, nil, nil
        },
    )
}
```

**Note on `commandObject.Command.Data`:** `base.Event` is `map[FieldName]interface{}`. Use `MarshalInto(ctx, &req)` to deserialize — same pattern as trading's `core/account/controller/pkg/command/command-object-executor-create.go`.

### 5. Add `CreateCommandConsumer` to `task/controller/pkg/factory/factory.go`

Extend the factory with a new pure-composition function (no business logic, no conditionals):

```go
// CreateCommandConsumer wires a CQRS command consumer for agent-task-v1-request.
func CreateCommandConsumer(
    saramaClientProvider libkafka.SaramaClientProvider,
    syncProducer libkafka.SyncProducer,
    db libkv.DB,
    branch base.Branch,
    resultWriter result.ResultWriter,
) run.Func {
    executor := command.NewTaskResultExecutor(resultWriter)
    return cdb.RunCommandConsumerTxDefault(
        saramaClientProvider,
        syncProducer,
        db,
        lib.TaskV1SchemaID,
        branch,
        true, // ignoreUnsupported: skip commands with unknown operations
        cdb.CommandObjectExecutorTxs{executor},
    )
}
```

Add the necessary imports: `"github.com/bborbe/agent/task/controller/pkg/command"`, `"github.com/bborbe/agent/task/controller/pkg/result"`, `libkv "github.com/bborbe/kv"`, `"github.com/bborbe/cqrs/cdb"`.

### 6. Create `task/controller/pkg/command/task_result_executor_test.go`

External test package (`package command_test`). Use Ginkgo/Gomega. Use counterfeiter mock for `result.ResultWriter`.

Required test cases:

- **Valid command** — `HandleCommand` with a well-formed JSON payload; verifies `WriteResult` is called once with the correct `TaskFile` (matching TaskIdentifier, Frontmatter, Content)
- **Malformed JSON** — `HandleCommand` with invalid JSON payload; verifies it returns `(nil, nil, nil)` and `WriteResult` is never called
- **Invalid request (empty task ID)** — valid JSON but `TaskIdentifier` is empty; verifies it returns `(nil, nil, nil)` and `WriteResult` is never called
- **WriteResult returns error** — `WriteResult` mock returns an error; verifies `HandleCommand` returns that error wrapped
- **`CommandOperation()`** — returns `"UpdateResult"`
- **`SendResultEnabled()`** — returns `false`

### 7. Create suite bootstrap `task/controller/pkg/command/command_suite_test.go`

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/onsi/gomega/format"
)

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

func TestCommand(t *testing.T) {
    time.Local = time.UTC
    format.TruncatedDiff = false
    RegisterFailHandler(Fail)
    RunSpecs(t, "Command Suite")
}
```

### 8. Update the K8s Deployment for task/controller

Add `DATA_DIR` and `NO_SYNC` env vars to `task/controller/k8s/agent-task-controller-sts.yaml` (StatefulSet):

```yaml
- name: DATA_DIR
  value: /data/bolt
- name: NO_SYNC
  value: "false"
```

task/controller is already a StatefulSet with a PVC. Use `/data/bolt` as the BoltDB directory. Read the manifest first to find the correct container env var location and verify the volume mount covers `/data/`.

### 9. Run `make generate` in task/controller

```bash
cd task/controller && make generate
```

This regenerates counterfeiter mocks for the new `ResultWriter` interface. Must exit 0.

### 10. Update `CHANGELOG.md`

Add or append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Wire CQRS command consumer in task/controller to consume agent-task-v1-request and write results to vault via ResultWriter
- feat: Add DataDir and NoSync CLI flags to task/controller for BoltDB Kafka offset persistence
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT change the `agent-task-v1-event` topic or the existing task/controller git-to-Kafka sync behavior
- task/controller remains the single git writer
- task/executor stays a Job launcher — no changes to task/executor in this prompt
- Consumer group for request consumption is distinct from the event-producing group — `cdb.RunCommandConsumerTxDefault` generates the consumer group name from `TaskV1SchemaID.CommandTopic(branch)`; this is different from the event topic, so no collision
- Factory function `CreateCommandConsumer` must have zero business logic (no conditionals, no loops, no I/O) — pure wiring only
- `ignoreUnsupported: true` in `RunCommandConsumerTxDefault` — commands with unknown operations are silently skipped
- BoltDB must be opened with `boltkv.OpenDir` (not `boltkv.OpenFile`) — creates the directory if it doesn't exist
- `task/controller/go.mod` must have `boltkv` as a direct dependency (not just indirect) after `go mod tidy`
- The BoltDB NoSync option (`func(opts *bolt.Options) { opts.NoSync = true }`) should only be appended when `a.NoSync == true` — never hardcode it
- Errors must be wrapped with `errors.Wrapf(ctx, err, "message")` — never `fmt.Errorf`
- Do NOT call `context.Background()` in pkg/ code — always thread ctx from the caller
- Test coverage ≥80% for the new `pkg/command/` package
</constraints>

<verification>
```bash
cd task/controller && go mod tidy
```
Must exit 0.

```bash
cd task/controller && make generate
```
Must exit 0 (regenerates counterfeiter mocks).

```bash
cd task/controller && make test
```
Must exit 0.

```bash
cd task/controller && go test -coverprofile=/tmp/cover.out -mod=vendor ./pkg/command/... && go tool cover -func=/tmp/cover.out
```
Statement coverage for `pkg/command/` must be ≥80%.

```bash
cd task/controller && make precommit
```
Must exit 0.
</verification>
