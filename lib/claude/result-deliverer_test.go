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
	"github.com/bborbe/agent/lib/delivery"
	libmocks "github.com/bborbe/agent/lib/mocks"
)

var errDelivery = errors.New("delivery failed")

var _ = Describe("BuildResultSection", func() {
	var (
		result  claude.AgentResult
		section string
	)

	JustBeforeEach(func() {
		section = claude.BuildResultSection(result)
	})

	BeforeEach(func() {
		result = claude.AgentResult{
			Status: claude.AgentStatusDone,
		}
	})

	It("always contains status", func() {
		Expect(section).To(ContainSubstring("**Status:** done"))
	})

	Context("with empty message", func() {
		BeforeEach(func() {
			result.Message = ""
		})

		It("omits Message line", func() {
			Expect(section).NotTo(ContainSubstring("**Message:**"))
		})
	})

	Context("with non-empty message", func() {
		BeforeEach(func() {
			result.Message = "something went wrong"
		})

		It("includes message", func() {
			Expect(section).To(ContainSubstring("**Message:** something went wrong"))
		})
	})

	Context("with files", func() {
		BeforeEach(func() {
			result.Files = []string{"report.md", "data.csv"}
		})

		It("renders each file as wiki link", func() {
			Expect(section).To(ContainSubstring("- [[report.md]]"))
			Expect(section).To(ContainSubstring("- [[data.csv]]"))
		})
	})

	Context("with no files", func() {
		BeforeEach(func() {
			result.Files = nil
		})

		It("omits Files section", func() {
			Expect(section).NotTo(ContainSubstring("**Files:**"))
		})
	})

	Context("with failed status", func() {
		BeforeEach(func() {
			result.Status = claude.AgentStatusFailed
			result.Message = "timeout"
		})

		It("contains failed status", func() {
			Expect(section).To(ContainSubstring("**Status:** failed"))
		})
	})
})

var _ = Describe("resultDelivererAdapter", func() {
	var (
		ctx         context.Context
		inner       *libmocks.AgentResultDeliverer
		adapter     claude.ResultDeliverer[claude.AgentResult]
		agentResult claude.AgentResult
		deliverErr  error
	)

	BeforeEach(func() {
		ctx = context.Background()
		inner = &libmocks.AgentResultDeliverer{}
		adapter = claude.NewResultDelivererAdapter[claude.AgentResult](inner)
		agentResult = claude.AgentResult{
			Status:  claude.AgentStatusDone,
			Message: "all good",
			Files:   []string{"out.md"},
		}
	})

	JustBeforeEach(func() {
		deliverErr = adapter.DeliverResult(ctx, agentResult)
	})

	It("calls inner deliverer once", func() {
		Expect(inner.DeliverResultCallCount()).To(Equal(1))
	})

	It("passes Status to inner deliverer", func() {
		_, info := inner.DeliverResultArgsForCall(0)
		Expect(info.Status).To(Equal(delivery.AgentStatusDone))
	})

	It("passes Message to inner deliverer", func() {
		_, info := inner.DeliverResultArgsForCall(0)
		Expect(info.Message).To(Equal("all good"))
	})

	It("passes BuildResultSection output as Output to inner deliverer", func() {
		_, info := inner.DeliverResultArgsForCall(0)
		expected := claude.BuildResultSection(agentResult)
		Expect(info.Output).To(Equal(expected))
	})

	It("returns nil on success", func() {
		Expect(deliverErr).To(BeNil())
	})

	Context("when inner deliverer returns error", func() {
		BeforeEach(func() {
			inner.DeliverResultReturns(errDelivery)
		})

		It("propagates the error", func() {
			Expect(deliverErr).To(MatchError(errDelivery))
		})
	})
})
