// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-claude binary.
//
// All factory functions follow the Create* prefix convention and contain
// zero business logic — they compose constructors with config.
package factory

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"

	"github.com/bborbe/agent/agent/claude/pkg/prompts"
	agentlib "github.com/bborbe/agent/lib"
	claudelib "github.com/bborbe/agent/lib/claude"
	delivery "github.com/bborbe/agent/lib/delivery"
	healthcheck "github.com/bborbe/agent/lib/healthcheck"
)

const serviceName = "agent-claude"

// CreateClaudeRunner constructs a ClaudeRunner pre-configured with tools,
// model, working directory, and CLI environment.
func CreateClaudeRunner(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	env map[string]string,
) claudelib.ClaudeRunner {
	return claudelib.NewClaudeRunner(claudelib.ClaudeRunnerConfig{
		ClaudeConfigDir:  claudeConfigDir,
		AllowedTools:     allowedTools,
		Model:            model,
		WorkingDirectory: agentDir,
		Env:              env,
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
// output back to a markdown file (local CLI mode). Uses the passthrough
// content generator (same rationale as Kafka).
func CreateFileResultDeliverer(filePath string) agentlib.ResultDeliverer {
	return delivery.NewFileResultDeliverer(
		delivery.NewPassthroughContentGenerator(),
		filePath,
	)
}

// CreateAgent assembles the full 3-phase claude agent. Single Claude step
// shared across planning / in_progress / ai_review preserves the existing
// CRD trigger.phases behavior — every phase runs Claude once and emits
// done.
func CreateAgent(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) *agentlib.Agent {
	runner := CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv)
	step := claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "claude-task",
		Runner:        runner,
		Instructions:  prompts.BuildInstructions(),
		EnvContext:    envContext,
		OutputSection: "## Result",
		NextPhase:     "done",
	})
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("in_progress", step),
		agentlib.NewPhase("ai_review", step),
	)
}

// CreateAgentForTaskType dispatches on taskType to select the appropriate
// *agentlib.Agent implementation. TaskTypeClaude uses the full 3-phase domain
// agent; TaskTypeHealthcheck and TaskTypeOAuthProbe use the lightweight
// healthcheck agent. Any other value returns an error.
func CreateAgentForTaskType(
	ctx context.Context,
	taskType agentlib.TaskType,
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) (*agentlib.Agent, error) {
	switch taskType {
	case agentlib.TaskTypeClaude:
		return CreateAgent(
			claudeConfigDir,
			agentDir,
			allowedTools,
			model,
			claudeEnv,
			envContext,
		), nil
	case agentlib.TaskTypeHealthcheck, agentlib.TaskTypeOAuthProbe:
		runner := CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv)
		return healthcheck.NewAgent(healthcheck.NewClaudeStep(runner)), nil
	default:
		return nil, errors.Errorf(
			ctx,
			"unknown task_type %q for agent-claude; accepted: [%s %s %s]",
			taskType,
			agentlib.TaskTypeClaude,
			agentlib.TaskTypeHealthcheck,
			agentlib.TaskTypeOAuthProbe,
		)
	}
}

// CreateDeliverer builds the Kafka-or-Noop deliverer used by the Kafka
// entry point. Empty taskID means "no Kafka" — returns a noop deliverer
// and an empty cleanup. Non-empty taskID requires non-empty brokers; the
// returned cleanup closes the underlying SyncProducer (logged-and-ignored
// on error).
func CreateDeliverer(
	ctx context.Context,
	taskID agentlib.TaskIdentifier,
	brokers libkafka.Brokers,
	branch base.Branch,
	originalContent string,
) (agentlib.ResultDeliverer, func(), error) {
	if taskID == "" {
		glog.V(2).Infof("TASK_ID not set, skipping task result publishing")
		return delivery.NewNoopResultDeliverer(), func() {}, nil
	}
	if len(brokers) == 0 {
		return nil, nil, errors.Errorf(ctx, "KAFKA_BROKERS must be set when TASK_ID is set")
	}
	syncProducer, err := CreateSyncProducer(ctx, brokers)
	if err != nil {
		return nil, nil, errors.Wrap(ctx, err, "create sync producer failed")
	}
	deliverer := CreateKafkaResultDeliverer(
		syncProducer,
		branch,
		taskID,
		originalContent,
		libtime.NewCurrentDateTime(),
	)
	cleanup := func() {
		if err := syncProducer.Close(); err != nil {
			glog.Warningf("close sync producer failed: %v", err)
		}
	}
	return deliverer, cleanup, nil
}
