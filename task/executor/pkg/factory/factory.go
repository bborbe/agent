// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/log"
	"github.com/bborbe/run"
	libtime "github.com/bborbe/time"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	lib "github.com/bborbe/agent/lib"
	pkg "github.com/bborbe/agent/task/executor/pkg"
	"github.com/bborbe/agent/task/executor/pkg/handler"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// CreateK8sConnector returns a K8sConnector wired to the given rest.Config.
func CreateK8sConnector(config *rest.Config) pkg.K8sConnector {
	return pkg.NewK8sConnector(config, pkg.DefaultCRDClientBuilder)
}

// CreateEventHandlerAgentConfig returns an empty in-memory event handler for AgentConfig resources.
func CreateEventHandlerAgentConfig() pkg.EventHandlerAgentConfig {
	return pkg.NewEventHandlerAgentConfig()
}

// CreateResourceEventHandlerAgentConfig adapts an EventHandlerAgentConfig to cache.ResourceEventHandler.
func CreateResourceEventHandlerAgentConfig(
	ctx context.Context,
	handler pkg.EventHandlerAgentConfig,
) cache.ResourceEventHandler {
	return pkg.NewResourceEventHandlerAgentConfig(ctx, handler)
}

// CreateAgentConfigResolver returns an AgentConfigResolver backed by the given store.
func CreateAgentConfigResolver(
	handler pkg.EventHandlerAgentConfig,
	branch base.Branch,
) pkg.AgentConfigResolver {
	return pkg.NewAgentConfigResolver(handler, string(branch))
}

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and spawns K8s Jobs for qualifying tasks.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	kubeClient kubernetes.Interface,
	namespace string,
	kafkaBrokers string,
	resolver pkg.AgentConfigResolver,
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
	taskEventHandler := handler.NewTaskEventHandler(jobSpawner, branch, resolver)
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
