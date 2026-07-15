// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package healthcheck_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/healthcheck"
)

var _ = Describe("NewNopStep", func() {
	var (
		ctx  context.Context
		step agentlib.Step
	)

	BeforeEach(func() {
		ctx = context.Background()
		step = healthcheck.NewNopStep()
	})

	Describe("Name", func() {
		It("returns healthcheck-nop", func() {
			Expect(step.Name()).To(Equal("healthcheck-nop"))
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
		It("returns done with message ok", func() {
			result, err := step.Run(ctx, nil)
			Expect(err).To(BeNil())
			Expect(result).NotTo(BeNil())
			Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
			Expect(result.Message).To(Equal("ok"))
		})
	})
})
