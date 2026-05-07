// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task

import (
	"context"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o mocks/task-update-frontmatter-command-sender.go --fake-name TaskUpdateFrontmatterCommandSender . UpdateFrontmatterCommandSender

// UpdateFrontmatterCommandSender sends UpdateFrontmatterCommand payloads to Kafka.
// Calls Validate before publishing — a validation error is returned without touching Kafka.
type UpdateFrontmatterCommandSender interface {
	SendCommand(ctx context.Context, cmd UpdateFrontmatterCommand) error
}

// NewUpdateFrontmatterCommandSender creates an UpdateFrontmatterCommandSender.
func NewUpdateFrontmatterCommandSender(
	commandObjectSender cdb.CommandObjectSender,
) UpdateFrontmatterCommandSender {
	return &updateFrontmatterCommandSender{
		commandObjectSender: commandObjectSender,
	}
}

type updateFrontmatterCommandSender struct {
	commandObjectSender cdb.CommandObjectSender
}

func (s *updateFrontmatterCommandSender) SendCommand(
	ctx context.Context,
	cmd UpdateFrontmatterCommand,
) error {
	if err := cmd.Validate(ctx); err != nil {
		return errors.Wrapf(ctx, err, "validate UpdateFrontmatterCommand")
	}
	event, err := base.ParseEvent(ctx, cmd)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse UpdateFrontmatterCommand event")
	}
	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			UpdateFrontmatterCommandOperation,
			cqrsiam.Initiator("lib"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send UpdateFrontmatterCommand to Kafka")
	}
	return nil
}
