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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/mocks"
	"github.com/bborbe/agent/prompt/controller/pkg/handler"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handler Suite")
}

var _ = Describe("TaskEventHandler", func() {
	var (
		ctx           context.Context
		fakeTracker   *mocks.FakeDuplicateTracker
		fakePublisher *mocks.FakePromptPublisher
		h             handler.TaskEventHandler
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
