// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import (
	"context"

	"github.com/bborbe/cqrs/base"
	cdb "github.com/bborbe/cqrs/cdb"
	cqrsiam "github.com/bborbe/cqrs/iam"
	"github.com/bborbe/errors"
)

//counterfeiter:generate -o mocks/lib-create-task-command-sender.go --fake-name LibCreateTaskCommandSender . CreateTaskCommandSender

// CreateTaskCommandSender sends CreateTaskCommand payloads to Kafka.
// It calls Validate before publishing — a validation error is returned without touching Kafka.
type CreateTaskCommandSender interface {
	SendCommand(ctx context.Context, cmd CreateTaskCommand) error
}

// NewCreateTaskCommandSender creates a CreateTaskCommandSender using the given cdb.CommandObjectSender.
func NewCreateTaskCommandSender(
	commandObjectSender cdb.CommandObjectSender,
) CreateTaskCommandSender {
	return &createTaskCommandSender{
		commandObjectSender: commandObjectSender,
	}
}

type createTaskCommandSender struct {
	commandObjectSender cdb.CommandObjectSender
}

func (s *createTaskCommandSender) SendCommand(ctx context.Context, cmd CreateTaskCommand) error {
	if err := cmd.Validate(ctx); err != nil {
		return errors.Wrapf(ctx, err, "validate CreateTaskCommand")
	}
	event, err := base.ParseEvent(ctx, cmd)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse CreateTaskCommand event")
	}
	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			CreateTaskCommandOperation,
			cqrsiam.Initiator("lib"),
			"",
			event,
		),
		SchemaID: TaskV1SchemaID,
	}
	if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send CreateTaskCommand to Kafka")
	}
	return nil
}
