// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package task_test

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/bborbe/cqrs/base"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/lib/command/task"
)

var _ = Describe("CreateCommandOperation", func() {
	It("has expected string value", func() {
		Expect(task.CreateCommandOperation).To(Equal(base.CommandOperation("create-task")))
	})
})

var _ = Describe("CreateCommand", func() {
	It("round-trips through JSON with frontmatter and body", func() {
		cmd := task.CreateCommand{
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

		var got task.CreateCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TaskIdentifier).To(Equal(cmd.TaskIdentifier))
		Expect(got.Title).To(Equal(cmd.Title))
		Expect(got.Frontmatter).To(HaveLen(2))
		Expect(got.Frontmatter["assignee"]).To(Equal("alice"))
		Expect(got.Frontmatter["status"]).To(Equal("todo"))
		Expect(got.Body).To(Equal(cmd.Body))
	})

	It("omits body field when empty (omitempty)", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-nobody"),
			Title:          "My Task Name",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).NotTo(ContainSubstring(`"body"`))

		var got task.CreateCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.Title).To(Equal(cmd.Title))
		Expect(got.Body).To(BeEmpty())
	})

	It("JSON contains taskIdentifier, title, frontmatter keys", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("t1"),
			Title:          "T",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		jsonStr := string(data)
		Expect(jsonStr).To(ContainSubstring(`"taskIdentifier"`))
		Expect(jsonStr).To(ContainSubstring(`"title"`))
		Expect(jsonStr).To(ContainSubstring(`"frontmatter"`))
		Expect(jsonStr).NotTo(ContainSubstring(`"body"`))
	})

	It("round-trips with empty TargetVault: marshaled JSON has no targetVault key", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-novault"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).NotTo(ContainSubstring("targetVault"))

		var got task.CreateCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TargetVault).To(BeEmpty())
	})

	It("round-trips with explicit TargetVault: JSON contains targetVault value", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-personal"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			TargetVault:    "personal",
		}
		data, err := json.Marshal(cmd)
		Expect(err).To(BeNil())
		Expect(string(data)).To(ContainSubstring(`"targetVault":"personal"`))

		var got task.CreateCommand
		Expect(json.Unmarshal(data, &got)).To(Succeed())
		Expect(got.TargetVault).To(Equal("personal"))
	})
})

var _ = Describe("CreateCommand.Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("valid title succeeds", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("empty title returns error containing 'empty'", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(MatchError(ContainSubstring("empty")))
	})

	It("title of exactly 200 runes succeeds", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          strings.Repeat("a", 200),
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("title of 201 runes returns error", func() {
		cmd := task.CreateCommand{
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
			cmd := task.CreateCommand{
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
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "some..title",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("leading space returns error", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          " leading",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("trailing space returns error", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "trailing ",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("leading dot returns error", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          ".hidden",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("trailing dot returns error", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "trailing.",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	DescribeTable("Windows reserved names",
		func(title string) {
			cmd := task.CreateCommand{
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
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "Täsk Überblick",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("dot mid-name succeeds", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "my.task-name",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("body exceeding 500 KiB returns error", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           strings.Repeat("x", 500*1024+1),
		}
		Expect(cmd.Validate(ctx)).To(HaveOccurred())
	})

	It("body of exactly 500 KiB succeeds", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           strings.Repeat("x", 500*1024),
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	It("empty body succeeds", func() {
		cmd := task.CreateCommand{
			TaskIdentifier: lib.TaskIdentifier("task-1"),
			Title:          "My Task",
			Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
			Body:           "",
		}
		Expect(cmd.Validate(ctx)).To(Succeed())
	})

	DescribeTable("TargetVault empty value is valid",
		func(targetVault string) {
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "T",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
				TargetVault:    targetVault,
			}
			Expect(cmd.Validate(ctx)).To(Succeed())
		},
		Entry("empty string", ""),
		Entry("openclaw", "openclaw"),
		Entry("personal", "personal"),
		Entry("vault-2", "vault-2"),
	)

	DescribeTable("TargetVault invalid value is rejected",
		func(targetVault string) {
			cmd := task.CreateCommand{
				TaskIdentifier: lib.TaskIdentifier("task-1"),
				Title:          "T",
				Frontmatter:    lib.TaskFrontmatter{"status": "todo"},
				TargetVault:    targetVault,
			}
			err := cmd.Validate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("TargetVault"))
		},
		Entry("capitalized", "Personal"),
		Entry("leading space", " personal"),
		Entry("internal space", "per sonal"),
		Entry("leading digit", "1personal"),
		Entry("leading hyphen", "-personal"),
	)
})
