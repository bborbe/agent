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
})
