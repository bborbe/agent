// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/delivery"
)

var _ = Describe("FallbackContentGenerator", func() {
	var (
		ctx       context.Context
		generator delivery.ContentGenerator
	)

	BeforeEach(func() {
		ctx = context.Background()
		generator = delivery.NewFallbackContentGenerator()
	})

	Context("with frontmatter and body", func() {
		It("sets status=completed and phase=done for done result", func() {
			original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nRun a backtest.\n"
			result := delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
				Output: "## Result\n\n- Strategy: foo\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: completed"))
			Expect(generated).To(ContainSubstring("## Result"))
			Expect(generated).To(ContainSubstring("Strategy: foo"))
		})

		It("sets status=in_progress and phase=ai_review for failed result", func() {
			original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nRun a backtest.\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusFailed,
				Message: "timeout expired",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: in_progress"))
			Expect(generated).To(ContainSubstring("phase: ai_review"))
			Expect(generated).To(ContainSubstring("timeout expired"))
		})

		It("sets status=in_progress for needs_input result", func() {
			original := "---\ntitle: My Task\n---\n\n## Task\n\nRun a backtest.\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusNeedsInput,
				Message: "missing strategy",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: in_progress"))
		})
	})

	Context("with empty original content", func() {
		It("returns a ## Result section without frontmatter", func() {
			result := delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
				Output: "backtest complete",
			}
			generated, err := generator.Generate(ctx, "", result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Result"))
			Expect(generated).To(ContainSubstring("backtest complete"))
		})
	})

	Context("with existing ## Result section", func() {
		It("replaces the existing section", func() {
			original := "---\ntitle: My Task\n---\n\n## Task\n\nRun a backtest.\n\n## Result\n\nOld result.\n"
			result := delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
				Output: "new result content",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).NotTo(ContainSubstring("Old result."))
			Expect(generated).To(ContainSubstring("new result content"))
		})
	})

	It("output is valid markdown with frontmatter when original has frontmatter", func() {
		original := "---\ntitle: Test\n---\n\nBody.\n"
		generated, err := generator.Generate(
			ctx,
			original,
			delivery.AgentResultInfo{Status: delivery.AgentStatusDone, Output: "result"},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).To(HavePrefix("---"))
		Expect(generated[3:]).To(ContainSubstring("---"))
	})
})
