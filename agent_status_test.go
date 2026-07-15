// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
)

var _ = Describe("AgentStatus", func() {
	Describe("string values", func() {
		It("AgentStatusDone has correct value", func() {
			Expect(string(lib.AgentStatusDone)).To(Equal("done"))
		})

		It("AgentStatusInProgress has correct value", func() {
			Expect(string(lib.AgentStatusInProgress)).To(Equal("in_progress"))
		})

		It("AgentStatusFailed has correct value", func() {
			Expect(string(lib.AgentStatusFailed)).To(Equal("failed"))
		})

		It("AgentStatusNeedsInput has correct value", func() {
			Expect(string(lib.AgentStatusNeedsInput)).To(Equal("needs_input"))
		})
	})
})
