// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/claude/pkg/factory"
	agentlib "github.com/bborbe/agent/lib"
	claudelib "github.com/bborbe/agent/lib/claude"
)

var _ = Describe("CreateAgentForTaskType", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns a non-nil agent for TaskTypeClaude", func() {
		agent, err := factory.CreateAgentForTaskType(
			ctx,
			agentlib.TaskTypeClaude,
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir(""),
			nil,
			claudelib.ClaudeModel(""),
			nil,
			nil,
		)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("returns a non-nil agent for TaskTypeHealthcheck", func() {
		agent, err := factory.CreateAgentForTaskType(
			ctx,
			agentlib.TaskTypeHealthcheck,
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir(""),
			nil,
			claudelib.ClaudeModel(""),
			nil,
			nil,
		)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("returns a non-nil agent for TaskTypeOAuthProbe (alias to healthcheck)", func() {
		agent, err := factory.CreateAgentForTaskType(
			ctx,
			agentlib.TaskTypeOAuthProbe,
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir(""),
			nil,
			claudelib.ClaudeModel(""),
			nil,
			nil,
		)
		Expect(err).To(BeNil())
		Expect(agent).NotTo(BeNil())
	})

	It("returns nil agent and error for an unsupported task type", func() {
		agent, err := factory.CreateAgentForTaskType(
			ctx,
			agentlib.TaskType("bogus"),
			claudelib.ClaudeConfigDir(""),
			claudelib.AgentDir(""),
			nil,
			claudelib.ClaudeModel(""),
			nil,
			nil,
		)
		Expect(err).NotTo(BeNil())
		Expect(agent).To(BeNil())
		Expect(err.Error()).To(ContainSubstring("unknown task_type"))
		Expect(err.Error()).To(ContainSubstring("bogus"))
	})
})
