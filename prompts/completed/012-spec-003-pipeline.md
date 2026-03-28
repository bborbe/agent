---
status: completed
spec: ["003"]
summary: 'Implemented full task-to-prompt pipeline in prompt/controller: PromptPublisher, TaskEventHandler with DuplicateTracker, pure factory wiring, kafka-brokers/branch CLI flags in main.go, and full test coverage for handler and publisher packages.'
container: agent-012-spec-003-pipeline
dark-factory-version: v0.69.0
created: "2026-03-28T00:00:00Z"
queued: "2026-03-28T11:50:08Z"
started: "2026-03-28T11:53:50Z"
completed: "2026-03-28T12:05:06Z"
branch: dark-factory/task-to-prompt-consumer
---

<summary>
- prompt/controller gains two new CLI flags: kafka-brokers and branch
- The service continuously consumes task events from the agent-task-v1-event Kafka topic
- Task events with status=in_progress and a non-empty assignee produce exactly one prompt event
- Task events that fail the status or assignee check are silently skipped and their offset advanced
- Duplicate TaskIdentifiers (same task seen again within a process lifetime) are suppressed via an in-memory set
- Prompt events are published to the agent-prompt-v1-event topic via EventObjectSender (same stack as task/controller)
- Each published Prompt carries a new UUID PromptIdentifier, the source TaskIdentifier, Assignee, and Instruction derived from the task Content
- The Kafka consumer and HTTP server run concurrently; context cancellation stops both
- Graceful shutdown: cancelling the context stops the consumer loop and the HTTP server
</summary>

<objective>
Implement the full task-to-prompt pipeline in `prompt/controller`: a Kafka MessageHandler that converts qualifying task events into prompt events, a PromptPublisher backed by EventObjectSender, a pure factory that wires them together, and main.go changes to add the kafka-brokers/branch flags and run consumer + HTTP server concurrently. This is the second and final prompt for spec-003.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `prompt/controller/main.go` — current entry point; will be extended
- `prompt/controller/mocks/mocks.go` — placeholder for generated mocks
- `task/controller/main.go` — reference for Kafka wiring (SyncProducer → JSONSender → EventObjectSender)
- `task/controller/pkg/factory/factory.go` — reference for pure factory pattern
- `task/controller/pkg/publisher/task_publisher.go` — reference for EventObjectSender publish pattern
- `lib/agent_task.go` — `lib.Task` struct (frozen)
- `lib/agent_task-identifier.go` — `lib.TaskIdentifier` type (frozen)
- `lib/agent_prompt.go` — `lib.Prompt` struct (frozen)
- `lib/agent_prompt-identifier.go` — `lib.PromptIdentifier` type (frozen)
- `lib/agent_cdb-schema.go` — `lib.TaskV1SchemaID`, `lib.PromptV1SchemaID` (frozen)
</context>

<requirements>
### 1. Create `prompt/controller/pkg/publisher/prompt_publisher.go`

Define the `PromptPublisher` interface and its implementation backed by `cdb.EventObjectSender`. Pattern mirrors `task/controller/pkg/publisher/task_publisher.go`.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/google/uuid"
	"time"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/prompt_publisher.go --fake-name FakePromptPublisher . PromptPublisher

// PromptPublisher publishes prompt events to the event bus.
type PromptPublisher interface {
	PublishPrompt(ctx context.Context, prompt lib.Prompt) error
}

// NewPromptPublisher creates a new PromptPublisher backed by EventObjectSender.
func NewPromptPublisher(
	eventObjectSender cdb.EventObjectSender,
	schemaID cdb.SchemaID,
) PromptPublisher {
	return &promptPublisher{
		eventObjectSender: eventObjectSender,
		schemaID:          schemaID,
	}
}

type promptPublisher struct {
	eventObjectSender cdb.EventObjectSender
	schemaID          cdb.SchemaID
}

func (p *promptPublisher) PublishPrompt(ctx context.Context, prompt lib.Prompt) error {
	now := libtime.DateTime(time.Now())
	prompt.Object = base.Object[base.Identifier]{
		Identifier: base.Identifier(uuid.New().String()),
		Created:    now,
		Modified:   now,
	}
	event, err := base.ParseEvent(ctx, prompt)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for prompt %s failed", prompt.PromptIdentifier)
	}
	if err := p.eventObjectSender.SendUpdate(ctx, cdb.EventObject{
		Event:    event,
		ID:       base.EventID(prompt.PromptIdentifier),
		SchemaID: p.schemaID,
	}); err != nil {
		return errors.Wrapf(ctx, err, "publish prompt %s failed", prompt.PromptIdentifier)
	}
	return nil
}
```

### 2. Create `prompt/controller/pkg/handler/task_event_handler.go`

This file implements `kafka.MessageHandler`. It deserializes a Kafka message as `lib.Task`, filters it, deduplicates via an injected `DuplicateTracker`, and publishes a `lib.Prompt` via `PromptPublisher`.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"github.com/google/uuid"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
)

//counterfeiter:generate -o ../../mocks/duplicate_tracker.go --fake-name FakeDuplicateTracker . DuplicateTracker

// DuplicateTracker tracks which TaskIdentifiers have already produced a prompt in this process lifetime.
type DuplicateTracker interface {
	// IsDuplicate returns true if the given TaskIdentifier was already processed.
	IsDuplicate(id lib.TaskIdentifier) bool
	// MarkProcessed records the TaskIdentifier so future calls to IsDuplicate return true.
	MarkProcessed(id lib.TaskIdentifier)
}

// NewInMemoryDuplicateTracker creates a new in-memory DuplicateTracker.
func NewInMemoryDuplicateTracker() DuplicateTracker {
	return &inMemoryDuplicateTracker{
		seen: make(map[lib.TaskIdentifier]struct{}),
	}
}

type inMemoryDuplicateTracker struct {
	seen map[lib.TaskIdentifier]struct{}
}

func (t *inMemoryDuplicateTracker) IsDuplicate(id lib.TaskIdentifier) bool {
	_, ok := t.seen[id]
	return ok
}

func (t *inMemoryDuplicateTracker) MarkProcessed(id lib.TaskIdentifier) {
	t.seen[id] = struct{}{}
}

//counterfeiter:generate -o ../../mocks/task_event_handler.go --fake-name FakeTaskEventHandler . TaskEventHandler

// TaskEventHandler processes a single task event message from Kafka.
type TaskEventHandler interface {
	ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

// NewTaskEventHandler creates a new TaskEventHandler.
func NewTaskEventHandler(
	duplicateTracker DuplicateTracker,
	promptPublisher publisher.PromptPublisher,
) TaskEventHandler {
	return &taskEventHandler{
		duplicateTracker: duplicateTracker,
		promptPublisher:  promptPublisher,
	}
}

type taskEventHandler struct {
	duplicateTracker DuplicateTracker
	promptPublisher  publisher.PromptPublisher
}

func (h *taskEventHandler) ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if len(msg.Value) == 0 {
		glog.V(3).Infof("skip empty message at offset %d", msg.Offset)
		return nil
	}

	var task lib.Task
	if err := json.Unmarshal(msg.Value, &task); err != nil {
		glog.Warningf("failed to unmarshal task event at offset %d: %v", msg.Offset, err)
		return nil
	}

	if task.TaskIdentifier == "" {
		glog.Warningf("task event at offset %d has empty TaskIdentifier, skipping", msg.Offset)
		return nil
	}

	if task.Status != "in_progress" {
		glog.V(3).Infof("skip task %s with status %s", task.TaskIdentifier, task.Status)
		return nil
	}

	if task.Assignee == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		return nil
	}

	if h.duplicateTracker.IsDuplicate(task.TaskIdentifier) {
		glog.V(3).Infof("skip duplicate task %s", task.TaskIdentifier)
		return nil
	}

	prompt := lib.Prompt{
		PromptIdentifier: lib.PromptIdentifier(uuid.New().String()),
		TaskIdentifier:   task.TaskIdentifier,
		Assignee:         task.Assignee,
		Instruction:      lib.PromptInstruction(task.Content),
	}

	if err := h.promptPublisher.PublishPrompt(ctx, prompt); err != nil {
		return errors.Wrapf(ctx, err, "publish prompt for task %s failed", task.TaskIdentifier)
	}

	h.duplicateTracker.MarkProcessed(task.TaskIdentifier)
	glog.V(2).Infof("published prompt %s for task %s", prompt.PromptIdentifier, task.TaskIdentifier)
	return nil
}
```

**Important**: `TaskEventHandler` is defined as its own interface (not directly as `kafka.MessageHandler`) to allow counterfeiter to generate a mock. The concrete type satisfies `kafka.MessageHandler` since it implements `ConsumeMessage(ctx context.Context, msg *sarama.ConsumerMessage) error`.

### 3. Create `prompt/controller/pkg/factory/factory.go`

Pure composition — no conditionals, no I/O, no `context.Background()`.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/handler"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
)

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and publishes prompt events.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	eventObjectSender cdb.EventObjectSender,
	logSamplerFactory log.SamplerFactory,
) libkafka.Consumer {
	duplicateTracker := handler.NewInMemoryDuplicateTracker()
	promptPublisher := publisher.NewPromptPublisher(eventObjectSender, lib.PromptV1SchemaID)
	taskEventHandler := handler.NewTaskEventHandler(duplicateTracker, promptPublisher)
	topic := lib.TaskV1SchemaID.EventTopic(branch)
	offsetManager := libkafka.NewSaramaOffsetManager(
		saramaClient,
		libkafka.Group("agent-prompt-controller"),
		libkafka.OffsetOldest,
		libkafka.OffsetOldest,
	)
	return libkafka.NewOffsetConsumerHighwaterMarks(
		saramaClient,
		topic,
		offsetManager,
		taskEventHandler,
		nil,
		logSamplerFactory,
	)
}
```

Note: `libkafka.NewOffsetConsumerHighwaterMarks` takes a `run.Fire` as its 5th argument (trigger). Pass `nil` since prompt/controller does not use an HTTP trigger for the consumer — the consumer runs continuously.

Re-read the signature of `libkafka.NewOffsetConsumerHighwaterMarks` from the module cache at `/home/node/go/pkg/mod/github.com/bborbe/kafka@v1.22.9/kafka_consumer-offset-highwater-marks.go` (or v1.22.8 if 9 is not cached) before writing this. Adjust parameter order if needed.

### 4. Update `prompt/controller/main.go`

Add `KafkaBrokers` and `Branch` fields to the `application` struct. Wire up the consumer in `Run()`. Run consumer and HTTP server concurrently via `service.Run`.

Replace the entire file contents:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libhttp "github.com/bborbe/http"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bborbe/agent/prompt/controller/pkg/factory"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN    string      `required:"true"  arg:"sentry-dsn"    env:"SENTRY_DSN"    usage:"SentryDSN"            display:"length"`
	SentryProxy  string      `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"  usage:"Sentry Proxy"`
	Listen       string      `required:"true"  arg:"listen"        env:"LISTEN"        usage:"address to listen to"`
	KafkaBrokers string      `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"comma-separated Kafka broker addresses"`
	Branch       base.Branch `required:"true"  arg:"branch"        env:"BRANCH"        usage:"Kafka topic prefix branch (develop/live)"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-prompt-controller started")

	saramaClient, err := libkafka.NewSaramaClient(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create sarama client")
	}
	defer saramaClient.Close()

	syncProducer, err := libkafka.NewSyncProducerFromClient(saramaClient)
	if err != nil {
		return errors.Wrapf(ctx, err, "create kafka sync producer")
	}
	defer syncProducer.Close()

	eventObjectSender := cdb.NewEventObjectSender(
		libkafka.NewJSONSender(syncProducer, log.DefaultSamplerFactory),
		a.Branch,
		log.DefaultSamplerFactory,
	)

	consumer := factory.CreateConsumer(
		saramaClient,
		a.Branch,
		eventObjectSender,
		log.DefaultSamplerFactory,
	)

	return service.Run(
		ctx,
		func(ctx context.Context) error {
			return consumer.Consume(ctx)
		},
		a.createHTTPServer(),
	)
}

func (a *application) createHTTPServer() run.Func {
	return func(ctx context.Context) error {
		router := mux.NewRouter()
		router.Path("/healthz").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/readiness").Handler(libhttp.NewPrintHandler("OK"))
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/setloglevel/{level}").
			Handler(log.NewSetLoglevelHandler(ctx, log.NewLogLevelSetter(2, 5*time.Minute)))

		glog.V(2).Infof("starting http server listen on %s", a.Listen)
		return libhttp.NewServer(
			a.Listen,
			router,
		).Run(ctx)
	}
}
```

**Important**: Before writing main.go, check the actual function names for creating a Sarama client and a SyncProducer from a client in `/home/node/go/pkg/mod/github.com/bborbe/kafka@v1.22.9/` (or v1.22.8). Look for:
- A constructor that creates a `sarama.Client` from broker addresses (search for `NewSaramaClient`, `NewClient`, etc.)
- A constructor that wraps a `sarama.Client` to get a `SyncProducer` (search for `NewSyncProducerFromClient`, etc.)

If `NewSaramaClient` does not exist, use `libkafka.NewSaramaClientNew` or the correct name. If `NewSyncProducerFromClient` does not exist, create the SyncProducer via `libkafka.NewSyncProducer(ctx, brokers)` as in task/controller (does not require a shared client), and pass the client separately to the factory. Adjust accordingly.

### 5. Create `prompt/controller/pkg/handler/task_event_handler_test.go`

Test all paths: status filter, assignee filter, dedup, successful publish, publish error (do not mark as processed on error).

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IBM/sarama"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/mocks"
	"github.com/bborbe/agent/prompt/controller/pkg/handler"
	"github.com/bborbe/errors"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handler Suite")
}

var _ = Describe("TaskEventHandler", func() {
	var (
		ctx              context.Context
		fakeTracker      *mocks.FakeDuplicateTracker
		fakePublisher    *mocks.FakePromptPublisher
		h                handler.TaskEventHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeTracker = new(mocks.FakeDuplicateTracker)
		fakePublisher = new(mocks.FakePromptPublisher)
		h = handler.NewTaskEventHandler(fakeTracker, fakePublisher)
	})

	buildMsg := func(task lib.Task) *sarama.ConsumerMessage {
		value, err := json.Marshal(task)
		Expect(err).To(BeNil())
		return &sarama.ConsumerMessage{Value: value}
	}

	Describe("ConsumeMessage", func() {
		It("skips empty message", func() {
			err := h.ConsumeMessage(ctx, &sarama.ConsumerMessage{Value: []byte{}})
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("skips malformed JSON without error", func() {
			err := h.ConsumeMessage(ctx, &sarama.ConsumerMessage{Value: []byte("not-json")})
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("skips task with empty TaskIdentifier", func() {
			task := lib.Task{Status: "in_progress", Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("skips task with status != in_progress", func() {
			task := lib.Task{TaskIdentifier: "tid-1", Status: "todo", Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("skips task with empty assignee", func() {
			task := lib.Task{TaskIdentifier: "tid-2", Status: "in_progress", Assignee: ""}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("skips duplicate TaskIdentifier", func() {
			fakeTracker.IsDuplicateReturns(true)
			task := lib.Task{TaskIdentifier: "tid-3", Status: "in_progress", Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(0))
		})

		It("publishes prompt for qualifying task", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{
				TaskIdentifier: "tid-4",
				Status:         "in_progress",
				Assignee:       "claude",
				Content:        "do the thing",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakePublisher.PublishPromptCallCount()).To(Equal(1))
			_, published := fakePublisher.PublishPromptArgsForCall(0)
			Expect(string(published.TaskIdentifier)).To(Equal("tid-4"))
			Expect(string(published.Assignee)).To(Equal("claude"))
			Expect(string(published.Instruction)).To(Equal("do the thing"))
			Expect(string(published.PromptIdentifier)).NotTo(BeEmpty())
		})

		It("marks task as processed after successful publish", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{TaskIdentifier: "tid-5", Status: "in_progress", Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(1))
			marked := fakeTracker.MarkProcessedArgsForCall(0)
			Expect(marked).To(Equal(lib.TaskIdentifier("tid-5")))
		})

		It("does not mark task as processed when publish fails", func() {
			fakeTracker.IsDuplicateReturns(false)
			fakePublisher.PublishPromptReturns(errors.Errorf(ctx, "kafka unavailable"))
			task := lib.Task{TaskIdentifier: "tid-6", Status: "in_progress", Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).NotTo(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(0))
		})
	})
})
```

### 6. Create `prompt/controller/pkg/publisher/prompt_publisher_test.go`

Test the happy path (verifies that PublishPrompt calls SendUpdate with the right SchemaID and a non-empty PromptIdentifier) and the error path (SendUpdate returns an error → PublishPrompt returns an error).

Use a mock for `cdb.EventObjectSender`. The counterfeiter annotation for `cdb.EventObjectSender` is in `github.com/bborbe/cqrs/cdb`; you can reference the existing generated mock at `github.com/bborbe/cqrs/mocks` or generate a local one.

For the publisher test, create a simple fake inline (using `cdb.EventObjectSenderFunc`) rather than counterfeiter:

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher_test

import (
	"context"
	"testing"

	"github.com/bborbe/cqrs/cdb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
	"github.com/bborbe/errors"
)

func TestPublisher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Publisher Suite")
}

var _ = Describe("PromptPublisher", func() {
	var (
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("publishes prompt via EventObjectSender", func() {
		var capturedEvent cdb.EventObject
		sender := cdb.EventObjectSenderFunc(
			func(c context.Context, e cdb.EventObject) error {
				capturedEvent = e
				return nil
			},
			func(c context.Context, e cdb.EventObject) error {
				return errors.Errorf(c, "unexpected delete")
			},
		)
		p := publisher.NewPromptPublisher(sender, lib.PromptV1SchemaID)
		prompt := lib.Prompt{
			PromptIdentifier: lib.PromptIdentifier("prompt-uuid-1"),
			TaskIdentifier:   lib.TaskIdentifier("task-uuid-1"),
			Assignee:         lib.TaskAssignee("claude"),
			Instruction:      lib.PromptInstruction("do the thing"),
		}
		err := p.PublishPrompt(ctx, prompt)
		Expect(err).To(BeNil())
		Expect(capturedEvent.SchemaID).To(Equal(lib.PromptV1SchemaID))
		Expect(string(capturedEvent.ID)).To(Equal("prompt-uuid-1"))
	})

	It("returns error when EventObjectSender fails", func() {
		sender := cdb.EventObjectSenderFunc(
			func(c context.Context, e cdb.EventObject) error {
				return errors.Errorf(c, "kafka down")
			},
			func(c context.Context, e cdb.EventObject) error { return nil },
		)
		p := publisher.NewPromptPublisher(sender, lib.PromptV1SchemaID)
		prompt := lib.Prompt{
			PromptIdentifier: lib.PromptIdentifier("prompt-uuid-2"),
			TaskIdentifier:   lib.TaskIdentifier("task-uuid-2"),
		}
		err := p.PublishPrompt(ctx, prompt)
		Expect(err).NotTo(BeNil())
	})
})
```

### 7. Run `make generate` to create mocks

```bash
cd prompt/controller && make generate
```

This runs counterfeiter for the `//counterfeiter:generate` annotations in:
- `pkg/publisher/prompt_publisher.go` → `mocks/prompt_publisher.go`
- `pkg/handler/task_event_handler.go` → `mocks/duplicate_tracker.go` and `mocks/task_event_handler.go`

If `make generate` is not configured to run counterfeiter automatically, run:
```bash
cd prompt/controller && go generate ./...
```

### 8. Update `CHANGELOG.md`

Add or append to `## Unreleased` in the root `CHANGELOG.md`:

```
## Unreleased

- feat: Add Kafka task event consumer to prompt/controller that converts in-progress tasks into prompt events
- feat: Add kafka-brokers and branch CLI flags to prompt/controller
```

### 9. Run `make test` in `prompt/controller/`

```bash
cd prompt/controller && make test
```

All tests must pass with exit code 0.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types (`lib.Task`, `lib.Prompt`, `lib.TaskV1SchemaID`, `lib.PromptV1SchemaID`) — these are frozen
- Existing HTTP endpoints (healthz, readiness, metrics) must continue working unchanged
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have `//counterfeiter:generate` annotations
- All tests must use external test packages (`package handler_test`, `package publisher_test`)
- Counterfeiter mocks must be regenerated with `make generate` after adding annotations
- Consumer must respect context cancellation: the `Consumer.Consume(ctx)` method returns when ctx is done
- In-memory duplicate tracker is sufficient for MVP — do NOT add persistent storage
- Consumer group ID must be deterministic and service-specific: use `"agent-prompt-controller"`
- The `TaskEventHandler` type satisfies `kafka.MessageHandler` (same method signature)
- Error on publish → return error from ConsumeMessage (offset NOT advanced, event will be retried)
- Malformed JSON → log warning, return nil (offset advanced, event is skipped)
- `make precommit` must pass before declaring done
</constraints>

<verification>
```bash
cd prompt/controller && make generate
```
Must exit 0 and produce mock files in `mocks/`.

```bash
cd prompt/controller && make test
```
Must exit 0.

```bash
cd prompt/controller && make precommit
```
Must exit 0.

Verify coverage for changed packages:
```bash
cd prompt/controller && go test -coverprofile=/tmp/cover.out ./pkg/handler/... ./pkg/publisher/... && go tool cover -func=/tmp/cover.out
```
Statement coverage for `handler` and `publisher` packages must be ≥ 80%.
</verification>
