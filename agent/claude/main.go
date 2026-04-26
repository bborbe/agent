// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command agent-claude is the canonical AI-heavy agent: one Claude
// invocation per phase, all logic in the prompt + allowed tools.
//
// This binary is the Kafka entry point — spawned as a K8s Job by
// task/executor with TASK_CONTENT + TASK_ID + PHASE + KAFKA_BROKERS env.
// For local CLI mode (file-based), see cmd/run-task/main.go.
//
// Reference implementation for AI-heavy agents using the agent framework
// (lib.NewAgent + claude.NewAgentStep). Other agents (trade-analysis,
// pr-reviewer) follow the same shape — copy this main.go and swap
// prompts/tools.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"

	"github.com/bborbe/agent/agent/claude/pkg/factory"
	"github.com/bborbe/agent/agent/claude/pkg/prompts"
	agentlib "github.com/bborbe/agent/lib"
	claudelib "github.com/bborbe/agent/lib/claude"
	delivery "github.com/bborbe/agent/lib/delivery"
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

	// Task content from agent pipeline
	TaskContent string `required:"true" arg:"task-content" env:"TASK_CONTENT" usage:"Raw task markdown from vault"`

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Environment variables passed to Claude CLI process (comma-separated KEY=VALUE pairs)
	ClaudeEnvRaw string `required:"false" arg:"claude-env" env:"CLAUDE_ENV" usage:"Comma-separated KEY=VALUE pairs for Claude CLI environment"`

	// Branch for Kafka result delivery
	Branch base.Branch `required:"true" arg:"branch" env:"BRANCH" usage:"branch"`

	// Kafka delivery (optional — only active when TASK_ID is set)
	KafkaBrokers libkafka.Brokers `required:"false" arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma separated list of Kafka brokers"`
	TaskID       string           `required:"false" arg:"task-id"       env:"TASK_ID"       usage:"Agent task identifier for publishing results back to task controller"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	phase := os.Getenv("PHASE")
	if phase == "" {
		phase = "in_progress"
	}
	glog.V(2).Infof("agent-claude started phase=%s", phase)

	deliverer, cleanup, err := a.createDeliverer(ctx)
	if err != nil {
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

	runner := factory.CreateClaudeRunner(
		a.ClaudeConfigDir,
		a.AgentDir,
		claudelib.ParseAllowedTools(a.AllowedToolsRaw),
		a.Model,
		parseKeyValuePairs(a.ClaudeEnvRaw),
	)

	step := claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "claude-task",
		Runner:        runner,
		Instructions:  prompts.BuildInstructions(),
		EnvContext:    parseKeyValuePairs(a.EnvContextRaw),
		OutputSection: "## Result",
		NextPhase:     "done",
	})

	// Three phases share the same Claude step — preserves the existing
	// behavior where this agent runs Claude on any phase trigger and
	// emits done. The CRD trigger.phases is [planning, in_progress, ai_review].
	agent := agentlib.NewAgent(
		agentlib.NewPhase("planning", step),
		agentlib.NewPhase("in_progress", step),
		agentlib.NewPhase("ai_review", step),
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

// printResult marshals the framework Result to JSON and prints to stdout.
// Single-shot replacement for the legacy claudelib.PrintResult.
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
