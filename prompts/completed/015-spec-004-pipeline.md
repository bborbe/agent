---
status: completed
spec: [004-task-executor-service]
summary: 'Implemented task/executor pipeline: JobSpawner (K8s batch/v1), TaskEventHandler (status/phase/assignee filters + dedup), pure factory wiring, full main.go with Kafka consumer + HTTP server, and complete test coverage (handler 88.9%, spawner 100%)'
container: agent-015-spec-004-pipeline
dark-factory-version: v0.69.0
created: "2026-03-29T13:00:00Z"
queued: "2026-03-29T14:35:59Z"
started: "2026-03-29T14:40:26Z"
completed: "2026-03-29T14:51:37Z"
branch: dark-factory/task-executor-service
---

<summary>
- task/executor consumes task events from the agent-task-v1-event Kafka topic
- Events are filtered: only status=in_progress, phase in {planning, in_progress, ai_review}, and non-empty assignee pass through
- An in-memory deduplication tracker prevents spawning a second Job for a task ID already seen in this process lifetime
- The assignee field is resolved to a container image via a hardcoded map; unknown assignees log a warning and are skipped
- A K8s batch/v1 Job is spawned for each qualifying task: name agent-{taskID-short}, env TASK_CONTENT/KAFKA_BROKERS/BRANCH, restartPolicy=Never, backoffLimit=0
- K8s AlreadyExists errors are handled gracefully (treated as success, task marked as processed)
- K8s API unreachable returns an error; Kafka consumer retries the message on the next poll cycle
- All external dependencies (K8s client, event handler) are injected and fully mockable
- Full test coverage ≥80% for all new packages
</summary>

<objective>
Implement the full task event processing pipeline in `task/executor`: a TaskEventHandler with filtering, deduplication, and assignee-to-image resolution, a JobSpawner interface backed by the K8s batch/v1 client, a pure factory for wiring, and a complete main.go with Kafka consumer + HTTP server running concurrently. This is the second prompt for spec-004.
</objective>

<context>
Read CLAUDE.md for project conventions, and all relevant `go-*.md` docs in `/home/node/.claude/docs/`.

Key files to read before making changes:
- `task/executor/main.go` — current bare main; will be extended with Kafka and K8s wiring
- `task/executor/mocks/mocks.go` — placeholder for generated mocks
- `prompt/controller/main.go` — reference for Kafka wiring (CreateSaramaClient → NewSyncProducerFromSaramaClient → EventObjectSender)
- `prompt/controller/pkg/factory/factory.go` — reference for pure factory pattern
- `prompt/controller/pkg/handler/task_event_handler.go` — reference for filtering and dedup patterns
- `lib/agent_task.go` — lib.Task struct (frozen; has TaskIdentifier, Status, Assignee, Content, Phase fields)
- `lib/agent_task-identifier.go` — lib.TaskIdentifier type (frozen)
- `lib/agent_cdb-schema.go` — lib.TaskV1SchemaID (frozen)

Phase constants are in `github.com/bborbe/vault-cli/pkg/domain`:
```go
TaskPhasePlanning    TaskPhase = "planning"
TaskPhaseInProgress  TaskPhase = "in_progress"
TaskPhaseAIReview    TaskPhase = "ai_review"
```
The `Phase` field is `*domain.TaskPhase` (pointer). The `domain.TaskPhases` type has `.Contains(phase)` method.

Before writing main.go, verify the exact function names for Sarama client construction in the `prompt/controller/main.go` (which uses `libkafka.CreateSaramaClient` and `libkafka.NewSyncProducerFromSaramaClient`). Use those same names in task/executor.

For the K8s client, use `k8s.io/client-go/kubernetes`. The in-cluster config is at `k8s.io/client-go/rest`. Import paths:
- `k8s.io/client-go/kubernetes` — `kubernetes.NewForConfig`
- `k8s.io/client-go/rest` — `rest.InClusterConfig()`
- `k8s.io/api/batch/v1` — `batchv1.Job`
- `k8s.io/apimachinery/pkg/apis/meta/v1` — `metav1.ObjectMeta`, `metav1.CreateOptions`
- `k8s.io/apimachinery/pkg/api/errors` — `k8serrors.IsAlreadyExists`
</context>

<requirements>
### 1. Create `task/executor/pkg/spawner/job_spawner.go`

Define the `JobSpawner` interface and a K8s implementation. The implementation creates a `batch/v1 Job` in the given namespace.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spawner

import (
	"context"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/job_spawner.go --fake-name FakeJobSpawner . JobSpawner

// JobSpawner creates a K8s Job for a task.
type JobSpawner interface {
	SpawnJob(ctx context.Context, task lib.Task, image string) error
}

// NewJobSpawner creates a new JobSpawner backed by the K8s batch/v1 API.
func NewJobSpawner(
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	branch string,
) JobSpawner {
	return &jobSpawner{
		kubeClient:   kubeClient,
		namespace:    namespace,
		kafkaBrokers: kafkaBrokers,
		branch:       branch,
	}
}

type jobSpawner struct {
	kubeClient   kubernetes.Interface
	namespace    string
	kafkaBrokers string
	branch       string
}

func (s *jobSpawner) SpawnJob(ctx context.Context, task lib.Task, image string) error {
	jobName := jobNameFromTask(task.TaskIdentifier)
	backoffLimit := int32(0)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: s.namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "agent",
							Image: image,
							Env: []corev1.EnvVar{
								{Name: "TASK_CONTENT", Value: string(task.Content)},
								{Name: "KAFKA_BROKERS", Value: s.kafkaBrokers},
								{Name: "BRANCH", Value: s.branch},
							},
						},
					},
				},
			},
		},
	}

	_, err := s.kubeClient.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			glog.V(2).Infof("job %s already exists for task %s, treating as success", jobName, task.TaskIdentifier)
			return nil
		}
		return errors.Wrapf(ctx, err, "create job %s for task %s failed", jobName, task.TaskIdentifier)
	}
	glog.V(2).Infof("created job %s for task %s with image %s", jobName, task.TaskIdentifier, image)
	return nil
}

// jobNameFromTask returns the K8s Job name for a task: "agent-{first-8-chars-of-taskID}".
func jobNameFromTask(taskID lib.TaskIdentifier) string {
	id := string(taskID)
	if len(id) > 8 {
		id = id[:8]
	}
	return "agent-" + id
}
```

Note: `lib.Task.Content` may be a string type alias — read `lib/agent_task.go` to confirm the field type and cast appropriately.

### 2. Create `task/executor/pkg/handler/task_event_handler.go`

Implements `kafka.MessageHandler`. Deserializes a Kafka message as `lib.Task`, applies all filters in order, deduplicates via injected `DuplicateTracker`, resolves assignee to image, then spawns a K8s Job via `JobSpawner`.

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
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// allowedPhases lists the task phases that qualify for job spawning.
var allowedPhases = domain.TaskPhases{
	domain.TaskPhasePlanning,
	domain.TaskPhaseInProgress,
	domain.TaskPhaseAIReview,
}

//counterfeiter:generate -o ../../mocks/duplicate_tracker.go --fake-name FakeDuplicateTracker . DuplicateTracker

// DuplicateTracker tracks which TaskIdentifiers have already spawned a Job in this process lifetime.
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
	jobSpawner spawner.JobSpawner,
	assigneeImages map[string]string,
) TaskEventHandler {
	return &taskEventHandler{
		duplicateTracker: duplicateTracker,
		jobSpawner:       jobSpawner,
		assigneeImages:   assigneeImages,
	}
}

type taskEventHandler struct {
	duplicateTracker DuplicateTracker
	jobSpawner       spawner.JobSpawner
	assigneeImages   map[string]string
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

	if task.Phase == nil || !allowedPhases.Contains(*task.Phase) {
		glog.V(3).Infof("skip task %s with phase %v", task.TaskIdentifier, task.Phase)
		return nil
	}

	if task.Assignee == "" {
		glog.V(3).Infof("skip task %s with empty assignee", task.TaskIdentifier)
		return nil
	}

	image, ok := h.assigneeImages[string(task.Assignee)]
	if !ok {
		glog.Warningf("skip task %s: unknown assignee %s", task.TaskIdentifier, task.Assignee)
		return nil
	}

	if h.duplicateTracker.IsDuplicate(task.TaskIdentifier) {
		glog.V(3).Infof("skip duplicate task %s", task.TaskIdentifier)
		return nil
	}

	if err := h.jobSpawner.SpawnJob(ctx, task, image); err != nil {
		return errors.Wrapf(ctx, err, "spawn job for task %s failed", task.TaskIdentifier)
	}

	h.duplicateTracker.MarkProcessed(task.TaskIdentifier)
	glog.V(2).Infof("spawned job for task %s (assignee=%s)", task.TaskIdentifier, task.Assignee)
	return nil
}
```

**Note on filter order:**
1. Empty message → skip (nil return)
2. Unmarshal error → log warning, skip (nil return)
3. Empty TaskIdentifier → log warning, skip (nil return)
4. Status != "in_progress" → skip (nil return)
5. Phase nil or not in allowedPhases → skip (nil return)
6. Empty Assignee → skip (nil return)
7. Unknown assignee → log warning, skip (nil return)
8. Duplicate → skip (nil return)
9. Spawn job → return error if fails
10. Mark processed

**Important**: Unknown assignee returns `nil` (offset advanced, event skipped), NOT an error. Only K8s spawn failure returns a non-nil error (which causes Kafka to retry).

### 3. Create `task/executor/pkg/factory/factory.go`

Pure composition — no conditionals, no I/O, no `context.Background()`.

The factory wires together a Kafka consumer that reads from `agent-task-v1-event` and dispatches to the task event handler. The assignee-to-image map is passed in (injected from main).

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"k8s.io/client-go/kubernetes"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg/handler"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and spawns K8s Jobs for qualifying tasks.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	assigneeImages map[string]string,
	logSamplerFactory log.SamplerFactory,
) libkafka.Consumer {
	jobSpawner := spawner.NewJobSpawner(kubeClient, namespace, kafkaBrokers, string(branch))
	duplicateTracker := handler.NewInMemoryDuplicateTracker()
	taskEventHandler := handler.NewTaskEventHandler(duplicateTracker, jobSpawner, assigneeImages)
	topic := lib.TaskV1SchemaID.EventTopic(branch)
	offsetManager := libkafka.NewSaramaOffsetManager(
		saramaClient,
		libkafka.Group("agent-task-executor"),
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

**Before writing this file:** Check the actual signatures for `libkafka.NewSaramaOffsetManager` and `libkafka.NewOffsetConsumerHighwaterMarks` in `/home/node/go/pkg/mod/github.com/bborbe/kafka@v1.22.9/`. Look at the reference in `prompt/controller/pkg/factory/factory.go` for the correct parameter order and types.

Also check `lib.TaskV1SchemaID.EventTopic(branch)` — read `lib/agent_cdb-schema.go` to confirm the method exists and its signature.

### 4. Update `task/executor/main.go`

Replace the entire file with the full implementation that adds KafkaBrokers, Branch, Namespace CLI flags and wires up the K8s client and Kafka consumer:

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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bborbe/agent/task/executor/pkg/factory"
)

// assigneeImages maps assignee names to container images.
// Add new assignees here when new agent types are onboarded.
var assigneeImages = map[string]string{
	"claude": "docker.quant.benjamin-borbe.de:443/agent-claude:develop",
}

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN    string      `required:"true"  arg:"sentry-dsn"    env:"SENTRY_DSN"    usage:"SentryDSN"                                display:"length"`
	SentryProxy  string      `required:"false" arg:"sentry-proxy"  env:"SENTRY_PROXY"  usage:"Sentry Proxy"`
	Listen       string      `required:"true"  arg:"listen"        env:"LISTEN"        usage:"address to listen to"`
	KafkaBrokers string      `required:"true"  arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"comma-separated Kafka broker addresses"`
	Branch       base.Branch `required:"true"  arg:"branch"        env:"BRANCH"        usage:"Kafka topic prefix branch (develop/live)"`
	Namespace    string      `required:"true"  arg:"namespace"     env:"NAMESPACE"     usage:"K8s namespace to spawn Jobs in"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(1).Infof("agent-task-executor started")

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return errors.Wrapf(ctx, err, "get in-cluster k8s config")
	}
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return errors.Wrapf(ctx, err, "create k8s client")
	}

	saramaClient, err := libkafka.CreateSaramaClient(
		ctx,
		libkafka.ParseBrokersFromString(a.KafkaBrokers),
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "create sarama client")
	}
	defer saramaClient.Close()

	consumer := factory.CreateConsumer(
		saramaClient,
		a.Branch,
		kubeClient,
		a.Namespace,
		a.KafkaBrokers,
		assigneeImages,
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

**Important**: Before writing main.go, verify the exact function names in `prompt/controller/main.go`:
- `libkafka.CreateSaramaClient` (or `NewSaramaClient` — use whichever is in prompt/controller/main.go)
- Check if `libkafka.ParseBrokersFromString` exists — if not, use the correct helper

**Important**: The `assigneeImages` map is defined at package level in main.go. It is hardcoded — never read from config files or env vars. Add a comment explaining how to add new agents.

**Note on K8s client**: `rest.InClusterConfig()` only works when running inside a K8s Pod. For local development, it will fail — this is expected. The service is designed to run in-cluster.

**Note on NAMESPACE env var**: The namespace is passed as a CLI flag (`--namespace` / env `NAMESPACE`). The K8s downward API can inject it as an env var from the Pod metadata.

### 5. Create `task/executor/pkg/spawner/job_spawner_test.go`

Test the happy path, AlreadyExists graceful handling, and K8s error propagation.

```go
// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spawner_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/kubernetes/fake"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

func TestSpawner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Spawner Suite")
}

var _ = Describe("JobSpawner", func() {
	var (
		ctx          context.Context
		fakeClient   *fake.Clientset
		jobSpawner   spawner.JobSpawner
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = fake.NewClientset()
		jobSpawner = spawner.NewJobSpawner(fakeClient, "test-ns", "kafka:9092", "develop")
	})

	Describe("SpawnJob", func() {
		It("creates a job with correct name and env vars", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
				Assignee:       lib.TaskAssignee("claude"),
				Content:        "do the work",
			}
			err := jobSpawner.SpawnJob(ctx, task, "my-image:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items).To(HaveLen(1))

			job := jobs.Items[0]
			Expect(job.Name).To(Equal("agent-abc12345"))
			Expect(job.Namespace).To(Equal("test-ns"))
			Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
			Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))

			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("my-image:latest"))

			envMap := make(map[string]string)
			for _, e := range container.Env {
				envMap[e.Name] = e.Value
			}
			Expect(envMap["TASK_CONTENT"]).To(Equal("do the work"))
			Expect(envMap["KAFKA_BROKERS"]).To(Equal("kafka:9092"))
			Expect(envMap["BRANCH"]).To(Equal("develop"))
		})

		It("truncates task ID to 8 characters in job name", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abcdefghijklmnop"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(Equal("agent-abcdefgh"))
		})

		It("handles short task ID without panic", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())

			jobs, err := fakeClient.BatchV1().Jobs("test-ns").List(ctx, metav1.ListOptions{})
			Expect(err).To(BeNil())
			Expect(jobs.Items[0].Name).To(Equal("agent-abc"))
		})

		It("returns nil when job already exists (AlreadyExists)", func() {
			existingJob := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-abc12345",
					Namespace: "test-ns",
				},
			}
			fakeClient = fake.NewClientset(existingJob)
			jobSpawner = spawner.NewJobSpawner(fakeClient, "test-ns", "kafka:9092", "develop")

			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345-rest-ignored"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).To(BeNil())
		})

		It("returns error on unexpected K8s error", func() {
			fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, k8serrors.NewInternalError(fmt.Errorf("server error"))
			})
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("abc12345"),
			}
			err := jobSpawner.SpawnJob(ctx, task, "img:latest")
			Expect(err).NotTo(BeNil())
		})
	})
})
```

**Note**: Run `go mod tidy` after writing to resolve any missing K8s test dependencies.

### 6. Create `task/executor/pkg/handler/task_event_handler_test.go`

Test all filter paths and the full happy path. Mirror the pattern from `prompt/controller/pkg/handler/task_event_handler_test.go`.

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
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/mocks"
	"github.com/bborbe/agent/task/executor/pkg/handler"
	"github.com/bborbe/errors"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handler Suite")
}

var _ = Describe("TaskEventHandler", func() {
	var (
		ctx            context.Context
		fakeTracker    *mocks.FakeDuplicateTracker
		fakeSpawner    *mocks.FakeJobSpawner
		assigneeImages map[string]string
		h              handler.TaskEventHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeTracker = new(mocks.FakeDuplicateTracker)
		fakeSpawner = new(mocks.FakeJobSpawner)
		assigneeImages = map[string]string{
			"claude": "my-image:latest",
		}
		h = handler.NewTaskEventHandler(fakeTracker, fakeSpawner, assigneeImages)
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
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips malformed JSON without error", func() {
			err := h.ConsumeMessage(ctx, &sarama.ConsumerMessage{Value: []byte("not-json")})
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with empty TaskIdentifier", func() {
			task := lib.Task{Status: "in_progress", Phase: domain.TaskPhaseInProgress.Ptr(), Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with status != in_progress", func() {
			task := lib.Task{
				TaskIdentifier: "tid-1",
				Status:         "todo",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       "claude",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with nil phase", func() {
			task := lib.Task{TaskIdentifier: "tid-2", Status: "in_progress", Phase: nil, Assignee: "claude"}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase todo", func() {
			task := lib.Task{
				TaskIdentifier: "tid-3",
				Status:         "in_progress",
				Phase:          domain.TaskPhaseTodo.Ptr(),
				Assignee:       "claude",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase human_review", func() {
			task := lib.Task{
				TaskIdentifier: "tid-4",
				Status:         "in_progress",
				Phase:          domain.TaskPhaseHumanReview.Ptr(),
				Assignee:       "claude",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with empty assignee", func() {
			task := lib.Task{
				TaskIdentifier: "tid-5",
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       "",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips unknown assignee without error", func() {
			task := lib.Task{
				TaskIdentifier: "tid-6",
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       "unknown-agent",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips duplicate TaskIdentifier", func() {
			fakeTracker.IsDuplicateReturns(true)
			task := lib.Task{
				TaskIdentifier: "tid-7",
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       "claude",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("spawns job for qualifying task with known assignee", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-8"),
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       lib.TaskAssignee("claude"),
				Content:        "do the work",
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			_, spawnedTask, image := fakeSpawner.SpawnJobArgsForCall(0)
			Expect(string(spawnedTask.TaskIdentifier)).To(Equal("tid-8"))
			Expect(image).To(Equal("my-image:latest"))
		})

		It("marks task as processed after successful spawn", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-9"),
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       lib.TaskAssignee("claude"),
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(1))
			Expect(fakeTracker.MarkProcessedArgsForCall(0)).To(Equal(lib.TaskIdentifier("tid-9")))
		})

		It("does not mark task as processed when spawn fails", func() {
			fakeTracker.IsDuplicateReturns(false)
			fakeSpawner.SpawnJobReturns(errors.Errorf(ctx, "k8s unavailable"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-10"),
				Status:         "in_progress",
				Phase:          domain.TaskPhaseInProgress.Ptr(),
				Assignee:       lib.TaskAssignee("claude"),
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).NotTo(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(0))
		})

		It("accepts task with phase planning", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-11"),
				Status:         "in_progress",
				Phase:          domain.TaskPhasePlanning.Ptr(),
				Assignee:       lib.TaskAssignee("claude"),
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("accepts task with phase ai_review", func() {
			fakeTracker.IsDuplicateReturns(false)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-12"),
				Status:         "in_progress",
				Phase:          domain.TaskPhaseAIReview.Ptr(),
				Assignee:       lib.TaskAssignee("claude"),
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})
	})
})
```

### 7. Run `make generate` to create mocks

```bash
cd task/executor && make generate
```

This runs counterfeiter for the `//counterfeiter:generate` annotations in:
- `pkg/spawner/job_spawner.go` → `mocks/job_spawner.go`
- `pkg/handler/task_event_handler.go` → `mocks/duplicate_tracker.go` and `mocks/task_event_handler.go`

If `make generate` is not configured, run:
```bash
cd task/executor && go generate ./...
```

### 8. Run `make test`

```bash
cd task/executor && make test
```

All tests must pass. If any test fails due to missing `corev1` import in spawner_test.go, add the import. If `fakeClient.BatchV1().Jobs(...)` is needed for reactor-based tests, simplify to use `fake.NewClientset(existingJob)` instead.

### 9. Update `CHANGELOG.md`

Append to `## Unreleased` in the root `CHANGELOG.md`:

```
- feat: Implement task/executor pipeline with TaskEventHandler (status/phase/assignee filters, dedup), JobSpawner (K8s batch/v1), and factory wiring
```
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Do NOT modify `lib/` types (`lib.Task`, `lib.TaskIdentifier`, `lib.TaskV1SchemaID`) — these are frozen
- Consumer group must be `"agent-task-executor"` — distinct from prompt/controller's `"agent-prompt-controller"`
- The `agent-task-v1-event` Kafka topic must not be changed
- Use `github.com/bborbe/errors` for all error wrapping — never `fmt.Errorf`
- Factory functions must have zero business logic — no conditionals, no I/O, no `context.Background()`
- All new interfaces must have `//counterfeiter:generate` annotations
- All tests must use external test packages (`package spawner_test`, `package handler_test`)
- Counterfeiter mocks must be regenerated with `make generate` after adding annotations
- Unknown assignee → return nil (skip, commit offset) — NOT an error
- K8s AlreadyExists → return nil (treat as success, mark processed in dedup tracker)
- K8s other errors → return error (Kafka retries message)
- Spawn failure → do NOT call MarkProcessed
- Job name format: `agent-{first-8-chars-of-taskID}`
- Job env vars: `TASK_CONTENT`, `KAFKA_BROKERS`, `BRANCH`
- `restartPolicy=Never`, `backoffLimit=0`
- Assignee-to-image map is hardcoded in main.go — never read from external config
- `make precommit` must pass before declaring done
</constraints>

<verification>
```bash
cd task/executor && make generate
```
Must exit 0 and produce mock files in `mocks/`.

```bash
cd task/executor && make test
```
Must exit 0.

```bash
cd task/executor && go test -coverprofile=/tmp/cover.out ./pkg/handler/... ./pkg/spawner/... && go tool cover -func=/tmp/cover.out
```
Statement coverage for `handler` and `spawner` packages must be ≥ 80%.

```bash
cd task/executor && make precommit
```
Must exit 0.
</verification>
