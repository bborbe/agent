// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package envparse_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/envparse"
)

var _ = Describe("IsSensitiveKey", func() {
	It("flags ANTHROPIC_AUTH_TOKEN as sensitive", func() {
		Expect(envparse.IsSensitiveKey("ANTHROPIC_AUTH_TOKEN")).To(BeTrue())
	})

	It("flags GH_TOKEN as sensitive", func() {
		Expect(envparse.IsSensitiveKey("GH_TOKEN")).To(BeTrue())
	})

	It("flags GITHUB_TOKEN as sensitive", func() {
		Expect(envparse.IsSensitiveKey("GITHUB_TOKEN")).To(BeTrue())
	})

	It("flags ANTHROPIC_API_KEY as sensitive", func() {
		Expect(envparse.IsSensitiveKey("ANTHROPIC_API_KEY")).To(BeTrue())
	})

	It("flags DB_PASSWORD as sensitive", func() {
		Expect(envparse.IsSensitiveKey("DB_PASSWORD")).To(BeTrue())
	})

	It("flags MY_SECRET as sensitive", func() {
		Expect(envparse.IsSensitiveKey("MY_SECRET")).To(BeTrue())
	})

	It("flags AWS_ACCESS_KEY_ID as sensitive", func() {
		Expect(envparse.IsSensitiveKey("AWS_ACCESS_KEY_ID")).To(BeTrue())
	})

	It("flags lowercase aws_credentials as sensitive (case-insensitive)", func() {
		Expect(envparse.IsSensitiveKey("aws_credentials")).To(BeTrue())
	})

	It("does not flag PATH as sensitive", func() {
		Expect(envparse.IsSensitiveKey("PATH")).To(BeFalse())
	})

	It("does not flag HOME as sensitive", func() {
		Expect(envparse.IsSensitiveKey("HOME")).To(BeFalse())
	})

	It("does not flag ANTHROPIC_BASE_URL as sensitive", func() {
		Expect(envparse.IsSensitiveKey("ANTHROPIC_BASE_URL")).To(BeFalse())
	})

	It("does not flag ANTHROPIC_MODEL as sensitive", func() {
		Expect(envparse.IsSensitiveKey("ANTHROPIC_MODEL")).To(BeFalse())
	})

	It("does not flag BOT_GITHUB_LOGIN as sensitive", func() {
		Expect(envparse.IsSensitiveKey("BOT_GITHUB_LOGIN")).To(BeFalse())
	})

	It("does not flag ZONEINFO as sensitive", func() {
		Expect(envparse.IsSensitiveKey("ZONEINFO")).To(BeFalse())
	})
})

var _ = Describe("RedactForLog", func() {
	It("returns nil for nil input (preserves cmd.Env inherit-parent semantics)", func() {
		Expect(envparse.RedactForLog(nil)).To(BeNil())
	})

	It("returns an empty slice for empty input", func() {
		Expect(envparse.RedactForLog([]string{})).To(Equal([]string{}))
	})

	It(
		"keeps non-sensitive entries verbatim and redacts sensitive values, preserving order",
		func() {
			input := []string{
				"PATH=/usr/bin",
				"ANTHROPIC_AUTH_TOKEN=sk-ant-xxxxxxxx",
				"HOME=/home/agent",
				"GH_TOKEN=ghp_yyyyyyyy",
			}
			expected := []string{
				"PATH=/usr/bin",
				"ANTHROPIC_AUTH_TOKEN=***",
				"HOME=/home/agent",
				"GH_TOKEN=***",
			}
			Expect(envparse.RedactForLog(input)).To(Equal(expected))
		},
	)

	It("replaces an empty sensitive value with ***", func() {
		Expect(envparse.RedactForLog([]string{"GH_TOKEN="})).To(Equal([]string{"GH_TOKEN=***"}))
	})

	It("passes entries without '=' through unchanged", func() {
		Expect(
			envparse.RedactForLog([]string{"NOEQ", "FOO=bar"}),
		).To(Equal([]string{"NOEQ", "FOO=bar"}))
	})

	It("does not mutate the input slice", func() {
		input := []string{"PATH=/usr/bin", "GH_TOKEN=ghp_secret"}
		_ = envparse.RedactForLog(input)
		Expect(input).To(Equal([]string{"PATH=/usr/bin", "GH_TOKEN=ghp_secret"}))
	})
})
