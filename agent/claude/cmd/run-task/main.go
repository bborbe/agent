// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"os"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"

	"github.com/bborbe/agent/agent/claude/pkg/factory"
	"github.com/bborbe/agent/agent/claude/pkg/prompts"
	claudelib "github.com/bborbe/agent/lib/claude"
	libagent "github.com/bborbe/agent/lib/delivery"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// Claude Code CLI configuration
	ClaudeConfigDir claudelib.ClaudeConfigDir `required:"false" arg:"claude-config-dir" env:"CLAUDE_CONFIG_DIR" usage:"Claude Code config directory"`

	// Agent directory (contains .claude/ with CLAUDE.md and commands)
	AgentDir claudelib.AgentDir `required:"false" arg:"agent-dir" env:"AGENT_DIR" usage:"Agent directory with .claude/ config" default:"agent"`

	// Model selection
	Model claudelib.ClaudeModel `required:"false" arg:"model" env:"MODEL" usage:"Claude model to use (sonnet, opus)" default:"sonnet"`

	// Allowed tools (comma-separated)
	AllowedToolsRaw string `required:"false" arg:"allowed-tools" env:"ALLOWED_TOOLS" usage:"Comma-separated list of allowed tools"`

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Environment variables passed to Claude CLI process (comma-separated KEY=VALUE pairs)
	ClaudeEnvRaw string `required:"false" arg:"claude-env" env:"CLAUDE_ENV" usage:"Comma-separated KEY=VALUE pairs for Claude CLI environment"`

	// Environment
	Branch base.Branch `required:"true" arg:"branch" env:"BRANCH" usage:"branch" default:"dev"`

	// Task file for local development
	TaskFilePath string `required:"true" arg:"task-file" env:"TASK_FILE" usage:"Path to the markdown task file"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	taskContent, err := os.ReadFile(
		a.TaskFilePath,
	) // #nosec G304 -- filePath from trusted CLI input
	if err != nil {
		return errors.Wrapf(ctx, err, "read task file: %s", a.TaskFilePath)
	}

	deliverer := claudelib.NewResultDelivererAdapter[claudelib.AgentResult](
		libagent.NewFileResultDeliverer(libagent.NewFallbackContentGenerator(), a.TaskFilePath),
	)

	taskRunner := factory.CreateTaskRunner(
		a.ClaudeConfigDir,
		a.AgentDir,
		claudelib.ParseAllowedTools(a.AllowedToolsRaw),
		a.Model,
		parseKeyValuePairs(a.ClaudeEnvRaw),
		parseKeyValuePairs(a.EnvContextRaw),
		prompts.BuildInstructions(),
		deliverer,
	)

	result, err := taskRunner.Run(ctx, string(taskContent))
	if err != nil {
		return errors.Wrap(ctx, err, "run task")
	}
	return libagent.PrintResult(ctx, *result)
}

// parseKeyValuePairs parses "KEY1=VALUE1,KEY2=VALUE2" into a map.
func parseKeyValuePairs(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}
