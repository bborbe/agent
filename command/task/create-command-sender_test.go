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

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/command/task"
)

var _ = Describe("CreateCommandSender", func() {
	var (
		ctx        context.Context
		fakeSender *cqrsmocks.CDBCommandObjectSender
		sender     task.CreateCommandSender
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeSender = &cqrsmocks.CDBCommandObjectSender{}
		sender = task.NewCreateCommandSender(fakeSender, "")
	})

	It("validation fails → publisher not called", func() {
		cmd := task.CreateCommand{
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
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(Succeed())
		Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
		Expect(cmdObj.Command.Operation).To(Equal(task.CreateCommandOperation))
		Expect(cmdObj.SchemaID).To(Equal(lib.TaskV1SchemaID))
	})

	It("publisher returns error → error propagated", func() {
		fakeSender.SendCommandObjectReturns(stderrors.New("kafka down"))
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := sender.SendCommand(ctx, cmd)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kafka down"))
	})

	Context("defaultVault substitution", func() {
		// publishedCmd decodes the embedded CreateCommand from the most recent
		// published CommandObject. It captures ctx and fakeSender from the
		// outer BeforeEach and reads the most recent call.
		publishedCmd := func() task.CreateCommand {
			_, cmdObj := fakeSender.SendCommandObjectArgsForCall(0)
			var got task.CreateCommand
			Expect(cmdObj.Command.Data.MarshalInto(ctx, &got)).To(Succeed())
			return got
		}

		It("defaultVault '' preserves input TargetVault (AC 5)", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewCreateCommandSender(fakeSender, "")
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "My Task",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
				TargetVault:    "openclaw",
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			Expect(publishedCmd().TargetVault).To(Equal("openclaw"))
		})

		It("defaultVault 'personal' fills empty input (AC 6)", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewCreateCommandSender(fakeSender, "personal")
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "My Task",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			Expect(publishedCmd().TargetVault).To(Equal("personal"))
		})

		It("defaultVault does not override explicit input (AC 7)", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewCreateCommandSender(fakeSender, "personal")
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "My Task",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
				TargetVault:    "openclaw",
			}
			Expect(localSender.SendCommand(ctx, cmd)).To(Succeed())
			Expect(publishedCmd().TargetVault).To(Equal("openclaw"))
		})

		It("invalid defaultVault surfaces at first SendCommand (AC 8)", func() {
			fakeSender.SendCommandObjectReturns(nil)
			localSender := task.NewCreateCommandSender(fakeSender, "Bad Vault")
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "My Task",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			}
			err := localSender.SendCommand(ctx, cmd)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validate CreateCommand"))
			Expect(err.Error()).To(ContainSubstring("TargetVault"))
			// Publisher must not be called.
			Expect(fakeSender.SendCommandObjectCallCount()).To(Equal(0))
		})
	})
})
