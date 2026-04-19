// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
	libmocks "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("TaskRunner", func() {
	var (
		ctx         context.Context
		runner      *libmocks.ClaudeRunner
		deliverer   *libmocks.ClaudeResultDeliverer
		taskRunner  claude.TaskRunner
		taskContent string
		result      *claude.AgentResult
		runErr      error
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &libmocks.ClaudeRunner{}
		deliverer = &libmocks.ClaudeResultDeliverer{}
		taskRunner = claude.NewTaskRunner(
			runner,
			claude.Instructions{
				{Name: "system", Content: "You are helpful."},
			},
			map[string]string{},
			deliverer,
		)
		taskContent = "Do something useful."
	})

	JustBeforeEach(func() {
		result, runErr = taskRunner.Run(ctx, taskContent)
	})

	Context("with empty task content", func() {
		BeforeEach(func() {
			taskContent = ""
		})

		It("returns no error", func() {
			Expect(runErr).To(BeNil())
		})

		It("returns AgentStatusNeedsInput", func() {
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusNeedsInput))
		})

		It("calls deliverer with NeedsInput status", func() {
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, delivered := deliverer.DeliverResultArgsForCall(0)
			Expect(delivered.Status).To(Equal(claude.AgentStatusNeedsInput))
		})
	})

	Context("when runner returns error", func() {
		BeforeEach(func() {
			runner.RunReturns(nil, errors.New("cli crashed"))
		})

		It("returns no error", func() {
			Expect(runErr).To(BeNil())
		})

		It("returns AgentStatusFailed", func() {
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusFailed))
		})

		It("calls deliverer with Failed status", func() {
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, delivered := deliverer.DeliverResultArgsForCall(0)
			Expect(delivered.Status).To(Equal(claude.AgentStatusFailed))
		})
	})

	Context("when runner returns invalid JSON", func() {
		BeforeEach(func() {
			runner.RunReturns(&claude.ClaudeResult{Result: "not-json"}, nil)
		})

		It("returns no error", func() {
			Expect(runErr).To(BeNil())
		})

		It("returns AgentStatusFailed", func() {
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusFailed))
		})

		It("calls deliverer with Failed status", func() {
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, delivered := deliverer.DeliverResultArgsForCall(0)
			Expect(delivered.Status).To(Equal(claude.AgentStatusFailed))
		})
	})

	Context("when runner returns valid JSON result", func() {
		BeforeEach(func() {
			runner.RunReturns(&claude.ClaudeResult{
				Result: `{"status":"done","message":"task complete"}`,
			}, nil)
		})

		It("returns no error", func() {
			Expect(runErr).To(BeNil())
		})

		It("returns parsed AgentResult", func() {
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusDone))
			Expect(result.Message).To(Equal("task complete"))
		})

		It("calls deliverer with the parsed result", func() {
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, delivered := deliverer.DeliverResultArgsForCall(0)
			Expect(delivered.Status).To(Equal(claude.AgentStatusDone))
			Expect(delivered.Message).To(Equal("task complete"))
		})
	})

	Context("when runner returns JSON preceded by prose (spec 010)", func() {
		BeforeEach(func() {
			runner.RunReturns(&claude.ClaudeResult{
				Result: "No live-stage trades exist for 2026-04-03.\n\n{\"status\":\"needs_input\",\"message\":\"no trades\"}",
			}, nil)
		})

		It("parses the trailing JSON object", func() {
			Expect(runErr).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusNeedsInput))
			Expect(result.Message).To(Equal("no trades"))
		})
	})

	Context("when runner returns JSON with trailing prose (spec 010)", func() {
		BeforeEach(func() {
			runner.RunReturns(&claude.ClaudeResult{
				Result: "{\"status\":\"done\",\"message\":\"ok\"}\n\nThat concludes the analysis.",
			}, nil)
		})

		It("parses the leading JSON object", func() {
			Expect(runErr).To(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusDone))
		})
	})

	Context("when runner returns JSON with nested braces and quoted strings (spec 010)", func() {
		BeforeEach(func() {
			runner.RunReturns(&claude.ClaudeResult{
				Result: `Narrative text. {"status":"done","message":"result with }curly{ inside","data":{"nested":true}}`,
			}, nil)
		})

		It("extracts the outermost balanced object", func() {
			Expect(runErr).To(BeNil())
			Expect(result.Status).To(Equal(claude.AgentStatusDone))
			Expect(result.Message).To(Equal("result with }curly{ inside"))
		})
	})
})
