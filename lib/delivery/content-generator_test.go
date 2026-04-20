// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"strings"

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

		It("sets status=in_progress and phase=human_review for needs_input result", func() {
			original := "---\ntitle: My Task\n---\n\n## Task\n\nRun a backtest.\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusNeedsInput,
				Message: "missing strategy",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: in_progress"))
			Expect(generated).To(ContainSubstring("phase: human_review"))
		})
	})

	Context("with empty original content", func() {
		It("returns a ## Result section without frontmatter", func() {
			result := delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
				Output: "## Result\n\nbacktest complete\n",
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
				Output: "## Result\n\nnew result content\n",
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
			delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
				Output: "## Result\n\nresult\n",
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).To(HavePrefix("---"))
		Expect(generated[3:]).To(ContainSubstring("---"))
	})

	Context("2026-04-20b regression", func() {
		It("does NOT double the ## Result heading when Output contains it", func() {
			original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Details\n\nd\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusDone,
				Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
				Message: "hello from dev",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Count(generated, "## Result")).To(Equal(1))
		})

		It("does NOT duplicate the **Message:** line when Output already contains it", func() {
			original := "---\ntitle: My Task\n---\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusDone,
				Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
				Message: "hello from dev",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Count(generated, "**Message:** hello from dev")).To(Equal(1))
		})

		It("replaces an existing ## Result section without duplication on re-run", func() {
			original := "---\ntitle: My Task\n---\n\n## Result\n\n**Status:** done\n**Message:** old\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusDone,
				Output:  "## Result\n\n**Status:** done\n**Message:** new\n",
				Message: "new",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Count(generated, "## Result")).To(Equal(1))
			Expect(strings.Count(generated, "**Message:** new")).To(Equal(1))
			Expect(generated).NotTo(ContainSubstring("**Message:** old"))
		})
	})

	Context("with empty Output (fallback minimal section)", func() {
		It("synthesises a ## Result block from Status+Message when Output is empty", func() {
			original := "---\ntitle: My Task\n---\n"
			result := delivery.AgentResultInfo{
				Status:  delivery.AgentStatusFailed,
				Output:  "",
				Message: "container OOMKilled",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Result"))
			Expect(generated).To(ContainSubstring("**Status:** failed"))
			Expect(generated).To(ContainSubstring("**Message:** container OOMKilled"))
			Expect(strings.Count(generated, "## Result")).To(Equal(1))
		})

		It("omits **Message:** when both Output and Message are empty", func() {
			original := "---\ntitle: My Task\n---\n"
			result := delivery.AgentResultInfo{
				Status: delivery.AgentStatusDone,
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("**Status:** done"))
			Expect(generated).NotTo(ContainSubstring("**Message:**"))
		})
	})
})
