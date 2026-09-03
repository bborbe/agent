// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"encoding/json"
	stderrors "errors"

	cqrsmocks "github.com/bborbe/cqrs/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/command/task"
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
		sender = task.NewIncrementFrontmatterCommandSender(fakeSender, "")
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

	Context("defaultVault substitution", func() {
		// publishedCmd decodes the embedded IncrementFrontmatterCommand from the
		// most recent published CommandObject. It captures ctx and fakeSender
		// from the outer BeforeEach and reads the most recent call.
		publishedCmd := func() task.IncrementFrontmatterCommand {
			_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
			var got task.IncrementFrontmatterCommand
			Expect(cmdObj.Command.Data.MarshalInto(ctx, &got)).To(Succeed())
			return got
		}

		It("defaultVault 'personal' fills empty TargetVault", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewIncrementFrontmatterCommandSender(fakeSender, "personal")
			cmd := task.IncrementFrontmatterCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Field:          "trigger_count",
				Delta:          1,
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			Expect(publishedCmd().TargetVault).To(Equal("personal"))
		})

		It("defaultVault does not override explicit TargetVault", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewIncrementFrontmatterCommandSender(fakeSender, "personal")
			cmd := task.IncrementFrontmatterCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Field:          "trigger_count",
				Delta:          1,
				TargetVault:    "openclaw",
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			Expect(publishedCmd().TargetVault).To(Equal("openclaw"))
		})

		It("both empty → targetVault absent from published payload", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewIncrementFrontmatterCommandSender(fakeSender, "")
			cmd := task.IncrementFrontmatterCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Field:          "trigger_count",
				Delta:          1,
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
			data, err := json.Marshal(cmdObj.Command.Data)
			Expect(err).To(BeNil())
			Expect(string(data)).NotTo(ContainSubstring("targetVault"))
		})
	})
})
