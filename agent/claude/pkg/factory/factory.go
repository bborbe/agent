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

	"github.com/bborbe/agent/agent/claude/pkg"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
)

const serviceName = "agent-claude"

// CreateTaskRunner wires a complete TaskRunner with ClaudeRunner,
// prompt assembly, and result delivery.
func CreateTaskRunner(
	claudeConfigDir pkg.ClaudeConfigDir,
	agentDir pkg.AgentDir,
	allowedTools pkg.AllowedTools,
	model pkg.ClaudeModel,
	env map[string]string,
	envContext map[string]string,
	instructions pkg.Instructions,
	deliverer pkg.ResultDeliverer,
) pkg.TaskRunner {
	return pkg.NewTaskRunner(
		pkg.NewClaudeRunner(pkg.ClaudeRunnerConfig{
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
) pkg.ResultDeliverer {
	return pkg.NewResultDelivererAdapter(
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
