// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	libtime "github.com/bborbe/time"
	"github.com/google/uuid"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../mocks/result_publisher.go --fake-name FakeResultPublisher . ResultPublisher

// ResultPublisher publishes agent-task-v1-request commands to Kafka so the
// controller writes them to the vault task file.
type ResultPublisher interface {
	// PublishSpawnNotification publishes current_job and job_started_at without
	// triggering the controller's retry counter (spawn_notification: true).
	PublishSpawnNotification(ctx context.Context, task lib.Task, jobName string) error
	// PublishFailure publishes a synthetic failure result that increments the
	// controller's retry counter. jobName and reason are appended to the task body.
	PublishFailure(ctx context.Context, task lib.Task, jobName string, reason string) error
	// PublishRetryCountBump increments retry_count by 1 in the task frontmatter and
	// publishes the update to agent-task-v1-request BEFORE the K8s Job is created.
	// If this publish fails the caller must NOT spawn the Job.
	PublishRetryCountBump(ctx context.Context, task lib.Task) error
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

type resultPublisher struct {
	commandObjectSender cdb.CommandObjectSender
	currentDateTime     libtime.CurrentDateTimeGetter
}

func (p *resultPublisher) PublishSpawnNotification(
	ctx context.Context,
	task lib.Task,
	jobName string,
) error {
	fm := lib.TaskFrontmatter{}
	for k, v := range task.Frontmatter {
		fm[k] = v
	}
	fm["spawn_notification"] = true
	fm["current_job"] = jobName
	fm["job_started_at"] = p.currentDateTime.Now().UTC().Format("2006-01-02T15:04:05Z07:00")

	return p.publish(ctx, task.TaskIdentifier, fm, task.Content)
}

func (p *resultPublisher) PublishFailure(
	ctx context.Context,
	task lib.Task,
	jobName string,
	reason string,
) error {
	fm := lib.TaskFrontmatter{}
	for k, v := range task.Frontmatter {
		fm[k] = v
	}
	fm["status"] = "in_progress"
	fm["phase"] = "ai_review"
	fm["current_job"] = ""

	body := string(
		task.Content,
	) + "\n\n## Job Failure\n\nJob `" + jobName + "` failed: " + reason + "\n"
	return p.publish(ctx, task.TaskIdentifier, fm, lib.TaskContent(body))
}

func (p *resultPublisher) PublishRetryCountBump(ctx context.Context, task lib.Task) error {
	fm := lib.TaskFrontmatter{}
	for k, v := range task.Frontmatter {
		fm[k] = v
	}
	fm["retry_count"] = task.Frontmatter.RetryCount() + 1
	return p.publish(ctx, task.TaskIdentifier, fm, task.Content)
}

func (p *resultPublisher) publish(
	ctx context.Context,
	taskID lib.TaskIdentifier,
	fm lib.TaskFrontmatter,
	content lib.TaskContent,
) error {
	now := p.currentDateTime.Now()
	t := lib.Task{
		Object: base.Object[base.Identifier]{
			Identifier: base.Identifier(uuid.New().String()),
			Created:    now,
			Modified:   now,
		},
		TaskIdentifier: taskID,
		Frontmatter:    fm,
		Content:        content,
	}

	event, err := base.ParseEvent(ctx, t)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for task %s", taskID)
	}

	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			base.UpdateOperation,
			cqrsiam.Initiator("executor"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := p.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send command for task %s", taskID)
	}
	return nil
}
