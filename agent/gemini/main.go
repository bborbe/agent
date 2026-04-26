// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command agent-gemini is the canonical boundary-translator agent reference.
//
// Demonstrates the canonical AI usage pattern: Gemini structured-output
// parses fuzzy human-written markdown into a typed Plan struct at the
// planning boundary, then pure-Go ExecuteStep + VerifyStep do the work
// deterministically. AI only at the boundary — code everywhere else.
//
// Three phases (planning, in_progress, ai_review). Planning uses
// lib.NewParseStep[Plan] (generic boundary translator); the other two
// are pure Go. Useful template for agents that take fuzzy human input
// but produce deterministic results.
//
// Kafka entry point — spawned as a K8s Job by task/executor with
// TASK_CONTENT + TASK_ID + PHASE + KAFKA_BROKERS + GEMINI_API_KEY env.
// For local CLI mode (file-based), see cmd/run-task/main.go.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"

	"github.com/bborbe/agent/agent/gemini/pkg/factory"
	"github.com/bborbe/agent/agent/gemini/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
	delivery "github.com/bborbe/agent/lib/delivery"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// Task content from agent pipeline
	TaskContent string `required:"true" arg:"task-content" env:"TASK_CONTENT" usage:"Raw task markdown from vault"`

	// Branch for Kafka result delivery
	Branch base.Branch `required:"true" arg:"branch" env:"BRANCH" usage:"branch"`

	// Kafka delivery (optional — only active when TASK_ID is set)
	KafkaBrokers libkafka.Brokers `required:"false" arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma separated list of Kafka brokers"`
	TaskID       string           `required:"false" arg:"task-id"       env:"TASK_ID"       usage:"Agent task identifier for publishing results back to task controller"`

	// Gemini API
	GeminiAPIKey string `required:"true"  arg:"gemini-api-key" env:"GEMINI_API_KEY" usage:"Gemini API key"          display:"length"`
	GeminiModel  string `required:"false" arg:"gemini-model"   env:"GEMINI_MODEL"   usage:"Gemini model identifier"                  default:"gemini-2.0-flash"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	phase := os.Getenv("PHASE")
	if phase == "" {
		phase = "planning"
	}
	glog.V(2).Infof("agent-gemini started phase=%s", phase)

	parser, err := factory.CreateGeminiParser(ctx, a.GeminiAPIKey, a.GeminiModel)
	if err != nil {
		return errors.Wrap(ctx, err, "create gemini parser")
	}

	deliverer, cleanup, err := a.createDeliverer(ctx)
	if err != nil {
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

	agent := agentlib.NewAgent(
		agentlib.NewPhase(
			"planning",
			agentlib.NewParseStep[steps.Plan]("parse-plan", parser, "## Plan", "in_progress"),
		),
		agentlib.NewPhase("in_progress", steps.NewExecuteStep()),
		agentlib.NewPhase("ai_review", steps.NewVerifyStep()),
	)

	result, err := agent.Run(ctx, phase, a.TaskContent, deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	return printResult(result)
}

func (a *application) createDeliverer(
	ctx context.Context,
) (agentlib.ResultDeliverer, func(), error) {
	if a.TaskID != "" {
		if len(a.KafkaBrokers) == 0 {
			return nil, nil, errors.Errorf(ctx, "KAFKA_BROKERS must be set when TASK_ID is set")
		}
		syncProducer, err := factory.CreateSyncProducer(ctx, a.KafkaBrokers)
		if err != nil {
			return nil, nil, errors.Wrap(ctx, err, "create sync producer failed")
		}
		taskID := agentlib.TaskIdentifier(a.TaskID)
		deliverer := factory.CreateKafkaResultDeliverer(
			syncProducer,
			a.Branch,
			taskID,
			a.TaskContent,
			libtime.NewCurrentDateTime(),
		)
		return deliverer, func() {
			if err := syncProducer.Close(); err != nil {
				glog.Warningf("close sync producer failed: %v", err)
			}
		}, nil
	}
	glog.V(2).Infof("TASK_ID not set, skipping task result publishing")
	return delivery.NewNoopResultDeliverer(), func() {}, nil
}

// printResult marshals the framework Result to JSON and prints to stdout.
func printResult(result *agentlib.Result) error {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
