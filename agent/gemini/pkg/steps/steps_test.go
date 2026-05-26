// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"

	agentlib "github.com/bborbe/agent/lib"
)

func TestSteps(t *testing.T) {
	time.Local = time.UTC
	format.TruncatedDiff = false
	RegisterFailHandler(Fail)
	RunSpecs(t, "Steps Suite")
}

var _ = Describe("ExecuteStep", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Name", func() {
		It("returns execute", func() {
			step := NewExecuteStep()
			Expect(step.Name()).To(Equal("execute"))
		})
	})

	Describe("ShouldRun", func() {
		It("returns true when ## Result is absent", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"## Plan\n```json\n{\"operation\":\"add\",\"a\":1,\"b\":2}\n```",
			)
			Expect(err).To(BeNil())
			step := NewExecuteStep()
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})

		It("returns false when ## Result is present", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"## Plan\n```json\n{\"operation\":\"add\",\"a\":1,\"b\":2}\n```\n\n## Result\n```json\n{\"value\":3}\n```",
			)
			Expect(err).To(BeNil())
			step := NewExecuteStep()
			ok, err := step.ShouldRun(ctx, md)
			Expect(err).To(BeNil())
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Run", func() {
		It("adds two numbers", func() {
			md := makePlanMarkdown(ctx, "add", 2, 3)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("ai_review"))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"operation": "add"`))
			Expect(section.Body).To(ContainSubstring(`"value": 5`))
		})

		It("subtracts two numbers", func() {
			md := makePlanMarkdown(ctx, "sub", 5, 3)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": 2`))
		})

		It("multiplies two numbers", func() {
			md := makePlanMarkdown(ctx, "mul", 4, 7)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": 28`))
		})

		It("returns needsInput for unknown operation", func() {
			md := makePlanMarkdown(ctx, "div", 1, 2)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("unknown operation"))
		})

		It("returns needsInput when ## Plan is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "## Result\n```json\n{\"value\":3}\n```")
			Expect(err).To(BeNil())
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Plan section missing"))
		})

		It("handles negative numbers", func() {
			md := makePlanMarkdown(ctx, "add", -5, 3)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": -2`))
		})

		It("handles zero", func() {
			md := makePlanMarkdown(ctx, "add", 0, 0)
			step := NewExecuteStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

			section, ok := md.FindSection("## Result")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"value": 0`))
		})
	})
})

var _ = Describe("VerifyStep", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Name", func() {
		It("returns verify", func() {
			step := NewVerifyStep()
			Expect(step.Name()).To(Equal("verify"))
		})
	})

	Describe("ShouldRun", func() {
		It("always returns true", func() {
			step := NewVerifyStep()
			ok, err := step.ShouldRun(ctx, nil)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Run", func() {
		It("sets nextPhase to done on pass verdict", func() {
			md := makeVerifyMarkdown(ctx, "add", 2, 3, 5)
			step := NewVerifyStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("done"))
			Expect(result.Message).To(BeEmpty())

			section, ok := md.FindSection("## Review")
			Expect(ok).To(BeTrue())
			Expect(section.Body).To(ContainSubstring(`"verdict": "pass"`))
		})

		It("sets nextPhase to human_review on fail verdict", func() {
			md := makeVerifyMarkdown(ctx, "add", 2, 3, 99)
			step := NewVerifyStep()
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

		It("returns needsInput when ## Plan is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "## Result\n```json\n{}\n```")
			Expect(err).To(BeNil())
			step := NewVerifyStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Plan section missing"))
		})

		It("returns needsInput when ## Result is missing", func() {
			md, err := agentlib.ParseMarkdown(ctx, "## Plan\n```json\n{}\n```")
			Expect(err).To(BeNil())
			step := NewVerifyStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("## Result section missing"))
		})

		It("returns needsInput when result has malformed JSON", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"## Plan\n```json\n{\"operation\":\"add\",\"a\":2,\"b\":3}\n```\n\n## Result\nnot json at all",
			)
			Expect(err).To(BeNil())
			step := NewVerifyStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("json block missing"))
		})

		It("returns needsInput when compute fails with unknown op", func() {
			md, err := agentlib.ParseMarkdown(
				ctx,
				"## Plan\n```json\n{\"operation\":\"div\",\"a\":1,\"b\":2}\n```\n\n## Result\n```json\n{\"operation\":\"div\",\"value\":0}\n```",
			)
			Expect(err).To(BeNil())
			step := NewVerifyStep()
			result, err := step.Run(ctx, md)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
			Expect(result.Message).To(ContainSubstring("unknown operation"))
		})
	})
})

var _ = Describe("compute", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("adds two numbers", func() {
		result, err := compute(ctx, "add", 2, 3)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(5))
	})

	It("subtracts two numbers", func() {
		result, err := compute(ctx, "sub", 5, 3)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(2))
	})

	It("multiplies two numbers", func() {
		result, err := compute(ctx, "mul", 4, 7)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(28))
	})

	It("returns error for unknown operation", func() {
		result, err := compute(ctx, "div", 1, 2)
		Expect(err).To(Not(BeNil()))
		Expect(err.Error()).To(ContainSubstring("unknown operation"))
		Expect(result).To(Equal(0))
	})

	It("handles negative numbers", func() {
		result, err := compute(ctx, "add", -10, -5)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(-15))
	})

	It("handles zero", func() {
		result, err := compute(ctx, "mul", 0, 99)
		Expect(err).To(BeNil())
		Expect(result).To(Equal(0))
	})
})

var _ = Describe("needsInput", func() {
	It("returns AgentStatusNeedsInput in result", func() {
		result, err := needsInput("something missing")
		Expect(err).To(BeNil())
		Expect(result.Status).To(Equal(agentlib.AgentStatusNeedsInput))
		Expect(result.Message).To(Equal("something missing"))
	})
})

// Helpers

func makePlanMarkdown(ctx context.Context, op string, a, b int) *agentlib.Markdown {
	content := fmt.Sprintf(
		"## Plan\n```json\n{\"operation\":\"%s\",\"a\":%d,\"b\":%d}\n```",
		op,
		a,
		b,
	)
	md, err := agentlib.ParseMarkdown(ctx, content)
	if err != nil {
		panic("makePlanMarkdown: " + err.Error())
	}
	return md
}

func makeVerifyMarkdown(ctx context.Context, op string, a, b, resultValue int) *agentlib.Markdown {
	content := fmt.Sprintf(
		"## Plan\n```json\n{\"operation\":\"%s\",\"a\":%d,\"b\":%d}\n```\n\n## Result\n```json\n{\"operation\":\"%s\",\"value\":%d}\n```",
		op,
		a,
		b,
		op,
		resultValue,
	)
	md, err := agentlib.ParseMarkdown(ctx, content)
	if err != nil {
		panic("makeVerifyMarkdown: " + err.Error())
	}
	return md
}
