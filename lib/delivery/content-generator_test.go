// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent/lib"
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
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "## Result\n\n- Strategy: foo\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: completed"))
			Expect(generated).To(ContainSubstring("## Result"))
			Expect(generated).To(ContainSubstring("Strategy: foo"))
		})

		It(
			"sets status=in_progress and phase=human_review for failed result with ## Failure section",
			func() {
				original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nRun a backtest.\n"
				generated, err := generator.Generate(ctx, original, agentlib.AgentResultInfo{
					Status:  agentlib.AgentStatusFailed,
					Message: "claude CLI failed: exit status 1",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(generated).To(ContainSubstring("status: in_progress"))
				Expect(generated).To(ContainSubstring("phase: human_review"))
				Expect(generated).NotTo(ContainSubstring("phase: ai_review"))
				Expect(generated).To(ContainSubstring("## Failure"))
				Expect(generated).To(ContainSubstring("claude CLI failed: exit status 1"))
				Expect(generated).NotTo(ContainSubstring("## Result"))
			},
		)

		It(
			"keeps status=in_progress, phase=human_review, ## Result section for needs_input",
			func() {
				original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nRun a backtest.\n"
				generated, err := generator.Generate(ctx, original, agentlib.AgentResultInfo{
					Status:  agentlib.AgentStatusNeedsInput,
					Message: "no date range in task",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(generated).To(ContainSubstring("status: in_progress"))
				Expect(generated).To(ContainSubstring("phase: human_review"))
				Expect(generated).To(ContainSubstring("## Result"))
				Expect(generated).NotTo(ContainSubstring("## Failure"))
			},
		)

		It("sets status=in_progress and phase=human_review for needs_input result", func() {
			original := "---\ntitle: My Task\n---\n\n## Task\n\nRun a backtest.\n"
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusNeedsInput,
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
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
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
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
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
			agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
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
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusDone,
				Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
				Message: "hello from dev",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Count(generated, "## Result")).To(Equal(1))
		})

		It("does NOT duplicate the **Message:** line when Output already contains it", func() {
			original := "---\ntitle: My Task\n---\n"
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusDone,
				Output:  "## Result\n\n**Status:** done\n**Message:** hello from dev\n",
				Message: "hello from dev",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Count(generated, "**Message:** hello from dev")).To(Equal(1))
		})

		It("replaces an existing ## Result section without duplication on re-run", func() {
			original := "---\ntitle: My Task\n---\n\n## Result\n\n**Status:** done\n**Message:** old\n"
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusDone,
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

	Context("with AgentStatusInProgress", func() {
		It("sets status=in_progress and preserves phase from incoming task", func() {
			original := "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\n## Task\n\nRun a backtest.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusInProgress,
				Output: "## Plan\n\n- Step 1\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: in_progress"))
			Expect(generated).To(ContainSubstring("phase: planning"))
			Expect(generated).NotTo(ContainSubstring("phase: human_review"))
			Expect(generated).NotTo(ContainSubstring("phase: done"))
		})
	})

	Context("with empty Output (fallback minimal section)", func() {
		It(
			"synthesises a ## Failure block from Message when Output is empty for failed status",
			func() {
				original := "---\ntitle: My Task\n---\n"
				result := agentlib.AgentResultInfo{
					Status:  agentlib.AgentStatusFailed,
					Output:  "",
					Message: "container OOMKilled",
				}
				generated, err := generator.Generate(ctx, original, result)
				Expect(err).NotTo(HaveOccurred())
				Expect(generated).To(ContainSubstring("## Failure"))
				Expect(generated).To(ContainSubstring("container OOMKilled"))
				Expect(generated).NotTo(ContainSubstring("## Result"))
				Expect(strings.Count(generated, "## Failure")).To(Equal(1))
			},
		)

		It("omits **Message:** when both Output and Message are empty", func() {
			original := "---\ntitle: My Task\n---\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("**Status:** done"))
			Expect(generated).NotTo(ContainSubstring("**Message:**"))
		})
	})
})

var _ = Describe("PassthroughContentGenerator", func() {
	var (
		ctx       context.Context
		generator delivery.ContentGenerator
	)

	BeforeEach(func() {
		ctx = context.Background()
		generator = delivery.NewPassthroughContentGenerator()
	})

	It("writes ## Failure with result.Message on AgentStatusFailed", func() {
		result := agentlib.AgentResultInfo{
			Status:  agentlib.AgentStatusFailed,
			Output:  "",
			Message: "pr-plan claude run failed: claude CLI failed: exit status 1",
		}
		generated, err := generator.Generate(ctx, "", result)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).To(ContainSubstring("## Failure"))
		Expect(
			generated,
		).To(ContainSubstring("pr-plan claude run failed: claude CLI failed: exit status 1"))
	})

	It(
		"writes ## Failure with result.Message on AgentStatusFailed when Output has frontmatter",
		func() {
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Output:  "---\nstatus: in_progress\n---\nBody.\n",
				Message: "pr-plan claude run failed: claude CLI failed: exit status 1",
			}
			generated, err := generator.Generate(ctx, "", result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Failure"))
			Expect(
				generated,
			).To(ContainSubstring("pr-plan claude run failed: claude CLI failed: exit status 1"))
			Expect(generated).To(ContainSubstring("phase: human_review"))
			Expect(generated).To(ContainSubstring("status: in_progress"))
		},
	)

	It("writes ## Failure with result.Message on AgentStatusNeedsInput", func() {
		result := agentlib.AgentResultInfo{
			Status:  agentlib.AgentStatusNeedsInput,
			Output:  "",
			Message: "missing PR URL in task description",
		}
		generated, err := generator.Generate(ctx, "", result)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).To(ContainSubstring("## Failure"))
		Expect(generated).To(ContainSubstring("missing PR URL in task description"))
	})

	It(
		"returns result.Output verbatim with status=completed frontmatter on AgentStatusDone",
		func() {
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "---\ntitle: My Task\n---\n\n## Review\n\nLooks good.\n",
			}
			generated, err := generator.Generate(ctx, "", result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: completed"))
			Expect(generated).To(ContainSubstring("## Review"))
			Expect(generated).To(ContainSubstring("Looks good."))
			Expect(generated).NotTo(ContainSubstring("## Failure"))
		},
	)

	It("preserves phase from input and keeps status=in_progress on AgentStatusInProgress", func() {
		result := agentlib.AgentResultInfo{
			Status: agentlib.AgentStatusInProgress,
			Output: "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\n## Plan\n\n- Step 1\n",
		}
		generated, err := generator.Generate(ctx, "", result)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).To(ContainSubstring("status: in_progress"))
		Expect(generated).To(ContainSubstring("phase: planning"))
		Expect(generated).NotTo(ContainSubstring("phase: human_review"))
		Expect(generated).NotTo(ContainSubstring("phase: done"))
		Expect(generated).NotTo(ContainSubstring("## Failure"))
	})
})

var _ = DescribeTable(
	"every ContentGenerator surfaces result.Message on non-success status",
	func(generatorName string, generator delivery.ContentGenerator, status agentlib.AgentStatus) {
		ctx := context.Background()
		originalContent := "---\nstatus: in_progress\n---\nTags: [[Task]]\n"
		result := agentlib.AgentResultInfo{
			Status:  status,
			Output:  "",
			Message: "diagnostic message for " + string(status),
		}
		out, err := generator.Generate(ctx, originalContent, result)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("diagnostic message for "+string(status)),
			"%s on status=%s MUST surface result.Message in the body, got:\n%s",
			generatorName, status, out)
	},
	Entry(
		"fallback / failed",
		"fallback",
		delivery.NewFallbackContentGenerator(),
		agentlib.AgentStatusFailed,
	),
	Entry(
		"fallback / needs_input",
		"fallback",
		delivery.NewFallbackContentGenerator(),
		agentlib.AgentStatusNeedsInput,
	),
	Entry(
		"section / failed",
		"section",
		delivery.NewSectionContentGenerator("## Plan"),
		agentlib.AgentStatusFailed,
	),
	Entry(
		"section / needs_input",
		"section",
		delivery.NewSectionContentGenerator("## Plan"),
		agentlib.AgentStatusNeedsInput,
	),
	Entry(
		"passthrough / failed",
		"passthrough",
		delivery.NewPassthroughContentGenerator(),
		agentlib.AgentStatusFailed,
	),
	Entry(
		"passthrough / needs_input",
		"passthrough",
		delivery.NewPassthroughContentGenerator(),
		agentlib.AgentStatusNeedsInput,
	),
)

var _ = Describe("NewSectionContentGenerator", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("heading parameterization", func() {
		It("writes output under the configured heading for done status", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\n## Task\n\nDo planning.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "## Plan\n\n- Step 1\n- Step 2\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Plan"))
			Expect(generated).To(ContainSubstring("Step 1"))
			Expect(generated).To(ContainSubstring("Step 2"))
		})
	})

	Context("failure ignores heading", func() {
		It("writes ## Failure section regardless of configured heading", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\nstatus: in_progress\n---\n\n## Task\n\nDo planning.\n"
			result := agentlib.AgentResultInfo{
				Status:  agentlib.AgentStatusFailed,
				Message: "boom",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Failure"))
			Expect(generated).To(ContainSubstring("boom"))
			Expect(generated).NotTo(ContainSubstring("## Plan"))
		})
	})

	Context("empty Output uses minimal section", func() {
		It("uses buildMinimalResultSection when Output is empty for done status", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\n---\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("**Status:** done"))
		})
	})

	Context("in-place save preservation with AgentStatusInProgress", func() {
		It("preserves phase: planning when status is in_progress", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\nstatus: in_progress\nphase: planning\n---\n\n## Task\n\nDo planning.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusInProgress,
				Output: "## Plan\n\n- Draft step\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("status: in_progress"))
			Expect(generated).To(ContainSubstring("phase: planning"))
			Expect(generated).NotTo(ContainSubstring("phase: human_review"))
			Expect(generated).NotTo(ContainSubstring("phase: done"))
		})
	})

	Context("section replacement", func() {
		It("replaces existing ## Plan section without duplication", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\n---\n\n## Task\n\nDo planning.\n\n## Plan\n\nOld plan content.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "## Plan\n\nNew plan content.\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).NotTo(ContainSubstring("Old plan content."))
			Expect(generated).To(ContainSubstring("New plan content."))
			Expect(strings.Count(generated, "## Plan")).To(Equal(1))
		})
	})

	Context("section append", func() {
		It("appends ## Plan when not present in input", func() {
			generator := delivery.NewSectionContentGenerator("## Plan")
			original := "---\ntitle: My Task\n---\n\n## Task\n\nDo planning.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "## Plan\n\nAppended plan.\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Plan"))
			Expect(generated).To(ContainSubstring("Appended plan."))
		})
	})

	Context("custom heading", func() {
		It("writes output under ## Review when configured with that heading", func() {
			generator := delivery.NewSectionContentGenerator("## Review")
			original := "---\ntitle: My Task\n---\n\n## Task\n\nDo review.\n"
			result := agentlib.AgentResultInfo{
				Status: agentlib.AgentStatusDone,
				Output: "## Review\n\nLooks good.\n",
			}
			generated, err := generator.Generate(ctx, original, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(ContainSubstring("## Review"))
			Expect(generated).To(ContainSubstring("Looks good."))
		})
	})
})
