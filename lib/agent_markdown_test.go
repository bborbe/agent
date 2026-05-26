// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("Markdown", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Marshal", func() {
		It("serializes frontmatter and multiple sections", func() {
			md := &lib.Markdown{
				Frontmatter: lib.TaskFrontmatter{
					"status":   "in_progress",
					"assignee": "test-agent",
				},
				Preamble: "Some preamble text",
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "plan content"},
					{Heading: "## Review", Body: ""},
				},
			}

			out, err := md.Marshal(ctx)
			Expect(err).To(BeNil())
			Expect(out).To(ContainSubstring("---\n"))
			Expect(out).To(ContainSubstring("status: in_progress"))
			Expect(out).To(ContainSubstring("assignee: test-agent"))
			Expect(out).To(ContainSubstring("---\n"))
			Expect(out).To(ContainSubstring("Some preamble text"))
			Expect(out).To(ContainSubstring("## Plan"))
			Expect(out).To(ContainSubstring("plan content"))
			Expect(out).To(ContainSubstring("## Review"))
		})

		It("serializes without frontmatter", func() {
			md := &lib.Markdown{
				Preamble: "preamble only",
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "content"},
				},
			}

			out, err := md.Marshal(ctx)
			Expect(err).To(BeNil())
			Expect(out).NotTo(ContainSubstring("---\n"))
			Expect(out).To(ContainSubstring("preamble only"))
			Expect(out).To(ContainSubstring("## Plan"))
		})
	})

	Describe("ParseMarkdown", func() {
		It("parses valid frontmatter and body", func() {
			content := `---
status: done
---

preamble text

## Plan

some plan content
`
			md, err := lib.ParseMarkdown(ctx, content)
			Expect(err).To(BeNil())
			Expect(md.Frontmatter).NotTo(BeNil())
			statusVal, ok := md.Frontmatter["status"].(string)
			Expect(ok).To(BeTrue())
			Expect(statusVal).To(Equal("done"))
			Expect(md.Preamble).To(ContainSubstring("preamble text"))
			Expect(len(md.Sections)).To(BeNumerically(">", 0))
		})

		It("parses body without frontmatter delimiters", func() {
			content := `no frontmatter

## Plan

plan content
`
			md, err := lib.ParseMarkdown(ctx, content)
			Expect(err).To(BeNil())
			Expect(md.Frontmatter).To(BeEmpty())
			Expect(md.Preamble).To(ContainSubstring("no frontmatter"))
		})

		It("handles malformed YAML gracefully", func() {
			content := `---
status: [invalid yaml
---

## Plan
`
			md, err := lib.ParseMarkdown(ctx, content)
			Expect(err).To(BeNil())
			Expect(md.Frontmatter).To(BeEmpty())
		})
	})

	Describe("FindSection", func() {
		It("returns section when present", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "content"},
					{Heading: "## Review", Body: "review content"},
				},
			}

			section, found := md.FindSection("## Review")
			Expect(found).To(BeTrue())
			Expect(section.Heading).To(Equal("## Review"))
			Expect(section.Body).To(Equal("review content"))
		})

		It("returns false when section absent", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "content"},
				},
			}

			_, found := md.FindSection("## Missing")
			Expect(found).To(BeFalse())
		})
	})

	Describe("AddSection", func() {
		It("appends section to end", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "content"},
				},
			}

			md.AddSection(lib.Section{Heading: "## Review", Body: "review content"})
			Expect(len(md.Sections)).To(Equal(2))
			Expect(md.Sections[1].Heading).To(Equal("## Review"))
		})
	})

	Describe("ReplaceSection", func() {
		It("replaces existing section", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "old content"},
				},
			}

			md.ReplaceSection(lib.Section{Heading: "## Plan", Body: "new content"})
			Expect(len(md.Sections)).To(Equal(1))
			Expect(md.Sections[0].Body).To(Equal("new content"))
		})

		It("appends when section does not exist", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "content"},
				},
			}

			md.ReplaceSection(lib.Section{Heading: "## New", Body: "new section"})
			Expect(len(md.Sections)).To(Equal(2))
		})
	})

	Describe("InsertSection", func() {
		It("inserts before given heading", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "plan"},
					{Heading: "## Review", Body: "review"},
				},
			}

			md.InsertSection(1, lib.Section{Heading: "## Execute", Body: "exec"})
			Expect(len(md.Sections)).To(Equal(3))
			Expect(md.Sections[1].Heading).To(Equal("## Execute"))
		})

		It("clamps negative position to 0", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "plan"},
				},
			}

			md.InsertSection(-5, lib.Section{Heading: "## First", Body: "first"})
			Expect(md.Sections[0].Heading).To(Equal("## First"))
		})

		It("clamps out-of-range position to len", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "plan"},
				},
			}

			md.InsertSection(99, lib.Section{Heading: "## Last", Body: "last"})
			Expect(md.Sections[len(md.Sections)-1].Heading).To(Equal("## Last"))
		})
	})
})
