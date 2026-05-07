// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"

	lib "github.com/bborbe/agent/lib"
)

// UpdateFrontmatterCommandOperation is the Kafka command operation for partial frontmatter update.
// Wire string unchanged: "update-frontmatter".
const UpdateFrontmatterCommandOperation base.CommandOperation = "update-frontmatter"

// UpdateFrontmatterCommand is the payload for UpdateFrontmatterCommandOperation.
// JSON tags are byte-identical to the former lib.UpdateFrontmatterCommand.
type UpdateFrontmatterCommand struct {
	TaskIdentifier lib.TaskIdentifier  `json:"taskIdentifier"`
	Updates        lib.TaskFrontmatter `json:"updates"`
	Body           *BodySection        `json:"body,omitempty"`
}

// BodySection describes an idempotent body-section write for UpdateFrontmatterCommand.
// Heading MUST include the markdown prefix (e.g. "## Failure").
// Section MUST include the heading as its first line and a trailing newline.
type BodySection struct {
	Heading string `json:"heading"`
	Section string `json:"section"`
}

// Validate enforces UpdateFrontmatterCommand schema rules before publishing.
// TaskIdentifier must be non-empty. At least one of Updates (non-empty map) or
// Body (non-nil) must be set — a no-op command with both absent is a producer bug.
func (cmd UpdateFrontmatterCommand) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("TaskIdentifier", cmd.TaskIdentifier),
		validation.Name(
			"UpdatesOrBody",
			validation.HasValidationFunc(func(ctx context.Context) error {
				if len(cmd.Updates) == 0 && cmd.Body == nil {
					return errors.Wrap(
						ctx,
						validation.Error,
						"at least one of Updates or Body must be set",
					)
				}
				return nil
			}),
		),
	}.Validate(ctx)
}
