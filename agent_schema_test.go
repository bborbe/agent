// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
)

var _ = Describe("Schema", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("ExtractSection", func() {
		It("returns section when found with valid JSON", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "```json\n{\"action\":\"test\"}\n```"},
				},
			}

			result, err := lib.ExtractSection[map[string]any](ctx, md, "## Plan")
			Expect(err).To(BeNil())
			Expect(*result).To(HaveKeyWithValue("action", "test"))
		})

		It("returns error when section missing", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Plan", Body: "```json\n{\"action\":\"test\"}\n```"},
				},
			}

			_, err := lib.ExtractSection[map[string]any](ctx, md, "## Missing")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("## Missing"))
			Expect(err.Error()).To(ContainSubstring("missing"))
		})
	})

	Describe("ExtractSectionMap", func() {
		It("extracts section as map[string]any", func() {
			md := &lib.Markdown{
				Sections: []lib.Section{
					{Heading: "## Config", Body: "```json\n{\"key\":\"value\"}\n```"},
				},
			}

			result, err := lib.ExtractSectionMap(ctx, md, "## Config")
			Expect(err).To(BeNil())
			Expect(result).To(HaveKeyWithValue("key", "value"))
		})
	})

	Describe("MarshalSectionTyped", func() {
		It("marshals value as section with JSON fence", func() {
			section, err := lib.MarshalSectionTyped(
				ctx,
				"## Plan",
				map[string]string{"action": "do something"},
			)
			Expect(err).To(BeNil())
			Expect(section.Heading).To(Equal("## Plan"))
			Expect(section.Body).To(ContainSubstring("```json"))
			Expect(section.Body).To(ContainSubstring("do something"))
		})

		It("returns error when JSON marshal fails", func() {
			_, err := lib.MarshalSectionTyped(ctx, "## Bad", func() {})
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("marshal"))
		})
	})
})
