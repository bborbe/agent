// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher

import (
	"context"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/google/uuid"

	"github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/task_publisher.go --fake-name FakeTaskPublisher . TaskPublisher

// TaskPublisher publishes task change and deletion events to Kafka.
type TaskPublisher interface {
	// PublishChanged publishes an upsert event for the given task file.
	PublishChanged(ctx context.Context, taskFile lib.TaskFile) error
	// PublishDeleted publishes a deletion event for the given task identifier.
	PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error
}

// NewTaskPublisher creates a TaskPublisher that sends events via EventObjectSender.
func NewTaskPublisher(
	eventObjectSender cdb.EventObjectSender,
	schemaID cdb.SchemaID,
) TaskPublisher {
	return &taskPublisher{
		eventObjectSender: eventObjectSender,
		schemaID:          schemaID,
	}
}

type taskPublisher struct {
	eventObjectSender cdb.EventObjectSender
	schemaID          cdb.SchemaID
}

func (p *taskPublisher) PublishChanged(ctx context.Context, taskFile lib.TaskFile) error {
	now := libtime.DateTime(time.Now())
	taskFile.Object = base.Object[base.Identifier]{
		Identifier: base.Identifier(uuid.New().String()),
		Created:    now,
		Modified:   now,
	}
	event, err := base.ParseEvent(ctx, taskFile)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for task %s failed", taskFile.TaskIdentifier)
	}
	if err := p.eventObjectSender.SendUpdate(ctx, cdb.EventObject{
		Event:    event,
		ID:       base.EventID(taskFile.TaskIdentifier),
		SchemaID: p.schemaID,
	}); err != nil {
		return errors.Wrapf(ctx, err, "publish changed task %s failed", taskFile.TaskIdentifier)
	}
	return nil
}

func (p *taskPublisher) PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error {
	if err := p.eventObjectSender.SendDelete(ctx, cdb.EventObject{
		ID:       base.EventID(id),
		SchemaID: p.schemaID,
	}); err != nil {
		return errors.Wrapf(ctx, err, "publish deleted task %s failed", id)
	}
	return nil
}
