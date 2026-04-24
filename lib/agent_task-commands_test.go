// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"encoding/json"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
)

var _ = Describe("Command operations", func() {
	It("IncrementFrontmatterCommandOperation has expected string value", func() {
		Expect(
			lib.IncrementFrontmatterCommandOperation,
		).To(Equal(base.CommandOperation("increment-frontmatter")))
	})

	It("UpdateFrontmatterCommandOperation has expected string value", func() {
		Expect(
			lib.UpdateFrontmatterCommandOperation,
		).To(Equal(base.CommandOperation("update-frontmatter")))
	})
})

var _ = Describe("CommandOperation validation", func() {
	var ctx context.Context
	BeforeEach(func() {
		ctx = context.Background()
	})

	// IMPORTANT: this table is the single source of truth for "every CommandOperation
	// constant declared in lib/". When you add a new lib.*CommandOperation constant,
	// you MUST add a matching Entry here. If you forget, this suite will not catch it —
	// so reviewers must enforce the rule. The comment above the constants in
	// agent_task-commands.go reminds contributors of this.
	DescribeTable("all lib CommandOperation constants pass base.CommandOperation.Validate",
		func(op base.CommandOperation) {
			Expect(op.Validate(ctx)).To(Succeed())
		},
		Entry("IncrementFrontmatterCommandOperation", lib.IncrementFrontmatterCommandOperation),
		Entry("UpdateFrontmatterCommandOperation", lib.UpdateFrontmatterCommandOperation),
	)
})

var _ = Describe("IncrementFrontmatterCommand", func() {
	It("round-trips through JSON", func() {
		cmd := lib.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-abc"),
			Field:          "trigger_count",
			Delta:          1,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got lib.IncrementFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Field).To(Equal(cmd.Field))
		Expect(got.Delta).To(Equal(cmd.Delta))
	})

	It("handles zero delta", func() {
		cmd := lib.IncrementFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-xyz"),
			Field:          "retry_count",
			Delta:          0,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got lib.IncrementFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Delta).To(Equal(0))
	})
})

var _ = Describe("UpdateFrontmatterCommand", func() {
	It("round-trips through JSON with two-key updates map", func() {
		cmd := lib.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-def"),
			Updates: lib.TaskFrontmatter{
				"status": "completed",
				"phase":  "done",
			},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got lib.UpdateFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Updates).To(HaveLen(2))
		Expect(got.Updates["status"]).To(Equal("completed"))
		Expect(got.Updates["phase"]).To(Equal("done"))
	})

	It("handles nil updates map", func() {
		cmd := lib.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-nil"),
			Updates:        nil,
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got lib.UpdateFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Updates).To(BeNil())
	})
})
