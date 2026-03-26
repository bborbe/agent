// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/collection"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"
)

const (
	TodoTaskStatus        TaskStatus = "todo"
	InProgressTaskStatus  TaskStatus = "in_progress"
	HumanReviewTaskStatus TaskStatus = "human_review"
	DoneTaskStatus        TaskStatus = "done"
)

// AvailableTaskStatuses lists all valid task statuses.
var AvailableTaskStatuses = TaskStatuses{
	TodoTaskStatus,
	InProgressTaskStatus,
	HumanReviewTaskStatus,
	DoneTaskStatus,
}

// TaskStatuses is a slice of TaskStatus.
type TaskStatuses []TaskStatus

// Contains returns true if the slice contains the given status.
func (t TaskStatuses) Contains(status TaskStatus) bool {
	return collection.Contains(t, status)
}

// TaskStatus represents the current state of an agent task.
type TaskStatus string

func (t TaskStatus) Validate(ctx context.Context) error {
	if !AvailableTaskStatuses.Contains(t) {
		return errors.Wrapf(ctx, validation.Error, "unknown task status '%s'", t)
	}
	return nil
}

func (t TaskStatus) String() string {
	return string(t)
}
