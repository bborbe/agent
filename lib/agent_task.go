// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/validation"
)

// Task is the payload published by an agent when it finishes a task.
// task/controller consumes this from agent-task-v1-request and writes it to the vault file.
// Frontmatter is a generic map — task/controller serializes it to YAML without interpreting individual fields.
// Content is the markdown body after the frontmatter closing delimiter.
// The agent owns the content transformation (status, phase, Result section, etc.).
type Task struct {
	base.Object[base.Identifier]
	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
	Frontmatter    TaskFrontmatter `json:"frontmatter"`
	Content        TaskContent     `json:"content"`
}

func (t Task) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Object", t.Object),
		validation.Name("TaskIdentifier", t.TaskIdentifier),
		validation.Name("Content", t.Content),
	}.Validate(ctx)
}

func (t Task) Ptr() *Task {
	return &t
}
