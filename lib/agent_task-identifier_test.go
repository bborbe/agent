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

var _ = Describe("TaskIdentifier", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("String", func() {
		It("returns the string value", func() {
			id := lib.TaskIdentifier("abc-123")
			Expect(id.String()).To(Equal("abc-123"))
		})
	})

	Describe("Bytes", func() {
		It("returns byte slice", func() {
			id := lib.TaskIdentifier("abc-123")
			Expect(id.Bytes()).To(Equal([]byte("abc-123")))
		})
	})

	Describe("Contains", func() {
		It("returns true when identifier is in slice", func() {
			ids := lib.TaskIdentifiers{"abc-123", "def-456"}
			Expect(ids.Contains(lib.TaskIdentifier("abc-123"))).To(BeTrue())
		})

		It("returns false when identifier is not in slice", func() {
			ids := lib.TaskIdentifiers{"abc-123"}
			Expect(ids.Contains(lib.TaskIdentifier("missing"))).To(BeFalse())
		})
	})

	Describe("Ptr", func() {
		It("returns pointer to identifier", func() {
			id := lib.TaskIdentifier("abc-123")
			ptr := id.Ptr()
			Expect(*ptr).To(Equal(id))
		})
	})

	Describe("Equal", func() {
		It("returns true for matching identifier", func() {
			id := lib.TaskIdentifier("abc-123")
			Expect(id.Equal(lib.TaskIdentifier("abc-123"))).To(BeTrue())
		})

		It("returns false for non-matching identifier", func() {
			id := lib.TaskIdentifier("abc-123")
			Expect(id.Equal(lib.TaskIdentifier("def-456"))).To(BeFalse())
		})
	})

	Describe("Validate", func() {
		It("returns nil for non-empty identifier", func() {
			id := lib.TaskIdentifier("abc-123")
			Expect(id.Validate(ctx)).To(BeNil())
		})

		It("returns error for empty identifier", func() {
			id := lib.TaskIdentifier("")
			err := id.Validate(ctx)
			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(ContainSubstring("identifier"))
		})
	})
})
