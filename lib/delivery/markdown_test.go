// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/delivery"
)

var _ = Describe("SetFrontmatterField", func() {
	It("updates an existing field", func() {
		content := "---\ntitle: Test\nstatus: in_progress\n---\n\nBody.\n"
		result := delivery.SetFrontmatterField(content, "status", "completed")
		Expect(result).To(ContainSubstring("status: completed"))
		Expect(result).NotTo(ContainSubstring("status: in_progress"))
	})

	It("adds a new field when not present", func() {
		content := "---\ntitle: Test\n---\n\nBody.\n"
		result := delivery.SetFrontmatterField(content, "phase", "done")
		Expect(result).To(ContainSubstring("phase: done"))
	})

	It("returns content unchanged when no frontmatter", func() {
		content := "# No frontmatter\n\nBody.\n"
		result := delivery.SetFrontmatterField(content, "status", "done")
		Expect(result).To(Equal(content))
	})

	It("does not confuse prefix keys (e.g. status vs backtest_status)", func() {
		content := "---\nstatus: running\ntitle: T\n---\n\nBody.\n"
		result := delivery.SetFrontmatterField(content, "backtest_state", "DONE")
		Expect(result).To(ContainSubstring("status: running"))
		Expect(result).To(ContainSubstring("backtest_state: DONE"))
	})
})

var _ = Describe("ReplaceOrAppendSection", func() {
	It("appends section when heading not found", func() {
		content := "---\ntitle: Test\n---\n\n## Task\n\nContent.\n"
		result := delivery.ReplaceOrAppendSection(content, "## Result", "## Result\n\nnew result\n")
		Expect(result).To(ContainSubstring("## Result"))
		Expect(result).To(ContainSubstring("new result"))
	})

	It("replaces existing section", func() {
		content := "---\ntitle: Test\n---\n\n## Task\n\nContent.\n\n## Result\n\nOld result.\n"
		result := delivery.ReplaceOrAppendSection(
			content,
			"## Result",
			"## Result\n\nnew result\n",
		)
		Expect(result).NotTo(ContainSubstring("Old result."))
		Expect(result).To(ContainSubstring("new result"))
	})

	It("replaces section when not the last section", func() {
		content := "---\ntitle: Test\n---\n\n## Result\n\nOld.\n\n## Appendix\n\nExtra.\n"
		result := delivery.ReplaceOrAppendSection(
			content,
			"## Result",
			"## Result\n\nNew.\n",
		)
		Expect(result).NotTo(ContainSubstring("Old."))
		Expect(result).To(ContainSubstring("New."))
		Expect(result).To(ContainSubstring("## Appendix"))
	})
})

var _ = Describe("ParseMarkdownFrontmatter", func() {
	It("returns empty map and full content when no frontmatter", func() {
		content := "# Hello\n\nBody text.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(BeEmpty())
		Expect(body).To(Equal(content))
	})

	It("returns empty map and full content when only opening delimiter", func() {
		content := "---\ntitle: Test\nNo closing delimiter\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(BeEmpty())
		Expect(body).To(Equal(content))
	})

	It("parses simple string frontmatter fields", func() {
		content := "---\ntitle: My Task\nstatus: in_progress\n---\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(HaveKeyWithValue("title", "My Task"))
		Expect(fm).To(HaveKeyWithValue("status", "in_progress"))
		Expect(body).To(Equal("Body.\n"))
	})

	It("handles arrays by converting to string representation", func() {
		content := "---\ntags:\n  - tag1\n  - tag2\n---\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(HaveKey("tags"))
		Expect(fm["tags"]).To(ContainSubstring("tag1"))
		Expect(fm["tags"]).To(ContainSubstring("tag2"))
		Expect(body).To(Equal("Body.\n"))
	})

	It("skips nil values", func() {
		content := "---\ntitle: Test\nnullfield:\n---\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(HaveKeyWithValue("title", "Test"))
		Expect(fm).NotTo(HaveKey("nullfield"))
		Expect(body).To(Equal("Body.\n"))
	})

	It("handles numeric values", func() {
		content := "---\ncount: 42\nprice: 3.14\n---\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(HaveKeyWithValue("count", "42"))
		Expect(fm).To(HaveKeyWithValue("price", "3.14"))
		Expect(body).To(Equal("Body.\n"))
	})

	It("returns empty map for invalid YAML", func() {
		content := "---\n: invalid yaml [\n---\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(BeEmpty())
		Expect(body).To(Equal(content))
	})

	It("strips leading newlines from body", func() {
		content := "---\ntitle: Test\n---\n\n\n\nBody.\n"
		fm, body := delivery.ParseMarkdownFrontmatter(content)
		Expect(fm).To(HaveKeyWithValue("title", "Test"))
		Expect(body).To(Equal("Body.\n"))
	})
})

var _ = Describe("IsValidMarkdownWithFrontmatter", func() {
	It("returns true for valid frontmatter", func() {
		Expect(
			delivery.IsValidMarkdownWithFrontmatter("---\ntitle: Test\n---\n\nBody.\n"),
		).To(BeTrue())
	})

	It("returns false when not starting with ---", func() {
		Expect(delivery.IsValidMarkdownWithFrontmatter("# No frontmatter")).To(BeFalse())
	})

	It("returns false when --- not followed by newline", func() {
		Expect(delivery.IsValidMarkdownWithFrontmatter("---title: Test\n---\n")).To(BeFalse())
	})

	It("returns false when no closing delimiter", func() {
		Expect(
			delivery.IsValidMarkdownWithFrontmatter("---\ntitle: Test\nno closing\n"),
		).To(BeFalse())
	})

	It("returns true for empty frontmatter", func() {
		Expect(delivery.IsValidMarkdownWithFrontmatter("---\n---\n\nBody.\n")).To(BeTrue())
	})

	It("returns true for frontmatter with CRLF line endings", func() {
		Expect(
			delivery.IsValidMarkdownWithFrontmatter("---\r\ntitle: Test\r\n---\r\n\r\nBody.\r\n"),
		).To(BeTrue())
	})
})

var _ = Describe("StripMarkdownCodeFences", func() {
	It("removes json code fence wrapper", func() {
		input := "```json\n{\"key\": \"value\"}\n```"
		result := delivery.StripMarkdownCodeFences(input)
		Expect(result).To(Equal(`{"key": "value"}`))
	})

	It("removes plain code fence wrapper", func() {
		input := "```\n{\"key\": \"value\"}\n```"
		result := delivery.StripMarkdownCodeFences(input)
		Expect(result).To(Equal(`{"key": "value"}`))
	})

	It("returns string unchanged when no code fence", func() {
		input := `{"key": "value"}`
		result := delivery.StripMarkdownCodeFences(input)
		Expect(result).To(Equal(input))
	})
})
