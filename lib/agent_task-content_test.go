// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("TaskContent", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("String", func() {
		It("returns string value", func() {
			c := lib.TaskContent("some content")
			Expect(c.String()).To(Equal("some content"))
		})
	})

	Describe("Validate", func() {
		It("returns nil for non-empty content", func() {
			c := lib.TaskContent("some content")
			Expect(c.Validate(ctx)).To(BeNil())
		})

		It("returns error for empty content", func() {
			c := lib.TaskContent("")
			err := c.Validate(ctx)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("content"))
		})
	})
})
