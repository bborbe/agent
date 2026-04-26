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
	"github.com/golang/glog"

	"github.com/bborbe/agent/agent/code/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
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
		agentlib.NewPhase("in_progress", steps.NewExecuteStep()),
		agentlib.NewPhase("ai_review", steps.NewVerifyStep()),
	)
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
