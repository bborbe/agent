// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/claude"
)

// expandTilde is tested indirectly through ClaudeConfigDir.Resolve.
var _ = Describe("expandTilde", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Context("empty input", func() {
		It("returns empty string with no error", func() {
			result, err := claude.ClaudeConfigDir("").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(""))
		})
	})

	Context("tilde-slash prefix", func() {
		It("expands ~/.claude to <home>/.claude", func() {
			t := GinkgoT()
			t.Setenv("HOME", "/test-home")
			result, err := claude.ClaudeConfigDir("~/.claude").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/test-home/.claude"))
		})
	})

	Context("lone tilde", func() {
		It("expands ~ to home directory", func() {
			t := GinkgoT()
			t.Setenv("HOME", "/test-home")
			result, err := claude.ClaudeConfigDir("~").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/test-home"))
		})
	})

	Context("absolute path", func() {
		It("returns path unchanged", func() {
			result, err := claude.ClaudeConfigDir("/abs/path").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/abs/path"))
		})
	})

	Context("relative path", func() {
		It("returns path unchanged", func() {
			result, err := claude.ClaudeConfigDir("relative/path").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("relative/path"))
		})
	})

	Context("other-user tilde form", func() {
		It("returns path unchanged (not expanded)", func() {
			result, err := claude.ClaudeConfigDir("~user/foo").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("~user/foo"))
		})
	})
})
