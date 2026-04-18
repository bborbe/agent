// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/mocks"
	"github.com/bborbe/agent/task/controller/pkg/result"
)

var errTest = errors.New("test error")

func extractTestFrontmatter(content string) (map[string]interface{}, error) {
	if !strings.HasPrefix(content, "---\n") {
		return nil, errors.New("no frontmatter")
	}
	rest := content[4:]
	before, _, found := strings.Cut(rest, "\n---\n")
	if !found {
		return nil, errors.New("frontmatter not closed")
	}
	var fm map[string]interface{}
	if err := yaml.Unmarshal([]byte(before), &fm); err != nil {
		return nil, err
	}
	return fm, nil
}

var _ = Describe("ResultWriter", func() {
	var (
		ctx        context.Context
		tmpDir     string
		taskDir    string
		fakeGit    *mocks.FakeGitClient
		writer     result.ResultWriter
		taskFile   lib.Task
		identifier lib.TaskIdentifier
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "result-writer-test-*")
		Expect(err).NotTo(HaveOccurred())

		taskDir = "tasks"
		Expect(os.MkdirAll(filepath.Join(tmpDir, taskDir), 0750)).To(Succeed())

		fakeGit = &mocks.FakeGitClient{}
		fakeGit.PathReturns(tmpDir)
		fakeGit.AtomicWriteAndCommitPushStub = func(ctx context.Context, absPath string, content []byte, message string) error {
			return os.WriteFile(absPath, content, 0600)
		}

		identifier = lib.TaskIdentifier("test-task-uuid-1234")
		writer = result.NewResultWriter(fakeGit, taskDir)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	writeTaskFile := func(name, content string) string {
		absPath := filepath.Join(tmpDir, taskDir, name)
		Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())
		return absPath
	}

	Describe("WriteResult", func() {
		Context("normal write", func() {
			It("writes frontmatter and content to the matched file", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: in-progress\nassignee: backtest-agent\ntags:\n  - agent-task\n  - test\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "done",
						"phase":           "done",
					},
					Content: lib.TaskContent("New content\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())
				Expect(string(written)).To(HavePrefix("---\n"))
				Expect(string(written)).To(ContainSubstring("status: done"))
				Expect(string(written)).To(ContainSubstring("phase: done"))
				Expect(string(written)).To(ContainSubstring("assignee: backtest-agent"))
				Expect(string(written)).To(ContainSubstring("agent-task"))
				Expect(string(written)).To(ContainSubstring("---\nNew content\n"))

				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
				_, _, _, msg := fakeGit.AtomicWriteAndCommitPushArgsForCall(0)
				Expect(msg).To(ContainSubstring(string(identifier)))
			})
		})

		Context("frontmatter merge", func() {
			It("preserves existing frontmatter keys not sent by agent", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nassignee: backtest-agent\ntags:\n  - a\n  - b\ncustom_field: hello\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "completed",
						"phase":           "done",
					},
					Content: lib.TaskContent("New content\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())
				s := string(written)
				Expect(s).To(ContainSubstring("assignee: backtest-agent"))
				Expect(s).To(ContainSubstring("custom_field: hello"))
				Expect(s).To(ContainSubstring("status: completed"))
				Expect(s).To(ContainSubstring("phase: done"))
				Expect(s).To(ContainSubstring("task_identifier: test-task-uuid-1234"))
				// tags preserved
				Expect(s).To(ContainSubstring("- a"))
				Expect(s).To(ContainSubstring("- b"))
			})

			It("agent keys override existing keys", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: in_progress\nphase: in_progress\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "completed",
						"phase":           "done",
					},
					Content: lib.TaskContent("New content\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())
				s := string(written)
				Expect(s).To(ContainSubstring("status: completed"))
				Expect(s).To(ContainSubstring("phase: done"))
				Expect(s).NotTo(ContainSubstring("in_progress"))
			})
		})

		Context("overwrite", func() {
			It("fully replaces content on second call", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: in-progress\n---\nFirst content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "done",
					},
					Content: lib.TaskContent("First result\n"),
				}
				Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())

				taskFile.Content = lib.TaskContent("Second result\n")
				taskFile.Frontmatter["status"] = "closed"
				Expect(writer.WriteResult(ctx, taskFile)).To(Succeed())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())
				Expect(string(written)).To(ContainSubstring("Second result\n"))
				Expect(string(written)).To(ContainSubstring("status: closed"))
				Expect(string(written)).NotTo(ContainSubstring("First result"))

				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(2))
			})
		})

		Context("unknown task identifier", func() {
			It("returns nil without committing when no matching file found", func() {
				writeTaskFile(
					"other-task.md",
					"---\ntask_identifier: different-uuid\nstatus: open\n---\nSome content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: lib.TaskIdentifier("non-existent-uuid"),
					Frontmatter:    lib.TaskFrontmatter{"status": "done"},
					Content:        lib.TaskContent("Result\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(0))
			})
		})

		Context("empty frontmatter", func() {
			It("preserves existing keys when agent sends empty frontmatter", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nassignee: backtest-agent\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter:    lib.TaskFrontmatter{},
					Content:        lib.TaskContent("New content\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())
				s := string(written)
				Expect(s).To(ContainSubstring("task_identifier: test-task-uuid-1234"))
				Expect(s).To(ContainSubstring("assignee: backtest-agent"))
				Expect(s).To(ContainSubstring("---\nNew content\n"))
			})
		})

		Context("frontmatter with nested values", func() {
			It("serializes lists and nested maps correctly", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: open\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "done",
						"tags":            []interface{}{"agent-task", "automated"},
						"meta": map[string]interface{}{
							"model": "claude-sonnet-4-6",
						},
					},
					Content: lib.TaskContent("Result content\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())

				// Parse and verify frontmatter
				s := string(written)
				Expect(s).To(HavePrefix("---\n"))
				parts := strings.SplitN(s[4:], "\n---\n", 2)
				Expect(parts).To(HaveLen(2))

				var parsedFm map[string]interface{}
				Expect(yaml.Unmarshal([]byte(parts[0]), &parsedFm)).To(Succeed())
				Expect(parsedFm["status"]).To(Equal("done"))

				tags, ok := parsedFm["tags"].([]interface{})
				Expect(ok).To(BeTrue())
				Expect(tags).To(ContainElements("agent-task", "automated"))

				meta, ok := parsedFm["meta"].(map[string]interface{})
				Expect(ok).To(BeTrue())
				Expect(meta["model"]).To(Equal("claude-sonnet-4-6"))

				Expect(parts[1]).To(Equal("Result content\n"))
			})
		})

		Context("content with YAML delimiters", func() {
			It("preserves bare --- lines in body without escaping", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: open\n---\nOld content\n",
				)

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "done",
					},
					Content: lib.TaskContent(
						"## Result\n\nOutput:\n---\nsome yaml block\n---\nDone.\n",
					),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "my-task.md"))
				Expect(readErr).NotTo(HaveOccurred())

				s := string(written)
				// Verify frontmatter is parseable
				Expect(s).To(HavePrefix("---\n"))
				parts := strings.SplitN(s[4:], "\n---\n", 2)
				Expect(parts).To(HaveLen(2))

				var parsedFm map[string]interface{}
				Expect(yaml.Unmarshal([]byte(parts[0]), &parsedFm)).To(Succeed())
				Expect(parsedFm["status"]).To(Equal("done"))

				// Body --- preserved as-is (valid markdown horizontal rule)
				Expect(parts[1]).To(ContainSubstring("\n---\n"))
				Expect(parts[1]).NotTo(ContainSubstring(`\-\-\-`))

				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
			})
		})

		Context("realistic end-to-end", func() {
			It("reads a real Obsidian task, applies agent result, and produces correct output", func() {
				// Realistic task file matching actual Obsidian format:
				// frontmatter + Tags line + --- separator + body
				originalTask := `---
task_identifier: e2e-uuid-1234-5678
status: in_progress
phase: ai_review
assignee: backtest-agent
stage: dev
planned_date: "2026-04-17"
page_type: task
tags:
  - agent-task
  - backtest
---
Tags: [[Task]] [[Trading]]

---

Run a backtest for strategy **capitalcom-backtest-BACKTEST** from 2026-04-10 to 2026-04-17.

## Details

- **Strategy:** capitalcom-backtest-BACKTEST
- **From:** 2026-04-10
- **Until:** 2026-04-17
`
				writeTaskFile("e2e-backtest.md", originalTask)

				// Simulate agent result: status completed, body includes --- separators
				taskFile = lib.Task{
					TaskIdentifier: lib.TaskIdentifier("e2e-uuid-1234-5678"),
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "e2e-uuid-1234-5678",
						"status":          "completed",
						"phase":           "done",
					},
					Content: lib.TaskContent(`Tags: [[Task]] [[Trading]]

---

Run a backtest for strategy **capitalcom-backtest-BACKTEST** from 2026-04-10 to 2026-04-17.

## Details

- **Strategy:** capitalcom-backtest-BACKTEST
- **From:** 2026-04-10
- **Until:** 2026-04-17

## Result

- **Strategy:** capitalcom-backtest-BACKTEST
- **Period:** 2026-04-10 — 2026-04-17
- **Backtest ID:** b3b44eb0-60d9-40b9-9e7d-5afdc3272020
- **Status:** DONE
`),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).NotTo(HaveOccurred())

				written, readErr := os.ReadFile(filepath.Join(tmpDir, taskDir, "e2e-backtest.md"))
				Expect(readErr).NotTo(HaveOccurred())
				s := string(written)

				// 1. File starts with frontmatter delimiter
				Expect(s).To(HavePrefix("---\n"))

				// 2. Parse frontmatter correctly
				parts := strings.SplitN(s[4:], "\n---\n", 2)
				Expect(parts).To(HaveLen(2), "frontmatter must be closed by ---")

				var parsedFm map[string]interface{}
				Expect(yaml.Unmarshal([]byte(parts[0]), &parsedFm)).To(Succeed())

				// 3. Agent keys override existing
				Expect(parsedFm["status"]).To(Equal("completed"))
				Expect(parsedFm["phase"]).To(Equal("done"))
				Expect(parsedFm["task_identifier"]).To(Equal("e2e-uuid-1234-5678"))

				// 4. Existing keys NOT in agent result are preserved
				Expect(parsedFm["assignee"]).To(Equal("backtest-agent"))
				Expect(parsedFm["stage"]).To(Equal("dev"))
				Expect(parsedFm["page_type"]).To(Equal("task"))
				Expect(parsedFm["planned_date"]).To(Equal("2026-04-17"))

				// 5. Tags list preserved
				tags, ok := parsedFm["tags"].([]interface{})
				Expect(ok).To(BeTrue(), "tags should be a list")
				Expect(tags).To(ContainElements("agent-task", "backtest"))

				// 6. Body contains --- as-is (not escaped to \-\-\-)
				body := parts[1]
				Expect(body).To(ContainSubstring("\n---\n"), "body --- must be preserved")
				Expect(body).NotTo(ContainSubstring(`\-\-\-`), "body --- must NOT be escaped")

				// 7. Body contains result section
				Expect(body).To(ContainSubstring("## Result"))
				Expect(body).To(ContainSubstring("b3b44eb0-60d9-40b9-9e7d-5afdc3272020"))
				Expect(body).To(ContainSubstring("DONE"))

				// 8. Body contains Tags line (Obsidian links)
				Expect(body).To(ContainSubstring("Tags: [[Task]] [[Trading]]"))

				// 9. Committed exactly once
				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))

				// 10. Verify the full file can be re-read and re-parsed
				// (simulates controller reading it again on next cycle)
				reParsedFm, reParseErr := extractTestFrontmatter(s)
				Expect(reParseErr).NotTo(HaveOccurred())
				Expect(reParsedFm["status"]).To(Equal("completed"))
				Expect(reParsedFm["phase"]).To(Equal("done"))
			})
		})

		Context("atomic write and push error", func() {
			It("returns error when AtomicWriteAndCommitPush fails", func() {
				writeTaskFile(
					"my-task.md",
					"---\ntask_identifier: test-task-uuid-1234\nstatus: open\n---\nOld content\n",
				)
				fakeGit.AtomicWriteAndCommitPushStub = func(ctx context.Context, absPath string, content []byte, message string) error {
					return errTest
				}

				taskFile = lib.Task{
					TaskIdentifier: identifier,
					Frontmatter: lib.TaskFrontmatter{
						"task_identifier": "test-task-uuid-1234",
						"status":          "done",
					},
					Content: lib.TaskContent("Result\n"),
				}

				err := writer.WriteResult(ctx, taskFile)
				Expect(err).To(HaveOccurred())
				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
			})
		})
	})
})
