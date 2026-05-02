// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
)

var _ = Describe("ClaudeConfigDir", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("String", func() {
		It("returns raw value without expansion", func() {
			Expect(claude.ClaudeConfigDir("~/.claude").String()).To(Equal("~/.claude"))
		})

		It("returns empty string for empty value", func() {
			Expect(claude.ClaudeConfigDir("").String()).To(Equal(""))
		})

		It("returns absolute path unchanged", func() {
			Expect(
				claude.ClaudeConfigDir("/home/user/.claude").String(),
			).To(Equal("/home/user/.claude"))
		})
	})

	Describe("Resolve", func() {
		It("expands tilde prefix to home directory", func() {
			t := GinkgoT()
			t.Setenv("HOME", "/test-home")
			result, err := claude.ClaudeConfigDir("~/.claude").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/test-home/.claude"))
		})

		It("returns empty string for empty value with no error", func() {
			result, err := claude.ClaudeConfigDir("").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(""))
		})

		It("returns absolute path unchanged", func() {
			result, err := claude.ClaudeConfigDir("/home/user/.claude").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/home/user/.claude"))
		})
	})
})
