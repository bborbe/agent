// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package command_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/cqrs/cdb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	"github.com/bborbe/agent/task/controller/mocks"
	"github.com/bborbe/agent/task/controller/pkg/command"
)

var _ = Describe("NewUpdateFrontmatterExecutor", func() {
	var (
		ctx      context.Context
		tmpDir   string
		taskDir  string
		fakeGit  *mocks.FakeGitClient
		executor cdb.CommandObjectExecutorTx
		schemaID cdb.SchemaID
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "update-fm-test-*")
		Expect(err).NotTo(HaveOccurred())

		taskDir = "tasks"
		Expect(os.MkdirAll(filepath.Join(tmpDir, taskDir), 0750)).To(Succeed())

		fakeGit = &mocks.FakeGitClient{}
		fakeGit.PathReturns(tmpDir)
		fakeGit.AtomicReadModifyWriteAndCommitPushStub = func(
			ctx context.Context,
			absPath string,
			modify func([]byte) ([]byte, error),
			message string,
		) error {
			current, err := os.ReadFile(absPath) // #nosec G304 -- test helper
			if err != nil {
				return err
			}
			updated, err := modify(current)
			if err != nil {
				return err
			}
			return os.WriteFile(absPath, updated, 0600) // #nosec G306 -- test helper
		}

		executor = command.NewUpdateFrontmatterExecutor(fakeGit, taskDir)
		schemaID = cdb.SchemaID{Group: "agent", Kind: "task", Version: "v1"}
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	writeTaskFile := func(name, content string) string {
		absPath := filepath.Join(tmpDir, taskDir, name)
		Expect(os.WriteFile(absPath, []byte(content), 0600)).To(Succeed())
		return absPath
	}

	parseFrontmatter := func(absPath string) map[string]interface{} {
		content, err := os.ReadFile(absPath) // #nosec G304 -- test helper
		Expect(err).NotTo(HaveOccurred())
		s := string(content)
		Expect(s).To(HavePrefix("---\n"))
		rest := s[4:]
		before, _, found := strings.Cut(rest, "\n---\n")
		Expect(found).To(BeTrue())
		var fm map[string]interface{}
		Expect(yaml.Unmarshal([]byte(before), &fm)).To(Succeed())
		return fm
	}

	buildCmdObj := func(cmd lib.UpdateFrontmatterCommand) cdb.CommandObject {
		event, err := base.ParseEvent(ctx, cmd)
		Expect(err).NotTo(HaveOccurred())
		return cdb.CommandObject{
			Command: base.Command{
				RequestID: base.NewRequestID(),
				Operation: command.UpdateFrontmatterCommandOperation,
				Initiator: "test",
				Data:      event,
			},
			SchemaID: schemaID,
		}
	}

	Describe("CommandOperation", func() {
		It("returns update-frontmatter", func() {
			Expect(
				executor.CommandOperation(),
			).To(Equal(base.CommandOperation("update-frontmatter")))
		})
	})

	Describe("HandleCommand", func() {
		Context("only named keys change", func() {
			It("updates only the specified key and leaves others unchanged", func() {
				taskFile := writeTaskFile(
					"task.md",
					"---\ntask_identifier: update-test-uuid\nstatus: in_progress\nphase: ai_review\nassignee: claude\n---\nbody\n",
				)
				cmd := buildCmdObj(lib.UpdateFrontmatterCommand{
					TaskIdentifier: lib.TaskIdentifier("update-test-uuid"),
					Updates: lib.TaskFrontmatter{
						"phase": "human_review",
					},
				})
				_, _, err := executor.HandleCommand(ctx, nil, cmd)
				Expect(err).NotTo(HaveOccurred())
				fm := parseFrontmatter(taskFile)
				Expect(fm["phase"]).To(Equal("human_review"))
				Expect(fm["status"]).To(Equal("in_progress"))
				Expect(fm["assignee"]).To(Equal("claude"))
			})
		})

		Context("empty updates", func() {
			It("returns nil without writing when Updates is nil", func() {
				writeTaskFile(
					"task.md",
					"---\ntask_identifier: noop-uuid\nstatus: open\n---\nbody\n",
				)
				cmd := buildCmdObj(lib.UpdateFrontmatterCommand{
					TaskIdentifier: lib.TaskIdentifier("noop-uuid"),
					Updates:        nil,
				})
				_, _, err := executor.HandleCommand(ctx, nil, cmd)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeGit.AtomicReadModifyWriteAndCommitPushCallCount()).To(Equal(0))
			})

			It("returns nil without writing when Updates is empty map", func() {
				writeTaskFile(
					"task.md",
					"---\ntask_identifier: noop2-uuid\nstatus: open\n---\nbody\n",
				)
				cmd := buildCmdObj(lib.UpdateFrontmatterCommand{
					TaskIdentifier: lib.TaskIdentifier("noop2-uuid"),
					Updates:        lib.TaskFrontmatter{},
				})
				_, _, err := executor.HandleCommand(ctx, nil, cmd)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeGit.AtomicReadModifyWriteAndCommitPushCallCount()).To(Equal(0))
			})
		})

		Context("task not found", func() {
			It("returns nil without writing when no matching file exists", func() {
				_, _, err := executor.HandleCommand(
					ctx,
					nil,
					buildCmdObj(lib.UpdateFrontmatterCommand{
						TaskIdentifier: lib.TaskIdentifier("nonexistent-uuid"),
						Updates:        lib.TaskFrontmatter{"phase": "human_review"},
					}),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(fakeGit.AtomicReadModifyWriteAndCommitPushCallCount()).To(Equal(0))
			})
		})

		Context("multiple updates", func() {
			It("applies all specified keys without touching unspecified ones", func() {
				taskFile := writeTaskFile(
					"task.md",
					"---\ntask_identifier: multi-update-uuid\nstatus: in_progress\nphase: ai_review\nassignee: claude\ncustom: preserve\n---\nbody\n",
				)
				cmd := buildCmdObj(lib.UpdateFrontmatterCommand{
					TaskIdentifier: lib.TaskIdentifier("multi-update-uuid"),
					Updates: lib.TaskFrontmatter{
						"status": "completed",
						"phase":  "done",
					},
				})
				_, _, err := executor.HandleCommand(ctx, nil, cmd)
				Expect(err).NotTo(HaveOccurred())
				fm := parseFrontmatter(taskFile)
				Expect(fm["status"]).To(Equal("completed"))
				Expect(fm["phase"]).To(Equal("done"))
				Expect(fm["assignee"]).To(Equal("claude"))
				Expect(fm["custom"]).To(Equal("preserve"))
			})
		})
	})
})
