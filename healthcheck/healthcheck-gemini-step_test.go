// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck_test

import (
	"context"
	"encoding/json"
	stderrors "errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/healthcheck"
	"github.com/bborbe/agent/mocks"
)

var _ = Describe("NewGeminiStep", func() {
	var (
		ctx        context.Context
		fakeParser *mocks.AgentAIParser
		step       agentlib.Step
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeParser = &mocks.AgentAIParser{}
		step = healthcheck.NewGeminiStep(fakeParser)
	})

	Describe("Name", func() {
		It("returns healthcheck-gemini", func() {
			Expect(step.Name()).To(Equal("healthcheck-gemini"))
		})
	})

	Describe("ShouldRun", func() {
		It("always returns true", func() {
			ok, err := step.ShouldRun(ctx, nil)
			Expect(err).To(BeNil())
			Expect(ok).To(BeTrue())
		})
	})

	Describe("Run", func() {
		It("returns done when the parser populates a non-empty reply", func() {
			fakeParser.ParseStub = func(_ context.Context, _ string, target any) error {
				return json.Unmarshal([]byte(`{"ok":"pong"}`), target)
			}
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.NextPhase).To(Equal("done"),
				"healthcheck must request explicit terminal phase — empty NextPhase is an in-place save")
			Expect(result.Message).To(Equal("pong"))
		})

		It("returns failed when the parser returns an error", func() {
			fakeParser.ParseReturns(stderrors.New("gemini unavailable"))
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(result.Message).To(ContainSubstring("healthcheck-gemini parse failed"))
		})

		It("returns failed when the parser populates an empty OK field", func() {
			fakeParser.ParseStub = func(_ context.Context, _ string, target any) error {
				return json.Unmarshal([]byte(`{"ok":""}`), target)
			}
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(result.Message).To(ContainSubstring("gemini healthcheck reply empty"))
		})
	})
})
