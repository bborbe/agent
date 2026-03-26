// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/validation"
	"github.com/bborbe/vault-cli/pkg/domain"
)

// Task represents an agent task managed via CQRS over Kafka.
// base.Object provides event-level Identifier, Created, Modified.
// TaskIdentifier is the business key for this task.
type Task struct {
	base.Object[base.Identifier]
	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
	Name           TaskName        `json:"name"`
	Status         domain.TaskStatus `json:"status"`
	Phase          *domain.TaskPhase `json:"phase,omitempty"`
	Assignee       TaskAssignee    `json:"assignee"`
	Content        TaskContent     `json:"content"`
}

func (t Task) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("Object", t.Object),
		validation.Name("TaskIdentifier", t.TaskIdentifier),
		validation.Name("Status", t.Status),
		validation.Name("Assignee", t.Assignee),
	}.Validate(ctx)
}

func (t Task) Ptr() *Task {
	return &t
}
