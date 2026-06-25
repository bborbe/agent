// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	stderrors "errors"

	cqrsmocks "github.com/bborbe/cqrs/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent"
	"github.com/bborbe/agent/command/task"
)

var _ = Describe("UpdateFrontmatterCommandSender", func() {
	var (
		ctx        context.Context
		fakeSender *cqrsmocks.CDBCommandObjectSender
		sender     task.UpdateFrontmatterCommandSender
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSender = &cqrsmocks.CDBCommandObjectSender{}
		sender = task.NewUpdateFrontmatterCommandSender(fakeSender)
	})

	It("validation fails → publisher not called", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier(""),
			Updates:        nil,
			Body:           nil,
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(0))
	})

	It("validation passes → publisher called once with correct operation and schemaID", func() {
		fakeSender.SendCommandObjectReturns(nil)
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(Succeed())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
		Expect(cmdObj.Command.Operation).To(Equal(task.UpdateFrontmatterCommandOperation))
		Expect(cmdObj.SchemaID).To(Equal(lib.TaskV1SchemaID))
	})

	It("publisher returns error → error propagated", func() {
		fakeSender.SendCommandObjectReturns(stderrors.New("kafka down"))
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kafka down"))
	})
})
