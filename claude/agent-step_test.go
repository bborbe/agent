// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bborbe/collection"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/claude"
	libmocks "github.com/bborbe/agent/mocks"
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
						{
							Heading: "## Analysis",
							Body:    `{"status":"done","message":"analysis complete"}`,
						},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeFalse())
			})

			It("success body → false", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{
							Heading: "## Analysis",
							Body:    `{"status":"done","message":"analysis complete","next_phase":"done"}`,
						},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeFalse())
			})
		})

		Context("when a ## Failure section is present", func() {
			It("returns true", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{
							Heading: "## Analysis",
							Body:    `{"status":"done","message":"analysis complete"}`,
						},
						{Heading: "## Failure", Body: "- **Reason:** job failed"},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeTrue())
			})
		})

		Context("when the output section body is a needs_input AgentResult", func() {
			It("returns true", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{
							Heading: "## Analysis",
							Body:    `{"status":"needs_input","message":"permission denied"}`,
						},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeTrue())
			})
		})

		Context("when the output section body is a failed AgentResult", func() {
			It("returns true", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{
							Heading: "## Analysis",
							Body:    `{"status":"failed","message":"claude CLI crashed"}`,
						},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeTrue())
			})
		})

		Context("when the output section body is unparseable prose", func() {
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

		Context("when the output section body has an unknown status", func() {
			It("returns false", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{
							Heading: "## Analysis",
							Body:    `{"status":"in_progress","message":"working"}`,
						},
					},
				}
				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeFalse())
			})
		})

		Context("when the output section body is invalid JSON with balanced braces", func() {
			It("returns false", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{Heading: "## Analysis", Body: `{"status": }`},
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

			It("does not write the output section", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				_, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeFalse())
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

		Context("when the runner returns an envelope carrying an output payload", func() {
			// The wire shape a planning phase actually emits: the payload is
			// the envelope's `output` value, newlines escaped as on the wire.
			const planPayload = "## Plan\n\n```json\n{\"steps\":[{\"id\":\"s1\"}]}\n```\n"

			BeforeEach(func() {
				envelope, err := json.Marshal(map[string]string{
					"status":     "done",
					"next_phase": "in_progress",
					"message":    "plan extracted",
					"output":     planPayload,
				})
				Expect(err).NotTo(HaveOccurred())
				mockRunner.RunReturns(&claude.ClaudeResult{Result: string(envelope)}, nil)
			})

			It("writes the extracted payload, not the raw envelope", func() {
				md := &lib.Markdown{
					Sections: []lib.Section{
						{Heading: "## Plan", Body: "old plan"},
					},
				}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status).To(Equal(lib.AgentStatusDone))

				section, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeTrue())
				Expect(section.Body).To(Equal(planPayload))
				Expect(section.Body).NotTo(ContainSubstring(`"status"`))
			})

			It("round-trips through ShouldRun as a genuine success", func() {
				md := &lib.Markdown{}
				_, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())

				shouldRun, err := agentStep.ShouldRun(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(shouldRun).To(BeFalse())
			})
		})

		Context("when the runner result is not an envelope", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result: "plain prose, no JSON envelope here",
				}, nil)
			})

			It("writes the runner's raw text into the section", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status).To(Equal(lib.AgentStatusDone))

				section, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeTrue())
				Expect(section.Body).To(Equal("plain prose, no JSON envelope here"))
			})
		})

		Context("when runner returns a needs_input body", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result: `{"status":"needs_input","message":"permission denied"}`,
				}, nil)
			})

			It("returns Result with NeedsInput status and does not write the section", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
				Expect(result.Message).To(ContainSubstring("permission denied"))
				_, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeFalse())
			})
		})

		Context("when runner returns a failed body", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result: `{"status":"failed","message":"claude CLI crashed"}`,
				}, nil)
			})

			It("returns Result with Failed status and does not write the section", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Status).To(Equal(lib.AgentStatusFailed))
				Expect(result.Message).To(ContainSubstring("claude CLI crashed"))
				_, exists := md.FindSection("## Analysis")
				Expect(exists).To(BeFalse())
			})
		})

		Context("when the runner reports observed counts", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result:           `{"status":"done","message":"analysis complete"}`,
					NumTurns:         7,
					InteractionCount: collection.Ptr(int64(0)),
				}, nil)
			})

			It(
				"carries the turn total and the evidenced interaction count on a done result",
				func() {
					md := &lib.Markdown{}
					result, err := agentStep.Run(ctx, md)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.AgentTurns).NotTo(BeNil())
					Expect(*result.AgentTurns).To(Equal(int64(7)))
					Expect(result.InteractionCount).NotTo(BeNil())
					Expect(*result.InteractionCount).To(Equal(int64(0)))
				},
			)
		})

		Context("when the runner returns an agent-reported failed body with counts", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result:           `{"status":"failed","message":"claude CLI crashed"}`,
					NumTurns:         5,
					InteractionCount: collection.Ptr(int64(2)),
				}, nil)
			})

			It("carries both counts on an agent-reported failed body", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status).To(Equal(lib.AgentStatusFailed))
				Expect(result.AgentTurns).NotTo(BeNil())
				Expect(*result.AgentTurns).To(Equal(int64(5)))
				Expect(result.InteractionCount).NotTo(BeNil())
				Expect(*result.InteractionCount).To(Equal(int64(2)))
			})
		})

		Context("when the runner summary reported no counts", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result:           `{"status":"done","message":"analysis complete"}`,
					NumTurns:         0,
					InteractionCount: nil,
				}, nil)
			})

			It("omits the turn count when the summary reported none", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.AgentTurns).To(BeNil())
				Expect(result.InteractionCount).To(BeNil())
			})
		})

		Context("when the runner summary reported a negative turn total", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result:   `{"status":"done","message":"analysis complete"}`,
					NumTurns: -3,
				}, nil)
			})

			It("treats a negative turn total as no measurement", func() {
				md := &lib.Markdown{}
				result, err := agentStep.Run(ctx, md)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.AgentTurns).To(BeNil())
			})
		})

		Context("when runner returns a needs_input body without a message", func() {
			BeforeEach(func() {
				mockRunner.RunReturns(&claude.ClaudeResult{
					Result: `{"status":"needs_input"}`,
				}, nil)
			})

			It(
				"returns NeedsInput status with a fallback message and does not write the section",
				func() {
					md := &lib.Markdown{}
					result, err := agentStep.Run(ctx, md)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).NotTo(BeNil())
					Expect(result.Status).To(Equal(lib.AgentStatusNeedsInput))
					Expect(result.Message).NotTo(BeEmpty())
					_, exists := md.FindSection("## Analysis")
					Expect(exists).To(BeFalse())
				},
			)
		})
	})
})
