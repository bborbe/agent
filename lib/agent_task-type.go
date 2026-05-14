// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"
	"regexp"

	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

var taskTypeRegexp = regexp.MustCompile(`^[a-z0-9-]+$`)

// TaskType identifies the category of work a task represents.
// Matched against the agent's declared task-type set before spawning a Job.
type TaskType string

func (t TaskType) String() string {
	return string(t)
}

func (t TaskType) Bytes() []byte {
	return []byte(t)
}

func (t TaskType) Ptr() *TaskType {
	return &t
}

// Validate returns an error when the task type is empty, contains characters
// outside [a-z0-9-], or exceeds 63 characters — matching the CRD-side constraint.
func (t TaskType) Validate(ctx context.Context) error {
	if t == "" {
		return errors.Wrap(ctx, validation.Error, "task type missing")
	}
	if len(t) > 63 {
		return errors.Wrap(ctx, validation.Error, "task type exceeds 63 characters")
	}
	if !taskTypeRegexp.MatchString(string(t)) {
		return errors.Wrap(ctx, validation.Error, "task type must match ^[a-z0-9-]+$")
	}
	return nil
}

const (
	// TaskTypeClaude is the task type for Claude agent jobs.
	TaskTypeClaude TaskType = "claude"
	// TaskTypePRReview is the task type for PR review jobs.
	TaskTypePRReview TaskType = "pr-review"
	// TaskTypeBacktest is the task type for backtesting jobs.
	TaskTypeBacktest TaskType = "backtest"
	// TaskTypeHypothesis is the task type for hypothesis evaluation jobs.
	TaskTypeHypothesis TaskType = "hypothesis"
	// TaskTypeTradeAnalysis is the task type for trade analysis jobs.
	TaskTypeTradeAnalysis TaskType = "trade-analysis"
	// TaskTypeOAuthProbe is the task type for OAuth probe health-check jobs.
	//
	// Deprecated: use TaskTypeHealthcheck once introduced by the oauth-probe rename spec.
	TaskTypeOAuthProbe TaskType = "oauth-probe"
)
