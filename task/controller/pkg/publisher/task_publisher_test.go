// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher_test

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/cdb"
	kafkamocks "github.com/bborbe/kafka/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/publisher"
)

var _ = Describe("TaskPublisher", func() {
	var (
		ctx          context.Context
		fakeProducer *kafkamocks.KafkaSyncProducer
		schemaID     cdb.SchemaID
		branch       string
		tp           publisher.TaskPublisher
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeProducer = &kafkamocks.KafkaSyncProducer{}
		schemaID = cdb.SchemaID{
			Group:   "agent",
			Kind:    "task",
			Version: "v1",
		}
		branch = "main"
		tp = publisher.NewTaskPublisher(fakeProducer, schemaID, branch)
	})

	Describe("PublishChanged", func() {
		It("sends a message with the correct topic, key, and valid JSON value", func() {
			fakeProducer.SendMessageReturns(0, 0, nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("24 Tasks/test.md"),
				Name:           lib.TaskName("test"),
				Assignee:       lib.TaskAssignee("user@example.com"),
			}

			err := tp.PublishChanged(ctx, task)
			Expect(err).To(BeNil())
			Expect(fakeProducer.SendMessageCallCount()).To(Equal(1))

			_, msg := fakeProducer.SendMessageArgsForCall(0)
			Expect(msg.Topic).To(Equal("main-agent-task-v1-event"))
			msgKey, ok := msg.Key.(sarama.ByteEncoder)
			Expect(ok).To(BeTrue())
			Expect([]byte(msgKey)).To(Equal(task.TaskIdentifier.Bytes()))

			msgValue, ok := msg.Value.(sarama.ByteEncoder)
			Expect(ok).To(BeTrue())
			var decoded lib.Task
			Expect(json.Unmarshal(msgValue, &decoded)).To(BeNil())
			Expect(decoded.TaskIdentifier).To(Equal(task.TaskIdentifier))
			Expect(decoded.Object.Identifier).NotTo(BeEmpty())
		})

		It("returns an error when SendMessage fails", func() {
			fakeProducer.SendMessageReturns(0, 0, errors.New("kafka down"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("24 Tasks/test.md"),
			}

			err := tp.PublishChanged(ctx, task)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("PublishDeleted", func() {
		It("sends a tombstone message with nil value and correct topic and key", func() {
			fakeProducer.SendMessageReturns(0, 0, nil)
			id := lib.TaskIdentifier("24 Tasks/deleted.md")

			err := tp.PublishDeleted(ctx, id)
			Expect(err).To(BeNil())
			Expect(fakeProducer.SendMessageCallCount()).To(Equal(1))

			_, msg := fakeProducer.SendMessageArgsForCall(0)
			Expect(msg.Topic).To(Equal("main-agent-task-v1-event"))
			msgKey, ok := msg.Key.(sarama.ByteEncoder)
			Expect(ok).To(BeTrue())
			Expect([]byte(msgKey)).To(Equal(id.Bytes()))
			Expect(msg.Value).To(BeNil())
		})

		It("returns an error when SendMessage fails", func() {
			fakeProducer.SendMessageReturns(0, 0, errors.New("kafka down"))
			id := lib.TaskIdentifier("24 Tasks/deleted.md")

			err := tp.PublishDeleted(ctx, id)
			Expect(err).NotTo(BeNil())
		})
	})
})
