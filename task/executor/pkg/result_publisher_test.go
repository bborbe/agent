// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	libtimetest "github.com/bborbe/time/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/executor/pkg"
)

// capturingSyncProducer implements libkafka.SyncProducer and records sent messages.
type capturingSyncProducer struct {
	messages []*sarama.ProducerMessage
}

func (c *capturingSyncProducer) SendMessage(
	_ context.Context,
	msg *sarama.ProducerMessage,
) (int32, int64, error) {
	c.messages = append(c.messages, msg)
	return 0, 0, nil
}

func (c *capturingSyncProducer) SendMessages(
	_ context.Context,
	msgs []*sarama.ProducerMessage,
) error {
	c.messages = append(c.messages, msgs...)
	return nil
}

func (c *capturingSyncProducer) Close() error { return nil }

var _ libkafka.SyncProducer = &capturingSyncProducer{}

// decodeUpdateFrontmatterCommand extracts the operation and UpdateFrontmatterCommand from a captured message.
func decodeUpdateFrontmatterCommand(
	msg *sarama.ProducerMessage,
) (base.CommandOperation, lib.UpdateFrontmatterCommand) {
	raw, err := msg.Value.Encode()
	Expect(err).NotTo(HaveOccurred())

	var command base.Command
	Expect(json.Unmarshal(raw, &command)).To(Succeed())

	// Re-marshal the Event data and unmarshal into UpdateFrontmatterCommand.
	dataBytes, err := json.Marshal(command.Data)
	Expect(err).NotTo(HaveOccurred())

	var cmd lib.UpdateFrontmatterCommand
	Expect(json.Unmarshal(dataBytes, &cmd)).To(Succeed())

	return command.Operation, cmd
}

var _ = Describe("ResultPublisher", func() {
	var (
		ctx             context.Context
		publisher       pkg.ResultPublisher
		currentDateTime libtime.CurrentDateTime
		producer        *capturingSyncProducer
	)

	BeforeEach(func() {
		ctx = context.Background()
		currentDateTime = libtime.NewCurrentDateTime()
		currentDateTime.SetNow(libtimetest.ParseDateTime("2026-04-18T12:00:00Z"))
		producer = &capturingSyncProducer{}
		publisher = pkg.NewResultPublisher(
			producer,
			base.Branch("prod"),
			currentDateTime,
		)
	})

	Describe("PublishSpawnNotification", func() {
		It("sends exactly three keys via UpdateFrontmatterCommand", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-1"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         "ai_review",
					"assignee":      "claude",
					"trigger_count": 1,
				},
				Content: lib.TaskContent("do the work"),
			}
			err := publisher.PublishSpawnNotification(ctx, task, "claude-20260418120000")
			Expect(err).NotTo(HaveOccurred())

			Expect(producer.messages).To(HaveLen(1))
			operation, cmd := decodeUpdateFrontmatterCommand(producer.messages[0])

			Expect(string(operation)).To(Equal(string(lib.UpdateFrontmatterCommandOperation)))
			Expect(cmd.Updates).To(HaveLen(3))

			Expect(cmd.Updates["spawn_notification"]).To(Equal(true))
			Expect(cmd.Updates["current_job"]).To(Equal("claude-20260418120000"))
			Expect(cmd.Updates["job_started_at"]).To(Equal("2026-04-18T12:00:00Z"))

			_, hasTriggerCount := cmd.Updates["trigger_count"]
			Expect(hasTriggerCount).To(BeFalse(), "trigger_count must not be in spawn notification")
			_, hasStatus := cmd.Updates["status"]
			Expect(hasStatus).To(BeFalse(), "status must not be in spawn notification")
			_, hasPhase := cmd.Updates["phase"]
			Expect(hasPhase).To(BeFalse(), "phase must not be in spawn notification")
		})
	})

	Describe("PublishFailure", func() {
		It(
			"publishes a failure command with phase human_review and a ## Failure body section",
			func() {
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("test-task-2"),
					Frontmatter: lib.TaskFrontmatter{
						"status":        "in_progress",
						"phase":         "ai_review",
						"assignee":      "claude",
						"trigger_count": 2,
					},
					Content: lib.TaskContent("do the work"),
				}
				err := publisher.PublishFailure(
					ctx,
					task,
					"claude-20260418120000",
					"pod OOM killed",
				)
				Expect(err).NotTo(HaveOccurred())

				Expect(producer.messages).To(HaveLen(1))
				operation, cmd := decodeUpdateFrontmatterCommand(producer.messages[0])

				Expect(string(operation)).To(Equal(string(lib.UpdateFrontmatterCommandOperation)))
				Expect(cmd.Updates).To(HaveLen(3))

				Expect(cmd.Updates["status"]).To(Equal("in_progress"))
				Expect(cmd.Updates["phase"]).To(Equal("human_review"))
				Expect(cmd.Updates["current_job"]).To(Equal(""))

				_, hasTriggerCount := cmd.Updates["trigger_count"]
				Expect(hasTriggerCount).To(BeFalse(), "trigger_count must not be in failure update")
				_, hasSpawnNotification := cmd.Updates["spawn_notification"]
				Expect(
					hasSpawnNotification,
				).To(BeFalse(), "spawn_notification must not be in failure update")

				Expect(cmd.Body).NotTo(BeNil())
				Expect(cmd.Body.Heading).To(Equal("## Failure"))
				Expect(cmd.Body.Section).To(ContainSubstring("2026-04-18T12:00:00Z"))
				Expect(cmd.Body.Section).To(ContainSubstring("claude-20260418120000"))
				Expect(cmd.Body.Section).To(ContainSubstring("pod OOM killed"))
			},
		)
	})
})
