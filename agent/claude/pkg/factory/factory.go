// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"

	agentlib "github.com/bborbe/agent/lib"
	claudelib "github.com/bborbe/agent/lib/claude"
	delivery "github.com/bborbe/agent/lib/delivery"
)

const serviceName = "agent-claude"

// CreateTaskRunner wires a complete TaskRunner with ClaudeRunner,
// prompt assembly, and result delivery.
func CreateTaskRunner(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	env map[string]string,
	envContext map[string]string,
	instructions claudelib.Instructions,
	deliverer claudelib.ResultDeliverer,
) claudelib.TaskRunner {
	return claudelib.NewTaskRunner(
		claudelib.NewClaudeRunner(claudelib.ClaudeRunnerConfig{
			ClaudeConfigDir:  claudeConfigDir,
			AllowedTools:     allowedTools,
			Model:            model,
			WorkingDirectory: agentDir,
			Env:              env,
		}),
		instructions,
		envContext,
		deliverer,
	)
}

// CreateSyncProducer creates a Kafka sync producer.
func CreateSyncProducer(
	ctx context.Context,
	brokers libkafka.Brokers,
) (libkafka.SyncProducer, error) {
	producer, err := libkafka.NewSyncProducerWithName(ctx, brokers, serviceName)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "create sync producer failed")
	}
	return producer, nil
}

// CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task updates to Kafka.
func CreateKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
	taskID agentlib.TaskIdentifier,
	taskContent string,
) claudelib.ResultDeliverer {
	return claudelib.NewResultDelivererAdapter(
		delivery.NewKafkaResultDeliverer(
			syncProducer,
			branch,
			taskID,
			taskContent,
			delivery.NewFallbackContentGenerator(),
			libtime.NewCurrentDateTime(),
		),
	)
}
