// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent/lib"
)

var _ = Describe("TaskType", func() {
	var (
		ctx context.Context
	)
	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Validate", func() {
		DescribeTable(
			"valid values",
			func(value lib.TaskType) {
				Expect(value.Validate(ctx)).To(Succeed())
			},
			Entry("llm constant", lib.TaskTypeLLM),
			Entry("pr-review constant", lib.TaskTypePRReview),
			Entry("backtest constant", lib.TaskTypeBacktest),
			Entry("hypothesis constant", lib.TaskTypeHypothesis),
			Entry("trade-analysis constant", lib.TaskTypeTradeAnalysis),
			Entry("oauth-probe constant", lib.TaskTypeOAuthProbe),
			Entry("healthcheck constant", lib.TaskTypeHealthcheck),
			Entry(
				"63-character value",
				lib.TaskType("a23456789012345678901234567890123456789012345678901234567890abc"),
			),
		)

		DescribeTable(
			"invalid values",
			func(value lib.TaskType) {
				Expect(value.Validate(ctx)).NotTo(Succeed())
			},
			Entry("empty string", lib.TaskType("")),
			Entry("uppercase letter", lib.TaskType("MyType")),
			Entry("underscore", lib.TaskType("my_type")),
			Entry(
				"64-character value",
				lib.TaskType("a234567890123456789012345678901234567890123456789012345678901234"),
			),
		)
	})

	Describe("String", func() {
		It("returns the underlying string", func() {
			Expect(lib.TaskTypeLLM.String()).To(Equal("llm"))
		})
	})

	Describe("Bytes", func() {
		It("returns the underlying bytes", func() {
			Expect(lib.TaskTypeLLM.Bytes()).To(Equal([]byte("llm")))
		})
	})

	Describe("Ptr", func() {
		It("returns a non-nil pointer to the value", func() {
			tt := lib.TaskTypeLLM
			Expect(lib.TaskTypeLLM.Ptr()).To(Equal(&tt))
		})
	})
})
