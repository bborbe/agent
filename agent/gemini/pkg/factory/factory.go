// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-gemini binary.
//
// Boundary-translator agent — Gemini parses fuzzy markdown into typed
// structs at the planning boundary; the rest is pure code.
package factory

import (
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent/agent/gemini/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
	healthcheck "github.com/bborbe/agent/lib/healthcheck"
)

const ServiceName = "agent-gemini"

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

// CreateAgent assembles the 3-phase boundary-translator agent. Planning
// uses generic ParseStep[Plan] backed by Gemini structured output; the
// other two phases are pure-Go ExecuteStep + VerifyStep.
func CreateAgent(geminiParser agentlib.AIParser) *agentlib.Agent {
	return agentlib.NewAgent(
		agentlib.NewPhase(
			"planning",
			agentlib.NewParseStep[steps.Plan](
				"parse-plan",
				geminiParser,
				"## Plan",
				string(domain.TaskPhaseExecution),
			),
		),
		agentlib.NewPhase(domain.TaskPhaseExecution, steps.NewExecuteStep()),
		agentlib.NewPhase("ai_review", steps.NewVerifyStep()),
	)
}

// CreateAgentProvider wires the per-task-type dispatch for agent-gemini.
// Healthcheck-only binary: TaskTypeHealthcheck routes to the gemini liveness
// agent; any other value hits the default-error branch of lib.AgentProvider.Get.
func CreateAgentProvider(parser agentlib.AIParser) agentlib.AgentProvider {
	livenessAgent := healthcheck.NewAgent(healthcheck.NewGeminiStep(parser))
	return agentlib.NewAgentProvider("agent-gemini", map[agentlib.TaskType]*agentlib.Agent{
		agentlib.TaskTypeHealthcheck: livenessAgent,
	})
}
