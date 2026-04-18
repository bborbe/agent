// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/claude"
)

var _ = Describe("BuildPrompt", func() {
	var (
		instructions string
		envContext   map[string]string
		taskContent  string
		result       string
	)

	JustBeforeEach(func() {
		result = claude.BuildPrompt(instructions, envContext, taskContent)
	})

	BeforeEach(func() {
		instructions = "You are a helpful agent."
		envContext = nil
		taskContent = "Do the thing."
	})

	It("instructions appear first", func() {
		Expect(result).To(HavePrefix("You are a helpful agent."))
	})

	It("task section is always present with ## Task header", func() {
		Expect(result).To(ContainSubstring("## Task"))
		Expect(result).To(ContainSubstring("Do the thing."))
	})

	Context("with empty envContext", func() {
		BeforeEach(func() {
			envContext = map[string]string{}
		})

		It("does not include env section", func() {
			Expect(result).NotTo(ContainSubstring("## Environment"))
		})
	})

	Context("with nil envContext", func() {
		BeforeEach(func() {
			envContext = nil
		})

		It("does not include env section", func() {
			Expect(result).NotTo(ContainSubstring("## Environment"))
		})
	})

	Context("with non-empty envContext", func() {
		BeforeEach(func() {
			envContext = map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			}
		})

		It("includes env section", func() {
			Expect(result).To(ContainSubstring("## Environment"))
		})

		It("includes env keys and values", func() {
			Expect(result).To(ContainSubstring("FOO: bar"))
			Expect(result).To(ContainSubstring("BAZ: qux"))
		})

		It("env keys are sorted alphabetically", func() {
			bazPos := indexOfString(result, "BAZ")
			fooPos := indexOfString(result, "FOO")
			Expect(bazPos).To(BeNumerically("<", fooPos))
		})

		It("env section appears after instructions", func() {
			instrPos := indexOfString(result, instructions)
			envPos := indexOfString(result, "## Environment")
			Expect(instrPos).To(BeNumerically("<", envPos))
		})
	})

	Context("with multiple env entries sorting check", func() {
		BeforeEach(func() {
			envContext = map[string]string{
				"ZEBRA":  "last",
				"ALPHA":  "first",
				"MIDDLE": "mid",
			}
		})

		It("keys appear in alphabetical order", func() {
			alphaPos := indexOfString(result, "ALPHA")
			middlePos := indexOfString(result, "MIDDLE")
			zebraPos := indexOfString(result, "ZEBRA")
			Expect(alphaPos).To(BeNumerically("<", middlePos))
			Expect(middlePos).To(BeNumerically("<", zebraPos))
		})
	})
})

// indexOfString returns the byte index of substr in s, or -1 if not found.
func indexOfString(s, substr string) int {
	for i := range s {
		if len(s)-i >= len(substr) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
