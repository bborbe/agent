// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang/glog"
)

//counterfeiter:generate -o ../mocks/pkg-task-runner.go --fake-name PkgTaskRunner . TaskRunner

// TaskRunner orchestrates task execution by launching a single Claude Code session.
type TaskRunner interface {
	Run(ctx context.Context, taskContent string) (*AgentResult, error)
}

// NewTaskRunner creates a TaskRunner with injected dependencies.
func NewTaskRunner(
	runner ClaudeRunner,
	instructions Instructions,
	envContext map[string]string,
	deliverer ResultDeliverer,
) TaskRunner {
	return &taskRunner{
		runner:       runner,
		instructions: instructions,
		envContext:   envContext,
		deliverer:    deliverer,
	}
}

type taskRunner struct {
	runner       ClaudeRunner
	instructions Instructions
	envContext   map[string]string
	deliverer    ResultDeliverer
}

func (r *taskRunner) Run(ctx context.Context, taskContent string) (*AgentResult, error) {
	taskContent = strings.TrimSpace(taskContent)
	if taskContent == "" {
		return r.deliver(ctx, AgentResult{
			Status:  AgentStatusNeedsInput,
			Message: "task content is empty",
		})
	}

	prompt := BuildPrompt(
		r.instructions.String(),
		r.envContext,
		taskContent,
	)

	glog.V(2).Infof("launching claude CLI for task execution")

	result, err := r.runner.Run(ctx, prompt)
	if err != nil {
		return r.deliver(ctx, AgentResult{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("claude CLI failed: %v", err),
		})
	}

	var agentResult AgentResult
	if err := json.Unmarshal([]byte(result.Result), &agentResult); err != nil {
		return r.deliver(ctx, AgentResult{
			Status:  AgentStatusFailed,
			Message: fmt.Sprintf("parse claude result failed: %s", result.Result),
		})
	}

	return r.deliver(ctx, agentResult)
}

// deliver sends the result to the deliverer and returns it.
func (r *taskRunner) deliver(ctx context.Context, result AgentResult) (*AgentResult, error) {
	if err := r.deliverer.DeliverResult(ctx, result); err != nil {
		glog.Warningf("deliver result failed: %v", err)
	}
	return &result, nil
}
