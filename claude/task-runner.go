// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang/glog"
)

//counterfeiter:generate -o ../mocks/claude-task-runner.go --fake-name ClaudeTaskRunner . TaskRunner

// TaskRunner orchestrates task execution by launching a single Claude Code session.
type TaskRunner[T AgentResultLike] interface {
	Run(ctx context.Context, taskContent string) (*T, error)
}

// NewTaskRunner creates a TaskRunner with injected dependencies.
func NewTaskRunner[T AgentResultLike](
	runner ClaudeRunner,
	instructions Instructions,
	envContext map[string]string,
	deliverer ResultDeliverer[T],
) TaskRunner[T] {
	return &taskRunner[T]{
		runner:       runner,
		instructions: instructions,
		envContext:   envContext,
		deliverer:    deliverer,
	}
}

type taskRunner[T AgentResultLike] struct {
	runner       ClaudeRunner
	instructions Instructions
	envContext   map[string]string
	deliverer    ResultDeliverer[T]
}

func (r *taskRunner[T]) Run(ctx context.Context, taskContent string) (*T, error) {
	taskContent = strings.TrimSpace(taskContent)
	if taskContent == "" {
		return r.deliver(ctx, newErrorResult[T](AgentStatusNeedsInput, "task content is empty"))
	}

	prompt := BuildPrompt(
		r.instructions.String(),
		r.envContext,
		taskContent,
	)

	glog.V(2).Infof("launching claude CLI for task execution")

	result, err := r.runner.Run(ctx, prompt)
	if err != nil {
		return r.deliver(
			ctx,
			newErrorResult[T](AgentStatusFailed, fmt.Sprintf("claude CLI failed: %v", err)),
		)
	}

	jsonBlob, ok := extractLastJSONObject(result.Result)
	if !ok {
		return r.deliver(ctx, newErrorResult[T](AgentStatusFailed, fmt.Sprintf(
			"parse claude result failed (no JSON object found): %s",
			result.Result,
		)))
	}
	var agentResult T
	if err := json.Unmarshal([]byte(jsonBlob), &agentResult); err != nil {
		return r.deliver(ctx, newErrorResult[T](AgentStatusFailed, fmt.Sprintf(
			"parse claude result failed: %v (raw: %s)",
			err, result.Result,
		)))
	}

	return r.deliver(ctx, agentResult)
}

func (r *taskRunner[T]) deliver(ctx context.Context, result T) (*T, error) {
	if err := r.deliverer.DeliverResult(ctx, result); err != nil {
		glog.Warningf("deliver result failed: %v", err)
	}
	return &result, nil
}

// newErrorResult creates a T with status and message set via JSON round-trip.
func newErrorResult[T AgentResultLike](status AgentStatus, message string) T {
	var zero T
	data, _ := json.Marshal(AgentResult{Status: status, Message: message})
	_ = json.Unmarshal(data, &zero)
	return zero
}

// extractLastJSONObject scans s for the last balanced top-level JSON object and returns it.
// Robust to Claude emitting narrative prose around the result (spec 010).
// String literals and escaped braces are handled correctly.
func extractLastJSONObject(s string) (string, bool) {
	sc := jsonScanner{bestStart: -1, bestEnd: -1, start: -1}
	for i := 0; i < len(s); i++ {
		sc.step(i, s[i])
	}
	if sc.bestStart < 0 || sc.bestEnd < 0 {
		return "", false
	}
	return s[sc.bestStart : sc.bestEnd+1], true
}

type jsonScanner struct {
	bestStart, bestEnd int
	depth              int
	start              int
	inString           bool
	escaped            bool
}

func (sc *jsonScanner) step(i int, c byte) {
	if sc.inString {
		sc.stepString(c)
		return
	}
	sc.stepCode(i, c)
}

func (sc *jsonScanner) stepString(c byte) {
	if sc.escaped {
		sc.escaped = false
		return
	}
	switch c {
	case '\\':
		sc.escaped = true
	case '"':
		sc.inString = false
	}
}

func (sc *jsonScanner) stepCode(i int, c byte) {
	switch c {
	case '"':
		sc.inString = true
	case '{':
		if sc.depth == 0 {
			sc.start = i
		}
		sc.depth++
	case '}':
		sc.closeBrace(i)
	}
}

func (sc *jsonScanner) closeBrace(i int) {
	if sc.depth == 0 {
		return
	}
	sc.depth--
	if sc.depth == 0 && sc.start >= 0 {
		sc.bestStart, sc.bestEnd = sc.start, i
		sc.start = -1
	}
}
