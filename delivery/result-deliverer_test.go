// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/bborbe/collection"
	"github.com/bborbe/cqrs/base"
	cqrsmocks "github.com/bborbe/cqrs/mocks"
	libkafka "github.com/bborbe/kafka"
	kafkamocks "github.com/bborbe/kafka/mocks"
	libtime "github.com/bborbe/time"
	timemocks "github.com/bborbe/time/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/delivery"
	libmocks "github.com/bborbe/agent/mocks"
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
			agentlib.AgentResultInfo{Status: agentlib.AgentStatusDone},
		)
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns nil for failed result", func() {
		deliverer := delivery.NewNoopResultDeliverer()
		err := deliverer.DeliverResult(
			ctx,
			agentlib.AgentResultInfo{Status: agentlib.AgentStatusFailed},
		)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("FileResultDeliverer", func() {
	var (
		ctx       context.Context
		generator *libmocks.AgentContentGenerator
		tmpFile   *os.File
		deliverer agentlib.ResultDeliverer
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
			agentlib.AgentResultInfo{Status: agentlib.AgentStatusDone, Output: "bt-123"},
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
			agentlib.AgentResultInfo{Status: agentlib.AgentStatusDone},
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
		deliverer       agentlib.ResultDeliverer
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
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "done",
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

	It("preserves assignee on failed result below the trigger cap (retry stays routable)", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: planning\nassignee: github-update-go-agent\ntrigger_count: 2\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:  agentlib.AgentStatusFailed,
			Message: "task runner failed: timeout",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("planning"))
		Expect(fm["phase"]).NotTo(Equal("human_review"))
		Expect(fm["status"]).To(Equal("in_progress"))
		Expect(fm["assignee"]).To(Equal("github-update-go-agent"))
		Expect(fm).NotTo(HaveKey("previous_assignee"))
	})

	It(
		"escalates on failed result at the trigger cap (assignee cleared, previous_assignee recorded)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\nassignee: github-update-go-agent\ntrigger_count: 3\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "task runner failed: timeout",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("planning"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
			Expect(fm["previous_assignee"]).To(Equal("github-update-go-agent"))
		},
	)

	It(
		"publishes needs_input result with phase unchanged from incoming and assignee cleared",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
				Message: "no date range in task",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("planning"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
		},
	)

	It(
		"records previous_assignee and clears assignee on needs_input with an incoming owner",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\nassignee: github-update-go-agent\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
				Message: "no date range in task",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("planning"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
			Expect(fm["previous_assignee"]).To(Equal("github-update-go-agent"))
		},
	)

	It(
		"treats done result with empty NextPhase as in-place save (phase preserved, status in_progress)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			// Empty NextPhase means "stay in current phase" (agentlib.Result contract) —
			// never terminal. Regression: preflight steps publishing Done+ContinueToNext
			// with empty NextPhase marked live tasks phase: done / status: completed.
			Expect(fm["phase"]).To(Equal("planning"))
			Expect(fm["phase"]).NotTo(Equal("done"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	It(
		"treats done result with empty NextPhase and ContinueToNext as in-place save (phase preserved, status in_progress)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: execution\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:         agentlib.AgentStatusDone,
				NextPhase:      "",
				ContinueToNext: true,
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("execution"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	It("sets phase=execution when done result requests NextPhase=execution", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: execution\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "execution",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("execution"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})

	It(
		"sets phase=execution when done result requests NextPhase=in_progress (legacy alias normalized)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: execution\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "in_progress",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("execution"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	It("sets phase=planning when done result requests NextPhase=planning", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "planning",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("planning"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})

	It("sets phase=done when done result requests NextPhase=done explicitly", func() {
		generator.GenerateReturns(
			"---\nstatus: completed\nphase: done\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "done",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("done"))
		Expect(fm["status"]).To(Equal("completed"))
	})

	It("sets phase=human_review when done result requests NextPhase=human_review", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "human_review",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("human_review"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})

	It("falls back to phase=done when NextPhase is invalid", func() {
		generator.GenerateReturns(
			"---\nstatus: completed\nphase: done\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "bogus_phase",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("done"))
		Expect(fm["status"]).To(Equal("completed"))
	})

	It(
		"sets phase=human_review when failed result requests NextPhase=in_progress (NextPhase ignored)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Failure\n\n- **Reason:** infra error\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusFailed,
				Message:   "infra error",
				NextPhase: "in_progress",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	It(
		"emits ## Failure body section when failed result has NextPhase set (body shape from 077 unchanged)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n\n## Failure\n\n- **Reason:** crash\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusFailed,
				Message:   "crash",
				NextPhase: "done",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("human_review"))
		},
	)

	It("sets phase=ai_review when done result requests NextPhase=ai_review", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: ai_review\n---\n\nBody.\n",
			nil,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status:    agentlib.AgentStatusDone,
			NextPhase: "ai_review",
		})
		Expect(err).NotTo(HaveOccurred())
		_, cmdObj := sender.SendCommandObjectArgsForCall(0)
		frontmatter, ok := cmdObj.Command.Data["frontmatter"]
		Expect(ok).To(BeTrue())
		fm, ok := frontmatter.(map[string]interface{})
		Expect(ok).To(BeTrue())
		Expect(fm["phase"]).To(Equal("ai_review"))
		Expect(fm["status"]).To(Equal("in_progress"))
	})

	It(
		"keeps status=in_progress when done result requests NextPhase=in_progress (legacy alias normalized to execution)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: execution\n---\n\nBody.\n\n## Plan\n\n[plan content]\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				Message:   "plan extracted",
				NextPhase: "in_progress",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("execution"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	Context("with AgentStatusInProgress and incoming phase: planning", func() {
		BeforeEach(func() {
			originalContent = "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\nBody.\n"
		})

		It("publishes in_progress result preserving phase from incoming task", func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n\n## Plan\n\n- Step 1\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusInProgress,
				Output: "## Plan\n\n- Step 1\n",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["phase"]).To(Equal("planning"))
		})

		It(
			"publishes in_progress result ignoring NextPhase (phase preserved from incoming task)",
			func() {
				generator.GenerateReturns(
					"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n\n## Plan\n\n- Step 1\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:    agentlib.AgentStatusInProgress,
					Output:    "## Plan\n\n- Step 1\n",
					NextPhase: "ai_review",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				frontmatter, ok := cmdObj.Command.Data["frontmatter"]
				Expect(ok).To(BeTrue())
				fm, ok := frontmatter.(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(fm["status"]).To(Equal("in_progress"))
				// NextPhase=ai_review must be ignored — phase stays as planning from incoming task
				Expect(fm["phase"]).To(Equal("planning"))
				Expect(fm["phase"]).NotTo(Equal("ai_review"))
			},
		)
	})

	It(
		"preserves phase from incoming content and clears assignee when needs_input result requests NextPhase=done (NextPhase ignored)",
		func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusNeedsInput,
				Message:   "missing date range",
				NextPhase: "done",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("planning"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
		},
	)

	Context("AgentStatusNeedsInput with incoming phase: in_progress", func() {
		It("preserves phase: in_progress and clears assignee", func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: in_progress\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
				Message: "needs info",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("in_progress"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
		})
	})

	Context("AgentStatusNeedsInput with incoming phase: ai_review", func() {
		It("preserves phase: ai_review and clears assignee", func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: ai_review\n---\n\nBody.\n\n## Result\n\nneeds more info\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
				Message: "needs info",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("ai_review"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal(""))
		})
	})

	Context("AgentStatusFailed with incoming phase: in_progress", func() {
		It("preserves phase: in_progress and assignee below the trigger cap", func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: in_progress\nassignee: github-update-go-agent\ntrigger_count: 1\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "task runner failed: timeout",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("in_progress"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal("github-update-go-agent"))
			Expect(fm).NotTo(HaveKey("previous_assignee"))
		})
	})

	Context("AgentStatusFailed with incoming phase: ai_review", func() {
		It("preserves phase: ai_review and assignee below the trigger cap", func() {
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: ai_review\nassignee: github-update-go-agent\ntrigger_count: 1\n---\n\nBody.\n\n## Failure\n\n- **Reason:** task runner failed: timeout\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "task runner failed: timeout",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["phase"]).To(Equal("ai_review"))
			Expect(fm["phase"]).NotTo(Equal("human_review"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["assignee"]).To(Equal("github-update-go-agent"))
			Expect(fm).NotTo(HaveKey("previous_assignee"))
		})
	})

	Context("target_vault echo from originalContent (spec 052)", func() {
		BeforeEach(func() {
			originalContent = "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: personal\n---\n\nBody.\n"
		})

		It("AC1: stamps target_vault on a stub result (empty Output, failed status)", func() {
			// Stub result: the generator produces status-only frontmatter (the
			// observed frontmatter keys=1 case from the spec) and the result
			// has empty Output.
			generator.GenerateReturns(
				"---\nstatus: in_progress\n---\n\nBody.\n",
				nil,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Output:  "",
				Message: "claude step failed",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["target_vault"]).To(Equal("personal"))
		})

		Context("AC2: full result already carries target_vault in the generated content", func() {
			BeforeEach(func() {
				// Stale originalContent value must never clobber the generated
				// value — this is what makes the "no overwrite" assertion
				// meaningful (Failure Modes row 2).
				originalContent = "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: openclaw\n---\n\nBody.\n"
			})

			It("preserves the existing value exactly once (no overwrite, no duplicate)", func() {
				// Full echo: the generated content itself carries target_vault.
				generator.GenerateReturns(
					"---\nstatus: completed\nphase: done\ntarget_vault: personal\n---\n\nBody.\n\n## Result\n\nok\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:    agentlib.AgentStatusDone,
					NextPhase: "done",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				// The generated value (personal) wins; the stale openclaw from
				// originalContent is never stamped. A Go map cannot hold the key
				// twice, so "personal" present with the stale value absent proves
				// no overwrite and no duplicate.
				Expect(fm["target_vault"]).To(Equal("personal"))
			})
		})

		Context("AC3: originalContent without target_vault", func() {
			BeforeEach(func() {
				originalContent = "---\ntitle: Legacy task\nstatus: in_progress\n---\n\nBody.\n"
			})

			It("adds no target_vault key", func() {
				generator.GenerateReturns(
					"---\nstatus: in_progress\n---\n\nBody.\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status: agentlib.AgentStatusFailed,
					Output: "",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(fm).NotTo(HaveKey("target_vault"))
			})
		})

		Context("AC3: originalContent without frontmatter", func() {
			BeforeEach(func() {
				originalContent = "Just body text with no frontmatter delimiters.\n"
			})

			It("adds no target_vault key", func() {
				generator.GenerateReturns(
					"---\nstatus: in_progress\n---\n\nBody.\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status: agentlib.AgentStatusFailed,
					Output: "",
				})
				Expect(err).NotTo(HaveOccurred())
				_, cmdObj := sender.SendCommandObjectArgsForCall(0)
				fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(fm).NotTo(HaveKey("target_vault"))
			})
		})
	})

	Context("real passthrough generator end-to-end (spec 052 reproduction)", func() {
		It("publishes target_vault on a failed stub with empty Output", func() {
			originalContent := "---\ntitle: Analyze Sentry issue NUKE-DEV-A4 - 2026-09-05\nstatus: in_progress\ntarget_vault: personal\n---\n\nAnalyze the sentry issue.\n"
			passthrough := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				originalContent,
				delivery.NewPassthroughContentGenerator(),
				clock,
			)
			err := passthrough.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Output:  "",
				Message: "claude step failed",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			// The real passthrough generator drops target_vault (it ignores
			// originalContent); the deliverer's stamp must restore it.
			Expect(fm["target_vault"]).To(Equal("personal"))
		})
	})

	Context("metrics frontmatter (spec 053)", func() {
		publishedFrontmatter := func() map[string]interface{} {
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			return fm
		}

		fullResultContent := "---\nstatus: completed\nphase: done\n---\n\nBody.\n\n## Result\n\nok\n"

		Context("AC1: every payload path carries the turn count", func() {
			paths := []struct {
				name      string
				status    agentlib.AgentStatus
				nextPhase string
			}{
				{"done with a next phase", agentlib.AgentStatusDone, "done"},
				{"done without a next phase (in-place save)", agentlib.AgentStatusDone, ""},
				{"in_progress", agentlib.AgentStatusInProgress, ""},
				{"needs_input", agentlib.AgentStatusNeedsInput, ""},
				{"failed", agentlib.AgentStatusFailed, ""},
			}

			for _, path := range paths {
				It("publishes metrics_agent_turns on "+path.name, func() {
					generator.GenerateReturns(fullResultContent, nil)
					err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
						Status:           path.status,
						NextPhase:        path.nextPhase,
						AgentTurns:       collection.Ptr(int64(7)),
						InteractionCount: collection.Ptr(int64(0)),
					})
					Expect(err).NotTo(HaveOccurred())
					Expect(publishedFrontmatter()["metrics_agent_turns"]).
						To(BeNumerically("==", 7))
				})
			}
		})

		It("AC3: omits the turn entry when no count was reported", func() {
			generator.GenerateReturns(fullResultContent, nil)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:     agentlib.AgentStatusDone,
				NextPhase:  "done",
				AgentTurns: nil,
			})
			Expect(err).NotTo(HaveOccurred())
			_, ok := publishedFrontmatter()["metrics_agent_turns"]
			Expect(ok).To(BeFalse())
		})

		Context("AC4: the interaction count is evidenced, not assumed", func() {
			It("publishes an observed zero", func() {
				generator.GenerateReturns(fullResultContent, nil)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:           agentlib.AgentStatusDone,
					NextPhase:        "done",
					InteractionCount: collection.Ptr(int64(0)),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(publishedFrontmatter()["metrics_interaction_count"]).
					To(BeNumerically("==", 0))
			})

			It("publishes an observed two", func() {
				generator.GenerateReturns(fullResultContent, nil)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:           agentlib.AgentStatusDone,
					NextPhase:        "done",
					InteractionCount: collection.Ptr(int64(2)),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(publishedFrontmatter()["metrics_interaction_count"]).
					To(BeNumerically("==", 2))
			})
		})

		It("AC5: omits the interaction entry when the evidence was unavailable", func() {
			generator.GenerateReturns(fullResultContent, nil)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:           agentlib.AgentStatusDone,
				NextPhase:        "done",
				AgentTurns:       collection.Ptr(int64(7)),
				InteractionCount: nil,
			})
			Expect(err).NotTo(HaveOccurred())
			fm := publishedFrontmatter()
			_, ok := fm["metrics_interaction_count"]
			Expect(ok).To(BeFalse())
			Expect(fm["metrics_agent_turns"]).To(BeNumerically("==", 7))
		})

		Context("AC6: a recorded count is never lowered", func() {
			BeforeEach(func() {
				originalContent = "---\ntitle: My Task\nstatus: in_progress\nmetrics_interaction_count: 101\n---\n\nBody.\n"
			})

			It("AC6a: keeps the recorded 101 when the run observed 0", func() {
				generator.GenerateReturns(
					"---\nstatus: completed\nphase: done\nmetrics_interaction_count: 101\n---\n\nBody.\n\n## Result\n\nok\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:           agentlib.AgentStatusDone,
					NextPhase:        "done",
					InteractionCount: collection.Ptr(int64(0)),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(publishedFrontmatter()["metrics_interaction_count"]).
					To(BeNumerically("==", 101))
				Expect(publishedFrontmatter()["metrics_interaction_count"]).
					NotTo(BeNumerically("==", 0))
			})

			It("AC6b: publishes a larger observation of 103", func() {
				generator.GenerateReturns(
					"---\nstatus: completed\nphase: done\nmetrics_interaction_count: 101\n---\n\nBody.\n\n## Result\n\nok\n",
					nil,
				)
				err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:           agentlib.AgentStatusDone,
					NextPhase:        "done",
					InteractionCount: collection.Ptr(int64(103)),
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(publishedFrontmatter()["metrics_interaction_count"]).
					To(BeNumerically("==", 103))
			})
		})

		It("AC7: carries both values as numbers, never as strings", func() {
			generator.GenerateReturns(fullResultContent, nil)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:           agentlib.AgentStatusDone,
				NextPhase:        "done",
				AgentTurns:       collection.Ptr(int64(7)),
				InteractionCount: collection.Ptr(int64(2)),
			})
			Expect(err).NotTo(HaveOccurred())
			fm := publishedFrontmatter()
			Expect(fm["metrics_agent_turns"]).To(BeNumerically("==", 7))
			Expect(fm["metrics_agent_turns"]).NotTo(BeAssignableToTypeOf(""))
			Expect(fm["metrics_interaction_count"]).To(BeNumerically("==", 2))
			Expect(fm["metrics_interaction_count"]).NotTo(BeAssignableToTypeOf(""))
		})

		It("AC8: both entries are additive to the published key set", func() {
			generator.GenerateReturns(fullResultContent, nil)

			baselineSender := &cqrsmocks.CDBCommandObjectSender{}
			baselineSender.SendCommandObjectReturns(nil)
			baselineDeliverer := delivery.NewKafkaResultDelivererWithSender(
				baselineSender, taskID, originalContent, generator, clock,
			)
			err := baselineDeliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "done",
			})
			Expect(err).NotTo(HaveOccurred())

			err = deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:           agentlib.AgentStatusDone,
				NextPhase:        "done",
				AgentTurns:       collection.Ptr(int64(7)),
				InteractionCount: collection.Ptr(int64(2)),
			})
			Expect(err).NotTo(HaveOccurred())

			_, baselineCmdObj := baselineSender.SendCommandObjectArgsForCall(0)
			baselineFM, ok := baselineCmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			baselineKeys := map[string]struct{}{}
			for k := range baselineFM {
				baselineKeys[k] = struct{}{}
			}
			withMetricsKeys := map[string]struct{}{}
			for k := range publishedFrontmatter() {
				withMetricsKeys[k] = struct{}{}
			}

			diff := map[string]struct{}{}
			for k := range baselineKeys {
				if _, ok := withMetricsKeys[k]; !ok {
					diff[k] = struct{}{}
				}
			}
			for k := range withMetricsKeys {
				if _, ok := baselineKeys[k]; !ok {
					diff[k] = struct{}{}
				}
			}
			diffKeys := make([]string, 0, len(diff))
			for k := range diff {
				diffKeys = append(diffKeys, k)
			}
			sort.Strings(diffKeys)
			Expect(diffKeys).To(
				Equal([]string{"metrics_agent_turns", "metrics_interaction_count"}),
			)
		})
	})
})

var _ = Describe("NewKafkaResultDeliverer (topic prefix wiring)", func() {
	// This exercises the REAL delivery.NewKafkaResultDeliverer constructor (not
	// NewKafkaResultDelivererWithSender) with a real cdb.NewCommandObjectSender fed a
	// fake libkafka.SyncProducer, so a fat-fingered topicPrefix (e.g. always "") would
	// be caught: the sender publishes a command, so the topic uses the CommandTopic
	// ("request") suffix. Golden topic strings are frozen in agent_cdb-schema_test.go.
	var (
		ctx             context.Context
		syncProducer    *kafkamocks.KafkaSyncProducer
		clock           *timemocks.CurrentDateTimeGetter
		generator       *libmocks.AgentContentGenerator
		taskID          agentlib.TaskIdentifier
		originalContent string
	)

	BeforeEach(func() {
		ctx = context.Background()
		syncProducer = &kafkamocks.KafkaSyncProducer{}
		syncProducer.SendMessageReturns(int32(0), int64(123), nil)
		clock = &timemocks.CurrentDateTimeGetter{}
		clock.NowReturns(libtime.DateTime(time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)))
		generator = &libmocks.AgentContentGenerator{}
		generator.GenerateReturns(
			"---\nstatus: completed\nphase: done\n---\n\nBody.\n\n## Result\n\nok\n",
			nil,
		)
		taskID = agentlib.TaskIdentifier("task-abc-123")
		originalContent = "---\ntitle: My Task\nstatus: in_progress\n---\n\nBody.\n"
	})

	It(
		"publishes to the develop-prefixed request topic when topicPrefix is derived from branch dev",
		func() {
			deliverer := delivery.NewKafkaResultDeliverer(
				syncProducer,
				base.TopicPrefixFromBranch(base.Branch("dev")),
				taskID,
				originalContent,
				generator,
				clock,
			)
			err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(syncProducer.SendMessageCallCount()).To(Equal(1))
			_, msg := syncProducer.SendMessageArgsForCall(0)
			Expect(msg.Topic).To(Equal(libkafka.Topic("develop-agent-task-v1-request").String()))
		},
	)

	It("publishes to the unprefixed request topic when topicPrefix is empty", func() {
		deliverer := delivery.NewKafkaResultDeliverer(
			syncProducer,
			base.TopicPrefix(""),
			taskID,
			originalContent,
			generator,
			clock,
		)
		err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusDone,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(syncProducer.SendMessageCallCount()).To(Equal(1))
		_, msg := syncProducer.SendMessageArgsForCall(0)
		Expect(msg.Topic).To(Equal(libkafka.Topic("agent-task-v1-request").String()))
	})
})
