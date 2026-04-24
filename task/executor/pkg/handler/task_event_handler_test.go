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
	agentv1 "github.com/bborbe/agent/task/executor/k8s/apis/agent.benjamin-borbe.de/v1"
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

		It(
			"publishes increment trigger_count before spawning job (retry_count bump no longer called)",
			func() {
				fakeSpawner.IsJobActiveReturns(false, nil)
				fakeSpawner.SpawnJobReturns("claude-20260418120000", nil)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
					Frontmatter: lib.TaskFrontmatter{
						"status":        "in_progress",
						"phase":         string(domain.TaskPhaseAIReview),
						"assignee":      "claude",
						"stage":         "prod",
						"trigger_count": 1,
						"max_triggers":  3,
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeResultPublisher.PublishIncrementTriggerCountCallCount()).To(Equal(1))
				_, calledTask := fakeResultPublisher.PublishIncrementTriggerCountArgsForCall(0)
				Expect(string(calledTask.TaskIdentifier)).To(Equal("test-task-uuid-1234"))
				Expect(fakeResultPublisher.PublishRetryCountBumpCallCount()).To(Equal(0))
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			},
		)

		It("does not spawn job when PublishIncrementTriggerCount fails", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeResultPublisher.PublishIncrementTriggerCountReturns(
				errors.New(ctx, "kafka unavailable"),
			)
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
			Expect(err).To(HaveOccurred())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips spawn when trigger_count >= max_triggers (cap reached)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-cap-1"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         string(domain.TaskPhaseAIReview),
					"assignee":      "claude",
					"stage":         "prod",
					"trigger_count": 3,
					"max_triggers":  3,
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeResultPublisher.PublishIncrementTriggerCountCallCount()).To(Equal(0))
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("publishes increment and spawns when below cap (happy path)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeSpawner.SpawnJobReturns("claude-job-1", nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-cap-2"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         string(domain.TaskPhaseAIReview),
					"assignee":      "claude",
					"stage":         "prod",
					"trigger_count": 1,
					"max_triggers":  3,
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeResultPublisher.PublishIncrementTriggerCountCallCount()).To(Equal(1))
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It(
			"blocks spawn when PublishIncrementTriggerCount fails (publish-failure scenario)",
			func() {
				fakeSpawner.IsJobActiveReturns(false, nil)
				fakeResultPublisher.PublishIncrementTriggerCountReturns(
					errors.New(ctx, "kafka down"),
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("test-task-cap-3"),
					Frontmatter: lib.TaskFrontmatter{
						"status":        "in_progress",
						"phase":         string(domain.TaskPhaseAIReview),
						"assignee":      "claude",
						"stage":         "prod",
						"trigger_count": 0,
						"max_triggers":  3,
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(HaveOccurred())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
			},
		)

		It("skips spawn when max_triggers=0 (zero-cap edge case)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-cap-4"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         string(domain.TaskPhaseAIReview),
					"assignee":      "claude",
					"stage":         "prod",
					"trigger_count": 0,
					"max_triggers":  0,
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeResultPublisher.PublishIncrementTriggerCountCallCount()).To(Equal(0))
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("publishes increment once even when SpawnJob fails (over-count documented)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeSpawner.SpawnJobReturns("", errors.New(ctx, "k8s create failed"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-cap-5"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         string(domain.TaskPhaseAIReview),
					"assignee":      "claude",
					"stage":         "prod",
					"trigger_count": 1,
					"max_triggers":  3,
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(HaveOccurred())
			Expect(fakeResultPublisher.PublishIncrementTriggerCountCallCount()).To(Equal(1))
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
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

		It("removes task from taskStore when event has status=completed", func() {
			taskStore.Store(lib.TaskIdentifier("test-task-uuid-1234"), lib.Task{
				TaskIdentifier: "test-task-uuid-1234",
			})
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-uuid-1234"),
				Frontmatter: lib.TaskFrontmatter{
					"status": "completed",
					"phase":  "done",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			_, ok := taskStore.Load(lib.TaskIdentifier("test-task-uuid-1234"))
			Expect(ok).To(BeFalse())
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

		It("spawns job when Trigger == nil (default phases and statuses apply)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeResolver.ResolveReturns(
				pkg.AgentConfiguration{Assignee: "claude", Image: "my-image:latest", Trigger: nil},
				nil,
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-trigger-1"),
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

		It("spawns job when Config has Trigger.Phases=[todo] and event phase=todo", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeResolver.ResolveReturns(
				pkg.AgentConfiguration{
					Assignee: "claude",
					Image:    "my-image:latest",
					Trigger:  &agentv1.Trigger{Phases: domain.TaskPhases{domain.TaskPhaseTodo}},
				},
				nil,
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-trigger-2"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseTodo),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(task))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It(
			"spawns job when Config has Trigger.Statuses=[completed] and event status=completed",
			func() {
				fakeSpawner.IsJobActiveReturns(false, nil)
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Statuses: domain.TaskStatuses{domain.TaskStatusCompleted},
						},
					},
					nil,
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-trigger-3"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "completed",
						"phase":    string(domain.TaskPhaseInProgress),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			},
		)

		It(
			"spawns job when combined trigger matches event (Phases=[done], Statuses=[completed])",
			func() {
				fakeSpawner.IsJobActiveReturns(false, nil)
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Phases:   domain.TaskPhases{domain.TaskPhaseDone},
							Statuses: domain.TaskStatuses{domain.TaskStatusCompleted},
						},
					},
					nil,
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-trigger-4a"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "completed",
						"phase":    string(domain.TaskPhaseDone),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			},
		)

		It(
			"does not spawn when combined trigger does not match event (non-matching event)",
			func() {
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Phases:   domain.TaskPhases{domain.TaskPhaseDone},
							Statuses: domain.TaskStatuses{domain.TaskStatusCompleted},
						},
					},
					nil,
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-trigger-4b"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    string(domain.TaskPhasePlanning),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
			},
		)

		It(
			"increments skipped_status and does not spawn when phase matches but status does not",
			func() {
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Phases:   domain.TaskPhases{domain.TaskPhaseInProgress},
							Statuses: domain.TaskStatuses{domain.TaskStatusCompleted},
						},
					},
					nil,
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-trigger-5"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    string(domain.TaskPhaseInProgress),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
			},
		)

		It(
			"increments skipped_phase and does not spawn when status matches but phase does not",
			func() {
				fakeResolver.ResolveReturns(
					pkg.AgentConfiguration{
						Assignee: "claude",
						Image:    "my-image:latest",
						Trigger: &agentv1.Trigger{
							Phases:   domain.TaskPhases{domain.TaskPhaseDone},
							Statuses: domain.TaskStatuses{domain.TaskStatusInProgress},
						},
					},
					nil,
				)
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("tid-trigger-6"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    string(domain.TaskPhaseInProgress),
						"assignee": "claude",
					},
				}
				err := h.ConsumeMessage(ctx, buildMsg(task))
				Expect(err).To(BeNil())
				Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
			},
		)

		It("spawns job when Trigger has empty phase and status lists (defaults apply)", func() {
			fakeSpawner.IsJobActiveReturns(false, nil)
			fakeResolver.ResolveReturns(
				pkg.AgentConfiguration{
					Assignee: "claude",
					Image:    "my-image:latest",
					Trigger:  &agentv1.Trigger{},
				},
				nil,
			)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("tid-trigger-7"),
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
	})
})
