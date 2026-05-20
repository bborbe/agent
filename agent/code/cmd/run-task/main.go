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
	"os"

	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent/agent/code/pkg/factory"
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
	Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"planning"`

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

	result, err := factory.CreateAgent().Run(ctx, a.Phase, string(taskContent), deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	return agentlib.PrintResult(result)
}
