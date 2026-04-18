// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package delivery_test

import (
	"context"
	"io"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib/delivery"
)

var _ = Describe("PrintResult", func() {
	var (
		ctx    context.Context
		result delivery.AgentResultInfo
		err    error
	)

	BeforeEach(func() {
		ctx = context.Background()
		result = delivery.AgentResultInfo{
			Status: delivery.AgentStatusDone,
			Output: "task complete",
		}
	})

	JustBeforeEach(func() {
		// Redirect stdout to discard output during tests.
		old := os.Stdout
		r, w, pipeErr := os.Pipe()
		Expect(pipeErr).To(BeNil())
		os.Stdout = w

		err = delivery.PrintResult(ctx, result)

		_ = w.Close()
		_, _ = io.ReadAll(r)
		os.Stdout = old
	})

	It("returns no error for a valid result", func() {
		Expect(err).To(BeNil())
	})
})
