// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command run-task is the local-CLI entry point for agent-code.
//
// Reads a markdown task file, runs the agent against it, writes the
// updated content back to the same file. Mirrors the Kafka entry point
// (../../main.go) but uses file I/O instead of Kafka/CQRS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"

	"github.com/bborbe/agent/agent/code/pkg/factory"
	"github.com/bborbe/agent/agent/code/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// Phase to run (defaults to planning; framework requires explicit phase)
	Phase string `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | in_progress | ai_review" default:"planning"`

	// Task file for local development
	TaskFilePath string `required:"true" arg:"task-file" env:"TASK_FILE" usage:"Path to the markdown task file"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	taskContent, err := os.ReadFile(
		a.TaskFilePath,
	) // #nosec G304 -- filePath from trusted CLI input
	if err != nil {
		return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
	}

	deliverer := factory.CreateFileResultDeliverer(a.TaskFilePath)

	agent := agentlib.NewAgent(
		agentlib.NewPhase("planning", steps.NewPlanStep()),
		agentlib.NewPhase("in_progress", steps.NewExecuteStep()),
		agentlib.NewPhase("ai_review", steps.NewVerifyStep()),
	)

	result, err := agent.Run(ctx, a.Phase, string(taskContent), deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	return printResult(result)
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
