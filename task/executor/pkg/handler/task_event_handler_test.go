// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package handler_test

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/mocks"
	pkg "github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/handler"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handler Suite")
}

var _ = Describe("TaskEventHandler", func() {
	var (
		ctx                 context.Context
		fakeSpawner         *mocks.FakeJobSpawner
		fakeResolver        *mocks.FakeConfigResolver
		fakeResultPublisher *mocks.FakeResultPublisher
		taskStore           *pkg.TaskStore
		h                   handler.TaskEventHandler
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSpawner = new(mocks.FakeJobSpawner)
		fakeResolver = &mocks.FakeConfigResolver{}
		fakeResolver.ResolveReturns(
			pkg.AgentConfiguration{Assignee: "claude", Image: "my-image:latest"},
			nil,
		)
		fakeResultPublisher = &mocks.FakeResultPublisher{}
		taskStore = pkg.NewTaskStore()
		h = handler.NewTaskEventHandler(
			fakeSpawner,
			base.Branch("prod"),
			fakeResolver,
			fakeResultPublisher,
			taskStore,
		)
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
			task := lib.Task{
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with status != in_progress", func() {
			task := lib.Task{
				TaskIdentifier: "tid-1",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "todo",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with nil phase", func() {
			task := lib.Task{
				TaskIdentifier: "tid-2",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase todo", func() {
			task := lib.Task{
				TaskIdentifier: "tid-3",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseTodo),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase human_review", func() {
			task := lib.Task{
				TaskIdentifier: "tid-4",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseHumanReview),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with empty assignee", func() {
			task := lib.Task{
				TaskIdentifier: "tid-5",
				Frontmatter: lib.TaskFrontmatter{
					"status": "in_progress",
					"phase":  string(domain.TaskPhaseInProgress),
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips unknown assignee without error", func() {
			fakeResolver.ResolveReturns(
				pkg.AgentConfiguration{},
				errors.Wrapf(ctx, pkg.ErrConfigNotFound, "find assignee"),
			)
			task := lib.Task{
				TaskIdentifier: "tid-6",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "unknown-agent",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("returns wrapped error when resolver fails with non-NotFound", func() {
			fakeResolver.ResolveReturns(pkg.AgentConfiguration{}, errors.Errorf(ctx, "boom"))
			task := lib.Task{
				TaskIdentifier: "tid-6b",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "some-agent",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).NotTo(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task when active job exists", func() {
			fakeSpawner.IsJobActiveReturns(true, nil)
			task := lib.Task{
				TaskIdentifier: "tid-7",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("spawns job when no active job exists", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-8"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
				Content: lib.TaskContent("do the work"),
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			_, spawnedTask, config := fakeSpawner.SpawnJobArgsForCall(0)
			Expect(string(spawnedTask.TaskIdentifier)).To(Equal("tid-8"))
			Expect(config.Image).To(Equal("my-image:latest"))
		})

		It("returns error when IsJobActive fails", func() {
			fakeSpawner.IsJobActiveReturns(false, errors.Errorf(ctx, "k8s unavailable"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-9"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).NotTo(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("returns error when SpawnJob fails", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeSpawner.SpawnJobReturns("", errors.Errorf(ctx, "k8s unavailable"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-10"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).NotTo(BeNil())
		})

		It("accepts task with phase planning", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-11"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhasePlanning),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("accepts task with phase ai_review", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-12"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseAIReview),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("publishes spawn notification after successful spawn", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeSpawner.SpawnJobReturns("claude-20260418120000", nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseAIReview),
					"assignee": "claude",
					"stage":    "prod",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeResultPublisher.PublishSpawnNotificationCallCount()).To(Equal(1))
			_, calledTask, calledJobName := fakeResultPublisher.PublishSpawnNotificationArgsForCall(
				0,
			)
			Expect(string(calledTask.TaskIdentifier)).To(Equal("test-task-uuid-1234"))
			Expect(calledJobName).To(Equal("claude-20260418120000"))
		})

		It("stores task in taskStore after successful spawn", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeSpawner.SpawnJobReturns("claude-20260418120000", nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseAIReview),
					"assignee": "claude",
					"stage":    "prod",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			_, ok := taskStore.Load(lib.TaskIdentifier("test-task-uuid-1234"))
			Expect(ok).To(BeTrue())
		})

		It("skips spawn when current_job in frontmatter and K8s job is active", func() {
			fakeSpawner.IsJobActiveReturns(true, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
				Frontmatter: lib.TaskFrontmatter{
					"status":      "in_progress",
					"phase":       string(domain.TaskPhaseAIReview),
					"assignee":    "claude",
					"stage":       "prod",
					"current_job": "claude-20260418000000",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("spawns job when stage is absent (defaults to prod) and executor is prod", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-stage-1"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("skips task with stage=dev when executor branch is prod", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-stage-2"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
					"stage":    "dev",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("spawns job with stage=dev when executor branch is dev", func() {
			localSpawner := new(mocks.FakeJobSpawner)
			localSpawner.IsJobActiveReturns(false, nil)
			localResolver := &mocks.FakeConfigResolver{}
			localResolver.ResolveReturns(
				pkg.AgentConfiguration{Assignee: "claude", Image: "my-image:latest"},
				nil,
			)
			localHandler := handler.NewTaskEventHandler(
				localSpawner,
				base.Branch("dev"),
				localResolver,
				&mocks.FakeResultPublisher{},
				pkg.NewTaskStore(),
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-stage-3"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
					"stage":    "dev",
				},
			}
			err := localHandler.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(localSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("skips task with absent stage (defaults to prod) when executor branch is dev", func() {
			localSpawner := new(mocks.FakeJobSpawner)
			localResolver := &mocks.FakeConfigResolver{}
			localResolver.ResolveReturns(
				pkg.AgentConfiguration{Assignee: "claude", Image: "my-image:latest"},
				nil,
			)
			localHandler := handler.NewTaskEventHandler(
				localSpawner,
				base.Branch("dev"),
				localResolver,
				&mocks.FakeResultPublisher{},
				pkg.NewTaskStore(),
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-stage-4"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := localHandler.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(localSpawner.SpawnJobCallCount()).To(Equal(0))
		})
	})
})
