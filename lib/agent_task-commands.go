// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"github.com/bborbe/cqrs/base"
)

// IncrementFrontmatterCommandOperation is the Kafka command operation
// for atomically incrementing a single frontmatter field by a delta.
// Published by the executor on agent-task-v1-request; handled by the controller.
const IncrementFrontmatterCommandOperation base.CommandOperation = "increment_frontmatter"

// UpdateFrontmatterCommandOperation is the Kafka command operation
// for atomically setting specific frontmatter keys without touching other keys.
const UpdateFrontmatterCommandOperation base.CommandOperation = "update_frontmatter"

// IncrementFrontmatterCommand is the payload for IncrementFrontmatterCommandOperation.
// The controller reads the current value of Field from disk, adds Delta, and writes
// the result atomically — so the write is never idempotent.
type IncrementFrontmatterCommand struct {
	TaskIdentifier TaskIdentifier `json:"taskIdentifier"`
	Field          string         `json:"field"`
	Delta          int            `json:"delta"`
}

// UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
// The controller applies only the listed key-value pairs; all other frontmatter
// keys in the task file are left unchanged.
type UpdateFrontmatterCommand struct {
	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
	Updates        TaskFrontmatter `json:"updates"`
}
