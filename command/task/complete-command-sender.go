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

	lib "github.com/bborbe/agent"
)

//counterfeiter:generate -o ../../mocks/task-complete-command-sender.go --fake-name TaskCompleteCommandSender . CompleteCommandSender

// CompleteCommandSender sends CompleteCommand payloads to Kafka.
// Calls Validate before publishing — a validation error is returned without touching Kafka.
type CompleteCommandSender interface {
	SendCommand(ctx context.Context, cmd CompleteCommand) error
}

// NewCompleteCommandSender creates a CompleteCommandSender using the given cdb.CommandObjectSender.
// The defaultVault is substituted into cmd.TargetVault at SendCommand time when
// cmd.TargetVault is empty; an invalid defaultVault surfaces as a validation
// error on the first SendCommand call.
func NewCompleteCommandSender(
	commandObjectSender cdb.CommandObjectSender,
	defaultVault string,
) CompleteCommandSender {
	return &completeCommandSender{
		commandObjectSender: commandObjectSender,
		defaultVault:        defaultVault,
	}
}

type completeCommandSender struct {
	commandObjectSender cdb.CommandObjectSender
	defaultVault        string
}

func (s *completeCommandSender) SendCommand(ctx context.Context, cmd CompleteCommand) error {
	if cmd.TargetVault == "" && s.defaultVault != "" {
		cmd.TargetVault = s.defaultVault
	}
	if err := cmd.Validate(ctx); err != nil {
		return errors.Wrapf(ctx, err, "validate CompleteCommand")
	}
	event, err := base.ParseEvent(ctx, cmd)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse CompleteCommand event")
	}
	requestIDCh := make(chan base.RequestID, 1)
	requestIDCh <- base.NewRequestID()
	commandCreator := base.NewCommandCreator(requestIDCh)
	commandObject := cdb.CommandObject{
		Command: commandCreator.NewCommand(
			CompleteCommandOperation,
			cqrsiam.Initiator("lib"),
			"",
			event,
		),
		SchemaID: lib.TaskV1SchemaID,
	}
	if err := s.commandObjectSender.SendCommandObject(ctx, commandObject); err != nil {
		return errors.Wrapf(ctx, err, "send CompleteCommand to Kafka")
	}
	return nil
}
