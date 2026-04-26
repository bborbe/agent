// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-gemini binary.
//
// Boundary-translator agent — Gemini parses fuzzy markdown into typed
// structs at the planning boundary; the rest is pure code.
package factory

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"

	"github.com/bborbe/agent/agent/gemini/pkg/parser"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
)

const serviceName = "agent-gemini"

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

// CreateGeminiParser constructs a Gemini-backed AIParser for use with
// lib.NewParseStep. Returns the AIParser interface so callers stay
// decoupled from the concrete Gemini implementation.
func CreateGeminiParser(
	ctx context.Context,
	apiKey string,
	model string,
) (agentlib.AIParser, error) {
	p, err := parser.New(apiKey, model)
	if err != nil {
		return nil, errors.Wrap(ctx, err, "create gemini parser failed")
	}
	return p, nil
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
