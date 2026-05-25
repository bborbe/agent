// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/code/pkg/steps"
	agentlib "github.com/bborbe/agent/lib"
)

var _ = Describe("PlanStep", func() {
	var (
		ctx  context.Context
		step *steps.PlanStep
	)

	BeforeEach(func() {
		ctx = context.Background()
		step = steps.NewPlanStep()
	})

	Describe("Name", func() {
		It("returns 'plan'", func() {
			Expect(step.Name()).To(Equal("plan"))
		})
	})

	Describe("ShouldRun", func() {
		It("returns true when no ## Plan section exists", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: add\na: 2\nb: 3\n---\n")
			Expect(err).To(BeNil())
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})

		It("returns false when ## Plan section already exists", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"add\",\n  \"a\": 2,\n  \"b\": 3\n}\n```\n",
			)
			Expect(err).To(BeNil())
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Run", func() {
		It("writes ## Plan section and returns AgentStatusDone + NextPhase in_progress", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: add\na: 2\nb: 3\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("execution"))

			section, ok := md.FindSection("## Plan")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"operation": "add"`))
			Expect(section.Body).To(ContainSubstring(`"a": 2`))
			Expect(section.Body).To(ContainSubstring(`"b": 3`))
		})

		It("returns needsInput when operation is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\na: 2\nb: 3\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("operation"))
		})

		It("returns needsInput when operation is empty", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: \"\"\na: 2\nb: 3\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("operation"))
		})

		It("returns needsInput when 'a' is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: add\nb: 3\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("a"))
		})

		It("returns needsInput when 'b' is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: add\na: 2\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("a"))
			Expect(result.Message).To(ContainSubstring("b"))
		})

		It("returns needsInput when 'a' is not an integer", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: notanumber\nb: 3\n---\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("a"))
		})
	})
})

var _ = Describe("ExecuteStep", func() {
	var (
		ctx  context.Context
		step *steps.ExecuteStep
	)

	BeforeEach(func() {
		ctx = context.Background()
		step = steps.NewExecuteStep()
	})

	Describe("Name", func() {
		It("returns 'execute'", func() {
			Expect(step.Name()).To(Equal("execute"))
		})
	})

	Describe("ShouldRun", func() {
		It("returns true when no ## Result section exists", func() {
			md := &agentlib.Markdown{}
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})

		It("returns false when ## Result section already exists", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Result\n\n```json\n{\n  \"operation\": \"add\",\n  \"value\": 5\n}\n```\n",
			)
			Expect(err).To(BeNil())
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Run", func() {
		It("adds 2 + 3 = 5 and transitions to ai_review", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"add\",\n  \"a\": 2,\n  \"b\": 3\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("ai_review"))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"operation": "add"`))
			Expect(section.Body).To(ContainSubstring(`"value": 5`))
		})

		It("subtracts 10 - 4 = 6", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: sub\na: 10\nb: 4\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"sub\",\n  \"a\": 10,\n  \"b\": 4\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": 6`))
		})

		It("multiplies 3 * 7 = 21", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: mul\na: 3\nb: 7\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"mul\",\n  \"a\": 3,\n  \"b\": 7\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": 21`))
		})

		It("returns needsInput when ## Plan section is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "---\noperation: add\na: 2\nb: 3\n---\n")
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Plan"))
		})

		It("returns needsInput when operation is unknown", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: div\na: 6\nb: 2\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"div\",\n  \"a\": 6,\n  \"b\": 2\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("unknown operation"))
		})

		It("returns needsInput when ## Plan has no json block", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\nNot json at all\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("json block missing"))
		})
	})
})

var _ = Describe("VerifyStep", func() {
	var (
		ctx  context.Context
		step *steps.VerifyStep
	)

	BeforeEach(func() {
		ctx = context.Background()
		step = steps.NewVerifyStep()
	})

	Describe("Name", func() {
		It("returns 'verify'", func() {
			Expect(step.Name()).To(Equal("verify"))
		})
	})

	Describe("ShouldRun", func() {
		It("always returns true — final phase has no skip guard", func() {
			md := &agentlib.Markdown{}
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Run", func() {
		It("passes when ## Plan (add, 2, 3) matches ## Result (value: 5)", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"add\",\n  \"a\": 2,\n  \"b\": 3\n}\n```\n\n## Result\n\n```json\n{\n  \"operation\": \"add\",\n  \"value\": 5\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("done"))

			section, ok := md.FindSection("## Review")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"verdict": "pass"`))
		})

		It("fails when ## Plan (add, 2, 3) does not match ## Result (value: 99)", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"add\",\n  \"a\": 2,\n  \"b\": 3\n}\n```\n\n## Result\n\n```json\n{\n  \"operation\": \"add\",\n  \"value\": 99\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("human_review"))
			Expect(result.Message).To(ContainSubstring("expected"))
			Expect(result.Message).To(ContainSubstring("got"))

			section, ok := md.FindSection("## Review")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"verdict": "fail"`))
		})

		It("returns needsInput when ## Plan section is missing", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Result\n\n```json\n{\n  \"operation\": \"add\",\n  \"value\": 5\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Plan"))
		})

		It("returns needsInput when ## Result section is missing", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"add\",\n  \"a\": 2,\n  \"b\": 3\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Result"))
		})

		It("returns needsInput when ## Plan has unknown operation", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: div\na: 6\nb: 2\n---\n\n## Plan\n\n```json\n{\n  \"operation\": \"div\",\n  \"a\": 6,\n  \"b\": 2\n}\n```\n\n## Result\n\n```json\n{\n  \"operation\": \"div\",\n  \"value\": 3\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("unknown operation"))
		})

		It("returns needsInput when ## Plan has no json block", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"---\noperation: add\na: 2\nb: 3\n---\n\n## Plan\n\nNot json at all\n\n## Result\n\n```json\n{\n  \"operation\": \"add\",\n  \"value\": 5\n}\n```\n",
			)
			Expect(err).To(BeNil())

			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("json block missing"))
		})
	})
})
