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
				Status:   "in_progress",
				Phase:    domain.TaskPhaseInProgress.Ptr(),
				Assignee: "claude",
			}
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
			task := lib.Task{
				TaskIdentifier: "tid-2",
				Status:         "in_progress",
				Phase:          nil,
				Assignee:       "claude",
			}
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
