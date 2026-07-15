// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
)

var _ = Describe("TaskAssignee", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("String", func() {
		It("returns the string value", func() {
			a := lib.TaskAssignee("my-agent")
			Expect(a.String()).To(Equal("my-agent"))
		})
	})

	Describe("Validate", func() {
		It("returns nil for non-empty assignee", func() {
			a := lib.TaskAssignee("my-agent")
			Expect(a.Validate(ctx)).To(BeNil())
		})

		It("returns error for empty assignee", func() {
			a := lib.TaskAssignee("")
			err := a.Validate(ctx)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("assignee"))
		})
	})
})
