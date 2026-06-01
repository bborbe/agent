// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"context"
	"encoding/json"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	libtimetest "github.com/bborbe/time/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	taskcmd "github.com/bborbe/agent/lib/command/task"
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

// failingSyncProducer implements libkafka.SyncProducer and always returns an error.
type failingSyncProducer struct {
	err error
}

func (f *failingSyncProducer) SendMessage(
	_ context.Context,
	_ *sarama.ProducerMessage,
) (int32, int64, error) {
	return 0, 0, f.err
}

func (f *failingSyncProducer) SendMessages(
	_ context.Context,
	_ []*sarama.ProducerMessage,
) error {
	return f.err
}

func (f *failingSyncProducer) Close() error { return nil }

var _ libkafka.SyncProducer = &failingSyncProducer{}

// decodeUpdateFrontmatterCommand extracts the operation and UpdateFrontmatterCommand from a captured message.
func decodeUpdateFrontmatterCommand(
	msg *sarama.ProducerMessage,
) (base.CommandOperation, taskcmd.UpdateFrontmatterCommand) {
	raw, err := msg.Value.Encode()
	Expect(err).NotTo(HaveOccurred())

	var command base.Command
	Expect(json.Unmarshal(raw, &command)).To(Succeed())

	// Re-marshal the Event data and unmarshal into UpdateFrontmatterCommand.
	dataBytes, err := json.Marshal(command.Data)
	Expect(err).NotTo(HaveOccurred())

	var cmd taskcmd.UpdateFrontmatterCommand
	Expect(json.Unmarshal(dataBytes, &cmd)).To(Succeed())

	return command.Operation, cmd
}

// decodeIncrementFrontmatterCommand extracts the operation and IncrementFrontmatterCommand from a captured message.
func decodeIncrementFrontmatterCommand(
	msg *sarama.ProducerMessage,
) (base.CommandOperation, taskcmd.IncrementFrontmatterCommand) {
	raw, err := msg.Value.Encode()
	Expect(err).NotTo(HaveOccurred())

	var command base.Command
	Expect(json.Unmarshal(raw, &command)).To(Succeed())

	dataBytes, err := json.Marshal(command.Data)
	Expect(err).NotTo(HaveOccurred())

	var cmd taskcmd.IncrementFrontmatterCommand
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

			Expect(string(operation)).To(Equal(string(taskcmd.UpdateFrontmatterCommandOperation)))
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
			"publishes two commands: UpdateFrontmatterCommand clearing current_job with ## Failure body, then IncrementFrontmatterCommand bumping trigger_count",
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

				Expect(producer.messages).To(HaveLen(2))

				// First message: UpdateFrontmatterCommand
				operation, updateCmd := decodeUpdateFrontmatterCommand(producer.messages[0])
				Expect(
					string(operation),
				).To(Equal(string(taskcmd.UpdateFrontmatterCommandOperation)))
				Expect(updateCmd.Updates).To(HaveLen(1))
				Expect(updateCmd.Updates["current_job"]).To(Equal(""))

				_, hasStatus := updateCmd.Updates["status"]
				Expect(hasStatus).To(BeFalse(), "status must not be in failure update")
				_, hasPhase := updateCmd.Updates["phase"]
				Expect(hasPhase).To(BeFalse(), "phase must not be in failure update")
				_, hasAssignee := updateCmd.Updates["assignee"]
				Expect(hasAssignee).To(BeFalse(), "assignee must not be in failure update")
				_, hasPreviousAssignee := updateCmd.Updates["previous_assignee"]
				Expect(
					hasPreviousAssignee,
				).To(BeFalse(), "previous_assignee must not be in failure update")
				_, hasTriggerCount := updateCmd.Updates["trigger_count"]
				Expect(hasTriggerCount).To(BeFalse(), "trigger_count must not be in failure update")

				Expect(updateCmd.Body).NotTo(BeNil())
				Expect(updateCmd.Body.Heading).To(Equal("## Failure"))
				Expect(updateCmd.Body.Section).To(ContainSubstring("2026-04-18T12:00:00Z"))
				Expect(updateCmd.Body.Section).To(ContainSubstring("claude-20260418120000"))
				Expect(updateCmd.Body.Section).To(ContainSubstring("pod OOM killed"))

				// Second message: IncrementFrontmatterCommand
				incOperation, incCmd := decodeIncrementFrontmatterCommand(producer.messages[1])
				Expect(
					string(incOperation),
				).To(Equal(string(taskcmd.IncrementFrontmatterCommandOperation)))
				Expect(string(incCmd.TaskIdentifier)).To(Equal("test-task-2"))
				Expect(incCmd.Field).To(Equal("trigger_count"))
				Expect(incCmd.Delta).To(Equal(1))
			},
		)
	})

	Describe("PublishFailure dedupe", func() {
		It("suppresses a second call with the same job name", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-dedupe"),
				Frontmatter: lib.TaskFrontmatter{
					"status": "in_progress",
				},
				Content: lib.TaskContent("do the work"),
			}

			err := publisher.PublishFailure(ctx, task, "claude-20260418120000", "pod OOM killed")
			Expect(err).NotTo(HaveOccurred())
			Expect(producer.messages).To(HaveLen(2))

			err = publisher.PublishFailure(ctx, task, "claude-20260418120000", "pod OOM killed")
			Expect(err).NotTo(HaveOccurred())
			Expect(producer.messages).To(HaveLen(2), "second call should be deduped")
		})
	})

	Describe("PublishTypeMismatchFailure", func() {
		It(
			"publishes assignee='', previous_assignee=<prior>, current_job='' and Assignee bullet in body",
			func() {
				task := lib.Task{
					TaskIdentifier: lib.TaskIdentifier("test-task-3"),
					Frontmatter: lib.TaskFrontmatter{
						"status":   "in_progress",
						"phase":    "planning",
						"assignee": "agent-pr-reviewer",
					},
				}
				err := publisher.PublishTypeMismatchFailure(
					ctx,
					task,
					`task_type "healthcheck" not in effective set [pr-review] of agent "agent-pr-reviewer"`,
				)
				Expect(err).NotTo(HaveOccurred())

				Expect(producer.messages).To(HaveLen(1))
				operation, cmd := decodeUpdateFrontmatterCommand(producer.messages[0])

				Expect(
					string(operation),
				).To(Equal(string(taskcmd.UpdateFrontmatterCommandOperation)))
				Expect(cmd.Updates).To(HaveLen(3))
				Expect(cmd.Updates["assignee"]).To(Equal(""))
				Expect(cmd.Updates["previous_assignee"]).To(Equal("agent-pr-reviewer"))
				Expect(cmd.Updates["current_job"]).To(Equal(""))

				_, hasStatus := cmd.Updates["status"]
				Expect(hasStatus).To(BeFalse(), "status must not be in type mismatch update")
				_, hasPhase := cmd.Updates["phase"]
				Expect(hasPhase).To(BeFalse(), "phase must not be in type mismatch update")
				_, hasTriggerCount := cmd.Updates["trigger_count"]
				Expect(
					hasTriggerCount,
				).To(BeFalse(), "trigger_count must not be in type mismatch update")

				Expect(cmd.Body).NotTo(BeNil())
				Expect(cmd.Body.Heading).To(Equal("## Failure"))
				Expect(cmd.Body.Section).To(ContainSubstring("2026-04-18T12:00:00Z"))
				Expect(cmd.Body.Section).To(ContainSubstring("agent-pr-reviewer"))
				Expect(cmd.Body.Section).To(ContainSubstring("healthcheck"))
			},
		)
	})

	Describe("PublishIncrementTriggerCount", func() {
		It("sends IncrementFrontmatterCommand with trigger_count and delta 1", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-4"),
				Frontmatter: lib.TaskFrontmatter{
					"status":        "in_progress",
					"phase":         "planning",
					"trigger_count": 0,
				},
			}
			err := publisher.PublishIncrementTriggerCount(ctx, task)
			Expect(err).NotTo(HaveOccurred())

			Expect(producer.messages).To(HaveLen(1))
			operation, cmd := decodeIncrementFrontmatterCommand(producer.messages[0])

			Expect(
				string(operation),
			).To(Equal(string(taskcmd.IncrementFrontmatterCommandOperation)))
			Expect(string(cmd.TaskIdentifier)).To(Equal("test-task-4"))
			Expect(cmd.Field).To(Equal("trigger_count"))
			Expect(cmd.Delta).To(Equal(1))
		})

		It("returns error when sender fails", func() {
			failingProducer := &failingSyncProducer{
				err: errors.New(context.Background(), "kafka: leader not available"),
			}
			failingPublisher := pkg.NewResultPublisher(
				failingProducer,
				base.Branch("prod"),
				currentDateTime,
			)

			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("test-task-5"),
				Frontmatter: lib.TaskFrontmatter{
					"status": "in_progress",
				},
			}
			err := failingPublisher.PublishIncrementTriggerCount(ctx, task)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kafka: leader not available"))
		})

		It("handles empty task identifier gracefully", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier(""),
				Frontmatter: lib.TaskFrontmatter{
					"status": "in_progress",
				},
			}
			err := publisher.PublishIncrementTriggerCount(ctx, task)
			Expect(err).NotTo(HaveOccurred())

			Expect(producer.messages).To(HaveLen(1))
			_, cmd := decodeIncrementFrontmatterCommand(producer.messages[0])

			Expect(string(cmd.TaskIdentifier)).To(Equal(""))
			Expect(cmd.Field).To(Equal("trigger_count"))
			Expect(cmd.Delta).To(Equal(1))
		})
	})

	Describe("PublishRaw", func() {
		It("returns wrapped error when base.ParseEvent fails", func() {
			// Pass an invalid JSON string to cause ParseEvent to fail
			invalidJSON := "{not valid json"
			err := publisher.PublishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, invalidJSON)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse event for operation"))
			Expect(err.Error()).To(ContainSubstring("update-frontmatter"))
		})
	})
})
