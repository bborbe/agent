// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"os"
	"time"

	cqrsmocks "github.com/bborbe/cqrs/mocks"
	libtime "github.com/bborbe/time"
	timemocks "github.com/bborbe/time/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/delivery"
	libmocks "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("NoopResultDeliverer", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns nil for done result", func() {
		deliverer := delivery.NewNoopResultDeliverer()
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone},
		)
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns nil for failed result", func() {
		deliverer := delivery.NewNoopResultDeliverer()
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusFailed},
		)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("FileResultDeliverer", func() {
	var (
		ctx       context.Context
		generator *libmocks.AgentContentGenerator
		tmpFile   *os.File
		deliverer delivery.ResultDeliverer
	)

	BeforeEach(func() {
		ctx = context.Background()
		generator = &libmocks.AgentContentGenerator{}
		var err error
		tmpFile, err = os.CreateTemp("", "task-*.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(
			os.WriteFile(tmpFile.Name(), []byte("---\ntitle: Test\n---\n\nBody.\n"), 0600),
		).To(Succeed())
		deliverer = delivery.NewFileResultDeliverer(generator, tmpFile.Name())
	})

	AfterEach(func() {
		Expect(os.Remove(tmpFile.Name())).To(Succeed())
	})

	It("calls generator with file content and writes generated result to disk", func() {
		generated := "---\ntitle: Test\nstatus: completed\n---\n\nBody.\n\n## Result\n\nbt-123\n"
		generator.GenerateReturns(generated, nil)
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone, Output: "bt-123"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(generator.GenerateCallCount()).To(Equal(1))
		written, err := os.ReadFile(tmpFile.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(string(written)).To(Equal(generated))
	})

	It("returns error when file does not exist", func() {
		deliverer = delivery.NewFileResultDeliverer(generator, "/nonexistent/path/task.md")
		err := deliverer.DeliverResult(
			ctx,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone},
		)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("KafkaResultDeliverer", func() {
	var (
		ctx             context.Context
		sender          *cqrsmocks.CDBCommandObjectSender
		clock           *timemocks.CurrentDateTimeGetter
		generator       *libmocks.AgentContentGenerator
		deliverer       delivery.ResultDeliverer
		taskID          agentlib.TaskIdentifier
		originalContent string
	)

	BeforeEach(func() {
		ctx = context.Background()
		sender = &cqrsmocks.CDBCommandObjectSender{}
		sender.SendCommandObjectReturns(nil)
		clock = &timemocks.CurrentDateTimeGetter{}
		clock.NowReturns(libtime.DateTime(time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)))
		generator = &libmocks.AgentContentGenerator{}
		taskID = agentlib.TaskIdentifier("task-abc-123")
		originalContent = "---\ntitle: My Task\nstatus: in_progress\n---\n\nBody.\n"
	})

	JustBeforeEach(func() {
		deliverer = delivery.NewKafkaResultDelivererWithSender(
			sender,
			taskID,
			originalContent,
			generator,
			clock,
		)
	})

	It("publishes done result with phase=done", func() {
		generator.GenerateReturns(
			"---\nstatus: completed\nphase: done\n---\n\nBody.\n\n## Result\n\nok\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, delivery.AgentResultInfo{
			Status: delivery.AgentStatusDone,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("done"))
		Expect(fm["status"]).To(Equal("completed"))
	})

	It("publishes failed result with phase=human_review", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, delivery.AgentResultInfo{
			Status:  delivery.AgentStatusFailed,
			Message: "task runner failed: timeout",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("human_review"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})

	It("publishes needs_input result with phase=human_review", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, delivery.AgentResultInfo{
			Status:  delivery.AgentStatusNeedsInput,
			Message: "no date range in task",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("human_review"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})
})
