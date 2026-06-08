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

//counterfeiter:generate -o ../../mocks/task-increment-frontmatter-command-sender.go --fake-name TaskIncrementFrontmatterCommandSender . IncrementFrontmatterCommandSender

// IncrementFrontmatterCommandSender sends IncrementFrontmatterCommand payloads to Kafka.
// Calls Validate before publishing — a validation error is returned without touching Kafka.
type IncrementFrontmatterCommandSender interface {
	SendCommand(ctx context.Context, cmd IncrementFrontmatterCommand) error
}

// NewIncrementFrontmatterCommandSender creates an IncrementFrontmatterCommandSender.
func NewIncrementFrontmatterCommandSender(
	commandObjectSender cdb.CommandObjectSender,
) IncrementFrontmatterCommandSender {
	return &incrementFrontmatterCommandSender{
		commandObjectSender: commandObjectSender,
	}
}

type incrementFrontmatterCommandSender struct {
	commandObjectSender cdb.CommandObjectSender
}

func (s *incrementFrontmatterCommandSender) SendCommand(
	ctx context.Context,
	cmd IncrementFrontmatterCommand,
) error {
	if err := cmd.Validate(ctx); err != nil {
		return errors.Wrapf(ctx, err, "validate IncrementFrontmatterCommand")
	}
	event, err := base.ParseEvent(ctx, cmd)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse IncrementFrontmatterCommand event")
	}
	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			IncrementFrontmatterCommandOperation,
			cqrsiam.Initiator("lib"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send IncrementFrontmatterCommand to Kafka")
	}
	return nil
}
