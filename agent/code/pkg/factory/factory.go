// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-code binary.
//
// Pure-code agent — no Claude/Gemini/LLM dependencies, just deliverers.
package factory

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent/agent/code/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
	healthcheck "github.com/bborbe/agent/lib/healthcheck"
)

const serviceName = "agent-code"

// CreateSyncProducer creates a Kafka sync producer.
func CreateSyncProducer(
	ctx context.Context,
	brokers libkafka.Brokers,
) (libkafka.SyncProducer, error) {
	producer, err := libkafka.NewSyncProducerWithName(ctx, brokers, serviceName)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create sync producer failed")
	}
	return producer, nil
}

// CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task
// updates to Kafka via CQRS commands. Uses the passthrough content generator
// — the agent framework's StepRunner already produces the full marshaled
// task in result.Output; the deliverer publishes it as-is and overrides
// status/phase frontmatter based on the result Status.
func CreateKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	branch base.Branch,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return delivery.NewKafkaResultDeliverer(
		syncProducer,
		branch,
		taskID,
		originalContent,
		delivery.NewPassthroughContentGenerator(),
		currentDateTime,
	)
}

// CreateFileResultDeliverer creates a ResultDeliverer that writes the agent's
// output back to a markdown file (local CLI mode).
func CreateFileResultDeliverer(filePath string) agentlib.ResultDeliverer {
	return delivery.NewFileResultDeliverer(
		delivery.NewPassthroughContentGenerator(),
		filePath,
	)
}

// CreateAgent assembles the 3-phase pure-code agent — no LLM deps.
// PlanStep reads frontmatter, ExecuteStep computes, VerifyStep checks.
func CreateAgent() *agentlib.Agent {
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", steps.NewPlanStep()),
		agentlib.NewPhase(domain.TaskPhaseExecution, steps.NewExecuteStep()),
		agentlib.NewPhase("ai_review", steps.NewVerifyStep()),
	)
}

// CreateAgentProvider wires the per-task-type dispatch for agent-code.
// Healthcheck-only binary, pure-Go (no LLM): TaskTypeHealthcheck routes to a
// Nop liveness agent — reaching it proves binary booted, envconfig parsed,
// Kafka client opened. Any other task_type hits the default-error branch.
func CreateAgentProvider() agentlib.AgentProvider {
	livenessAgent := healthcheck.NewAgent(healthcheck.NewNopStep())
	return agentlib.NewAgentProvider("agent-code", map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeHealthcheck: livenessAgent,
	})
}
