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

//counterfeiter:generate -o mocks/task-create-command-sender.go --fake-name TaskCreateCommandSender . CreateCommandSender

// CreateCommandSender sends CreateCommand payloads to Kafka.
// Calls Validate before publishing — a validation error is returned without touching Kafka.
type CreateCommandSender interface {
	SendCommand(ctx context.Context, cmd CreateCommand) error
}

// NewCreateCommandSender creates a CreateCommandSender using the given cdb.CommandObjectSender.
func NewCreateCommandSender(commandObjectSender cdb.CommandObjectSender) CreateCommandSender {
	return &createCommandSender{
		commandObjectSender: commandObjectSender,
	}
}

type createCommandSender struct {
	commandObjectSender cdb.CommandObjectSender
}

func (s *createCommandSender) SendCommand(ctx context.Context, cmd CreateCommand) error {
	if err := cmd.Validate(ctx); err != nil {
		return errors.Wrapf(ctx, err, "validate CreateCommand")
	}
	event, err := base.ParseEvent(ctx, cmd)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse CreateCommand event")
	}
	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			CreateCommandOperation,
			cqrsiam.Initiator("lib"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send CreateCommand to Kafka")
	}
	return nil
}
