// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package publisher_test

import (
	"context"
	"errors"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	cqrsmocks "github.com/bborbe/cqrs/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/pkg/publisher"
)

var _ = Describe("TaskPublisher", func() {
	var (
		ctx        context.Context
		fakeSender *cqrsmocks.CDBEventObjectSender
		schemaID   cdb.SchemaID
		tp         publisher.TaskPublisher
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSender = &cqrsmocks.CDBEventObjectSender{}
		schemaID = cdb.SchemaID{
			Group:   "agent",
			Kind:    "task",
			Version: "v1",
		}
		tp = publisher.NewTaskPublisher(fakeSender, schemaID)
	})

	Describe("PublishChanged", func() {
		It("calls SendUpdate with correct EventObject", func() {
			fakeSender.SendUpdateReturns(nil)
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("24 Tasks/test.md"),
				Name:           lib.TaskName("test"),
				Assignee:       lib.TaskAssignee("user@example.com"),
			}

			err := tp.PublishChanged(ctx, task)
			Expect(err).To(BeNil())
			Expect(fakeSender.SendUpdateCallCount()).To(Equal(1))

			_, eventObject := fakeSender.SendUpdateArgsForCall(0)
			Expect(eventObject.SchemaID).To(Equal(schemaID))
			Expect(eventObject.ID).To(Equal(base.EventID("24 Tasks/test.md")))
			Expect(eventObject.Event).NotTo(BeNil())
		})

		It("returns an error when SendUpdate fails", func() {
			fakeSender.SendUpdateReturns(errors.New("kafka down"))
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("24 Tasks/test.md"),
			}

			err := tp.PublishChanged(ctx, task)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("PublishDeleted", func() {
		It("calls SendDelete with correct EventObject", func() {
			fakeSender.SendDeleteReturns(nil)
			id := lib.TaskIdentifier("24 Tasks/deleted.md")

			err := tp.PublishDeleted(ctx, id)
			Expect(err).To(BeNil())
			Expect(fakeSender.SendDeleteCallCount()).To(Equal(1))

			_, eventObject := fakeSender.SendDeleteArgsForCall(0)
			Expect(eventObject.SchemaID).To(Equal(schemaID))
			Expect(eventObject.ID).To(Equal(base.EventID("24 Tasks/deleted.md")))
		})

		It("returns an error when SendDelete fails", func() {
			fakeSender.SendDeleteReturns(errors.New("kafka down"))
			id := lib.TaskIdentifier("24 Tasks/deleted.md")

			err := tp.PublishDeleted(ctx, id)
			Expect(err).NotTo(BeNil())
		})
	})
})
