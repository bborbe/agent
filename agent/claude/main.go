// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/golang/glog"

	"github.com/bborbe/agent/agent/claude/pkg"
	"github.com/bborbe/agent/agent/claude/pkg/factory"
	"github.com/bborbe/agent/agent/claude/pkg/prompts"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN   string `required:"false" arg:"sentry-dsn"   env:"SENTRY_DSN"   usage:"SentryDSN"    display:"length"`
	SentryProxy string `required:"false" arg:"sentry-proxy" env:"SENTRY_PROXY" usage:"Sentry Proxy"`

	// Claude Code CLI configuration
	ClaudeConfigDir pkg.ClaudeConfigDir `required:"false" arg:"claude-config-dir" env:"CLAUDE_CONFIG_DIR" usage:"Claude Code config directory"`

	// Agent directory (contains .claude/ with CLAUDE.md and commands)
	AgentDir pkg.AgentDir `required:"false" arg:"agent-dir" env:"AGENT_DIR" usage:"Agent directory with .claude/ config" default:"agent"`

	// Model selection
	Model pkg.ClaudeModel `required:"false" arg:"model" env:"MODEL" usage:"Claude model to use (sonnet, opus)" default:"sonnet"`

	// Allowed tools (comma-separated)
	AllowedToolsRaw string `required:"false" arg:"allowed-tools" env:"ALLOWED_TOOLS" usage:"Comma-separated list of allowed tools"`

	// Task content from agent pipeline
	TaskContent string `required:"true" arg:"task-content" env:"TASK_CONTENT" usage:"Raw task markdown from vault"`

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Environment variables passed to Claude CLI process (comma-separated KEY=VALUE pairs)
	ClaudeEnvRaw string `required:"false" arg:"claude-env" env:"CLAUDE_ENV" usage:"Comma-separated KEY=VALUE pairs for Claude CLI environment"`
}

func (a *application) Run(ctx context.Context, sentryClient libsentry.Client) error {
	glog.V(2).Infof("agent-claude started")

	taskRunner := factory.CreateTaskRunner(
		a.ClaudeConfigDir,
		a.AgentDir,
		pkg.ParseAllowedTools(a.AllowedToolsRaw),
		a.Model,
		parseKeyValuePairs(a.ClaudeEnvRaw),
		parseKeyValuePairs(a.EnvContextRaw),
		prompts.BuildInstructions(),
		pkg.NewNoopResultDeliverer(),
	)

	result, err := taskRunner.Run(ctx, a.TaskContent)
	if err != nil {
		return pkg.PrintResult(ctx, pkg.AgentResult{
			Status:  pkg.AgentStatusFailed,
			Message: fmt.Sprintf("task runner failed: %v", err),
		})
	}

	return pkg.PrintResult(ctx, *result)
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
