// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"encoding/json"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lib "github.com/bborbe/agent"
	"github.com/bborbe/agent/command/task"
)

var _ = Describe("IncrementFrontmatterCommandOperation", func() {
	It("has expected string value", func() {
		Expect(
			task.IncrementFrontmatterCommandOperation,
		).To(Equal(base.CommandOperation("increment-frontmatter")))
	})
})

var _ = Describe("IncrementFrontmatterCommand", func() {
	It("round-trips through JSON", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-abc"),
			Field:          "trigger_count",
			Delta:          1,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got task.IncrementFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Field).To(Equal(cmd.Field))
		Expect(got.Delta).To(Equal(cmd.Delta))
	})

	It("handles zero delta", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-xyz"),
			Field:          "retry_count",
			Delta:          0,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got task.IncrementFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Delta).To(Equal(0))
	})

	It("handles negative delta", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-neg"),
			Field:          "counter",
			Delta:          -1,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got task.IncrementFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Delta).To(Equal(-1))
	})

	It("JSON contains taskIdentifier, field, delta keys", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("t"),
			Field:          "f",
			Delta:          1,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		jsonStr := string(data)
		Expect(jsonStr).To(ContainSubstring(`"taskIdentifier"`))
		Expect(jsonStr).To(ContainSubstring(`"field"`))
		Expect(jsonStr).To(ContainSubstring(`"delta"`))
	})
})

var _ = Describe("IncrementFrontmatterCommand.Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("valid: TaskIdentifier non-empty + Field non-empty + Delta 0 succeeds", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "counter",
			Delta:          0,
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("valid: negative Delta succeeds", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "counter",
			Delta:          -5,
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("valid: positive Delta succeeds", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "counter",
			Delta:          10,
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("error: empty TaskIdentifier", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier(""),
			Field:          "counter",
			Delta:          1,
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("error: empty Field", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Field:          "",
			Delta:          1,
		}
		err := cmd.Validate(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Or(ContainSubstring("Field"), ContainSubstring("empty")))
	})

	It("error: empty TaskIdentifier AND empty Field", func() {
		cmd := task.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier(""),
			Field:          "",
			Delta:          1,
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})
})
