// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	stderrors "errors"

	cqrsmocks "github.com/bborbe/cqrs/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("CreateTaskCommandSender", func() {
	var (
		ctx        context.Context
		fakeSender *cqrsmocks.CDBCommandObjectSender
		sender     lib.CreateTaskCommandSender
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSender = &cqrsmocks.CDBCommandObjectSender{}
		sender = lib.NewCreateTaskCommandSender(fakeSender)
	})

	It("validation fails → publisher not called", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(0))
	})

	It("validation passes → publisher called once with correct operation and schemaID", func() {
		fakeSender.SendCommandObjectReturns(nil)
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(Succeed())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
		Expect(cmdObj.Command.Operation).To(Equal(lib.CreateTaskCommandOperation))
		Expect(cmdObj.SchemaID).To(Equal(lib.TaskV1SchemaID))
	})

	It("publisher returns error → error propagated", func() {
		fakeSender.SendCommandObjectReturns(stderrors.New("kafka down"))
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kafka down"))
	})
})
