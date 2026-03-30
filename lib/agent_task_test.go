// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"time"

	"github.com/bborbe/cqrs/base"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("Task", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	validObject := func() base.Object[base.Identifier] {
		now := libtime.DateTime(time.Now())
		return base.Object[base.Identifier]{
			Identifier: base.Identifier("test-id"),
			Created:    now,
			Modified:   now,
		}
	}

	Describe("Validate", func() {
		It("returns nil for valid Task", func() {
			task := lib.Task{
				Object:         validObject(),
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).To(BeNil())
		})

		It("returns error when TaskIdentifier is empty", func() {
			task := lib.Task{
				Object:         validObject(),
				TaskIdentifier: lib.TaskIdentifier(""),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})

		It("returns error when Content is empty", func() {
			task := lib.Task{
				Object:         validObject(),
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent(""),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})

		It("returns error when Object Identifier is empty", func() {
			now := libtime.DateTime(time.Now())
			task := lib.Task{
				Object: base.Object[base.Identifier]{
					Identifier: base.Identifier(""),
					Created:    now,
					Modified:   now,
				},
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})

		It("returns error when Object Created is zero", func() {
			now := libtime.DateTime(time.Now())
			task := lib.Task{
				Object: base.Object[base.Identifier]{
					Identifier: base.Identifier("test-id"),
					Created:    libtime.DateTime{},
					Modified:   now,
				},
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})
	})
})

var _ = Describe("TaskContent", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Validate", func() {
		It("returns nil for non-empty content", func() {
			Expect(lib.TaskContent("hello").Validate(ctx)).To(BeNil())
		})

		It("returns error for empty content", func() {
			Expect(lib.TaskContent("").Validate(ctx)).NotTo(BeNil())
		})
	})
})

var _ = Describe("TaskFrontmatter", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	_ = ctx

	Describe("Phase", func() {
		It("returns nil when phase key is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.Phase()).To(BeNil())
		})

		It("returns nil when phase value is empty string", func() {
			fm := lib.TaskFrontmatter{"phase": ""}
			Expect(fm.Phase()).To(BeNil())
		})

		It("returns pointer to phase when phase is set", func() {
			fm := lib.TaskFrontmatter{"phase": "in-progress"}
			result := fm.Phase()
			Expect(result).NotTo(BeNil())
			Expect(result.String()).To(Equal("in-progress"))
		})

		It("returns nil when phase value is not a string", func() {
			fm := lib.TaskFrontmatter{"phase": 42}
			Expect(fm.Phase()).To(BeNil())
		})
	})
})
