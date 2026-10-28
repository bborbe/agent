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
	"github.com/bborbe/vault-cli/pkg/domain"
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

	Context("phase advanced in-process within one Job (passthrough generator)", func() {
		It("keeps the advanced phase on a later in-place save", func() {
			jobStartContent := "---\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\n---\n\nBody.\n"
			passthrough := delivery.NewFileResultDeliverer(
				delivery.NewPassthroughContentGenerator(),
				tmpFile.Name(),
			)

			Expect(passthrough.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				Output:    jobStartContent,
				NextPhase: "execution",
			})).To(Succeed())
			Expect(passthrough.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				Output:    jobStartContent,
				NextPhase: "",
			})).To(Succeed())

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["phase"]).To(Equal("execution"))
		})
	})

	Context("phase preservation on a result that advances nothing", func() {
		writeTask := func(content string) {
			Expect(os.WriteFile(tmpFile.Name(), []byte(content), 0600)).To(Succeed())
		}

		It("keeps phase: human_review on disk when a done result has empty NextPhase", func() {
			// The generator rebuilds from the Job-start snapshot and would write
			// planning; the deliverer must carry the on-disk park forward instead.
			writeTask("---\nstatus: in_progress\nphase: human_review\n---\n\nBody.\n")
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
				nil,
			)
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})).To(Succeed())

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["phase"]).To(Equal("human_review"))
		})

		It("keeps phase: done on disk on the same delivery", func() {
			writeTask("---\nstatus: in_progress\nphase: done\n---\n\nBody.\n")
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n",
				nil,
			)
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})).To(Succeed())

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["phase"]).To(Equal("done"))
		})

		It("still writes the requested phase when a done result advances to execution", func() {
			writeTask("---\nstatus: in_progress\nphase: planning\n---\n\nBody.\n")
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: execution\n---\n\nBody.\n",
				nil,
			)
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["phase"]).To(Equal("execution"))
		})
	})

	Context("failure bound across consecutive Jobs (fallback generator)", func() {
		// The deliverer reads the file, so each DeliverResult call sees the previous
		// run's published retry_count — exactly what a fresh Job would see on its own
		// start-of-run snapshot. Both attempts below run under the new bound.
		var fileDeliverer agentlib.ResultDeliverer

		BeforeEach(func() {
			fileDeliverer = delivery.NewFileResultDeliverer(
				delivery.NewFallbackContentGenerator(),
				tmpFile.Name(),
			)
			Expect(os.WriteFile(
				tmpFile.Name(),
				[]byte(
					"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\n---\n\nBody.\n",
				),
				0600,
			)).To(Succeed())
		})

		It("retries once under the bound, then succeeds and clears the streak", func() {
			Expect(fileDeliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "git clone: Failed to connect to github.com:443 after 2 ms",
			})).To(Succeed())

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["retry_count"]).To(Equal(1))
			Expect(fm["assignee"]).To(Equal("github-releaser-agent"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["phase"]).To(Equal("planning"))

			Expect(fileDeliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())

			written, err = os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ = delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["retry_count"]).To(Equal(0))
			Expect(fm["phase"]).To(Equal("execution"))
			Expect(fm["status"]).To(Equal("in_progress"))
		})

		It("escalates at max_retries: stops re-dispatch without going terminal", func() {
			for i := 0; i < 3; i++ {
				Expect(fileDeliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
					Status:  agentlib.AgentStatusFailed,
					Message: "task runner failed: OOMKilled",
				})).To(Succeed())
			}

			written, err := os.ReadFile(tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			fm, _ := delivery.ParseMarkdownFrontmatter(string(written))
			Expect(fm["retry_count"]).To(Equal(3))
			// Out of the claim predicate — no further dispatch for this task id.
			// Asserted through the accessor the predicate itself uses: an empty YAML
			// scalar parses back as nil, which Assignee() reads as "".
			Expect(agentlib.TaskFrontmatter(fm).Assignee()).To(BeEmpty())
			Expect(fm["previous_assignee"]).To(Equal("github-releaser-agent"))
			// ...while the record stays non-terminal and keeps its resume cursor.
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["phase"]).To(Equal("planning"))
		})
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
		"bounds the retry: escalates at max_retries and leaves the record non-terminal",
		func() {
			// Third consecutive failure: the Job started at retry_count 2, so this
			// result is attempt 3, which reaches the default max_retries of 3.
			// No trigger_count here — the bound must hold on its own, because the
			// executor's trigger cap only applies to tasks that opted in with
			// max_triggers or carry a ref.
			//
			// The generator's output already carries the incremented value, exactly
			// as the real fallback/passthrough/section generators leave it after
			// applyStatusFrontmatter. The deliverer must derive the streak from the
			// Job's starting content, so the count lands on 3 and is not doubled
			// into 4 by counting the same failed Job twice.
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 3\n---\n\nBody.\n",
				nil,
			)
			bounded := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 2\n---\n\nBody.\n",
				generator,
				clock,
			)
			err := bounded.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "git clone: Failed to connect to github.com:443 after 2 ms",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			// The command payload round-trips through JSON, so numbers arrive as
			// float64 here — the value, not the Go type, is what matters.
			Expect(fm["retry_count"]).To(BeNumerically("==", 3))
			// Escalated out of the claim predicate: empty assignee is the
			// operator-inbox signal, so the controller no longer selects it.
			Expect(fm["assignee"]).To(Equal(""))
			Expect(fm["previous_assignee"]).To(Equal("github-releaser-agent"))
			// ...while the record stays non-terminal and keeps its resume cursor.
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["phase"]).To(Equal("planning"))
		},
	)

	It(
		"records the retry count below the bound and keeps the record routable",
		func() {
			// Started at 1, so this failure is attempt 2 — below the default
			// max_retries of 3, which keeps the assignee in place and the record
			// inside the claim predicate.
			generator.GenerateReturns(
				"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 2\n---\n\nBody.\n",
				nil,
			)
			bounded := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 1\n---\n\nBody.\n",
				generator,
				clock,
			)
			err := bounded.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "task runner failed: timeout",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			frontmatter, ok := cmdObj.Command.Data["frontmatter"]
			Expect(ok).To(BeTrue())
			fm, ok := frontmatter.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm["retry_count"]).To(BeNumerically("==", 2))
			Expect(fm["assignee"]).To(Equal("github-releaser-agent"))
			Expect(fm).NotTo(HaveKey("previous_assignee"))
			Expect(fm["status"]).To(Equal("in_progress"))
			Expect(fm["phase"]).To(Equal("planning"))
		},
	)

	It("clears the failure streak on a forward-progress result", func() {
		generator.GenerateReturns(
			"---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 2\n---\n\nBody.\n",
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
		Expect(fm["retry_count"]).To(BeNumerically("==", 0))
		Expect(fm["phase"]).To(Equal("execution"))
	})

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
			// No phase published: the controller's merge keeps the on-disk phase,
			// so a park written while the run was in flight survives.
			Expect(fm).NotTo(HaveKey("phase"))
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
			Expect(fm).NotTo(HaveKey("phase"))
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
			// No phase is published: the controller's merge preserves the on-disk one.
			Expect(fm).NotTo(HaveKey("phase"))
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
			Expect(fm).NotTo(HaveKey("phase"))
			Expect(fm["status"]).To(Equal("in_progress"))
		},
	)

	It(
		"publishes no phase for a done result with empty NextPhase when the task started at phase: planning",
		func() {
			// The Job-start snapshot carries planning; a park may have moved the task
			// to human_review while the run was still going. Omitting the key lets the
			// controller's merge keep whatever the task holds now.
			originalContent = "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\nBody.\n"
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
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(fm).NotTo(HaveKey("phase"))
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
			// No phase published: the controller's merge preserves the on-disk value.
			Expect(fm).NotTo(HaveKey("phase"))
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
				// NextPhase=ai_review must be ignored — no phase is published, so the
				// controller's merge keeps the on-disk phase.
				Expect(fm).NotTo(HaveKey("phase"))
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
			Expect(fm).NotTo(HaveKey("phase"))
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
			Expect(fm).NotTo(HaveKey("phase"))
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
			Expect(fm).NotTo(HaveKey("phase"))
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

	Context("failure bound with a generator that already applies it (fallback generator)", func() {
		// The fallback, passthrough and section generators all run
		// applyStatusFrontmatter, which already records the increment. The Kafka
		// deliverer then applies the bound a second time on the generated
		// frontmatter, so one failed Job must still count as ONE failure: the
		// deliverer derives the count from the Job's own starting content, not from
		// whatever the generator left behind.
		It("counts one failed Job once, not twice", func() {
			originalContent := "---\nstatus: in_progress\nphase: planning\nassignee: github-releaser-agent\nretry_count: 0\n---\n\nBody.\n"
			fallback := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				originalContent,
				delivery.NewFallbackContentGenerator(),
				clock,
			)
			err := fallback.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "git clone failed",
			})
			Expect(err).NotTo(HaveOccurred())
			_, cmdObj := sender.SendCommandObjectArgsForCall(0)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			// Numbers round-trip through JSON as float64; the value is what matters.
			Expect(fm["retry_count"]).To(BeNumerically("==", 1))
			Expect(fm["assignee"]).To(Equal("github-releaser-agent"))
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

	Context("phase advanced in-process within one Job", func() {
		// A multi-phase Agent walks planning -> execution inside one Job with a
		// single deliverer. Every save after the first is generated from the
		// Job-start content, so it carries phase: planning unless the deliverer
		// remembers the advance.
		const advancedContent = "---\nstatus: in_progress\nphase: execution\nassignee: hypothesis-agent\n---\n\nBody.\n"
		const jobStartContent = "---\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\n---\n\nBody.\n"

		publishedFrontmatterAt := func(i int) map[string]interface{} {
			_, cmdObj := sender.SendCommandObjectArgsForCall(i)
			fm, ok := cmdObj.Command.Data["frontmatter"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			return fm
		}

		It("5a: publishes the advanced phase on a later Done without NextPhase", func() {
			generator.GenerateReturnsOnCall(0, advancedContent, nil)
			generator.GenerateReturnsOnCall(1, jobStartContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			Expect(publishedFrontmatterAt(0)["phase"]).To(Equal("execution"))
			Expect(publishedFrontmatterAt(1)).NotTo(HaveKey("phase"))
			Expect(publishedFrontmatterAt(1)["status"]).To(Equal("in_progress"))
		})

		It("5b: publishes the advanced phase on a later in_progress save", func() {
			generator.GenerateReturnsOnCall(0, advancedContent, nil)
			generator.GenerateReturnsOnCall(1, jobStartContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusInProgress,
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			Expect(publishedFrontmatterAt(1)).NotTo(HaveKey("phase"))
			Expect(publishedFrontmatterAt(1)["status"]).To(Equal("in_progress"))
			Expect(publishedFrontmatterAt(1)["assignee"]).To(Equal("hypothesis-agent"))
		})

		It("5c: publishes the advanced phase on a later needs_input escalation", func() {
			generator.GenerateReturnsOnCall(0, advancedContent, nil)
			generator.GenerateReturnsOnCall(1, jobStartContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
				Message: "need input",
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			Expect(publishedFrontmatterAt(1)).NotTo(HaveKey("phase"))
			Expect(publishedFrontmatterAt(1)["status"]).To(Equal("in_progress"))
			Expect(publishedFrontmatterAt(1)["assignee"]).To(Equal(""))
			Expect(publishedFrontmatterAt(1)["previous_assignee"]).To(Equal("hypothesis-agent"))
		})

		It("5d: keeps the retry routable on the advanced phase after a failure", func() {
			failedContent := "---\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\ntrigger_count: 1\n---\n\nBody.\n"
			generator.GenerateReturnsOnCall(0, advancedContent, nil)
			generator.GenerateReturnsOnCall(1, failedContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "execution",
			})).To(Succeed())
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "timeout",
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			// The failed branch still publishes its phase (the controller's
			// planning-retry gate reads it from the payload), so the generated
			// Job-start phase survives rather than an in-process advance.
			Expect(publishedFrontmatterAt(1)["phase"]).To(Equal("planning"))
			Expect(publishedFrontmatterAt(1)["assignee"]).To(Equal("hypothesis-agent"))
		})

		It("5e: keeps the incoming phase when nothing advanced", func() {
			generator.GenerateReturns(jobStartContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(1))
			Expect(publishedFrontmatterAt(0)).NotTo(HaveKey("phase"))
		})

		It("5f: never remembers the terminal done phase", func() {
			generator.GenerateReturnsOnCall(
				0,
				"---\nstatus: completed\nphase: done\n---\n\nBody.\n",
				nil,
			)
			generator.GenerateReturnsOnCall(1, jobStartContent, nil)

			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "done",
			})).To(Succeed())
			Expect(deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "",
			})).To(Succeed())

			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			Expect(publishedFrontmatterAt(1)).NotTo(HaveKey("phase"))
		})

		It("5g: runs a real two-phase Agent through the real deliverer end-to-end", func() {
			jobContent := "---\ntitle: Hypothesis\nstatus: in_progress\nphase: planning\nassignee: hypothesis-agent\n---\n\nBody.\n"
			planStep := &libmocks.AgentStep{}
			planStep.NameReturns("plan")
			planStep.ShouldRunReturns(true, nil)
			planStep.RunReturns(
				&agentlib.Result{
					Status:    agentlib.AgentStatusDone,
					NextPhase: "execution",
				},
				nil,
			)
			execStep := &libmocks.AgentStep{}
			execStep.NameReturns("exec")
			execStep.ShouldRunReturns(true, nil)
			execStep.RunReturns(
				&agentlib.Result{Status: agentlib.AgentStatusInProgress},
				nil,
			)
			agent := agentlib.NewAgent(
				agentlib.NewPhase(domain.TaskPhasePlanning, planStep),
				agentlib.NewPhase(domain.TaskPhaseExecution, execStep),
			)
			realDeliverer := delivery.NewKafkaResultDelivererWithSender(
				sender,
				taskID,
				jobContent,
				delivery.NewPassthroughContentGenerator(),
				clock,
			)

			_, err := agent.Run(ctx, domain.TaskPhasePlanning, jobContent, realDeliverer)
			Expect(err).NotTo(HaveOccurred())

			Expect(planStep.RunCallCount()).To(Equal(1))
			Expect(execStep.RunCallCount()).To(Equal(1))
			Expect(sender.SendCommandObjectCallCount()).To(Equal(2))
			Expect(publishedFrontmatterAt(0)["phase"]).To(Equal("execution"))
			Expect(publishedFrontmatterAt(1)).NotTo(HaveKey("phase"))
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
