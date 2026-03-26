// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/validation"
)

// Prompt represents a work request sent to an agent job via the controller.
// base.Object provides event-level Identifier, Created, Modified.
// PromptIdentifier is the business key for this prompt.
type Prompt struct {
	base.Object[base.Identifier]
	PromptIdentifier PromptIdentifier  `json:"promptIdentifier"`
	TaskIdentifier   TaskIdentifier    `json:"taskIdentifier"`
	Assignee         TaskAssignee      `json:"assignee"`
	Instruction      PromptInstruction `json:"instruction"`
	Parameters       PromptParameters  `json:"parameters,omitempty"`
}

func (p Prompt) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Object", p.Object),
		validation.Name("PromptIdentifier", p.PromptIdentifier),
		validation.Name("TaskIdentifier", p.TaskIdentifier),
		validation.Name("Assignee", p.Assignee),
		validation.Name("Instruction", p.Instruction),
	}.Validate(ctx)
}

func (p Prompt) Ptr() *Prompt {
	return &p
}

// PromptResult represents the outcome of a prompt execution by an agent job.
type PromptResult struct {
	base.Object[base.Identifier]
	PromptIdentifier PromptIdentifier `json:"promptIdentifier"`
	TaskIdentifier   TaskIdentifier   `json:"taskIdentifier"`
	Status           PromptStatus     `json:"status"`
	Output           PromptOutput     `json:"output,omitempty"`
	Message          PromptMessage    `json:"message,omitempty"`
	Links            []string         `json:"links,omitempty"`
}

func (r PromptResult) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Object", r.Object),
		validation.Name("PromptIdentifier", r.PromptIdentifier),
		validation.Name("TaskIdentifier", r.TaskIdentifier),
		validation.Name("Status", r.Status),
	}.Validate(ctx)
}
