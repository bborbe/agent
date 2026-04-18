// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
)

var _ = Describe("ParseAllowedTools", func() {
	Context("with empty string", func() {
		It("returns nil", func() {
			result := claude.ParseAllowedTools("")
			Expect(result).To(BeNil())
		})
	})

	Context("with single tool", func() {
		It("returns slice with one element", func() {
			result := claude.ParseAllowedTools("bash")
			Expect(result).To(Equal(claude.AllowedTools{"bash"}))
		})
	})

	Context("with comma-separated tools", func() {
		It("returns all tools as slice", func() {
			result := claude.ParseAllowedTools("a,b,c")
			Expect(result).To(Equal(claude.AllowedTools{"a", "b", "c"}))
		})
	})
})

var _ = Describe("AllowedTools.String", func() {
	Context("with empty tools", func() {
		It("returns empty string", func() {
			tools := claude.AllowedTools{}
			Expect(tools.String()).To(Equal(""))
		})
	})

	Context("with single tool", func() {
		It("returns tool name", func() {
			tools := claude.AllowedTools{"bash"}
			Expect(tools.String()).To(Equal("bash"))
		})
	})

	Context("with multiple tools", func() {
		It("returns comma-joined string", func() {
			tools := claude.AllowedTools{"a", "b", "c"}
			Expect(tools.String()).To(Equal("a,b,c"))
		})
	})

	Context("round-trip", func() {
		It("ParseAllowedTools then String matches original", func() {
			original := "tool1,tool2,tool3"
			tools := claude.ParseAllowedTools(original)
			Expect(tools.String()).To(Equal(original))
		})
	})
})
