// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"github.com/bborbe/cqrs/base"
)

// IMPORTANT: operation strings must match base.CommandOperation.Validate regex
// `^[a-z][a-z-]*$` (lowercase letters and hyphens only, starting with a letter).
// Underscores, digits, and uppercase are REJECTED at runtime by cqrs.
// Every constant below MUST also be added to the Validate-all test table in
// agent_task-commands_test.go. CI catches misses there.

// IncrementFrontmatterCommandOperation is the Kafka command operation
// for atomically incrementing a single frontmatter field by a delta.
// Published by the executor on agent-task-v1-request; handled by the controller.
const IncrementFrontmatterCommandOperation base.CommandOperation = "increment-frontmatter"

// UpdateFrontmatterCommandOperation is the Kafka command operation
// for atomically setting specific frontmatter keys without touching other keys.
const UpdateFrontmatterCommandOperation base.CommandOperation = "update-frontmatter"

// CreateTaskCommandOperation is the Kafka command operation for creating a new vault task.
// The controller materializes a task file at the standard vault location for the given
// task_identifier. If a file already exists for that identifier, the command is a no-op.
const CreateTaskCommandOperation base.CommandOperation = "create-task"

// IncrementFrontmatterCommand is the payload for IncrementFrontmatterCommandOperation.
// The controller reads the current value of Field from disk, adds Delta, and writes
// the result atomically — so the write is never idempotent.
type IncrementFrontmatterCommand struct {
	TaskIdentifier TaskIdentifier `json:"taskIdentifier"`
	Field          string         `json:"field"`
	Delta          int            `json:"delta"`
}

// UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
// Merges Updates into the existing frontmatter (partial merge — absent keys preserved).
// When Body is set, its section is appended to (or replaced in) the task body via
// lib/delivery.ReplaceOrAppendSection. Unset Body means frontmatter-only update.
type UpdateFrontmatterCommand struct {
	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
	Updates        TaskFrontmatter `json:"updates"`
	Body           *BodySection    `json:"body,omitempty"`
}

// CreateTaskCommand is the payload for CreateTaskCommandOperation.
// The controller creates a new vault task file at the standard path for TaskIdentifier,
// writing the supplied Frontmatter and optional Body. If a file for TaskIdentifier already
// exists the command is a strict no-op (idempotent). Frontmatter MUST include at minimum
// "assignee" and "status" keys; the executor rejects the command with a validation error
// if either is absent.
type CreateTaskCommand struct {
	TaskIdentifier TaskIdentifier  `json:"taskIdentifier"`
	Frontmatter    TaskFrontmatter `json:"frontmatter"`
	Body           string          `json:"body,omitempty"`
}

// BodySection describes an idempotent body-section write: the controller's
// UpdateFrontmatterExecutor calls ReplaceOrAppendSection(content, Heading, Section).
// Heading MUST include the markdown prefix (e.g. "## Failure"). Section MUST
// include the heading as its first line and a trailing newline.
type BodySection struct {
	Heading string `json:"heading"`
	Section string `json:"section"`
}
