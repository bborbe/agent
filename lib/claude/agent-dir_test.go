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

var _ = Describe("AgentDir", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("String", func() {
		It("returns raw value without expansion", func() {
			Expect(claude.AgentDir("~/workspace").String()).To(Equal("~/workspace"))
		})

		It("returns empty string for empty value", func() {
			Expect(claude.AgentDir("").String()).To(Equal(""))
		})

		It("returns absolute path unchanged", func() {
			Expect(
				claude.AgentDir("/home/user/workspace").String(),
			).To(Equal("/home/user/workspace"))
		})
	})

	Describe("Resolve", func() {
		It("expands tilde prefix to home directory", func() {
			t := GinkgoT()
			t.Setenv("HOME", "/test-home")
			result, err := claude.AgentDir("~/workspace").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/test-home/workspace"))
		})

		It("returns empty string for empty value with no error", func() {
			result, err := claude.AgentDir("").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(""))
		})

		It("returns absolute path unchanged", func() {
			result, err := claude.AgentDir("/home/user/workspace").Resolve(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("/home/user/workspace"))
		})
	})
})
