// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib_test

import (
	"context"
	"encoding/json"
	"strings"

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

	It("CreateTaskCommandOperation has expected string value", func() {
		Expect(
			lib.CreateTaskCommandOperation,
		).To(Equal(base.CommandOperation("create-task")))
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
		Entry("CreateTaskCommandOperation", lib.CreateTaskCommandOperation),
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

var _ = Describe("CreateTaskCommand", func() {
	It("round-trips through JSON with frontmatter and body", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-new"),
			Title:          "My Task Name",
			Frontmatter: lib.TaskFrontmatter{
				"assignee": "alice",
				"status":   "todo",
			},
			Body: "## Description\nsome content\n",
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())

		var got lib.CreateTaskCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Title).To(Equal(cmd.Title))
		Expect(got.Frontmatter).To(HaveLen(2))
		Expect(got.Frontmatter["assignee"]).To(Equal("alice"))
		Expect(got.Frontmatter["status"]).To(Equal("todo"))
		Expect(got.Body).To(Equal(cmd.Body))
	})

	It("omits body field when empty", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-nobody"),
			Title:          "My Task Name",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).NotTo(ContainSubstring(`"body"`))

		var got lib.CreateTaskCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Title).To(Equal(cmd.Title))
		Expect(got.Body).To(BeEmpty())
	})
})

var _ = Describe("CreateTaskCommand.Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("valid title succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("empty title returns error containing 'empty'", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(MatchError(ContainSubstring("empty")))
	})

	It("title of exactly 200 runes succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          strings.Repeat("a", 200),
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("title of 201 runes returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          strings.Repeat("a", 201),
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		err := cmd.Validate(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Or(ContainSubstring("exceed"), ContainSubstring("200")))
	})

	DescribeTable("forbidden characters",
		func(title string) {
			cmd := lib.CreateTaskCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          title,
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			}
			Expect(cmd.Validate(ctx)).To(HaveOccurred())
		},
		Entry("less-than <", "bad<title"),
		Entry("greater-than >", "bad>title"),
		Entry("colon :", "bad:title"),
		Entry("double-quote \"", "bad\"title"),
		Entry("forward-slash /", "bad/title"),
		Entry("backslash \\", "bad\\title"),
		Entry("pipe |", "bad|title"),
		Entry("question-mark ?", "bad?title"),
		Entry("asterisk *", "bad*title"),
		Entry("control char 0x01", "bad\x01title"),
		Entry("DEL 0x7F", "bad\x7Ftitle"),
	)

	It("path traversal '..' returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "some..title",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("leading space returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          " leading",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("trailing space returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "trailing ",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("leading dot returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          ".hidden",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("trailing dot returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "trailing.",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	DescribeTable("Windows reserved names",
		func(title string) {
			cmd := lib.CreateTaskCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          title,
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			}
			Expect(cmd.Validate(ctx)).To(HaveOccurred())
		},
		Entry("CON uppercase", "CON"),
		Entry("con lowercase", "con"),
		Entry("Con mixed", "Con"),
		Entry("CON.md with extension", "CON.md"),
		Entry("PRN", "PRN"),
		Entry("AUX", "AUX"),
		Entry("NUL", "NUL"),
		Entry("COM1", "COM1"),
		Entry("COM9", "COM9"),
		Entry("LPT1", "LPT1"),
		Entry("LPT9", "LPT9"),
	)

	It("unicode title succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "Täsk Überblick",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("dot mid-name succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "my.task-name",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("body exceeding 500 KiB returns error", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           strings.Repeat("x", 500*1024+1),
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("body of exactly 500 KiB succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           strings.Repeat("x", 500*1024),
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("empty body succeeds", func() {
		cmd := lib.CreateTaskCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           "",
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})
})
