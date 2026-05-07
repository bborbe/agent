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

// IncrementFrontmatterCommandOperation is the Kafka command operation for atomic field increment.
// Wire string unchanged: "increment-frontmatter".
const IncrementFrontmatterCommandOperation base.CommandOperation = "increment-frontmatter"

// IncrementFrontmatterCommand is the payload for IncrementFrontmatterCommandOperation.
// JSON tags are byte-identical to the former lib.IncrementFrontmatterCommand.
type IncrementFrontmatterCommand struct {
	TaskIdentifier lib.TaskIdentifier `json:"taskIdentifier"`
	Field          string             `json:"field"`
	Delta          int                `json:"delta"`
}

// Validate enforces IncrementFrontmatterCommand schema rules before publishing.
// TaskIdentifier and Field must be non-empty. Delta is unconstrained (zero and negative are valid).
func (cmd IncrementFrontmatterCommand) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("TaskIdentifier", cmd.TaskIdentifier),
		validation.Name("Field", validation.HasValidationFunc(func(ctx context.Context) error {
			if cmd.Field == "" {
				return errors.Wrap(ctx, validation.Error, "field must not be empty")
			}
			return nil
		})),
	}.Validate(ctx)
}
