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
	"github.com/bborbe/errors"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/mocks"
	"github.com/bborbe/agent/task/executor/pkg/handler"
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

	buildMsg := func(taskFile lib.TaskFile) *sarama.ConsumerMessage {
		value, err := json.Marshal(taskFile)
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
			taskFile := lib.TaskFile{
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with status != in_progress", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-1",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "todo",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with nil phase", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-2",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase todo", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-3",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseTodo),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with phase human_review", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-4",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseHumanReview),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips task with empty assignee", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-5",
				Frontmatter: lib.TaskFrontmatter{
					"status": "in_progress",
					"phase":  string(domain.TaskPhaseInProgress),
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips unknown assignee without error", func() {
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-6",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "unknown-agent",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("skips duplicate TaskIdentifier", func() {
			fakeTracker.IsDuplicateReturns(true)
			taskFile := lib.TaskFile{
				TaskIdentifier: "tid-7",
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(0))
		})

		It("spawns job for qualifying task with known assignee", func() {
			fakeTracker.IsDuplicateReturns(false)
			taskFile := lib.TaskFile{
				TaskIdentifier: lib.TaskIdentifier("tid-8"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
				Content: "do the work",
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
			_, spawnedTaskFile, image := fakeSpawner.SpawnJobArgsForCall(0)
			Expect(string(spawnedTaskFile.TaskIdentifier)).To(Equal("tid-8"))
			Expect(image).To(Equal("my-image:latest"))
		})

		It("marks task as processed after successful spawn", func() {
			fakeTracker.IsDuplicateReturns(false)
			taskFile := lib.TaskFile{
				TaskIdentifier: lib.TaskIdentifier("tid-9"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(1))
			Expect(fakeTracker.MarkProcessedArgsForCall(0)).To(Equal(lib.TaskIdentifier("tid-9")))
		})

		It("does not mark task as processed when spawn fails", func() {
			fakeTracker.IsDuplicateReturns(false)
			fakeSpawner.SpawnJobReturns(errors.Errorf(ctx, "k8s unavailable"))
			taskFile := lib.TaskFile{
				TaskIdentifier: lib.TaskIdentifier("tid-10"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseInProgress),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).NotTo(BeNil())
			Expect(fakeTracker.MarkProcessedCallCount()).To(Equal(0))
		})

		It("accepts task with phase planning", func() {
			fakeTracker.IsDuplicateReturns(false)
			taskFile := lib.TaskFile{
				TaskIdentifier: lib.TaskIdentifier("tid-11"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhasePlanning),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})

		It("accepts task with phase ai_review", func() {
			fakeTracker.IsDuplicateReturns(false)
			taskFile := lib.TaskFile{
				TaskIdentifier: lib.TaskIdentifier("tid-12"),
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"phase":    string(domain.TaskPhaseAIReview),
					"assignee": "claude",
				},
			}
			err := h.ConsumeMessage(ctx, buildMsg(taskFile))
			Expect(err).To(BeNil())
			Expect(fakeSpawner.SpawnJobCallCount()).To(Equal(1))
		})
	})
})
