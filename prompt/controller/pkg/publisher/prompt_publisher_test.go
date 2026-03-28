// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher_test

//go:generate go run -mod=mod github.com/maxbrunsfeld/counterfeiter/v6 -generate

import (
	"context"
	"testing"

	"github.com/bborbe/cqrs/cdb"
	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/prompt/controller/pkg/publisher"
)

func TestPublisher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Publisher Suite")
}

var _ = Describe("PromptPublisher", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("publishes prompt via EventObjectSender", func() {
		var capturedEvent cdb.EventObject
		sender := cdb.EventObjectSenderFunc(
			func(c context.Context, e cdb.EventObject) error {
				capturedEvent = e
				return nil
			},
			func(c context.Context, e cdb.EventObject) error {
				return errors.Errorf(c, "unexpected delete")
			},
		)
		p := publisher.NewPromptPublisher(sender, lib.PromptV1SchemaID)
		prompt := lib.Prompt{
			PromptIdentifier: lib.PromptIdentifier("prompt-uuid-1"),
			TaskIdentifier:   lib.TaskIdentifier("task-uuid-1"),
			Assignee:         lib.TaskAssignee("claude"),
			Instruction:      lib.PromptInstruction("do the thing"),
		}
		err := p.PublishPrompt(ctx, prompt)
		Expect(err).To(BeNil())
		Expect(capturedEvent.SchemaID).To(Equal(lib.PromptV1SchemaID))
		Expect(string(capturedEvent.ID)).To(Equal("prompt-uuid-1"))
	})

	It("returns error when EventObjectSender fails", func() {
		sender := cdb.EventObjectSenderFunc(
			func(c context.Context, e cdb.EventObject) error {
				return errors.Errorf(c, "kafka down")
			},
			func(c context.Context, e cdb.EventObject) error { return nil },
		)
		p := publisher.NewPromptPublisher(sender, lib.PromptV1SchemaID)
		prompt := lib.Prompt{
			PromptIdentifier: lib.PromptIdentifier("prompt-uuid-2"),
			TaskIdentifier:   lib.TaskIdentifier("task-uuid-2"),
		}
		err := p.PublishPrompt(ctx, prompt)
		Expect(err).NotTo(BeNil())
	})
})
