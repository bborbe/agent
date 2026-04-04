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
			It("escapes bare --- lines so frontmatter is not corrupted", func() {
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
				// Verify frontmatter is parseable — exactly two delimiters wrap it
				Expect(s).To(HavePrefix("---\n"))
				parts := strings.SplitN(s[4:], "\n---\n", 2)
				Expect(parts).To(HaveLen(2))

				var parsedFm map[string]interface{}
				Expect(yaml.Unmarshal([]byte(parts[0]), &parsedFm)).To(Succeed())
				Expect(parsedFm["status"]).To(Equal("done"))

				// Bare --- in content must be escaped
				Expect(parts[1]).To(ContainSubstring(`\-\-\-`))
				Expect(parts[1]).NotTo(ContainSubstring("\n---\n"))

				Expect(fakeGit.AtomicWriteAndCommitPushCallCount()).To(Equal(1))
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
