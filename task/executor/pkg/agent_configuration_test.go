// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkg_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/task/executor/pkg"
)

func TestPkg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pkg Suite")
}

var _ = Describe("AgentConfigurations", func() {
	var configs pkg.AgentConfigurations

	BeforeEach(func() {
		configs = pkg.AgentConfigurations{
			{
				Assignee: "claude",
				Image:    "registry/agent-claude",
				Env:      map[string]string{},
			},
			{
				Assignee: "backtest-agent",
				Image:    "registry/agent-backtest",
				Env:      map[string]string{"GEMINI_API_KEY": "test-key"},
			},
		}
	})

	Describe("FindByAssignee", func() {
		It("returns config when found", func() {
			config, ok := configs.FindByAssignee("claude")
			Expect(ok).To(BeTrue())
			Expect(config.Assignee).To(Equal("claude"))
			Expect(config.Image).To(Equal("registry/agent-claude"))
		})

		It("returns backtest config when found", func() {
			config, ok := configs.FindByAssignee("backtest-agent")
			Expect(ok).To(BeTrue())
			Expect(config.Assignee).To(Equal("backtest-agent"))
			Expect(config.Env["GEMINI_API_KEY"]).To(Equal("test-key"))
		})

		It("returns false when not found", func() {
			_, ok := configs.FindByAssignee("unknown-agent")
			Expect(ok).To(BeFalse())
		})

		It("returns zero value config when not found", func() {
			config, ok := configs.FindByAssignee("unknown-agent")
			Expect(ok).To(BeFalse())
			Expect(config.Assignee).To(Equal(""))
			Expect(config.Image).To(Equal(""))
		})
	})

	Describe("TaggedConfigurations", func() {
		It("appends branch as tag to all images", func() {
			tagged := configs.TaggedConfigurations("dev")
			Expect(tagged[0].Image).To(Equal("registry/agent-claude:dev"))
			Expect(tagged[1].Image).To(Equal("registry/agent-backtest:dev"))
		})

		It("preserves assignee", func() {
			tagged := configs.TaggedConfigurations("prod")
			Expect(tagged[0].Assignee).To(Equal("claude"))
			Expect(tagged[1].Assignee).To(Equal("backtest-agent"))
		})

		It("preserves env", func() {
			tagged := configs.TaggedConfigurations("prod")
			Expect(tagged[1].Env["GEMINI_API_KEY"]).To(Equal("test-key"))
		})

		It("returns same length as input", func() {
			tagged := configs.TaggedConfigurations("dev")
			Expect(tagged).To(HaveLen(len(configs)))
		})
	})
})
