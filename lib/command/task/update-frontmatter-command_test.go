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

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/command/task"
)

var _ = Describe("UpdateFrontmatterCommandOperation", func() {
	It("has expected string value", func() {
		Expect(
			task.UpdateFrontmatterCommandOperation,
		).To(Equal(base.CommandOperation("update-frontmatter")))
	})
})

var _ = Describe("UpdateFrontmatterCommand", func() {
	It("round-trips through JSON with two-key updates map", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-def"),
			Updates: lib.TaskFrontmatter{
				"status": "completed",
				"phase":  "done",
			},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got task.UpdateFrontmatterCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Updates).To(HaveLen(2))
		Expect(got.Updates["status"]).To(Equal("completed"))
		Expect(got.Updates["phase"]).To(Equal("done"))
	})

	It("omits body key when Body is nil", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-t"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).NotTo(ContainSubstring(`"body"`))
	})

	It("JSON contains taskIdentifier and updates keys", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("t"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		jsonStr := string(data)
		Expect(jsonStr).To(ContainSubstring(`"taskIdentifier"`))
		Expect(jsonStr).To(ContainSubstring(`"updates"`))
	})
})

var _ = Describe("BodySection", func() {
	It("round-trips through JSON", func() {
		bs := task.BodySection{
			Heading: "## H",
			Section: "## H\n\ntext\n",
		}
		data, err := json.Marshal(bs)
		Expect(err).To(BeNil())

		var got task.BodySection
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Heading).To(Equal(bs.Heading))
		Expect(got.Section).To(Equal(bs.Section))
	})
})

var _ = Describe("UpdateFrontmatterCommand.Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("valid: TaskIdentifier non-empty + Updates non-empty succeeds", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("valid: TaskIdentifier non-empty + Body non-nil (Updates empty) succeeds", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Body:           &task.BodySection{Heading: "## H", Section: "## H\n"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("valid: TaskIdentifier non-empty + both Updates and Body set succeeds", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Updates:        lib.TaskFrontmatter{"status": "done"},
			Body:           &task.BodySection{Heading: "## H", Section: "## H\n"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("error: empty TaskIdentifier + non-empty Updates", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier(""),
			Updates:        lib.TaskFrontmatter{"status": "done"},
		}
		err := cmd.Validate(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Or(ContainSubstring("TaskIdentifier"), ContainSubstring("empty")))
	})

	It("error: non-empty TaskIdentifier + empty Updates + nil Body", func() {
		cmd := task.UpdateFrontmatterCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Updates:        nil,
			Body:           nil,
		}
		err := cmd.Validate(ctx)
		Expect(err).To(HaveOccurred())
		Expect(
			err.Error(),
		).To(Or(ContainSubstring("UpdatesOrBody"), ContainSubstring("at least one")))
	})
})
