// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	libtime "github.com/bborbe/time"
	"github.com/bborbe/validation"
)

// Task represents an agent task managed via CQRS over Kafka.
type Task struct {
	Identifier   TaskIdentifier   `json:"identifier"`
	Created      libtime.DateTime `json:"created"`
	Modified     libtime.DateTime `json:"modified"`
	Name         string           `json:"name"`
	Status       TaskStatus       `json:"status"`
	Assignee     TaskAssignee     `json:"assignee"`
	Content      string           `json:"content"`
	ExecutionLog ExecutionLog     `json:"executionLog,omitempty"`
}

func (t Task) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Identifier", t.Identifier),
		validation.Name("Created", t.Created),
		validation.Name("Modified", t.Modified),
		validation.Name("Status", t.Status),
		validation.Name("Assignee", t.Assignee),
	}.Validate(ctx)
}

func (t Task) Ptr() *Task {
	return &t
}
