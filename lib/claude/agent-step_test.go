// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/claude"
	libmocks "github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("AgentStep", func() {
	var (
		ctx        context.Context
		mockRunner *libmocks.ClaudeRunner
		step       claude.AgentStepConfig
		agentStep  lib.Step
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockRunner = &libmocks.ClaudeRunner{}
	})

	Describe("NewAgentStep", func() {
		It("creates a step with the given config", func() {
			step = claude.AgentStepConfig{
				Name:          "test-step",
				Runner:        mockRunner,
				Instructions:  claude.Instructions{{Name: "system", Content: "You are helpful."}},
				EnvContext:    map[string]string{"FOO": "bar"},
				OutputSection: "## Analysis",
				NextPhase:     "done",
			}
			agentStep = claude.NewAgentStep(step)
			Expect(agentStep).NotTo(BeNil())
		})
	})

	Describe("Name", func() {
		BeforeEach(func() {
			step = claude.AgentStepConfig{
				Name:          "my-agent-step",
				Runner:        mockRunner,
				Instructions:  claude.Instructions{{Name: "system", Content: "You are helpful."}},
				OutputSection: "## Analysis",
			}
			agentStep = claude.NewAgentStep(step)
		})

		It("returns the step name", func() {
			Expect(agentStep.Name()).To(Equal("my-agent-step"))
		})
	})

	Describe("ShouldRun", func() {
		BeforeEach(func() {
			step = claude.AgentStepConfig{
				Name:          "test-step",
				Runner:        mockRunner,
				Instructions:  claude.Instructions{{Name: "system", Content: "You are helpful."}},
				OutputSection: "## Analysis",
			}
			agentStep = claude.NewAgentStep(step)
		})

		Context("when section does not exist", func() {
			It("returns true", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{Heading: "## Plan", Body: "some content"},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeTrue())
			})
		})

		Context("when section already exists", func() {
			It("returns false", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{Heading: "## Analysis", Body: "already done"},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeFalse())
			})
		})
	})

	Describe("Run", func() {
		BeforeEach(func() {
			step = claude.AgentStepConfig{
				Name:          "test-step",
				Runner:        mockRunner,
				Instructions:  claude.Instructions{{Name: "system", Content: "You are helpful."}},
				EnvContext:    map[string]string{"KEY": "value"},
				OutputSection: "## Analysis",
				NextPhase:     "done",
			}
			agentStep = claude.NewAgentStep(step)
		})

		Context("when runner returns error", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(nil, errors.New("claude CLI crashed"))
			})

			It("returns Result with Failed status and no error", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Status).To(Equal(lib.AgentStatusFailed))
				Expect(result.Message).To(ContainSubstring("claude CLI crashed"))
			})
		})

		Context("when runner succeeds", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result: `{"status":"done","message":"analysis complete"}`,
				}, nil)
			})

			It("returns Result with Done status and replaces section", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{Heading: "## Plan", Body: "some plan"},
					},
				}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Status).To(Equal(lib.AgentStatusDone))
				Expect(result.NextPhase).To(Equal("done"))

				// Verify section was replaced
				section, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeTrue())
				Expect(section.Body).To(Equal(`{"status":"done","message":"analysis complete"}`))
			})
		})
	})
})
