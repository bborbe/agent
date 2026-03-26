// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	"github.com/google/uuid"

	"github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/task_publisher.go --fake-name FakeTaskPublisher . TaskPublisher

// TaskPublisher publishes task change and deletion events to Kafka.
type TaskPublisher interface {
	// PublishChanged publishes an upsert event for the given task.
	PublishChanged(ctx context.Context, task lib.Task) error
	// PublishDeleted publishes a deletion event for the given task identifier.
	PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error
}

// NewTaskPublisher creates a TaskPublisher that sends events to Kafka.
func NewTaskPublisher(
	syncProducer libkafka.SyncProducer,
	schemaID cdb.SchemaID,
	branch string,
) TaskPublisher {
	return &taskPublisher{
		syncProducer: syncProducer,
		topic:        string(schemaID.EventTopic(base.Branch(branch))),
	}
}

type taskPublisher struct {
	syncProducer libkafka.SyncProducer
	topic        string
}

func (p *taskPublisher) PublishChanged(ctx context.Context, task lib.Task) error {
	now := libtime.DateTime(time.Now())
	task.Object = base.Object[base.Identifier]{
		Identifier: base.Identifier(uuid.New().String()),
		Created:    now,
		Modified:   now,
	}
	jsonBytes, err := json.Marshal(task)
	if err != nil {
		return errors.Wrapf(ctx, err, "publish changed task %s failed", task.TaskIdentifier)
	}
	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.ByteEncoder(task.TaskIdentifier.Bytes()),
		Value: sarama.ByteEncoder(jsonBytes),
	}
	if _, _, err := p.syncProducer.SendMessage(ctx, msg); err != nil {
		return errors.Wrapf(ctx, err, "publish changed task %s failed", task.TaskIdentifier)
	}
	return nil
}

func (p *taskPublisher) PublishDeleted(ctx context.Context, id lib.TaskIdentifier) error {
	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.ByteEncoder(id.Bytes()),
		Value: nil,
	}
	if _, _, err := p.syncProducer.SendMessage(ctx, msg); err != nil {
		return errors.Wrapf(ctx, err, "publish deleted task %s failed", id)
	}
	return nil
}
