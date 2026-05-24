// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-pi binary.
package factory

import (
	"context"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"

	"github.com/bborbe/agent/agent/pi/pkg/prompts"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
	healthcheck "github.com/bborbe/agent/lib/healthcheck"
	pilib "github.com/bborbe/agent/lib/pi"
)

const serviceName = "agent-pi"

// CreatePiRunner constructs a Pi Runner pre-configured with tools, model, and env.
func CreatePiRunner(
	agentDir string,
	allowedTools string,
	model string,
	env map[string]string,
) pilib.Runner {
	return pilib.NewRunner(pilib.PiRunnerConfig{
		AgentDir:     agentDir,
		AllowedTools: allowedTools,
		Model:        model,
		Env:          env,
	})
}

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

// CreateFileResultDeliverer creates a ResultDeliverer that writes the agent's output back to a markdown file.
func CreateFileResultDeliverer(filePath string) agentlib.ResultDeliverer {
	return delivery.NewFileResultDeliverer(
		delivery.NewPassthroughContentGenerator(),
		filePath,
	)
}

// CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task updates to Kafka.
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

// CreateAgent assembles the full 3-phase pi agent.
func CreateAgent(
	agentDir string,
	allowedTools string,
	model string,
	piEnv map[string]string,
	envContext map[string]string,
) *agentlib.Agent {
	return CreateAgentFromRunner(
		CreatePiRunner(agentDir, allowedTools, model, piEnv),
		envContext,
	)
}

// CreateAgentFromRunner builds the 3-phase pi agent given a pre-constructed Runner.
func CreateAgentFromRunner(
	runner pilib.Runner,
	envContext map[string]string,
) *agentlib.Agent {
	step := pilib.NewStep(pilib.StepConfig{
		Name:          "pi-task",
		Runner:        runner,
		Instructions:  prompts.BuildInstructions(),
		EnvContext:    envContext,
		OutputSection: "## Result",
		NextPhase:     "done",
	})
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("execution", step),
		agentlib.NewPhase("ai_review", step),
	)
}

// CreateAgentProvider wires the per-task-type dispatch table.
func CreateAgentProvider(
	agentDir string,
	allowedTools string,
	model string,
	piEnv map[string]string,
	envContext map[string]string,
) agentlib.AgentProvider {
	runner := CreatePiRunner(agentDir, allowedTools, model, piEnv)
	domainAgent := CreateAgentFromRunner(runner, envContext)
	livenessAgent := healthcheck.NewAgent(healthcheck.NewPiStep(runner))
	return agentlib.NewAgentProvider(serviceName, map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeClaude:      domainAgent, // Reuse same agent for claude task type too.
		agentlib.TaskTypeHealthcheck: livenessAgent,
		agentlib.TaskTypeOAuthProbe:  livenessAgent,
	})
}

// ParseKeyValuePairs parses "KEY=VALUE,KEY2=VALUE2" into a map.
func ParseKeyValuePairs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	result := map[string]string{}
	pairs := strings.Split(raw, ",")
	for _, p := range pairs {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}
