// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package parser_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/agent/gemini/pkg/parser"
)

var _ = Describe("New", func() {
	Context("with empty api key", func() {
		It("returns error wrapped with context", func() {
			ctx := context.Background()
			_, err := parser.New(ctx, "", "gemini-2.0-flash")
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("create genai client failed"))
		})
	})

	Context("with cancelled context", func() {
		It("returns without error (context stored for later API calls)", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			// genai.NewClient does not fail immediately on cancelled context;
			// it stores the context for use during actual API operations (Parse).
			// The context is correctly propagated, which is what this change achieves.
			p, err := parser.New(ctx, "test-api-key", "gemini-2.0-flash")
			Expect(err).To(BeNil())
			Expect(p).NotTo(BeNil())
		})
	})
})

var _ = Describe("NewWithClient", func() {
	It("returns parser with the given client and model", func() {
		// NewWithClient is used for testing with mock clients
		p := parser.NewWithClient(nil, "test-model")
		Expect(p).NotTo(BeNil())
	})
})
