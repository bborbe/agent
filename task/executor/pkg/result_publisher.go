// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"fmt"
	"time"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libtime "github.com/bborbe/time"

	lib "github.com/bborbe/agent/lib"
	taskcmd "github.com/bborbe/agent/lib/command/task"
)

//counterfeiter:generate -o ../mocks/result_publisher.go --fake-name FakeResultPublisher . ResultPublisher

// ResultPublisher publishes agent-task-v1-request commands to Kafka so the
// controller writes them to the vault task file.
type ResultPublisher interface {
	// PublishSpawnNotification publishes current_job, job_started_at, and
	// spawn_notification without touching any other frontmatter keys.
	PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error
	// PublishFailure publishes a partial frontmatter update setting status, phase,
	// and current_job. Body content is not mutated by this publisher.
	PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error
	// PublishIncrementTriggerCount sends an IncrementFrontmatterCommand that atomically
	// increments trigger_count by 1. Must complete before SpawnJob is called.
	PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error
	// PublishTypeMismatchFailure publishes a synthetic failure when the task's task_type
	// is not in the agent's effective type set. Sets phase=ai_review and clears assignee
	// so the task surfaces in the operator inbox. Does not bump trigger_count or retry_count.
	PublishTypeMismatchFailure(ctx context.Context, task lib.Task, reason string) error
	// PublishRaw publishes a raw payload for testing error paths.
	PublishRaw(ctx context.Context, operation base.CommandOperation, payload interface{}) error
}

// NewResultPublisher creates a ResultPublisher.
func NewResultPublisher(
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
	currentDateTime libtime.CurrentDateTimeGetter,
) ResultPublisher {
	return &resultPublisher{
		commandObjectSender: cdb.NewCommandObjectSender(
			syncProducer,
			branch,
			log.DefaultSamplerFactory,
		),
		currentDateTime: currentDateTime,
	}
}

// resultPublisher implements ResultPublisher by sending CQRS command objects to Kafka.
type resultPublisher struct {
	commandObjectSender cdb.CommandObjectSender
	currentDateTime     libtime.CurrentDateTimeGetter
}

func (p *resultPublisher) PublishSpawnNotification(
	ctx context.Context,
	task lib.Task,
	jobName string,
) error {
	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"spawn_notification": true,
			"current_job":        jobName,
			"job_started_at":     p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
	}
	return p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) PublishFailure(
	ctx context.Context,
	task lib.Task,
	jobName string,
	reason string,
) error {
	now := p.currentDateTime.Now().UTC().Format(time.RFC3339)
	section := fmt.Sprintf(
		"## Failure\n\n- **Timestamp:** %s\n- **Job:** %s\n- **Reason:** %s\n",
		now,
		jobName,
		reason,
	)
	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"status":      "in_progress",
			"phase":       "human_review",
			"current_job": "",
		},
		Body: &taskcmd.BodySection{
			Heading: "## Failure",
			Section: section,
		},
	}
	return p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) PublishIncrementTriggerCount(ctx context.Context, task lib.Task) error {
	cmd := taskcmd.IncrementFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Field:          "trigger_count",
		Delta:          1,
	}
	return p.publishRaw(ctx, taskcmd.IncrementFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) PublishTypeMismatchFailure(
	ctx context.Context,
	task lib.Task,
	reason string,
) error {
	now := p.currentDateTime.Now().UTC().Format(time.RFC3339)
	section := fmt.Sprintf(
		"## Failure\n\n- **Timestamp:** %s\n- **Assignee:** %s\n- **Reason:** %s\n",
		now,
		task.Frontmatter.Assignee(),
		reason,
	)
	cmd := taskcmd.UpdateFrontmatterCommand{
		TaskIdentifier: task.TaskIdentifier,
		Updates: lib.TaskFrontmatter{
			"status":      "in_progress",
			"phase":       "ai_review",
			"assignee":    "",
			"current_job": "",
		},
		Body: &taskcmd.BodySection{
			Heading: "## Failure",
			Section: section,
		},
	}
	return p.publishRaw(ctx, taskcmd.UpdateFrontmatterCommandOperation, cmd)
}

func (p *resultPublisher) publishRaw(
	ctx context.Context,
	operation base.CommandOperation,
	payload interface{},
) error {
	event, err := base.ParseEvent(ctx, payload)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for operation %s", operation)
	}

	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			operation,
			cqrsiam.Initiator("executor"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send command for operation %s", operation)
	}
	return nil
}

// PublishRaw exposes publishRaw for testing error path coverage.
func (p *resultPublisher) PublishRaw(
	ctx context.Context,
	operation base.CommandOperation,
	payload interface{},
) error {
	return p.publishRaw(ctx, operation, payload)
}
