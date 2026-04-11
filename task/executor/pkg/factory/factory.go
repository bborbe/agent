// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libtime "github.com/bborbe/time"
	"k8s.io/client-go/kubernetes"

	lib "github.com/bborbe/agent/lib"
	pkg "github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/handler"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and spawns K8s Jobs for qualifying tasks.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	agentConfigs pkg.AgentConfigurations,
	logSamplerFactory log.SamplerFactory,
	currentDateTimeGetter libtime.CurrentDateTimeGetter,
) libkafka.Consumer {
	jobSpawner := spawner.NewJobSpawner(
		kubeClient,
		namespace,
		kafkaBrokers,
		string(branch),
		currentDateTimeGetter,
	)
	taskEventHandler := handler.NewTaskEventHandler(jobSpawner, branch, agentConfigs)
	topic := lib.TaskV1SchemaID.EventTopic(branch)
	offsetManager := libkafka.NewSaramaOffsetManager(
		saramaClient,
		libkafka.Group("agent-task-executor"),
		libkafka.OffsetOldest,
		libkafka.OffsetOldest,
	)
	return libkafka.NewOffsetConsumerHighwaterMarks(
		saramaClient,
		topic,
		offsetManager,
		taskEventHandler,
		run.NewTrigger(),
		logSamplerFactory,
	)
}
