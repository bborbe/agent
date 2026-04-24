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

var _ = Describe("Task", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Validate", func() {
		It("returns nil for valid Task", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).To(BeNil())
		})

		It("returns error when TaskIdentifier is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier(""),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})

		It("returns error when Content is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent(""),
			}
			Expect(task.Validate(ctx)).NotTo(BeNil())
		})

		It("returns nil when Object is empty", func() {
			task := lib.Task{
				TaskIdentifier: lib.TaskIdentifier("task-uuid-123"),
				Content:        lib.TaskContent("some content"),
			}
			Expect(task.Validate(ctx)).To(BeNil())
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

	Describe("Stage", func() {
		It("returns prod when stage key is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.Stage()).To(Equal("prod"))
		})

		It("returns prod when stage value is empty string", func() {
			fm := lib.TaskFrontmatter{"stage": ""}
			Expect(fm.Stage()).To(Equal("prod"))
		})

		It("returns the stage value when stage is set", func() {
			fm := lib.TaskFrontmatter{"stage": "dev"}
			Expect(fm.Stage()).To(Equal("dev"))
		})
	})

	Describe("RetryCount", func() {
		It("returns 0 when key is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.RetryCount()).To(Equal(0))
		})

		It("returns value when key is int (YAML path)", func() {
			fm := lib.TaskFrontmatter{"retry_count": int(2)}
			Expect(fm.RetryCount()).To(Equal(2))
		})

		It("returns value when key is float64 (JSON/Kafka path)", func() {
			fm := lib.TaskFrontmatter{"retry_count": float64(3)}
			Expect(fm.RetryCount()).To(Equal(3))
		})

		It("returns 0 when key is explicitly 0", func() {
			fm := lib.TaskFrontmatter{"retry_count": int(0)}
			Expect(fm.RetryCount()).To(Equal(0))
		})
	})

	Describe("MaxRetries", func() {
		It("returns 3 when key is absent (spec default)", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.MaxRetries()).To(Equal(3))
		})

		It("returns value when key is int 5", func() {
			fm := lib.TaskFrontmatter{"max_retries": int(5)}
			Expect(fm.MaxRetries()).To(Equal(5))
		})

		It("returns value when key is float64 10.0", func() {
			fm := lib.TaskFrontmatter{"max_retries": float64(10)}
			Expect(fm.MaxRetries()).To(Equal(10))
		})

		It(
			"returns 0 when key is explicitly 0 (max_retries: 0 escalates on first failure)",
			func() {
				fm := lib.TaskFrontmatter{"max_retries": int(0)}
				Expect(fm.MaxRetries()).To(Equal(0))
			},
		)
	})

	Describe("TriggerCount", func() {
		It("returns 0 when field is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.TriggerCount()).To(Equal(0))
		})
		It("returns the value when set as int", func() {
			fm := lib.TaskFrontmatter{"trigger_count": 2}
			Expect(fm.TriggerCount()).To(Equal(2))
		})
		It("returns the value when set as float64 (JSON default)", func() {
			fm := lib.TaskFrontmatter{"trigger_count": float64(5)}
			Expect(fm.TriggerCount()).To(Equal(5))
		})
	})

	Describe("MaxTriggers", func() {
		It("returns 3 when field is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.MaxTriggers()).To(Equal(3))
		})
		It("returns the value when set as int", func() {
			fm := lib.TaskFrontmatter{"max_triggers": 10}
			Expect(fm.MaxTriggers()).To(Equal(10))
		})
		It("returns the value when set as float64", func() {
			fm := lib.TaskFrontmatter{"max_triggers": float64(7)}
			Expect(fm.MaxTriggers()).To(Equal(7))
		})
	})

	Describe("SpawnNotification", func() {
		It("returns false when key is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.SpawnNotification()).To(BeFalse())
		})

		It("returns true when key is bool true", func() {
			fm := lib.TaskFrontmatter{"spawn_notification": bool(true)}
			Expect(fm.SpawnNotification()).To(BeTrue())
		})

		It("returns false when key is bool false", func() {
			fm := lib.TaskFrontmatter{"spawn_notification": bool(false)}
			Expect(fm.SpawnNotification()).To(BeFalse())
		})
	})

	Describe("CurrentJob", func() {
		It("returns empty string when key is absent", func() {
			fm := lib.TaskFrontmatter{}
			Expect(fm.CurrentJob()).To(Equal(""))
		})

		It("returns the job name when key is a non-empty string", func() {
			fm := lib.TaskFrontmatter{"current_job": "claude-20260418120000"}
			Expect(fm.CurrentJob()).To(Equal("claude-20260418120000"))
		})

		It("returns empty string when key is empty string", func() {
			fm := lib.TaskFrontmatter{"current_job": ""}
			Expect(fm.CurrentJob()).To(Equal(""))
		})
	})
})
