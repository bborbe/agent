// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/mocks"
)

var _ = Describe("Phase", func() {
	Describe("NewPhase", func() {
		It("constructs phase with name and steps", func() {
			step := &mocks.AgentStep{}
			step.NameReturns("test-step")

			phase := lib.NewPhase(domain.TaskPhase("planning"), step)

			Expect(phase.Name).To(Equal(domain.TaskPhase("planning")))
			Expect(len(phase.Steps)).To(Equal(1))
			Expect(phase.Steps[0].Name()).To(Equal("test-step"))
		})

		It("constructs phase with no steps", func() {
			phase := lib.NewPhase(domain.TaskPhase("done"))
			Expect(phase.Name).To(Equal(domain.TaskPhase("done")))
			Expect(phase.Steps).To(BeEmpty())
		})
	})
})
