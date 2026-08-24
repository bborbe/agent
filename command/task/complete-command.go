// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	"github.com/bborbe/validation"

	lib "github.com/bborbe/agent"
)

// CompleteCommandOperation is the Kafka command operation for closing an open
// vault task as completed. Wire string unchanged: "complete-task".
const CompleteCommandOperation base.CommandOperation = "complete-task"

// CompleteCommand is the payload for CompleteCommandOperation.
//
// It closes the vault task whose task_identifier matches TaskIdentifier,
// transitioning it to status: completed, phase: done with a # Resolution
// body section carrying the recovery SHA. The watcher publishes this on a
// red → green build transition (spec 076); the task controller applies it.
type CompleteCommand struct {
	TaskIdentifier lib.TaskIdentifier `json:"taskIdentifier"`
	// RecoverySHA is the default-branch HEAD (40-char hex) at close time, for audit.
	RecoverySHA string `json:"recoverySha,omitempty"`
	// TargetVault is the slug of the Obsidian vault this task belongs in.
	// Empty value means "use the controller's legacy default (openclaw)".
	// Wire format uses omitempty so legacy producers that never set it stay byte-compatible.
	TargetVault string `json:"targetVault,omitempty"`
}

// Validate enforces CompleteCommand schema rules before publishing or processing.
func (cmd CompleteCommand) Validate(ctx context.Context) error {
	return validation.All{
		validation.Name("TaskIdentifier", validateCompleteTaskIdentifier(cmd.TaskIdentifier)),
		validation.Name("RecoverySHA", validateRecoverySHA(cmd.RecoverySHA)),
		validation.Name("TargetVault", validateCreateTargetVault(cmd.TargetVault)),
	}.Validate(ctx)
}

func validateCompleteTaskIdentifier(id lib.TaskIdentifier) validation.HasValidation {
	return validation.HasValidationFunc(func(ctx context.Context) error {
		return id.Validate(ctx)
	})
}

func validateRecoverySHA(recoverySHA string) validation.HasValidation {
	return validation.HasValidationFunc(func(ctx context.Context) error {
		if recoverySHA == "" {
			return nil
		}
		if len(recoverySHA) != 40 {
			return errors.Wrapf(
				ctx,
				validation.Error,
				"recoverySha %q must be a 40-char hex SHA (got %d chars)",
				recoverySHA,
				len(recoverySHA),
			)
		}
		for _, r := range recoverySHA {
			if !isHexRune(r) {
				return errors.Wrapf(
					ctx,
					validation.Error,
					"recoverySha %q must be hex (invalid char %q)",
					recoverySHA,
					r,
				)
			}
		}
		return nil
	})
}

func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
