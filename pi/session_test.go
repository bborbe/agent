// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pi_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/mocks"
	"github.com/bborbe/agent/pi"
)

var _ = Describe("piSessionFactory", func() {
	var (
		ctx      context.Context
		base     pi.PiRunnerConfig
		captured []pi.PiRunnerConfig
		runner   *mocks.PiRunner
		factory  agentlib.SessionFactory
	)

	BeforeEach(func() {
		ctx = context.Background()
		base = pi.PiRunnerConfig{
			AgentDir:     "/agent",
			AllowedTools: "read,write",
			Model:        "MiniMax-M2.7-highspeed",
			Env:          map[string]string{"MINIMAX_API_KEY": "test-key"},
		}
		captured = nil
		runner = &mocks.PiRunner{}
		runner.RunReturns(&pi.Result{Result: "the answer"}, nil)
		factory = pi.NewSessionFactory(base, func(config pi.PiRunnerConfig) pi.Runner {
			captured = append(captured, config)
			return runner
		})
	})

	It("session path persists", func() {
		session := factory.Create("sess-1")

		Expect(session).NotTo(BeNil())
		Expect(captured).To(HaveLen(1))
		Expect(captured[0].PersistSession).To(BeTrue())
		Expect(captured[0].SessionID).To(Equal("sess-1"))
		Expect(captured[0].AgentDir).To(Equal("/agent"))
		Expect(captured[0].AllowedTools).To(Equal("read,write"))
		Expect(captured[0].Model).To(Equal("MiniMax-M2.7-highspeed"))
		Expect(captured[0].Env).To(Equal(base.Env))
	})

	It("task path does not", func() {
		_ = factory.Create("")

		Expect(captured).To(HaveLen(1))
		Expect(captured[0].PersistSession).To(BeFalse())
		Expect(captured[0].SessionID).To(BeEmpty())
	})

	It("returns the runner's answer from Prompt", func() {
		session := factory.Create("sess-1")

		result, err := session.Prompt(ctx, "hello")

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("the answer"))
		Expect(runner.RunCallCount()).To(Equal(1))
		_, prompt := runner.RunArgsForCall(0)
		Expect(prompt).To(Equal("hello"))
	})

	It("wraps a runner failure", func() {
		runner.RunReturns(nil, errors.New("pi exploded"))
		session := factory.Create("sess-1")

		result, err := session.Prompt(ctx, "hello")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("pi exploded"))
		Expect(result).To(BeEmpty())
	})

	It("closes without error", func() {
		session := factory.Create("sess-1")

		Expect(session.Close(ctx)).To(Succeed())
	})
})
