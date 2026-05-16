// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	libcron "github.com/bborbe/cron"
	libk8s "github.com/bborbe/k8s"
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
	"github.com/bborbe/agent/task/executor/pkg/probe"
	"github.com/bborbe/agent/task/executor/pkg/spawner"
)

// CreateJobWatcher creates a JobWatcher that reacts to terminal batch/v1 Job states.
func CreateJobWatcher(
	kubeClient kubernetes.Interface,
	namespace libk8s.Namespace,
	taskStore *pkg.TaskStore,
	publisher pkg.ResultPublisher,
) pkg.JobWatcher {
	return pkg.NewJobWatcher(kubeClient, namespace, taskStore, publisher)
}

// CreateK8sConnector returns a K8sConnector wired to the given rest.Config.
func CreateK8sConnector(config *rest.Config) pkg.K8sConnector {
	return pkg.NewK8sConnector(config, pkg.DefaultCRDClientBuilder)
}

// CreateEventHandlerConfig returns an empty in-memory event handler for Config resources.
func CreateEventHandlerConfig() pkg.EventHandlerConfig {
	return pkg.NewEventHandlerConfig()
}

// CreateResourceEventHandlerConfig adapts an EventHandlerConfig to cache.ResourceEventHandler.
func CreateResourceEventHandlerConfig(
	ctx context.Context,
	handler pkg.EventHandlerConfig,
) cache.ResourceEventHandler {
	return pkg.NewResourceEventHandlerConfig(ctx, handler)
}

// CreateConfigResolver returns a ConfigResolver backed by the given store.
func CreateConfigResolver(
	handler pkg.EventHandlerConfig,
	branch base.Branch,
) pkg.ConfigResolver {
	return pkg.NewConfigResolver(handler, branch)
}

// CreateConsumer wires together all components and returns a Kafka Consumer that
// reads task events and spawns K8s Jobs for qualifying tasks.
func CreateConsumer(
	saramaClient sarama.Client,
	branch base.Branch,
	kubeClient kubernetes.Interface,
	namespace libk8s.Namespace,
	kafkaBrokers string,
	resolver pkg.ConfigResolver,
	logSamplerFactory log.SamplerFactory,
	currentDateTimeGetter libtime.CurrentDateTimeGetter,
	resultPublisher pkg.ResultPublisher,
	taskStore *pkg.TaskStore,
) libkafka.Consumer {
	jobSpawner := spawner.NewJobSpawner(
		kubeClient,
		namespace,
		kafkaBrokers,
		string(branch),
		currentDateTimeGetter,
	)
	taskEventHandler := handler.NewTaskEventHandler(
		jobSpawner,
		branch,
		resolver,
		resultPublisher,
		taskStore,
		currentDateTimeGetter,
	)
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

// CreateHealthcheckRunner creates the healthcheck runner shared between the cron path and the
// HTTP trigger path. Callers must pass the same instance to both CreateHealthcheckCron and
// the HTTP handler so probe behavior is identical regardless of invocation path.
func CreateHealthcheckRunner(
	configProvider pkg.EventHandlerConfig,
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
) probe.HealthcheckRunner {
	sender := cdb.NewCommandObjectSender(syncProducer, branch, log.DefaultSamplerFactory)
	publisher := probe.NewCommandPublisher(sender)
	return probe.NewHealthcheckRunner(configProvider, publisher, branch)
}

// CreateHealthcheckCron wraps the given runner in a cron scheduler. Pass the runner returned by
// CreateHealthcheckRunner so the cron and the HTTP handler share the same instance.
func CreateHealthcheckCron(
	expression libcron.Expression,
	runner probe.HealthcheckRunner,
) run.Runnable {
	return libcron.NewExpressionCron(expression, runner)
}
