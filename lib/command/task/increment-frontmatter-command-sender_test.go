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

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/command/task"
)

var _ = Describe("IncrementFrontmatterCommandSender", func() {
	var (
		ctx        context.Context
		fakeSender *cqrsmocks.CDBCommandObjectSender
		sender     task.IncrementFrontmatterCommandSender
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSender = &cqrsmocks.CDBCommandObjectSender{}
		sender = task.NewIncrementFrontmatterCommandSender(fakeSender)
	})

	It("validation fails → publisher not called", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "",
			Delta:          1,
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(0))
	})

	It("validation passes → publisher called once with correct operation and schemaID", func() {
		fakeSender.SendCommandObjectReturns(nil)
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "trigger_count",
			Delta:          1,
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(Succeed())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
		Expect(cmdObj.Command.Operation).To(Equal(task.IncrementFrontmatterCommandOperation))
		Expect(cmdObj.SchemaID).To(Equal(lib.TaskV1SchemaID))
	})

	It("publisher returns error → error propagated", func() {
		fakeSender.SendCommandObjectReturns(stderrors.New("kafka down"))
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "trigger_count",
			Delta:          1,
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kafka down"))
	})
})
