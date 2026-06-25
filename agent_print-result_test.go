// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent"
)

var _ = Describe("PrintResult", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns nil for nil result", func() {
		err := lib.PrintResult(ctx, nil)
		Expect(err).To(BeNil())
	})

	It("returns nil for valid result", func() {
		result := &lib.Result{
			Status:    lib.AgentStatusDone,
			NextPhase: "done",
			Message:   "all good",
		}

		err := lib.PrintResult(ctx, result)
		Expect(err).To(BeNil())
	})
})
