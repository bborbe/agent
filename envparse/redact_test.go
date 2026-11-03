// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package envparse_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/envparse"
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

	It("flags HTTPS_PROXY as sensitive (a proxy URL may embed credentials)", func() {
		Expect(envparse.IsSensitiveKey("HTTPS_PROXY")).To(BeTrue())
	})

	It("flags lowercase http_proxy as sensitive (case-insensitive)", func() {
		Expect(envparse.IsSensitiveKey("http_proxy")).To(BeTrue())
	})

	It(
		"flags NO_PROXY as sensitive too — no secret in it, but the marker is deliberately broad",
		func() {
			Expect(envparse.IsSensitiveKey("NO_PROXY")).To(BeTrue())
		},
	)

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

	DescribeTable(
		"classifies GitHub App and Anthropic identifiers",
		func(key string, sensitive bool) {
			Expect(envparse.IsSensitiveKey(key)).To(Equal(sensitive))
		},
		Entry("GitHub App private key is sensitive", "GITHUB_APP_PEM", true),
		Entry("GitHub App id is not sensitive", "GITHUB_APP_ID", false),
		Entry("GitHub App installation id is not sensitive", "GITHUB_APP_INSTALLATION_ID", false),
		Entry("Anthropic base URL is not sensitive", "ANTHROPIC_BASE_URL", false),
		Entry("Anthropic model is not sensitive", "ANTHROPIC_MODEL", false),
	)
})

var _ = Describe("RedactForLog", func() {
	It("returns nil for nil input (preserves cmd.Env inherit-parent semantics)", func() {
		Expect(envparse.RedactForLog(nil)).To(BeNil())
	})

	It("returns an empty slice for empty input", func() {
		Expect(envparse.RedactForLog([]string{})).To(Equal([]string{}))
	})

	It("redacts a credential-bearing proxy URL", func() {
		input := []string{"HTTPS_PROXY=http://user:pass@proxy:8888"}
		Expect(envparse.RedactForLog(input)).To(Equal([]string{"HTTPS_PROXY=***"}))
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

	pemPlaceholder := "-----BEGIN RSA PRIVATE KEY-----sentinel-----END RSA PRIVATE KEY-----" // #nosec G101 -- synthetic placeholder, not a real key

	DescribeTable(
		"redacts every sensitive marker, keeping the key name and replacing the value",
		func(entry, expected string) {
			Expect(envparse.RedactForLog([]string{entry})).To(Equal([]string{expected}))
		},
		Entry("PEM", "GITHUB_APP_PEM="+pemPlaceholder, "GITHUB_APP_PEM=***"),
		Entry("TOKEN", "ANTHROPIC_AUTH_TOKEN=sk-ant-xxxxxxxx", "ANTHROPIC_AUTH_TOKEN=***"),
		Entry("SECRET", "MY_SECRET=value", "MY_SECRET=***"),
		Entry("PASSWORD", "DB_PASSWORD=hunter2", "DB_PASSWORD=***"),
		Entry("PASSWD", "DB_PASSWD=hunter2", "DB_PASSWD=***"),
		Entry("CREDENTIAL", "AWS_CREDENTIAL=value", "AWS_CREDENTIAL=***"),
		Entry("API_KEY", "ANTHROPIC_API_KEY=sk-ant-xxxxxxxx", "ANTHROPIC_API_KEY=***"),
		Entry("PRIVATE_KEY", "SSH_PRIVATE_KEY=value", "SSH_PRIVATE_KEY=***"),
		Entry("ACCESS_KEY", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "AWS_ACCESS_KEY_ID=***"),
		Entry("PROXY", "HTTPS_PROXY=http://user:pass@proxy:8888", "HTTPS_PROXY=***"),
	)

	DescribeTable(
		"passes non-sensitive entries through unchanged",
		func(entry string) {
			Expect(envparse.RedactForLog([]string{entry})).To(Equal([]string{entry}))
		},
		Entry("GitHub App id", "GITHUB_APP_ID=123456"),
		Entry("GitHub App installation id", "GITHUB_APP_INSTALLATION_ID=98765432"),
		Entry("Anthropic base URL", "ANTHROPIC_BASE_URL=http://127.0.0.1:8788"),
		Entry("Anthropic model", "ANTHROPIC_MODEL=claude-sonnet-4"),
	)

	It("redacts only the PEM and token in a realistic mixed environment, preserving order", func() {
		input := []string{
			"GITHUB_APP_ID=123456",
			"GITHUB_APP_PEM=" + pemPlaceholder,
			"GITHUB_APP_INSTALLATION_ID=98765432",
			"ANTHROPIC_BASE_URL=http://127.0.0.1:8788",
			"ANTHROPIC_AUTH_TOKEN=sk-ant-xxxxxxxx",
		}
		redacted := envparse.RedactForLog(input)
		Expect(redacted).To(Equal([]string{
			"GITHUB_APP_ID=123456",
			"GITHUB_APP_PEM=***",
			"GITHUB_APP_INSTALLATION_ID=98765432",
			"ANTHROPIC_BASE_URL=http://127.0.0.1:8788",
			"ANTHROPIC_AUTH_TOKEN=***",
		}))
		Expect(redacted).NotTo(ContainElement(ContainSubstring("sentinel")))
	})
})
