// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/handler"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
)

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and publishes prompt events.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	eventObjectSender cdb.EventObjectSender,
	logSamplerFactory log.SamplerFactory,
) libkafka.Consumer {
	duplicateTracker := handler.NewInMemoryDuplicateTracker()
	promptPublisher := publisher.NewPromptPublisher(eventObjectSender, lib.PromptV1SchemaID)
	taskEventHandler := handler.NewTaskEventHandler(duplicateTracker, promptPublisher)
	topic := lib.TaskV1SchemaID.EventTopic(branch)
	offsetManager := libkafka.NewSaramaOffsetManager(
		saramaClient,
		libkafka.Group("agent-prompt-controller"),
		libkafka.OffsetOldest,
		libkafka.OffsetOldest,
	)
	return libkafka.NewOffsetConsumerHighwaterMarks(
		saramaClient,
		topic,
		offsetManager,
		taskEventHandler,
		nil,
		logSamplerFactory,
	)
}
