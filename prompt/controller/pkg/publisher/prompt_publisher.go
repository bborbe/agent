// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher

import (
	"context"
	"time"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/google/uuid"

	lib "github.com/bborbe/agent/lib"
)

//counterfeiter:generate -o ../../mocks/prompt_publisher.go --fake-name FakePromptPublisher . PromptPublisher

// PromptPublisher publishes prompt events to the event bus.
type PromptPublisher interface {
	PublishPrompt(ctx context.Context, prompt lib.Prompt) error
}

// NewPromptPublisher creates a new PromptPublisher backed by EventObjectSender.
func NewPromptPublisher(
	eventObjectSender cdb.EventObjectSender,
	schemaID cdb.SchemaID,
) PromptPublisher {
	return &promptPublisher{
		eventObjectSender: eventObjectSender,
		schemaID:          schemaID,
	}
}

type promptPublisher struct {
	eventObjectSender cdb.EventObjectSender
	schemaID          cdb.SchemaID
}

func (p *promptPublisher) PublishPrompt(ctx context.Context, prompt lib.Prompt) error {
	now := libtime.DateTime(time.Now())
	prompt.Object = base.Object[base.Identifier]{
		Identifier: base.Identifier(uuid.New().String()),
		Created:    now,
		Modified:   now,
	}
	event, err := base.ParseEvent(ctx, prompt)
	if err != nil {
		return errors.Wrapf(ctx, err, "parse event for prompt %s failed", prompt.PromptIdentifier)
	}
	if err := p.eventObjectSender.SendUpdate(ctx, cdb.EventObject{
		Event:    event,
		ID:       base.EventID(prompt.PromptIdentifier),
		SchemaID: p.schemaID,
	}); err != nil {
		return errors.Wrapf(ctx, err, "publish prompt %s failed", prompt.PromptIdentifier)
	}
	return nil
}
