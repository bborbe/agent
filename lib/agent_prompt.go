// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	libtime "github.com/bborbe/time"
	"github.com/bborbe/validation"
)

// Prompt represents a work request sent to an agent job via the controller.
type Prompt struct {
	Identifier   PromptIdentifier `json:"identifier"`
	Created      libtime.DateTime `json:"created"`
	Modified     libtime.DateTime `json:"modified"`
	TaskID       TaskIdentifier   `json:"taskId"`
	Assignee     TaskAssignee     `json:"assignee"`
	Instruction  string           `json:"instruction"`
	Parameters   map[string]string `json:"parameters,omitempty"`
	ExecutionLog ExecutionLog     `json:"executionLog,omitempty"`
}

func (p Prompt) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Identifier", p.Identifier),
		validation.Name("Created", p.Created),
		validation.Name("Modified", p.Modified),
		validation.Name("TaskID", p.TaskID),
		validation.Name("Assignee", p.Assignee),
	}.Validate(ctx)
}

func (p Prompt) Ptr() *Prompt {
	return &p
}

// PromptResult represents the outcome of a prompt execution by an agent job.
type PromptResult struct {
	Identifier PromptIdentifier `json:"identifier"`
	PromptID   PromptIdentifier `json:"promptId"`
	TaskID     TaskIdentifier   `json:"taskId"`
	Status     PromptStatus     `json:"status"`
	Output     string           `json:"output,omitempty"`
	Message    string           `json:"message,omitempty"`
	Links      []string         `json:"links,omitempty"`
}

func (r PromptResult) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Identifier", r.Identifier),
		validation.Name("PromptID", r.PromptID),
		validation.Name("TaskID", r.TaskID),
		validation.Name("Status", r.Status),
	}.Validate(ctx)
}
